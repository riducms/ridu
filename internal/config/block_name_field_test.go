package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestBlockNameResolvesAcrossInlineRegisteredAndEmbeddedSchemas(t *testing.T) {
	block := field.Block{
		Slug: "card", Admin: field.BlockAdmin{RowLabelPath: "count"},
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
		if resolved.Admin == nil || resolved.Admin.RowLabel != "count" {
			t.Fatalf("block admin metadata = %#v", resolved.Admin)
		}
		name, found := resolvedDirectField(resolved.ResolvedFields(), "blockName")
		if !found || name.Type != schema.FieldTypeText || name.Required || name.Default == nil || *name.Default != "" || name.Admin.Label != "Block name" {
			t.Fatalf("resolved block name = %#v", name)
		}
	}

	// Registry and placement views remain detached while the manifest stays compact.
	registeredSnapshot.Blocks[0].Fields[2].Name = "changed"
	again := registered.Snapshot()
	if again.Blocks[0].ResolvedFields()[2].Name != "blockName" || again.Collections[0].Fields[0].Blocks.ResolvedTypes()[0].ResolvedFields()[2].Name != "blockName" {
		t.Fatal("block name mutated through a snapshot view")
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
	if len(registeredField.Blocks.Types) != 0 || registeredField.Blocks.ResolvedTypes()[0].ResolvedFields()[2].Name != "blockName" {
		t.Fatal("block name expanded or disappeared from compact registered metadata")
	}
	encoded, err := embedded.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := schema.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := resolvedDirectField(parsed.Snapshot().Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].ResolvedFields(), "blockName"); !found {
		t.Fatal("embedded block name disappeared during schema parsing")
	}
}

func TestBlockNameAllowsAuthoredTextAndRejectsIncompatibleFields(t *testing.T) {
	custom, err := Resolve(Input{Name: "Custom block name", Collections: []Collection{{Slug: "pages", Fields: field.Fields{
		field.Blocks("layout", field.Block{Slug: "card", Fields: field.Fields{field.Text("blockName").Label("Editorial name").MaxLength(120).Default("Draft")}}),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	fields := custom.Snapshot().Collections[0].Fields[0].Blocks.ResolvedTypes()[0].ResolvedFields()
	if len(fields) != 1 || fields[0].Default == nil || *fields[0].Default != "Draft" || fields[0].Text.MaxLength == nil || *fields[0].Text.MaxLength != 120 || fields[0].Admin.Label != "Editorial name" {
		t.Fatalf("authored block name changed: %#v", fields)
	}

	tests := []struct {
		name    string
		field   field.Node
		message string
	}{
		{name: "non text", field: field.Number("blockName"), message: "direct stored text child"},
		{name: "slug", field: field.Slug("blockName", "title"), message: "slug field; select ordinary text"},
		{name: "custom editor", field: field.Text("blockName").Admin(field.Admin{Editor: field.Component("app:Name")}), message: "custom editor; select text using the built-in editor"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Resolve(Input{Name: "Invalid block name", Collections: []Collection{{Slug: "pages", Fields: field.Fields{
				field.Blocks("layout", field.Block{Slug: "card", Fields: field.Fields{field.Text("title"), test.field}}),
			}}}})
			if err == nil || !strings.Contains(err.Error(), "collections[0].fields[0].blocks[0].fields.blockName") || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("Resolve error = %v, want blockName path and %q", err, test.message)
			}
		})
	}
}

func TestSchemaParseRejectsInvalidBlockName(t *testing.T) {
	base, err := Resolve(Input{Name: "Schema block names", Collections: []Collection{{Slug: "pages", Fields: field.Fields{
		field.Blocks("layout", field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("missing inline name", func(t *testing.T) {
		snapshot := base.Snapshot()
		block := &snapshot.Collections[0].Fields[0].Blocks.Types[0]
		block.Fields = block.Fields[:1]
		encoded, encodeErr := schema.NewManifest(snapshot).Bytes()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, parseErr := schema.Parse(encoded)
		if parseErr == nil || !strings.Contains(parseErr.Error(), "collections[0].fields[0].blocks.types[0].fields.blockName") {
			t.Fatalf("Parse error = %v, want missing inline block name", parseErr)
		}
	})
	tests := []struct {
		name    string
		mutate  func(*schema.Field)
		message string
	}{
		{name: "non text", mutate: func(child *schema.Field) {
			child.Type, child.Text, child.Number = schema.FieldTypeNumber, nil, &schema.NumberField{}
		}, message: "direct stored text child"},
		{name: "slug", mutate: func(child *schema.Field) {
			path, _ := query.NewPath("title")
			child.Text.Slug = &schema.SlugField{SourcePath: path}
			child.Required, child.Unique, child.Index = true, true, true
		}, message: "slug field; select ordinary text"},
		{name: "custom editor", mutate: func(child *schema.Field) {
			child.Admin.Editor = &schema.FieldEditor{Reference: "app:Name"}
		}, message: "custom editor; select text using the built-in editor"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := base.Snapshot()
			block := &snapshot.Collections[0].Fields[0].Blocks.Types[0]
			test.mutate(&block.Fields[1])
			encoded, encodeErr := schema.NewManifest(snapshot).Bytes()
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			_, parseErr := schema.Parse(encoded)
			if parseErr == nil || !strings.Contains(parseErr.Error(), ".fields.blockName") || !strings.Contains(parseErr.Error(), test.message) {
				t.Fatalf("Parse error = %v, want blockName path and %q", parseErr, test.message)
			}
		})
	}
	t.Run("unused registered definition", func(t *testing.T) {
		snapshot := base.Snapshot()
		definition := snapshot.Collections[0].Fields[0].Blocks.Types[0]
		definition.Fields[1].Type, definition.Fields[1].Text, definition.Fields[1].Number = schema.FieldTypeNumber, nil, &schema.NumberField{}
		snapshot.Blocks = []schema.BlockType{definition}
		snapshot.Collections[0].Fields = nil
		encoded, encodeErr := schema.NewManifest(snapshot).Bytes()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, parseErr := schema.Parse(encoded)
		if parseErr == nil || !strings.Contains(parseErr.Error(), "blocks[0].fields.blockName") {
			t.Fatalf("Parse error = %v, want registered definition name diagnostic", parseErr)
		}
	})
	t.Run("missing registered name", func(t *testing.T) {
		snapshot := base.Snapshot()
		definition := snapshot.Collections[0].Fields[0].Blocks.Types[0]
		definition.Fields = definition.Fields[:1]
		snapshot.Blocks = []schema.BlockType{definition}
		snapshot.Collections[0].Fields = nil
		encoded, encodeErr := schema.NewManifest(snapshot).Bytes()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, parseErr := schema.Parse(encoded)
		if parseErr == nil || !strings.Contains(parseErr.Error(), "blocks[0].fields.blockName") {
			t.Fatalf("Parse error = %v, want missing registered block name", parseErr)
		}
	})
	t.Run("missing embedded name", func(t *testing.T) {
		tree := field.EmbeddedTree{
			Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind",
			Cases: []field.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []field.Block{{Slug: "card", Fields: field.Fields{field.Text("title")}}}}},
		}
		embedded, resolveErr := Resolve(Input{Name: "Embedded block names", Plugins: []Plugin{{Key: "outline"}}, Collections: []Collection{{Slug: "pages", Fields: field.Fields{
			field.Plugin("body", "outline", json.RawMessage(`{}`)).EmbeddedTrees(tree),
		}}}})
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		snapshot := embedded.Snapshot()
		block := &snapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Cases[0].Types[0]
		block.Fields = block.Fields[:1]
		encoded, encodeErr := schema.NewManifest(snapshot).Bytes()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, parseErr := schema.Parse(encoded)
		if parseErr == nil || !strings.Contains(parseErr.Error(), "plugin.embeddedTrees[0].cases[0].types[0].fields.blockName") {
			t.Fatalf("Parse error = %v, want missing embedded block name", parseErr)
		}
	})
}
