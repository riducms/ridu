// Package blockrename moves the stored content of a confirmed block field
// rename. A block definition is shared by every placement, so the rename moves
// one key in every stored block of the definition, wherever the block graph
// places it, in current documents and version snapshots alike.
package blockrename

import (
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Rename applies one block field rename to stored values. It finds the blocks
// through the after schema, so the containers above them must already have
// their after names: resource renames run first, and Order sorts block renames
// so a definition's renames precede those of the definitions it places.
type Rename struct {
	graph       *blockgraph.Graph
	walker      *blockgraph.Walker
	block       string
	containers  []string
	source      string
	destination string
}

// New prepares intent for stored values of after's resources. reverse moves
// content from the after name back to the before name, as a rollback does.
func New(after schema.Snapshot, intent migration.Rename, reverse bool) (*Rename, error) {
	if intent.Block == "" {
		return nil, fmt.Errorf("block field rename names no block")
	}
	beforePath, afterPath := strings.Split(intent.FieldBefore, "."), strings.Split(intent.FieldAfter, ".")
	if len(beforePath) != len(afterPath) || strings.Join(beforePath[:len(beforePath)-1], ".") != strings.Join(afterPath[:len(afterPath)-1], ".") {
		return nil, fmt.Errorf("block field rename %s.%s -> %s.%s moves the field to another container", intent.Block, intent.FieldBefore, intent.Block, intent.FieldAfter)
	}
	graph := blockgraph.New(after)
	rename := &Rename{
		graph: graph, walker: graph.Walker(map[string]bool{intent.Block: true}), block: intent.Block,
		containers: afterPath[:len(afterPath)-1], source: beforePath[len(beforePath)-1], destination: afterPath[len(afterPath)-1],
	}
	if reverse {
		rename.source, rename.destination = rename.destination, rename.source
	}
	return rename, nil
}

// Order returns intents with resource renames first, in order, then block
// field renames ordered so each definition precedes those it places.
func Order(after schema.Snapshot, intents []migration.Rename) []migration.Rename {
	var resources, blocks []migration.Rename
	for _, intent := range intents {
		if intent.Block == "" {
			resources = append(resources, intent)
		} else {
			blocks = append(blocks, intent)
		}
	}
	if len(blocks) == 0 {
		return intents
	}
	depths := blockgraph.New(after).Depths()
	sort.SliceStable(blocks, func(left, right int) bool { return depths[blocks[left].Block] < depths[blocks[right].Block] })
	return append(resources, blocks...)
}

// Applies reports whether stored documents of resource can hold the block.
func (rename *Rename) Applies(resource schema.Collection) bool {
	return len(rename.Roots(resource)) != 0
}

// Roots returns the top-level fields of resource whose stored values can hold
// the block, at any depth.
func (rename *Rename) Roots(resource schema.Collection) []schema.Field {
	node, found := rename.graph.ResourceOf(resource)
	if !found {
		return nil
	}
	slugs := map[string]bool{rename.block: true}
	var roots []schema.Field
	for _, root := range node.Roots {
		if rename.graph.RootPlaces(root, slugs) {
			roots = append(roots, root.Field)
		}
	}
	return roots
}

// Values renames the field in every block stored in a document's values of
// fields, returning the values with the changed blocks.
func (rename *Rename) Values(fields []schema.Field, values store.Values) (store.Values, bool, error) {
	return rename.walker.Rewrite(fields, values, "", rename.row)
}

// Field renames the field in every block stored in one top-level field's
// value. locale names the translation a per-locale column holds, whose field
// is then read as unlocalized.
func (rename *Rename) Field(field schema.Field, value store.Value, locale schema.LocaleCode) (store.Value, bool, error) {
	if locale != "" {
		field.Localized = false
	}
	return rename.walker.RewriteField(field, value, locale, rename.row)
}

func (rename *Rename) row(row blockgraph.Row, values store.Values) (bool, error) {
	return move(row.Block.ResolvedFields(), values, rename.containers, rename.source, rename.destination, row.Locale != "")
}

// move renames source to destination beneath containers of one stored object.
// A localized container holds one object per locale.
func move(fields []schema.Field, values store.Values, containers []string, source, destination string, inLocale bool) (bool, error) {
	if len(containers) == 0 {
		value, exists := values[source]
		if !exists {
			return false, nil
		}
		if existing, occupied := values[destination]; occupied && !existing.IsZero() && existing.Kind() != store.ValueNull {
			return false, fmt.Errorf("field rename %s to %s would overwrite existing content", source, destination)
		}
		delete(values, source)
		values[destination] = value
		return true, nil
	}
	var container *schema.Field
	for index := range fields {
		if fields[index].Name == containers[0] && fields[index].Nested != nil {
			container = &fields[index]
			break
		}
	}
	value, exists := values[containers[0]]
	if container == nil || !exists || value.IsZero() || value.Kind() == store.ValueNull {
		return false, nil
	}
	children := container.Nested.ResolvedFields()
	inside := func(value store.Value, inLocale bool) (store.Value, bool, error) {
		if container.Type == schema.FieldTypeGroup {
			object, valid := value.CopyObject()
			if !valid {
				return value, false, nil
			}
			changed, err := move(children, object, containers[1:], source, destination, inLocale)
			if err != nil || !changed {
				return value, false, err
			}
			return store.Object(object), true, nil
		}
		items, valid := value.CopyList()
		if !valid {
			return value, false, nil
		}
		changed := false
		for index, item := range items {
			object, valid := item.CopyObject()
			if !valid {
				continue
			}
			itemChanged, err := move(children, object, containers[1:], source, destination, inLocale)
			if err != nil {
				return value, false, err
			}
			if itemChanged {
				items[index] = store.Object(object)
				changed = true
			}
		}
		if !changed {
			return value, false, nil
		}
		return store.List(items...), true, nil
	}
	if container.Localized && !inLocale {
		translations, valid := value.CopyObject()
		if !valid {
			return false, nil
		}
		changed := false
		codes := make([]string, 0, len(translations))
		for code := range translations {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		for _, code := range codes {
			updated, translationChanged, err := inside(translations[code], true)
			if err != nil {
				return false, err
			}
			if translationChanged {
				translations[code] = updated
				changed = true
			}
		}
		if changed {
			values[containers[0]] = store.Object(translations)
		}
		return changed, nil
	}
	updated, changed, err := inside(value, inLocale)
	if changed {
		values[containers[0]] = updated
	}
	return changed, err
}
