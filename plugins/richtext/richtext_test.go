package richtext_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/plugintest"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestRichTextContentCanBeLocalized(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Localized rich text", Plugins: []ridu.Plugin{richtext.New()},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{{Slug: "pages", Fields: []field.Definition{
			richtext.Field("content", field.Required(), field.Localized()),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	english := document(store.Object(store.Values{"type": store.String("paragraph"), "children": store.List(store.Object(store.Values{"type": store.String("text"), "text": store.String("Hello")}))}))
	french := document(store.Object(store.Values{"type": store.String("paragraph"), "children": store.List(store.Object(store.Values{"type": store.String("text"), "text": store.String("Bonjour")}))}))
	created, err := application.Local().Create(context.Background(), "pages", store.Values{"content": english}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(context.Background(), "pages", created.ID, store.Values{"content": french}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	all, err := application.Local().Find(context.Background(), "pages", created.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	localized, ok := all.Values["content"].ObjectValue()
	if !ok || localized["en"].Kind() != store.ValueObject || localized["fr"].Kind() != store.ValueObject {
		t.Fatalf("localized rich text = %#v", all.Values["content"])
	}
}

func TestPluginConformance(t *testing.T) {
	plugintest.Run(t, plugintest.Fixture{
		Plugin: richtext.New(), Fields: []field.Definition{richtext.Field("content")},
		ValidData:   store.Values{"content": document()},
		InvalidData: store.Values{"content": store.String("not a document")},
		Compatibility: []plugintest.CompatibilityCase{
			{RiduVersion: ridu.FrameworkVersion, Compatible: true},
			{RiduVersion: "0.2.0", Compatible: true},
		},
	})
}

func TestDefaultConfigIncludesReferencesAndIsDefensive(t *testing.T) {
	first := richtext.DefaultConfig()
	if !hasFeature(first.Features, richtext.FeatureUploads) || !hasFeature(first.Features, richtext.FeatureRelationships) {
		t.Fatalf("default features = %v, want uploads and relationships", first.Features)
	}
	first.Features[0] = richtext.FeatureBlocks
	second := richtext.DefaultConfig()
	if hasFeature(second.Features, richtext.FeatureBlocks) {
		t.Fatalf("mutating DefaultConfig changed later defaults: %v", second.Features)
	}
}

func TestFieldWithConfigInheritsDefaultsOnlyWhenFeaturesAreOmitted(t *testing.T) {
	inherited := richtext.FieldWithConfig("content", richtext.Config{UploadCollections: []string{"media"}})
	inheritedConfig := decodeConfig(t, inherited)
	if !hasFeature(inheritedConfig.Features, richtext.FeatureUploads) ||
		!hasFeature(inheritedConfig.Features, richtext.FeatureCode) {
		t.Fatalf("inherited features = %v", inheritedConfig.Features)
	}
	if len(inheritedConfig.UploadCollections) != 1 || inheritedConfig.UploadCollections[0] != "media" {
		t.Fatalf("upload collections = %v", inheritedConfig.UploadCollections)
	}

	replaced := richtext.FieldWithConfig("content", richtext.Config{Features: []richtext.Feature{}})
	replacedConfig := decodeConfig(t, replaced)
	if len(replacedConfig.Features) != 0 {
		t.Fatalf("explicit replacement features = %v, want none", replacedConfig.Features)
	}
}

func TestVersionedDocumentValidationAndRendering(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name:    "Rich text",
		Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{{
			Slug: "pages", Fields: []field.Definition{
				richtext.FieldWithConfig("content", richtext.Config{Features: []richtext.Feature{richtext.FeatureLinks}}, field.Required()),
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	value := document(store.Object(store.Values{
		"type": store.String("paragraph"),
		"children": store.List(
			store.Object(store.Values{"type": store.String("text"), "text": store.String("Hello ")}),
			store.Object(store.Values{"type": store.String("link"), "url": store.String("https://example.test"), "children": store.List(
				store.Object(store.Values{"type": store.String("text"), "text": store.String("world")}),
			)}),
		),
	}))
	created, err := application.Local().Create(context.Background(), "pages", store.Values{"content": value}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := richtext.RenderHTML(created.Values["content"], nil)
	if err != nil || rendered != `<p>Hello <a href="https://example.test">world</a></p>` {
		t.Fatalf("rendered = %q, %v", rendered, err)
	}
	invalid := store.Object(store.Values{"version": store.Number(2), "root": store.Object(store.Values{"type": store.String("root"), "children": store.List()})})
	if _, err := application.Local().Create(context.Background(), "pages", store.Values{"content": invalid}, nil); err == nil || !strings.Contains(err.Error(), "validation") {
		t.Fatalf("unsupported document version error = %v", err)
	}
	manifestField := application.Manifest().Snapshot().Collections[0].Fields[0]
	if manifestField.Plugin == nil || manifestField.Plugin.Key != richtext.Key {
		t.Fatalf("rich-text manifest field = %#v", manifestField)
	}
	if len(manifestField.Plugin.ReferenceKeys) != 1 || manifestField.Plugin.ReferenceKeys[0] != "relationTo" {
		t.Fatalf("rich-text collection reference keys = %#v", manifestField.Plugin.ReferenceKeys)
	}
	manifestPlugin := application.Manifest().Snapshot().Plugins[0]
	if manifestPlugin.Admin == nil || manifestPlugin.Admin.Package != "@riducms/plugin-richtext" || manifestPlugin.Admin.Export != "richTextAdminPlugin" || manifestPlugin.Admin.PairingVersion != richtext.AdminPluginPairingVersion {
		t.Fatalf("rich-text admin plugin = %#v", manifestPlugin.Admin)
	}
}

func document(children ...store.Value) store.Value {
	return store.Object(store.Values{
		"version": store.Number(richtext.DocumentVersion),
		"root":    store.Object(store.Values{"type": store.String("root"), "children": store.List(children...)}),
	})
}

func TestLexicalHeadingAndQuoteRendering(t *testing.T) {
	value := document(
		store.Object(store.Values{"type": store.String("heading"), "tag": store.String("h2"), "children": store.List(store.Object(store.Values{"type": store.String("text"), "text": store.String("Title")}))}),
		store.Object(store.Values{"type": store.String("quote"), "children": store.List(store.Object(store.Values{"type": store.String("text"), "text": store.String("Safe")}))}),
	)
	rendered, err := richtext.RenderHTML(value, nil)
	if err != nil || rendered != "<h2>Title</h2><blockquote>Safe</blockquote>" {
		t.Fatalf("rendered = %q, %v", rendered, err)
	}
}

func TestRendererRejectsUnsafeLinks(t *testing.T) {
	value := document(store.Object(store.Values{"type": store.String("paragraph"), "children": store.List(store.Object(store.Values{
		"type": store.String("link"), "url": store.String("javascript:alert(1)"), "children": store.List(store.Object(store.Values{"type": store.String("text"), "text": store.String("bad")})),
	}))}))
	if _, err := richtext.RenderHTML(value, nil); err == nil {
		t.Fatal("unsafe link rendered")
	}
}

func TestDefaultFieldValidatesAndRendersPortableAuthoringNodes(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name:    "Portable rich text",
		Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{{
			Slug: "pages", Fields: []field.Definition{richtext.Field("content")},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	value := document(
		store.Object(store.Values{"type": store.String("paragraph"), "children": store.List(
			store.Object(store.Values{"type": store.String("text"), "text": store.String("Strong & safe"), "format": store.Number(3)}),
			store.Object(store.Values{"type": store.String("linebreak")}),
		)}),
		store.Object(store.Values{"type": store.String("list"), "listType": store.String("bullet"), "children": store.List(
			store.Object(store.Values{"type": store.String("listitem"), "children": store.List(
				store.Object(store.Values{"type": store.String("text"), "text": store.String("Trail tested")}),
			)}),
		)}),
		store.Object(store.Values{"type": store.String("code"), "children": store.List(
			store.Object(store.Values{"type": store.String("text"), "text": store.String("if a < b")}),
		)}),
		store.Object(store.Values{"type": store.String("horizontalrule")}),
	)
	created, err := application.Local().Create(context.Background(), "pages", store.Values{"content": value}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := richtext.RenderHTML(created.Values["content"], nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "<p><strong><em>Strong &amp; safe</em></strong><br></p><ul><li>Trail tested</li></ul><pre><code>if a &lt; b</code></pre><hr>"
	if rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}
}

func TestDefaultFieldEnablesUploads(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{
		Name:             "Default upload rich text",
		Plugins:          []ridu.Plugin{richtext.New()},
		Storage:          backend,
		StorageNamespace: "richtext-test",
		Collections: []ridu.Collection{
			{Slug: "media", Upload: true},
			{Slug: "pages", Fields: []field.Definition{richtext.Field("content")}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	value := document(store.Object(store.Values{
		"type": store.String("upload"), "relationTo": store.String("media"), "id": store.String("asset-1"),
	}))
	if _, err := application.Local().Create(context.Background(), "pages", store.Values{"content": value}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRelationshipNodesRespectConfiguredCollections(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name:    "Relationship rich text",
		Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{{
			Slug: "pages", Fields: []field.Definition{richtext.FieldWithConfig("content", richtext.Config{
				Features:                []richtext.Feature{richtext.FeatureRelationships},
				RelationshipCollections: []string{"posts"},
			})},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	reference := func(collection string) store.Value {
		return document(store.Object(store.Values{
			"type": store.String("relationship"), "relationTo": store.String(collection), "id": store.String("document-1"),
		}))
	}
	if _, err := application.Local().Create(context.Background(), "pages", store.Values{"content": reference("posts")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(context.Background(), "pages", store.Values{"content": reference("users")}, nil); err == nil || !strings.Contains(err.Error(), "validation") {
		t.Fatalf("disabled relationship collection error = %v", err)
	}
}

func TestChecklistAlignmentAndScriptRendering(t *testing.T) {
	value := document(store.Object(store.Values{
		"type": store.String("list"), "listType": store.String("check"), "format": store.String("center"), "indent": store.Number(2),
		"children": store.List(store.Object(store.Values{
			"type": store.String("listitem"), "checked": store.Boolean(true), "children": store.List(
				store.Object(store.Values{"type": store.String("text"), "text": store.String("H2O"), "format": store.Number(32)}),
				store.Object(store.Values{"type": store.String("text"), "text": store.String("2"), "format": store.Number(64)}),
			),
		})),
	}))
	rendered, err := richtext.RenderHTML(value, nil)
	want := `<ul data-list-type="check" data-align="center" data-indent="2"><li data-checked="true"><sub>H2O</sub><sup>2</sup></li></ul>`
	if err != nil || rendered != want {
		t.Fatalf("rendered = %q, %v; want %q", rendered, err, want)
	}
}

func TestUploadNodesRespectConfiguredCollections(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name:    "Upload rich text",
		Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{{
			Slug: "pages", Fields: []field.Definition{richtext.FieldWithConfig("content", richtext.Config{
				Features:          []richtext.Feature{richtext.FeatureUploads},
				UploadCollections: []string{"media"},
			})},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	upload := func(collection string, caption ...store.Value) store.Value {
		node := store.Values{
			"type": store.String("upload"), "relationTo": store.String(collection), "id": store.String("asset-1"),
		}
		if len(caption) > 0 {
			node["caption"] = caption[0]
		}
		return document(store.Object(node))
	}
	if _, err := application.Local().Create(context.Background(), "pages", store.Values{"content": upload("media", store.String("A per-placement caption"))}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(context.Background(), "pages", store.Values{"content": upload("private-media")}, nil); err == nil || !strings.Contains(err.Error(), "validation") {
		t.Fatalf("disabled upload collection error = %v", err)
	}
	if _, err := application.Local().Create(context.Background(), "pages", store.Values{"content": upload("media", store.Number(42))}, nil); err == nil || !strings.Contains(err.Error(), "validation") {
		t.Fatalf("invalid upload caption error = %v", err)
	}
}

func decodeConfig(t *testing.T, definition field.Definition) richtext.Config {
	t.Helper()
	config := richtext.Config{}
	if err := json.Unmarshal(definition.PluginConfig(), &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func hasFeature(features []richtext.Feature, candidate richtext.Feature) bool {
	for _, feature := range features {
		if feature == candidate {
			return true
		}
	}
	return false
}
