package schema

import (
	"bytes"
	"testing"

	"github.com/riducms/ridu/query"
)

func TestStorageSchemaClearsPresentationAndPreservesStorage(t *testing.T) {
	leaf := Field{
		Name: "title", Path: query.Field("title"), Type: FieldTypeText, Required: true,
		Admin: FieldAdmin{Hidden: true}, Text: &TextField{},
	}
	inline := BlockType{Slug: "inline", Admin: &BlockAdmin{RowLabel: "title"}, Fields: []Field{leaf}}
	tone := Field{Name: "tone", Path: query.Field("tone"), Type: FieldTypeSelect, Select: &SelectField{Options: []SelectOption{
		{Value: "light", Label: "Light"}, {Value: "dark", Label: "Dark"},
	}}}
	endpoint := Endpoint{Method: "GET", Path: "/popular", Summary: "Popular posts"}
	manifest := NewManifest(Snapshot{
		Version: CurrentVersion,
		Application: Application{
			Name: "Projection", NameTranslations: map[string]string{"fr": "Projection FR"},
			AdminLoaders: []AdminLoader{{Key: "stats", Input: AdminDataType{Kind: "object"}, Output: AdminDataType{Kind: "object"}}},
			AdminLocalization: &AdminLocalizationSettings{
				Languages: []AdminLanguage{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}, DefaultLanguage: "en",
				TimeZones: []AdminTimeZone{{ID: "UTC", Label: "UTC"}}, DefaultTimeZone: "UTC",
			},
			Localization: &LocalizationSettings{DefaultLocale: "en", Fallback: true, Locales: []Locale{{Code: "en", Label: "English"}, {Code: "ar", Label: "Arabic", RTL: true}}},
			Endpoints:    []Endpoint{endpoint},
		},
		Blocks: []BlockType{{
			Slug: "card", Labels: BlockLabels{Singular: "Card", Plural: "Cards"}, TypeName: "Card", Admin: &BlockAdmin{RowLabel: "title"},
			Fields: []Field{{Name: "details", Path: query.Field("details"), Type: FieldTypeGroup, Nested: &NestedField{Fields: []Field{leaf}}}},
		}},
		Collections: []Collection{{
			ID: "posts", Slug: "posts", Labels: CollectionLabels{Singular: "Post", Plural: "Posts"}, Admin: CollectionAdmin{Hidden: true},
			Endpoints: []Endpoint{endpoint},
			Fields:    []Field{{Name: "content", Path: query.Field("content"), Type: FieldTypeBlocks, Blocks: &BlocksField{BlockReferences: []string{"card"}}}, tone},
		}},
		Globals: []Global{{
			ID: "settings", Slug: "settings", Admin: CollectionAdmin{Hidden: true},
			Fields: []Field{{Name: "widgets", Path: query.Field("widgets"), Type: FieldTypePlugin, Plugin: &PluginField{EmbeddedTrees: []EmbeddedTree{{
				Cases: []EmbeddedTreeCase{{Types: []BlockType{inline}}, {BlockReferences: []string{"card"}}},
			}}}}},
		}},
		Plugins: []Plugin{{Key: "widgets", GoPackage: "example.com/widgets", Admin: &PluginAdmin{Package: "@example/widgets"}, FieldTypes: []PluginFieldType{{
			Key: "widget", TypeScriptPackage: "@example/widgets/document", TypeScriptOutput: "Widget", TypeScriptInput: "WidgetInput",
			JSONSchema: []byte(`{"type":"object"}`),
		}}}},
	})
	before, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	projected := manifest.StorageSchema()
	if !manifest.SameStorage(projected) {
		t.Fatal("presentation changed storage identity")
	}
	if manifest.Equal(projected) {
		t.Fatal("presentation was retained")
	}
	snapshot := projected.Snapshot()
	placed := snapshot.Collections[0].Fields[0].Blocks.ResolvedTypes()[0]
	if placed.Admin != nil || placed.ResolvedFields()[0].Nested.ResolvedFields()[0].Admin.Hidden {
		t.Fatal("registered block placements retained admin metadata")
	}
	cases := snapshot.Globals[0].Fields[0].Plugin.EmbeddedTrees[0].Cases
	if cases[0].ResolvedTypes()[0].Admin != nil || cases[0].ResolvedTypes()[0].Fields[0].Admin.Hidden ||
		cases[1].ResolvedTypes()[0].ResolvedFields()[0].Nested.ResolvedFields()[0].Admin.Hidden {
		t.Fatal("embedded block cases retained admin metadata")
	}
	// Generated client types may move between packages without a migration.
	moved := manifest.Snapshot()
	moved.Plugins[0].FieldTypes[0].TypeScriptPackage = "@example/sdk/widgets"
	if !manifest.SameStorage(NewManifest(moved)) {
		t.Fatal("moving generated plugin types changed storage identity")
	}
	moved.Plugins[0].FieldTypes[0].JSONSchema = []byte(`{"type":"string"}`)
	if manifest.SameStorage(NewManifest(moved)) {
		t.Fatal("a plugin value schema change was ignored")
	}
	snapshot.Blocks[0].Fields[0].Nested.Fields[0].Required = false
	if manifest.SameStorage(NewManifest(snapshot)) {
		t.Fatal("a storage change inside a shared block was ignored")
	}

	// None of these shapes stored data or how an adapter plans a migration.
	for name, change := range map[string]func(*Snapshot){
		"application name": func(s *Snapshot) { s.Application.Name, s.Application.NameTranslations = "Renamed", nil },
		"admin loader":     func(s *Snapshot) { s.Application.AdminLoaders = nil },
		"admin language": func(s *Snapshot) {
			s.Application.AdminLocalization.Languages = s.Application.AdminLocalization.Languages[:1]
		},
		"admin timezone":       func(s *Snapshot) { s.Application.AdminLocalization.DefaultTimeZone = "Europe/London" },
		"application endpoint": func(s *Snapshot) { s.Application.Endpoints[0].Path = "/trending" },
		"collection endpoint":  func(s *Snapshot) { s.Collections[0].Endpoints = nil },
		"collection labels":    func(s *Snapshot) { s.Collections[0].Labels.Plural = "Articles" },
		"block labels":         func(s *Snapshot) { s.Blocks[0].Labels.Singular, s.Blocks[0].TypeName = "Teaser", "Teaser" },
		"locale presentation": func(s *Snapshot) {
			s.Application.Localization.Locales[1].Label, s.Application.Localization.Locales[1].RTL = "العربية", false
		},
		"select option labels": func(s *Snapshot) {
			options := s.Collections[0].Fields[1].Select.Options
			options[0], options[1] = SelectOption{Value: "dark", Label: "Night"}, SelectOption{Value: "light", Label: "Day"}
		},
	} {
		changed := manifest.Snapshot()
		change(&changed)
		if !manifest.SameStorage(NewManifest(changed)) {
			t.Errorf("%s changed storage identity", name)
		}
	}
	// Content locales key stored values, and option values and ID policy are
	// validated against them.
	for name, change := range map[string]func(*Snapshot){
		"content locale":    func(s *Snapshot) { s.Application.Localization.Locales = s.Application.Localization.Locales[:1] },
		"locale fallback":   func(s *Snapshot) { s.Application.Localization.Fallback = false },
		"select option":     func(s *Snapshot) { s.Collections[0].Fields[1].Select.Options[1].Value = "dim" },
		"ID on create":      func(s *Snapshot) { s.Application.AllowIDOnCreate = true },
		"collection fields": func(s *Snapshot) { s.Collections[0].Fields = s.Collections[0].Fields[:1] },
	} {
		changed := manifest.Snapshot()
		change(&changed)
		if manifest.SameStorage(NewManifest(changed)) {
			t.Errorf("%s was ignored", name)
		}
	}
	after, err := manifest.Bytes()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("projection mutated its source manifest: %v", err)
	}
}
