package richtext_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestUnifiedRichTextConfigurationChecksEveryGraphHost(t *testing.T) {
	invalid := richtext.Field("body", richtext.Config{Features: []richtext.Feature{richtext.FeatureBlocks}})
	for _, test := range []struct {
		name   string
		graph  field.Fields
		path   string
		blocks []field.Block
	}{
		{"root", field.Fields{
			invalid,
		}, "pages.body", nil},
		{"group", field.Fields{
			field.Group("meta", field.Fields{
				invalid,
			}),
		}, "pages.meta.body", nil},
		{"array", field.Fields{
			field.Array("sections", field.Fields{
				invalid,
			}),
		}, "pages.sections.body", nil},
		{"blocks", field.Fields{
			field.Blocks("content", field.Block{Slug: "quote", Fields: field.Fields{
				invalid,
			}}),
		}, "pages.content.quote.body", nil},
		{"registered", field.Fields{
			field.Blocks("content").References("quote"),
		}, "pages.content.quote.body", []field.Block{{Slug: "quote", Fields: field.Fields{invalid}}}},
		{"tab", field.Fields{
			field.NamedTab("meta", "Meta", field.Fields{
				invalid,
			}),
		}, "pages.meta.body", nil},
		{"embedded", field.Fields{
			richtext.Field("outer", richtext.Config{Blocks: []field.Block{field.Block{Slug: "quote", Fields: field.Fields{
				invalid,
			}}}}),
		}, "pages.outer.blocks.block.quote.body", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(ridu.Config{Name: "Graph rich text", Plugins: []ridu.Plugin{richtext.New()}, Blocks: test.blocks, Collections: []ridu.Collection{{Slug: "pages", Fields: test.graph}}})
			if err == nil || !strings.Contains(err.Error(), test.path) || !strings.Contains(err.Error(), "requires explicit") {
				t.Fatalf("unified host bypassed feature/allowlist validation: %v", err)
			}
		})
	}
	_, err := ridu.Resolve(ridu.Config{Name: "Global rich text", Plugins: []ridu.Plugin{richtext.New()}, Globals: []ridu.Global{{Slug: "site", Fields: field.Fields{
		invalid,
	}}}})
	if err == nil || !strings.Contains(err.Error(), "site.body") {
		t.Fatalf("global graph bypassed rich text validation: %v", err)
	}
}

// Block definitions are checked once each and the schema has no placement
// budget, so references may expand past both the former per-walk validation
// budget (10000) and the former schema-wide placement budget (100000).
func TestRegisteredBlockReferencesResolveBeyondTheValidationWalkBudget(t *testing.T) {
	fields := field.Fields{richtext.Field("body")}
	for index := range 40 {
		fields = append(fields, field.Text(fmt.Sprintf("text%d", index)))
	}
	var sections field.Fields
	for index := range 50 {
		sections = append(sections, field.Blocks(fmt.Sprintf("section%d", index)).References("wide"))
	}
	var layouts field.Fields
	for index := range 60 {
		layouts = append(layouts, field.Blocks(fmt.Sprintf("layout%d", index)).References("outer"))
	}
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Wide references", Plugins: []ridu.Plugin{richtext.New()},
		Blocks:      []field.Block{{Slug: "wide", Fields: fields}, {Slug: "outer", Fields: sections}},
		Collections: []ridu.Collection{{Slug: "pages", Fields: layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var count func([]schema.Field) int
	count = func(fields []schema.Field) int {
		total := len(fields)
		for _, field := range fields {
			if field.Blocks != nil {
				for _, block := range field.Blocks.ResolvedTypes() {
					total += count(block.ResolvedFields())
				}
			}
		}
		return total
	}
	if placements := count(manifest.Snapshot().Collections[0].Fields); placements <= 100000 {
		t.Fatalf("fixture has %d field placements, want more than the former 100000 budget", placements)
	}
}

type insertRichText struct{ body field.Node }

func (insertRichText) Key() string { return "late-richtext" }
func (p insertRichText) TransformFields(_ core.FieldGraphContext, graph field.Fields) (field.Fields, error) {
	return graph.Edit(func(draft *field.ChildrenDraft) error { return draft.Insert(len(graph), p.body) })
}

func TestRichTextValidatesFinalGraphAfterLaterPluginEdits(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name:    "Final graph validation",
		Plugins: []ridu.Plugin{richtext.New(), insertRichText{body: richtext.Field("body", richtext.Config{Features: []richtext.Feature{richtext.FeatureBlocks}})}},
		Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
			field.Text("title"),
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "plugin_field_graph_validation_failed") || !strings.Contains(err.Error(), "pages.body") || !strings.Contains(err.Error(), "requires explicit") {
		t.Fatalf("later graph edit bypassed final rich text configuration validation: %v", err)
	}
}

func TestRichTextFactoryOwnsBehaviorThroughRenameNestingAndReuse(t *testing.T) {
	var occurrences []operation.OccurrenceID
	title := field.Text("title").Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{
		func(ctx operation.Context, value operation.Value[string]) (operation.Change[string], error) {
			occurrences = append(occurrences, ctx.OccurrenceID)
			text, _ := value.Get()
			return operation.Set(strings.ToUpper(text)), nil
		},
	}})
	config := richtext.Config{Blocks: []field.Block{{Slug: "callout", Fields: field.Fields{title}}}}
	var body field.PluginField = richtext.Field("body", config)
	config.Blocks[0].Fields[0] = field.Text("replacement")
	config.Blocks[0].Slug = "replacement"
	application, err := ridu.New(ridu.Config{
		Name: "Reusable rich text", Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
			body.Rename("first"), field.Group("section", field.Fields{body.Rename("second")}),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	block := func(text string) store.Value {
		return document(store.Object(store.Values{
			"type": store.String("block"), "version": store.Number(1),
			"fields": store.Object(store.Values{"blockType": store.String("callout"), "title": store.String(text)}),
		}))
	}
	created, err := application.Local().Create(t.Context(), "pages", store.Values{
		"first": block("first"), "section": store.Object(store.Values{"second": block("second")}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(occurrences) != 2 || occurrences[0] == "" || occurrences[0] == occurrences[1] {
		t.Fatalf("reused factory lost concrete occurrence behavior: %v", occurrences)
	}
	for text, value := range map[string]store.Value{"FIRST": created.Values["first"], "SECOND": created.Values["section"].Get("second")} {
		node, _ := value.Get("root").Get("children").ListItem(0)
		if actual, _ := node.Get("fields").Get("title").StringValue(); actual != text {
			t.Fatalf("reused field hook output = %q, want %q", actual, text)
		}
	}
}
