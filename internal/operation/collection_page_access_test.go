package operation

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/riducms/ridu/internal/teststore"
	operationkind "github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestCollectionListAccessUsesFullDocumentSnapshots(t *testing.T) {
	titlePath, _ := query.NewPath("title")
	lockedPath, _ := query.NewPath("locked")
	title := schema.Field{ID: "posts-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar}
	locked := schema.Field{ID: "posts-locked", Name: "locked", Path: lockedPath, Type: schema.FieldTypeCheckbox, Category: schema.FieldCategoryScalar}
	backend := &transactionIntentRecordingStore{Store: teststore.New()}
	collection := Collection{
		Schema: schema.Collection{ID: "collection-posts", Slug: "posts", Fields: []schema.Field{title, locked}},
		Access: map[operationkind.Kind]Access{
			operationkind.Update: func(ctx Context) (Decision, error) {
				isLocked, _ := ctx.Data["locked"].BooleanValue()
				if isLocked {
					return Decision{Kind: Deny}, nil
				}
				return Decision{Kind: Allow}, nil
			},
		},
		Bindings: []FieldBinding{{
			ID: "title", Field: title,
			Access: FieldRules{Read: func(ctx Context) (bool, error) {
				isLocked, _ := ctx.Siblings.Get("locked").BooleanValue()
				return !isLocked, nil
			}},
		}},
	}
	engine, err := New(Config{Store: backend, Collections: []Collection{collection}})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		id     string
		locked bool
	}{
		{id: "editable", locked: false},
		{id: "locked", locked: true},
	} {
		_, err := engine.Execute(t.Context(), Request{
			Operation: operationkind.Create, Collection: "posts", ImportID: fixture.id,
			Data: store.Values{"title": store.String(fixture.id), "locked": store.Boolean(fixture.locked)},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	backend.writeBegins, backend.snapshotBegins = 0, 0

	result, err := engine.Execute(t.Context(), Request{
		Operation: operationkind.Read, Collection: "posts", Page: 1, Limit: 10,
		Select: []query.Path{titlePath}, IncludeAccess: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if backend.writeBegins != 0 || backend.snapshotBegins != 1 {
		t.Fatalf("enriched list transactions = write:%d snapshot:%d, want write:0 snapshot:1", backend.writeBegins, backend.snapshotBegins)
	}
	if result.Page == nil || result.PageAccess == nil || len(result.Page.Documents) != 2 || len(result.PageAccess.Documents) != 2 {
		t.Fatalf("enriched page = page:%#v access:%#v", result.Page, result.PageAccess)
	}
	if !result.PageAccess.Collection.Operations.Update {
		t.Fatal("collection-level update capability was not returned")
	}
	if !result.PageAccess.Documents["editable"].Operations.Update || result.PageAccess.Documents["locked"].Operations.Update {
		t.Fatalf("document update capabilities = %#v", result.PageAccess.Documents)
	}
	if !result.PageAccess.Documents["editable"].Fields["title"].Read || result.PageAccess.Documents["locked"].Fields["title"].Read {
		t.Fatalf("document field capabilities = %#v", result.PageAccess.Documents)
	}
}

func TestCollectionListAccessFailureFailsTheRequest(t *testing.T) {
	titlePath, _ := query.NewPath("title")
	collection := Collection{
		Schema: schema.Collection{
			ID: "collection-posts", Slug: "posts",
			Fields: []schema.Field{{ID: "posts-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar}},
		},
		Access: map[operationkind.Kind]Access{
			operationkind.Update: func(ctx Context) (Decision, error) {
				if ctx.ID == "broken" {
					return Decision{}, errors.New("capability backend failed")
				}
				return Decision{Kind: Allow}, nil
			},
		},
	}
	engine, err := New(Config{Store: teststore.New(), Collections: []Collection{collection}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), Request{Operation: operationkind.Create, Collection: "posts", ImportID: "broken", Data: store.Values{"title": store.String("Broken")}}); err != nil {
		t.Fatal(err)
	}

	_, err = engine.Execute(t.Context(), Request{Operation: operationkind.Read, Collection: "posts", Page: 1, Limit: 10, IncludeAccess: true})
	var operationError *Error
	if !errors.As(err, &operationError) || operationError.Code != "access_failed" {
		t.Fatalf("enriched list error = %#v", err)
	}

	ordinary, err := engine.Execute(t.Context(), Request{Operation: operationkind.Read, Collection: "posts", Page: 1, Limit: 10})
	if err != nil || ordinary.Page == nil || ordinary.PageAccess != nil {
		t.Fatalf("ordinary list = %#v, %v", ordinary, err)
	}
}

func TestCollectionListAccessRespectsLocaleAndTrashContext(t *testing.T) {
	collection := Collection{
		Schema: schema.Collection{
			ID: "collection-posts", Slug: "posts",
			Capabilities: schema.Capabilities{Trash: true},
		},
		Access: map[operationkind.Kind]Access{
			operationkind.Delete: func(ctx Context) (Decision, error) {
				if ctx.Operation != operationkind.RestoreDeleted || ctx.Locale == "fr" {
					return Decision{Kind: Allow}, nil
				}
				return Decision{Kind: Deny}, nil
			},
		},
	}
	engine, err := New(Config{
		Store: teststore.New(), Collections: []Collection{collection},
		Localization: &schema.LocalizationSettings{
			DefaultLocale: "en", Locales: []schema.Locale{{Code: "en"}, {Code: "fr"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), Request{Operation: operationkind.Create, Collection: "posts", ImportID: "deleted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), Request{Operation: operationkind.Delete, Collection: "posts", ID: "deleted"}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		locale string
		want   bool
	}{
		{locale: "en", want: false},
		{locale: "fr", want: true},
	} {
		result, err := engine.Execute(t.Context(), Request{
			Operation: operationkind.Read, Collection: "posts", Page: 1, Limit: 10,
			TrashOnly: true, IncludeAccess: true, Locale: test.locale,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.PageAccess == nil || result.PageAccess.Documents["deleted"].Operations.RestoreDeleted != test.want {
			t.Fatalf("%s trash capabilities = %#v, want restore %t", test.locale, result.PageAccess, test.want)
		}
	}
}

// readCountingStore counts the document reads its transactions make.
type readCountingStore struct {
	store.Store
	finds, lists int
}

func (backend *readCountingStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	return &readCountingTransaction{Transaction: transaction, backend: backend}, err
}

func (backend *readCountingStore) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.(store.SnapshotStore).BeginSnapshot(ctx)
	return &readCountingTransaction{Transaction: transaction, backend: backend}, err
}

type readCountingTransaction struct {
	store.Transaction
	backend *readCountingStore
}

func (transaction *readCountingTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	transaction.backend.finds++
	return transaction.Transaction.Find(ctx, request)
}

func (transaction *readCountingTransaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	transaction.backend.lists++
	return transaction.Transaction.List(ctx, request)
}

// Document access costs a bounded number of reads per page: one for the
// documents and one per distinct filtered decision, not reads per document.
// Each result still matches the single-document Capabilities evaluation.
func TestManyDocumentCapabilitiesUseBoundedReads(t *testing.T) {
	path := func(name string) query.Path {
		parsed, _ := query.NewPath(name)
		return parsed
	}
	field := func(name string, kind schema.FieldType) schema.Field {
		return schema.Field{ID: schema.StableID("posts-" + name), Name: name, Path: path(name), Type: kind, Category: schema.FieldCategoryScalar}
	}
	title, author, state := field("title", schema.FieldTypeText), field("author", schema.FieldTypeText), field("state", schema.FieldTypeText)
	backend := &readCountingStore{Store: teststore.New()}
	collection := Collection{
		Schema: schema.Collection{ID: "collection-posts", Slug: "posts", Fields: []schema.Field{title, author, state}},
		Access: map[operationkind.Kind]Access{
			// Every document shares one predicate.
			operationkind.Update: func(ctx Context) (Decision, error) {
				own := query.And(query.Equal(path("author"), ctx.Actor.ID), query.Equal(path("state"), "draft")).Node()
				return Decision{Kind: Where, Access: &own}, nil
			},
			// Drafts and published posts get different predicates.
			operationkind.Delete: func(ctx Context) (Decision, error) {
				current, _ := ctx.Data["state"].StringValue()
				predicate := query.Equal(path("author"), ctx.Actor.ID).Node()
				if current != "draft" {
					predicate = query.Equal(path("state"), "archived").Node()
				}
				return Decision{Kind: Where, Access: &predicate}, nil
			},
		},
		Bindings: []FieldBinding{{
			ID: "title", Field: title,
			Access: FieldRules{
				Read: func(ctx Context) (bool, error) {
					current, _ := ctx.Siblings.Get("state").StringValue()
					return current != "hidden", nil
				},
				Update: func(ctx Context) (bool, error) {
					owner, _ := ctx.Siblings.Get("author").StringValue()
					return owner == ctx.Actor.ID, nil
				},
			},
		}},
	}
	engine, err := New(Config{Store: backend, Collections: []Collection{collection}})
	if err != nil {
		t.Fatal(err)
	}
	authors, states := []string{"ada", "grace"}, []string{"draft", "published", "hidden"}
	for index := range 24 {
		data := store.Values{"title": store.String("Post"), "author": store.String(authors[index%2]), "state": store.String(states[index%3])}
		if _, err := engine.Execute(t.Context(), Request{Operation: operationkind.Create, Collection: "posts", ImportID: "post-" + strconv.Itoa(index), Data: data}); err != nil {
			t.Fatal(err)
		}
	}
	actor := &store.Document{ID: "ada"}
	expected := func(id string) AccessCapabilities {
		t.Helper()
		capabilities, err := engine.Capabilities(t.Context(), CapabilitiesRequest{Collection: "posts", ID: id, Actor: actor})
		if err != nil {
			t.Fatal(err)
		}
		return capabilities
	}

	backend.finds, backend.lists = 0, 0
	result, err := engine.Execute(t.Context(), Request{Operation: operationkind.Read, Collection: "posts", Page: 1, Limit: 24, IncludeAccess: true, Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	// The page, its complete documents, one update predicate and two delete predicates.
	if backend.finds != 0 || backend.lists != 5 {
		t.Fatalf("page access reads = find:%d list:%d, want find:0 list:5", backend.finds, backend.lists)
	}
	if len(result.PageAccess.Documents) != 24 {
		t.Fatalf("page access documents = %d", len(result.PageAccess.Documents))
	}
	for id, capabilities := range result.PageAccess.Documents {
		if want := expected(id); !reflect.DeepEqual(capabilities, want) {
			t.Fatalf("%s page capabilities = %#v, want %#v", id, capabilities, want)
		}
	}

	backend.finds, backend.lists = 0, 0
	selection, err := engine.ResolveFilteredSelection(t.Context(), FilteredSelectionRequest{Collection: "posts", Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	if backend.finds != 0 || backend.lists != 4 || len(selection.Items) != 24 {
		t.Fatalf("selection reads = find:%d list:%d items:%d, want find:0 list:4 items:24", backend.finds, backend.lists, len(selection.Items))
	}
	for _, item := range selection.Items {
		if want := expected(item.ID); !reflect.DeepEqual(item.Capabilities, want) {
			t.Fatalf("%s selection capabilities = %#v, want %#v", item.ID, item.Capabilities, want)
		}
	}
}
