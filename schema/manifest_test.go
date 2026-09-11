package schema_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestManifestOwnsAnImmutableSnapshot(t *testing.T) {
	path, err := query.ParsePath("status")
	if err != nil {
		t.Fatal(err)
	}
	titlePath, err := query.ParsePath("title")
	if err != nil {
		t.Fatal(err)
	}
	minimumLength := 2
	input := schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{
			Name: "Example", Admin: &schema.AdminSettings{UserCollectionID: "users", UserCollectionSlug: "users"},
			Endpoints: []schema.Endpoint{{Method: "GET", Path: "/status", Summary: "Read status"}},
			AdminLocalization: &schema.AdminLocalizationSettings{
				DefaultLanguage: "en", Languages: []schema.AdminLanguage{{Code: "en", Label: "English"}, {Code: "ar", Label: "Arabic", RTL: true}},
				DefaultTimeZone: "UTC", TimeZones: []schema.AdminTimeZone{{ID: "UTC", Label: "UTC"}},
			},
			Localization: &schema.LocalizationSettings{DefaultLocale: "en", Fallback: true, Locales: []schema.Locale{
				{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
			}},
		},
		Collections: []schema.Collection{{
			ID:        "posts",
			Slug:      "posts",
			Indexes:   []schema.CollectionIndex{{Fields: []query.Path{path, titlePath}, Unique: true}},
			Endpoints: []schema.Endpoint{{Method: "POST", Path: "/:id/reindex"}},
			Fields: []schema.Field{{
				ID:       "post-status",
				Name:     "status",
				Path:     path,
				Type:     schema.FieldTypeSelect,
				Category: schema.FieldCategoryScalar,
				Admin:    schema.FieldAdmin{Row: &schema.FieldRow{ID: "post-status"}},
				Select: &schema.SelectField{Options: []schema.SelectOption{{
					Value: "draft",
					Label: "Draft",
				}}},
			}, {
				ID: "post-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText,
				Category: schema.FieldCategoryScalar, Admin: schema.FieldAdmin{Label: "Title"},
				Text: &schema.TextField{MinLength: &minimumLength},
			}},
		}},
		Globals: []schema.Global{{
			ID: "global-settings", Slug: "settings",
			Endpoints: []schema.Endpoint{{Method: "PUT", Path: "/refresh"}},
		}},
		Plugins: []schema.Plugin{{Key: "editor", Admin: &schema.PluginAdmin{Package: "@example/editor", Export: "editorAdminPlugin", APIVersion: schema.CurrentAdminPluginAPIVersion, PairingVersion: 1}}},
	}

	manifest := schema.NewManifest(input)
	before, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	input.Collections[0].Fields[0].Select.Options[0].Value = "input-mutated"
	*input.Collections[0].Fields[1].Text.MinLength = 99
	input.Collections[0].Indexes[0].Fields[0] = query.Path{}
	input.Collections[0].Fields[0].Admin.Row.ID = "input-mutated"
	input.Application.Admin.UserCollectionSlug = "input-mutated"
	input.Application.AdminLocalization.Languages[0].Label = "input-mutated"
	input.Application.AdminLocalization.TimeZones[0].Label = "input-mutated"
	input.Application.Localization.Locales[1].FallbackLocales[0] = "input-mutated"
	input.Application.Endpoints[0].Path = "/input-mutated"
	input.Collections[0].Endpoints[0].Path = "/input-mutated"
	input.Globals[0].Endpoints[0].Path = "/input-mutated"
	input.Plugins[0].Admin.Package = "input-mutated"
	returned := manifest.Snapshot()
	returned.Collections[0].Fields[0].Select.Options[0].Value = "snapshot-mutated"
	*returned.Collections[0].Fields[1].Text.MinLength = 100
	returned.Collections[0].Indexes[0].Fields[0] = query.Path{}
	returned.Collections[0].Fields[0].Admin.Row.ID = "snapshot-mutated"
	returned.Application.Admin.UserCollectionSlug = "snapshot-mutated"
	returned.Application.AdminLocalization.Languages[0].Label = "snapshot-mutated"
	returned.Application.AdminLocalization.TimeZones[0].Label = "snapshot-mutated"
	returned.Application.Localization.Locales[1].FallbackLocales[0] = "snapshot-mutated"
	returned.Application.Endpoints[0].Path = "/snapshot-mutated"
	returned.Collections[0].Endpoints[0].Path = "/snapshot-mutated"
	returned.Globals[0].Endpoints[0].Path = "/snapshot-mutated"
	returned.Plugins[0].Admin.Package = "snapshot-mutated"
	after, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(before, after) {
		t.Fatalf("manifest changed after external mutation:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got := manifest.Snapshot().Collections[0].Fields[0].Select.Options[0].Value; got != "draft" {
		t.Fatalf("manifest option = %q, want draft", got)
	}
	if got := *manifest.Snapshot().Collections[0].Fields[1].Text.MinLength; got != 2 {
		t.Fatalf("manifest minimum length = %d, want 2", got)
	}
	if got := manifest.Snapshot().Collections[0].Indexes[0].Fields[0].String(); got != "status" {
		t.Fatalf("manifest index path = %q, want status", got)
	}
	if got := manifest.Snapshot().Collections[0].Fields[0].Admin.Row.ID; got != "post-status" {
		t.Fatalf("manifest row ID = %q, want post-status", got)
	}
	if got := manifest.Snapshot().Application.Admin.UserCollectionSlug; got != "users" {
		t.Fatalf("manifest admin user collection = %q, want users", got)
	}
	if got := manifest.Snapshot().Application.AdminLocalization; got.Languages[0].Label != "English" || got.TimeZones[0].Label != "UTC" {
		t.Fatalf("manifest admin localization = %#v", got)
	}
	if got := manifest.Snapshot().Application.Localization.Locales[1].FallbackLocales[0]; got != "en" {
		t.Fatalf("manifest fallback locale = %q, want en", got)
	}
	if got := manifest.Snapshot().Application.Endpoints[0].Path; got != "/status" {
		t.Fatalf("manifest application endpoint path = %q, want /status", got)
	}
	if got := manifest.Snapshot().Collections[0].Endpoints[0].Path; got != "/:id/reindex" {
		t.Fatalf("manifest collection endpoint path = %q, want /:id/reindex", got)
	}
	if got := manifest.Snapshot().Globals[0].Endpoints[0].Path; got != "/refresh" {
		t.Fatalf("manifest global endpoint path = %q, want /refresh", got)
	}
	if got := manifest.Snapshot().Plugins[0].Admin.Package; got != "@example/editor" {
		t.Fatalf("manifest admin plugin package = %q, want @example/editor", got)
	}
}

func TestCurrentManifestMetadataFailsClosed(t *testing.T) {
	invalid := []byte(`{"version":1,"application":{"name":"Invalid"},"collections":[{"id":"posts","slug":"posts","labels":{"singular":"Post","plural":"Posts"},"admin":{},"capabilities":{"auth":false,"upload":false,"versions":false,"trash":false},"fields":[{"id":"posts-score","name":"score","path":"score","type":"number","category":"scalar","required":false,"unique":false,"admin":{"label":"Score"},"number":{"step":0}}]}],"plugins":[]}`)
	if _, err := schema.Parse(invalid); err == nil || !strings.Contains(err.Error(), "invalid number constraints") {
		t.Fatalf("Parse invalid current metadata error = %v", err)
	}
}

func TestManifestParseValidatesSelectMetadata(t *testing.T) {
	path, err := query.ParsePath("status")
	if err != nil {
		t.Fatal(err)
	}
	newSnapshot := func() schema.Snapshot {
		return schema.Snapshot{
			Version:     schema.CurrentVersion,
			Application: schema.Application{Name: "Example"},
			Collections: []schema.Collection{{
				ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
				Fields: []schema.Field{{
					ID: "post-status", Name: "status", Path: path, Type: schema.FieldTypeSelect, Category: schema.FieldCategoryScalar,
					Admin: schema.FieldAdmin{Label: "Status"}, Select: &schema.SelectField{Options: []schema.SelectOption{
						{Value: "draft", Label: "Draft"}, {Value: "published", Label: "Published"},
					}},
				}},
			}},
		}
	}
	stringPointer := func(value string) *string { return &value }
	tests := []struct {
		name   string
		mutate func(*schema.Field)
		want   string
	}{
		{name: "missing details", mutate: func(candidate *schema.Field) { candidate.Select = nil }, want: "require scalar select details"},
		{name: "details on non-select", mutate: func(candidate *schema.Field) { candidate.Type = schema.FieldTypeText }, want: "cannot declare select details"},
		{name: "missing options", mutate: func(candidate *schema.Field) { candidate.Select.Options = nil }, want: "missing select options"},
		{name: "missing option value", mutate: func(candidate *schema.Field) { candidate.Select.Options[0].Value = "" }, want: "missing select option value"},
		{name: "duplicate option", mutate: func(candidate *schema.Field) { candidate.Select.Options[1].Value = "draft" }, want: "duplicate select option value"},
		{name: "non-canonical label", mutate: func(candidate *schema.Field) { candidate.Select.Options[0].Label = " Draft " }, want: "invalid canonical select option label"},
		{name: "radio has many", mutate: func(candidate *schema.Field) { candidate.Type, candidate.Select.HasMany = schema.FieldTypeRadio, true }, want: "invalid radio cardinality"},
		{name: "multi scalar default", mutate: func(candidate *schema.Field) {
			candidate.Select.HasMany, candidate.Default = true, stringPointer("draft")
		}, want: "invalid multi-select default"},
		{name: "multi unknown default", mutate: func(candidate *schema.Field) {
			candidate.Select.HasMany, candidate.Select.DefaultValues = true, []string{"missing"}
		}, want: "invalid multi-select default"},
		{name: "multi duplicate default", mutate: func(candidate *schema.Field) {
			candidate.Select.HasMany, candidate.Select.DefaultValues = true, []string{"draft", "draft"}
		}, want: "duplicate multi-select default"},
		{name: "scalar list default", mutate: func(candidate *schema.Field) { candidate.Select.DefaultValues = []string{"draft"} }, want: "invalid scalar select defaults"},
		{name: "scalar unknown default", mutate: func(candidate *schema.Field) { candidate.Default = stringPointer("missing") }, want: "invalid select default"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := newSnapshot()
			test.mutate(&snapshot.Collections[0].Fields[0])
			encoded, marshalError := schema.NewManifest(snapshot).Bytes()
			if marshalError != nil {
				t.Fatal(marshalError)
			}
			if _, parseError := schema.Parse(encoded); parseError == nil || !strings.Contains(parseError.Error(), test.want) {
				t.Fatalf("Parse error = %v, want substring %q", parseError, test.want)
			}
		})
	}

	valid := newSnapshot()
	valid.Collections[0].Fields[0].Select.HasMany = true
	valid.Collections[0].Fields[0].Select.DefaultValues = []string{"published", "draft"}
	encoded, err := schema.NewManifest(valid).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.Parse(encoded); err != nil {
		t.Fatalf("Parse valid multi-select metadata: %v", err)
	}
}

func TestManifestParseRejectsInvalidLocalizationMetadata(t *testing.T) {
	for _, encoded := range []string{
		`{"version":1,"application":{"name":"Example","localization":{"locales":[],"defaultLocale":"en","fallback":true}},"collections":[],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example","localization":{"locales":[{"code":"en","label":"English","fallbackLocale":["fr"]},{"code":"fr","label":"French","fallbackLocale":["en"]}],"defaultLocale":"en","fallback":true}},"collections":[],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example"},"collections":[{"id":"posts","slug":"posts","labels":{"singular":"Post","plural":"Posts"},"admin":{},"capabilities":{"auth":false,"upload":false,"versions":false,"trash":false},"fields":[{"id":"posts-title","name":"title","path":"title","type":"text","category":"scalar","required":false,"unique":false,"localized":true,"admin":{"label":"Title"},"text":{}}]}],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example","adminLocalization":{"languages":[{"code":"en","label":"English"}],"defaultLanguage":"en","timeZones":[{"id":"Mars/Olympus","label":"Unknown"}] }},"collections":[],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example","adminLocalization":{"languages":[{"code":"en","label":"English"}],"defaultLanguage":"en","timeZones":[{"id":"Factory","label":"Factory"}] }},"collections":[],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example","adminLocalization":{"languages":[{"code":"en","label":"English"}],"defaultLanguage":"en","timeZones":[{"id":"+25:00","label":"Invalid"}] }},"collections":[],"plugins":[]}`,
	} {
		if _, err := schema.Parse([]byte(encoded)); err == nil {
			t.Fatalf("Parse invalid localization metadata succeeded: %s", encoded)
		}
	}
}

func TestManifestParseRejectsInvalidAdminLocalizationMetadata(t *testing.T) {
	for _, encoded := range []string{
		`{"version":1,"application":{"name":"Example","adminLocalization":{"languages":[],"defaultLanguage":"en"}},"collections":[],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example","adminLocalization":{"languages":[{"code":"en","label":"English"}],"defaultLanguage":"fr"}},"collections":[],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example","adminLocalization":{"languages":[{"code":"en","label":"English"}],"defaultLanguage":"en","timeZones":[{"id":"../London","label":"Unsafe"}]}},"collections":[],"plugins":[]}`,
	} {
		if _, err := schema.Parse([]byte(encoded)); err == nil {
			t.Fatalf("Parse invalid admin localization metadata succeeded: %s", encoded)
		}
	}
}

func TestManifestParseRejectsInvalidAdminDisplayTranslations(t *testing.T) {
	for _, encoded := range []string{
		`{"version":1,"application":{"name":"Example","nameTranslations":{"fr":"Exemple"},"adminLocalization":{"languages":[{"code":"en","label":"English"}],"defaultLanguage":"en"}},"collections":[],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example","adminLocalization":{"languages":[{"code":"en","label":"English","labelTranslations":{"fr":"Anglais"}}],"defaultLanguage":"en"}},"collections":[],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example","adminLocalization":{"languages":[{"code":"en","label":"English"}],"defaultLanguage":"en"}},"collections":[{"id":"posts","slug":"posts","labels":{"singular":"Post","plural":"Posts"},"admin":{},"capabilities":{"auth":false,"upload":false,"versions":false,"trash":false},"fields":[{"id":"title","name":"title","path":"title","type":"text","category":"scalar","required":false,"unique":false,"admin":{"label":"Title","labelTranslations":{"en":" "}},"text":{}}]}],"plugins":[]}`,
	} {
		if _, err := schema.Parse([]byte(encoded)); err == nil {
			t.Fatalf("Parse invalid admin display translations succeeded: %s", encoded)
		}
	}
}

func TestManifestParseRejectsInvalidAdminFieldMetadata(t *testing.T) {
	for _, encoded := range []string{
		`{"version":1,"application":{"name":"Example"},"collections":[{"id":"posts","slug":"posts","labels":{"singular":"Post","plural":"Posts"},"admin":{},"capabilities":{"auth":false,"upload":false,"versions":false,"trash":false},"fields":[{"id":"posts-featured","name":"featured","path":"featured","type":"checkbox","category":"scalar","required":false,"unique":false,"admin":{"label":"Featured","placeholder":"Choose"}}]}],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example","adminLocalization":{"languages":[{"code":"en","label":"English"}],"defaultLanguage":"en"}},"collections":[{"id":"posts","slug":"posts","labels":{"singular":"Post","plural":"Posts"},"admin":{},"capabilities":{"auth":false,"upload":false,"versions":false,"trash":false},"fields":[{"id":"posts-title","name":"title","path":"title","type":"text","category":"scalar","required":false,"unique":false,"admin":{"label":"Title","placeholderTranslations":{"en":"Enter a title"}},"text":{}}]}],"plugins":[]}`,
		`{"version":1,"application":{"name":"Example"},"collections":[{"id":"posts","slug":"posts","labels":{"singular":"Post","plural":"Posts"},"admin":{},"capabilities":{"auth":false,"upload":false,"versions":false,"trash":false},"fields":[{"id":"posts-seo","name":"seo","path":"seo","type":"group","category":"nested","required":false,"unique":false,"admin":{"label":"SEO"},"nested":{"fields":[{"id":"posts-seo-title","name":"title","path":"seo.title","type":"text","category":"scalar","required":false,"unique":false,"admin":{"label":"Title","sidebar":true},"text":{}}]}}]}],"plugins":[]}`,
	} {
		if _, err := schema.Parse([]byte(encoded)); err == nil {
			t.Fatalf("Parse invalid admin field metadata succeeded: %s", encoded)
		}
	}
}

func TestManifestParseRejectsUnsupportedUniqueMetadata(t *testing.T) {
	parentPath, err := query.ParsePath("meta")
	if err != nil {
		t.Fatal(err)
	}
	childPath, err := query.ParsePath("meta.code")
	if err != nil {
		t.Fatal(err)
	}
	nestedFields := func(idPrefix string) []schema.Field {
		return []schema.Field{{
			ID: schema.StableID(idPrefix + "-meta"), Name: "meta", Path: parentPath,
			Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
			Admin: schema.FieldAdmin{Label: "Meta"}, Nested: &schema.NestedField{Fields: []schema.Field{{
				ID: schema.StableID(idPrefix + "-meta-code"), Name: "code", Path: childPath,
				Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Unique: true,
				Admin: schema.FieldAdmin{Label: "Code"}, Text: &schema.TextField{},
			}}},
		}}
	}
	referencePath, err := query.ParsePath("reference")
	if err != nil {
		t.Fatal(err)
	}
	rootReference := func(relationship *schema.RelationshipField, upload *schema.UploadField) []schema.Field {
		fieldType, category := schema.FieldTypeRelationship, schema.FieldCategoryRelationship
		if upload != nil {
			fieldType, category = schema.FieldTypeUpload, schema.FieldCategoryUpload
		}
		return []schema.Field{{
			ID: "posts-reference", Name: "reference", Path: referencePath,
			Type: fieldType, Category: category, Unique: true, Admin: schema.FieldAdmin{Label: "Reference"},
			Relationship: relationship, Upload: upload,
		}}
	}
	rootOutput := func(fieldType schema.FieldType) []schema.Field {
		return []schema.Field{{
			ID: "posts-output", Name: "output", Path: referencePath,
			Type: fieldType, Category: schema.FieldCategoryPresentation, Unique: true,
			Admin: schema.FieldAdmin{Label: "Output"},
		}}
	}
	rootStored := func(fieldType schema.FieldType, category schema.FieldCategory) []schema.Field {
		return []schema.Field{{
			ID: "posts-value", Name: "value", Path: referencePath,
			Type: fieldType, Category: category, Unique: true,
			Admin: schema.FieldAdmin{Label: "Value"},
		}}
	}
	tests := []struct {
		name     string
		snapshot schema.Snapshot
		expected string
	}{
		{
			name: "nested collection",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Nested unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: nestedFields("posts")}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].nested.fields[0].unique: nested fields",
		},
		{
			name: "nested global",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Nested unique"},
				Globals: []schema.Global{{ID: "global-settings", Slug: "settings", Fields: nestedFields("global-settings")}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at globals[0].fields[0].nested.fields[0].unique: nested fields",
		},
		{
			name: "has-many relationship",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootReference(&schema.RelationshipField{HasMany: true}, nil)}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].unique: list-valued references",
		},
		{
			name: "polymorphic relationship",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootReference(&schema.RelationshipField{Polymorphic: true}, nil)}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].unique: polymorphic references",
		},
		{
			name: "has-many upload",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootReference(nil, &schema.UploadField{HasMany: true})}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].unique: list-valued references",
		},
		{
			name: "UI field",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootOutput(schema.FieldTypeUI)}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].unique: output-only and presentation fields",
		},
		{
			name: "join field",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootOutput(schema.FieldTypeJoin)}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].unique: output-only and presentation fields",
		},
		{
			name: "virtual field",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootOutput(schema.FieldTypeVirtual)}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].unique: output-only and presentation fields",
		},
		{
			name: "mismatched presentation category",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootOutput(schema.FieldTypeText)}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].unique: output-only and presentation fields",
		},
		{
			name: "UI type with scalar category",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: func() []schema.Field {
					fields := rootOutput(schema.FieldTypeUI)
					fields[0].Category = schema.FieldCategoryScalar
					return fields
				}()}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].unique: output-only and presentation fields",
		},
		{
			name: "join type with scalar category",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: func() []schema.Field {
					fields := rootOutput(schema.FieldTypeJoin)
					fields[0].Category = schema.FieldCategoryScalar
					return fields
				}()}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].unique: output-only and presentation fields",
		},
		{
			name: "virtual type with scalar category",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: func() []schema.Field {
					fields := rootOutput(schema.FieldTypeVirtual)
					fields[0].Category = schema.FieldCategoryScalar
					return fields
				}()}}, Plugins: []schema.Plugin{},
			},
			expected: "unsupported unique field at collections[0].fields[0].unique: output-only and presentation fields",
		},
		{
			name: "JSON field",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootStored(schema.FieldTypeJSON, schema.FieldCategoryScalar)}}, Plugins: []schema.Plugin{},
			},
			expected: `unsupported unique field at collections[0].fields[0].unique: field type "json"`,
		},
		{
			name: "point field",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootStored(schema.FieldTypePoint, schema.FieldCategoryScalar)}}, Plugins: []schema.Plugin{},
			},
			expected: `unsupported unique field at collections[0].fields[0].unique: field type "point"`,
		},
		{
			name: "group field",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootStored(schema.FieldTypeGroup, schema.FieldCategoryNested)}}, Plugins: []schema.Plugin{},
			},
			expected: `unsupported unique field at collections[0].fields[0].unique: field type "group"`,
		},
		{
			name: "array field",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootStored(schema.FieldTypeArray, schema.FieldCategoryNested)}}, Plugins: []schema.Plugin{},
			},
			expected: `unsupported unique field at collections[0].fields[0].unique: field type "array"`,
		},
		{
			name: "blocks field",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootStored(schema.FieldTypeBlocks, schema.FieldCategoryNested)}}, Plugins: []schema.Plugin{},
			},
			expected: `unsupported unique field at collections[0].fields[0].unique: field type "blocks"`,
		},
		{
			name: "plugin field",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootStored(schema.FieldTypePlugin, schema.FieldCategoryPlugin)}}, Plugins: []schema.Plugin{},
			},
			expected: `unsupported unique field at collections[0].fields[0].unique: field type "plugin"`,
		},
		{
			name: "scalar type with relationship category",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootStored(schema.FieldTypeText, schema.FieldCategoryRelationship)}}, Plugins: []schema.Plugin{},
			},
			expected: `unsupported unique field at collections[0].fields[0].unique: field type "text"`,
		},
		{
			name: "relationship without descriptor",
			snapshot: schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unsupported unique"},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: rootStored(schema.FieldTypeRelationship, schema.FieldCategoryRelationship)}}, Plugins: []schema.Plugin{},
			},
			expected: `unsupported unique field at collections[0].fields[0].unique: field type "relationship"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := schema.NewManifest(test.snapshot).Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("Parse error = %v, want %q", err, test.expected)
			}
		})
	}
}

func TestManifestParseRejectsMalformedServerEnforcedReferenceFilters(t *testing.T) {
	mustPath := func(value string) query.Path {
		path, err := query.ParsePath(value)
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	mustPathPointer := func(value string) *query.Path {
		path := mustPath(value)
		return &path
	}
	scalar := func(id, name string) schema.Field {
		return schema.Field{
			ID: schema.StableID(id), Name: name, Path: mustPath(name), Type: schema.FieldTypeText,
			Category: schema.FieldCategoryScalar, Admin: schema.FieldAdmin{Label: name}, Text: &schema.TextField{},
		}
	}
	base := schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Reference filters"},
		Collections: []schema.Collection{
			{ID: "authors", Slug: "authors", Fields: []schema.Field{scalar("authors-name", "name")}},
			{ID: "media", Slug: "media", Capabilities: schema.Capabilities{Upload: true}, Fields: []schema.Field{scalar("media-mime", "mimeType")}},
			{ID: "posts", Slug: "posts", Fields: []schema.Field{
				scalar("posts-category", "category"),
				{
					ID: "posts-author", Name: "author", Path: mustPath("author"), Type: schema.FieldTypeRelationship,
					Category: schema.FieldCategoryRelationship, Admin: schema.FieldAdmin{Label: "Author"},
					Relationship: &schema.RelationshipField{
						CollectionID: "authors", CollectionSlug: "authors",
						OptionFilters: []schema.RelationshipFilter{
							{TargetPath: mustPath("name"), Operator: "equals", SourcePath: mustPathPointer("category")},
							{TargetPath: mustPath("name"), Operator: "like", SourcePath: mustPathPointer("category")},
						},
					},
				},
				{
					ID: "posts-hero", Name: "hero", Path: mustPath("hero"), Type: schema.FieldTypeUpload,
					Category: schema.FieldCategoryUpload, Admin: schema.FieldAdmin{Label: "Hero"},
					Upload: &schema.UploadField{
						CollectionID: "media", CollectionSlug: "media",
						OptionFilters: []schema.RelationshipFilter{{TargetPath: mustPath("mimeType"), Operator: "equals", SourcePath: mustPathPointer("category")}},
					},
				},
			}},
		},
		Plugins: []schema.Plugin{},
	}

	mutations := []struct {
		name     string
		mutate   func(*schema.Snapshot)
		expected string
	}{
		{
			name: "unknown operator", mutate: func(snapshot *schema.Snapshot) {
				snapshot.Collections[2].Fields[1].Relationship.OptionFilters[0].Operator = "executeSQL"
			}, expected: "unsupported operator",
		},
		{
			name: "missing source", mutate: func(snapshot *schema.Snapshot) {
				snapshot.Collections[2].Fields[1].Relationship.OptionFilters[0].SourcePath = mustPathPointer("missing")
			}, expected: "source field",
		},
		{
			name: "missing relationship target", mutate: func(snapshot *schema.Snapshot) {
				snapshot.Collections[2].Fields[1].Relationship.OptionFilters[0].TargetPath = mustPath("missing")
			}, expected: "target field",
		},
		{
			name: "wrong polymorphic scope", mutate: func(snapshot *schema.Snapshot) {
				snapshot.Collections[2].Fields[1].Relationship.OptionFilters[0].CollectionSlug = "media"
			}, expected: "does not target collection",
		},
		{
			name: "mismatched target identity", mutate: func(snapshot *schema.Snapshot) {
				snapshot.Collections[2].Fields[1].Relationship.CollectionSlug = "writers"
			}, expected: "target collection",
		},
		{
			name: "missing upload target field", mutate: func(snapshot *schema.Snapshot) {
				snapshot.Collections[2].Fields[2].Upload.OptionFilters[0].TargetPath = mustPath("missing")
			}, expected: "target field",
		},
		{
			name: "incompatible scalar domains", mutate: func(snapshot *schema.Snapshot) {
				snapshot.Collections[2].Fields[0].Type = schema.FieldTypeNumber
				snapshot.Collections[2].Fields[0].Text = nil
			}, expected: "compatible scalar operand and target types",
		},
	}

	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			snapshot := schema.NewManifest(base).Snapshot()
			test.mutate(&snapshot)
			encoded, err := schema.NewManifest(snapshot).Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("Parse error = %v, want %q", err, test.expected)
			}
		})
	}
}

func TestManifestConditionSnapshotIsImmutable(t *testing.T) {
	path, err := query.ParsePath("status")
	if err != nil {
		t.Fatal(err)
	}
	condition := &schema.FieldCondition{
		Kind: schema.FieldConditionKindNot,
		Conditions: []schema.FieldCondition{{
			Kind: schema.FieldConditionKindPredicate,
			Predicate: &schema.FieldConditionPredicate{
				Scope: schema.FieldConditionDocument, Path: path, Operator: schema.FieldConditionOneOf,
				Values: []schema.ScalarLiteral{{Type: schema.ValueTypeString, Value: "draft"}, {Type: schema.ValueTypeString, Value: "published"}},
			},
		}},
	}
	input := schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Conditions"},
		Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: []schema.Field{{
			ID: "posts-title", Name: "title", Path: path, Type: schema.FieldTypeText,
			Category: schema.FieldCategoryScalar, Admin: schema.FieldAdmin{Label: "Title", Condition: condition}, Text: &schema.TextField{},
		}}}}, Plugins: []schema.Plugin{},
	}
	manifest := schema.NewManifest(input)
	condition.Conditions[0].Predicate.Values[0].Value = "mutated-input"
	returned := manifest.Snapshot()
	returned.Collections[0].Fields[0].Admin.Condition.Conditions[0].Predicate.Values[1].Value = "mutated-snapshot"

	got := manifest.Snapshot().Collections[0].Fields[0].Admin.Condition.Conditions[0].Predicate.Values
	if got[0].Value != "draft" || got[1].Value != "published" {
		t.Fatalf("manifest condition was mutated through an external snapshot: %#v", got)
	}
}

func TestManifestParseRejectsMalformedFieldConditions(t *testing.T) {
	statusPath, err := query.ParsePath("status")
	if err != nil {
		t.Fatal(err)
	}
	titlePath, err := query.ParsePath("title")
	if err != nil {
		t.Fatal(err)
	}
	base := schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Conditions"},
		Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: []schema.Field{{
			ID: "posts-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText,
			Category: schema.FieldCategoryScalar, Admin: schema.FieldAdmin{Label: "Title", Condition: &schema.FieldCondition{
				Kind: schema.FieldConditionKindPredicate,
				Predicate: &schema.FieldConditionPredicate{
					Scope: schema.FieldConditionDocument, Path: statusPath, Operator: schema.FieldConditionEquals,
					Values: []schema.ScalarLiteral{{Type: schema.ValueTypeString, Value: "published"}},
				},
			}}, Text: &schema.TextField{},
		}, {
			ID: "posts-status", Name: "status", Path: statusPath, Type: schema.FieldTypeText,
			Category: schema.FieldCategoryScalar, Admin: schema.FieldAdmin{Label: "Status"}, Text: &schema.TextField{},
		}}}}, Plugins: []schema.Plugin{},
	}
	tests := []struct {
		name     string
		mutate   func(*schema.FieldCondition)
		expected string
	}{
		{"empty all", func(condition *schema.FieldCondition) {
			condition.Kind, condition.Predicate, condition.Conditions = schema.FieldConditionKindAll, nil, nil
		}, "invalid all field condition"},
		{"unknown scope", func(condition *schema.FieldCondition) {
			condition.Predicate.Scope = "nearest"
		}, "invalid field condition scope"},
		{"wrong value count", func(condition *schema.FieldCondition) {
			condition.Predicate.Values = append(condition.Predicate.Values, schema.ScalarLiteral{Type: schema.ValueTypeString, Value: "draft"})
		}, "requires exactly one value"},
		{"invalid boolean", func(condition *schema.FieldCondition) {
			condition.Predicate.Values[0] = schema.ScalarLiteral{Type: schema.ValueTypeBoolean, Value: "yes"}
		}, "invalid boolean field condition value"},
		{"json operand", func(condition *schema.FieldCondition) {
			condition.Predicate.Values[0] = schema.ScalarLiteral{Type: schema.ValueTypeJSON, Value: "{}"}
		}, "invalid field condition value type"},
		{"unknown field path", func(condition *schema.FieldCondition) {
			condition.Predicate.Path, _ = query.ParsePath("missing")
		}, "invalid field condition path"},
		{"mismatched operand", func(condition *schema.FieldCondition) {
			condition.Predicate.Values[0] = schema.ScalarLiteral{Type: schema.ValueTypeNumber, Value: "1"}
		}, "invalid field condition operand"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := schema.NewManifest(base).Snapshot()
			test.mutate(snapshot.Collections[0].Fields[0].Admin.Condition)
			encoded, err := schema.NewManifest(snapshot).Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("Parse error = %v, want %q", err, test.expected)
			}
		})
	}
}

func TestManifestRowLabelComponentSnapshotIsImmutable(t *testing.T) {
	arrayPath, err := query.ParsePath("items")
	if err != nil {
		t.Fatal(err)
	}
	config := json.RawMessage(`{"key":"optionKey","label":"label"}`)
	input := schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Row labels"},
		Collections: []schema.Collection{{ID: "questions", Slug: "questions", Fields: []schema.Field{{
			ID: "questions-items", Name: "items", Path: arrayPath, Type: schema.FieldTypeArray,
			Category: schema.FieldCategoryNested, Admin: schema.FieldAdmin{Label: "Items"},
			Nested: &schema.NestedField{Fields: []schema.Field{}, RowLabelComponent: &schema.FieldAdminComponent{
				Plugin: "curriculum", Component: "questionOption", Config: config,
			}},
		}}}},
		Plugins: []schema.Plugin{{Key: "curriculum", Version: "1.0.0", GoPackage: "example.com/plugins/curriculum",
			APIVersion: schema.CurrentPluginAPIVersion, Ridu: &schema.PluginCompatibility{Minimum: "0.1.0", MaximumExclusive: "0.2.0"}, Admin: &schema.PluginAdmin{
				Package: "@example/curriculum-admin", Export: "curriculumAdminPlugin",
				APIVersion: schema.CurrentAdminPluginAPIVersion, PairingVersion: 1,
			}}},
	}
	manifest := schema.NewManifest(input)
	before, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	config[2] = 'X'
	returned := manifest.Snapshot()
	returned.Collections[0].Fields[0].Nested.RowLabelComponent.Config[2] = 'Y'
	after, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("row label component manifest changed after mutation:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestManifestParseValidatesRowLabelComponents(t *testing.T) {
	arrayPath, err := query.ParsePath("items")
	if err != nil {
		t.Fatal(err)
	}
	base := schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Row labels"},
		Collections: []schema.Collection{{ID: "questions", Slug: "questions", Fields: []schema.Field{{
			ID: "questions-items", Name: "items", Path: arrayPath, Type: schema.FieldTypeArray,
			Category: schema.FieldCategoryNested, Admin: schema.FieldAdmin{Label: "Items"},
			Nested: &schema.NestedField{Fields: []schema.Field{}, RowLabelComponent: &schema.FieldAdminComponent{
				Plugin: "curriculum", Component: "questionOption", Config: json.RawMessage(`{"key":"optionKey"}`),
			}},
		}}}},
		Plugins: []schema.Plugin{{Key: "curriculum", Version: "1.0.0", GoPackage: "example.com/plugins/curriculum",
			APIVersion: schema.CurrentPluginAPIVersion, Ridu: &schema.PluginCompatibility{Minimum: "0.1.0", MaximumExclusive: "0.2.0"}, Admin: &schema.PluginAdmin{
				Package: "@example/curriculum-admin", Export: "curriculumAdminPlugin",
				APIVersion: schema.CurrentAdminPluginAPIVersion, PairingVersion: 1,
			}}},
	}
	valid, err := schema.NewManifest(base).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.Parse(valid); err != nil {
		t.Fatalf("Parse valid row label component: %v", err)
	}

	tests := []struct {
		name     string
		mutate   func(*schema.Snapshot)
		expected string
	}{
		{"missing pair", func(snapshot *schema.Snapshot) { snapshot.Plugins = nil }, "requires paired admin plugin"},
		{"invalid component", func(snapshot *schema.Snapshot) {
			snapshot.Collections[0].Fields[0].Nested.RowLabelComponent.Component = "bad-name"
		}, "invalid admin row label component"},
		{"scalar config", func(snapshot *schema.Snapshot) {
			snapshot.Collections[0].Fields[0].Nested.RowLabelComponent.Config = json.RawMessage(`true`)
		}, "invalid admin row label component"},
		{"unsupported field", func(snapshot *schema.Snapshot) {
			snapshot.Collections[0].Fields[0].Type = schema.FieldTypeGroup
		}, "requires an array or blocks field"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := schema.NewManifest(base).Snapshot()
			test.mutate(&snapshot)
			encoded, encodeErr := schema.NewManifest(snapshot).Bytes()
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if _, parseErr := schema.Parse(encoded); parseErr == nil || !strings.Contains(parseErr.Error(), test.expected) {
				t.Fatalf("Parse error = %v, want %q", parseErr, test.expected)
			}
		})
	}
}

func TestManifestParseRoundTrip(t *testing.T) {
	path, err := query.ParsePath("title")
	if err != nil {
		t.Fatal(err)
	}
	original := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Example"},
		Collections: []schema.Collection{{
			ID:   "posts",
			Slug: "posts",
			Fields: []schema.Field{{
				ID:       "post-title",
				Name:     "title",
				Path:     path,
				Type:     schema.FieldTypeText,
				Category: schema.FieldCategoryScalar,
				Admin:    schema.FieldAdmin{Row: &schema.FieldRow{ID: "post-title"}},
				Text:     &schema.TextField{},
			}},
		}},
		Plugins: []schema.Plugin{},
	})
	encoded, err := original.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := schema.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !original.Equal(parsed) {
		t.Fatalf("round-tripped manifest differs:\n%s", encoded)
	}

	var unmarshaled schema.Manifest
	if err := json.Unmarshal(encoded, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal Manifest: %v", err)
	}
	if !original.Equal(unmarshaled) {
		t.Fatal("json.Unmarshaled manifest differs")
	}
}

func TestManifestParseRejectsUnknownAndUnsupportedContracts(t *testing.T) {
	if _, err := schema.Parse([]byte(`{"version":1,"application":{"name":"Example"},"collections":[],"plugins":[],"unknown":true}`)); err == nil {
		t.Fatal("Parse with unknown property succeeded, want error")
	}
	if _, err := schema.Parse([]byte(`{"version":999,"application":{"name":"Example"},"collections":[],"plugins":[]}`)); err == nil {
		t.Fatal("Parse with unsupported version succeeded, want error")
	}
}

func TestManifestParseRejectsUnsafeAdminPluginImports(t *testing.T) {
	base := schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Example"}, Collections: []schema.Collection{},
		Plugins: []schema.Plugin{{
			Key: "editor", Version: "1.0.0", GoPackage: "example.com/editor", APIVersion: schema.CurrentPluginAPIVersion,
			Ridu:  &schema.PluginCompatibility{Minimum: "0.0.0-dev"},
			Admin: &schema.PluginAdmin{Package: "@example/editor", Export: "editorAdminPlugin", APIVersion: schema.CurrentAdminPluginAPIVersion, PairingVersion: 1},
		}},
	}
	tests := []struct {
		name   string
		mutate func(*schema.PluginAdmin)
		want   string
	}{
		{name: "package", mutate: func(admin *schema.PluginAdmin) { admin.Package = "../editor" }, want: "invalid admin plugin package"},
		{name: "export", mutate: func(admin *schema.PluginAdmin) { admin.Export = "editor;run()" }, want: "invalid admin plugin export"},
		{name: "route", mutate: func(admin *schema.PluginAdmin) { admin.Routes = []string{"collections/posts"} }, want: "invalid admin plugin route"},
		{name: "asset", mutate: func(admin *schema.PluginAdmin) { admin.Assets = []string{"../steal.js"} }, want: "invalid admin plugin asset"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := schema.NewManifest(base).Snapshot()
			test.mutate(snapshot.Plugins[0].Admin)
			encoded, err := schema.NewManifest(snapshot).Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSemanticVersionRangesFollowPrereleaseOrdering(t *testing.T) {
	if !schema.IsValidSemanticVersionRange("1.0.0-alpha.2", "1.0.0-alpha.10") {
		t.Fatal("numeric prerelease range was rejected")
	}
	if schema.IsValidSemanticVersionRange("1.0.0", "1.0.0-beta.1") {
		t.Fatal("empty reverse release range was accepted")
	}
}

func TestRetiredPluginSQLMetadataIsRejected(t *testing.T) {
	_, err := schema.Parse([]byte(`{"version":1,"application":{"name":"Retired"},"collections":[],"plugins":[{"key":"audit","databaseContributions":[]}]}`))
	if err == nil || !strings.Contains(err.Error(), "databaseContributions") {
		t.Fatalf("retired SQL metadata accepted: %v", err)
	}
}
