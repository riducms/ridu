package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestOutputFieldAfterReadHookReceivesResolvedValue(t *testing.T) {
	observed := make(map[ridu.Operation]string)
	var observedPath string
	application, err := ridu.New(ridu.Config{Name: "Output field hook", Collections: []ridu.Collection{{
		Slug:   "posts",
		Fields: []field.Definition{field.Text("title", field.Required()), field.Virtual("summary", field.ValueString)},
		Computed: map[string]ridu.Computed{
			"summary": func(ctx ridu.ComputedContext) (store.Value, error) {
				title, _ := ctx.Document.Values["title"].StringValue()
				return store.String(title + " summary"), nil
			},
		},
		FieldHooks: map[string]ridu.CollectionHooks{"summary": {AfterRead: []ridu.Hook{func(ctx ridu.HookContext) error {
			observedPath = ctx.FieldPath
			if ctx.Document != nil {
				observed[ctx.Operation], _ = ctx.Document.Values["summary"].StringValue()
			}
			return nil
		}}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Ridu")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := application.Local().Update(context.Background(), "posts", created.ID, store.Values{"title": store.String("Updated")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Find(context.Background(), "posts", updated.ID, nil); err != nil {
		t.Fatal(err)
	}
	if observedPath != "summary" || observed[ridu.OperationCreate] != "Ridu summary" || observed[ridu.OperationUpdate] != "Updated summary" || observed[ridu.OperationRead] != "Updated summary" {
		t.Fatalf("output after-read hook observed path %q and values %#v", observedPath, observed)
	}
}

func TestFieldRuntimeConfigAcceptsCanonicalStoredAndOutputPaths(t *testing.T) {
	allow := func(ridu.FieldAccessContext) (bool, error) { return true, nil }
	observe := func(ridu.HookContext) error { return nil }
	_, err := ridu.Resolve(ridu.Config{
		Name:    "Canonical field runtime paths",
		Plugins: []ridu.Plugin{keyPlugin("color")},
		Collections: []ridu.Collection{{
			Slug: "pages",
			Fields: []field.Definition{
				field.Text("slug", field.Unique()),
				field.Group("meta", field.Fields(field.Text("description"))),
				field.Array("rows", field.Fields(field.Text("label"))),
				field.Blocks("layout", field.BlockTypes(field.BlockType("hero", "Hero", field.Text("heading")))),
				field.Plugin("accent", "color", json.RawMessage(`{"format":"hex"}`)),
				field.Relationship("featuredComment", field.To("comments"), field.Unique()),
				field.Upload("heroImage", field.To("media"), field.Unique()),
				field.Join("comments", "comments", "page"),
				field.Virtual("summary", field.ValueString),
			},
			FieldAccess: map[string]ridu.FieldAccess{
				"meta.description":    {Read: allow},
				"rows.label":          {Update: allow},
				"layout.hero.heading": {Create: allow},
				"accent":              {Update: allow},
				"comments":            {Read: allow},
				"summary":             {Read: allow},
			},
			FieldHooks: map[string]ridu.CollectionHooks{
				"meta.description":    {BeforeValidate: []ridu.Hook{observe}},
				"rows.label":          {AfterChange: []ridu.Hook{observe}},
				"layout.hero.heading": {BeforeChange: []ridu.Hook{observe}},
				"accent":              {BeforeValidate: []ridu.Hook{observe}},
				"comments":            {AfterRead: []ridu.Hook{observe}},
				"summary":             {AfterRead: []ridu.Hook{observe}},
			},
			Computed: map[string]ridu.Computed{
				"summary": func(ridu.ComputedContext) (store.Value, error) { return store.String("summary"), nil },
			},
		}, {
			Slug:   "comments",
			Fields: []field.Definition{field.Relationship("page", field.To("pages"))},
		}, {
			Slug: "media", Upload: true,
		}},
		Globals: []ridu.Global{{
			Slug:   "site-settings",
			Fields: []field.Definition{field.Group("branding", field.Fields(field.Text("name")))},
			FieldAccess: map[string]ridu.FieldAccess{
				"branding.name": {Read: allow},
			},
			FieldHooks: map[string]ridu.CollectionHooks{
				"branding.name": {AfterRead: []ridu.Hook{observe}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Resolve canonical field paths: %v", err)
	}
}

func TestFieldRuntimeConfigRejectsUnknownAndIncompatiblePaths(t *testing.T) {
	allow := func(ridu.FieldAccessContext) (bool, error) { return true, nil }
	observe := func(ridu.HookContext) error { return nil }
	tests := []struct {
		name    string
		mutate  func(*ridu.Config)
		code    string
		path    string
		message string
	}{
		{
			name: "collection access typo",
			mutate: func(config *ridu.Config) {
				config.Collections[0].FieldAccess = map[string]ridu.FieldAccess{"titel": {Read: allow}}
			},
			code: "unknown_field_access", path: `collections[0].fieldAccess["titel"]`, message: `field access path "titel" does not name a resolved field`,
		},
		{
			name: "global access typo",
			mutate: func(config *ridu.Config) {
				config.Globals[0].FieldAccess = map[string]ridu.FieldAccess{"taglinee": {Read: allow}}
			},
			code: "unknown_field_access", path: `globals[0].fieldAccess["taglinee"]`, message: `field access path "taglinee" does not name a resolved field`,
		},
		{
			name: "global create access",
			mutate: func(config *ridu.Config) {
				config.Globals[0].FieldAccess = map[string]ridu.FieldAccess{"tagline": {Create: allow}}
			},
			code: "incompatible_field_access", path: `globals[0].fieldAccess["tagline"]`, message: "singleton initialization is an update operation",
		},
		{
			name: "collection hook typo",
			mutate: func(config *ridu.Config) {
				config.Collections[0].FieldHooks = map[string]ridu.CollectionHooks{"titel": {BeforeValidate: []ridu.Hook{observe}}}
			},
			code: "unknown_field_hook", path: `collections[0].fieldHooks["titel"]`, message: `field hook path "titel" does not name a resolved field`,
		},
		{
			name: "global hook typo",
			mutate: func(config *ridu.Config) {
				config.Globals[0].FieldHooks = map[string]ridu.CollectionHooks{"taglinee": {AfterRead: []ridu.Hook{observe}}}
			},
			code: "unknown_field_hook", path: `globals[0].fieldHooks["taglinee"]`, message: `field hook path "taglinee" does not name a resolved field`,
		},
		{
			name: "UI access",
			mutate: func(config *ridu.Config) {
				config.Collections[0].FieldAccess = map[string]ridu.FieldAccess{"guide": {Read: allow}}
			},
			code: "incompatible_field_access", path: `collections[0].fieldAccess["guide"]`, message: "presentation-only field",
		},
		{
			name: "virtual write access",
			mutate: func(config *ridu.Config) {
				config.Collections[0].FieldAccess = map[string]ridu.FieldAccess{"summary": {Update: allow}}
			},
			code: "incompatible_field_access", path: `collections[0].fieldAccess["summary"]`, message: "only read access may be configured",
		},
		{
			name: "virtual write-phase hooks",
			mutate: func(config *ridu.Config) {
				config.Collections[0].FieldHooks = map[string]ridu.CollectionHooks{"summary": {BeforeChange: []ridu.Hook{observe}}}
			},
			code: "incompatible_field_hook", path: `collections[0].fieldHooks["summary"]`, message: "only after-read hooks may be configured",
		},
		{
			name: "unsupported collection field hook phases",
			mutate: func(config *ridu.Config) {
				config.Collections[0].FieldHooks = map[string]ridu.CollectionHooks{"title": {
					BeforeRead: []ridu.Hook{observe}, AfterError: []ridu.Hook{observe},
				}}
			},
			code: "incompatible_field_hook", path: `collections[0].fieldHooks["title"]`, message: "unsupported phases: [BeforeRead AfterError]",
		},
		{
			name: "unsupported global-only field hook phases",
			mutate: func(config *ridu.Config) {
				config.Globals[0].FieldHooks = map[string]ridu.CollectionHooks{"tagline": {
					BeforeDuplicate: []ridu.Hook{observe}, BeforeDelete: []ridu.Hook{observe},
					AfterDelete: []ridu.Hook{observe}, AfterError: []ridu.Hook{observe},
				}}
			},
			code: "incompatible_field_hook", path: `globals[0].fieldHooks["tagline"]`, message: "unsupported phases: [BeforeDuplicate BeforeDelete AfterDelete AfterError]",
		},
		{
			name: "unsupported global resource hook phases",
			mutate: func(config *ridu.Config) {
				config.Globals[0].Hooks = ridu.CollectionHooks{
					BeforeDuplicate: []ridu.Hook{observe}, BeforeDelete: []ridu.Hook{observe}, AfterDelete: []ridu.Hook{observe},
				}
			},
			code: "incompatible_global_hook", path: `globals[0].hooks`, message: "unsupported phases: [BeforeDuplicate BeforeDelete AfterDelete]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := fieldRuntimeConfig()
			test.mutate(&config)
			_, err := ridu.Resolve(config)
			issue := requireValidationIssue(t, err, test.code, test.path)
			if !strings.Contains(issue.Message, test.message) {
				t.Fatalf("issue message = %q, want substring %q", issue.Message, test.message)
			}
		})
	}
}

func TestApplicationRejectsUnknownFieldHookBeforeServingInput(t *testing.T) {
	called := false
	_, err := ridu.New(ridu.Config{
		Name: "Unknown field hook",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: []field.Definition{field.Text("title")},
			FieldHooks: map[string]ridu.CollectionHooks{"titel": {
				BeforeValidate: []ridu.Hook{func(ridu.HookContext) error { called = true; return nil }},
			}},
		}},
	}, teststore.New())
	requireValidationIssue(t, err, "unknown_field_hook", `collections[0].fieldHooks["titel"]`)
	if called {
		t.Fatal("unknown field hook ran before invalid configuration was rejected")
	}
}

func TestUnsupportedUniqueFieldsFailClosedForCollectionsAndGlobals(t *testing.T) {
	tests := []struct {
		name   string
		field  field.Definition
		global bool
		path   string
	}{
		{
			name: "collection group", field: field.Group("meta", field.Fields(field.Text("code", field.Unique()))),
			path: "collections[0].fields[0].options.fields[0].options.unique",
		},
		{
			name: "collection array", field: field.Array("rows", field.Fields(field.Text("code", field.Unique()))),
			path: "collections[0].fields[0].options.fields[0].options.unique",
		},
		{
			name: "collection block", field: field.Blocks("layout", field.BlockTypes(field.BlockType("hero", "Hero", field.Text("code", field.Unique())))),
			path: "collections[0].fields[0].options.blocks[0].fields[0].options.unique",
		},
		{
			name: "collection named tab", field: field.Tabs(field.NamedTab("seo", "SEO", field.Text("code", field.Unique()))),
			path: "collections[0].fields[0].tabs[0].fields[0].options.unique",
		},
		{
			name: "global group", field: field.Group("meta", field.Fields(field.Text("code", field.Unique()))), global: true,
			path: "globals[0].fields[0].options.fields[0].options.unique",
		},
		{
			name: "has-many relationship", field: field.Relationship("authors", field.ToMany("users"), field.Unique()),
			path: "collections[0].fields[0].options.unique",
		},
		{
			name: "polymorphic relationship", field: field.Relationship("owner", field.ToAny("users", "teams"), field.Unique()),
			path: "collections[0].fields[0].options.unique",
		},
		{
			name: "has-many upload", field: field.Upload("attachments", field.ToMany("media"), field.Unique()),
			path: "collections[0].fields[0].options.unique",
		},
		{
			name: "global has-many relationship", field: field.Relationship("authors", field.ToMany("users"), field.Unique()), global: true,
			path: "globals[0].fields[0].options.unique",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := ridu.Config{Name: "Unsupported unique", Collections: []ridu.Collection{
				{Slug: "posts", Fields: []field.Definition{field.Text("title")}},
				{Slug: "users", Fields: []field.Definition{field.Text("name")}},
				{Slug: "teams", Fields: []field.Definition{field.Text("name")}},
				{Slug: "media", Upload: true},
			}}
			if test.global {
				config.Globals = []ridu.Global{{Slug: "settings", Fields: []field.Definition{test.field}}}
			} else {
				config.Collections[0].Fields = []field.Definition{test.field}
			}
			_, err := ridu.Resolve(config)
			issue := requireValidationIssue(t, err, "unsupported_unique", test.path)
			if !strings.Contains(issue.Message, `cannot be unique`) {
				t.Fatalf("issue message = %q", issue.Message)
			}
		})
	}
}

func fieldRuntimeConfig() ridu.Config {
	return ridu.Config{
		Name: "Field runtime validation",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: []field.Definition{field.Text("title"), field.UI("guide"), field.Virtual("summary", field.ValueString)},
			Computed: map[string]ridu.Computed{
				"summary": func(ridu.ComputedContext) (store.Value, error) { return store.String("summary"), nil },
			},
		}},
		Globals: []ridu.Global{{Slug: "site-settings", Fields: []field.Definition{field.Text("tagline")}}},
	}
}

func requireValidationIssue(t *testing.T, err error, code, path string) schema.Issue {
	t.Helper()
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Resolve error = %T %v, want *schema.ValidationError", err, err)
	}
	for _, issue := range validationError.Issues {
		if issue.Code == code && issue.Path == path {
			return issue
		}
	}
	t.Fatalf("issues = %#v, want %s at %s", validationError.Issues, code, path)
	return schema.Issue{}
}
