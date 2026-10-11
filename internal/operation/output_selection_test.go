package operation

import (
	"testing"

	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestMissingGlobalDefaultsRespectStoredAndOutputSelection(t *testing.T) {
	headlinePath, _ := query.NewPath("headline")
	privatePath, _ := query.NewPath("privateNote")
	summaryPath, _ := query.NewPath("summary")
	headlineDefault := "Welcome"
	privateDefault := "resolver dependency"
	global := schema.Collection{
		ID: "global-settings", Slug: "settings",
		Capabilities: schema.Capabilities{Global: true},
		Fields: []schema.Field{
			{ID: "headline", Name: "headline", Path: headlinePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Default: &headlineDefault},
			{ID: "private-note", Name: "privateNote", Path: privatePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Default: &privateDefault},
			{ID: "summary", Name: "summary", Path: summaryPath, Type: schema.FieldTypeVirtual, Category: schema.FieldCategoryPresentation, Virtual: &schema.VirtualField{ValueType: schema.ValueTypeString}},
		},
	}
	engine, err := New(Config{
		Store: teststore.New(),
		Collections: []Collection{{
			Key: "global:settings", Schema: global,
			Bindings: []FieldBinding{{ID: "summary", Field: global.Fields[2], Computed: func(_ Context, document store.Document) (store.Value, error) {
				dependency, _ := document.Values["privateNote"].StringValue()
				return store.String("Summary: " + dependency), nil
			}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	stored, err := engine.Execute(t.Context(), Request{
		Operation: operation.Read, Collection: "global:settings", ID: "settings",
		Select: []query.Path{headlinePath}, OutputFields: []query.Path{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if headline, _ := stored.Document.Values["headline"].StringValue(); headline != headlineDefault {
		t.Fatalf("selected synthesized default = %q, want %q", headline, headlineDefault)
	}
	if _, leaked := stored.Document.Values["privateNote"]; leaked {
		t.Fatalf("unselected synthesized default leaked: %#v", stored.Document.Values)
	}
	if _, leaked := stored.Document.Values["summary"]; leaked {
		t.Fatalf("unselected synthesized output leaked: %#v", stored.Document.Values)
	}

	output, err := engine.Execute(t.Context(), Request{
		Operation: operation.Read, Collection: "global:settings", ID: "settings",
		Select: []query.Path{}, OutputFields: []query.Path{summaryPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary, _ := output.Document.Values["summary"].StringValue(); summary != "Summary: "+privateDefault {
		t.Fatalf("selected synthesized output = %q", summary)
	}
	if len(output.Document.Values) != 1 {
		t.Fatalf("output-only synthesized global leaked dependencies: %#v", output.Document.Values)
	}
}

// A selection shapes the response only. Read hooks and field read rules still
// observe the complete document, so a rule that reads an unselected sibling
// can't be bypassed by leaving that sibling out.
func TestSelectionDoesNotChangeWhatReadCallbacksObserve(t *testing.T) {
	titlePath, _ := query.NewPath("title")
	lockedPath, _ := query.NewPath("locked")
	title := schema.Field{ID: "posts-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar}
	locked := schema.Field{ID: "posts-locked", Name: "locked", Path: lockedPath, Type: schema.FieldTypeCheckbox, Category: schema.FieldCategoryScalar}
	for _, withHook := range []bool{false, true} {
		var hookSawLocked []bool
		collection := Collection{
			Schema: schema.Collection{ID: "collection-posts", Slug: "posts", Fields: []schema.Field{title, locked}},
			Bindings: []FieldBinding{{
				ID: "title", Field: title,
				// This rule fails open: a missing sibling reads as unlocked.
				Access: FieldRules{Read: func(ctx Context) (bool, error) {
					isLocked, _ := ctx.Siblings.Get("locked").BooleanValue()
					return !isLocked, nil
				}},
			}},
		}
		if withHook {
			collection.Hooks.AfterRead = []Hook{func(ctx Context) error {
				_, present := ctx.Data["locked"]
				hookSawLocked = append(hookSawLocked, present)
				return nil
			}}
		}
		engine, err := New(Config{Store: teststore.New(), Collections: []Collection{collection}})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"editable", "locked"} {
			if _, err := engine.Execute(t.Context(), Request{Operation: operation.Create, Collection: "posts", ImportID: id, Data: store.Values{"title": store.String(id), "locked": store.Boolean(id == "locked")}}); err != nil {
				t.Fatal(err)
			}
		}
		for _, read := range []Request{
			{Operation: operation.Read, Collection: "posts", ID: "locked", Select: []query.Path{titlePath}},
			{Operation: operation.Read, Collection: "posts", Page: 1, Limit: 10, Select: []query.Path{titlePath}},
		} {
			hookSawLocked = nil
			result, err := engine.Execute(t.Context(), read)
			if err != nil {
				t.Fatal(err)
			}
			documents := []store.Document{}
			if result.Document != nil {
				documents = append(documents, *result.Document)
			} else {
				documents = result.Page.Documents
			}
			for _, document := range documents {
				_, titled := document.Values["title"]
				if _, returned := document.Values["locked"]; returned {
					t.Fatalf("hook=%v %s: returned unselected root: %#v", withHook, document.ID, document.Values)
				}
				if titled != (document.ID == "editable") {
					t.Fatalf("hook=%v %s: title returned = %v; the read rule saw a partial document", withHook, document.ID, titled)
				}
			}
			for _, saw := range hookSawLocked {
				if !saw {
					t.Fatalf("read hook saw a partial document")
				}
			}
			if withHook && len(hookSawLocked) != len(documents) {
				t.Fatalf("read hook ran %d times for %d documents", len(hookSawLocked), len(documents))
			}
		}
	}
}
