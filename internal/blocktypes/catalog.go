// Package blocktypes assigns ordinary Blocks generated symbols from the resolved
// manifest. It does not replace persisted field identities or intern schemas.
package blocktypes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	// Block is the family's representative: its shared definition view (see
	// schema.BlocksField.Definitions).
	Block schema.BlockType
}

// Catalog is a deterministic, generation-local index into resolved schemas.
// It is built from definitions: each block is cataloged once, not once per
// placement.
type Catalog struct {
	// Fields indexes containers by the field IDs a definition traversal sees:
	// a resource's own fields by their stable IDs, and fields of blocks by
	// their definition-relative IDs. Generators therefore traverse blocks
	// through Definitions or a Variant's Block.
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

type claim struct {
	shape, path, owner string
	// wire is the discriminator and identity properties of the payload.
	wire       string
	localized  bool
	normalized string
	index      int
}

// subtreeKey identifies one block's child fields in one locale context. Shared
// definition views are the same slice at every placement.
type subtreeKey struct {
	fields     *schema.Field
	count      int
	suppressed bool
}

type builder struct {
	result  *Catalog
	symbols map[string]claim
	shapes  map[string]claim
	issues  []schema.Issue
	wire    map[schema.StableID][2]string
	walked  map[subtreeKey]bool
	shaper  *Shaper
}

// Build validates naming and reuse without mutating the manifest. Labels and
// presentation metadata never participate in a block's resolved shape. Each
// definition is cataloged once per locale context, so the work is
// proportional to definitions, not to every path through the block graph. A
// variant is named by its definition's TypeName, or the slug in PascalCase,
// never by a placement.
func Build(snapshot schema.Snapshot) (*Catalog, error) {
	b := &builder{
		result:  &Catalog{Fields: make(map[schema.StableID]Field)},
		symbols: map[string]claim{}, shapes: map[string]claim{},
		wire: map[schema.StableID][2]string{}, walked: map[subtreeKey]bool{},
		shaper: NewShaper(),
	}
	for _, name := range []string{"ID", "Locale", "RiduConfig", "ScalarWhere", "TimestampWhere", "MembershipWhere", "ExistsWhere", "RiduPrefixedPaths", "ContractError", "Reference", "Array", "Record", "Partial", "Readonly", "Omit", "Pick", "Promise", "ReturnType", "Parameters", "Extract", "Exclude", "NonNullable", "RiduClient", "ClientOptions", "Symbol", "RiduLocalizedValues", "CollectionSlug", "GlobalSlug", "BlockOptional"} {
		b.reserve(name, "framework", "generated")
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
			for _, suffix := range []string{"", "Create", "Update", "Draft", "DraftCreate", "DraftUpdate", "AllLocales", "Where", "Select", "Populate", "PopulationSelect", "ValidationPath"} {
				if _, exists := b.symbols[base+suffix]; !exists {
					b.symbols[base+suffix] = claim{owner: "resource " + string(resource.ID), path: string(resource.Slug)}
				}
			}
		}
		binding := Identifier(resource.Labels.Plural) + "Collection"
		if resource.Capabilities.Global {
			binding = Identifier(resource.Labels.Singular) + "Global"
		}
		for _, suffix := range []string{"", "Read", "AllLocales"} {
			if _, exists := b.symbols[binding+suffix]; !exists {
				b.symbols[binding+suffix] = claim{owner: "resource binding " + string(resource.ID), path: string(resource.Slug)}
			}
		}
	}
	for _, resource := range resources {
		prefix := Identifier(string(resource.Slug))
		if resource.Capabilities.Global {
			prefix = "Global" + prefix
		}
		b.walk(resource.Fields, prefix, string(resource.Slug)+".fields", false, false)
	}
	if len(b.issues) > 0 {
		return nil, schema.NewValidationError(b.issues)
	}
	sort.Slice(b.result.Variants, func(i, j int) bool { return b.result.Variants[i].Name < b.result.Variants[j].Name })
	return b.result, nil
}

func (b *builder) reserve(name, owner, path string) {
	if prior, ok := b.symbols[name]; ok && prior.owner != owner {
		b.issues = append(b.issues, schema.Issue{Code: "block_type_name_conflict", Path: path, Message: fmt.Sprintf("generated name %q conflicts with %s at %s", name, prior.owner, prior.path)})
	} else {
		b.symbols[name] = claim{owner: owner, path: path}
	}
}

func (b *builder) walk(fields []schema.Field, prefix, path string, insideBlock, localizedAncestor bool) {
	for _, field := range fields {
		fieldPath := path + "." + field.Name
		if field.Plugin != nil {
			virtual := schema.EmbeddedDefinitionBlocks(field)
			k := 0
			for _, tree := range field.Plugin.EmbeddedTrees {
				for _, c := range tree.Cases {
					b.wire[virtual[k].ID] = [2]string{c.Discriminator, c.Identity}
					k++
				}
			}
			b.walk(virtual, prefix+Identifier(field.Name), path+"."+field.Name, insideBlock, localizedAncestor || field.Localized)
		}
		name := prefix + Identifier(field.Name)
		if field.Blocks != nil {
			b.container(field, name, fieldPath, localizedAncestor)
		} else if field.Nested != nil {
			if insideBlock {
				nestedName := name
				if field.Type == schema.FieldTypeArray {
					nestedName += "Row"
				}
				for _, suffix := range []string{"", "Input", "Update", "Draft", "DraftInput", "DraftUpdate", "AllLocales", "AllLocalesValue"} {
					b.reserve(nestedName+suffix, "nested "+name+" ("+field.Name+")", fieldPath)
				}
			}
			childPrefix := name
			if insideBlock && field.Type == schema.FieldTypeArray {
				childPrefix += "Row"
			}
			b.walk(field.Nested.ResolvedFields(), childPrefix, fieldPath, insideBlock, localizedAncestor || field.Localized)
		}
	}
}

func (b *builder) container(field schema.Field, name, fieldPath string, localizedAncestor bool) {
	keys := [2]string{"blockType", "_key"}
	configured, embedded := b.wire[field.ID]
	if embedded {
		keys = configured
	}
	container := Field{Name: name, Discriminator: keys[0], Identity: keys[1], Embedded: embedded}
	suppressed := localizedAncestor || field.Localized
	for _, block := range field.Blocks.Definitions() {
		blockPath := fieldPath + ".blocks." + block.Slug + ".typeName"
		variantName := block.TypeName
		if variantName == "" {
			variantName = Identifier(block.Slug)
		}
		if !ValidName(variantName) {
			b.issues = append(b.issues, schema.Issue{Code: "invalid_block_type_name", Path: blockPath, Message: "block type name must be an exported identifier containing only ASCII letters and digits"})
			continue
		}
		wire := keys[0] + "/" + keys[1] + "/"
		shape, err := b.shaper.shape(block, false)
		normalized, normalizedErr := b.shaper.shape(block, true)
		if err = errors.Join(err, normalizedErr); err != nil {
			b.issues = append(b.issues, schema.Issue{Code: "invalid_block_shape", Path: blockPath, Message: err.Error()})
			continue
		}
		shape, normalized = wire+shape, wire+normalized
		if prior, ok := b.shapes[variantName]; ok {
			switch {
			case prior.wire != wire:
				// One definition generates one payload type, whose discriminator and
				// identity properties belong to its shape.
				b.issues = append(b.issues, schema.Issue{Code: "block_envelope_conflict", Path: blockPath, Message: fmt.Sprintf("block %q is selected here by a container whose payloads use discriminator and identity %q, but by one using %q at %s; a block definition has one payload shape, so declare a separate block for the other container", block.Slug, strings.TrimSuffix(wire, "/"), strings.TrimSuffix(prior.wire, "/"), prior.path)})
			case prior.shape != shape && (!(prior.localized || suppressed) || prior.normalized != normalized):
				b.issues = append(b.issues, schema.Issue{Code: "block_type_name_conflict", Path: blockPath, Message: fmt.Sprintf("block type name %q has a different resolved shape at %s", variantName, prior.path)})
			}
			if prior.localized && !suppressed {
				b.result.Variants[prior.index].Block = block
				b.shapes[variantName] = claim{shape: shape, path: blockPath, wire: wire, normalized: normalized, index: prior.index}
			}
		} else {
			b.shapes[variantName] = claim{shape: shape, path: blockPath, wire: wire, localized: suppressed, normalized: normalized, index: len(b.result.Variants)}
			b.result.Variants = append(b.result.Variants, Variant{Name: variantName, Block: block, Discriminator: keys[0], Identity: keys[1]})
		}
		b.reserve(variantName+"BlockType", "block "+variantName, blockPath)
		for _, suffix := range []string{"", "Input", "Update", "Draft", "DraftInput", "DraftUpdate", "AllLocales", "AllLocalesValue", "Where", "Populate", "ValidationPath", "UnlocalizedValidationPath"} {
			b.reserve(variantName+suffix, "block "+variantName, blockPath)
		}
		container.Variants = append(container.Variants, variantName)
		// Descendants of explicitly named variants share their parent's symbols.
		// A shared definition's descendants are the same at every placement, so
		// they are cataloged once for each locale context.
		children := block.ResolvedFields()
		key := subtreeKey{count: len(children), suppressed: suppressed}
		if len(children) > 0 {
			key.fields = &children[0]
		}
		if key.fields != nil && b.walked[key] {
			continue
		}
		b.walked[key] = true
		b.walk(children, variantName, fieldPath+".blocks."+block.Slug, true, suppressed)
	}
	for _, suffix := range []string{"", "Input", "AllLocales", "Block", "BlockInput", "BlockAllLocales", "InputBlock", "AllLocalesBlock", "AllLocalesValue", "BlockAllLocalesValue", "AllLocalesValueBlock", "Update", "BlockUpdate", "UpdateBlock", "Draft", "DraftInput", "DraftUpdate", "BlockDraftInput", "BlockDraftUpdate"} {
		b.reserve(name+suffix, "container "+name+" ("+field.Name+")", fieldPath)
	}
	if embedded {
		for _, suffix := range []string{"Payload", "InputPayload", "UpdatePayload", "DraftPayload", "DraftInputPayload", "DraftUpdatePayload", "AllLocalesPayload", "AllLocalesValuePayload"} {
			b.reserve(name+suffix, "embedded payload "+name, fieldPath)
		}
	}
	b.result.Fields[field.ID] = container
}

// Shaper computes resolved block shapes, remembering each block's shape so a
// definition placed many times is encoded once.
type Shaper struct {
	shapes map[shapeKey]string
}

type shapeKey struct {
	fields *schema.Field
	count  int
	slug   string
	clear  bool
}

// NewShaper returns an empty shape memo. A Shaper is not safe for concurrent use.
func NewShaper() *Shaper { return &Shaper{shapes: make(map[shapeKey]string)} }

// Shape compares resolved value contracts, excluding placement and author-facing
// metadata. It deliberately retains defaults, validation and reference targets.
// Nested block types contribute their type name, discriminator and a digest of
// their own shape, so each block is encoded once however often it is nested.
func (s *Shaper) Shape(block schema.BlockType) (string, error) {
	return s.shape(block, false)
}

// Shape is Shaper.Shape with a single-use memo.
func Shape(block schema.BlockType) (string, error) { return NewShaper().Shape(block) }

// shape encodes block, treating every field as unlocalized when clear is set:
// a localized ancestor stores one projected child tree per locale, which is a
// contextual transformation, not a different reusable block definition.
func (s *Shaper) shape(block schema.BlockType, clear bool) (string, error) {
	fields := block.ResolvedFields()
	key := shapeKey{count: len(fields), slug: block.Slug, clear: clear}
	if len(fields) > 0 {
		key.fields = &fields[0]
		if cached, ok := s.shapes[key]; ok {
			return cached, nil
		}
	}
	shaped, err := s.fields(fields, clear)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(shapedBlock{Slug: block.Slug, Fields: shaped})
	if err != nil {
		return "", err
	}
	shape := string(encoded)
	if key.fields != nil {
		s.shapes[key] = shape
	}
	return shape, nil
}

func (s *Shaper) digest(block schema.BlockType, clear bool) (shapedType, error) {
	shape, err := s.shape(block, clear)
	sum := sha256.Sum256([]byte(shape))
	return shapedType{Slug: block.Slug, TypeName: block.TypeName, Shape: hex.EncodeToString(sum[:])}, err
}

type shapedBlock struct {
	Slug   string        `json:"slug"`
	Fields []shapedField `json:"fields"`
}

// shapedField is one field's value contract. Children of groups and arrays are
// part of the enclosing block; nested block types are referenced by digest.
type shapedField struct {
	Field    schema.Field   `json:"field"`
	Children []shapedField  `json:"children,omitempty"`
	Types    []shapedType   `json:"types,omitempty"`
	Cases    [][]shapedType `json:"cases,omitempty"`
}

type shapedType struct {
	Slug     string `json:"slug"`
	TypeName string `json:"typeName,omitempty"`
	Shape    string `json:"shape"`
}

func (s *Shaper) fields(fields []schema.Field, clear bool) ([]shapedField, error) {
	result := make([]shapedField, 0, len(fields))
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation && field.Type != schema.FieldTypeVirtual && field.Type != schema.FieldTypeJoin {
			continue
		}
		shaped := shapedField{}
		var err error
		field.ID = ""
		// Placement paths differ; the name is the field's identity in its block.
		if field.Path, err = query.NewPath(field.Name); err != nil {
			return nil, err
		}
		field.Admin = schema.FieldAdmin{}
		if clear {
			field.Localized = false
		}
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
			if shaped.Children, err = s.fields(field.Nested.ResolvedFields(), clear); err != nil {
				return nil, err
			}
			field.Nested = &schema.NestedField{MinRows: field.Nested.MinRows, MaxRows: field.Nested.MaxRows}
		}
		if field.Blocks != nil {
			for _, block := range field.Blocks.Definitions() {
				digest, err := s.digest(block, clear)
				if err != nil {
					return nil, err
				}
				shaped.Types = append(shaped.Types, digest)
			}
			field.Blocks = &schema.BlocksField{MinRows: field.Blocks.MinRows, MaxRows: field.Blocks.MaxRows}
		}
		if field.Plugin != nil {
			plugin := *field.Plugin
			plugin.EmbeddedTrees = append([]schema.EmbeddedTree(nil), plugin.EmbeddedTrees...)
			for i := range plugin.EmbeddedTrees {
				plugin.EmbeddedTrees[i].Cases = append([]schema.EmbeddedTreeCase(nil), plugin.EmbeddedTrees[i].Cases...)
				for j := range plugin.EmbeddedTrees[i].Cases {
					c := &plugin.EmbeddedTrees[i].Cases[j]
					types := make([]shapedType, 0, len(c.Definitions()))
					for _, block := range c.Definitions() {
						digest, err := s.digest(block, clear)
						if err != nil {
							return nil, err
						}
						types = append(types, digest)
					}
					shaped.Cases = append(shaped.Cases, types)
					*c = schema.EmbeddedTreeCase{TagValue: c.TagValue, Payload: c.Payload, Discriminator: c.Discriminator, Identity: c.Identity}
				}
			}
			field.Plugin = &plugin
		}
		shaped.Field = field
		result = append(result, shaped)
	}
	return result, nil
}
