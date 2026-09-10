// Package embedded interprets the resolved, bounded envelope declared by a
// plugin. It knows no plugin node vocabulary and never traverses payload JSON:
// callers return schema-owned payloads to their ordinary field traversal.
package embedded

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const (
	MaxDepth = 64
	MaxNodes = 10000
	MaxWork  = 100000
)

// Budget is shared across nested ordinary fields and plugin envelopes during a
// pass. A new budget describes a new bounded pass, not a new embedded field.
type Budget struct {
	depth, nodes, work int
	workOnly           bool
}

func NewBudget() *Budget { return &Budget{} }

// NewWorkBudget bounds repeated admitted traversal in one hook phase. Each
// pass has its own node ceiling; aggregate work may not exceed MaxWork.
func NewWorkBudget() *Budget { return &Budget{workOnly: true} }
func (b *Budget) limit() int { return MaxWork }
func (b *Budget) Enter(path string) error {
	b.depth++
	b.work++
	if b.depth > MaxDepth || b.work > b.limit() {
		b.depth--
		return failure("embedded_limit", path, "embedded field traversal exceeds its depth or work budget")
	}
	return nil
}
func (b *Budget) Leave() { b.depth-- }
func (b *Budget) node(path string) error {
	b.nodes++
	b.work++
	if (!b.workOnly && b.nodes > MaxNodes) || b.work > b.limit() {
		return failure("embedded_limit", path, "embedded field traversal exceeds its node or work budget")
	}
	return nil
}

// Error contains only structural diagnostics, never an opaque plugin payload.
type Error struct{ Issue schema.Issue }

func (e *Error) Error() string { return e.Issue.Path + ": " + e.Issue.Message }
func failure(code, path, message string) error {
	return &Error{Issue: schema.Issue{Code: code, Path: path, Message: message}}
}

// Occurrence is one schema-owned payload. Identity is stable under structural
// moves within this tree. Callers namespace it by the containing occurrence.
type Occurrence struct {
	Tree        schema.EmbeddedTree
	Case        schema.EmbeddedTreeCase
	Type        schema.BlockType
	Fields      []schema.Field
	Payload     store.Values
	RuntimePath string
	Identity    string
	Key         string
}

func identity(tree, tag, key string) string {
	return fmt.Sprintf("%d:%s/%d:%s/%d:%s", len(tree), tree, len(tag), tag, len(key), key)
}
func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// ReadOccurrence exposes an immutable payload to internal readers and transforms.
// Its metadata and visitation order are identical to Occurrence.
type ReadOccurrence struct {
	Tree        schema.EmbeddedTree
	Case        schema.EmbeddedTreeCase
	Type        schema.BlockType
	Fields      []schema.Field
	Payload     store.Value
	RuntimePath string
	Identity    string
	Key         string
}

func (o ReadOccurrence) detached() Occurrence {
	payload, _ := o.Payload.CopyObject()
	return Occurrence{Tree: o.Tree, Case: o.Case, Type: o.Type, Fields: o.Fields, Payload: payload, RuntimePath: o.RuntimePath, Identity: o.Identity, Key: o.Key}
}

// Transform processes each declared payload exactly once. Mutable callback
// inputs and returned maps remain detached from document storage.
func Transform(field schema.Field, value store.Value, prefix string, budget *Budget, transform func(Occurrence) (store.Values, error)) (store.Value, error) {
	return TransformValue(field, value, prefix, budget, func(o ReadOccurrence) (store.Value, bool, error) {
		updated, err := transform(o.detached())
		if err != nil {
			return o.Payload, false, err
		}
		return store.Object(updated), true, nil
	})
}

// TransformValue processes immutable payloads in admission order. A callback
// reports whether it changed its payload; unchanged envelope branches are reused.
// Errors return the original field value, including after earlier callbacks ran.
func TransformValue(field schema.Field, value store.Value, prefix string, budget *Budget, transform func(ReadOccurrence) (store.Value, bool, error)) (store.Value, error) {
	return walk(field, value, prefix, budget, transform)
}

// Visit observes immutable payloads with the same admission, budgets and order
// as TransformValue, including callbacks visited before a later structural error.
func Visit(field schema.Field, value store.Value, prefix string, budget *Budget, visitor func(ReadOccurrence) error) error {
	_, err := walk(field, value, prefix, budget, func(o ReadOccurrence) (store.Value, bool, error) {
		return o.Payload, false, visitor(o)
	})
	return err
}

// visit preserves the detached mutable payload contract for existing callers.
func visit(field schema.Field, value store.Value, prefix string, budget *Budget, visitor func(Occurrence) error) error {
	return Visit(field, value, prefix, budget, func(o ReadOccurrence) error { return visitor(o.detached()) })
}

func walk(field schema.Field, value store.Value, prefix string, budget *Budget, transform func(ReadOccurrence) (store.Value, bool, error)) (store.Value, error) {
	if field.Plugin == nil || len(field.Plugin.EmbeddedTrees) == 0 || value.Kind() == store.ValueNull {
		return value, nil
	}
	if budget == nil {
		budget = NewBudget()
	}
	original := value
	for _, tree := range field.Plugin.EmbeddedTrees {
		seen := map[string]string{}
		var node func(store.Value, string) (store.Value, bool, error)
		node = func(value store.Value, path string) (store.Value, bool, error) {
			if err := budget.Enter(path); err != nil {
				return value, false, err
			}
			defer budget.Leave()
			if err := budget.node(path); err != nil {
				return value, false, err
			}
			if value.Kind() != store.ValueObject {
				return value, false, failure("invalid_embedded_node", path, "embedded tree node must be an object")
			}
			var object store.Values
			set := func(name string, child store.Value) {
				if object == nil {
					object, _ = value.CopyObject()
				}
				object[name] = child
			}
			tag, _ := value.Get(tree.Tag).StringValue()
			for _, candidate := range tree.Cases {
				if candidate.TagValue != tag {
					continue
				}
				payloadPath := join(path, candidate.Payload)
				payload := value.Get(candidate.Payload)
				if payload.Kind() != store.ValueObject {
					return value, false, failure("invalid_embedded_payload", payloadPath, "embedded payload must be an object")
				}
				kind, ok := payload.Get(candidate.Discriminator).StringValue()
				var variant *schema.BlockType
				if ok {
					for i := range candidate.ResolvedTypes() {
						if candidate.ResolvedTypes()[i].Slug == kind {
							variant = &candidate.ResolvedTypes()[i]
							break
						}
					}
				}
				if variant == nil {
					return value, false, failure("unknown_embedded_schema", join(payloadPath, candidate.Discriminator), "embedded discriminator does not name a configured schema")
				}
				key := ""
				if raw, exists := payload.Lookup(candidate.Identity); exists {
					key, ok = raw.StringValue()
					if !ok || !utf8.ValidString(key) || strings.TrimSpace(key) == "" {
						return value, false, failure("invalid_row_key", join(payloadPath, candidate.Identity), "occurrence identity must be a nonempty string")
					}
					if previous, exists := seen[key]; exists {
						return value, false, failure("duplicate_row_key", join(payloadPath, candidate.Identity), "occurrence identity duplicates "+previous)
					}
					seen[key] = join(payloadPath, candidate.Identity)
				}
				updated, changed, err := transform(ReadOccurrence{Tree: tree, Case: candidate, Type: *variant, Fields: variant.ResolvedFields(), Payload: payload, RuntimePath: payloadPath, Identity: identity(tree.Key, candidate.TagValue+"/"+variant.Slug, key), Key: key})
				if err != nil {
					return value, false, err
				}
				if changed {
					set(candidate.Payload, updated)
				}
				break
			}
			raw, exists := value.Lookup(tree.Children)
			if object != nil {
				raw, exists = object[tree.Children]
			}
			if exists && raw.Kind() != store.ValueNull {
				if raw.Kind() != store.ValueList {
					return value, false, failure("invalid_embedded_children", join(path, tree.Children), "embedded children must be an array")
				}
				updated, changed, err := mapNodes(raw, join(path, tree.Children), node)
				if err != nil {
					return value, false, err
				}
				if changed {
					set(tree.Children, updated)
				}
			}
			if object != nil {
				return store.Object(object), true, nil
			}
			return value, false, nil
		}
		var root func(store.Value, []string, string) (store.Value, bool, error)
		root = func(value store.Value, segments []string, path string) (store.Value, bool, error) {
			if len(segments) == 0 {
				if value.Kind() == store.ValueList {
					return mapNodes(value, path, node)
				}
				return node(value, path)
			}
			if err := budget.Enter(path); err != nil {
				return value, false, err
			}
			defer budget.Leave()
			if value.Kind() != store.ValueObject {
				return value, false, failure("invalid_embedded_root", path, "embedded traversal root must be an object")
			}
			child, exists := value.Lookup(segments[0])
			if !exists {
				return value, false, failure("invalid_embedded_root", join(path, segments[0]), "embedded traversal root is missing")
			}
			updated, changed, err := root(child, segments[1:], join(path, segments[0]))
			if err != nil {
				return value, false, err
			}
			if changed {
				object, _ := value.CopyObject()
				object[segments[0]] = updated
				return store.Object(object), true, nil
			}
			return value, false, nil
		}
		updated, _, err := root(value, tree.Root, prefix)
		if err != nil {
			return original, err
		}
		value = updated
	}
	return value, nil
}

// mapNodes allocates a list only when at least one node changed. A pass that
// edits many nodes still materializes the list once, not once per replacement.
func mapNodes(value store.Value, path string, transform func(store.Value, string) (store.Value, bool, error)) (store.Value, bool, error) {
	var items []store.Value
	index := -1
	for item := range value.Elements() {
		index++
		updated, changed, err := transform(item, fmt.Sprintf("%s.%d", path, index))
		if err != nil {
			return value, false, err
		}
		if changed {
			if items == nil {
				items, _ = value.CopyList()
			}
			items[index] = updated
		}
	}
	if items != nil {
		return store.List(items...), true, nil
	}
	return value, false, nil
}

// Occurrences is the read-only view of Transform. It validates the same envelope
// and returns detached payloads; no callback can mutate its source document.
func Occurrences(field schema.Field, value store.Value, prefix string, budget *Budget) ([]Occurrence, error) {
	var result []Occurrence
	err := visit(field, value, prefix, budget, func(occurrence Occurrence) error {
		result = append(result, occurrence)
		return nil
	})
	return result, err
}

// HasFields reports whether the field owns embedded schemas.
func HasFields(field schema.Field) bool {
	return field.Plugin != nil && len(field.Plugin.EmbeddedTrees) != 0
}

// SchemaFields visits declared payload schemas, without inspecting any values.
func SchemaFields(field schema.Field, visit func([]schema.Field)) {
	if field.Plugin == nil {
		return
	}
	for _, tree := range field.Plugin.EmbeddedTrees {
		for _, candidate := range tree.Cases {
			for _, variant := range candidate.ResolvedTypes() {
				visit(variant.ResolvedFields())
			}
		}
	}
}
