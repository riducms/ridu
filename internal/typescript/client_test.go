package typescript_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestGeneratedClientIsCurrentAndContainsNoAny(t *testing.T) {
	manifest, err := ridu.Resolve(clientFixtureConfig())
	if err != nil {
		t.Fatal(err)
	}
	actual, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(moduleRoot(t), "testdata", "generated", "ridu.generated.ts")
	if os.Getenv("RIDU_UPDATE_GENERATED_CLIENT") == "1" {
		if err := os.WriteFile(path, actual, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated client %s: %v\nactual:\n%s", path, err, actual)
	}
	if string(actual) != string(expected) {
		t.Fatalf("generated client drift; run RIDU_UPDATE_GENERATED_CLIENT=1 go test ./internal/typescript -run TestGeneratedClientIsCurrentAndContainsNoAny to regenerate %s\nexpected:\n%s\nactual:\n%s", path, expected, actual)
	}
	if strings.Contains(string(actual), " any") || strings.Contains(string(actual), "any[]") {
		t.Fatal("generated public client contains any")
	}
	if !strings.Contains(string(actual), "return createRuntimeClient<RiduConfig>(options)") {
		t.Fatal("generated client does not explicitly bind its application contract")
	}
}

func TestGeneratedCreateIncludesCallerIDOnlyWhenEnabled(t *testing.T) {
	generate := func(allow bool) string {
		t.Helper()
		manifest, err := ridu.Resolve(ridu.Config{
			Name: "Caller IDs", AllowIDOnCreate: allow,
			Collections: []ridu.Collection{{
				Slug:   "posts",
				Fields: field.Fields{field.Text("title").Required()},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		generated, err := typescript.Client(manifest)
		if err != nil {
			t.Fatal(err)
		}
		return string(generated)
	}

	if enabled := generate(true); !strings.Contains(enabled, "export interface PostsCreate {\n\tid?: ID;\n") {
		t.Fatalf("caller-ID create contract is missing:\n%s", enabled)
	}
	if disabled := generate(false); strings.Contains(disabled, "export interface PostsCreate {\n\tid?: ID;\n") {
		t.Fatalf("disabled caller-ID create contract exposes id:\n%s", disabled)
	}
}

func TestGeneratedClientUsesPluginOwnedExactTypes(t *testing.T) {
	path, err := query.NewPath("accent")
	if err != nil {
		t.Fatal(err)
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Plugin types"},
		Plugins:     []schema.Plugin{{Key: "color", FieldTypes: []schema.PluginFieldType{{Key: "color", TypeScriptPackage: "@example/color-admin", TypeScriptOutput: "Color", TypeScriptInput: "ColorInput", TypeScriptWhere: "ColorWhere"}}}},
		Collections: []schema.Collection{{ID: "brands", Slug: "brands", Labels: schema.CollectionLabels{Singular: "Brand", Plural: "Brands"}, Fields: []schema.Field{{ID: "accent", Name: "accent", Path: path, Type: schema.FieldTypePlugin, Required: true, Plugin: &schema.PluginField{Key: "color"}}}}},
	})
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`import("@example/color-admin").Color`, `import("@example/color-admin").ColorInput`, `import("@example/color-admin").ColorWhere`} {
		if !strings.Contains(string(generated), expected) {
			t.Fatalf("generated client missing %s:\n%s", expected, generated)
		}
	}
}

func TestGeneratedClientIncludesTypedGlobals(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name:        "Generated globals",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Label: "Site settings", Versions: true,
			Fields: field.Fields{field.Text("siteName").Required()},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"export interface SiteSettings {", `"siteName": string;`, `"site-settings": {`,
		"versions: true;", "output: SiteSettings;", "update: SiteSettingsUpdate;",
	} {
		if !strings.Contains(string(generated), expected) {
			t.Fatalf("generated global client missing %q:\n%s", expected, generated)
		}
	}
}

func TestGeneratedClientDistinguishesSingleAndAllLocaleDocuments(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Localized contracts",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{Slug: "authors", Fields: field.Fields{field.Text("name").Localized()}},
			{Slug: "posts", Fields: field.Fields{field.Text("title").Required().Localized(), field.Relationship("author", "authors").Localized(), field.Group("seo", field.Fields{field.Text("description").Localized(), field.Text("slug")})}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`export type Locale = "en" | "fr";`,
		`type RiduLocalizedValues<Value> = Partial<Record<Locale, Value>> & {`,
		`readonly [Symbol.toStringTag]?: "RiduLocalizedValues";`,
		`export interface PostsAllLocales {`,
		`"title"?: RiduLocalizedValues<string>;`,
		`"author"?: RiduLocalizedValues<ID | AuthorsAllLocales | null>;`,
		`"description"?: RiduLocalizedValues<string | null>;`,
		`export interface PostsAllLocalesPopulateOutput {`,
		`"author": RiduLocalizedValues<ID | AuthorsAllLocales | null>;`,
		`allOutput: PostsAllLocales;`,
		`allPopulateOutput: PostsAllLocalesPopulateOutput;`,
		`locale: Locale;`,
		"`title.${Locale}`",
	} {
		if !strings.Contains(string(generated), expected) {
			t.Fatalf("generated localized client missing %q:\n%s", expected, generated)
		}
	}
}

func TestGeneratedClientMatchesRESTSelectAndPopulateShapes(t *testing.T) {
	path := func(segments ...string) query.Path {
		t.Helper()
		value, err := query.NewPath(segments...)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	people := schema.RelationshipField{CollectionID: "people", CollectionSlug: "people"}
	media := schema.UploadField{CollectionID: "media", CollectionSlug: "media"}
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Query option contracts"},
		Collections: []schema.Collection{
			{ID: "people", Slug: "people", Capabilities: schema.Capabilities{Versions: true}, Versions: &schema.VersionSettings{}, Fields: []schema.Field{
				{ID: "people-name", Name: "name", Path: path("name"), Type: schema.FieldTypeText},
				{ID: "people-label", Name: "label", Path: path("label"), Type: schema.FieldTypeVirtual, Category: schema.FieldCategoryPresentation, Virtual: &schema.VirtualField{ValueType: schema.ValueTypeString}},
				{ID: "people-posts", Name: "posts", Path: path("posts"), Type: schema.FieldTypeJoin, Category: schema.FieldCategoryPresentation, Join: &schema.JoinField{CollectionID: "posts", CollectionSlug: "posts", On: path("subject")}},
			}},
			{ID: "media", Slug: "media", Capabilities: schema.Capabilities{Upload: true, Trash: true}, Upload: &schema.UploadSettings{}, Fields: []schema.Field{
				{ID: "media-name", Name: "name", Path: path("name"), Type: schema.FieldTypeText},
				{ID: "media-caption", Name: "caption", Path: path("caption"), Type: schema.FieldTypeText},
			}},
			{
				ID:           "posts",
				Slug:         "posts",
				Capabilities: schema.Capabilities{Trash: true, Versions: true},
				Versions:     &schema.VersionSettings{},
				Fields: []schema.Field{
					{ID: "title", Name: "title", Path: path("title"), Type: schema.FieldTypeText},
					{ID: "meta", Name: "meta", Path: path("meta"), Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{
						{ID: "meta-reviewer", Name: "reviewer", Path: path("meta", "reviewer"), Type: schema.FieldTypeRelationship, Relationship: &people},
					}}},
					{ID: "sections", Name: "sections", Path: path("sections"), Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{
						{ID: "sections-editor", Name: "editor", Path: path("sections", "editor"), Type: schema.FieldTypeRelationship, Relationship: &people},
					}}},
					{ID: "layout", Name: "layout", Path: path("layout"), Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{Types: []schema.BlockType{{Slug: "quote", Fields: []schema.Field{
						{ID: "layout-quote-source", Name: "source", Path: path("layout", "quote", "source"), Type: schema.FieldTypeRelationship, Relationship: &people},
					}}}}},
					{ID: "subject", Name: "subject", Path: path("subject"), Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{Polymorphic: true, Targets: []schema.RelationshipTarget{
						{CollectionID: "people", CollectionSlug: "people"},
						{CollectionID: "media", CollectionSlug: "media"},
					}}},
					{ID: "cover", Name: "cover", Path: path("cover"), Type: schema.FieldTypeUpload, Upload: &media},
				},
			},
		},
	})
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	if !strings.Contains(text, "export type PeoplePopulate = Record<string, never>;") || strings.Contains(text, "export type PeoplePopulate = Record<never, never>;") {
		t.Fatalf("generated empty population contract is not exact:\n%s", text)
	}
	section := func(name string) string {
		t.Helper()
		prefix := "export interface " + name + " {"
		start := strings.Index(text, prefix)
		if start == -1 {
			t.Fatalf("generated client has no %s", name)
		}
		end := strings.Index(text[start:], "\n}\n")
		if end == -1 {
			t.Fatalf("generated %s is unterminated", name)
		}
		return text[start : start+end+3]
	}

	selectContract := section("PostsSelect")
	for _, expected := range []string{
		"deletedAt?: boolean;", "_status?: boolean;", "_revision?: boolean;",
		`"meta"?: boolean;`, `"sections"?: boolean;`, `"layout"?: boolean;`,
	} {
		if !strings.Contains(selectContract, expected) {
			t.Fatalf("generated select is missing %q:\n%s", expected, selectContract)
		}
	}
	if strings.Contains(selectContract, `"reviewer"`) || strings.Contains(selectContract, "boolean | {") {
		t.Fatalf("generated select does not match the REST top-level boolean grammar:\n%s", selectContract)
	}
	peopleSelectContract := section("PeopleSelect")
	for _, expected := range []string{`"name"?: boolean;`, `"label"?: boolean;`, `"posts"?: boolean;`} {
		if !strings.Contains(peopleSelectContract, expected) {
			t.Fatalf("generated root select is missing output field %q:\n%s", expected, peopleSelectContract)
		}
	}
	peoplePopulationSelect := section("PeoplePopulationSelect")
	for _, expected := range []string{"id?: boolean;", "createdAt?: boolean;", "updatedAt?: boolean;", "_status?: boolean;", "_revision?: boolean;", `"name"?: boolean;`} {
		if !strings.Contains(peoplePopulationSelect, expected) {
			t.Fatalf("generated population target select is missing %q:\n%s", expected, peoplePopulationSelect)
		}
	}
	for _, unsupported := range []string{`"label"`, `"posts"`, "deletedAt"} {
		if strings.Contains(peoplePopulationSelect, unsupported) {
			t.Fatalf("generated population target select includes unsupported field %q:\n%s", unsupported, peoplePopulationSelect)
		}
	}
	mediaPopulationSelect := section("MediaPopulationSelect")
	for _, expected := range []string{"deletedAt?: boolean;", `"name"?: boolean;`, `"caption"?: boolean;`} {
		if !strings.Contains(mediaPopulationSelect, expected) {
			t.Fatalf("generated upload population target select is missing %q:\n%s", expected, mediaPopulationSelect)
		}
	}
	if strings.Contains(mediaPopulationSelect, "_status") || strings.Contains(mediaPopulationSelect, "_revision") {
		t.Fatalf("generated upload population target select includes unavailable version metadata:\n%s", mediaPopulationSelect)
	}

	whereContract := section("PostsWhere")
	for _, expected := range []string{
		"id?: ScalarWhere<ID>;", "createdAt?: TimestampWhere;", "updatedAt?: TimestampWhere;", `_status?: ScalarWhere<"published">;`,
		`"meta"?: ExistsWhere;`, `"sections"?: ExistsWhere;`, `"layout"?: ExistsWhere;`,
		`"meta.reviewer"?: ScalarWhere<ID>;`,
		`"sections.editor"?: ScalarWhere<ID>;`,
		`"layout.quote.source"?: ScalarWhere<ID>;`,
	} {
		if !strings.Contains(whereContract, expected) {
			t.Fatalf("generated where is missing queryable system field %q:\n%s", expected, whereContract)
		}
	}
	for _, unsupported := range []string{`"meta"?: {`, `"sections"?: {`, `"layout"?: ScalarWhere`} {
		if strings.Contains(whereContract, unsupported) {
			t.Fatalf("generated where still contains non-REST nested shape %q:\n%s", unsupported, whereContract)
		}
	}

	populateContract := section("PostsPopulate")
	for _, expected := range []string{
		`"meta.reviewer"?: boolean | PeoplePopulationSelect | { depth?: number; select?: PeoplePopulationSelect };`,
		`"sections.editor"?: boolean | PeoplePopulationSelect | { depth?: number; select?: PeoplePopulationSelect };`,
		`"layout.quote.source"?: boolean | PeoplePopulationSelect | { depth?: number; select?: PeoplePopulationSelect };`,
		`"subject"?: boolean | PeoplePopulationSelect | MediaPopulationSelect | { depth?: number; select?: PeoplePopulationSelect | MediaPopulationSelect };`,
		`"cover"?: boolean | MediaPopulationSelect | { depth?: number; select?: MediaPopulationSelect };`,
	} {
		if !strings.Contains(populateContract, expected) {
			t.Fatalf("generated populate is missing %q:\n%s", expected, populateContract)
		}
	}
	for _, unsupported := range []string{`"meta"?: {`, `"people"?: PeoplePopulationSelect`, `"media"?: MediaPopulationSelect`} {
		if strings.Contains(populateContract, unsupported) {
			t.Fatalf("generated populate still contains unsupported nested shape %q:\n%s", unsupported, populateContract)
		}
	}
}

func TestGeneratedClientIncludesStoredFieldFamiliesAndOmitsUIFields(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name:        "Field contracts",
		Collections: []ridu.Collection{{Slug: "showcases", Fields: field.Fields{field.Code("source").Admin(field.Admin{CodeLanguage: "typescript"}), field.Radio("priority", "low", "high"), field.Point("location"), field.UI("guide").Admin(field.Admin{Description: "presentation only"}), field.Collapsible("advanced", field.Fields{field.Text("internalName")}).Admin(field.Admin{InitiallyCollapsed: true})}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"source"?: string`, `"priority"?: "low" | "high"`, `"location"?: [longitude: number, latitude: number]`, `"internalName"?: string`} {
		if !strings.Contains(string(generated), expected) {
			t.Fatalf("generated client missing %q:\n%s", expected, generated)
		}
	}
	if strings.Contains(string(generated), `"guide"`) {
		t.Fatalf("presentation-only UI field leaked into generated client:\n%s", generated)
	}
}

func TestGeneratedClientTypesMultiSelectAsOrderedOptionArray(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name:        "Multi-select contracts",
		Collections: []ridu.Collection{{Slug: "users", Fields: field.Fields{field.MultiSelect("roles", "admin", "editor").Default("admin").Required()}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, expected := range []string{
		`"roles"?: Array<"admin" | "editor">;`,
		"export interface UsersCreate {\n\t\"roles\"?: Array<\"admin\" | \"editor\">;",
		"export interface UsersUpdate {\n\t\"roles\"?: Array<\"admin\" | \"editor\">;",
		`"roles"?: MultiSelectWhere<"admin" | "editor">;`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated multi-select client missing %q:\n%s", expected, text)
		}
	}
}

func TestGeneratedClientKeepsJoinAndVirtualFieldsOutputOnly(t *testing.T) {
	joinPath, _ := query.NewPath("category")
	joinFieldPath, _ := query.NewPath("posts")
	virtualPath, _ := query.NewPath("label")
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Computed contracts"},
		Collections: []schema.Collection{
			{ID: "categories", Slug: "categories", Fields: []schema.Field{
				{ID: "categories-posts", Name: "posts", Path: joinFieldPath, Type: schema.FieldTypeJoin, Category: schema.FieldCategoryPresentation, Join: &schema.JoinField{CollectionID: "posts", CollectionSlug: "posts", On: joinPath, Limit: 10}},
				{ID: "categories-label", Name: "label", Path: virtualPath, Type: schema.FieldTypeVirtual, Category: schema.FieldCategoryPresentation, Virtual: &schema.VirtualField{ValueType: schema.ValueTypeString}},
			}},
			{ID: "posts", Slug: "posts"},
		},
	})
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, expected := range []string{`"posts"?: Array<Posts>;`, `"label"?: string | null;`, "export interface CategoriesCreate {\n}\n", "export interface CategoriesUpdate {\n}\n"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated client missing %q:\n%s", expected, text)
		}
	}
	selectStart := strings.Index(text, "export interface CategoriesSelect {")
	if selectStart < 0 {
		t.Fatalf("generated client is missing CategoriesSelect:\n%s", text)
	}
	selectEnd := strings.Index(text[selectStart:], "\n}\n")
	if selectEnd < 0 {
		t.Fatalf("generated CategoriesSelect is unterminated:\n%s", text[selectStart:])
	}
	selectSection := text[selectStart : selectStart+selectEnd]
	for _, expected := range []string{`"posts"?: boolean;`, `"label"?: boolean;`} {
		if !strings.Contains(selectSection, expected) {
			t.Fatalf("generated output selection missing %q:\n%s", expected, selectSection)
		}
	}
}

func TestGeneratedClientModelsServerOwnedUploadsDraftsAndValidationPaths(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Precision contracts",
		Collections: []ridu.Collection{
			{Slug: "media", Upload: true, Fields: field.Fields{field.Text("alt")}},
			{
				Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields: field.Fields{field.Upload("cover", "media"), field.Array("sections", field.Fields{field.MultiSelect("roles", "author", "editor")}), field.Blocks("layout", field.Block{Slug: "quote", Fields: field.Fields{field.Relationship("reviewer", "media")}})},
			},
			{Slug: "history", Versions: true, Fields: field.Fields{field.Text("event")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	section := func(name string) string {
		t.Helper()
		prefix := "export interface " + name + " {"
		start := strings.Index(text, prefix)
		if start == -1 {
			t.Fatalf("generated client has no %s", name)
		}
		end := strings.Index(text[start:], "\n}\n")
		if end == -1 {
			t.Fatalf("generated %s is unterminated", name)
		}
		return text[start : start+end+3]
	}
	mediaOutput := section("Media")
	mediaCreate := section("MediaCreate")
	mediaUpdate := section("MediaUpdate")
	for _, metadata := range []string{`"filename"?: string;`, `"mimeType"?: string;`, `"filesize"?: number;`, `"url"?: string;`} {
		if !strings.Contains(mediaOutput, metadata) {
			t.Fatalf("upload output missing server metadata %q:\n%s", metadata, mediaOutput)
		}
	}
	for _, metadata := range []string{`"filename"`, `"mimeType"`, `"filesize"`, `"url"`, `"objectKey"`} {
		if strings.Contains(mediaCreate, metadata) || strings.Contains(mediaUpdate, metadata) {
			t.Fatalf("server-owned upload metadata %s leaked into create/update:\n%s\n%s", metadata, mediaCreate, mediaUpdate)
		}
	}
	if !strings.Contains(section("PostsCreate"), `"cover"?: ID | null;`) {
		t.Fatalf("authored upload reference was removed from create input:\n%s", section("PostsCreate"))
	}
	for _, expected := range []string{
		`_status: "draft" | "published";`,
		`drafts: true;`,
		`export interface History {`,
		`_status: "published";`,
		`drafts: false;`,
		"`sections.${number}.roles.${number}`",
		"`layout.${number}.blockType`",
		"`layout.${number}.reviewer`",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated precision contract missing %q:\n%s", expected, text)
		}
	}
}

func clientFixtureConfig() ridu.Config {
	return ridu.Config{
		Name: "Generated client fixture",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{
				Slug: "authors", Versions: true, Trash: true,
				Fields: field.Fields{field.Text("name").Required().Localized(), field.Text("bio"), field.Virtual("displayName", field.ValueString,

					func(operation.Context) (operation.Value[store.Value],

						error) {
						return operation.Present(store.String("Ada")), nil
					}), field.Join("posts", "posts", "author")},
			},
			{
				Slug: "history", Versions: true,
				Fields: field.Fields{field.Text("event").Required()},
			},
			{
				Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields: field.Fields{field.Text("title").Required().Localized(), field.Select("status", "draft", "published").Default("draft"), field.Relationship("author", "authors").Localized(), field.Group("seo", field.Fields{field.Text("description"), field.Relationship("reviewer", "authors")}), field.Array("sections", field.Fields{field.Relationship("reviewer", "authors")}), field.Blocks("layout", field.Block{Slug: "quote", Fields: field.Fields{field.Relationship("source", "authors")}}), field.Group("localeNamed", field.Fields{field.Relationship("en", "authors")}), field.PolymorphicRelationship("subject", "authors", "posts")},
			},
		},
	}
}
