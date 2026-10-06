package operation

import (
	"sort"
	"strconv"
	"strings"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Request-time traversal follows document values and enters blocks through
// their shared definitions (see schema.BlocksField.Definitions), so its
// cost follows the document, not the number of placements in the schema. A
// definition's fields carry definition-relative paths and IDs, and a block
// definition's field is bound once for all of its placements; callbacks,
// issue targets and capabilities still name each field by its placement, which
// the walks below derive from the canonical path they follow.

// placementField names field, reached at canonical path segments, by its
// placement. A resource's own fields already do; a block definition's fields
// take the placement's path and stable ID.
func placementField(resource schema.StableID, field schema.Field, canonical []string, shared bool) schema.Field {
	if !shared {
		return field
	}
	field.Path, _ = query.NewPath(canonical...)
	field.ID = schema.PlacementFieldID(resource, canonical)
	return field
}

// fieldPlacement identifies a field location's schema placement: its canonical
// path segments and whether it was reached through a shared definition.
type fieldPlacement struct {
	canonical []string
	shared    bool
}

func (placement fieldPlacement) path() string { return strings.Join(placement.canonical, ".") }

func (placement fieldPlacement) child(name string) fieldPlacement {
	return fieldPlacement{canonical: append(placement.canonical[:len(placement.canonical):len(placement.canonical)], name), shared: placement.shared}
}

// enter describes the fields of a block selected beneath this container: a
// definition's fields, shared at every placement.
func (placement fieldPlacement) enter(segments ...string) fieldPlacement {
	return fieldPlacement{canonical: append(placement.canonical[:len(placement.canonical):len(placement.canonical)], segments...), shared: true}
}

// walkPosition is where a document walk stands: the value it visits, named by
// its runtime path, identity path and binding identity, the placement of the
// fields around it, and the scopes it inherits.
type walkPosition struct {
	runtime, identity, binding string
	// base is the canonical path of the block definition placement whose
	// fields the walk is within, or empty within a resource's own fields. A
	// field's canonical path follows from it (see canonicalPath), so the walk
	// derives it only where it is needed.
	base string
	// field is the field whose value this position holds, if any.
	field  *schema.Field
	locale schema.LocaleCode
	// localized reports a value of a localized field or inside one, whose
	// values differ by locale.
	localized bool
}

// canonicalPath is the canonical path of field within the block definition
// placement at base, or of a resource's own field when base is empty.
func canonicalPath(base string, field *schema.Field) string {
	if base == "" {
		return field.Path.String()
	}
	return base + "." + field.Path.String()
}

// rootPosition is the position of a collection's root object.
func rootPosition(collection Collection) walkPosition {
	return walkPosition{base: collection.placement.path()}
}

// member is the position of field's value in the object at p.
func (p walkPosition) member(field *schema.Field) walkPosition {
	p.runtime = joinFieldPath(p.runtime, field.Name)
	p.identity = joinFieldPath(p.identity, field.Name)
	p.binding = joinFieldPath(p.binding, field.Name)
	p.field = field
	p.localized = p.localized || field.Localized
	return p
}

// translation is the position of one locale's value of the localized field at p.
func (p walkPosition) translation(code schema.LocaleCode) walkPosition {
	p.runtime = joinFieldPath(p.runtime, string(code))
	p.identity = joinFieldPath(p.identity, string(code))
	p.locale = code
	return p
}

// row is the position of the row at index, with identity rowIdentity, of the
// repeated field at p.
func (p walkPosition) row(index int, rowIdentity string) walkPosition {
	p.runtime = p.runtime + "." + strconv.Itoa(index)
	p.identity = joinFieldPath(p.identity, rowIdentity)
	p.binding = joinFieldPath(p.binding, rowIdentity)
	return p
}

// enter is the position of a block definition's fields selected by segments
// beneath the container whose value is at p: a block slug, or an embedded
// tree key, case tag and slug.
func (p walkPosition) enter(segments ...string) walkPosition {
	p.base = canonicalPath(p.base, p.field) + "." + strings.Join(segments, ".")
	p.field = nil
	return p
}

// placementKey names a block placement beneath a container: the selected
// slug, after an embedded tree key and case tag for a plugin field.
type placementKey [3]string

// placementBases holds the base of each block placement entered beneath one
// container, so every row that selects a block shares one canonical path.
type placementBases map[placementKey]string

// enterVia is enter for the placement key names, through bases.
func (p walkPosition) enterVia(bases placementBases, key placementKey) walkPosition {
	base, known := bases[key]
	if !known {
		base = canonicalPath(p.base, p.field)
		for _, segment := range key {
			if segment != "" {
				base += "." + segment
			}
		}
		bases[key] = base
	}
	p.base, p.field = base, nil
	return p
}

// payload is the position of an embedded occurrence's payload fields beneath
// the plugin field at p, entered through bases.
func (p walkPosition) payload(occurrence embedded.ReadOccurrence, bases placementBases) walkPosition {
	at := p.enterVia(bases, placementKey{occurrence.Tree.Key, occurrence.Case.TagValue, occurrence.Type.Slug})
	at.runtime = occurrence.RuntimePath
	at.identity = joinFieldPath(p.identity, occurrence.Identity)
	at.binding = joinFieldPath(p.binding, occurrence.Identity)
	return at
}

// location describes the value at p of its field, a member of the object
// holding fields whose runtime path is parent.
func (p walkPosition) location(fields []schema.Field, value, siblings store.Value, parent string) fieldLocation {
	return fieldLocation{
		root: parent == "", fields: fields, value: value, siblings: siblings,
		runtimePath: p.runtime, identity: p.identity, bindingIdentity: p.binding, locale: p.locale,
		field: p.field, base: p.base, localeOwned: p.localized,
	}
}

// eachFieldLocation visits every field location in a document: each field of
// every object its values contain (the root, groups, rows and embedded
// payloads), including absent members of present objects, as
// collectFieldLocations reports them. Locations are visited in document order.
func eachFieldLocation(collection Collection, root store.Value, allLocales bool, visit func(fieldLocation)) {
	var object func([]schema.Field, store.Value, walkPosition)
	var descend func(*schema.Field, store.Value, walkPosition)
	object = func(fields []schema.Field, values store.Value, at walkPosition) {
		for index := range fields {
			field := &fields[index]
			value, exists := values.Lookup(field.Name)
			member := at.member(field)
			if allLocales && field.Localized {
				if !exists || value.Kind() != store.ValueObject {
					continue
				}
				codes := make([]string, 0, value.Len())
				for code := range value.Entries() {
					codes = append(codes, code)
				}
				sort.Strings(codes)
				for _, code := range codes {
					localized, translation := schema.LocaleCode(code), value.Get(code)
					translated := member.translation(localized)
					visit(translated.location(fields, translation, fieldLocationSiblings(fields, values, allLocales, localized), at.runtime))
					descend(field, translation, translated)
				}
				continue
			}
			visit(member.location(fields, value, fieldLocationSiblings(fields, values, allLocales, at.locale), at.runtime))
			if exists {
				descend(field, value, member)
			}
		}
	}
	descend = func(field *schema.Field, value store.Value, at walkPosition) {
		bases := placementBases{}
		switch field.Type {
		case schema.FieldTypePlugin:
			// Operation preflight rejects malformed envelopes.
			_ = embedded.Visit(*field, value, at.runtime, nil, func(occurrence embedded.ReadOccurrence) error {
				object(occurrence.Fields, occurrence.Payload, at.payload(occurrence, bases))
				return nil
			})
		case schema.FieldTypeGroup:
			if value.Kind() == store.ValueObject && field.Nested != nil {
				object(field.Nested.ResolvedFields(), value, at)
			}
		case schema.FieldTypeArray:
			if field.Nested == nil {
				return
			}
			for row := range identifiedRowsInOrder(value, nil) {
				object(field.Nested.ResolvedFields(), row.value, at.row(row.index, row.identity))
			}
		case schema.FieldTypeBlocks:
			if field.Blocks == nil {
				return
			}
			for row := range identifiedRowsInOrder(value, field.Blocks) {
				block, _ := field.Blocks.Definition(row.kind)
				object(block.ResolvedFields(), row.value, at.row(row.index, row.identity).enterVia(bases, placementKey{2: row.kind}))
			}
		}
	}
	object(collection.Schema.Fields, root, rootPosition(collection))
}

// placementOrder is the canonical path's position in a depth-first walk of
// the schema beneath fields: the index of each field in its list, of each
// selected block in its container, and of each embedded tree, case and block.
// Comparing two keys element by element, a prefix first, orders placements as
// that walk visits them.
func placementOrder(fields []schema.Field, segments []string) []int {
	key := make([]int, 0, len(segments))
	for len(segments) != 0 {
		index := slicesIndexField(fields, segments[0])
		if index < 0 {
			return key
		}
		key = append(key, index)
		field := fields[index]
		segments = segments[1:]
		switch {
		case len(segments) == 0:
			return key
		case field.Type == schema.FieldTypeBlocks && field.Blocks != nil:
			definitions := field.Blocks.Definitions()
			selected := indexBlock(definitions, segments[0])
			if selected < 0 {
				return key
			}
			key = append(key, selected)
			fields, segments = definitions[selected].ResolvedFields(), segments[1:]
		case embedded.HasFields(field) && len(segments) >= 3:
			found := false
			for treeIndex, tree := range field.Plugin.EmbeddedTrees {
				if tree.Key != segments[0] {
					continue
				}
				for caseIndex, c := range tree.Cases {
					if c.TagValue != segments[1] {
						continue
					}
					definitions := c.Definitions()
					if selected := indexBlock(definitions, segments[2]); selected >= 0 {
						key = append(key, treeIndex, caseIndex, selected)
						fields, found = definitions[selected].ResolvedFields(), true
					}
				}
			}
			if !found {
				return key
			}
			segments = segments[3:]
		case field.Nested != nil:
			fields = field.Nested.ResolvedFields()
		default:
			return key
		}
	}
	return key
}

func slicesIndexField(fields []schema.Field, name string) int {
	for index := range fields {
		if fields[index].Name == name {
			return index
		}
	}
	return -1
}

func indexBlock(blocks []schema.BlockType, slug string) int {
	for index := range blocks {
		if blocks[index].Slug == slug {
			return index
		}
	}
	return -1
}

// compareOrder compares two placementOrder keys.
func compareOrder(left, right []int) int {
	for index := 0; index < len(left) && index < len(right); index++ {
		if left[index] != right[index] {
			if left[index] < right[index] {
				return -1
			}
			return 1
		}
	}
	return len(left) - len(right)
}
