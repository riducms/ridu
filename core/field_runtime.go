package core

import (
	"fmt"
	"sort"

	"github.com/riducms/ridu/schema"
)

type fieldRuntimeKind uint8

const (
	fieldRuntimeStored fieldRuntimeKind = iota
	fieldRuntimeOutput
	fieldRuntimePresentation
)

func validateFieldRuntime(config Config, manifest schema.Manifest) error {
	snapshot := manifest.Snapshot()
	var issues []schema.Issue
	validate := func(prefix string, fields []schema.Field, access map[string]FieldAccess, hooks map[string]CollectionHooks, global bool) {
		targets := make(map[string]fieldRuntimeKind)
		indexFieldRuntimeTargets(fields, targets)

		for _, path := range sortedRuntimePaths(access) {
			rules := access[path]
			kind, exists := targets[path]
			if !exists {
				issues = append(issues, schema.Issue{
					Code:    "unknown_field_access",
					Path:    fmt.Sprintf("%s.fieldAccess[%q]", prefix, path),
					Message: fmt.Sprintf("field access path %q does not name a resolved field", path),
				})
				continue
			}
			switch {
			case kind == fieldRuntimePresentation:
				issues = append(issues, schema.Issue{
					Code:    "incompatible_field_access",
					Path:    fmt.Sprintf("%s.fieldAccess[%q]", prefix, path),
					Message: fmt.Sprintf("field access path %q targets a presentation-only field with no runtime value", path),
				})
			case global && rules.Create != nil:
				issues = append(issues, schema.Issue{
					Code:    "incompatible_field_access",
					Path:    fmt.Sprintf("%s.fieldAccess[%q]", prefix, path),
					Message: fmt.Sprintf("global field access path %q cannot configure create access; singleton initialization is an update operation", path),
				})
			case kind == fieldRuntimeOutput && (rules.Create != nil || rules.Update != nil):
				issues = append(issues, schema.Issue{
					Code:    "incompatible_field_access",
					Path:    fmt.Sprintf("%s.fieldAccess[%q]", prefix, path),
					Message: fmt.Sprintf("field access path %q targets an output-only field; only read access may be configured", path),
				})
			}
		}

		for _, path := range sortedRuntimePaths(hooks) {
			fieldHooks := hooks[path]
			kind, exists := targets[path]
			if !exists {
				issues = append(issues, schema.Issue{
					Code:    "unknown_field_hook",
					Path:    fmt.Sprintf("%s.fieldHooks[%q]", prefix, path),
					Message: fmt.Sprintf("field hook path %q does not name a resolved field", path),
				})
				continue
			}
			if phases := unsupportedFieldHookPhases(fieldHooks, global); len(phases) != 0 {
				issues = append(issues, schema.Issue{
					Code:    "incompatible_field_hook",
					Path:    fmt.Sprintf("%s.fieldHooks[%q]", prefix, path),
					Message: fmt.Sprintf("field hook path %q configures unsupported phases: %v", path, phases),
				})
				continue
			}
			if kind == fieldRuntimePresentation {
				issues = append(issues, schema.Issue{
					Code:    "incompatible_field_hook",
					Path:    fmt.Sprintf("%s.fieldHooks[%q]", prefix, path),
					Message: fmt.Sprintf("field hook path %q targets a field without a stored lifecycle value", path),
				})
			} else if kind == fieldRuntimeOutput && hasHooksOutsideAfterRead(fieldHooks) {
				issues = append(issues, schema.Issue{
					Code:    "incompatible_field_hook",
					Path:    fmt.Sprintf("%s.fieldHooks[%q]", prefix, path),
					Message: fmt.Sprintf("field hook path %q targets an output-only field; only after-read hooks may be configured", path),
				})
			}
		}
	}

	for index, collection := range config.Collections {
		validate(fmt.Sprintf("collections[%d]", index), snapshot.Collections[index].Fields, collection.FieldAccess, collection.FieldHooks, false)
	}
	for index, global := range config.Globals {
		prefix := fmt.Sprintf("globals[%d]", index)
		validate(prefix, snapshot.Globals[index].Fields, global.FieldAccess, global.FieldHooks, true)
		if phases := unsupportedGlobalHookPhases(global.Hooks); len(phases) != 0 {
			issues = append(issues, schema.Issue{
				Code:    "incompatible_global_hook",
				Path:    prefix + ".hooks",
				Message: fmt.Sprintf("global hooks configure unsupported phases: %v", phases),
			})
		}
	}
	if len(issues) != 0 {
		return schema.NewValidationError(issues)
	}
	return nil
}

func unsupportedGlobalHookPhases(hooks CollectionHooks) []string {
	var phases []string
	if len(hooks.BeforeDuplicate) != 0 {
		phases = append(phases, "BeforeDuplicate")
	}
	if len(hooks.BeforeDelete) != 0 {
		phases = append(phases, "BeforeDelete")
	}
	if len(hooks.AfterDelete) != 0 {
		phases = append(phases, "AfterDelete")
	}
	return phases
}

func unsupportedFieldHookPhases(hooks CollectionHooks, global bool) []string {
	var phases []string
	if global && len(hooks.BeforeDuplicate) != 0 {
		phases = append(phases, "BeforeDuplicate")
	}
	if len(hooks.BeforeRead) != 0 {
		phases = append(phases, "BeforeRead")
	}
	if global && len(hooks.BeforeDelete) != 0 {
		phases = append(phases, "BeforeDelete")
	}
	if global && len(hooks.AfterDelete) != 0 {
		phases = append(phases, "AfterDelete")
	}
	if len(hooks.AfterError) != 0 {
		phases = append(phases, "AfterError")
	}
	return phases
}

func hasHooksOutsideAfterRead(hooks CollectionHooks) bool {
	return len(hooks.BeforeDuplicate) != 0 ||
		len(hooks.BeforeValidate) != 0 ||
		len(hooks.BeforeChange) != 0 ||
		len(hooks.BeforeOperation) != 0 ||
		len(hooks.BeforeRead) != 0 ||
		len(hooks.BeforeDelete) != 0 ||
		len(hooks.AfterChange) != 0 ||
		len(hooks.AfterDelete) != 0 ||
		len(hooks.AfterOperation) != 0 ||
		len(hooks.AfterError) != 0 ||
		len(hooks.AfterCommit) != 0
}

func indexFieldRuntimeTargets(fields []schema.Field, targets map[string]fieldRuntimeKind) {
	for _, candidate := range fields {
		kind := fieldRuntimeStored
		switch candidate.Type {
		case schema.FieldTypeJoin, schema.FieldTypeVirtual:
			kind = fieldRuntimeOutput
		case schema.FieldTypeUI:
			kind = fieldRuntimePresentation
		}
		targets[candidate.Path.String()] = kind
		if candidate.Nested != nil {
			indexFieldRuntimeTargets(candidate.Nested.Fields, targets)
		}
		if candidate.Blocks != nil {
			for _, block := range candidate.Blocks.Types {
				indexFieldRuntimeTargets(block.Fields, targets)
			}
		}
	}
}

func sortedRuntimePaths[Value any](values map[string]Value) []string {
	paths := make([]string, 0, len(values))
	for path := range values {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
