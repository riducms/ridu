package operation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// An empty store is intentional: admission must not depend on document values
// or on whether a query would have matched any rows.
func TestQueryAdmissionPrecedesStoreReadsAndFieldRuleEvaluation(t *testing.T) {
	private, _ := query.NewPath("privateKey")
	public, _ := query.NewPath("title")
	filter := query.Equal(private, query.String("fixture-value"))
	publicFilter := query.Equal(public, query.String("fixture-title"))
	and, _ := query.And(publicFilter, filter)
	or, _ := query.Or(publicFilter, filter)
	not, _ := query.Not(filter)
	backend := &queryAdmissionStore{Store: teststore.New()}
	calls := 0
	collection := queryAdmissionCollection()
	collection.Schema.Fields[1].Unique, collection.Schema.Fields[1].Index = true, true
	collection.Schema.Fields[1].QueryRestricted = true
	collection.Bindings = []FieldBinding{{ID: "privateKey", Field: collection.Schema.Fields[1], Access: FieldRules{Read: func(Context) (bool, error) {
		calls++
		t.Error("restricted query evaluated its read policy")
		return false, nil
	}}}}
	engine, err := New(Config{Store: backend, Collections: []Collection{collection}})
	if err != nil {
		t.Fatal(err)
	}
	check := func(err error) {
		t.Helper()
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "field_access_denied" || failure.Status != 403 || len(failure.Issues) != 1 || failure.Issues[0].Path != "privateKey" {
			t.Fatalf("admission error = %#v", err)
		}
		if strings.Contains(failure.Message, "fixture-") || strings.Contains(failure.Issues[0].Message, "fixture-") {
			t.Fatal("admission error contains a fixture value")
		}
		if backend.reads != 0 || calls != 0 {
			t.Fatalf("rejection performed %d store reads and %d field evaluations", backend.reads, calls)
		}
	}
	for _, actor := range []*store.Document{nil, {ID: "fixture-actor"}} {
		for _, expression := range []query.Expression{filter, and, or, not} {
			_, err := engine.Execute(t.Context(), Request{Operation: operation.Read, Collection: "posts", Filter: expression, Actor: actor, Limit: 1})
			check(err)
		}
		for _, direction := range []query.Direction{query.Ascending, query.Descending} {
			_, err := engine.Execute(t.Context(), Request{Operation: operation.Read, Collection: "posts", Actor: actor, Sort: []query.Sort{{Path: private, Direction: direction}}})
			check(err)
		}
		for _, kind := range []operation.Kind{operation.Read, operation.Update, operation.Delete} {
			_, err := engine.Execute(t.Context(), Request{Operation: kind, Collection: "posts", ID: "fixture-document", Actor: actor, Filter: filter})
			check(err)
		}
		_, err := engine.Distinct(t.Context(), DistinctRequest{Collection: "posts", Field: private, Actor: actor})
		check(err)
		_, err = engine.Distinct(t.Context(), DistinctRequest{Collection: "posts", Field: public, Filter: filter, Actor: actor})
		check(err)
		_, err = engine.ResolveFilteredSelection(t.Context(), FilteredSelectionRequest{Collection: "posts", Filter: filter, Actor: actor})
		check(err)
		_, err = engine.Execute(t.Context(), Request{Operation: operation.Read, Collection: "posts", Actor: actor, Limit: 1, IndexWindow: &store.IndexWindow{Path: private, LowerBound: "a", UpperBound: "z"}})
		check(err)
	}
}

func TestQueryAdmissionPreservesTrustedPredicatesAndPageScopedReadRules(t *testing.T) {
	backend := &queryAdmissionStore{Store: teststore.New()}
	collection := queryAdmissionCollection()
	private, _ := query.NewPath("privateKey")
	title, _ := query.NewPath("title")
	access := query.Equal(private, query.String("included")).Node()
	collection.Access = map[operation.Kind]Access{operation.Read: func(Context) (Decision, error) { return Decision{Kind: Where, Access: &access}, nil }}
	var evaluated []string
	collection.Schema.Fields[1].QueryRestricted = true
	collection.Bindings = []FieldBinding{{ID: "privateKey", Field: collection.Schema.Fields[1], Access: FieldRules{Read: func(ctx Context) (bool, error) {
		if ctx.Document == nil || ctx.Document.ID != ctx.ID {
			t.Fatal("dynamic Read rule lacks its document")
		}
		evaluated = append(evaluated, ctx.ID)
		return ctx.Actor != nil && ctx.Actor.ID == ctx.ID, nil
	}}}}
	engine, err := New(Config{Store: backend, Collections: []Collection{collection}})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ id, title, private string }{
		{"fixture-a", "Alpha", "included"}, {"fixture-b", "Beta", "included"}, {"fixture-c", "Gamma", "excluded"},
	} {
		_, err := engine.Execute(t.Context(), Request{Operation: operation.Create, Collection: "posts", ImportID: fixture.id, Data: store.Values{"title": store.String(fixture.title), "privateKey": store.String(fixture.private)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, actor := range []*store.Document{nil, {ID: "fixture-b"}} {
		evaluated, backend.reads = nil, 0
		result, err := engine.Execute(t.Context(), Request{Operation: operation.Read, Collection: "posts", Actor: actor, Sort: []query.Sort{{Path: title, Direction: query.Ascending}}, Page: 2, Limit: 1})
		if err != nil || result.Page == nil || result.Page.Total != 2 || len(result.Page.Documents) != 1 || result.Page.Documents[0].ID != "fixture-b" {
			t.Fatalf("access-filtered page = %#v, %v", result.Page, err)
		}
		if backend.reads != 1 || len(evaluated) != 1 || evaluated[0] != "fixture-b" {
			t.Fatalf("pagination performed %d reads, evaluated %v", backend.reads, evaluated)
		}
		_, visible := result.Page.Documents[0].Values["privateKey"]
		if visible != (actor != nil) {
			t.Fatalf("dynamic Read result visibility = %v for actor %#v", visible, actor)
		}
	}
	evaluated, backend.reads = nil, 0
	values, err := engine.Distinct(t.Context(), DistinctRequest{Collection: "posts", Field: title})
	if err != nil || values.Total != 2 || backend.reads != 1 || len(evaluated) != 0 {
		t.Fatalf("trusted distinct = %#v, %v; reads=%d, evaluations=%v", values, err, backend.reads, evaluated)
	}
}

func queryAdmissionCollection() Collection {
	fields := make([]schema.Field, 0, 2)
	for _, name := range []string{"title", "privateKey"} {
		path, _ := query.NewPath(name)
		fields = append(fields, schema.Field{ID: schema.StableID("posts-" + name), Name: name, Path: path, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar})
	}
	return Collection{Schema: schema.Collection{ID: "posts", Slug: "posts", Fields: fields}}
}

type queryAdmissionStore struct {
	*teststore.Store
	reads int
}

func (backend *queryAdmissionStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &queryAdmissionTransaction{Transaction: transaction, owner: backend}, nil
}

func (backend *queryAdmissionStore) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.BeginSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return &queryAdmissionTransaction{Transaction: transaction, owner: backend}, nil
}

type queryAdmissionTransaction struct {
	store.Transaction
	owner *queryAdmissionStore
}

func (transaction *queryAdmissionTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	transaction.owner.reads++
	return transaction.Transaction.Find(ctx, request)
}

func (transaction *queryAdmissionTransaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	transaction.owner.reads++
	return transaction.Transaction.List(ctx, request)
}

func (transaction *queryAdmissionTransaction) ResolveFilteredSelection(ctx context.Context, request store.FilteredSelectionRequest) (store.FilteredSelection, error) {
	transaction.owner.reads++
	return transaction.Transaction.ResolveFilteredSelection(ctx, request)
}

func (transaction *queryAdmissionTransaction) Distinct(ctx context.Context, request store.DistinctRequest) (store.DistinctPage, error) {
	transaction.owner.reads++
	return transaction.Transaction.(store.DistinctTransaction).Distinct(ctx, request)
}

func (transaction *queryAdmissionTransaction) ListWindow(ctx context.Context, request store.Request) (store.Window, error) {
	transaction.owner.reads++
	return transaction.Transaction.(store.WindowTransaction).ListWindow(ctx, request)
}
