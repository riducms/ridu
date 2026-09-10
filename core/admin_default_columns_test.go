package core_test

import (
	"errors"
	"slices"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func TestCollectionDefaultColumnsAcceptOnlyCapabilityOwnedSystemFields(t *testing.T) {
	config := ridu.Config{Name: "System list columns", Collections: []ridu.Collection{
		{
			Slug: "posts", Versions: true, Trash: true,
			Admin:  ridu.CollectionAdmin{DefaultColumns: []string{"title", "id", "createdAt", "updatedAt", "deletedAt", "_status", "_revision"}},
			Fields: field.Fields{field.Text("title")},
		},
		{
			Slug: "media", Upload: true,
			Admin: ridu.CollectionAdmin{DefaultColumns: []string{
				"filename", "mimeType", "filesize", "url", "objectKey", "width", "height", "sizes",
				"focalX", "focalY", "cropX", "cropY", "cropWidth", "cropHeight",
			}},
			Fields: field.Fields{field.Text("alt")},
		},
	}}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	collections := manifest.Snapshot().Collections
	if !slices.Equal(collections[0].Admin.DefaultColumns, config.Collections[0].Admin.DefaultColumns) {
		t.Fatalf("document default columns = %v", collections[0].Admin.DefaultColumns)
	}
	if !slices.Equal(collections[1].Admin.DefaultColumns, config.Collections[1].Admin.DefaultColumns) {
		t.Fatalf("upload default columns = %v", collections[1].Admin.DefaultColumns)
	}

	tests := []struct {
		name       string
		collection ridu.Collection
		column     string
	}{
		{name: "draft status without versions", collection: ridu.Collection{Slug: "posts"}, column: "_status"},
		{name: "revision without versions", collection: ridu.Collection{Slug: "posts"}, column: "_revision"},
		{name: "deleted timestamp without trash", collection: ridu.Collection{Slug: "posts"}, column: "deletedAt"},
		{name: "upload metadata without upload", collection: ridu.Collection{Slug: "posts"}, column: "filename"},
		{name: "unknown upload metadata", collection: ridu.Collection{Slug: "media", Upload: true}, column: "thumbnailURL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.collection.Admin.DefaultColumns = []string{test.column}
			_, resolveError := ridu.Resolve(ridu.Config{Name: "Invalid system list column", Collections: []ridu.Collection{test.collection}})
			var validation *schema.ValidationError
			if !errors.As(resolveError, &validation) || !slices.ContainsFunc(validation.Issues, func(issue schema.Issue) bool {
				return issue.Code == "unknown_admin_field" && issue.Path == "collections[0].admin.defaultColumns[0]"
			}) {
				t.Fatalf("Resolve error = %v", resolveError)
			}
		})
	}
}
