package embedded

import (
	"fmt"
	"github.com/riducms/ridu/schema"
	"reflect"
)

// CompareEvolution checks embedded descriptor topology separately from ordinary
// child schema evolution. Moving a payload boundary requires an explicit data
// transform; adding ordinary fields/variants uses the adapter's existing rules.
// The returned field has the prior types solely for the caller's outer-shape
// comparison; neither input is mutated.
func CompareEvolution(before, after schema.Field, compare func(string, []schema.BlockType, []schema.BlockType) error) (schema.Field, error) {
	if !HasFields(before) && !HasFields(after) {
		return after, nil
	}
	if before.Plugin == nil || after.Plugin == nil || len(before.Plugin.EmbeddedTrees) != len(after.Plugin.EmbeddedTrees) {
		return after, evolutionError(before)
	}
	plugin := *after.Plugin
	plugin.EmbeddedTrees = append([]schema.EmbeddedTree(nil), after.Plugin.EmbeddedTrees...)
	for i, previous := range before.Plugin.EmbeddedTrees {
		next := plugin.EmbeddedTrees[i]
		if len(previous.Cases) != len(next.Cases) {
			return after, evolutionError(before)
		}
		next.Cases = append([]schema.EmbeddedTreeCase(nil), next.Cases...)
		for j, old := range previous.Cases {
			candidate := next.Cases[j]
			candidate = schema.EmbeddedTreeCase{TagValue: candidate.TagValue, Payload: candidate.Payload, Discriminator: candidate.Discriminator, Identity: candidate.Identity}
			oldMetadata := schema.EmbeddedTreeCase{TagValue: old.TagValue, Payload: old.Payload, Discriminator: old.Discriminator, Identity: old.Identity}
			if !reflect.DeepEqual(oldMetadata, candidate) {
				return after, evolutionError(before)
			}
			if err := compare(before.Path.String()+"."+previous.Key+"."+old.TagValue, old.ResolvedTypes(), next.Cases[j].ResolvedTypes()); err != nil {
				return after, err
			}
			next.Cases[j] = old
		}
		if !reflect.DeepEqual(previous, next) {
			return after, evolutionError(before)
		}
		plugin.EmbeddedTrees[i] = next
	}
	after.Plugin = &plugin
	return after, nil
}
func evolutionError(field schema.Field) error {
	return fmt.Errorf("embedded descriptor %q changed its envelope; use an explicit data migration to transform stored payloads", field.Path.String())
}

// DescendantPath identifies an ordinary field reached through an embedded
// payload boundary. The plugin root itself remains an ordinary stored field.
func DescendantPath(fields []schema.Field, path string) bool {
	for _, field := range fields {
		if HasFields(field) {
			var contains func([]schema.Field) bool
			contains = func(children []schema.Field) bool {
				for _, child := range children {
					if child.Path.String() == path || contains(schema.ChildFields(child)) {
						return true
					}
				}
				return false
			}
			if contains(schema.ChildFields(field)) {
				return true
			}
		}
		if DescendantPath(schema.ChildFields(field), path) {
			return true
		}
	}
	return false
}
