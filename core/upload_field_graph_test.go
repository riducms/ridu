package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func uploadGraphConfig(fields field.Fields) Config {
	return Config{Name: "Upload field graph", Collections: []Collection{{
		Slug: "media", Upload: true, Fields: fields,
		UploadConfig: UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
	}}}
}

func TestUploadGraphRejectsUnsupportedMetadataConstraints(t *testing.T) {
	for _, test := range []struct {
		name string
		node field.Node
		code string
	}{
		{"wrong kind", field.Number("filename"), "invalid_upload_metadata_field"},
		{"localized", field.Text("filename").Localized(), "invalid_upload_metadata_field"},
		{"optional metadata made required", field.Number("width").Required(), "unsupported_upload_metadata_constraint"},
		{"index", field.Text("filename").Index(), "unsupported_upload_metadata_constraint"},
		{"unique", field.Text("filename").Unique(), "unsupported_upload_metadata_constraint"},
		{"default", field.Text("filename").Default("unnamed"), "unsupported_upload_metadata_constraint"},
		{"slug", field.Slug("filename", "title"), "unsupported_upload_metadata_constraint"},
		{"min length", field.Text("filename").MinLength(1), "unsupported_upload_metadata_constraint"},
		{"max length", field.Text("filename").MaxLength(10), "unsupported_upload_metadata_constraint"},
		{"minimum", field.Number("filesize").Min(1), "unsupported_upload_metadata_constraint"},
		{"maximum", field.Number("filesize").Max(10), "unsupported_upload_metadata_constraint"},
		{"step", field.Number("width").Step(2), "unsupported_upload_metadata_constraint"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Resolve(uploadGraphConfig(field.Fields{test.node}))
			var validation *schema.ValidationError
			if !errors.As(err, &validation) || len(validation.Issues) == 0 {
				t.Fatalf("unsupported metadata constraint = %v", err)
			}
			for _, issue := range validation.Issues {
				if issue.Code != test.code || !strings.HasPrefix(issue.Path, "collections[0].fields[0]") {
					t.Fatalf("unsupported metadata diagnostic = %#v", issue)
				}
			}
		})
	}
	config := uploadGraphConfig(nil)
	config.Plugins = []Plugin{graphEditPlugin{key: "metadata-constraint", transform: func(_ FieldGraphContext, fields field.Fields) (field.Fields, error) {
		return fields.Edit(func(draft *field.ChildrenDraft) error {
			return draft.EditText("filename", func(node field.TextField) field.TextField { return node.MaxLength(1) })
		})
	}}}
	if _, err := Resolve(config); err == nil || !strings.Contains(err.Error(), "unsupported_upload_metadata_constraint") {
		t.Fatalf("plugin metadata constraint was silently dropped: %v", err)
	}
}

func TestUploadGraphPreservesManagedStorageContracts(t *testing.T) {
	config := uploadGraphConfig(nil)
	resolution, err := resolveTestFieldGraph(config)
	if err != nil {
		t.Fatal(err)
	}
	fields := resolution.Manifest().Snapshot().Collections[0].Fields
	if len(fields) != 14 {
		t.Fatalf("managed field count = %d", len(fields))
	}
	expected := map[string]schema.Field{
		"filename":  {ID: "upload-filename", Name: "filename", Type: schema.FieldTypeText, Category: schema.FieldCategoryUpload, Required: true, Admin: schema.FieldAdmin{Label: "Filename", ReadOnly: true}, Text: &schema.TextField{}},
		"objectKey": {ID: "upload-object-key", Name: "objectKey", Type: schema.FieldTypeText, Category: schema.FieldCategoryUpload, Required: true, Index: true, Admin: schema.FieldAdmin{Label: "Object key", ReadOnly: true}, Text: &schema.TextField{}},
		"filesize":  {ID: "upload-filesize", Name: "filesize", Type: schema.FieldTypeNumber, Category: schema.FieldCategoryUpload, Required: true, Admin: schema.FieldAdmin{Label: "File size", ReadOnly: true}},
		"sizes":     {ID: "upload-sizes", Name: "sizes", Type: schema.FieldTypeJSON, Category: schema.FieldCategoryUpload, Admin: schema.FieldAdmin{Label: "Generated sizes", ReadOnly: true}},
	}
	byName := make(map[string]schema.Field, len(fields))
	for _, candidate := range fields {
		byName[candidate.Name] = candidate
		if wanted, exists := expected[candidate.Name]; exists {
			wanted.Path, _ = query.NewPath(candidate.Name)
			if !reflect.DeepEqual(candidate, wanted) {
				t.Fatalf("managed %s storage contract changed:\n got %#v\nwant %#v", candidate.Name, candidate, wanted)
			}
		}
	}
	for _, occurrence := range resolution.Occurrences() {
		manifestField := byName[occurrence.Name]
		view, exists := resolution.graph.Binding(occurrence.ID)
		if !exists || occurrence.SchemaID != manifestField.ID || view.Required() != manifestField.Required || view.Index() != manifestField.Index || !view.AdminPolicy().ReadOnly {
			t.Fatalf("graph and manifest disagree at %s: %#v", occurrence.Name, occurrence)
		}
	}
	second, err := resolveTestFieldGraph(config)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(resolution)
	secondJSON, _ := json.Marshal(second)
	if !bytes.Equal(firstJSON, secondJSON) || config.Collections[0].Fields != nil {
		t.Fatal("managed field resolution is nondeterministic or mutates authoring input")
	}
}

func TestUploadGraphOwnsMetadataAccessAndCallbacks(t *testing.T) {
	var readOccurrence operation.OccurrenceID
	var validated int
	config := uploadGraphConfig(field.Fields{
		field.Text("objectKey").Access(field.Access{Read: func(operation.AccessContext) (bool, error) { return false, nil }}),
		field.JSON("sizes").Access(field.Access{Read: func(operation.AccessContext) (bool, error) { return false, nil }}),
	})
	config.Plugins = []Plugin{graphEditPlugin{key: "upload-behavior", transform: func(_ FieldGraphContext, fields field.Fields) (field.Fields, error) {
		return fields.Edit(func(draft *field.ChildrenDraft) error {
			return draft.EditText("filename", func(node field.TextField) field.TextField {
				return node.Label("Original filename").Validate(func(operation.ValidationContext, operation.Value[string]) ([]operation.Issue, error) {
					validated++
					return nil, nil
				}).ReadHooks(field.ReadHooks[string]{AfterRead: []field.OutputTransform[string]{func(ctx operation.ReadContext, value operation.Value[string]) (operation.Change[string], error) {
					readOccurrence = ctx.OccurrenceID
					name, _ := value.Get()
					return operation.Replace(operation.Present(strings.ToUpper(name))), nil
				}}})
			})
		})
	}}}
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config.Storage = backend
	config.StorageNamespace = "upload-field-graph"
	application, err := New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", UploadInput{Filename: "example.txt", Reader: strings.NewReader("example")})
	if err != nil {
		t.Fatal(err)
	}
	if name, _ := document.Values["filename"].StringValue(); name != "EXAMPLE.TXT" || readOccurrence == "" || validated == 0 {
		t.Fatalf("metadata callbacks were lost: filename=%q occurrence=%q validated=%d", name, readOccurrence, validated)
	}
	for _, name := range []string{"objectKey", "sizes"} {
		if _, exposed := document.Values[name]; exposed {
			t.Fatalf("metadata read access exposed %s", name)
		}
	}
	for _, candidate := range application.Manifest().Snapshot().Collections[0].Fields {
		switch candidate.Name {
		case "filename":
			if candidate.Admin.Label != "Original filename" || !candidate.Admin.ReadOnly {
				t.Fatalf("metadata presentation was lost: %#v", candidate.Admin)
			}
		case "objectKey", "sizes":
			if !candidate.QueryRestricted {
				t.Fatalf("read-protected metadata %s lacks query restriction", candidate.Name)
			}
		}
	}
	found, err := application.Local().Find(context.Background(), "media", document.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := found.Values["filename"].StringValue(); value != "EXAMPLE.TXT" {
		t.Fatalf("local read did not run attached metadata callback: %q", value)
	}
	if _, err := application.Local().Update(context.Background(), "media", document.ID, store.Values{"filename": store.String("forged.txt")}, nil); err == nil {
		t.Fatal("field-owned behavior made managed metadata writable")
	}
}
