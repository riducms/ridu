package operation

import (
	"testing"

	"github.com/riducms/ridu/internal/teststore"
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
			Computed: map[string]Computed{"summary": func(_ Context, document store.Document) (store.Value, error) {
				dependency, _ := document.Values["privateNote"].StringValue()
				return store.String("Summary: " + dependency), nil
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	stored, err := engine.Execute(t.Context(), Request{
		Operation: Read, Collection: "global:settings", ID: "settings",
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
		Operation: Read, Collection: "global:settings", ID: "settings",
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
