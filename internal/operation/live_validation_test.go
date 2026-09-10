package operation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func liveTestField(name string, kind schema.FieldType) schema.Field {
	path, _ := query.NewPath(strings.Split(name, ".")...)
	parts := strings.Split(name, ".")
	return schema.Field{ID: schema.StableID(name), Name: parts[len(parts)-1], Path: path, Type: kind}
}
func liveTestCollection(fields ...schema.Field) Collection {
	return Collection{Schema: schema.Collection{ID: "products", Slug: "products", Fields: fields}}
}
func liveTestEngine(t *testing.T, collection Collection, values store.Values) (*Engine, string) {
	t.Helper()
	backend := teststore.New()
	id := ""
	if values != nil {
		tx, err := backend.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		document, err := tx.Create(t.Context(), store.CreateRequest{Collection: collection.Schema, ID: "product-a", Values: values})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(t.Context()); err != nil {
			t.Fatal(err)
		}
		id = document.ID
	}
	engine, err := New(Config{Store: backend, Collections: []Collection{collection}})
	if err != nil {
		t.Fatal(err)
	}
	return engine, id
}
func liveAssertStatus(t *testing.T, err error, status int) {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) || failure.Status != status {
		t.Fatalf("error=%#v, want status %d", err, status)
	}
}

func TestLiveValidationResourceAdmissionAndAtomicPredicates(t *testing.T) {
	title := liveTestField("title", schema.FieldTypeText)
	for _, kind := range []operation.Kind{operation.Admin, operation.Read, operation.Create, operation.Update} {
		t.Run(string(kind), func(t *testing.T) {
			calls := 0
			collection := liveTestCollection(title)
			collection.Bindings = []FieldBinding{{Field: title, LiveValidators: []FieldLiveValidator{func(Context) ([]schema.Issue, bool, error) { calls++; return nil, true, nil }}}}
			collection.Access = map[operation.Kind]Access{kind: func(Context) (Decision, error) { return Decision{Kind: Deny}, nil }}
			var values store.Values
			if kind == operation.Update {
				values = store.Values{"title": store.String("old")}
			}
			engine, id := liveTestEngine(t, collection, values)
			_, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", ID: id, Data: store.Values{"title": store.String("new")}, Fields: []string{"title"}})
			liveAssertStatus(t, err, 403)
			if calls != 0 {
				t.Fatal("denied callback ran")
			}
		})
	}
	for _, kind := range []operation.Kind{operation.Read, operation.Update} {
		t.Run("filtered "+string(kind), func(t *testing.T) {
			collection := liveTestCollection(title)
			calls := 0
			predicate := query.Equal(title.Path, query.String("not persisted")).Node()
			collection.Access = map[operation.Kind]Access{kind: func(Context) (Decision, error) { return Decision{Kind: Where, Access: &predicate}, nil }}
			collection.Bindings = []FieldBinding{{Field: title, LiveValidators: []FieldLiveValidator{func(Context) ([]schema.Issue, bool, error) { calls++; return nil, true, nil }}}}
			engine, id := liveTestEngine(t, collection, store.Values{"title": store.String("persisted")})
			_, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", ID: id, Data: store.Values{"title": store.String("not persisted")}, Fields: []string{"title"}})
			liveAssertStatus(t, err, 404)
			if calls != 0 {
				t.Fatal("browser snapshot satisfied persisted filter")
			}
		})
	}
}

func TestLiveValidationRedactsPriorAndCandidateBeforeCallback(t *testing.T) {
	sku := liveTestField("sku", schema.FieldTypeText)
	secret := liveTestField("secret", schema.FieldTypeText)
	visible := liveTestField("visible", schema.FieldTypeCheckbox)
	collection := liveTestCollection(sku, secret, visible)
	calls := 0
	collection.Bindings = []FieldBinding{
		{Field: secret, Access: FieldRules{Read: func(ctx Context) (bool, error) { value, _ := ctx.Data["visible"].BooleanValue(); return value, nil }}},
		{Field: sku, LiveValidators: []FieldLiveValidator{func(ctx Context) ([]schema.Issue, bool, error) {
			calls++
			for name, values := range map[string]store.Values{"root": ctx.RootData, "siblings": ctx.SiblingData, "prior": ctx.OriginalSiblingData, "input": ctx.InputSiblingData} {
				if _, ok := values["secret"]; ok {
					t.Errorf("%s leaked secret", name)
				}
			}
			if old, _ := ctx.OriginalSiblingData["sku"].StringValue(); old != "old" {
				t.Errorf("prior sku=%q", old)
			}
			if _, ok := ctx.InputSiblingData["sku"]; ok {
				t.Error("retained sku appeared submitted")
			}
			return nil, true, nil
		}}},
	}
	engine, id := liveTestEngine(t, collection, store.Values{"sku": store.String("old"), "secret": store.String("hidden"), "visible": store.Boolean(false)})
	result, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", ID: id, Data: store.Values{"visible": store.Boolean(true)}, Fields: []string{"sku"}})
	if err != nil || calls != 1 || result.Evaluations[0].Status != "checked" {
		t.Fatalf("result=%#v err=%v calls=%d", result, err, calls)
	}
}

func TestLiveValidationRequiresFieldAndAncestorReadWrite(t *testing.T) {
	child := liveTestField("meta.sku", schema.FieldTypeText)
	parent := liveTestField("meta", schema.FieldTypeGroup)
	parent.Nested = &schema.NestedField{Fields: []schema.Field{child}}
	for _, policy := range []string{"parent read", "parent write", "child read", "child write"} {
		t.Run(policy, func(t *testing.T) {
			calls := 0
			collection := liveTestCollection(parent)
			collection.Bindings = []FieldBinding{{Field: parent}, {Field: child, LiveValidators: []FieldLiveValidator{func(Context) ([]schema.Issue, bool, error) { calls++; return nil, true, nil }}}}
			index := 0
			if strings.HasPrefix(policy, "child") {
				index = 1
			}
			deny := func(Context) (bool, error) { return false, nil }
			if strings.HasSuffix(policy, "read") {
				collection.Bindings[index].Access.Read = deny
			} else {
				collection.Bindings[index].Access.Create = deny
			}
			engine, _ := liveTestEngine(t, collection, nil)
			_, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", Data: store.Values{"meta": store.Object(store.Values{"sku": store.String("value")})}, Fields: []string{"meta.sku"}})
			liveAssertStatus(t, err, 403)
			if calls != 0 {
				t.Fatal("protected callback ran")
			}
		})
	}
}

func TestLiveValidationMissingMalformedAndOperationalResults(t *testing.T) {
	child := liveTestField("meta.sku", schema.FieldTypeText)
	parent := liveTestField("meta", schema.FieldTypeGroup)
	parent.Nested = &schema.NestedField{Fields: []schema.Field{child}}
	collection := liveTestCollection(parent)
	calls := 0
	collection.Bindings = []FieldBinding{{Field: parent}, {Field: child, LiveValidators: []FieldLiveValidator{func(ctx Context) ([]schema.Issue, bool, error) {
		if ctx.Value.Kind() != "" && ctx.Value.Kind() != store.ValueNull && ctx.Value.Kind() != store.ValueString {
			return nil, false, nil
		}
		calls++
		text, _ := ctx.Value.StringValue()
		if text == "failure" {
			return nil, false, errors.New("private database information")
		}
		return nil, true, nil
	}}}}
	engine, _ := liveTestEngine(t, collection, nil)
	for name, value := range map[string]store.Value{"missing": {}, "null": store.Null(), "malformed": store.Number(3), "missing scalar": store.Object(store.Values{}), "bad scalar": store.Object(store.Values{"sku": store.Number(3)}), "string": store.Object(store.Values{"sku": store.String("ok")})} {
		t.Run(name, func(t *testing.T) {
			data := store.Values{}
			if value.Kind() != "" {
				data["meta"] = value
			}
			result, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", Data: data, Fields: []string{"meta.sku"}})
			want := "skipped"
			if name == "missing scalar" || name == "string" {
				want = "checked"
			}
			if err != nil || len(result.Evaluations) != 1 || result.Evaluations[0].Status != want {
				t.Fatalf("%#v %v", result, err)
			}
		})
	}
	if calls != 2 {
		t.Fatalf("callbacks=%d", calls)
	}
	_, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", Data: store.Values{"meta": store.Object(store.Values{"sku": store.String("failure")})}, Fields: []string{"meta.sku"}})
	liveAssertStatus(t, err, 500)
	if strings.Contains(err.Error(), "database") {
		t.Fatal("raw failure exposed")
	}
}

func TestLiveValidationRejectsForgedSelectorsAndBounds(t *testing.T) {
	child := liveTestField("rows.sku", schema.FieldTypeText)
	rows := liveTestField("rows", schema.FieldTypeArray)
	rows.Nested = &schema.NestedField{Fields: []schema.Field{child}}
	collection := liveTestCollection(rows)
	collection.Bindings = []FieldBinding{{Field: rows}, {Field: child, LiveValidators: []FieldLiveValidator{func(Context) ([]schema.Issue, bool, error) { return nil, true, nil }}}}
	engine, _ := liveTestEngine(t, collection, nil)
	row := store.Object(store.Values{"_key": store.String("A"), "sku": store.String("x")})
	base := LiveValidationRequest{Collection: "products", Data: store.Values{"rows": store.List(row)}, Fields: []string{"rows.0.sku"}}
	for name, mutate := range map[string]func(*LiveValidationRequest){
		"unknown":        func(r *LiveValidationRequest) { r.Fields = []string{"rows.0.secret"} },
		"row index":      func(r *LiveValidationRequest) { r.Fields = []string{"rows.4.sku"} },
		"negative":       func(r *LiveValidationRequest) { r.Fields = []string{"rows.-1.sku"} },
		"duplicate path": func(r *LiveValidationRequest) { r.Fields = []string{"rows.0.sku", "rows.0.sku"} },
		"too many":       func(r *LiveValidationRequest) { r.Fields = make([]string, 65) },
		"locale":         func(r *LiveValidationRequest) { r.Locale = "fr" },
		"all locales":    func(r *LiveValidationRequest) { r.Locale = "all" },
		"duplicate row":  func(r *LiveValidationRequest) { r.Data = store.Values{"rows": store.List(row, row)} },
		"missing key": func(r *LiveValidationRequest) {
			r.Data = store.Values{"rows": store.List(store.Object(store.Values{"sku": store.String("x")}))}
		},
		"too many scopes": func(r *LiveValidationRequest) { r.Embedded = make([]LiveValidationEmbeddedScope, 9) },
	} {
		t.Run(name, func(t *testing.T) {
			request := base
			mutate(&request)
			_, err := engine.LiveValidate(t.Context(), request)
			liveAssertStatus(t, err, 400)
		})
	}
	request := base
	request.ID = "missing"
	_, err := engine.LiveValidate(t.Context(), request)
	liveAssertStatus(t, err, 404)
}

func TestLiveValidationReadonlyReaderAndCancellation(t *testing.T) {
	sku := liveTestField("sku", schema.FieldTypeText)
	secret := liveTestField("secret", schema.FieldTypeText)
	collection := liveTestCollection(sku, secret)
	hooks := 0
	hook := func(Context) error { hooks++; return nil }
	collection.Hooks = Hooks{BeforeOperation: []Hook{hook}, BeforeRead: []Hook{hook}, AfterRead: []Hook{hook}, BeforeChange: []Hook{hook}}
	var engine *Engine
	var id string
	calls := 0
	collection.Bindings = []FieldBinding{{Field: secret, Access: FieldRules{Read: func(Context) (bool, error) { return false, nil }}}, {Field: sku, LiveValidators: []FieldLiveValidator{func(ctx Context) ([]schema.Issue, bool, error) {
		calls++
		document, err := engine.LiveRead(ctx.Context, CapabilitiesRequest{Collection: "products", ID: id, Actor: ctx.Actor, ActorCollection: ctx.ActorCollection})
		if err != nil {
			return nil, false, err
		}
		if _, ok := document.Values["secret"]; ok {
			t.Error("reader leaked secret")
		}
		_, err = engine.Execute(ctx.Context, Request{Operation: operation.Update, Collection: "products", ID: id, Data: store.Values{"sku": store.String("MUTATED")}})
		if !errors.Is(err, errMutationInReadOnlyTransaction) {
			t.Errorf("mutation error=%v", err)
		}
		return nil, true, nil
	}}}}
	engine, id = liveTestEngine(t, collection, store.Values{"sku": store.String("old"), "secret": store.String("private")})
	result, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", ID: id, Data: store.Values{"sku": store.String("new")}, Fields: []string{"sku"}})
	if err != nil || calls != 1 || len(result.Evaluations) != 1 || hooks != 0 {
		t.Fatalf("result=%#v err=%v calls=%d hooks=%d", result, err, calls, hooks)
	}
	document, err := engine.LiveRead(t.Context(), CapabilitiesRequest{Collection: "products", ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := document.Values["sku"].StringValue(); got != "old" {
		t.Fatalf("persisted=%q", got)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = engine.LiveValidate(canceled, LiveValidationRequest{Collection: "products", ID: id, Data: store.Values{}, Fields: []string{"sku"}})
	if err == nil {
		t.Fatal("canceled request succeeded")
	}
	if calls != 1 {
		t.Fatal("callback invoked after cancellation")
	}
	_, err = engine.LiveRead(canceled, CapabilitiesRequest{Collection: "products", ID: id})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("reader cancellation=%v", err)
	}
}

func TestLiveValidationCooperativeDeadlineDoesNotSucceed(t *testing.T) {
	sku := liveTestField("sku", schema.FieldTypeText)
	collection := liveTestCollection(sku)
	collection.Bindings = []FieldBinding{{Field: sku, LiveValidators: []FieldLiveValidator{func(ctx Context) ([]schema.Issue, bool, error) { <-ctx.Context.Done(); return nil, true, nil }}}}
	engine, _ := liveTestEngine(t, collection, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err := engine.LiveValidate(ctx, LiveValidationRequest{Collection: "products", Data: store.Values{}, Fields: []string{"sku"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline=%v", err)
	}
}

func TestLiveValidationBoundsCallbackIssues(t *testing.T) {
	sku := liveTestField("sku", schema.FieldTypeText)
	collection := liveTestCollection(sku)
	collection.Bindings = []FieldBinding{{Field: sku, LiveValidators: []FieldLiveValidator{func(Context) ([]schema.Issue, bool, error) {
		issues := make([]schema.Issue, 257)
		for i := range issues {
			issues[i] = schema.Issue{Path: "sku", Message: fmt.Sprint(i)}
		}
		return issues, true, nil
	}}}}
	engine, _ := liveTestEngine(t, collection, nil)
	_, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", Data: store.Values{}, Fields: []string{"sku"}})
	liveAssertStatus(t, err, 500)
}

func TestLiveValidationPointAndPolymorphicEnvelopeDecodeBeforeCallback(t *testing.T) {
	point := liveTestField("point", schema.FieldTypePoint)
	reference := liveTestField("reference", schema.FieldTypeRelationship)
	reference.Relationship = &schema.RelationshipField{Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionSlug: "products", CollectionID: "products"}}}
	references := reference
	references.Name = "references"
	references.ID = "references"
	references.Path, _ = query.NewPath("references")
	many := *reference.Relationship
	many.HasMany = true
	references.Relationship = &many
	collection := liveTestCollection(point, reference, references)
	calls := 0
	for _, field := range collection.Schema.Fields {
		collection.Bindings = append(collection.Bindings, FieldBinding{Field: field, LiveValidators: []FieldLiveValidator{func(Context) ([]schema.Issue, bool, error) { calls++; return nil, true, nil }}})
	}
	engine, _ := liveTestEngine(t, collection, nil)
	valid := store.Object(store.Values{"relationTo": store.String("products"), "id": store.String("a")})
	for _, test := range []struct {
		name, path string
		value      store.Value
		status     string
	}{
		{"point pair", "point", store.List(store.Number(1), store.Number(2)), "checked"},
		{"point one", "point", store.List(store.Number(1)), "skipped"},
		{"point three", "point", store.List(store.Number(1), store.Number(2), store.Number(3)), "skipped"},
		{"point string", "point", store.List(store.String("1"), store.Number(2)), "skipped"},
		{"reference valid", "reference", valid, "checked"},
		{"reference missing id", "reference", store.Object(store.Values{"relationTo": store.String("products")}), "skipped"},
		{"reference malformed id", "reference", store.Object(store.Values{"relationTo": store.String("products"), "id": store.Number(3)}), "skipped"},
		{"reference unlisted target", "reference", store.Object(store.Values{"relationTo": store.String("secret"), "id": store.String("a")}), "skipped"},
		{"references valid", "references", store.List(valid), "checked"},
		{"references malformed", "references", store.List(valid, store.String("a")), "skipped"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := calls
			result, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", Data: store.Values{test.path: test.value}, Fields: []string{test.path}})
			if err != nil || result.Evaluations[0].Status != test.status {
				t.Fatalf("%#v %v", result, err)
			}
			if calls-before != map[string]int{"checked": 1, "skipped": 0}[test.status] {
				t.Fatal("malformed value reached callback")
			}
		})
	}
}

func TestLiveValidationResourceLocalQueriesDoNotRunLifecycle(t *testing.T) {
	sku := liveTestField("sku", schema.FieldTypeText)
	collection := liveTestCollection(sku)
	hooks := 0
	reads := 0
	hook := func(Context) error { hooks++; return nil }
	collection.Hooks = Hooks{BeforeRead: []Hook{hook}, AfterRead: []Hook{hook}, BeforeOperation: []Hook{hook}, AfterError: []Hook{hook}}
	var engine *Engine
	collection.Access = map[operation.Kind]Access{operation.Create: func(ctx Context) (Decision, error) {
		result, err := engine.Execute(ctx.Context, Request{Operation: operation.Read, Collection: "products", Filter: query.Equal(sku.Path, query.String("known")), Limit: 10, Select: []query.Path{sku.Path}, Actor: ctx.Actor})
		if err != nil {
			return Decision{}, err
		}
		reads = len(result.Page.Documents)
		return Decision{Kind: Allow}, nil
	}}
	collection.Bindings = []FieldBinding{{Field: sku, LiveValidators: []FieldLiveValidator{func(ctx Context) ([]schema.Issue, bool, error) {
		for _, request := range []Request{{Operation: operation.Read, Collection: "products", Limit: 101}, {Operation: operation.Read, Collection: "products", Populate: []query.Population{{Path: sku.Path, Depth: 1}}}, {Operation: operation.Read, Collection: "products", AllLocales: true}, {Operation: operation.Update, Collection: "products", ID: "product-a"}} {
			if _, err := engine.Execute(ctx.Context, request); err == nil {
				t.Error("unsupported advisory operation accepted")
			}
		}
		return nil, true, nil
	}}}}
	engine, _ = liveTestEngine(t, collection, store.Values{"sku": store.String("known")})
	_, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", Data: store.Values{}, Fields: []string{"sku"}})
	if err != nil || reads != 1 || hooks != 0 {
		t.Fatalf("error=%v reads=%d hooks=%d", err, reads, hooks)
	}
}

func TestLiveValidationDetachedSelectorsCannotCrossSchemasOrProtectedOwners(t *testing.T) {
	child := liveTestField("body.widgets.widget.card.sku", schema.FieldTypeText)
	note := liveTestField("body.widgets.widget.note.sku", schema.FieldTypeText)
	owner := liveTestField("body", schema.FieldTypePlugin)
	owner.Plugin = &schema.PluginField{EmbeddedTrees: []schema.EmbeddedTree{{Version: 1, Key: "widgets", Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []schema.BlockType{{Slug: "card", Fields: []schema.Field{child}}, {Slug: "note", Fields: []schema.Field{note}}}}}}}}
	payload := func(kind, key, value string) store.Values {
		return store.Values{"schema": store.String(kind), "uid": store.String(key), "sku": store.String(value)}
	}
	node := func(kind, key, value string) store.Value {
		return store.Object(store.Values{"kind": store.String("widget"), "content": store.Object(payload(kind, key, value))})
	}
	collection := liveTestCollection(owner)
	calls := 0
	denyOwner, denyChild := false, false
	var prior string
	collection.Bindings = []FieldBinding{{Field: owner, Access: FieldRules{Read: func(Context) (bool, error) { return !denyOwner, nil }}}, {Field: child, Access: FieldRules{Update: func(Context) (bool, error) { return !denyChild, nil }}, LiveValidators: []FieldLiveValidator{func(ctx Context) ([]schema.Issue, bool, error) {
		calls++
		prior, _ = ctx.OriginalSiblingData["sku"].StringValue()
		return nil, true, nil
	}}}, {Field: note, LiveValidators: []FieldLiveValidator{func(ctx Context) ([]schema.Issue, bool, error) {
		calls++
		prior, _ = ctx.OriginalSiblingData["sku"].StringValue()
		return nil, true, nil
	}}}}
	engine, id := liveTestEngine(t, collection, store.Values{"body": node("card", "A", "persisted")})
	request := func() LiveValidationRequest {
		return LiveValidationRequest{Collection: "products", ID: id, Data: store.Values{"body": node("card", "A", "current")}, Fields: []string{"sku"}, Embedded: []LiveValidationEmbeddedScope{{Field: "body", TreeKey: "widgets", CaseTag: "widget", VariantSlug: "card", Identity: "A", Data: payload("card", "A", "draft")}}}
	}
	result, err := engine.LiveValidate(t.Context(), request())
	if err != nil || len(result.Evaluations) != 1 || prior != "persisted" {
		t.Fatalf("result=%#v err=%v prior=%q", result, err, prior)
	}
	for name, mutate := range map[string]func(*LiveValidationRequest){
		"forged owner":      func(r *LiveValidationRequest) { r.Embedded[0].Field = "other" },
		"forged tree":       func(r *LiveValidationRequest) { r.Embedded[0].TreeKey = "other" },
		"forged case":       func(r *LiveValidationRequest) { r.Embedded[0].CaseTag = "other" },
		"forged schema":     func(r *LiveValidationRequest) { r.Embedded[0].VariantSlug = "other" },
		"identity mismatch": func(r *LiveValidationRequest) { r.Embedded[0].Identity = "B" },
		"blank identity": func(r *LiveValidationRequest) {
			r.Embedded[0].Identity = " "
			r.Embedded[0].Data = payload("card", " ", "draft")
		},
		"wrong case identity": func(r *LiveValidationRequest) {
			r.Embedded[0].VariantSlug = "note"
			r.Embedded[0].Data = payload("note", "A", "draft")
		},
		"malformed owner": func(r *LiveValidationRequest) {
			r.Data = store.Values{"body": store.Object(store.Values{"kind": store.String("widget"), "content": store.String("bad")})}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := request()
			mutate(&r)
			before := calls
			_, err := engine.LiveValidate(t.Context(), r)
			liveAssertStatus(t, err, 400)
			if calls != before {
				t.Fatal("forged selector ran callback")
			}
		})
	}
	denyOwner = true
	_, err = engine.LiveValidate(t.Context(), request())
	liveAssertStatus(t, err, 403)
	denyOwner = false
	denyChild = true
	_, err = engine.LiveValidate(t.Context(), request())
	liveAssertStatus(t, err, 403)
	denyChild = false
	// Replacing the persisted Block case never supplies the old case's Prior.
	replaced := request()
	replaced.Data = store.Values{"body": node("note", "A", "new case")}
	replaced.Embedded[0].VariantSlug = "note"
	replaced.Embedded[0].Data = payload("note", "A", "draft")
	if _, err := engine.LiveValidate(t.Context(), replaced); err != nil {
		t.Fatal(err)
	}
	if prior != "" {
		t.Fatalf("replacement inherited Prior %q", prior)
	}
	// A deleted occurrence, reopened as a new draft, also has no persisted Prior.
	deleted := request()
	deleted.Data = store.Values{"body": store.Object(store.Values{"kind": store.String("root"), "items": store.List()})}
	if _, err := engine.LiveValidate(t.Context(), deleted); err != nil {
		t.Fatal(err)
	}
	if prior != "" {
		t.Fatalf("deleted row inherited Prior %q", prior)
	}
}

func TestLiveValidationCannotSupplyReadAdmissionData(t *testing.T) {
	sku := liveTestField("sku", schema.FieldTypeText)
	collection := liveTestCollection(sku)
	collection.Access = map[operation.Kind]Access{operation.Read: func(ctx Context) (Decision, error) {
		if _, submitted := ctx.Data["grantRead"]; submitted {
			return Decision{Kind: Allow}, nil
		}
		return Decision{Kind: Deny}, nil
	}}
	collection.Bindings = []FieldBinding{{Field: sku, LiveValidators: []FieldLiveValidator{func(Context) ([]schema.Issue, bool, error) {
		t.Fatal("forged read admission ran callback")
		return nil, true, nil
	}}}}
	engine, id := liveTestEngine(t, collection, store.Values{"sku": store.String("secret")})
	_, err := engine.LiveValidate(t.Context(), LiveValidationRequest{Collection: "products", ID: id, Data: store.Values{"grantRead": store.Boolean(true)}, Fields: []string{"sku"}})
	liveAssertStatus(t, err, 403)
}
