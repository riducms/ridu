// Package localization owns locale request resolution and the logical
// single-locale/canonical-storage transform shared by operation engines and
// strict store fixtures.
package localization

import (
	"fmt"
	"math"
	"strings"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Selection is one validated request locale and its effective fallback chain.
type Selection struct {
	Locale     schema.LocaleCode
	Chain      []schema.LocaleCode
	All        bool
	Configured []schema.LocaleCode
	// PreserveNull is used only while completing write patches. Read projections
	// normally skip null locale values so fallback can select another locale.
	PreserveNull bool
}

// Resolve validates request locale input. ExplicitFallback replaces configured
// fallback behavior. DisableFallback prevents both locale and default fallback.
func Resolve(settings *schema.LocalizationSettings, locale string, explicitFallback []schema.LocaleCode, disableFallback, all bool) (Selection, error) {
	if settings == nil {
		if locale != "" || len(explicitFallback) != 0 || disableFallback || all {
			return Selection{}, fmt.Errorf("content localization is not configured")
		}
		return Selection{}, nil
	}
	configured := settings.LocaleCodes()
	byCode := make(map[schema.LocaleCode]schema.Locale, len(settings.Locales))
	for _, item := range settings.Locales {
		byCode[item.Code] = item
	}
	if locale == "all" || locale == "*" {
		all = true
		locale = ""
	}
	if all {
		if locale != "" || len(explicitFallback) != 0 {
			return Selection{}, fmt.Errorf("all-locales requests cannot select fallback locales")
		}
		return Selection{All: true, Chain: []schema.LocaleCode{settings.DefaultLocale}, Configured: configured}, nil
	}
	selected := schema.LocaleCode(locale)
	if selected == "" {
		selected = settings.DefaultLocale
	}
	item, exists := byCode[selected]
	if !exists {
		return Selection{}, fmt.Errorf("unknown locale %q", selected)
	}
	chain := []schema.LocaleCode{selected}
	if !disableFallback {
		fallbacks := item.FallbackLocales
		if explicitFallback != nil {
			fallbacks = explicitFallback
		}
		for _, fallback := range fallbacks {
			if _, exists := byCode[fallback]; !exists {
				return Selection{}, fmt.Errorf("unknown fallback locale %q", fallback)
			}
			chain = appendUnique(chain, fallback)
		}
		if settings.Fallback {
			chain = appendUnique(chain, settings.DefaultLocale)
		}
	}
	return Selection{Locale: selected, Chain: chain, Configured: configured}, nil
}

func appendUnique(values []schema.LocaleCode, value schema.LocaleCode) []schema.LocaleCode {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// StoragePatch turns selected-locale authored input into the canonical locale
// map expected by stores without adding or deleting values for other locales.
func StoragePatch(fields []schema.Field, values store.Values, selection Selection) (store.Values, error) {
	if selection.Locale == "" && !selection.All {
		return store.CloneValues(values), nil
	}
	result := store.Values{}
	for _, field := range fields {
		value, exists := values[field.Name]
		if !exists {
			continue
		}
		localized, _, err := storageValue(field, value, selection)
		if err != nil {
			return nil, err
		}
		result[field.Name] = localized
	}
	return result, nil
}

// ProjectDocument selects localized values for hooks and API output. All
// locale requests retain canonical locale maps.
func ProjectDocument(document store.Document, fields []schema.Field, selection Selection) store.Document {
	if selection.Locale == "" && !selection.All {
		return store.CloneDocument(document)
	}
	projected := store.CloneDocument(document)
	projected.LocalizationSources = make(map[string]schema.LocaleCode)
	for _, field := range fields {
		canonical, exists := projected.Values[field.Name]
		if !exists {
			continue
		}
		value, visible, _ := projectValueAt(field, canonical, selection, field.Name, projected.LocalizationSources)
		if !visible {
			delete(projected.Values, field.Name)
			continue
		}
		projected.Values[field.Name] = value
	}
	if len(projected.LocalizationSources) == 0 {
		projected.LocalizationSources = nil
	}
	return projected
}

// MergeStoragePatch combines partial locale maps during an update.
func MergeStoragePatch(fields []schema.Field, current, patch store.Values) store.Values {
	result := store.CloneValues(current)
	for _, field := range fields {
		next, exists := patch[field.Name]
		if !exists {
			continue
		}
		previous := result[field.Name]
		result[field.Name], _ = mergeValue(field, previous, next)
	}
	return result
}

// MergeStorageUpdate expands submitted structured fields against their current
// canonical value while keeping the update limited to top-level fields present
// in the patch. PostgreSQL stores groups, arrays, and blocks as one JSON value,
// so this makes its patch semantics match stores that merge those values in
// memory: omitted nested fields are preserved, explicit nulls still clear, and
// keyed row lists may still remove whole rows by omitting the row itself.
func MergeStorageUpdate(fields []schema.Field, current, patch store.Values) store.Values {
	result := store.CloneValues(patch)
	for _, field := range fields {
		next, exists := patch[field.Name]
		if !exists {
			continue
		}
		if field.Localized || field.Type == schema.FieldTypeGroup || field.Type == schema.FieldTypeArray || field.Type == schema.FieldTypeBlocks || embedded.HasFields(field) {
			result[field.Name], _ = mergeValue(field, current[field.Name], next)
		}
	}
	return result
}

// LocalizedValues extracts only localized authored values from one projected
// document. Row identity and block discriminators are retained so a
// copy-to-locale update can merge nested values without replacing shared
// structure or non-localized siblings.
func LocalizedValues(fields []schema.Field, values store.Values) store.Values {
	return localizedValues(fields, func(name string) (store.Value, bool) {
		value, exists := values[name]
		return value, exists
	})
}

func localizedValues(fields []schema.Field, lookup func(string) (store.Value, bool)) store.Values {
	result := store.Values{}
	for _, field := range fields {
		value, exists := lookup(field.Name)
		if !exists {
			continue
		}
		localized, include := localizedValue(field, value)
		if include {
			result[field.Name] = localized
		}
	}
	return result
}

// CopyLocaleIssues rejects structured localized copies that cannot match rows
// back to their canonical storage counterparts. Positional matching would let
// stale or reordered input overwrite non-localized siblings, so row keys are a
// safety requirement for non-localized arrays and blocks with localized
// descendants.
func CopyLocaleIssues(fields []schema.Field, values store.Values) []schema.Issue {
	return copyLocaleIssues(fields, func(name string) (store.Value, bool) {
		value, exists := values[name]
		return value, exists
	}, "")
}

func copyLocaleIssues(fields []schema.Field, lookup func(string) (store.Value, bool), prefix string) []schema.Issue {
	var issues []schema.Issue
	for _, field := range fields {
		if field.Localized {
			continue
		}
		value, exists := lookup(field.Name)
		if !exists {
			continue
		}
		path := field.Name
		if prefix != "" {
			path = prefix + "." + field.Name
		}
		if embedded.HasFields(field) {
			err := embedded.Visit(field, value, path, embedded.NewBudget(), func(o embedded.ReadOccurrence) error {
				if fieldsHaveLocalization(o.Fields) && o.Key == "" {
					issues = append(issues, schema.Issue{Code: "missing_row_key", Path: o.RuntimePath + "." + o.Case.Identity, Message: "localized copy requires a non-empty stable occurrence identity"})
				}
				issues = append(issues, copyLocaleIssues(o.Fields, o.Payload.Lookup, o.RuntimePath)...)
				return nil
			})
			if problem, ok := err.(*embedded.Error); ok {
				issues = append(issues, problem.Issue)
			}
			continue
		}
		switch field.Type {
		case schema.FieldTypeGroup:
			if value.Kind() == store.ValueObject && field.Nested != nil {
				issues = append(issues, copyLocaleIssues(field.Nested.ResolvedFields(), value.Lookup, path)...)
			}
		case schema.FieldTypeArray:
			if value.Kind() != store.ValueList || field.Nested == nil || !fieldsHaveLocalization(field.Nested.ResolvedFields()) {
				continue
			}
			issues = append(issues, copyLocaleRowIssues(value, path)...)
			index := -1
			for row := range value.Elements() {
				index++
				if row.Kind() == store.ValueObject {
					issues = append(issues, copyLocaleIssues(field.Nested.ResolvedFields(), row.Lookup, fmt.Sprintf("%s.%d", path, index))...)
				}
			}
		case schema.FieldTypeBlocks:
			if value.Kind() != store.ValueList || field.Blocks == nil {
				continue
			}
			hasLocalization := false
			for _, block := range field.Blocks.ResolvedTypes() {
				hasLocalization = hasLocalization || fieldsHaveLocalization(block.ResolvedFields())
			}
			if !hasLocalization {
				continue
			}
			issues = append(issues, copyLocaleRowIssues(value, path)...)
			index := -1
			for row := range value.Elements() {
				index++
				if row.Kind() != store.ValueObject {
					continue
				}
				blockType, _ := row.Get("blockType").StringValue()
				for _, block := range field.Blocks.ResolvedTypes() {
					if block.Slug == blockType {
						issues = append(issues, copyLocaleIssues(block.ResolvedFields(), row.Lookup, fmt.Sprintf("%s.%d", path, index))...)
						break
					}
				}
			}
		}
	}
	return issues
}

func copyLocaleRowIssues(rows store.Value, path string) []schema.Issue {
	seen := make(map[string]int, rows.Len())
	var issues []schema.Issue
	index := -1
	for row := range rows.Elements() {
		index++
		if row.Kind() != store.ValueObject {
			continue
		}
		key, valid := row.Get("_key").StringValue()
		keyPath := fmt.Sprintf("%s.%d._key", path, index)
		if !valid || strings.TrimSpace(key) == "" {
			issues = append(issues, schema.Issue{Code: "missing_row_key", Path: keyPath, Message: "localized copy requires a non-empty stable row key"})
			continue
		}
		if first, duplicate := seen[key]; duplicate {
			issues = append(issues, schema.Issue{Code: "duplicate_row_key", Path: keyPath, Message: fmt.Sprintf("row key %q duplicates %s.%d._key", key, path, first)})
			continue
		}
		seen[key] = index
	}
	return issues
}

func localizedValue(field schema.Field, value store.Value) (store.Value, bool) {
	if field.Localized {
		return value, true
	}
	if embedded.HasFields(field) {
		if !hasLocalizedDescendant(field) {
			return store.Value{}, false
		}
		transformed, err := embedded.TransformValue(field, value, field.Name, embedded.NewBudget(), func(o embedded.ReadOccurrence) (store.Value, bool, error) {
			children := localizedValues(o.Fields, o.Payload.Lookup)
			copyReserved(o.Payload, children, o.Case.Identity)
			copyReserved(o.Payload, children, o.Case.Discriminator)
			return store.Object(children), true, nil
		})
		return transformed, err == nil
	}
	switch field.Type {
	case schema.FieldTypeGroup:
		if value.Kind() != store.ValueObject || field.Nested == nil {
			return store.Value{}, false
		}
		children := localizedValues(field.Nested.ResolvedFields(), value.Lookup)
		return store.Object(children), len(children) > 0
	case schema.FieldTypeArray:
		if value.Kind() != store.ValueList || field.Nested == nil || !fieldsHaveLocalization(field.Nested.ResolvedFields()) {
			return store.Value{}, false
		}
		result := make([]store.Value, 0, value.Len())
		for item := range value.Elements() {
			if item.Kind() != store.ValueObject {
				continue
			}
			row := localizedValues(field.Nested.ResolvedFields(), item.Lookup)
			copyReserved(item, row, "_key")
			result = append(result, store.Object(row))
		}
		return store.List(result...), true
	case schema.FieldTypeBlocks:
		if value.Kind() != store.ValueList || field.Blocks == nil {
			return store.Value{}, false
		}
		hasLocalizedBlock := false
		for _, block := range field.Blocks.ResolvedTypes() {
			hasLocalizedBlock = hasLocalizedBlock || fieldsHaveLocalization(block.ResolvedFields())
		}
		if !hasLocalizedBlock {
			return store.Value{}, false
		}
		result := make([]store.Value, 0, value.Len())
		for item := range value.Elements() {
			if item.Kind() != store.ValueObject {
				continue
			}
			blockType, _ := item.Get("blockType").StringValue()
			row := store.Values{}
			for _, block := range field.Blocks.ResolvedTypes() {
				if block.Slug == blockType {
					row = localizedValues(block.ResolvedFields(), item.Lookup)
					break
				}
			}
			copyReserved(item, row, "_key")
			copyReserved(item, row, "blockType")
			result = append(result, store.Object(row))
		}
		return store.List(result...), true
	default:
		return store.Value{}, false
	}
}

func fieldsHaveLocalization(fields []schema.Field) bool {
	for _, field := range fields {
		if field.Localized || hasLocalizedDescendant(field) {
			return true
		}
	}
	return false
}

func copyReserved(source store.Value, target store.Values, name string) {
	if value, exists := source.Lookup(name); exists {
		target[name] = value
	}
}

// Schema-only checks avoid inspecting wide ordinary values when no descendant
// needs localization. Embedded descriptors still require traversal for their
// envelope validation and budgets, even when their payloads are not localized.
func hasLocalizedDescendant(field schema.Field) bool {
	return childFieldsMatch(field, fieldsHaveLocalization)
}

func fieldsNeedTraversal(fields []schema.Field) bool {
	for _, field := range fields {
		if field.Localized || embedded.HasFields(field) || childFieldsMatch(field, fieldsNeedTraversal) {
			return true
		}
	}
	return false
}

func childFieldsMatch(field schema.Field, matches func([]schema.Field) bool) bool {
	if field.Nested != nil && matches(field.Nested.ResolvedFields()) {
		return true
	}
	if field.Blocks != nil {
		for _, block := range field.Blocks.ResolvedTypes() {
			if matches(block.ResolvedFields()) {
				return true
			}
		}
	}
	if field.Plugin != nil {
		for _, tree := range field.Plugin.EmbeddedTrees {
			for _, c := range tree.Cases {
				for _, block := range c.ResolvedTypes() {
					if matches(block.ResolvedFields()) {
						return true
					}
				}
			}
		}
	}
	return false
}

func storageValue(field schema.Field, value store.Value, selection Selection) (store.Value, bool, error) {
	if field.Localized {
		if selection.All {
			if value.Kind() != store.ValueObject {
				return store.Value{}, false, fmt.Errorf("localized field %q must be a locale-keyed object for an all-locales write", field.Path.String())
			}
			for code := range value.Entries() {
				if !contains(selection.Configured, schema.LocaleCode(code)) {
					return store.Value{}, false, fmt.Errorf("localized field %q contains unknown locale %q", field.Path.String(), code)
				}
			}
			return value, false, nil
		}
		return store.Object(store.Values{string(selection.Locale): value}), true, nil
	}
	return mapChildObjects(field, value, "", func(fields []schema.Field, object store.Value, _ string) (store.Value, bool, error) {
		return transformObject(fields, object, func(child schema.Field, childValue store.Value) (store.Value, bool, error) {
			return storageValue(child, childValue, selection)
		})
	})
}

func projectValueAt(field schema.Field, value store.Value, selection Selection, path string, sources map[string]schema.LocaleCode) (store.Value, bool, bool) {
	if field.Localized {
		if selection.All {
			return value, true, false
		}
		if value.Kind() != store.ValueObject {
			return store.Value{}, false, true
		}
		for index, locale := range selection.Chain {
			candidate, exists := value.Lookup(string(locale))
			if exists && (candidate.Kind() != store.ValueNull || selection.PreserveNull) {
				if text, stringValue := candidate.StringValue(); stringValue && text == "" && index < len(selection.Chain)-1 {
					continue
				}
				sources[path] = locale
				return candidate, true, true
			}
		}
		return store.Value{}, false, true
	}
	transformed, changed, err := mapChildObjects(field, value, path, func(fields []schema.Field, object store.Value, objectPath string) (store.Value, bool, error) {
		projected, changed := projectObject(fields, object, selection, objectPath, sources)
		return projected, changed, nil
	})
	return transformed, err == nil, changed
}

func projectObject(fields []schema.Field, object store.Value, selection Selection, parentPath string, sources map[string]schema.LocaleCode) (store.Value, bool) {
	var result store.Values
	for _, field := range fields {
		if !field.Localized && !embedded.HasFields(field) && !childFieldsMatch(field, fieldsNeedTraversal) {
			continue
		}
		value, exists := object.Lookup(field.Name)
		if !exists {
			continue
		}
		projected, visible, changed := projectValueAt(field, value, selection, parentPath+"."+field.Name, sources)
		if visible && !changed {
			continue
		}
		if result == nil {
			result, _ = object.CopyObject()
		}
		if visible {
			result[field.Name] = projected
		} else {
			delete(result, field.Name)
		}
	}
	if result == nil {
		return object, false
	}
	return store.Object(result), true
}

type objectTransform func([]schema.Field, store.Value, string) (store.Value, bool, error)

// mapChildObjects retains untouched containers. A changed list is materialized
// once, rather than path-copying it once per changed row.
func mapChildObjects(field schema.Field, value store.Value, path string, transform objectTransform) (store.Value, bool, error) {
	if embedded.HasFields(field) {
		changed := false
		prefix := path
		if prefix == "" {
			prefix = field.Name
		}
		transformed, err := embedded.TransformValue(field, value, prefix, embedded.NewBudget(), func(o embedded.ReadOccurrence) (store.Value, bool, error) {
			next, updated, err := transform(o.Fields, o.Payload, o.RuntimePath)
			changed = changed || updated
			return next, updated, err
		})
		return transformed, changed, err
	}
	if !childFieldsMatch(field, fieldsNeedTraversal) {
		return value, false, nil
	}
	if field.Type == schema.FieldTypeGroup {
		if value.Kind() != store.ValueObject || field.Nested == nil {
			return value, false, nil
		}
		return transform(field.Nested.ResolvedFields(), value, path)
	}
	if value.Kind() != store.ValueList || (field.Type != schema.FieldTypeArray && field.Type != schema.FieldTypeBlocks) {
		return value, false, nil
	}
	var items []store.Value
	index := -1
	for item := range value.Elements() {
		index++
		if item.Kind() != store.ValueObject {
			continue
		}
		var fields []schema.Field
		if field.Type == schema.FieldTypeArray && field.Nested != nil {
			fields = field.Nested.ResolvedFields()
		} else if field.Type == schema.FieldTypeBlocks && field.Blocks != nil {
			blockType, _ := item.Get("blockType").StringValue()
			for _, block := range field.Blocks.ResolvedTypes() {
				if block.Slug == blockType {
					fields = block.ResolvedFields()
					break
				}
			}
		}
		if !fieldsNeedTraversal(fields) {
			continue
		}
		itemPath := ""
		if path != "" {
			itemPath = fmt.Sprintf("%s.%d", path, index)
		}
		transformed, changed, err := transform(fields, item, itemPath)
		if err != nil {
			return store.Value{}, false, err
		}
		if changed {
			if items == nil {
				items, _ = value.CopyList()
			}
			items[index] = transformed
		}
	}
	if items == nil {
		return value, false, nil
	}
	return store.List(items...), true, nil
}

func transformObject(fields []schema.Field, object store.Value, transform func(schema.Field, store.Value) (store.Value, bool, error)) (store.Value, bool, error) {
	var result store.Values
	for _, child := range fields {
		value, exists := object.Lookup(child.Name)
		if !exists {
			continue
		}
		transformed, changed, err := transform(child, value)
		if err != nil {
			return store.Value{}, false, err
		}
		if changed {
			if result == nil {
				result, _ = object.CopyObject()
			}
			result[child.Name] = transformed
		}
	}
	if result == nil {
		return object, false, nil
	}
	return store.Object(result), true, nil
}

// Merge change flags are relative to the patch: complete submitted objects can
// be retained even when their scalar values differ from current storage. Only
// omitted members or recursively completed children require a new container.
func mergeValue(field schema.Field, current, patch store.Value) (store.Value, bool) {
	if field.Localized {
		if patch.Kind() != store.ValueObject {
			return patch, false
		}
		if unchanged, complete := unchangedValuePatch(field, current, patch); unchanged {
			if complete {
				return patch, false
			}
			return current, true
		}
		merged := omittedMembers(current, patch)
		unlocalized := field
		unlocalized.Localized = false
		for locale, value := range patch.Entries() {
			previous, _ := current.Lookup(locale)
			next, changed := mergeValue(unlocalized, previous, value)
			if changed {
				if merged == nil {
					merged, _ = patch.CopyObject()
				}
				merged[locale] = next
			}
		}
		if merged == nil {
			return patch, false
		}
		return store.Object(merged), true
	}
	if embedded.HasFields(field) {
		previous := map[string]store.Value{}
		err := embedded.Visit(field, current, field.Name, embedded.NewBudget(), func(o embedded.ReadOccurrence) error {
			if o.Key != "" {
				previous[o.Identity] = o.Payload
			}
			return nil
		})
		if err != nil {
			return patch, false
		}
		changed := false
		transformed, err := embedded.TransformValue(field, patch, field.Name, embedded.NewBudget(), func(o embedded.ReadOccurrence) (store.Value, bool, error) {
			old, exists := previous[o.Identity]
			if !exists {
				return o.Payload, false, nil
			}
			merged, updated := mergeObjectValue(o.Fields, old, o.Payload)
			changed = changed || updated
			return merged, updated, nil
		})
		if err != nil {
			return patch, false
		}
		return transformed, changed
	}

	switch field.Type {
	case schema.FieldTypeGroup:
		if field.Nested != nil {
			return mergeObjectValue(field.Nested.ResolvedFields(), current, patch)
		}
	case schema.FieldTypeArray:
		return mergeRows(field.Nested, nil, current, patch)
	case schema.FieldTypeBlocks:
		return mergeRows(nil, field.Blocks, current, patch)
	}
	return patch, false
}

func omittedMembers(current, patch store.Value) store.Values {
	var merged store.Values
	for name, value := range current.Entries() {
		if _, exists := patch.Lookup(name); !exists {
			if merged == nil {
				merged, _ = patch.CopyObject()
			}
			merged[name] = value
		}
	}
	return merged
}

func mergeObjectValue(fields []schema.Field, current, patch store.Value) (store.Value, bool) {
	if current.Kind() != store.ValueObject || patch.Kind() != store.ValueObject {
		return patch, false
	}
	if unchanged, complete := unchangedObjectPatch(fields, current, patch); unchanged {
		if complete {
			return patch, false
		}
		return current, true
	}
	merged := omittedMembers(current, patch)
	for _, field := range fields {
		if value, exists := patch.Lookup(field.Name); exists {
			next, changed := mergeValue(field, current.Get(field.Name), value)
			if changed {
				if merged == nil {
					merged, _ = patch.CopyObject()
				}
				merged[field.Name] = next
			}
		}
	}
	if merged == nil {
		return patch, false
	}
	return store.Object(merged), true
}

// unchangedObjectPatch recognizes sparse no-op updates by inspecting only
// submitted members. This lets identity-only rows and empty group patches reuse
// current storage without first copying all their omitted members. Complete
// patches can still be retained directly by the caller.
func unchangedObjectPatch(fields []schema.Field, current, patch store.Value) (unchanged, complete bool) {
	if current.Kind() != store.ValueObject || patch.Kind() != store.ValueObject {
		return false, false
	}
	complete = current.Len() == patch.Len()
	pendingObjects := 0
	for name, value := range patch.Entries() {
		previous, exists := current.Lookup(name)
		if !exists {
			return false, false
		}
		if value.Kind() == store.ValueObject {
			if previous.Kind() != store.ValueObject {
				return false, false
			}
			pendingObjects++
			continue
		}
		if same, _ := unchangedValuePatch(schema.Field{}, previous, value); !same {
			return false, false
		}
	}
	if pendingObjects == 0 {
		return true, complete
	}
	// Scalar comparisons need no schema search. Resolve compound members in a
	// single schema pass, keeping wide objects linear without a per-row index.
	for _, field := range fields {
		value, exists := patch.Lookup(field.Name)
		if !exists || value.Kind() != store.ValueObject {
			continue
		}
		previous, _ := current.Lookup(field.Name)
		same, childComplete := unchangedValuePatch(field, previous, value)
		if !same {
			return false, false
		}
		complete = complete && childComplete
		pendingObjects--
		if pendingObjects == 0 {
			return true, complete
		}
	}
	// An unresolved object is opaque or unknown, so it cannot use this shortcut.
	return false, false
}

func unchangedValuePatch(field schema.Field, current, patch store.Value) (bool, bool) {
	if current.Kind() != patch.Kind() {
		return false, false
	}
	// Scalars compare without visiting child containers. Float bits retain signed zero;
	// Lookup in the caller distinguishes explicit null from an absent member.
	switch patch.Kind() {
	case store.ValueNull:
		return true, true
	case store.ValueString:
		previous, _ := current.StringValue()
		next, _ := patch.StringValue()
		return previous == next, true
	case store.ValueNumber:
		previous, _ := current.NumberValue()
		next, _ := patch.NumberValue()
		return math.Float64bits(previous) == math.Float64bits(next), true
	case store.ValueBoolean:
		previous, _ := current.BooleanValue()
		next, _ := patch.BooleanValue()
		return previous == next, true
	}
	if field.Localized && patch.Kind() == store.ValueObject {
		unlocalized := field
		unlocalized.Localized = false
		complete := current.Len() == patch.Len()
		for locale, value := range patch.Entries() {
			previous, exists := current.Lookup(locale)
			if !exists {
				return false, false
			}
			same, childComplete := unchangedValuePatch(unlocalized, previous, value)
			if !same {
				return false, false
			}
			complete = complete && childComplete
		}
		return true, complete
	}
	if field.Type == schema.FieldTypeGroup && field.Nested != nil && !embedded.HasFields(field) {
		return unchangedObjectPatch(field.Nested.ResolvedFields(), current, patch)
	}
	// Lists, populated documents, opaque objects and embedded envelopes retain
	// their normal merge/replacement and admission paths. Do not deep-compare them.
	return false, false
}

func mergeRows(nested *schema.NestedField, blocks *schema.BlocksField, current, patch store.Value) (store.Value, bool) {
	if current.Kind() != store.ValueList || patch.Kind() != store.ValueList {
		return patch, false
	}
	byKey := make(map[string]store.Value, current.Len())
	for row := range current.Elements() {
		key, _ := row.Get("_key").StringValue()
		if key != "" {
			byKey[key] = row
		}
	}

	var nextRows []store.Value
	index := -1
	for row := range patch.Elements() {
		index++
		if row.Kind() != store.ValueObject {
			continue
		}
		key, _ := row.Get("_key").StringValue()
		previous := byKey[key]
		var fields []schema.Field
		if nested != nil {
			fields = nested.ResolvedFields()
		} else if blocks != nil {
			blockType, _ := row.Get("blockType").StringValue()
			for _, block := range blocks.ResolvedTypes() {
				if block.Slug == blockType {
					fields = block.ResolvedFields()
					break
				}
			}
		}
		merged, changed := mergeObjectValue(fields, previous, row)
		if changed {
			if nextRows == nil {
				nextRows, _ = patch.CopyList()
			}
			nextRows[index] = merged
		}
	}
	if nextRows == nil {
		return patch, false
	}
	return store.List(nextRows...), true
}

func contains(values []schema.LocaleCode, value schema.LocaleCode) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

// ParseFallbackQuery accepts Payload-compatible disable tokens or a
// comma-separated fallback chain. A nil slice means no explicit override.
func ParseFallbackQuery(value string) ([]schema.LocaleCode, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false, nil
	}
	switch strings.ToLower(value) {
	case "false", "none", "null":
		return []schema.LocaleCode{}, true, nil
	}
	parts := strings.Split(value, ",")
	result := make([]schema.LocaleCode, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !schema.IsValidLocaleCode(part) {
			return nil, false, fmt.Errorf("invalid fallback locale %q", part)
		}
		result = append(result, schema.LocaleCode(part))
	}
	return result, false, nil
}
