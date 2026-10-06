package typescript

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/riducms/ridu/schema"
)

// Dotted-path contracts (where filters, population paths and validation paths)
// are generated once per definition. A collection, global or block variant
// lists its own paths relative to its root, descending through groups and
// arrays, and refers to each nested block variant's contract under the prefix
// of that placement. Reused block graphs therefore produce output proportional
// to their definitions instead of to every root-to-leaf placement path.

// blockPlacement is one block variant placed under a relative path prefix that
// ends in the block slug and a separating dot.
type blockPlacement struct {
	prefix  string
	variant string
}

// pathEntry is one dotted path owned directly by a definition.
type pathEntry struct {
	path  string
	field schema.Field
}

// pathDefinition is a definition's own paths and its nested block placements.
type pathDefinition struct {
	entries    []pathEntry
	placements []blockPlacement
}

// pathContracts generates the where and populate contracts of block variants
// on demand and remembers which variants contribute any path.
type pathContracts struct {
	generator    *clientGenerator
	variants     map[string]schema.BlockType
	where        map[string]pathDefinition
	populate     map[string]pathDefinition
	whereUsed    map[string]bool
	populateUsed map[string]bool
	err          error
}

func newPathContracts(generator *clientGenerator) *pathContracts {
	variants := make(map[string]schema.BlockType, len(generator.blocks.Variants))
	for _, variant := range generator.blocks.Variants {
		variants[variant.Name] = variant.Block
	}
	return &pathContracts{
		generator:    generator,
		variants:     variants,
		where:        map[string]pathDefinition{},
		populate:     map[string]pathDefinition{},
		whereUsed:    map[string]bool{},
		populateUsed: map[string]bool{},
	}
}

// placements names the variant of every block type a container accepts. The
// catalog records one variant per resolved block type, in declaration order.
func (contracts *pathContracts) placements(container schema.Field, path string) []blockPlacement {
	if container.Blocks == nil {
		return nil
	}
	types := container.Blocks.ResolvedTypes()
	definition, ok := contracts.generator.blocks.Fields[container.ID]
	if !ok || len(definition.Variants) != len(types) {
		if contracts.err == nil {
			contracts.err = fmt.Errorf("generated block catalog does not describe field %s", container.ID)
		}
		return nil
	}
	result := make([]blockPlacement, len(types))
	for index, block := range types {
		result[index] = blockPlacement{prefix: path + "." + block.Slug + ".", variant: definition.Variants[index]}
	}
	return result
}

// whereDefinition mirrors the REST decoder's canonical dotted-path grammar.
// Group, array and block containers expose their own existence comparison and
// act as traversal nodes. Read-restricted fields and their descendants cannot
// be queried by callers.
func (contracts *pathContracts) whereDefinition(fields []schema.Field) pathDefinition {
	var definition pathDefinition
	var walk func([]schema.Field, string)
	walk = func(fields []schema.Field, prefix string) {
		for _, candidate := range fields {
			if candidate.Category == schema.FieldCategoryPresentation || candidate.QueryRestricted {
				continue
			}
			path := prefix + candidate.Name
			definition.entries = append(definition.entries, pathEntry{path: path, field: candidate})
			switch candidate.Type {
			case schema.FieldTypeGroup, schema.FieldTypeArray:
				if candidate.Nested != nil {
					walk(candidate.Nested.ResolvedFields(), path+".")
				}
			case schema.FieldTypeBlocks:
				definition.placements = append(definition.placements, contracts.placements(candidate, path)...)
			}
		}
	}
	walk(fields, "")
	definition.placements = contracts.usedPlacements(definition.placements, contracts.variantWhere)
	return definition
}

// populateDefinition lists relationship and upload paths, including those in
// array rows, blocks and plugin-embedded block trees, by canonical schema path.
func (contracts *pathContracts) populateDefinition(fields []schema.Field) pathDefinition {
	var definition pathDefinition
	var walk func([]schema.Field, string)
	walk = func(fields []schema.Field, prefix string) {
		for _, candidate := range fields {
			path := prefix + candidate.Name
			switch {
			case candidate.Type == schema.FieldTypeRelationship || candidate.Type == schema.FieldTypeUpload:
				definition.entries = append(definition.entries, pathEntry{path: path, field: candidate})
				continue
			case candidate.Blocks != nil:
				definition.placements = append(definition.placements, contracts.placements(candidate, path)...)
			case candidate.Plugin != nil:
				// Embedded trees are reached through virtual containers whose canonical
				// paths append the tree key and case tag to the plugin field path.
				virtual := schema.EmbeddedBlocks(candidate)
				index := 0
				for _, tree := range candidate.Plugin.EmbeddedTrees {
					for _, treeCase := range tree.Cases {
						definition.placements = append(definition.placements, contracts.placements(virtual[index], path+"."+tree.Key+"."+treeCase.TagValue)...)
						index++
					}
				}
			}
			if candidate.Nested != nil {
				walk(candidate.Nested.ResolvedFields(), path+".")
			}
		}
	}
	walk(fields, "")
	definition.placements = contracts.usedPlacements(definition.placements, contracts.variantPopulate)
	return definition
}

// usedPlacements drops placements of variants that contribute no path and
// marks the remainder for emission.
func (contracts *pathContracts) usedPlacements(placements []blockPlacement, variant func(string) pathDefinition) []blockPlacement {
	result := placements[:0]
	for _, placement := range placements {
		definition := variant(placement.variant)
		if len(definition.entries) != 0 || len(definition.placements) != 0 {
			result = append(result, placement)
		}
	}
	return result
}

func (contracts *pathContracts) variantWhere(name string) pathDefinition {
	if definition, ok := contracts.where[name]; ok {
		return definition
	}
	// Block graphs are acyclic, so the placeholder is never observed.
	contracts.where[name] = pathDefinition{}
	definition := contracts.whereDefinition(contracts.variants[name].ResolvedFields())
	contracts.where[name] = definition
	contracts.whereUsed[name] = len(definition.entries) != 0 || len(definition.placements) != 0
	return definition
}

func (contracts *pathContracts) variantPopulate(name string) pathDefinition {
	if definition, ok := contracts.populate[name]; ok {
		return definition
	}
	contracts.populate[name] = pathDefinition{}
	definition := contracts.populateDefinition(contracts.variants[name].ResolvedFields())
	contracts.populate[name] = definition
	contracts.populateUsed[name] = len(definition.entries) != 0 || len(definition.placements) != 0
	return definition
}

// writePathInterface writes an exported interface that inherits every nested
// block placement's relative paths under that placement's prefix.
func writePathInterface(output *bytes.Buffer, name string, placements []blockPlacement, suffix string, members []string) {
	fmt.Fprintf(output, "export interface %s", name)
	if len(placements) != 0 {
		output.WriteString(" extends")
		for index, placement := range placements {
			if index != 0 {
				output.WriteString(",")
			}
			fmt.Fprintf(output, "\n\tRiduPrefixedPaths<%s, %s%s>", strconv.Quote(placement.prefix), placement.variant, suffix)
		}
	}
	if len(members) == 0 {
		output.WriteString(" {}\n\n")
		return
	}
	output.WriteString(" {\n")
	for _, member := range members {
		output.WriteString("\t" + member + "\n")
	}
	output.WriteString("}\n\n")
}

func (generator *clientGenerator) whereMembers(definition pathDefinition) []string {
	members := make([]string, len(definition.entries))
	for index, entry := range definition.entries {
		members[index] = property(entry.path) + "?: " + generator.whereType(entry.field) + ";"
	}
	return members
}

func (generator *clientGenerator) populateMembers(definition pathDefinition) []string {
	members := make([]string, len(definition.entries))
	for index, entry := range definition.entries {
		members[index] = property(entry.path) + "?: " + generator.populateType(entry.field) + ";"
	}
	return members
}

// writeVariantContracts emits the relative where and populate contracts of the
// block variants reached from at least one resource, in catalog order.
func (contracts *pathContracts) writeVariantContracts(output *bytes.Buffer) {
	for _, variant := range contracts.generator.blocks.Variants {
		if contracts.whereUsed[variant.Name] {
			fmt.Fprintf(output, "/** Filters relative to one %s block row; a resource filter prefixes them with the row's path. */\n", variant.Name)
			writePathInterface(output, variant.Name+"Where", contracts.where[variant.Name].placements, "Where", contracts.generator.whereMembers(contracts.where[variant.Name]))
		}
	}
	for _, variant := range contracts.generator.blocks.Variants {
		if contracts.populateUsed[variant.Name] {
			fmt.Fprintf(output, "/** Population paths relative to one %s block row. */\n", variant.Name)
			writePathInterface(output, variant.Name+"Populate", contracts.populate[variant.Name].placements, "Populate", contracts.generator.populateMembers(contracts.populate[variant.Name]))
		}
	}
}

// validationPaths generates the canonical issue-path vocabulary of writable
// fields, including locale and row-index segments. Nested block rows refer to
// each variant's own vocabulary. Inside a localized ancestor the manifest
// suppresses descendant localization, so such placements use the variant's
// unlocalized vocabulary.
type validationPaths struct {
	contracts *pathContracts
	// unions holds each referenced variant vocabulary by type name.
	unions    map[string][]string
	localized map[string]bool
}

func newValidationPaths(contracts *pathContracts) *validationPaths {
	return &validationPaths{contracts: contracts, unions: map[string][]string{}, localized: map[string]bool{}}
}

// terms returns the union members for fields, with localization suppressed
// when unlocalized is set.
func (paths *validationPaths) terms(fields []schema.Field, unlocalized bool) []string {
	result := make([]string, 0, len(fields))
	seen := make(map[string]bool)
	appendPath := func(path string) {
		var value string
		if strings.Contains(path, "${") {
			value = "`" + path + "`"
		} else {
			value = strconv.Quote(path)
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	var add func(schema.Field, string, bool)
	add = func(field schema.Field, prefix string, unlocalized bool) {
		path := field.Name
		if prefix != "" {
			path = prefix + "." + field.Name
		}
		appendPath(path)
		localized := field.Localized && !unlocalized
		variants := []string{path}
		if localized {
			localizedPath := path + ".${Locale}"
			appendPath(localizedPath)
			variants = append(variants, localizedPath)
		}
		// A localized container stores one projected child tree per locale.
		childrenUnlocalized := unlocalized || localized
		for _, variant := range variants {
			switch field.Type {
			case schema.FieldTypeGroup:
				if field.Nested != nil {
					for _, child := range writableFields(field.Nested.ResolvedFields()) {
						add(child, variant, childrenUnlocalized)
					}
				}
			case schema.FieldTypeArray:
				row := variant + ".${number}"
				appendPath(row)
				appendPath(row + "._key")
				if field.Nested != nil {
					for _, child := range writableFields(field.Nested.ResolvedFields()) {
						add(child, row, childrenUnlocalized)
					}
				}
			case schema.FieldTypeBlocks:
				row := variant + ".${number}"
				appendPath(row)
				appendPath(row + "._key")
				appendPath(row + ".blockType")
				for _, placement := range paths.contracts.placements(field, "") {
					if child := paths.variant(placement.variant, childrenUnlocalized); child != "" {
						appendPath(row + ".${" + child + "}")
					}
				}
			case schema.FieldTypePlugin:
				if field.Plugin != nil && len(field.Plugin.EmbeddedTrees) > 0 {
					appendPath(variant + ".${string}")
				}
			case schema.FieldTypeSelect:
				if field.Select != nil && field.Select.HasMany {
					appendPath(variant + ".${number}")
				}
			case schema.FieldTypeRelationship:
				if field.Relationship != nil && field.Relationship.HasMany {
					appendPath(variant + ".${number}")
				}
			case schema.FieldTypeUpload:
				if field.Upload != nil && field.Upload.HasMany {
					appendPath(variant + ".${number}")
				}
			}
		}
	}
	for _, field := range fields {
		add(field, "", unlocalized)
	}
	return result
}

// variant returns the type name of a block variant's vocabulary in the given
// localization context, emitting it on first use. It returns "" when the
// variant has no writable path.
func (paths *validationPaths) variant(name string, unlocalized bool) string {
	if unlocalized && !paths.hasLocalized(name) {
		unlocalized = false
	}
	typeName := name + "ValidationPath"
	if unlocalized {
		typeName = name + "UnlocalizedValidationPath"
	}
	if union, ok := paths.unions[typeName]; ok {
		if len(union) == 0 {
			return ""
		}
		return typeName
	}
	paths.unions[typeName] = nil
	union := paths.terms(writableFields(paths.contracts.variants[name].ResolvedFields()), unlocalized)
	paths.unions[typeName] = union
	if len(union) == 0 {
		return ""
	}
	return typeName
}

// hasLocalized reports whether a variant's vocabulary contains a locale
// segment, in which case its unlocalized vocabulary differs.
func (paths *validationPaths) hasLocalized(name string) bool {
	if localized, ok := paths.localized[name]; ok {
		return localized
	}
	paths.localized[name] = false
	var inspect func([]schema.Field) bool
	inspect = func(fields []schema.Field) bool {
		for _, field := range fields {
			if field.Localized {
				return true
			}
			if field.Nested != nil && inspect(writableFields(field.Nested.ResolvedFields())) {
				return true
			}
			for _, placement := range paths.contracts.placements(field, "") {
				if paths.hasLocalized(placement.variant) {
					return true
				}
			}
		}
		return false
	}
	localized := inspect(writableFields(paths.contracts.variants[name].ResolvedFields()))
	paths.localized[name] = localized
	return localized
}

// write emits the variant vocabularies referred to by resources, in catalog order.
func (paths *validationPaths) write(output *bytes.Buffer) {
	for _, variant := range paths.contracts.generator.blocks.Variants {
		for _, name := range []string{variant.Name + "ValidationPath", variant.Name + "UnlocalizedValidationPath"} {
			if union := paths.unions[name]; len(union) != 0 {
				fmt.Fprintf(output, "export type %s = %s;\n\n", name, strings.Join(union, " | "))
			}
		}
	}
}
