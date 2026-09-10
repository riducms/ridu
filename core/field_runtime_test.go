package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestOutputFieldAfterReadHookReceivesResolvedValue(t *testing.T) {
	observed := make(map[operation.Kind]string)
	var occurrence operation.OccurrenceID
	output := field.Virtual("summary", field.ValueString, func(ctx operation.ReadContext) (operation.Value[store.Value], error) {
		title, _ := ctx.Root.String("title")
		return operation.Present(store.String(title + " summary")), nil
	}).ReadHooks(field.ReadHooks[store.Value]{AfterRead: []field.OutputTransform[store.Value]{func(ctx operation.ReadContext, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
		occurrence = ctx.OccurrenceID
		value, _ := input.Get()
		observed[ctx.Operation], _ = value.StringValue()
		return operation.Keep[store.Value](), nil
	}}})
	application, err := ridu.New(ridu.Config{Name: "Output field hook", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title").Required(), output}}}}, teststore.New())
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
	if occurrence == "" || observed[operation.Create] != "Ridu summary" || observed[operation.Update] != "Updated summary" || observed[operation.Read] != "Updated summary" {
		t.Fatalf("output after-read hook observed path %q and values %#v", occurrence, observed)
	}
}

func TestUnsupportedUniqueFieldsFailClosedForCollectionsAndGlobals(t *testing.T) {
	tests := []struct {
		name   string
		field  field.Node
		global bool
		path   string
	}{
		{
			name: "collection group", field: field.Group("meta", field.Fields{field.Text("code").Unique()}),
			path: "collections[0].fields[0].fields[0].unique",
		},
		{
			name: "collection array", field: field.Array("rows", field.Fields{field.Text("code").Unique()}),
			path: "collections[0].fields[0].fields[0].unique",
		},
		{
			name: "collection block", field: field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("code").Unique()}}),
			path: "collections[0].fields[0].blocks[0].fields[0].unique",
		},
		{
			name: "collection named tab", field: field.Tabs(field.Fields{field.NamedTab("seo", "SEO", field.Fields{field.Text("code").Unique()})}),
			path: "collections[0].fields[0].tabs[0].fields[0].unique",
		},
		{
			name: "global group", field: field.Group("meta", field.Fields{field.Text("code").Unique()}), global: true,
			path: "globals[0].fields[0].fields[0].unique",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := ridu.Config{Name: "Unsupported unique", Collections: []ridu.Collection{
				{Slug: "posts", Fields: field.Fields{field.Text("title")}},
				{Slug: "users", Fields: field.Fields{field.Text("name")}},
				{Slug: "teams", Fields: field.Fields{field.Text("name")}},
				{Slug: "media", Upload: true},
			}}
			if test.global {
				config.Globals = []ridu.Global{{Slug: "settings", Fields: field.Fields{test.field}}}
			} else {
				config.Collections[0].Fields = field.Fields{test.field}
			}
			_, err := ridu.Resolve(config)
			issue := requireValidationIssue(t, err, "unsupported_unique", test.path)
			if !strings.Contains(issue.Message, `cannot be unique`) {
				t.Fatalf("issue message = %q", issue.Message)
			}
		})
	}
}

func TestGlobalResourceHooksRejectUnavailablePhases(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{Name: "Global phases", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}, Globals: []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Text("title")}, Hooks: ridu.CollectionHooks{BeforeDelete: []ridu.Hook{func(ridu.HookContext) error { return nil }}}}}})
	requireValidationIssue(t, err, "incompatible_global_hook", "globals[0].hooks")
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
