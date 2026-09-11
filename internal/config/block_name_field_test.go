package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestBlockNameFieldResolvesAcrossInlineRegisteredAndEmbeddedSchemas(t *testing.T) {
	block := field.Block{
		Slug:   "card",
		Admin:  field.BlockAdmin{NameField: "title", RowLabelPath: "count"},
		Fields: field.Fields{field.Text("title"), field.Number("count")},
	}
	inline, err := Resolve(Input{Name: "Inline block names", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", block)}}}})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := Resolve(Input{Name: "Registered block names", Blocks: []field.Block{block}, Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References("card")}}}})
	if err != nil {
		t.Fatal(err)
	}
	tree := field.EmbeddedTree{
		Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind",
		Cases: []field.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []field.Block{block}}},
	}
	embedded, err := Resolve(Input{
		Name: "Embedded block names", Plugins: []Plugin{{Key: "outline"}},
		Collections: []Collection{{Slug: "pages", Fields: field.Fields{
			field.Plugin("body", "outline", json.RawMessage(`{}`)).EmbeddedTrees(tree),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	inlineSnapshot := inline.Snapshot()
	registeredSnapshot := registered.Snapshot()
	embeddedSnapshot := embedded.Snapshot()
	blocks := []schema.BlockType{
		inlineSnapshot.Collections[0].Fields[0].Blocks.ResolvedTypes()[0],
		registeredSnapshot.Blocks[0],
		registeredSnapshot.Collections[0].Fields[0].Blocks.ResolvedTypes()[0],
		embeddedSnapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0],
	}
	for _, resolved := range blocks {
		if resolved.Admin == nil || resolved.Admin.NameField != "title" || resolved.Admin.RowLabel != "count" {
			t.Fatalf("block admin metadata = %#v", resolved.Admin)
		}
	}

	// Returned registry and placement views must not alias the immutable manifest
	// or each other.
	registeredSnapshot.Blocks[0].Admin.NameField = "changed"
	registeredSnapshot.Collections[0].Fields[0].Blocks.ResolvedTypes()[0].Admin.NameField = "changed"
	again := registered.Snapshot()
	if again.Blocks[0].Admin.NameField != "title" || again.Collections[0].Fields[0].Blocks.ResolvedTypes()[0].Admin.NameField != "title" {
		t.Fatal("block name metadata mutated through a snapshot view")
	}
	registeredBytes, err := registered.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	parsedRegistered, err := schema.Parse(registeredBytes)
	if err != nil {
		t.Fatal(err)
	}
	registeredField := parsedRegistered.Snapshot().Collections[0].Fields[0]
	if len(registeredField.Blocks.Types) != 0 || registeredField.Blocks.ResolvedTypes()[0].Admin.NameField != "title" {
		t.Fatal("name validation expanded or lost compact registered metadata")
	}
	encoded, err := embedded.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := schema.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Snapshot().Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].Admin.NameField != "title" {
		t.Fatal("embedded block name metadata was lost during schema parsing")
	}
}

func TestBlockNameFieldRejectsUnsupportedChildrenWithActionableConfigErrors(t *testing.T) {
	tests := []struct {
		name     string
		selected string
		fields   field.Fields
		message  string
	}{
		{name: "missing", selected: "missing", fields: field.Fields{field.Text("title")}, message: "existing direct stored text child"},
		{name: "nested path", selected: "group.title", fields: field.Fields{field.Group("group", field.Fields{field.Text("title")})}, message: "existing direct stored text child"},
		{name: "nested field", selected: "group", fields: field.Fields{field.Group("group", field.Fields{field.Text("title")})}, message: "direct stored text child"},
		{name: "non text", selected: "count", fields: field.Fields{field.Number("count")}, message: "direct stored text child"},
		{name: "slug", selected: "slug", fields: field.Fields{field.Text("title"), field.Slug("slug", "title")}, message: "slug field; select ordinary text"},
		{name: "custom editor", selected: "title", fields: field.Fields{field.Text("title").Admin(field.Admin{Editor: field.Component("app:Title")})}, message: "custom editor; select text using the built-in editor"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Resolve(Input{Name: "Invalid block name", Collections: []Collection{{Slug: "pages", Fields: field.Fields{
				field.Blocks("layout", field.Block{Slug: "card", Admin: field.BlockAdmin{NameField: test.selected}, Fields: test.fields}),
			}}}})
			if err == nil || !strings.Contains(err.Error(), "collections[0].fields[0].blocks[0].admin.nameField") || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("Resolve error = %v, want name-field path and %q", err, test.message)
			}
		})
	}
}

func TestSchemaParseRejectsInvalidBlockNameFieldsWithActionableErrors(t *testing.T) {
	base, err := Resolve(Input{Name: "Schema block names", Collections: []Collection{{Slug: "pages", Fields: field.Fields{
		field.Blocks("layout", field.Block{Slug: "card", Admin: field.BlockAdmin{NameField: "title"}, Fields: field.Fields{field.Text("title")}}),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		mutate  func(*schema.BlockType)
		message string
	}{
		{name: "missing", mutate: func(block *schema.BlockType) { block.Admin.NameField = "missing" }, message: "existing direct stored text child"},
		{name: "nested path", mutate: func(block *schema.BlockType) { block.Admin.NameField = "group.title" }, message: "existing direct stored text child"},
		{name: "nested field", mutate: func(block *schema.BlockType) {
			block.Admin.NameField = "group"
			block.Fields[0].Name, block.Fields[0].Type, block.Fields[0].Category = "group", schema.FieldTypeGroup, schema.FieldCategoryNested
			block.Fields[0].Text, block.Fields[0].Nested = nil, &schema.NestedField{Fields: []schema.Field{}}
		}, message: "direct stored text child"},
		{name: "non text", mutate: func(block *schema.BlockType) {
			child := &block.Fields[0]
			child.Type, child.Text, child.Number = schema.FieldTypeNumber, nil, &schema.NumberField{}
		}, message: "direct stored text child"},
		{name: "slug", mutate: func(block *schema.BlockType) {
			path, _ := query.NewPath("title")
			block.Fields[0].Text.Slug = &schema.SlugField{SourcePath: path}
			block.Fields[0].Required, block.Fields[0].Unique, block.Fields[0].Index = true, true, true
		}, message: "slug field; select ordinary text"},
		{name: "custom editor", mutate: func(block *schema.BlockType) {
			block.Fields[0].Admin.Editor = &schema.FieldEditor{Reference: "app:Title"}
		}, message: "custom editor; select text using the built-in editor"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := base.Snapshot()
			block := &snapshot.Collections[0].Fields[0].Blocks.Types[0]
			test.mutate(block)
			encoded, encodeErr := schema.NewManifest(snapshot).Bytes()
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			_, parseErr := schema.Parse(encoded)
			if parseErr == nil || !strings.Contains(parseErr.Error(), ".admin.nameField") || !strings.Contains(parseErr.Error(), test.message) {
				t.Fatalf("Parse error = %v, want name-field path and %q", parseErr, test.message)
			}
		})
	}
	t.Run("unused registered definition", func(t *testing.T) {
		snapshot := base.Snapshot()
		definition := snapshot.Collections[0].Fields[0].Blocks.Types[0]
		definition.Admin.NameField = "missing"
		snapshot.Blocks = []schema.BlockType{definition}
		snapshot.Collections[0].Fields = nil
		encoded, encodeErr := schema.NewManifest(snapshot).Bytes()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, parseErr := schema.Parse(encoded)
		if parseErr == nil || !strings.Contains(parseErr.Error(), "blocks[0].admin.nameField") || !strings.Contains(parseErr.Error(), "existing direct stored text child") {
			t.Fatalf("Parse error = %v, want registered definition name-field diagnostic", parseErr)
		}
	})
}
