package core_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

func registryCard(key, visibility string) store.Value {
	return store.Object(store.Values{"_key": store.String(key), "blockType": store.String("card"), "visibility": store.String(visibility), "secret": store.String("classified"), "details": store.Object(store.Values{"caption": store.String("nested")}), "children": store.List(store.Object(store.Values{"_key": store.String(key + "-child"), "blockType": store.String("note"), "text": store.String("leaf")}))})
}
func TestBlockRegistryCompactManifestAndPlacement(t *testing.T) {
	app, err := ridu.New(blockreferences.Config(true), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := app.Manifest().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(encoded, []byte(`"typeName": "Card"`)) != 1 {
		t.Fatalf("shared definition repeated: %s", encoded)
	}
	// An explicit empty inline array cannot mask references after JSON decoding.
	originalEncoded := bytes.Clone(encoded)
	encoded = bytes.ReplaceAll(encoded, []byte(`"blockReferences": [`), []byte(`"types": [], "blockReferences": [`))
	parsed, err := schema.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := parsed.Snapshot()
	if len(snapshot.Blocks) != 1 {
		t.Fatalf("registry: %+v", snapshot.Blocks)
	}
	layout := snapshot.Collections[0].Fields[1]
	if len(layout.Blocks.Types) != 0 || !reflect.DeepEqual(layout.Blocks.BlockReferences, []string{"card"}) {
		t.Fatal("reference expanded in wire snapshot")
	}
	child := layout.Blocks.ResolvedTypes()[0].ResolvedFields()[1]
	if child.Path.String() != "layout.card.secret" || child.ID != "pages-layout-card-secret" {
		t.Fatalf("placement: %+v", child)
	}
	caption := layout.Blocks.ResolvedTypes()[0].ResolvedFields()[2].Nested.ResolvedFields()[0]
	if caption.Path.String() != "layout.card.details.caption" {
		t.Fatalf("nested placement: %s", caption.Path)
	}
	// Mutation of a detached view must not change the manifest or another placement.
	layout.Blocks.ResolvedTypes()[0].ResolvedFields()[1].Admin.Label = "Changed"
	other := snapshot.Collections[0].Fields[2].Blocks.ResolvedTypes()[0].ResolvedFields()[1]
	if other.Admin.Label == "Changed" {
		t.Fatal("placement metadata aliases another placement")
	}
	again, _ := app.Manifest().Bytes()
	if !bytes.Equal(originalEncoded, again) {
		t.Fatal("snapshot mutation changed manifest")
	}
}
func TestBlockRegistryInlineAccessEquivalence(t *testing.T) {
	var expected [][]byte
	for _, references := range []bool{false, true} {
		app, err := ridu.New(blockreferences.Config(references), teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		var results [][]byte
		for _, collection := range []string{"pages", "articles"} {
			for _, tenant := range []string{"open", "closed"} {
				input := store.Values{"tenant": store.String(tenant), "layout": store.List(registryCard("a", "visible"), registryCard("b", "hidden"))}
				if collection == "pages" {
					input["body"] = richtextblocks.Document(richtextblocks.Block("card", "embedded", store.Values{"visibility": store.String("visible"), "secret": store.String("embedded-secret")}))
				}
				doc, err := app.Local().Create(t.Context(), collection, input, ridu.MutationOptions{})
				if err != nil {
					t.Fatal(err)
				}
				got, err := app.Local().Find(t.Context(), collection, doc.ID, ridu.FindOptions{})
				if err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(got.Values)
				results = append(results, data)
				capabilities, err := app.Local().Capabilities(t.Context(), collection, doc.ID, ridu.CapabilityOptions{})
				if err != nil {
					t.Fatal(err)
				}
				data, _ = json.Marshal(capabilities)
				results = append(results, data)
				patch := store.Values{"layout": store.List(store.Object(store.Values{"_key": store.String("b"), "blockType": store.String("card"), "secret": store.String("changed")}), store.Object(store.Values{"_key": store.String("a"), "blockType": store.String("card")}))}
				_, err = app.Local().Update(t.Context(), collection, doc.ID, patch, ridu.MutationOptions{})
				if err == nil {
					t.Fatal("document-dependent write denial was skipped")
				}
			}
		}
		if !references {
			expected = results
		} else if !reflect.DeepEqual(expected, results) {
			t.Fatalf("inline/reference differences\ninline: %s\nreference: %s", expected, results)
		}
	}
}

func TestBlockRegistryPlacementLocalizationAndLayouts(t *testing.T) {
	var expected []schema.Field
	for _, refs := range []bool{false, true} {
		block := field.Block{Slug: "card", TypeName: "Card", Fields: field.Fields{
			field.Row(field.Fields{field.Text("title").Localized(), field.Text("summary")}),
			field.Tabs(field.Fields{field.UnnamedTab("More", field.Fields{field.Text("caption")})}),
			field.Collapsible("details", field.Fields{field.Text("description")}),
		}}
		config := ridu.Config{Name: "Placement", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}}
		for _, slug := range []string{"pages", "articles"} {
			blocks := field.Blocks("layout", block)
			if refs {
				blocks = field.Blocks("layout").References("card")
			}
			config.Collections = append(config.Collections, ridu.Collection{Slug: schema.CollectionSlug(slug), Fields: field.Fields{blocks.Localized(), field.Group("group", field.Fields{blocks}).Localized()}})
		}
		if refs {
			config.Blocks = []field.Block{block}
		}
		manifest, err := ridu.Resolve(config)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := manifest.Bytes()
		parsed, err := schema.Parse(encoded)
		if err != nil {
			t.Fatal(err)
		}
		var fields []schema.Field
		var walk func([]schema.Field)
		walk = func(fs []schema.Field) {
			for _, f := range fs {
				if f.Blocks != nil {
					for _, b := range f.Blocks.ResolvedTypes() {
						fields = append(fields, b.ResolvedFields()...)
					}
				}
				if f.Nested != nil {
					walk(f.Nested.ResolvedFields())
				}
			}
		}
		for _, c := range parsed.Snapshot().Collections {
			walk(c.Fields)
		}
		if !refs {
			expected = fields
		} else if !reflect.DeepEqual(expected, fields) {
			a, _ := json.MarshalIndent(expected, "", " ")
			b, _ := json.MarshalIndent(fields, "", " ")
			t.Fatalf("placement difference\ninline %s\nreference %s", a, b)
		}
	}
}
func TestBlockRegistryRejectsReusedPlacementIDCollision(t *testing.T) {
	config := blockreferences.Config(true)
	config.Collections[1].Fields = append(config.Collections[1].Fields, field.Text("layout_card_secret"))
	if _, err := ridu.Resolve(config); err == nil || !strings.Contains(err.Error(), "duplicate_field_id") {
		t.Fatalf("collision not rejected: %v", err)
	}
}
func TestBlockRegistryForwardNestedAndInvalidReferences(t *testing.T) {
	leaf := field.Block{Slug: "leaf", Fields: field.Fields{field.Text("text")}}
	card := field.Block{Slug: "card", Fields: field.Fields{field.Blocks("children").References("leaf"), richtext.Field("body", richtext.Config{BlockReferences: []string{"leaf"}})}}
	config := ridu.Config{Name: "Nested", Blocks: []field.Block{card, leaf}, Plugins: []ridu.Plugin{richtext.New()}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References("card", "leaf")}}}}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	bytes, _ := manifest.Bytes()
	parsed, err := schema.Parse(bytes)
	if err != nil {
		t.Fatal(err)
	}
	nested := parsed.Snapshot().Collections[0].Fields[0].Blocks.ResolvedTypes()[0].ResolvedFields()
	if registry := parsed.Snapshot().Blocks; registry[0].TypeName != "Card" || registry[1].TypeName != "Leaf" {
		t.Fatalf("derived registry names: %+v", registry)
	}
	if got := nested[0].Blocks.ResolvedTypes()[0].ResolvedFields()[0].Path.String(); got != "layout.card.children.leaf.text" {
		t.Fatal(got)
	}
	if got := nested[1].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].ResolvedFields()[0].Path.String(); got != "layout.card.body.blocks.block.leaf.text" {
		t.Fatal(got)
	}
	config.Blocks = []field.Block{leaf, card}
	other, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	otherBytes, _ := other.Bytes()
	if !reflect.DeepEqual(bytes, otherBytes) {
		t.Fatal("registration order changes manifest")
	}
	for _, test := range []struct {
		name, code string
		blocks     []field.Block
		layout     field.BlocksField
	}{
		{"invalid slug", "invalid_block_slug", []field.Block{{Slug: "Invalid"}}, field.Blocks("layout").References("Invalid")},
		{"invalid type name", "invalid_block_type_name", []field.Block{{Slug: "invalid", TypeName: "lowercase"}}, field.Blocks("layout").References("invalid")},
		{"derived name conflict", "block_type_name_conflict", []field.Block{leaf, {Slug: "other", TypeName: "Leaf"}}, field.Blocks("layout").References("leaf")},
		{"missing", "unknown_block_reference", []field.Block{leaf}, field.Blocks("layout").References("missing")},
		{"duplicate registration", "duplicate_block_slug", []field.Block{leaf, leaf}, field.Blocks("layout").References("leaf")},
		{"duplicate ref", "duplicate_block_reference", []field.Block{leaf}, field.Blocks("layout").References("leaf", "leaf")},
		{"mixed", "mixed_block_definitions", []field.Block{leaf}, field.Blocks("layout", leaf).References("leaf")},
		{"cycle", "block reference cycle: card -> leaf -> card", []field.Block{card, {Slug: "leaf", Fields: field.Fields{field.Blocks("loop").References("card")}}}, field.Blocks("layout").References("card")},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := config
			cfg.Blocks = test.blocks
			cfg.Collections = []ridu.Collection{{Slug: "pages", Fields: field.Fields{test.layout}}}
			_, err := ridu.Resolve(cfg)
			if err == nil || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("wanted %s: %v", test.code, err)
			}
		})
	}
}

// Compare callback inputs as well as outcomes: a matching happy path cannot prove
// that document, prior-row or locale context was preserved across schema sharing.
func TestBlockRegistryAccessContextMatrix(t *testing.T) {
	var expectedResults, expectedCalls []string
	for _, refs := range []bool{false, true} {
		calls := []string{}
		rule := func(c operation.Context) (bool, error) {
			tenant, _ := c.Root.String("tenant")
			visibility, _ := c.Siblings.String("visibility")
			prior, _ := c.Prior.String("visibility")
			calls = append(calls, fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s", c.Operation, c.CollectionID, c.GlobalID, c.Actor.ID, c.Locale, c.ID, tenant, visibility, prior))
			return c.Actor.ID != "blocked" && tenant == "open" && visibility == "visible" && prior != "locked" && c.Locale != "fr", nil
		}
		card := field.Block{Slug: "context-card", TypeName: "ContextCard", Fields: field.Fields{field.Text("visibility"), field.Text("secret").Localized().Access(field.Access{Read: rule, Update: rule})}}
		makeFields := func() field.Fields {
			b := field.Blocks("layout", card)
			if refs {
				b = field.Blocks("layout").References("context-card")
			}
			return field.Fields{field.Text("tenant"), b.Access(field.Access{Read: func(c operation.Context) (bool, error) { return c.Actor.ID != "parent-denied", nil }, Update: func(c operation.Context) (bool, error) { return c.Actor.ID != "parent-denied", nil }})}
		}
		cfg := ridu.Config{Name: "Context matrix", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{{Slug: "pages", Fields: makeFields()}, {Slug: "articles", Fields: makeFields()}}, Globals: []ridu.Global{{Slug: "settings", Fields: makeFields()}}}
		if refs {
			cfg.Blocks = []field.Block{card}
		}
		app, err := ridu.New(cfg, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		results := []string{}
		record := func(value any, err error) {
			data, _ := json.Marshal(value)
			message := ""
			if err != nil {
				message = err.Error()
			}
			results = append(results, string(data)+message)
		}
		rows := func() store.Value {
			return store.List(store.Object(store.Values{"_key": store.String("a"), "blockType": store.String("context-card"), "visibility": store.String("visible"), "secret": store.String("A")}), store.Object(store.Values{"_key": store.String("b"), "blockType": store.String("context-card"), "visibility": store.String("locked"), "secret": store.String("B")}))
		}
		for _, resource := range []string{"pages", "articles"} {
			for _, tenant := range []string{"open", "closed"} {
				doc, err := app.Local().Create(t.Context(), resource, store.Values{"tenant": store.String(tenant), "layout": rows()}, ridu.MutationOptions{})
				if err != nil {
					t.Fatal(err)
				}
				for _, actorID := range []string{"allowed", "blocked", "parent-denied"} {
					actor := &store.Document{ID: actorID}
					for _, locale := range []schema.LocaleCode{"en", "fr"} {
						got, err := app.Local().Find(t.Context(), resource, doc.ID, ridu.FindOptions{Actor: actor, Locale: locale})
						record(got.Values, err)
						cap, err := app.Local().Capabilities(t.Context(), resource, doc.ID, ridu.CapabilityOptions{Actor: actor, Locale: locale})
						record(cap, err)
						// Existing capabilities also support unsaved candidates and absent documents.
						cap, err = app.Local().Capabilities(t.Context(), resource, "", ridu.CapabilityOptions{Actor: actor, Locale: locale})
						record(cap, err)
						patch := store.Values{"layout": store.List(store.Object(store.Values{"_key": store.String("b"), "blockType": store.String("context-card"), "visibility": store.String("visible"), "secret": store.String("changed")}), store.Object(store.Values{"_key": store.String("a"), "blockType": store.String("context-card")}))}
						cap, err = app.Local().Capabilities(t.Context(), resource, doc.ID, ridu.CapabilityOptions{Actor: actor, Locale: locale, Data: patch})
						record(cap, err)
						_, err = app.Local().Update(t.Context(), resource, doc.ID, patch, ridu.MutationOptions{Actor: actor, Locale: locale})
						if err == nil {
							t.Fatal("protected retained row accepted")
						}
						record(nil, err)
					}
				}
			}
		}
		_, err = app.Local().UpdateGlobal(t.Context(), "settings", store.Values{"tenant": store.String("open"), "layout": store.List(store.Object(store.Values{"_key": store.String("g"), "blockType": store.String("context-card"), "visibility": store.String("visible"), "secret": store.String("global")}))}, ridu.MutationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"allowed", "blocked"} {
			doc, err := app.Local().Global(t.Context(), "settings", ridu.FindOptions{Actor: &store.Document{ID: id}})
			record(doc.Values, err)
		}
		slices.Sort(calls)
		if !refs {
			expectedResults, expectedCalls = results, calls
		} else {
			if !reflect.DeepEqual(expectedResults, results) {
				t.Fatal("runtime or capabilities differ")
			}
			if !reflect.DeepEqual(expectedCalls, calls) {
				t.Fatal("callback context or evaluation counts differ")
			}
		}
	}
}

func TestBlockRegistrySymbolicReferencesValidateEachPlacement(t *testing.T) {
	block := field.Block{Slug: "card", Fields: field.Fields{field.Text("caption").Admin(field.Admin{VisibleWhen: field.Equal(field.Root("tenant"), "open")})}}
	config := ridu.Config{Name: "Selectors", Blocks: []field.Block{block}, Collections: []ridu.Collection{
		{Slug: "pages", Fields: field.Fields{field.Text("tenant"), field.Blocks("layout").References("card")}},
		{Slug: "articles", Fields: field.Fields{field.Blocks("layout").References("card")}},
	}}
	if _, err := ridu.Resolve(config); err == nil || !strings.Contains(err.Error(), "collections[1]") {
		t.Fatalf("missing target at second placement: %v", err)
	}
	config.Collections[1].Fields = append(config.Collections[1].Fields, field.Text("tenant"))
	if _, err := ridu.Resolve(config); err != nil {
		t.Fatal(err)
	}
}

func TestBlockRegistryCreateAccess(t *testing.T) {
	var expected []string
	for _, refs := range []bool{false, true} {
		card := field.Block{Slug: "card", Fields: field.Fields{field.Text("visibility"), field.Text("secret").Access(field.Access{Create: func(c operation.Context) (bool, error) {
			tenant, _ := c.Root.String("tenant")
			visibility, _ := c.Siblings.String("visibility")
			return tenant == "open" && visibility == "visible" && c.Actor.ID != "blocked", nil
		}})}}
		b := field.Blocks("layout", card)
		if refs {
			b = field.Blocks("layout").References("card")
		}
		config := ridu.Config{Name: "Create access", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Text("tenant"), b}}}}
		if refs {
			config.Blocks = []field.Block{card}
		}
		app, err := ridu.New(config, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		var results []string
		for _, actor := range []*store.Document{nil, {ID: "blocked"}} {
			for _, tenant := range []string{"open", "closed"} {
				for _, visibility := range []string{"visible", "hidden"} {
					values := store.Values{"tenant": store.String(tenant), "layout": store.List(store.Object(store.Values{"_key": store.String("a"), "blockType": store.String("card"), "visibility": store.String(visibility), "secret": store.String("value")}))}
					cap, err := app.Local().Capabilities(t.Context(), "pages", "", ridu.CapabilityOptions{Actor: actor, Data: values})
					if err != nil {
						t.Fatal(err)
					}
					data, _ := json.Marshal(cap)
					results = append(results, string(data))
					doc, err := app.Local().Create(t.Context(), "pages", values, ridu.MutationOptions{Actor: actor})
					allowed := actor == nil && tenant == "open" && visibility == "visible"
					if (err == nil) != allowed {
						t.Fatalf("create decision: %v", err)
					}
					data, _ = json.Marshal(doc.Values)
					results = append(results, string(data))
				}
			}
		}
		if !refs {
			expected = results
		} else if !reflect.DeepEqual(expected, results) {
			t.Fatal("create capabilities or enforcement differ")
		}
	}
}
