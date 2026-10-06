// Package blockspec builds the Ridu configuration of the block-heavy benchmark from the
// framework-neutral JSON spec written by ../spec.ts. The serving fixture (../ridu) and the
// migration-planning probe (../planning) share it, so both measure the same schema.
package blockspec

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
)

// Field is one field of a block or resource.
type Field struct {
	Type       string   `json:"type"`
	Name       string   `json:"name"`
	Required   bool     `json:"required,omitempty"`
	Options    []string `json:"options,omitempty"`
	RelationTo string   `json:"relationTo,omitempty"`
	Fields     []Field  `json:"fields,omitempty"`
	Blocks     []string `json:"blocks,omitempty"`
}

// Block is one registry block definition.
type Block struct {
	Slug   string  `json:"slug"`
	Fields []Field `json:"fields"`
}

// Resource is a collection or global.
type Resource struct {
	Slug   string  `json:"slug"`
	Drafts bool    `json:"drafts"`
	Fields []Field `json:"fields"`
}

// Spec is one scenario and declaration variant.
type Spec struct {
	Scenario   string `json:"scenario"`
	References bool   `json:"references"`
	// Hooks gives the first text field of every leaf block executable behavior; see hookedField.
	Hooks       bool       `json:"hooks"`
	Blocks      []Block    `json:"blocks"`
	Collections []Resource `json:"collections"`
	Globals     []Resource `json:"globals"`
}

// Read decodes the spec at path.
func Read(path string) (Spec, error) {
	if path == "" {
		return Spec{}, errors.New("RIDU_BLOCKS_SPEC must name the JSON spec written by spec.ts")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, err
	}
	var spec Spec
	if err := json.Unmarshal(content, &spec); err != nil {
		return Spec{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return spec, nil
}

// Config maps the spec onto Ridu authoring config. Reference specs register every block in
// ridu.Config.Blocks and select them by slug; inline specs declare one shared field.Block value
// per slug with an explicit TypeName, the documented way to reuse an inline definition.
func Config(spec Spec, plugins []ridu.Plugin) (ridu.Config, error) {
	definitions := make(map[string]Block, len(spec.Blocks))
	for _, block := range spec.Blocks {
		definitions[block.Slug] = block
	}
	inline := map[string]field.Block{}
	var buildFields func([]Field) (field.Fields, error)
	// blockFields builds a definition's fields. A hooks spec gives the first text field of a
	// leaf block executable behavior; every other item builds to one field at its own index.
	blockFields := func(definition Block) (field.Fields, error) {
		fields, err := buildFields(definition.Fields)
		if err != nil || !spec.Hooks || containsBlocks(definition.Fields) {
			return fields, err
		}
		for index, item := range definition.Fields {
			if item.Type == "text" {
				fields[index] = hookedText(field.Text(item.Name).Required(item.Required))
				break
			}
		}
		return fields, nil
	}
	var inlineBlock func(string) (field.Block, error)
	inlineBlock = func(slug string) (field.Block, error) {
		if block, ok := inline[slug]; ok {
			return block, nil
		}
		definition, ok := definitions[slug]
		if !ok {
			return field.Block{}, fmt.Errorf("unknown block %q", slug)
		}
		fields, err := blockFields(definition)
		if err != nil {
			return field.Block{}, err
		}
		block := field.Block{Slug: slug, TypeName: typeName(slug), Fields: fields}
		inline[slug] = block
		return block, nil
	}
	buildFields = func(specs []Field) (field.Fields, error) {
		fields := make(field.Fields, 0, len(specs))
		for _, item := range specs {
			switch item.Type {
			case "text":
				fields = append(fields, field.Text(item.Name).Required(item.Required))
			case "textarea":
				fields = append(fields, field.Textarea(item.Name).Required(item.Required))
			case "number":
				fields = append(fields, field.Number(item.Name))
			case "checkbox":
				fields = append(fields, field.Checkbox(item.Name))
			case "date":
				// Payload date fields store instants, so match them with RFC 3339 timestamps.
				fields = append(fields, field.Date(item.Name).Format(field.DateTime))
			case "select":
				fields = append(fields, field.Select(item.Name, item.Options...))
			case "relationship":
				fields = append(fields, field.Relationship(item.Name, schema.CollectionSlug(item.RelationTo)))
			case "group", "array":
				children, err := buildFields(item.Fields)
				if err != nil {
					return nil, err
				}
				if item.Type == "group" {
					fields = append(fields, field.Group(item.Name, children))
				} else {
					fields = append(fields, field.Array(item.Name, children))
				}
			case "blocks":
				if spec.References {
					fields = append(fields, field.Blocks(item.Name).References(item.Blocks...))
					continue
				}
				blocks := make([]field.Block, 0, len(item.Blocks))
				for _, slug := range item.Blocks {
					block, err := inlineBlock(slug)
					if err != nil {
						return nil, err
					}
					blocks = append(blocks, block)
				}
				fields = append(fields, field.Blocks(item.Name, blocks...))
			default:
				return nil, fmt.Errorf("unsupported field type %q", item.Type)
			}
		}
		return fields, nil
	}

	config := ridu.Config{
		Name:  "Ridu block benchmark " + spec.Scenario,
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug:   "users",
			Auth:   true,
			Admin:  ridu.CollectionAdmin{UseAsTitle: "email"},
			Fields: field.Fields{field.Email("email").Required().Unique()},
			Access: ridu.CollectionAccess{
				Admin:  allowAuthenticated,
				Read:   allowAuthenticated,
				Update: allowAuthenticated,
				Delete: allowAuthenticated,
			},
		}},
		Plugins: plugins,
	}
	if spec.References {
		for _, definition := range spec.Blocks {
			fields, err := blockFields(definition)
			if err != nil {
				return ridu.Config{}, err
			}
			config.Blocks = append(config.Blocks, field.Block{Slug: definition.Slug, Fields: fields})
		}
	}
	for _, resource := range spec.Collections {
		fields, err := buildFields(resource.Fields)
		if err != nil {
			return ridu.Config{}, err
		}
		collection := ridu.Collection{
			Slug:   schema.CollectionSlug(resource.Slug),
			Admin:  ridu.CollectionAdmin{UseAsTitle: "title"},
			Fields: fields,
			Access: ridu.CollectionAccess{
				Create: allowAuthenticated,
				Read:   allowEveryone,
				Update: allowAuthenticated,
				Delete: allowAuthenticated,
			},
		}
		if resource.Drafts {
			collection.Versions = true
			collection.VersionConfig = ridu.VersionConfig{Drafts: true, MaxPerDocument: 50}
		}
		config.Collections = append(config.Collections, collection)
	}
	for _, resource := range spec.Globals {
		fields, err := buildFields(resource.Fields)
		if err != nil {
			return ridu.Config{}, err
		}
		global := ridu.Global{
			Slug:   schema.CollectionSlug(resource.Slug),
			Fields: fields,
			Access: ridu.GlobalAccess{Read: allowEveryone, Update: allowAuthenticated},
		}
		if resource.Drafts {
			global.Versions = true
			global.VersionConfig = ridu.VersionConfig{Drafts: true, MaxPerDocument: 50}
		}
		config.Globals = append(config.Globals, global)
	}
	return config, nil
}

func containsBlocks(fields []Field) bool {
	for _, item := range fields {
		if item.Type == "blocks" || containsBlocks(item.Fields) {
			return true
		}
	}
	return false
}

// hookedText gives a hooks spec's leaf text field every kind of executable field behavior. The
// callbacks are package-level values, shared by every definition, and leave the workload's
// content unchanged: its text has no surrounding space, never equals the rejected value, and is
// always supplied, so the default only runs for an omitted value.
func hookedText(text field.TextField) field.TextField {
	return text.
		Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{trimText}}).
		Validate(rejectPlaceholder).
		// Globals place leaf blocks too, and global fields admit initialization through Update.
		Access(field.Access{Read: allowRead, Update: allowWrite}).
		DefaultFrom(defaultText).
		Admin(field.Admin{VisibleWhen: field.NotEqual(field.Sibling("blockName"), "hidden")})
}

func trimText(_ operation.Context, value operation.Value[string]) (operation.Change[string], error) {
	text, present := value.Get()
	if trimmed := strings.TrimSpace(text); present && trimmed != text {
		return operation.Set(trimmed), nil
	}
	return operation.Keep[string](), nil
}

func rejectPlaceholder(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
	if text, _ := value.Get(); text == "TODO" {
		return []operation.Issue{{Code: "placeholder_text", Message: "Replace the placeholder text"}}, nil
	}
	return nil, nil
}

func allowRead(ctx operation.Context) (bool, error) { return ctx.Operation != "", nil }

func allowWrite(ctx operation.Context) (bool, error) { return ctx.Actor.ID != "", nil }

func defaultText(ctx operation.Context) (operation.Value[string], error) {
	if ctx.Actor.ID == "" {
		return operation.Empty[string](), nil
	}
	return operation.Present("Untitled"), nil
}

func typeName(slug string) string {
	var out strings.Builder
	for _, part := range strings.Split(slug, "-") {
		out.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return out.String()
}

func allowEveryone(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }

func allowAuthenticated(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor == nil {
		return ridu.Deny(), nil
	}
	return ridu.Allow(), nil
}
