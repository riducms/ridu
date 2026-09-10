package generate

import (
	"encoding/json"
	"fmt"

	"github.com/riducms/ridu/schema"
)

func (g openAPIBlockGenerator) embeddedName(f schema.Field, t schema.EmbeddedTree, all bool) string {
	mode := "Output"
	if g.input {
		mode = "Input"
	}
	if g.update {
		mode = "Update"
	}
	if all {
		mode += "AllLocales"
	}
	return "Embedded_" + string(f.ID) + "_" + t.Key + "_" + mode
}
func (g openAPIBlockGenerator) embeddedFieldSchema(f schema.Field, base map[string]any, all bool) map[string]any {
	parts := []any{base}
	for _, t := range f.Plugin.EmbeddedTrees {
		ref := map[string]any{"$ref": "#/components/schemas/" + g.embeddedName(f, t, all)}
		var root any = map[string]any{"anyOf": []any{ref, map[string]any{"type": "array", "items": ref}}}
		for i := len(t.Root) - 1; i >= 0; i-- {
			root = map[string]any{"type": "object", "properties": map[string]any{t.Root[i]: root}, "required": []string{t.Root[i]}}
		}
		parts = append(parts, root)
	}
	return map[string]any{"allOf": parts, "x-ridu-embeddedTrees": f.Plugin.EmbeddedTrees}
}
func (g openAPIBlockGenerator) addEmbeddedSchemas(schemas map[string]openAPISchema, snapshot schema.Snapshot, plugins map[string]json.RawMessage) error {
	var walk func([]schema.Field) error
	walk = func(fields []schema.Field) error {
		for _, f := range fields {
			if f.Plugin != nil {
				for _, t := range f.Plugin.EmbeddedTrees {
					for _, mode := range []string{"output", "input", "update", "all"} {
						gen := g
						gen.input = mode == "input" || mode == "update"
						gen.update = mode == "update"
						all := mode == "all"
						name := gen.embeddedName(f, t, all)
						branches := []any{}
						for _, c := range t.Cases {
							variants := []any{}
							for _, b := range c.ResolvedTypes() {
								object, err := gen.fieldsObject(b.ResolvedFields(), plugins, all)
								if err != nil {
									return err
								}
								properties := object["properties"].(map[string]any)
								properties[c.Discriminator] = map[string]any{"type": "string", "const": b.Slug}
								properties[c.Identity] = map[string]any{"type": "string", "minLength": 1, "pattern": `\S`}
								required, _ := object["required"].([]string)
								required = append(required, c.Discriminator)
								if !gen.input || gen.update {
									required = append(required, c.Identity)
								}
								object["required"] = required
								if gen.update {
									createGenerator := gen
									createGenerator.update = false
									authoring, err := createGenerator.fieldsObject(b.ResolvedFields(), plugins, all)
									if err != nil {
										return err
									}
									authoringProperties := authoring["properties"].(map[string]any)
									authoringProperties[c.Discriminator] = properties[c.Discriminator]
									authoringProperties[c.Identity] = properties[c.Identity]
									authoringRequired, _ := authoring["required"].([]string)
									authoring["required"] = append(authoringRequired, c.Discriminator)
									variants = append(variants, map[string]any{"anyOf": []any{authoring, object}})
								} else {
									variants = append(variants, object)
								}
							}
							payload := map[string]any{"oneOf": variants, "discriminator": map[string]any{"propertyName": c.Discriminator}}
							if len(variants) == 0 {
								payload = map[string]any{"not": map[string]any{}}
							}
							if gen.update { // A new identity still uses authoring requirements at runtime.
								payload["description"] = "Keyed payload updates retain omitted children; newly assigned identities must satisfy the create schema."
							}
							branches = append(branches, map[string]any{"if": map[string]any{"properties": map[string]any{t.Tag: map[string]any{"const": c.TagValue}}, "required": []string{t.Tag}}, "then": map[string]any{"properties": map[string]any{c.Payload: payload}, "required": []string{c.Payload}}})
						}
						schemas[name] = openAPISchema{Type: "object", Properties: map[string]any{t.Children: map[string]any{"anyOf": []any{map[string]any{"type": "null"}, map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/" + name}}}}}, AllOf: branches, Description: fmt.Sprintf("Schema-owned payloads in %s; surrounding node vocabulary is owned by the plugin.", t.Key)}
					}
				}
			}
			if err := walk(schema.ChildFields(f)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, r := range append(snapshot.Collections, snapshot.Globals...) {
		if err := walk(r.Fields); err != nil {
			return err
		}
	}
	return nil
}
