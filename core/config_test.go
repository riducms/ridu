package core_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestUploadFileSizeHasAProductionMemoryCeiling(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{Name: "Upload ceiling", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: (256 << 20) + 1},
		Fields:       field.Fields{field.Text("alt")},
	}}})
	var validation *schema.ValidationError
	if !errors.As(err, &validation) || !strings.Contains(err.Error(), "256 MiB") {
		t.Fatalf("oversized upload configuration error = %v", err)
	}
}

func TestAuthSessionConfigurationIsCanonical(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{Name: "Auth", Admin: ridu.AdminConfig{User: "users"}, Collections: []ridu.Collection{{
		Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{SessionDuration: 2 * time.Hour},
		Fields: field.Fields{field.Text("email").Required().Unique()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	auth := manifest.Snapshot().Collections[0].Auth
	if auth == nil || auth.IdentityField != "email" || auth.SessionDurationSeconds != 7200 {
		t.Fatalf("auth settings = %#v", auth)
	}
	admin := manifest.Snapshot().Application.Admin
	if admin == nil || admin.UserCollectionID != "users" || admin.UserCollectionSlug != "users" {
		t.Fatalf("admin settings = %#v", admin)
	}
}

func TestTypedFieldConditionsResolveWithDocumentAndSiblingScopes(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Conditional authoring",
		Collections: []ridu.Collection{{
			Slug: "questions",
			Fields: field.Fields{
				field.Checkbox("archived"), field.Array("answers", field.Fields{field.Select("kind", "lesson", "question", "note"), field.Text("copy").Admin(field.Admin{VisibleWhen: field.All(
					field.OneOf(field.Sibling("kind"), "lesson", "question"),
					field.Not(field.Equal(field.Root("archived"), true)),
				)})}),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	condition := manifest.Snapshot().Collections[0].Fields[1].Nested.ResolvedFields()[1].Admin.Condition
	if condition == nil || condition.Kind != schema.FieldConditionKindAll || len(condition.Conditions) != 2 {
		t.Fatalf("resolved condition = %#v", condition)
	}
	sibling := condition.Conditions[0].Predicate
	if sibling == nil || sibling.Scope != schema.FieldConditionSibling || sibling.Path.String() != "kind" || sibling.Operator != schema.FieldConditionOneOf || len(sibling.Values) != 2 || sibling.Values[0].Type != schema.ValueTypeString {
		t.Fatalf("resolved sibling predicate = %#v", sibling)
	}
	negated := condition.Conditions[1]
	if negated.Kind != schema.FieldConditionKindNot || len(negated.Conditions) != 1 || negated.Conditions[0].Predicate == nil || negated.Conditions[0].Predicate.Scope != schema.FieldConditionDocument || negated.Conditions[0].Predicate.Values[0] != (schema.ScalarLiteral{Type: schema.ValueTypeBoolean, Value: "true"}) {
		t.Fatalf("resolved negated predicate = %#v", negated)
	}
}

func TestTypedFieldConditionsRejectUnknownRepeatedAndMismatchedPaths(t *testing.T) {
	tests := []struct {
		name   string
		fields field.Fields
		code   string
		path   string
	}{
		{
			name:   "unknown document field",
			fields: field.Fields{field.Text("title"), field.Text("summary").Admin(field.Admin{VisibleWhen: field.Equal(field.Root("missing"), "yes")})},
			code:   "invalid_field_condition_path", path: "collections[0].fields[1].admin.visibleWhen.reference.path",
		},
		{
			name:   "unknown row sibling",
			fields: field.Fields{field.Array("answers", field.Fields{field.Text("kind"), field.Text("copy").Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("missing"), "yes")})})},
			code:   "invalid_field_condition_path", path: "collections[0].fields[0].fields[1].admin.visibleWhen.reference.path",
		},
		{
			name:   "document path crosses repeated rows",
			fields: field.Fields{field.Array("answers", field.Fields{field.Text("kind")}), field.Text("summary").Admin(field.Admin{VisibleWhen: field.Equal(field.Root("answers.kind"), "lesson")})},
			code:   "invalid_field_condition_path", path: "collections[0].fields[1].admin.visibleWhen.reference.path",
		},
		{
			name:   "operand type differs from field",
			fields: field.Fields{field.Number("score"), field.Text("summary").Admin(field.Admin{VisibleWhen: field.Equal(field.Root("score"), "three")})},
			code:   "invalid_field_condition_type", path: "collections[0].fields[1].admin.visibleWhen.values[0]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(ridu.Config{
				Name:        "Invalid conditions",
				Collections: []ridu.Collection{{Slug: "questions", Fields: test.fields}},
			})
			var validationError *schema.ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
			}
			if !slices.ContainsFunc(validationError.Issues, func(issue schema.Issue) bool {
				return issue.Code == test.code && issue.Path == test.path
			}) {
				t.Fatalf("issues = %#v, want %s at %s", validationError.Issues, test.code, test.path)
			}
		})
	}
}

func TestTypedSiblingConditionTraversesOnlyNonRepeatedGroupsInItsCurrentRow(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Nested condition",
		Collections: []ridu.Collection{{
			Slug:   "questions",
			Fields: field.Fields{field.Array("answers", field.Fields{field.Group("settings", field.Fields{field.Select("kind", "lesson", "question")}), field.Text("copy").Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("settings.kind"), "lesson")})})},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdminUserCollectionValidation(t *testing.T) {
	authCollection := ridu.Collection{
		Slug: "users", Auth: true,
		Fields: field.Fields{field.Text("email").Required().Unique()},
	}
	tests := []struct {
		name   string
		config ridu.Config
		code   string
	}{
		{
			name:   "missing",
			config: ridu.Config{Name: "Admin", Collections: []ridu.Collection{authCollection}},
			code:   "missing_admin_user",
		},
		{
			name:   "unknown",
			config: ridu.Config{Name: "Admin", Admin: ridu.AdminConfig{User: "staff"}, Collections: []ridu.Collection{authCollection}},
			code:   "unknown_admin_user",
		},
		{
			name: "not auth enabled",
			config: ridu.Config{Name: "Admin", Admin: ridu.AdminConfig{User: "posts"}, Collections: []ridu.Collection{
				authCollection,
				{Slug: "posts", Fields: field.Fields{field.Text("title")}},
			}},
			code: "invalid_admin_user",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(test.config)
			var validationError *schema.ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
			}
			found := false
			for _, issue := range validationError.Issues {
				if issue.Code == test.code && issue.Path == "admin.user" {
					found = true
				}
			}
			if !found {
				t.Fatalf("issues = %#v, want %s at admin.user", validationError.Issues, test.code)
			}
		})
	}
}

func TestAdminUserCollectionCanSelectAmongAuthCollections(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name:  "Admin",
		Admin: ridu.AdminConfig{User: "staff"},
		Collections: []ridu.Collection{
			{Slug: "customers", Auth: true, Fields: field.Fields{field.Text("email").Required().Unique()}},
			{Slug: "staff", Auth: true, Fields: field.Fields{field.Text("email").Required().Unique()}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	admin := manifest.Snapshot().Application.Admin
	if admin == nil || admin.UserCollectionID != "staff" || admin.UserCollectionSlug != "staff" {
		t.Fatalf("admin settings = %#v", admin)
	}
}

func TestAdminLocalizationConfigurationIsCanonicalAndIndependentFromContentLocales(t *testing.T) {
	config := ridu.Config{
		Name: "Translated admin",
		Admin: ridu.AdminConfig{Localization: ridu.AdminLocalizationConfig{
			DefaultLanguage: "fr",
			Languages: []ridu.AdminLanguage{
				{Code: "en-us", Label: " English ", LabelTranslations: map[string]string{"fr": "Anglais"}},
				{Code: "fr", Label: " Français "},
				{Code: "ar", Label: " العربية ", RTL: true},
			},
			DefaultTimeZone: "Europe/London",
			TimeZones: []ridu.AdminTimeZone{
				{ID: "UTC", Label: " UTC "},
				{ID: "Europe/London", Label: " London ", LabelTranslations: map[string]string{"fr": "Londres"}},
				{ID: "+05:30", Label: " India offset "},
			},
		}},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	settings := manifest.Snapshot().Application.AdminLocalization
	if settings == nil || settings.DefaultLanguage != "fr" || settings.DefaultTimeZone != "Europe/London" || settings.Languages[0].Code != "en-US" {
		t.Fatalf("admin localization = %#v", settings)
	}
	if settings.Languages[0].Label != "English" || settings.Languages[0].LabelTranslations["fr"] != "Anglais" || !settings.Languages[2].RTL || settings.TimeZones[1].Label != "London" || settings.TimeZones[1].LabelTranslations["fr"] != "Londres" || settings.TimeZones[2].ID != "+05:30" {
		t.Fatalf("normalized admin localization = %#v", settings)
	}
	if manifest.Snapshot().Application.Localization != nil {
		t.Fatal("admin localization unexpectedly enabled content localization")
	}
	config.Admin.Localization.Languages[0].Label = "mutated"
	config.Admin.Localization.Languages[0].LabelTranslations["fr"] = "mutated"
	config.Admin.Localization.TimeZones[0].Label = "mutated"
	config.Admin.Localization.TimeZones[1].LabelTranslations["fr"] = "mutated"
	if got := manifest.Snapshot().Application.AdminLocalization; got.Languages[0].Label != "English" || got.Languages[0].LabelTranslations["fr"] != "Anglais" || got.TimeZones[0].Label != "UTC" || got.TimeZones[1].LabelTranslations["fr"] != "Londres" {
		t.Fatalf("manifest retained caller-owned admin localization slices: %#v", got)
	}
}

func TestAdminLocalizationRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name         string
		localization ridu.AdminLocalizationConfig
		code         string
		path         string
	}{
		{"missing languages", ridu.AdminLocalizationConfig{DefaultLanguage: "en"}, "missing_admin_languages", "admin.localization.languages"},
		{"unknown default", ridu.AdminLocalizationConfig{DefaultLanguage: "fr", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}}}, "unknown_default_admin_language", "admin.localization.defaultLanguage"},
		{"duplicate language", ridu.AdminLocalizationConfig{DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}, {Code: "en", Label: "Duplicate"}}}, "duplicate_admin_language_code", "admin.localization.languages[1].code"},
		{"unsafe timezone", ridu.AdminLocalizationConfig{DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}}, TimeZones: []ridu.AdminTimeZone{{ID: "../London", Label: "Unsafe"}}}, "invalid_admin_timezone", "admin.localization.timeZones[0].id"},
		{"unknown timezone", ridu.AdminLocalizationConfig{DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}}, TimeZones: []ridu.AdminTimeZone{{ID: "Mars/Olympus", Label: "Unknown"}}}, "invalid_admin_timezone", "admin.localization.timeZones[0].id"},
		{"browser-unsafe timezone alias", ridu.AdminLocalizationConfig{DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}}, TimeZones: []ridu.AdminTimeZone{{ID: "Factory", Label: "Factory"}}}, "invalid_admin_timezone", "admin.localization.timeZones[0].id"},
		{"invalid offset", ridu.AdminLocalizationConfig{DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}}, TimeZones: []ridu.AdminTimeZone{{ID: "+25:00", Label: "Invalid"}}}, "invalid_admin_timezone", "admin.localization.timeZones[0].id"},
		{"unknown default timezone", ridu.AdminLocalizationConfig{DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}}, DefaultTimeZone: "Europe/Paris", TimeZones: []ridu.AdminTimeZone{{ID: "UTC", Label: "UTC"}}}, "unknown_default_admin_timezone", "admin.localization.defaultTimeZone"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(ridu.Config{
				Name: "Translated admin", Admin: ridu.AdminConfig{Localization: test.localization},
				Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
			})
			var validationError *schema.ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
			}
			for _, issue := range validationError.Issues {
				if issue.Code == test.code && issue.Path == test.path {
					return
				}
			}
			t.Fatalf("issues = %#v, want %s at %s", validationError.Issues, test.code, test.path)
		})
	}
}

func TestLocalizationConfigurationIsCanonical(t *testing.T) {
	config := ridu.Config{
		Name: "Localized",
		Localization: ridu.LocalizationConfig{
			DefaultLocale: "en",
			Locales: []ridu.Locale{
				{Code: "en", Label: " English "},
				{Code: "pt-BR", Label: "Português", FallbackLocales: []schema.LocaleCode{"en"}},
				{Code: "ar", Label: "العربية", RTL: true, FallbackLocales: []schema.LocaleCode{"en"}},
			},
		},
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title").Required().Localized(), field.Group("seo", field.Fields{field.Text("description").Localized()})},
		}},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	localization := snapshot.Application.Localization
	if localization == nil || localization.DefaultLocale != "en" || !localization.Fallback {
		t.Fatalf("localization = %#v", localization)
	}
	if len(localization.Locales) != 3 || localization.Locales[0].Label != "English" || !localization.Locales[2].RTL {
		t.Fatalf("locales = %#v", localization.Locales)
	}
	if got := localization.Locales[1].FallbackLocales; len(got) != 1 || got[0] != "en" {
		t.Fatalf("fallback locales = %#v", got)
	}
	if !snapshot.Collections[0].Fields[0].Localized || !snapshot.Collections[0].Fields[1].Nested.ResolvedFields()[0].Localized {
		t.Fatalf("localized field metadata was not preserved: %#v", snapshot.Collections[0].Fields)
	}

	config.Localization.Locales[1].FallbackLocales[0] = "ar"
	if got := manifest.Snapshot().Application.Localization.Locales[1].FallbackLocales[0]; got != "en" {
		t.Fatalf("manifest retained caller-owned fallback slice: %q", got)
	}
}

func TestLocalizationConfigurationRejectsUnsafeGraphs(t *testing.T) {
	tests := []struct {
		name         string
		localization ridu.LocalizationConfig
		code         string
		path         string
	}{
		{"missing locales", ridu.LocalizationConfig{DefaultLocale: "en"}, "missing_locales", "localization.locales"},
		{"reserved code", ridu.LocalizationConfig{DefaultLocale: "all", Locales: []ridu.Locale{{Code: "all", Label: "All"}}}, "invalid_locale_code", "localization.locales[0].code"},
		{"duplicate code", ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "en", Label: "Duplicate"}}}, "duplicate_locale_code", "localization.locales[1].code"},
		{"unknown default", ridu.LocalizationConfig{DefaultLocale: "fr", Locales: []ridu.Locale{{Code: "en", Label: "English"}}}, "unknown_default_locale", "localization.defaultLocale"},
		{"unknown fallback", ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English", FallbackLocales: []schema.LocaleCode{"fr"}}}}, "unknown_fallback_locale", "localization.locales[0].fallbackLocales[0]"},
		{"cycle", ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English", FallbackLocales: []schema.LocaleCode{"fr"}}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}}, "fallback_locale_cycle", "localization.locales"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(ridu.Config{Name: "Localized", Localization: test.localization, Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title").Localized()}}}})
			var validationError *schema.ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
			}
			for _, issue := range validationError.Issues {
				if issue.Code == test.code && issue.Path == test.path {
					return
				}
			}
			t.Fatalf("issues = %#v, want %s at %s", validationError.Issues, test.code, test.path)
		})
	}
}

func TestLocalizedFieldRequiresApplicationLocalization(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{Name: "Localized", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title").Localized()}}}})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
	}
	for _, issue := range validationError.Issues {
		if issue.Code == "missing_localization_config" && issue.Path == "collections[0].fields[0].localized" {
			return
		}
	}
	t.Fatalf("issues = %#v", validationError.Issues)
}

func TestLivePreviewConfigurationIsCanonical(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Preview",
		Collections: []ridu.Collection{{
			Slug: "posts",
			Admin: ridu.CollectionAdmin{LivePreview: ridu.LivePreviewConfig{
				URL:         " /preview/{collection}/{id}?slug={field:slug} ",
				Breakpoints: []ridu.PreviewBreakpoint{{Name: "mobile", Width: 375, Height: 667}},
			}},
			Fields: field.Fields{field.Text("slug")},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	preview := manifest.Snapshot().Collections[0].Admin.LivePreview
	if preview == nil || preview.URL != "/preview/{collection}/{id}?slug={field:slug}" {
		t.Fatalf("live preview = %#v", preview)
	}
	if len(preview.Breakpoints) != 1 || preview.Breakpoints[0].Label != "Mobile" {
		t.Fatalf("breakpoints = %#v", preview.Breakpoints)
	}
}

func TestLivePreviewConfigurationRejectsUnknownFields(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Preview",
		Collections: []ridu.Collection{{
			Slug: "posts",
			Admin: ridu.CollectionAdmin{LivePreview: ridu.LivePreviewConfig{
				URL: "/preview/{field:missing}",
			}},
			Fields: field.Fields{field.Text("slug")},
		}},
	})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
	}
	for _, issue := range validationError.Issues {
		if issue.Code == "unknown_live_preview_field" && issue.Path == "collections[0].admin.livePreview.url" {
			return
		}
	}
	t.Fatalf("issues = %#v", validationError.Issues)
}

func TestManifestGoldenFixtures(t *testing.T) {
	tests := []struct {
		name   string
		config ridu.Config
	}{
		{"valid", validConfig()},
		{"nested", nestedConfig()},
		{"relationship", relationshipConfig()},
		{"plugin", pluginConfig()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest, err := ridu.Resolve(test.config)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			actual, err := manifest.Bytes()
			if err != nil {
				t.Fatalf("Manifest bytes: %v", err)
			}
			assertGolden(t, test.name+".json", actual)
		})
	}
}

func TestInvalidConfigGoldenFixture(t *testing.T) {
	_, err := ridu.Resolve(invalidConfig())
	if err == nil {
		t.Fatal("Resolve(invalid) succeeded, want validation error")
	}
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve(invalid) error = %T, want *schema.ValidationError", err)
	}
	actual, err := json.MarshalIndent(validationError, "", "  ")
	if err != nil {
		t.Fatalf("Marshal validation error: %v", err)
	}
	actual = append(actual, '\n')
	assertGolden(t, "invalid.json", actual)
}

func TestResolutionIsByteDeterministic(t *testing.T) {
	first, err := ridu.Resolve(relationshipConfig())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ridu.Resolve(relationshipConfig())
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := first.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := second.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("identical config produced different bytes:\n%s\n%s", firstBytes, secondBytes)
	}
}

func TestStableIdentitiesDefaultFromCollectionAndFieldPaths(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Derived identities",
		Collections: []ridu.Collection{{
			Slug:   "blog-posts",
			Fields: field.Fields{field.Text("title").Required(), field.Group("seo", field.Fields{field.Text("metaTitle")}), field.Select("status", "draft", "in_review")},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	if collection.ID != "blog-posts" {
		t.Fatalf("collection ID = %q, want blog-posts", collection.ID)
	}
	if collection.Fields[0].ID != "blog-posts-title" {
		t.Fatalf("title ID = %q, want blog-posts-title", collection.Fields[0].ID)
	}
	if collection.Fields[1].ID != "blog-posts-seo" || collection.Fields[1].Nested.ResolvedFields()[0].ID != "blog-posts-seo-meta-title" {
		t.Fatalf("nested derived IDs = %q / %q", collection.Fields[1].ID, collection.Fields[1].Nested.ResolvedFields()[0].ID)
	}
	if got := collection.Fields[2].Select.Options[1].Label; got != "In review" {
		t.Fatalf("derived option label = %q, want In review", got)
	}
}

func TestTabsResolveNamedDataAndUnnamedPresentationShapes(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Tabs",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Tabs(field.Fields{field.UnnamedTab("Content", field.Fields{field.Text("title"), field.Textarea("summary")}), field.NamedTab("seo", "SEO", field.Fields{field.Text("title"), field.Textarea("description")})})},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields := manifest.Snapshot().Collections[0].Fields
	if len(fields) != 3 || fields[0].Path.String() != "title" || fields[1].Path.String() != "summary" {
		t.Fatalf("unnamed tab fields = %#v, want flat title and summary", fields)
	}
	if fields[0].Admin.Tab != "Content" || fields[1].Admin.Tab != "Content" {
		t.Fatalf("unnamed tab labels = %q / %q, want Content", fields[0].Admin.Tab, fields[1].Admin.Tab)
	}
	if fields[0].Admin.TabGroup == nil || fields[0].Admin.TabGroup.ID == "" || fields[1].Admin.TabGroup == nil || fields[1].Admin.TabGroup.ID != fields[0].Admin.TabGroup.ID {
		t.Fatalf("unnamed tab group = %#v / %#v, want one shared local group", fields[0].Admin.TabGroup, fields[1].Admin.TabGroup)
	}
	seo := fields[2]
	if seo.Name != "seo" || seo.Path.String() != "seo" || seo.Type != schema.FieldTypeGroup || seo.Admin.Tab != "SEO" || !seo.Admin.NamedTab || seo.Nested == nil {
		t.Fatalf("named tab field = %#v, want data-bearing seo group", seo)
	}
	if seo.Admin.TabGroup == nil || seo.Admin.TabGroup.ID != fields[0].Admin.TabGroup.ID {
		t.Fatalf("named tab group = %#v, want %#v", seo.Admin.TabGroup, fields[0].Admin.TabGroup)
	}
	if got := seo.Nested.ResolvedFields()[0].Path.String(); got != "seo.title" {
		t.Fatalf("named tab child path = %q, want seo.title", got)
	}
}

func TestDirectTabMetadataRetainsOneLocalGroup(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Direct tabs",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title"), field.Number("score").Admin(field.Admin{Tab: "Scoring"}), field.Checkbox("published").Admin(field.Admin{Tab: "Scoring"})},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields := manifest.Snapshot().Collections[0].Fields
	if fields[0].Admin.TabGroup != nil {
		t.Fatalf("ordinary field tab group = %#v, want nil", fields[0].Admin.TabGroup)
	}
	if fields[1].Admin.TabGroup == nil || fields[2].Admin.TabGroup == nil || fields[1].Admin.TabGroup.ID != fields[2].Admin.TabGroup.ID {
		t.Fatalf("direct tab groups = %#v / %#v, want one shared local group", fields[1].Admin.TabGroup, fields[2].Admin.TabGroup)
	}
}

func TestFieldRenameChangesDerivedIdentityForMigrationMatching(t *testing.T) {
	resolve := func(name string) schema.Manifest {
		manifest, err := ridu.Resolve(ridu.Config{Name: "Rename", Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text(name)},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before := resolve("title").Snapshot().Collections[0].Fields[0]
	after := resolve("headline").Snapshot().Collections[0].Fields[0]
	if before.ID != "posts-title" || after.ID != "posts-headline" || before.Name == after.Name {
		t.Fatalf("derived rename identities before=%#v after=%#v", before, after)
	}
}

func TestDerivedFieldIdentityCollisionsAreRejected(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{Name: "Collision", Collections: []ridu.Collection{{
		Slug:   "posts",
		Fields: field.Fields{field.Text("metaTitle"), field.Text("meta_title")},
	}}})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
	}
	found := false
	for _, issue := range validationError.Issues {
		if issue.Code == "duplicate_field_id" && issue.Path == "collections[0].fields[1].name" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %#v, want derived identity collision", validationError.Issues)
	}
}

func TestRowGroupsFlatManifestFieldsWithoutChangingDocumentPaths(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Editorial rows",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title"), field.Row(field.Fields{field.Relationship("author", "users").Admin(field.Admin{Columns: 6}), field.Select("status", "draft", "published").Admin(field.Admin{Columns: 6})})},
		}, {
			Slug:   "users",
			Fields: field.Fields{field.Text("name")},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields := manifest.Snapshot().Collections[0].Fields
	if len(fields) != 3 {
		t.Fatalf("manifest fields = %d, want three flat data fields", len(fields))
	}
	if fields[0].Admin.Row != nil {
		t.Fatal("unwrapped title unexpectedly belongs to a row")
	}
	if fields[1].Name != "author" || fields[1].Path.String() != "author" || fields[1].ID != "posts-author" {
		t.Fatalf("author field = %#v, want ordinary top-level identity and path", fields[1])
	}
	if fields[1].Admin.Row == nil || fields[2].Admin.Row == nil || fields[1].Admin.Row.ID != "posts-author" || fields[2].Admin.Row.ID != fields[1].Admin.Row.ID {
		t.Fatalf("row metadata = %#v / %#v, want shared posts-author identity", fields[1].Admin.Row, fields[2].Admin.Row)
	}
	encoded, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"type": "row"`)) {
		t.Fatalf("presentation row leaked into data fields:\n%s", encoded)
	}
}

func TestRowRequiresChildren(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{Name: "Invalid row", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Row(field.Fields{})},
	}}})
	if err == nil || !strings.Contains(err.Error(), "collections[0].fields[0].fields") || !strings.Contains(err.Error(), "missing_row_fields") {
		t.Fatalf("Resolve empty row error = %v, want path-aware missing_row_fields", err)
	}
}

func TestTabsRequireSectionsLabelsAndChildren(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{Name: "Invalid tabs", Collections: []ridu.Collection{{
		Slug:   "posts",
		Fields: field.Fields{field.Tabs(field.Fields{}), field.Tabs(field.Fields{field.UnnamedTab("", field.Fields{field.Text("title")})}), field.Tabs(field.Fields{field.NamedTab("seo", "SEO", field.Fields{})})},
	}}})
	if err == nil {
		t.Fatal("Resolve invalid tabs succeeded")
	}
	for _, code := range []string{"missing_tabs", "missing_tab_label", "missing_tab_fields"} {
		if !strings.Contains(err.Error(), code) {
			t.Fatalf("Resolve invalid tabs error = %v, want %s", err, code)
		}
	}
}

func TestPluginTransformsRunInDeclarationOrderWithoutMutatingInput(t *testing.T) {
	applicationConfig := ridu.Config{
		Name: "Plugins",
		Collections: []ridu.Collection{{
			Slug: "posts",
		}},
	}
	first := appendFieldPlugin{key: "first", field: field.Text("first")}
	second := appendFieldPlugin{key: "second", field: field.Text("second")}
	applicationConfig.Plugins = []ridu.Plugin{first, second}

	manifest, err := ridu.Resolve(applicationConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(applicationConfig.Collections[0].Fields) != 0 {
		t.Fatal("plugin transform mutated caller-owned config")
	}
	snapshot := manifest.Snapshot()
	if got := snapshot.Collections[0].Fields[0].Name; got != "first" {
		t.Fatalf("first transformed field = %q, want first", got)
	}
	if got := snapshot.Collections[0].Fields[1].Name; got != "second" {
		t.Fatalf("second transformed field = %q, want second", got)
	}
	if got := snapshot.Plugins[0].Key; got != "first" {
		t.Fatalf("first manifest plugin = %q, want first", got)
	}
}

func TestZeroValueFieldFailsNameValidation(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Invalid field",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.TextField{}},
		}},
	})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
	}
	if got := validationError.Issues[0].Path; got != "collections[0].fields[0].name" {
		t.Fatalf("first issue path = %q, want field name path", got)
	}
}

func TestFrameworkDocumentFieldNamesAreReserved(t *testing.T) {
	for _, name := range []string{"id", "createdAt", "updatedAt", "deletedAt", "and", "or", "not"} {
		t.Run(name, func(t *testing.T) {
			_, err := ridu.Resolve(ridu.Config{
				Name: "Reserved field",
				Collections: []ridu.Collection{{
					Slug:   "posts",
					Fields: field.Fields{field.Text(name)},
				}},
			})
			var validationError *schema.ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
			}
			if got := validationError.Issues[0].Code; got != "reserved_field_name" {
				t.Fatalf("first issue code = %q, want reserved_field_name", got)
			}
		})
	}
}

func TestWhereControlNamesRemainAvailableBelowTheResourceRoot(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Nested query control names",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Group("metadata", field.Fields{field.Text("and"), field.Text("or"), field.Text("not")})},
		}},
	})
	if err != nil {
		t.Fatalf("Resolve nested query control names: %v", err)
	}
}

func TestBlockDiscriminatorFieldNameIsReserved(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Reserved block discriminator",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Blocks("content", field.Block{Slug: "hero", Fields: field.Fields{field.Row(field.Fields{field.Text("blockType")})}})},
		}},
	})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
	}
	for _, issue := range validationError.Issues {
		if issue.Code == "reserved_field_name" && strings.Contains(issue.Path, "blocks[0].fields") {
			return
		}
	}
	t.Fatalf("Resolve issues = %#v, want reserved blockType discriminator issue", validationError.Issues)
}

func TestTypedNilPluginIsAValidationError(t *testing.T) {
	var plugin *pointerPlugin
	_, err := ridu.Resolve(ridu.Config{
		Name:        "Typed nil plugin",
		Collections: []ridu.Collection{{Slug: "posts"}},
		Plugins:     []ridu.Plugin{plugin},
	})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
	}
	if got := validationError.Issues[0].Path; got != "plugins[0]" {
		t.Fatalf("plugin issue path = %q, want plugins[0]", got)
	}
}

func TestAdminPluginMetadataIsValidatedAndResolved(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name:        "Paired plugin",
		Collections: []ridu.Collection{{Slug: "posts"}},
		Plugins: []ridu.Plugin{pairedPlugin{key: "color", admin: ridu.AdminPluginMetadata{
			Package:        "@example/ridu-color-admin",
			Export:         "colorAdminPlugin",
			APIVersion:     ridu.AdminPluginAPIVersion,
			PairingVersion: 3,
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plugin := manifest.Snapshot().Plugins[0]
	if plugin.Admin == nil || plugin.Admin.Package != "@example/ridu-color-admin" || plugin.Admin.Export != "colorAdminPlugin" || plugin.Admin.APIVersion != ridu.AdminPluginAPIVersion || plugin.Admin.PairingVersion != 3 {
		t.Fatalf("resolved admin plugin = %#v", plugin.Admin)
	}
}

func TestRowLabelComponentsResolveForArraysAndBlocks(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Composite row labels",
		Plugins: []ridu.Plugin{pairedPlugin{key: "curriculum", admin: ridu.AdminPluginMetadata{
			Package:        "@example/curriculum-admin",
			Export:         "curriculumAdminPlugin",
			APIVersion:     ridu.AdminPluginAPIVersion,
			PairingVersion: 1,
		}}},
		Collections: []ridu.Collection{{
			Slug:   "questions",
			Fields: field.Fields{field.Array("options", field.Fields{field.Text("optionKey"), field.Text("label")}).Admin(field.Admin{RowLabelPath: "label", RowLabel: field.PluginComponent("curriculum", "questionOption", store.Object(store.Values{"key": store.String("optionKey"), "label": store.String("label")}))}), field.Blocks("content", field.Block{Slug: "lesson", Fields: field.Fields{field.Number("orderIndex")}}).Admin(field.Admin{RowLabel: field.PluginComponent("curriculum", "blockSummary", store.Object(store.Values{"type": store.String("blockType"), "order": store.String("orderIndex")}))})},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields := manifest.Snapshot().Collections[0].Fields
	arrayComponent := fields[0].Nested.RowLabelComponent
	if arrayComponent == nil || arrayComponent.Plugin != "curriculum" || arrayComponent.Component != "questionOption" || string(arrayComponent.Config) != `{"key":"optionKey","label":"label"}` || fields[0].Nested.RowLabel != "label" {
		t.Fatalf("array row label metadata = %#v", fields[0].Nested)
	}
	blocksComponent := fields[1].Nested.RowLabelComponent
	if blocksComponent == nil || blocksComponent.Plugin != "curriculum" || blocksComponent.Component != "blockSummary" || string(blocksComponent.Config) != `{"order":"orderIndex","type":"blockType"}` || len(fields[1].Nested.ResolvedFields()) != 0 {
		t.Fatalf("blocks row label metadata = %#v", fields[1].Nested)
	}
}

func TestRowLabelComponentRequiresPairedAdminPlugin(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name:        "Missing row label plugin",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Array("items", field.Fields{field.Text("label")}).Admin(field.Admin{RowLabel: field.PluginComponent("missing", "summary", store.Object(store.Values{}))})}}},
	})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
	}
	if !slices.ContainsFunc(validationError.Issues, func(issue schema.Issue) bool {
		return issue.Code == "missing_row_label_component_plugin" && issue.Path == "collections[0].fields[0].admin.rowLabel.pluginKey"
	}) {
		t.Fatalf("row label component issues = %#v", validationError.Issues)
	}
}

func TestInvalidAdminPluginMetadataReportsActionablePaths(t *testing.T) {
	tests := []struct {
		name     string
		metadata ridu.AdminPluginMetadata
		code     string
		path     string
	}{
		{name: "package", metadata: ridu.AdminPluginMetadata{Package: "../admin", Export: "colorAdminPlugin", APIVersion: ridu.AdminPluginAPIVersion, PairingVersion: 1}, code: "invalid_admin_plugin_package", path: "plugins[0].admin.package"},
		{name: "export", metadata: ridu.AdminPluginMetadata{Package: "@example/color-admin", Export: "color-plugin", APIVersion: ridu.AdminPluginAPIVersion, PairingVersion: 1}, code: "invalid_admin_plugin_export", path: "plugins[0].admin.export"},
		{name: "api", metadata: ridu.AdminPluginMetadata{Package: "@example/color-admin", Export: "colorAdminPlugin", APIVersion: 99, PairingVersion: 1}, code: "incompatible_admin_plugin_api", path: "plugins[0].admin.apiVersion"},
		{name: "pairing", metadata: ridu.AdminPluginMetadata{Package: "@example/color-admin", Export: "colorAdminPlugin", APIVersion: ridu.AdminPluginAPIVersion}, code: "invalid_admin_plugin_pairing_version", path: "plugins[0].admin.pairingVersion"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(ridu.Config{
				Name:        "Invalid paired plugin",
				Collections: []ridu.Collection{{Slug: "posts"}},
				Plugins:     []ridu.Plugin{pairedPlugin{key: "color", admin: test.metadata}},
			})
			var validationError *schema.ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
			}
			if validationError.Issues[0].Code != test.code || validationError.Issues[0].Path != test.path {
				t.Fatalf("first issue = %#v, want %s at %s", validationError.Issues[0], test.code, test.path)
			}
		})
	}
}

func TestPluginDescriptorCompatibilityIsValidated(t *testing.T) {
	tests := []struct {
		name       string
		descriptor ridu.PluginDescriptor
		code       string
	}{
		{name: "release range", descriptor: validDescriptor("audit", ridu.RiduCompatibility{Minimum: "1.0.0"}), code: "incompatible_ridu_plugin"},
		{name: "plugin API", descriptor: func() ridu.PluginDescriptor {
			value := validDescriptor("audit", ridu.RiduCompatibility{Minimum: "0.0.0-dev"})
			value.APIVersion = 2
			return value
		}(), code: "incompatible_plugin_api"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(ridu.Config{Name: "Plugin descriptor", Plugins: []ridu.Plugin{descriptorPlugin{key: "audit", descriptor: test.descriptor}}, Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}})
			var validationError *schema.ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Resolve error = %v", err)
			}
			found := false
			for _, issue := range validationError.Issues {
				found = found || issue.Code == test.code
			}
			if !found {
				t.Fatalf("issues = %#v, want code %s", validationError.Issues, test.code)
			}
		})
	}
}

func validDescriptor(key string, compatibility ridu.RiduCompatibility) ridu.PluginDescriptor {
	return ridu.PluginDescriptor{Version: "1.0.0", GoPackage: "example.com/plugins/" + key, APIVersion: ridu.PluginAPIVersion, Ridu: compatibility}
}

func validConfig() ridu.Config {
	return ridu.Config{
		Name: "Editorial",
		Collections: []ridu.Collection{{
			Slug: "posts",
			Labels: ridu.CollectionLabels{
				Singular: "Post",
				Plural:   "Posts",
			},
			Fields: field.Fields{field.Text("title").Label("Title").Required().Unique(), field.Select("status", "draft", "published").Default("draft")},
		}},
	}
}

func nestedConfig() ridu.Config {
	return ridu.Config{
		Name: "Nested",
		Collections: []ridu.Collection{{
			Slug:   "pages",
			Fields: field.Fields{field.Group("seo", field.Fields{field.Text("metaTitle").Required(), field.Text("description")}).Label("Search metadata")},
		}},
	}
}

func relationshipConfig() ridu.Config {
	return ridu.Config{
		Name: "Relationships",
		Collections: []ridu.Collection{
			{
				Slug:   "authors",
				Fields: field.Fields{field.Text("name").Required()},
			},
			{
				Slug:   "posts",
				Fields: field.Fields{field.Text("title").Required(), field.Relationship("author", "authors").Required()},
			},
		},
	}
}

func TestNestedRelationshipOptionFiltersAreValidatedRecursively(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Nested relationship filters",
		Collections: []ridu.Collection{
			{Slug: "authors", Fields: field.Fields{field.Text("name")}},
			{Slug: "posts", Fields: field.Fields{field.Text("category"), field.Group("meta", field.Fields{field.Relationship("author", "authors").FilterOptionRules(field.OptionFilter("missing", field.FilterEquals, "category"))}), field.Array("rows", field.Fields{field.Relationship("author", "authors").FilterOptionRules(field.OptionFilter("name", field.FilterEquals, "missing"))}), field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Relationship("author", "authors").FilterOptionRules(field.OptionFilterFor("editors", "name", field.FilterEquals, "category"))}})}},
		},
	})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %v, want validation error", err)
	}
	want := map[string]string{
		"collections[1].fields[1].nested.fields[0].optionFilters[0].targetPath":              "invalid_relationship_filter_target",
		"collections[1].fields[2].nested.fields[0].optionFilters[0].sourcePath":              "invalid_relationship_filter_source",
		"collections[1].fields[3].blocks.types[0].fields[0].optionFilters[0].collectionSlug": "invalid_relationship_filter_collection",
	}
	for _, issue := range validationError.Issues {
		if code, exists := want[issue.Path]; exists && code == issue.Code {
			delete(want, issue.Path)
		}
	}
	if len(want) != 0 {
		t.Fatalf("issues = %#v, missing %#v", validationError.Issues, want)
	}
}

func TestUploadOptionFiltersResolveIntoTheSharedReferenceContract(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Upload filters",
		Collections: []ridu.Collection{
			{Slug: "media", Upload: true},
			{Slug: "posts", Fields: field.Fields{field.Text("assetType"), field.Text("assetName"), field.Upload("hero", "media").FilterOptionRules(field.OptionFilter("mimeType", field.FilterEquals, "assetType"), field.OptionFilter("filename", field.FilterLike, "assetName"))}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := manifest.Snapshot().Collections[1].Fields[2].Upload
	if upload == nil || len(upload.OptionFilters) != 2 || upload.OptionFilters[0].TargetPath.String() != "mimeType" || upload.OptionFilters[0].SourcePath.String() != "assetType" {
		t.Fatalf("resolved upload filter = %#v", upload)
	}
	if upload.OptionFilters[1].TargetPath.String() != "filename" || upload.OptionFilters[1].Operator != "like" {
		t.Fatalf("resolved upload option filters = %#v", upload.OptionFilters)
	}
}

func TestMultiSelectCardinalityAndDefaultsResolve(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Multi select",
		Collections: []ridu.Collection{{
			Slug:   "users",
			Fields: field.Fields{field.MultiSelect("roles", "admin", "editor").Default("admin").Required()},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	roles := manifest.Snapshot().Collections[0].Fields[0]
	if roles.Select == nil || !roles.Select.HasMany || !slices.Equal(roles.Select.DefaultValues, []string{"admin"}) {
		t.Fatalf("resolved roles select = %#v", roles.Select)
	}
}

func TestLiteralReferenceFiltersResolvePublishedStatus(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Published references",
		Collections: []ridu.Collection{
			{Slug: "lessons", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title")}},
			{Slug: "islands", Fields: field.Fields{field.Relationship("lesson",
				"lessons").FilterOptionRules(field.OptionFilterValue("_status", field.FilterEquals, "published")),
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	filter := manifest.Snapshot().Collections[1].Fields[0].Relationship.OptionFilters[0]
	if filter.SourcePath != nil || filter.Value == nil || filter.Value.Type != schema.ValueTypeString || filter.Value.Value != "published" {
		t.Fatalf("resolved literal filter = %#v", filter)
	}
}

func TestReferenceOptionFiltersRejectIncompatibleScalarTypes(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Reference filter types",
		Collections: []ridu.Collection{
			{Slug: "authors", Fields: field.Fields{field.Text("name"), field.Number("score"), field.Checkbox("active")}},
			{Slug: "media", Upload: true},
			{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Number("score"), field.Checkbox("active"), field.Relationships("tags", "authors"), field.Relationship("badLike", "authors").FilterOptionRules(field.OptionFilter("name", field.FilterLike, "score")), field.Relationship("badRange", "authors").FilterOptionRules(field.OptionFilter("score", field.FilterGreaterThan, "title")), field.Relationship("badBoolean", "authors").FilterOptionRules(field.OptionFilter("active", field.FilterEquals, "title")), field.Relationship("badManySource", "authors").FilterOptionRules(field.OptionFilter("name", field.FilterEquals, "tags")), field.Upload("badUpload", "media").FilterOptionRules(field.OptionFilter("mimeType", field.FilterEquals, "score"))}},
		},
	})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %v, want validation error", err)
	}
	want := map[string]bool{
		"collections[2].fields[4].optionFilters[0]": true,
		"collections[2].fields[5].optionFilters[0]": true,
		"collections[2].fields[6].optionFilters[0]": true,
		"collections[2].fields[7].optionFilters[0]": true,
		"collections[2].fields[8].optionFilters[0]": true,
	}
	for _, issue := range validationError.Issues {
		if issue.Code == "invalid_relationship_filter_type" {
			delete(want, issue.Path)
		}
	}
	if len(want) != 0 {
		t.Fatalf("issues = %#v, missing incompatible filter paths %#v", validationError.Issues, want)
	}
}

func TestReferenceOptionFiltersAcceptCompatibleScalarTypes(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Compatible reference filter types",
		Collections: []ridu.Collection{
			{Slug: "authors", Fields: field.Fields{field.Text("name"), field.Number("score"), field.Checkbox("active")}},
			{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Number("score"), field.Checkbox("active"), field.Relationship("byName", "authors").FilterOptionRules(field.OptionFilter("name", field.FilterLike, "title")), field.Relationship("byScore", "authors").FilterOptionRules(field.OptionFilter("score", field.FilterGreaterThanEqual, "score")), field.Relationship("byActive", "authors").FilterOptionRules(field.OptionFilter("active", field.FilterEquals, "active"))}},
		},
	})
	if err != nil {
		t.Fatalf("Resolve compatible reference filters: %v", err)
	}
}

func pluginConfig() ridu.Config {
	return ridu.Config{
		Name: "Plugin contribution",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title")},
		}},
		Plugins: []ridu.Plugin{
			appendFieldPlugin{
				key:   "seo-fields",
				field: field.Text("pluginNote").Label("Plugin note"),
			},
		},
	}
}

func TestMultiwordBlockSlugsProduceCanonicalFieldPaths(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{Name: "Blocks", Collections: []ridu.Collection{{
		Slug:   "pages",
		Fields: field.Fields{field.Blocks("layout", field.Block{Slug: "featured-post", Fields: field.Fields{field.Text("title").Required()}})},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	blockField := manifest.Snapshot().Collections[0].Fields[0].Blocks.ResolvedTypes()[0].ResolvedFields()[0]
	if got := blockField.Path.String(); got != "layout.featured-post.title" {
		t.Fatalf("block field path = %q", got)
	}
}

func TestAdminDisplayTranslationsResolveAcrossAuthoringSurfaces(t *testing.T) {
	config := ridu.Config{
		Name:             "Editorial",
		NameTranslations: map[string]string{"fr": "Éditorial"},
		Admin: ridu.AdminConfig{Localization: ridu.AdminLocalizationConfig{
			DefaultLanguage: "en",
			Languages:       []ridu.AdminLanguage{{Code: "en", Label: "English"}, {Code: "fr", Label: "Français"}},
		}},
		Collections: []ridu.Collection{{
			Slug: "posts",
			Labels: ridu.CollectionLabels{
				Singular: "Post", SingularTranslations: map[string]string{"fr": "Article"},
				Plural: "Posts", PluralTranslations: map[string]string{"fr": "Articles"},
			},
			Admin: ridu.CollectionAdmin{
				Group: "Content", GroupTranslations: map[string]string{"fr": "Contenu"},
				Description: "Editorial posts", DescriptionTranslations: map[string]string{"fr": "Articles éditoriaux"},
				LivePreview: ridu.LivePreviewConfig{
					URL:         "/preview/{id}",
					Breakpoints: []ridu.PreviewBreakpoint{{Name: "mobile", Label: "Mobile", LabelTranslations: map[string]string{"fr": "Téléphone"}, Width: 375, Height: 667}},
				},
			},
			Fields: field.Fields{field.Text("title").Label("Title").Admin(field.Admin{LabelTranslations: map[string]string{"fr": "Titre"}, Description: "Public title", DescriptionTranslations: map[string]string{"fr": "Titre public"}}), field.Select("status").Options(field.Option{Value: "draft", Label: "Draft", LabelTranslations: map[string]string{"fr": "Brouillon"}}), field.Array("items", field.Fields{field.Text("name")}).Admin(field.Admin{RowLabels: field.RowLabels{
				Singular: "Item", SingularTranslations: map[string]string{"fr": "Élément"},
				Plural: "Items", PluralTranslations: map[string]string{"fr": "Éléments"},
			}}), field.Blocks("layout", field.Block{Slug: "hero", Labels: field.BlockLabels{SingularTranslations: map[string]string{"fr": "Bannière"}}, Fields: field.Fields{field.Text("heading")}}), field.Tabs(field.Fields{field.UnnamedTab("Details", field.Fields{field.Text("summary")}).LabelTranslations(map[string]string{"fr": "Détails"})}), field.Collapsible("settings", field.Fields{field.Text("notes")}).Admin(field.Admin{InitiallyCollapsed: false}).LabelTranslations(map[string]string{"fr": "Paramètres"}), field.Text("slug").Admin(field.Admin{Tab: "Metadata", TabTranslations: map[string]string{"fr": "Métadonnées"}}),
			},
		}},
		Globals: []ridu.Global{{
			Slug: "settings", Label: "Settings", LabelTranslations: map[string]string{"fr": "Réglages"},
			Admin: ridu.GlobalAdmin{
				Group: "Configuration", GroupTranslations: map[string]string{"fr": "Configuration"},
				Description: "Site settings", DescriptionTranslations: map[string]string{"fr": "Réglages du site"},
			},
			Fields: field.Fields{field.Text("siteName")},
		}},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	collection := snapshot.Collections[0]
	if snapshot.Application.Name != "Editorial" || snapshot.Application.NameTranslations["fr"] != "Éditorial" {
		t.Fatalf("application translations = %#v", snapshot.Application)
	}
	if collection.Labels.Singular != "Post" || collection.Labels.SingularTranslations["fr"] != "Article" || collection.Labels.PluralTranslations["fr"] != "Articles" {
		t.Fatalf("collection labels = %#v", collection.Labels)
	}
	if collection.Admin.GroupTranslations["fr"] != "Contenu" || collection.Admin.DescriptionTranslations["fr"] != "Articles éditoriaux" || collection.Admin.LivePreview.Breakpoints[0].LabelTranslations["fr"] != "Téléphone" {
		t.Fatalf("collection admin translations = %#v", collection.Admin)
	}
	if collection.Fields[0].Admin.LabelTranslations["fr"] != "Titre" || collection.Fields[0].Admin.DescriptionTranslations["fr"] != "Titre public" {
		t.Fatalf("field translations = %#v", collection.Fields[0].Admin)
	}
	if collection.Fields[1].Select.Options[0].LabelTranslations["fr"] != "Brouillon" {
		t.Fatalf("option translations = %#v", collection.Fields[1].Select.Options)
	}
	if rows := collection.Fields[2].Nested.RowLabels; rows == nil || rows.SingularTranslations["fr"] != "Élément" || rows.PluralTranslations["fr"] != "Éléments" {
		t.Fatalf("array row labels = %#v", rows)
	}
	if collection.Fields[3].Blocks.ResolvedTypes()[0].Labels.SingularTranslations["fr"] != "Bannière" {
		t.Fatalf("block translations = %#v", collection.Fields[3].Blocks.ResolvedTypes())
	}
	if collection.Fields[4].Admin.TabTranslations["fr"] != "Détails" || collection.Fields[5].Admin.Collapsible.LabelTranslations["fr"] != "Paramètres" || collection.Fields[6].Admin.TabTranslations["fr"] != "Métadonnées" {
		t.Fatalf("presentation translations = %#v %#v %#v", collection.Fields[4].Admin, collection.Fields[5].Admin, collection.Fields[6].Admin)
	}
	global := snapshot.Globals[0]
	if global.Labels.Singular != "Settings" || global.Labels.SingularTranslations["fr"] != "Réglages" || global.Admin.DescriptionTranslations["fr"] != "Réglages du site" {
		t.Fatalf("global translations = %#v", global)
	}

	config.NameTranslations["fr"] = "mutated"
	config.Collections[0].Labels.SingularTranslations["fr"] = "mutated"
	config.Collections[0].Admin.LivePreview.Breakpoints[0].LabelTranslations["fr"] = "mutated"
	config.Globals[0].LabelTranslations["fr"] = "mutated"
	snapshot.Application.NameTranslations["fr"] = "snapshot-mutated"
	snapshot.Collections[0].Fields[0].Admin.LabelTranslations["fr"] = "snapshot-mutated"
	snapshot.Collections[0].Fields[1].Select.Options[0].LabelTranslations["fr"] = "snapshot-mutated"
	snapshot.Collections[0].Fields[2].Nested.RowLabels.SingularTranslations["fr"] = "snapshot-mutated"
	snapshot.Collections[0].Fields[3].Blocks.ResolvedTypes()[0].Labels.SingularTranslations["fr"] = "snapshot-mutated"
	if after := manifest.Snapshot(); after.Application.NameTranslations["fr"] != "Éditorial" || after.Collections[0].Labels.SingularTranslations["fr"] != "Article" || after.Collections[0].Admin.LivePreview.Breakpoints[0].LabelTranslations["fr"] != "Téléphone" || after.Collections[0].Fields[0].Admin.LabelTranslations["fr"] != "Titre" || after.Collections[0].Fields[1].Select.Options[0].LabelTranslations["fr"] != "Brouillon" || after.Collections[0].Fields[2].Nested.RowLabels.SingularTranslations["fr"] != "Élément" || after.Collections[0].Fields[3].Blocks.ResolvedTypes()[0].Labels.SingularTranslations["fr"] != "Bannière" || after.Globals[0].Labels.SingularTranslations["fr"] != "Réglages" {
		t.Fatalf("manifest retained caller-owned translation maps: %#v", after)
	}
}

func TestAdminFieldPresentationMetadataResolvesWithoutChangingFieldIdentity(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Admin field metadata",
		Admin: ridu.AdminConfig{Localization: ridu.AdminLocalizationConfig{
			DefaultLanguage: "en",
			Languages:       []ridu.AdminLanguage{{Code: "en", Label: "English"}, {Code: "fr", Label: "Français"}},
		}},
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("summary").Admin(field.Admin{Placeholder: "Write a summary", PlaceholderTranslations: map[string]string{"fr": "Rédigez un résumé"}, Hidden: true, Sidebar: true})},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved := manifest.Snapshot().Collections[0].Fields[0]
	if resolved.Path.String() != "summary" || resolved.Admin.Placeholder != "Write a summary" || resolved.Admin.PlaceholderTranslations["fr"] != "Rédigez un résumé" || !resolved.Admin.Hidden || !resolved.Admin.Sidebar {
		t.Fatalf("resolved field metadata = %#v", resolved)
	}
	resolved.Admin.PlaceholderTranslations["fr"] = "mutated"
	if got := manifest.Snapshot().Collections[0].Fields[0].Admin.PlaceholderTranslations["fr"]; got != "Rédigez un résumé" {
		t.Fatalf("manifest placeholder translations mutated through snapshot: %q", got)
	}
}

func TestNestedSidebarPlacementIsRejected(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Nested sidebar",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Group("seo", field.Fields{field.Text("title").Admin(field.Admin{Sidebar: true})})},
		}},
	})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
	}
	if !slices.ContainsFunc(validationError.Issues, func(issue schema.Issue) bool {
		return issue.Code == "unsupported_sidebar" && issue.Path == "collections[0].fields[0].fields[0].admin.sidebar"
	}) {
		t.Fatalf("issues = %#v, want nested sidebar rejection", validationError.Issues)
	}
}

func TestAdminDisplayTranslationsRejectUnknownLanguagesAndBlankValues(t *testing.T) {
	tests := []struct {
		name   string
		config ridu.Config
		code   string
		path   string
	}{
		{
			name:   "unknown language",
			config: ridu.Config{Name: "Example", NameTranslations: map[string]string{"fr": "Exemple"}, Admin: ridu.AdminConfig{Localization: ridu.AdminLocalizationConfig{DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}}}}, Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}},
			code:   "unknown_admin_translation_language", path: `nameTranslations["fr"]`,
		},
		{
			name:   "blank value",
			config: ridu.Config{Name: "Example", Admin: ridu.AdminConfig{Localization: ridu.AdminLocalizationConfig{DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}}}}, Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title").Admin(field.Admin{LabelTranslations: map[string]string{"en": " "}})}}}},
			code:   "missing_admin_translation_value", path: `collections[0].fields[0].admin.labelTranslations["en"]`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(test.config)
			var validationError *schema.ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
			}
			for _, issue := range validationError.Issues {
				if issue.Code == test.code && issue.Path == test.path {
					return
				}
			}
			t.Fatalf("issues = %#v, want %s at %s", validationError.Issues, test.code, test.path)
		})
	}
}

func invalidConfig() ridu.Config {
	return ridu.Config{
		Name: " ",
		Collections: []ridu.Collection{
			{
				Slug: "posts",
				Fields: field.Fields{field.Text("Title"), field.Text("Title"), field.Group("meta", field.Fields{}), field.Select("status").Options(

					field.Option{Value: "draft", Label: "Draft"},
					field.Option{Value: "draft", Label: "Duplicate"}).Default("published"), field.Relationship("author", "missing"),
				},
			},
			{Slug: "posts"},
		},
		Plugins: []ridu.Plugin{keyPlugin("duplicate"), keyPlugin("duplicate")},
	}
}

type appendFieldPlugin struct {
	key   string
	field field.Node
}

func (plugin appendFieldPlugin) Key() string { return plugin.key }

func (plugin appendFieldPlugin) TransformConfig(applicationConfig ridu.Config) (ridu.Config, error) {
	applicationConfig.Collections[0].Fields = append(applicationConfig.Collections[0].Fields, plugin.field)
	return applicationConfig, nil
}

type keyPlugin string

func (plugin keyPlugin) Key() string { return string(plugin) }

type pairedPlugin struct {
	key   string
	admin ridu.AdminPluginMetadata
}

func (plugin pairedPlugin) Key() string { return plugin.key }
func (plugin pairedPlugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{Version: "1.0.0", GoPackage: "example.com/plugins/" + plugin.key, APIVersion: ridu.PluginAPIVersion, Ridu: ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion, MaximumExclusive: "0.3.0"}, Admin: &plugin.admin}
}

type pointerPlugin struct{}

func (plugin *pointerPlugin) Key() string { return "pointer" }

type descriptorPlugin struct {
	key        string
	descriptor ridu.PluginDescriptor
}

func (plugin descriptorPlugin) Key() string                       { return plugin.key }
func (plugin descriptorPlugin) Descriptor() ridu.PluginDescriptor { return plugin.descriptor }

func assertGolden(t *testing.T, name string, actual []byte) {
	t.Helper()
	path := filepath.Join("..", "testdata", "manifests", name)
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nactual:\n%s", path, err, actual)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("golden %s differs\nexpected:\n%s\nactual:\n%s", path, expected, actual)
	}
}

func TestBackendAPI1PairsWithAdminAPI1(t *testing.T) {
	descriptor := validDescriptor("audit", ridu.RiduCompatibility{Minimum: "0.0.0-dev"})
	descriptor.APIVersion = 1
	descriptor.Admin = &ridu.AdminPluginMetadata{Package: "@example/audit", Export: "audit", APIVersion: 1, PairingVersion: 1}
	_, err := ridu.Resolve(ridu.Config{Name: "API pairing", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}, Plugins: []ridu.Plugin{descriptorPlugin{key: "audit", descriptor: descriptor}}})
	if err != nil {
		t.Fatal(err)
	}
	if ridu.PluginAPIVersion != 1 || ridu.AdminPluginAPIVersion != 1 {
		t.Fatal("incorrect independent API markers")
	}
}
