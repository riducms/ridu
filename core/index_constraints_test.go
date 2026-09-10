package core_test

import (
	"errors"
	"slices"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func TestResolveCarriesScalarConstraintsAndOrderedIndexes(t *testing.T) {
	configuredPaths := []string{"seo.slug", "priority"}
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Indexed",
		Globals: []ridu.Global{{
			Slug: "settings", Fields: field.Fields{field.Text("siteName").Index()},
		}},
		Collections: []ridu.Collection{
			{Slug: "media", Upload: true},
			{
				Slug:    "posts",
				Fields:  field.Fields{field.Text("title").MinLength(2).MaxLength(20).Index(), field.Textarea("summary").MaxLength(200), field.Code("source").MinLength(1).Admin(field.Admin{CodeLanguage: "go"}), field.Number("priority").Min(0).Max(10).Step(0.5), field.Group("seo", field.Fields{field.Text("slug").Index()}), field.Upload("hero", "media").Index()},
				Indexes: []ridu.CollectionIndex{{Fields: configuredPaths, Unique: true}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	configuredPaths[0] = "mutated"
	snapshot := manifest.Snapshot()
	posts := snapshot.Collections[1]
	if snapshot.Version != schema.CurrentVersion || len(posts.Indexes) != 1 || !posts.Indexes[0].Unique || posts.Indexes[0].Fields[0].String() != "seo.slug" || posts.Indexes[0].Fields[1].String() != "priority" {
		t.Fatalf("resolved indexes = version %d %#v", snapshot.Version, posts.Indexes)
	}
	title, summary, source, priority := posts.Fields[0], posts.Fields[1], posts.Fields[2], posts.Fields[3]
	if !title.Index || title.Text == nil || title.Text.MinLength == nil || *title.Text.MinLength != 2 || title.Text.MaxLength == nil || *title.Text.MaxLength != 20 {
		t.Fatalf("title metadata = %#v", title)
	}
	if summary.Textarea == nil || summary.Textarea.MaxLength == nil || *summary.Textarea.MaxLength != 200 || source.Code == nil || source.Code.MinLength == nil || *source.Code.MinLength != 1 {
		t.Fatalf("string metadata = summary %#v source %#v", summary, source)
	}
	if priority.Number == nil || priority.Number.Min == nil || *priority.Number.Min != 0 || priority.Number.Max == nil || *priority.Number.Max != 10 || priority.Number.Step == nil || *priority.Number.Step != 0.5 {
		t.Fatalf("number metadata = %#v", priority.Number)
	}
	if len(snapshot.Globals) != 1 || len(snapshot.Globals[0].Fields) != 1 || !snapshot.Globals[0].Fields[0].Index {
		t.Fatalf("global field index metadata = %#v", snapshot.Globals)
	}
}

func TestResolveRejectsUnsupportedOrAmbiguousIndexes(t *testing.T) {
	tests := []struct {
		name   string
		config ridu.Config
		code   string
		path   string
	}{
		{
			name:   "array field index",
			config: ridu.Config{Name: "Array", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Array("items", field.Fields{field.Text("slug").Index()})}}}},
			code:   "unsupported_index", path: "collections[0].fields[0].fields[0].index",
		},
		{
			name:   "block field index",
			config: ridu.Config{Name: "Blocks", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Blocks("content", field.Block{Slug: "hero", Fields: field.Fields{field.Text("slug").Index()}})}}}},
			code:   "unsupported_index", path: "collections[0].fields[0].blocks[0].fields[0].index",
		},
		{
			name: "compound through array",
			config: ridu.Config{Name: "Repeated", Collections: []ridu.Collection{{
				Slug: "posts", Fields: field.Fields{field.Array("items", field.Fields{field.Text("slug")}), field.Text("title")},
				Indexes: []ridu.CollectionIndex{{Fields: []string{"items.slug", "title"}}},
			}}},
			code: "unsupported_index_field", path: "collections[0].indexes[0].fields[0]",
		},
		{
			name: "compound presentation terminal",
			config: ridu.Config{Name: "Presentation", Collections: []ridu.Collection{{
				Slug: "posts", Fields: field.Fields{field.UI("helper"), field.Text("title")},
				Indexes: []ridu.CollectionIndex{{Fields: []string{"helper", "title"}}},
			}}},
			code: "unsupported_index_field", path: "collections[0].indexes[0].fields[0]",
		},
		{
			name: "compound json terminal",
			config: ridu.Config{Name: "JSON", Collections: []ridu.Collection{{
				Slug: "posts", Fields: field.Fields{field.JSON("metadata"), field.Text("title")},
				Indexes: []ridu.CollectionIndex{{Fields: []string{"metadata", "title"}}},
			}}},
			code: "unsupported_index_field", path: "collections[0].indexes[0].fields[0]",
		},
		{
			name: "compound point terminal",
			config: ridu.Config{Name: "Point", Collections: []ridu.Collection{{
				Slug: "posts", Fields: field.Fields{field.Point("location"), field.Text("title")},
				Indexes: []ridu.CollectionIndex{{Fields: []string{"location", "title"}}},
			}}},
			code: "unsupported_index_field", path: "collections[0].indexes[0].fields[0]",
		},
		{
			name: "undersized compound index",
			config: ridu.Config{Name: "Small", Collections: []ridu.Collection{{
				Slug: "posts", Fields: field.Fields{field.Text("title")},
				Indexes: []ridu.CollectionIndex{{Fields: []string{"title"}}},
			}}},
			code: "invalid_index_size", path: "collections[0].indexes[0].fields",
		},
		{
			name: "oversized compound index",
			config: ridu.Config{Name: "Large", Collections: []ridu.Collection{{
				Slug: "posts", Fields: field.Fields{field.Text("title")},
				Indexes: []ridu.CollectionIndex{{Fields: []string{
					"title", "title", "title", "title", "title", "title", "title", "title", "title", "title", "title",
					"title", "title", "title", "title", "title", "title", "title", "title", "title", "title", "title",
					"title", "title", "title", "title", "title", "title", "title", "title", "title", "title", "title",
				}}},
			}}},
			code: "invalid_index_size", path: "collections[0].indexes[0].fields",
		},
		{
			name: "duplicate compound field",
			config: ridu.Config{Name: "Duplicate", Collections: []ridu.Collection{{
				Slug: "posts", Fields: field.Fields{field.Text("title"), field.Number("priority")},
				Indexes: []ridu.CollectionIndex{{Fields: []string{"title", "title"}}},
			}}},
			code: "duplicate_index_field", path: "collections[0].indexes[0].fields[1]",
		},
		{
			name: "duplicate ordered index",
			config: ridu.Config{Name: "Duplicate", Collections: []ridu.Collection{{
				Slug: "posts", Fields: field.Fields{field.Text("title"), field.Number("priority")},
				Indexes: []ridu.CollectionIndex{{Fields: []string{"title", "priority"}}, {Fields: []string{"title", "priority"}, Unique: true}},
			}}},
			code: "duplicate_index", path: "collections[0].indexes[1].fields",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(test.config)
			var validation *schema.ValidationError
			if !errors.As(err, &validation) || !slices.ContainsFunc(validation.Issues, func(issue schema.Issue) bool {
				return issue.Code == test.code && issue.Path == test.path
			}) {
				t.Fatalf("Resolve error = %v issues %#v, want %s at %s", err, validation, test.code, test.path)
			}
		})
	}
}
