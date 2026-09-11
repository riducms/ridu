package seo_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/seo"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSEOTransformsUnifiedGraphWithoutLosingFieldPolicies(t *testing.T) {
	for _, tabbed := range []bool{false, true} {
		t.Run(map[bool]string{false: "group", true: "tabs"}[tabbed], func(t *testing.T) {
			calls := 0
			title := field.Text("title").Required().Validate(func(operation.Context, operation.Value[string]) ([]operation.Issue, error) {
				calls++
				return nil, nil
			}).Private("application", store.String("never-serialize"))
			config := ridu.Config{Name: "Unified SEO", Plugins: []ridu.Plugin{seo.New(seo.Config{
				Collections: []schema.CollectionSlug{"posts"}, Globals: []schema.CollectionSlug{"site"}, TabbedUI: tabbed,
				Fields: func(defaults field.Fields) (field.Fields, error) {
					return defaults.Edit(func(draft *field.ChildrenDraft) error {
						return draft.EditText("title", func(title field.TextField) field.TextField {
							return title.MaxLength(70).Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(_ operation.Context, value operation.Value[string]) (operation.Change[string], error) {
								text, _ := value.Get()
								return operation.Replace(operation.Present("SEO: " + text)), nil
							}}})
						})
					})
				},
			})}, Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
				title,
			}}}, Globals: []ridu.Global{{Slug: "site", Fields: field.Fields{
				field.Text("name"),
			}}}}
			first, err := ridu.Resolve(config)
			if err != nil {
				t.Fatal(err)
			}
			second, err := ridu.Resolve(config)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(first)
			b, _ := json.Marshal(second)
			if string(a) != string(b) {
				t.Fatal("SEO graph edits accumulate or produce nondeterministic manifests")
			}
			app, err := ridu.New(config, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			document, err := app.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Post"), "meta": store.Object(store.Values{"title": store.String("Metadata")})}, ridu.MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			text, _ := document.Values["meta"].Get("title").StringValue()
			if calls != 1 || text != "SEO: Metadata" {
				t.Fatalf("graph-owned behavior lost across SEO injection: calls=%d title=%q", calls, text)
			}
		})
	}
}
