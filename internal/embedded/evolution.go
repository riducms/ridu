package embedded

import (
	"fmt"
	"github.com/riducms/ridu/schema"
	"reflect"
	"strings"
)

// CompareEvolution checks embedded descriptor topology separately from ordinary
// child schema evolution. Moving a payload boundary requires an explicit data
// transform; adding ordinary fields/variants uses the adapter's existing rules.
// compare receives each case's shared definition views, the same values at
// every placement, so a caller can compare each definition once. The returned
// field has the prior types solely for the caller's outer-shape comparison;
// neither input is mutated.
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
			if err := compare(before.Path.String()+"."+previous.Key+"."+old.TagValue, old.Definitions(), next.Cases[j].Definitions()); err != nil {
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

// DescendantPath identifies a path that crosses an embedded payload boundary
// among fields and their groups and arrays: the path of a plugin field that
// declares embedded schemas, followed by more segments. The plugin root itself
// remains an ordinary stored field. A block definition's fields have their own
// definition-relative paths, so the walk never enters block definitions.
func DescendantPath(fields []schema.Field, path string) bool {
	for _, field := range fields {
		if HasFields(field) && strings.HasPrefix(path, field.Path.String()+".") {
			return true
		}
		if field.Nested != nil && DescendantPath(field.Nested.ResolvedFields(), path) {
			return true
		}
	}
	return false
}
