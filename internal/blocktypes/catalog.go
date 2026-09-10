// Package blocktypes assigns ordinary Blocks generated symbols from the resolved
// manifest. It does not replace persisted field identities or intern schemas.
package blocktypes

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// Field describes a named block container at an existing stable field identity.
type Field struct {
	Embedded      bool
	Name          string
	Discriminator string
	Identity      string
	Variants      []string
}

// Variant describes one reusable concrete generated type family.
type Variant struct {
	Name          string
	Discriminator string
	Identity      string
	Block         schema.BlockType
}

// Catalog is a deterministic, generation-local index into resolved schemas.
type Catalog struct {
	Fields   map[schema.StableID]Field
	Variants []Variant
}

// ValidName limits explicit names to portable exported Go/TypeScript identifiers.
func ValidName(name string) bool { return schema.IsValidBlockTypeName(name) }

// Identifier derives a portable name from a resolved slug or field path.
func Identifier(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for i, part := range parts {
		chars := []rune(part)
		chars[0] = unicode.ToUpper(chars[0])
		parts[i] = string(chars)
	}
	return strings.Join(parts, "")
}

// Build validates naming and reuse without mutating the manifest. Labels and
// presentation metadata never participate in a block's resolved shape.
func Build(snapshot schema.Snapshot) (*Catalog, error) {
	result := &Catalog{Fields: make(map[schema.StableID]Field)}
	type claim struct {
		shape, path, owner string
		localized          bool
		normalized         string
		index              int
	}
	symbols := map[string]claim{}
	shapes := map[string]claim{}
	var issues []schema.Issue
	reserve := func(name, owner, path string) {
		if prior, ok := symbols[name]; ok && prior.owner != owner {
			issues = append(issues, schema.Issue{Code: "block_type_name_conflict", Path: path, Message: fmt.Sprintf("generated name %q conflicts with %s at %s", name, prior.owner, prior.path)})
		} else {
			symbols[name] = claim{owner: owner, path: path}
		}
	}
	for _, name := range []string{"ID", "Locale", "RiduConfig", "ScalarWhere", "TimestampWhere", "MultiSelectWhere", "ExistsWhere", "ContractError", "Reference", "Array", "Record", "Partial", "Readonly", "Omit", "Pick", "Promise", "ReturnType", "Parameters", "Extract", "Exclude", "NonNullable", "RiduClient", "ClientOptions", "Symbol", "RiduLocalizedValues", "CollectionSlug", "GlobalSlug", "BlockOptional"} {
		reserve(name, "framework", "generated")
	}
	resources := append(append([]schema.Collection{}, snapshot.Collections...), snapshot.Globals...)
	for _, resource := range resources {
		bases := []string{Identifier(string(resource.ID)), Identifier(resource.Labels.Singular)}
		if resource.Capabilities.Global {
			bases = append(bases, Identifier(string(resource.Slug)))
		}
		for _, base := range bases {
			if base == "" {
				continue
			}
			for _, suffix := range []string{"", "Create", "Update", "AllLocales", "Where", "Select", "Populate", "PopulateOutput", "AllLocalesPopulateOutput", "PopulationSelect", "ValidationPath"} {
				if _, exists := symbols[base+suffix]; !exists {
					symbols[base+suffix] = claim{owner: "resource " + string(resource.ID), path: string(resource.Slug)}
				}
			}
		}
		binding := Identifier(resource.Labels.Plural) + "Collection"
		if resource.Capabilities.Global {
			binding = Identifier(resource.Labels.Singular) + "Global"
		}
		for _, suffix := range []string{"", "Read", "AllLocales"} {
			if _, exists := symbols[binding+suffix]; !exists {
				symbols[binding+suffix] = claim{owner: "resource binding " + string(resource.ID), path: string(resource.Slug)}
			}
		}
	}
	wire := map[schema.StableID][2]string{}
	var walk func([]schema.Field, string, string, bool, bool)
	walk = func(fields []schema.Field, prefix, path string, insideBlock, localizedAncestor bool) {
		for _, field := range fields {
			fieldPath := path + "." + field.Name
			if field.Plugin != nil {
				virtual := schema.EmbeddedBlocks(field)
				k := 0
				for _, tree := range field.Plugin.EmbeddedTrees {
					for _, c := range tree.Cases {
						wire[virtual[k].ID] = [2]string{c.Discriminator, c.Identity}
						k++
					}
				}
				walk(virtual, prefix+Identifier(field.Name), path+"."+field.Name, insideBlock, localizedAncestor || field.Localized)
			}
			name := prefix + Identifier(field.Name)
			if field.Blocks != nil {
				keys := [2]string{"blockType", "_key"}
				if configured, ok := wire[field.ID]; ok {
					keys = configured
				}
				_, embedded := wire[field.ID]
				container := Field{Name: name, Discriminator: keys[0], Identity: keys[1], Embedded: embedded}
				for _, block := range field.Blocks.ResolvedTypes() {
					blockPath := fieldPath + ".blocks." + block.Slug + ".typeName"
					variantName := block.TypeName
					if variantName == "" {
						variantName = name + Identifier(block.Slug)
					}
					if !ValidName(variantName) {
						issues = append(issues, schema.Issue{Code: "invalid_block_type_name", Path: blockPath, Message: "block type name must be an exported identifier containing only ASCII letters and digits"})
						continue
					}
					shape, err := Shape(block)
					shape = keys[0] + "/" + keys[1] + "/" + shape
					suppressed := localizedAncestor || field.Localized
					normalizedBlock := block
					normalizedBlock.Fields = suppressLocalization(block.ResolvedFields())
					normalized, _ := Shape(normalizedBlock)
					normalized = keys[0] + "/" + keys[1] + "/" + normalized
					if err != nil {
						issues = append(issues, schema.Issue{Code: "invalid_block_shape", Path: blockPath, Message: err.Error()})
						continue
					}
					if prior, ok := shapes[variantName]; ok {
						if prior.shape != shape && (!(prior.localized || suppressed) || prior.normalized != normalized) {
							issues = append(issues, schema.Issue{Code: "block_type_name_conflict", Path: blockPath, Message: fmt.Sprintf("block type name %q has a different resolved shape at %s", variantName, prior.path)})
						}
						if prior.localized && !suppressed {
							result.Variants[prior.index].Block = block
							shapes[variantName] = claim{shape: shape, path: blockPath, normalized: normalized, index: prior.index}
						}
					} else {
						shapes[variantName] = claim{shape: shape, path: blockPath, localized: suppressed, normalized: normalized, index: len(result.Variants)}
						result.Variants = append(result.Variants, Variant{Name: variantName, Block: block, Discriminator: keys[0], Identity: keys[1]})
					}
					reserve(variantName+"BlockType", "block "+variantName, blockPath)
					for _, suffix := range []string{"", "Input", "Update", "AllLocales", "AllLocalesValue"} {
						reserve(variantName+suffix, "block "+variantName, blockPath)
					}
					container.Variants = append(container.Variants, variantName)
					// Descendants of explicitly named variants share their parent's symbols,
					// while retaining each occurrence's stable field identity in Fields.
					walk(block.ResolvedFields(), variantName, fieldPath+".blocks."+block.Slug, true, suppressed)
				}
				for _, suffix := range []string{"", "Input", "AllLocales", "Block", "BlockInput", "BlockAllLocales", "InputBlock", "AllLocalesBlock", "AllLocalesValue", "BlockAllLocalesValue", "AllLocalesValueBlock", "Update", "BlockUpdate", "UpdateBlock"} {
					reserve(name+suffix, "container "+name+" ("+field.Name+")", fieldPath)
				}
				if embedded {
					for _, suffix := range []string{"Payload", "InputPayload", "UpdatePayload", "AllLocalesPayload", "AllLocalesValuePayload"} {
						reserve(name+suffix, "embedded payload "+name, fieldPath)
					}
				}
				result.Fields[field.ID] = container
			} else if field.Nested != nil {
				if insideBlock {
					nestedName := name
					if field.Type == schema.FieldTypeArray {
						nestedName += "Row"
					}
					for _, suffix := range []string{"", "Input", "Update", "AllLocales", "AllLocalesValue"} {
						reserve(nestedName+suffix, "nested "+name+" ("+field.Name+")", fieldPath)
					}
				}
				childPrefix := name
				if insideBlock && field.Type == schema.FieldTypeArray {
					childPrefix += "Row"
				}
				walk(field.Nested.ResolvedFields(), childPrefix, fieldPath, insideBlock, localizedAncestor || field.Localized)
			}
		}
	}
	for _, resource := range resources {
		prefix := Identifier(string(resource.Slug))
		if resource.Capabilities.Global {
			prefix = "Global" + prefix
		}
		walk(resource.Fields, prefix, string(resource.Slug)+".fields", false, false)
	}
	if len(issues) > 0 {
		return nil, schema.NewValidationError(issues)
	}
	sort.Slice(result.Variants, func(i, j int) bool { return result.Variants[i].Name < result.Variants[j].Name })
	return result, nil
}

// Shape compares resolved value contracts, excluding placement and author-facing
// metadata. It deliberately retains defaults, validation and reference targets.
func Shape(block schema.BlockType) (string, error) {
	block.TypeName = ""
	block.Labels = schema.BlockLabels{}
	block.Admin = nil
	block.Fields = shapeFields(block.ResolvedFields())
	data, err := json.Marshal(block)
	return string(data), err
}

func shapeFields(fields []schema.Field) []schema.Field {
	result := make([]schema.Field, 0, len(fields))
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation && field.Type != schema.FieldTypeVirtual && field.Type != schema.FieldTypeJoin {
			continue
		}
		field.ID = ""
		field.Path, _ = query.NewPath(field.Name)
		field.Admin = schema.FieldAdmin{}
		if field.Select != nil {
			selectField := *field.Select
			selectField.Options = append([]schema.SelectOption{}, field.Select.Options...)
			for i := range selectField.Options {
				selectField.Options[i].Label = ""
				selectField.Options[i].LabelTranslations = nil
			}
			field.Select = &selectField
		}
		if field.Number != nil {
			number := *field.Number
			number.Step = nil
			field.Number = &number
		}
		if field.Code != nil {
			code := *field.Code
			code.Language = ""
			field.Code = &code
		}
		if field.Nested != nil {
			nested := *field.Nested
			nested.RowLabelComponent = nil
			nested.RowLabel = ""
			nested.RowLabels = nil
			nested.Fields = shapeFields(nested.ResolvedFields())
			field.Nested = &nested
		}
		if field.Blocks != nil {
			blocks := *field.Blocks
			blocks.BlockReferences = nil
			blocks.Types = make([]schema.BlockType, len(field.Blocks.ResolvedTypes()))
			for i, block := range field.Blocks.ResolvedTypes() {
				block.Labels = schema.BlockLabels{}
				block.Admin = nil
				block.Fields = shapeFields(block.ResolvedFields())
				blocks.ResolvedTypes()[i] = block
			}
			field.Blocks = &blocks
		}
		if field.Plugin != nil {
			plugin := *field.Plugin
			plugin.EmbeddedTrees = append([]schema.EmbeddedTree(nil), plugin.EmbeddedTrees...)
			for i := range plugin.EmbeddedTrees {
				plugin.EmbeddedTrees[i].Cases = append([]schema.EmbeddedTreeCase(nil), plugin.EmbeddedTrees[i].Cases...)
				for j := range plugin.EmbeddedTrees[i].Cases {
					c := &plugin.EmbeddedTrees[i].Cases[j]
					c.Types = append([]schema.BlockType(nil), c.ResolvedTypes()...)
					c.BlockReferences = nil
					for k := range c.ResolvedTypes() {
						b := &c.ResolvedTypes()[k]
						b.Labels = schema.BlockLabels{}
						b.Admin = nil
						b.Fields = shapeFields(b.ResolvedFields())
					}
				}
			}
			field.Plugin = &plugin
		}
		result = append(result, field)
	}
	return result
}

// A localized ancestor stores one projected child tree per locale. This is a
// contextual transformation, not a different reusable block definition.
func suppressLocalization(fields []schema.Field) []schema.Field {
	result := append([]schema.Field{}, fields...)
	for i := range result {
		result[i].Localized = false
		if result[i].Plugin != nil {
			plugin := *result[i].Plugin
			plugin.EmbeddedTrees = append([]schema.EmbeddedTree(nil), plugin.EmbeddedTrees...)
			for t := range plugin.EmbeddedTrees {
				plugin.EmbeddedTrees[t].Cases = append([]schema.EmbeddedTreeCase(nil), plugin.EmbeddedTrees[t].Cases...)
				for c := range plugin.EmbeddedTrees[t].Cases {
					v := &plugin.EmbeddedTrees[t].Cases[c]
					v.Types = append([]schema.BlockType(nil), v.ResolvedTypes()...)
					for b := range v.ResolvedTypes() {
						v.ResolvedTypes()[b].Fields = suppressLocalization(v.ResolvedTypes()[b].ResolvedFields())
					}
				}
			}
			result[i].Plugin = &plugin
		}
		if result[i].Nested != nil {
			nested := *result[i].Nested
			nested.Fields = suppressLocalization(nested.ResolvedFields())
			result[i].Nested = &nested
		}
		if result[i].Blocks != nil {
			blocks := *result[i].Blocks
			blocks.Types = append([]schema.BlockType{}, blocks.ResolvedTypes()...)
			for j := range blocks.ResolvedTypes() {
				blocks.ResolvedTypes()[j].Fields = suppressLocalization(blocks.ResolvedTypes()[j].ResolvedFields())
			}
			result[i].Blocks = &blocks
		}
	}
	return result
}
