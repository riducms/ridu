package seo_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/plugins/seo"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPluginInjectsCoreSEOFieldsAndPairedRenderers(t *testing.T) {
	plugin := seo.New(seo.Config{
		Collections:       []schema.CollectionSlug{"posts"},
		Globals:           []schema.CollectionSlug{"site"},
		UploadsCollection: "media",
		TabbedUI:          true,
		GenerateTitle:     func(context seo.GenerateContext) (string, error) { return "Generated", nil },
		GenerateURL:       func(context seo.GenerateContext) (string, error) { return "https://example.test", nil },
	})
	manifest, err := ridu.Resolve(ridu.Config{
		Name:         "SEO",
		Localization: ridu.LocalizationConfig{Locales: []ridu.Locale{{Code: "en", Label: "English"}}, DefaultLocale: "en"},
		Plugins:      []ridu.Plugin{plugin},
		Collections: []ridu.Collection{
			{Slug: "posts", Labels: ridu.CollectionLabels{Singular: "Post"}, Fields: []field.Definition{
				field.Tabs(field.UnnamedTab("General", field.Text("headline"))),
				field.Text("sidebarNote"),
			}},
			{Slug: "media", Upload: true, Fields: []field.Definition{field.Text("alt")}},
		},
		Globals: []ridu.Global{{Slug: "site", Label: "Site", Fields: []field.Definition{field.Text("name")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	if len(snapshot.Plugins) != 1 || snapshot.Plugins[0].Admin == nil || snapshot.Plugins[0].Admin.Package != "@riducms/plugin-seo" {
		t.Fatalf("SEO plugin metadata = %#v", snapshot.Plugins)
	}
	posts := snapshot.Collections[0]
	if len(posts.Fields) != 3 || posts.Fields[0].Path.String() != "headline" || posts.Fields[1].Path.String() != "meta" || posts.Fields[2].Path.String() != "sidebarNote" {
		t.Fatalf("tabbed post fields = %#v", posts.Fields)
	}
	meta := posts.Fields[1]
	if meta.Admin.Tab != "SEO" || meta.Nested == nil || len(meta.Nested.Fields) != 5 {
		t.Fatalf("SEO meta field = %#v", meta)
	}
	want := []struct {
		name, fieldType, component string
		localized                  bool
	}{
		{"overview", "ui", "overview", false},
		{"title", "text", "title", true},
		{"description", "textarea", "description", true},
		{"image", "upload", "image", true},
		{"preview", "ui", "preview", false},
	}
	for index, expected := range want {
		candidate := meta.Nested.Fields[index]
		if candidate.Name != expected.name || string(candidate.Type) != expected.fieldType || candidate.Localized != expected.localized || candidate.Admin.Component == nil || candidate.Admin.Component.Plugin != seo.Key || candidate.Admin.Component.Component != expected.component {
			t.Errorf("SEO child %d = %#v, want %#v", index, candidate, expected)
		}
	}
	componentConfig := meta.Nested.Fields[0].Admin.Component.Config
	componentConfig[0] = '['
	freshMeta := manifest.Snapshot().Collections[0].Fields[1]
	if freshMeta.Nested == nil || string(freshMeta.Nested.Fields[0].Admin.Component.Config[:1]) != "{" {
		t.Fatal("manifest snapshot exposed mutable admin component config")
	}
	if image := meta.Nested.Fields[3]; image.Upload == nil || image.Upload.CollectionSlug != "media" {
		t.Fatalf("SEO image = %#v", image)
	}
	global := snapshot.Globals[0]
	if len(global.Fields) != 2 || global.Fields[0].Admin.Tab != "Site" || global.Fields[1].Path.String() != "meta" || global.Fields[1].Admin.Tab != "SEO" {
		t.Fatalf("tabbed global fields = %#v", global.Fields)
	}
}

func TestPluginWithoutLocalizationKeepsDefaultFieldsUsable(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "SEO", Plugins: []ridu.Plugin{seo.New(seo.Config{Collections: []schema.CollectionSlug{"posts"}})},
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("headline")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	meta := manifest.Snapshot().Collections[0].Fields[1]
	if meta.Nested == nil || meta.Nested.Fields[1].Localized || meta.Nested.Fields[2].Localized {
		t.Fatalf("non-localized SEO fields = %#v", meta.Nested)
	}
}

func TestTabbedUIKeepsAuthEmailTopLevelAndLiftsExistingTabs(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "SEO", Admin: ridu.AdminConfig{User: "users"}, Plugins: []ridu.Plugin{seo.New(seo.Config{Collections: []schema.CollectionSlug{"users"}, TabbedUI: true})},
		Collections: []ridu.Collection{{Slug: "users", Auth: true, Fields: []field.Definition{
			field.Email("email", field.Required(), field.Unique()),
			field.Text("displayName"),
			field.Tabs(field.NamedTab("preferences", "Preferences", field.Text("timezone"))),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields := manifest.Snapshot().Collections[0].Fields
	if len(fields) != 4 || fields[0].Path.String() != "email" || fields[0].Admin.Tab != "" {
		t.Fatalf("auth fields = %#v", fields)
	}
	if fields[1].Path.String() != "displayName" || fields[1].Admin.Tab != "Content" {
		t.Fatalf("content tab field = %#v", fields[1])
	}
	if fields[2].Path.String() != "preferences" || fields[2].Admin.Tab != "Preferences" || !fields[2].Admin.NamedTab {
		t.Fatalf("lifted authored tab = %#v", fields[2])
	}
	if fields[3].Path.String() != "meta" || fields[3].Admin.Tab != "SEO" {
		t.Fatalf("SEO tab field = %#v", fields[3])
	}
}

func TestDirectPresentationConstructorsAcceptExplicitOverrides(t *testing.T) {
	overview := seo.OverviewWithConfig(seo.OverviewConfig{Label: "Search checks", TitlePath: "search.title"})
	preview := seo.PreviewWithConfig(seo.PreviewConfig{Label: "Result card", TitlePath: "search.title"})
	image := seo.MetaImageWithConfig(seo.MetaImageConfig{Collection: "media", Label: "Social card", Description: "Use 1200 by 630."})
	for name, definition := range map[string]field.Definition{"overview": overview, "preview": preview, "image": image} {
		if issues := definition.Issues(); len(issues) != 0 {
			t.Fatalf("%s issues = %#v", name, issues)
		}
	}
	if overview.Label() != "Search checks" || preview.Label() != "Result card" || image.Label() != "Social card" || image.Description() != "Use 1200 by 630." {
		t.Fatalf("direct labels = %q / %q / %q (%q)", overview.Label(), preview.Label(), image.Label(), image.Description())
	}
}

func TestPluginFieldOverrideAndTargetValidation(t *testing.T) {
	plugin := seo.New(seo.Config{
		Collections: []schema.CollectionSlug{"posts"},
		Fields: func(defaults []field.Definition) ([]field.Definition, error) {
			return append(defaults, field.Text("ogTitle")), nil
		},
	})
	manifest, err := ridu.Resolve(ridu.Config{Name: "SEO", Plugins: []ridu.Plugin{plugin}, Collections: []ridu.Collection{{Slug: "posts"}}})
	if err != nil {
		t.Fatal(err)
	}
	meta := manifest.Snapshot().Collections[0].Fields[0]
	if meta.Nested == nil || meta.Nested.Fields[len(meta.Nested.Fields)-1].Name != "ogTitle" {
		t.Fatalf("overridden SEO fields = %#v", meta.Nested)
	}

	_, err = ridu.Resolve(ridu.Config{Name: "SEO", Plugins: []ridu.Plugin{seo.New(seo.Config{Collections: []schema.CollectionSlug{"missing"}})}, Collections: []ridu.Collection{{Slug: "posts"}}})
	var validation *schema.ValidationError
	if !errors.As(err, &validation) || validation.Issues[0].Code != "plugin_transform_failed" || !strings.Contains(validation.Issues[0].Message, "unknown_seo_collection") {
		t.Fatalf("unknown target error = %v", err)
	}
}

func TestGenerationEndpointRequiresActorAndUsesCurrentDraft(t *testing.T) {
	plugin := seo.New(seo.Config{
		Collections: []schema.CollectionSlug{"posts"},
		GenerateTitle: func(context seo.GenerateContext) (string, error) {
			title, _ := context.Document["headline"].(string)
			return "Website — " + title, nil
		},
	})
	application, err := ridu.New(ridu.Config{
		Name: "SEO", Plugins: []ridu.Plugin{plugin},
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("headline")}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := plugin.Endpoints()[0]

	unauthorized := httptest.NewRecorder()
	endpoint.Handler(ridu.PluginEndpointContext{Writer: unauthorized, Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"collection":"posts","document":{}}`)), Local: application.Local()})
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	var unauthorizedBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(unauthorized.Body.Bytes(), &unauthorizedBody); err != nil || unauthorizedBody.Error.Code != "access_denied" {
		t.Fatalf("unauthorized body = %q / %v", unauthorized.Body.String(), err)
	}

	trailing := httptest.NewRecorder()
	endpoint.Handler(ridu.PluginEndpointContext{Writer: trailing, Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"collection":"posts","document":{}} {}`)), Actor: &store.Document{ID: "editor"}, Local: application.Local()})
	if trailing.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON status = %d body=%s", trailing.Code, trailing.Body.String())
	}

	actor := store.Document{ID: "editor", Values: store.Values{}}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"collection":"posts","document":{"headline":"Hello"}}`))
	response := httptest.NewRecorder()
	endpoint.Handler(ridu.PluginEndpointContext{Writer: response, Request: request, Actor: &actor, Local: application.Local(), ReportError: func(error, string) {}})
	if response.Code != http.StatusOK {
		t.Fatalf("generation status = %d body=%s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["result"] != "Website — Hello" {
		t.Fatalf("generation body = %q / %v", response.Body.String(), err)
	}
}

func TestEndpointCapturesApplicationResourceDefinitions(t *testing.T) {
	plugin := seo.New(seo.Config{GenerateTitle: func(context seo.GenerateContext) (string, error) {
		return context.Collection.Labels.Singular, nil
	}})
	first, err := ridu.New(ridu.Config{Name: "First", Localization: ridu.LocalizationConfig{Locales: []ridu.Locale{{Code: "en", Label: "English"}}, DefaultLocale: "en"}, Plugins: []ridu.Plugin{plugin}, Collections: []ridu.Collection{{Slug: "pages", Labels: ridu.CollectionLabels{Singular: "First page"}, Fields: []field.Definition{seo.MetaTitle(true)}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	firstEndpoint := plugin.Endpoints()[0]
	if _, err := ridu.New(ridu.Config{Name: "Second", Localization: ridu.LocalizationConfig{Locales: []ridu.Locale{{Code: "en", Label: "English"}}, DefaultLocale: "en"}, Plugins: []ridu.Plugin{plugin}, Collections: []ridu.Collection{{Slug: "pages", Labels: ridu.CollectionLabels{Singular: "Second page"}, Fields: []field.Definition{seo.MetaTitle(true)}}}}, teststore.New()); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	firstEndpoint.Handler(ridu.PluginEndpointContext{Writer: response, Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"collection":"pages","document":{}}`)), Actor: &store.Document{ID: "editor"}, Local: first.Local()})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "First page") {
		t.Fatalf("captured first application definition: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSEOValuesUseOrdinaryEnginePersistence(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "SEO", Plugins: []ridu.Plugin{seo.New(seo.Config{Collections: []schema.CollectionSlug{"posts"}})},
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("headline")}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(t.Context(), "posts", store.Values{
		"headline": store.String("Hello"),
		"meta":     store.Object(store.Values{"title": store.String("Search title"), "description": store.String("Search description")}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := created.Values["meta"].ObjectValue()
	if title, _ := meta["title"].StringValue(); title != "Search title" {
		t.Fatalf("persisted meta = %#v", meta)
	}
}
