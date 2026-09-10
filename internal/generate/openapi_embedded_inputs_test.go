package generate

import (
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func TestEmbeddedOnlyOpenAPIWriteOperations(t *testing.T) {
	for _, fixture := range []struct {
		name          string
		plugin        core.Plugin
		field         func(field.Block) field.Node
		discriminator string
		identity      string
		document      func(map[string]any) map[string]any
	}{
		{
			name: "richtext", plugin: richtext.New(), discriminator: "blockType", identity: "_key",
			field: func(block field.Block) field.Node {
				return richtext.Field("body", richtext.Config{Blocks: []field.Block{block}})
			},
			document: func(payload map[string]any) map[string]any {
				return map[string]any{"version": 1, "root": map[string]any{"type": "root", "children": []any{
					map[string]any{"type": "block", "version": 1, "fields": payload},
				}}}
			},
		},
		{
			name: "outline", plugin: outline.Plugin{}, discriminator: "schema", identity: "uid",
			field: func(block field.Block) field.Node { return outline.Field("body", block) },
			document: func(payload map[string]any) map[string]any {
				return map[string]any{"outline": []any{map[string]any{"kind": "widget", "content": payload}}}
			},
		},
	} {
		for _, placement := range []string{"direct", "group"} {
			t.Run(fixture.name+"/"+placement, func(t *testing.T) {
				definition := fixture.field(field.Block{Slug: "card", Fields: field.Fields{field.Text("title").Required(), field.Relationship("target", "targets")}})
				if placement == "group" {
					definition = field.Group("content", field.Fields{definition})
				}
				fields := field.Fields{definition}
				manifest, err := core.Resolve(core.Config{
					Name: "Embedded requests", Plugins: []core.Plugin{fixture.plugin},
					Collections: []core.Collection{
						{Slug: "targets", Fields: field.Fields{field.Text("name")}},
						{Slug: "articles", Fields: fields},
					},
					Globals: []core.Global{{Slug: "settings", Fields: fields}},
				})
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := openAPI(manifest)
				if err != nil {
					t.Fatal(err)
				}
				var document openAPIDocument
				if err := json.Unmarshal(encoded, &document); err != nil {
					t.Fatal(err)
				}
				input := func(payload map[string]any) map[string]any {
					values := map[string]any{"body": fixture.document(payload)}
					if placement == "group" {
						values = map[string]any{"content": values}
					}
					return values
				}
				for _, operation := range []struct{ path, method, schema string }{
					{"/api/collections/articles", "post", "articlesCreate"},
					{"/api/collections/articles/{id}", "patch", "articlesUpdate"},
					{"/api/globals/settings", "patch", "global-settingsUpdate"},
				} {
					t.Run(operation.schema, func(t *testing.T) {
						spec := document.Paths[operation.path][operation.method].(map[string]any)
						body, ok := spec["requestBody"].(map[string]any)
						if !ok {
							t.Fatal("embedded-only operation is missing its request body schema")
						}
						ref := body["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
						if ref != "#/components/schemas/"+operation.schema {
							t.Fatalf("request schema = %v", ref)
						}
						validator := resolvedResourceSchema(t, document, operation.schema)
						fresh := input(map[string]any{fixture.discriminator: "card", "title": "Hello", "target": "target-id"})
						if err := validator.Validate(fresh); err != nil {
							t.Fatalf("keyless authoring input rejected: %v", err)
						}
						if operation.method == "patch" {
							retained := input(map[string]any{fixture.discriminator: "card", fixture.identity: "existing"})
							if err := validator.Validate(retained); err != nil {
								t.Fatalf("retained partial update rejected: %v", err)
							}
						}
						for _, invalid := range []map[string]any{
							{fixture.discriminator: "card"},
							{fixture.discriminator: "unknown", "title": "Hello"},
							{fixture.discriminator: "card", "title": "Hello", "target": map[string]any{"id": "target-id"}},
							{fixture.discriminator: "card", "title": "Hello", "unknown": "value"},
						} {
							if validator.Validate(input(invalid)) == nil {
								t.Errorf("invalid authoring payload accepted: %v", invalid)
							}
						}
					})
				}
			})
		}
	}
}
