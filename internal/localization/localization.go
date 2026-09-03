// Package localization owns locale request resolution and the logical
// single-locale/canonical-storage transform shared by operation engines and
// strict store fixtures.
package localization

import (
	"fmt"
	"strings"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Selection is one validated request locale and its effective fallback chain.
type Selection struct {
	Locale     schema.LocaleCode
	Chain      []schema.LocaleCode
	All        bool
	Configured []schema.LocaleCode
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
		localized, err := storageValue(field, value, selection)
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
		value, visible := projectValueAt(field, canonical, selection, field.Name, projected.LocalizationSources)
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
		result[field.Name] = mergeValue(field, previous, next)
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
		if field.Localized || field.Type == schema.FieldTypeGroup || field.Type == schema.FieldTypeArray || field.Type == schema.FieldTypeBlocks {
			result[field.Name] = mergeValue(field, current[field.Name], next)
		}
	}
	return result
}

// LocalizedValues extracts only localized authored values from one projected
// document. Row identity and block discriminators are retained so a
// copy-to-locale update can merge nested values without replacing shared
// structure or non-localized siblings.
func LocalizedValues(fields []schema.Field, values store.Values) store.Values {
	result := store.Values{}
	for _, field := range fields {
		value, exists := values[field.Name]
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
	return copyLocaleIssues(fields, values, "")
}

func copyLocaleIssues(fields []schema.Field, values store.Values, prefix string) []schema.Issue {
	var issues []schema.Issue
	for _, field := range fields {
		if field.Localized {
			continue
		}
		value, exists := values[field.Name]
		if !exists {
			continue
		}
		path := field.Name
		if prefix != "" {
			path = prefix + "." + field.Name
		}
		switch field.Type {
		case schema.FieldTypeGroup:
			object, valid := value.ObjectValue()
			if valid && field.Nested != nil {
				issues = append(issues, copyLocaleIssues(field.Nested.Fields, object, path)...)
			}
		case schema.FieldTypeArray:
			rows, valid := value.Values()
			if !valid || field.Nested == nil || !fieldsHaveLocalization(field.Nested.Fields) {
				continue
			}
			issues = append(issues, copyLocaleRowIssues(rows, path)...)
			for index, row := range rows {
				object, valid := row.ObjectValue()
				if valid {
					issues = append(issues, copyLocaleIssues(field.Nested.Fields, object, fmt.Sprintf("%s.%d", path, index))...)
				}
			}
		case schema.FieldTypeBlocks:
			rows, valid := value.Values()
			if !valid || field.Blocks == nil {
				continue
			}
			hasLocalization := false
			for _, block := range field.Blocks.Types {
				hasLocalization = hasLocalization || fieldsHaveLocalization(block.Fields)
			}
			if !hasLocalization {
				continue
			}
			issues = append(issues, copyLocaleRowIssues(rows, path)...)
			for index, row := range rows {
				object, valid := row.ObjectValue()
				if !valid {
					continue
				}
				blockType, _ := object["blockType"].StringValue()
				for _, block := range field.Blocks.Types {
					if block.Key == blockType {
						issues = append(issues, copyLocaleIssues(block.Fields, object, fmt.Sprintf("%s.%d", path, index))...)
						break
					}
				}
			}
		}
	}
	return issues
}

func copyLocaleRowIssues(rows []store.Value, path string) []schema.Issue {
	seen := make(map[string]int, len(rows))
	var issues []schema.Issue
	for index, row := range rows {
		object, valid := row.ObjectValue()
		if !valid {
			continue
		}
		key, valid := object["_key"].StringValue()
		key = strings.TrimSpace(key)
		keyPath := fmt.Sprintf("%s.%d._key", path, index)
		if !valid || key == "" {
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
	switch field.Type {
	case schema.FieldTypeGroup:
		object, ok := value.ObjectValue()
		if !ok || field.Nested == nil {
			return store.Value{}, false
		}
		children := LocalizedValues(field.Nested.Fields, object)
		return store.Object(children), len(children) > 0
	case schema.FieldTypeArray:
		items, ok := value.Values()
		if !ok || field.Nested == nil || !fieldsHaveLocalization(field.Nested.Fields) {
			return store.Value{}, false
		}
		result := make([]store.Value, 0, len(items))
		for _, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			row := LocalizedValues(field.Nested.Fields, object)
			copyReserved(object, row, "_key")
			result = append(result, store.Object(row))
		}
		return store.List(result...), true
	case schema.FieldTypeBlocks:
		items, ok := value.Values()
		if !ok || field.Blocks == nil {
			return store.Value{}, false
		}
		hasLocalizedBlock := false
		for _, block := range field.Blocks.Types {
			hasLocalizedBlock = hasLocalizedBlock || fieldsHaveLocalization(block.Fields)
		}
		if !hasLocalizedBlock {
			return store.Value{}, false
		}
		result := make([]store.Value, 0, len(items))
		for _, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			blockType, _ := object["blockType"].StringValue()
			row := store.Values{}
			for _, block := range field.Blocks.Types {
				if block.Key == blockType {
					row = LocalizedValues(block.Fields, object)
					break
				}
			}
			copyReserved(object, row, "_key")
			copyReserved(object, row, "blockType")
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

func copyReserved(source, target store.Values, name string) {
	if value, exists := source[name]; exists {
		target[name] = value
	}
}

func hasLocalizedDescendant(field schema.Field) bool {
	if field.Nested != nil {
		for _, child := range field.Nested.Fields {
			if child.Localized || hasLocalizedDescendant(child) {
				return true
			}
		}
	}
	if field.Blocks != nil {
		for _, block := range field.Blocks.Types {
			for _, child := range block.Fields {
				if child.Localized || hasLocalizedDescendant(child) {
					return true
				}
			}
		}
	}
	return false
}

func storageValue(field schema.Field, value store.Value, selection Selection) (store.Value, error) {
	if field.Localized {
		if selection.All {
			object, ok := value.ObjectValue()
			if !ok {
				return store.Value{}, fmt.Errorf("localized field %q must be a locale-keyed object for an all-locales write", field.Path.String())
			}
			for code := range object {
				if !contains(selection.Configured, schema.LocaleCode(code)) {
					return store.Value{}, fmt.Errorf("localized field %q contains unknown locale %q", field.Path.String(), code)
				}
			}
			return store.Object(object), nil
		}
		return store.Object(store.Values{string(selection.Locale): value}), nil
	}
	return transformChildren(field, value, func(child schema.Field, childValue store.Value) (store.Value, error) {
		return storageValue(child, childValue, selection)
	})
}

func projectValueAt(field schema.Field, value store.Value, selection Selection, path string, sources map[string]schema.LocaleCode) (store.Value, bool) {
	if field.Localized {
		if selection.All {
			return value, true
		}
		localized, ok := value.ObjectValue()
		if !ok {
			return store.Value{}, false
		}
		for index, locale := range selection.Chain {
			candidate, exists := localized[string(locale)]
			if exists && candidate.Kind() != store.ValueNull {
				if text, stringValue := candidate.StringValue(); stringValue && text == "" && index < len(selection.Chain)-1 {
					continue
				}
				sources[path] = locale
				return candidate, true
			}
		}
		return store.Value{}, false
	}
	switch field.Type {
	case schema.FieldTypeGroup:
		object, ok := value.ObjectValue()
		if !ok || field.Nested == nil {
			return value, true
		}
		return store.Object(projectObject(field.Nested.Fields, object, selection, path, sources)), true
	case schema.FieldTypeArray:
		items, ok := value.Values()
		if !ok || field.Nested == nil {
			return value, true
		}
		for index, item := range items {
			object, valid := item.ObjectValue()
			if valid {
				items[index] = store.Object(projectObject(field.Nested.Fields, object, selection, fmt.Sprintf("%s.%d", path, index), sources))
			}
		}
		return store.List(items...), true
	case schema.FieldTypeBlocks:
		items, ok := value.Values()
		if !ok || field.Blocks == nil {
			return value, true
		}
		for index, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			blockType, _ := object["blockType"].StringValue()
			for _, block := range field.Blocks.Types {
				if block.Key == blockType {
					items[index] = store.Object(projectObject(block.Fields, object, selection, fmt.Sprintf("%s.%d", path, index), sources))
					break
				}
			}
		}
		return store.List(items...), true
	default:
		return value, true
	}
}

func projectObject(fields []schema.Field, object store.Values, selection Selection, parentPath string, sources map[string]schema.LocaleCode) store.Values {
	result := store.CloneValues(object)
	for _, field := range fields {
		value, exists := object[field.Name]
		if !exists {
			continue
		}
		path := parentPath + "." + field.Name
		projected, visible := projectValueAt(field, value, selection, path, sources)
		if visible {
			result[field.Name] = projected
		} else {
			delete(result, field.Name)
		}
	}
	return result
}

type valueTransform func(schema.Field, store.Value) (store.Value, error)

func transformChildren(field schema.Field, value store.Value, transform valueTransform) (store.Value, error) {
	switch field.Type {
	case schema.FieldTypeGroup:
		object, ok := value.ObjectValue()
		if !ok || field.Nested == nil {
			return value, nil
		}
		transformed, err := transformObject(field.Nested.Fields, object, transform)
		return store.Object(transformed), err
	case schema.FieldTypeArray:
		items, ok := value.Values()
		if !ok || field.Nested == nil {
			return value, nil
		}
		for index, item := range items {
			object, valid := item.ObjectValue()
			if valid {
				transformed, err := transformObject(field.Nested.Fields, object, transform)
				if err != nil {
					return store.Value{}, err
				}
				items[index] = store.Object(transformed)
			}
		}
		return store.List(items...), nil
	case schema.FieldTypeBlocks:
		items, ok := value.Values()
		if !ok || field.Blocks == nil {
			return value, nil
		}
		for index, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			blockType, _ := object["blockType"].StringValue()
			for _, block := range field.Blocks.Types {
				if block.Key == blockType {
					transformed, err := transformObject(block.Fields, object, transform)
					if err != nil {
						return store.Value{}, err
					}
					items[index] = store.Object(transformed)
					break
				}
			}
		}
		return store.List(items...), nil
	default:
		return value, nil
	}
}

func transformObject(fields []schema.Field, object store.Values, transform valueTransform) (store.Values, error) {
	result := store.CloneValues(object)
	for _, child := range fields {
		value, exists := object[child.Name]
		if !exists {
			continue
		}
		transformed, err := transform(child, value)
		if err != nil {
			return nil, err
		}
		result[child.Name] = transformed
	}
	return result, nil
}

func mergeValue(field schema.Field, current, patch store.Value) store.Value {
	if field.Localized {
		previous, previousOK := current.ObjectValue()
		next, nextOK := patch.ObjectValue()
		if !nextOK {
			return patch
		}
		if !previousOK {
			previous = store.Values{}
		}
		unlocalized := field
		unlocalized.Localized = false
		for locale, value := range next {
			previous[locale] = mergeValue(unlocalized, previous[locale], value)
		}
		return store.Object(previous)
	}
	switch field.Type {
	case schema.FieldTypeGroup:
		return mergeObjectValue(field.Nested, current, patch)
	case schema.FieldTypeArray:
		return mergeRows(field.Nested, nil, current, patch)
	case schema.FieldTypeBlocks:
		return mergeRows(nil, field.Blocks, current, patch)
	default:
		return patch
	}
}

func mergeObjectValue(nested *schema.NestedField, current, patch store.Value) store.Value {
	previous, previousOK := current.ObjectValue()
	next, nextOK := patch.ObjectValue()
	if !previousOK || !nextOK || nested == nil {
		return patch
	}
	merged := store.CloneValues(previous)
	for name, value := range next {
		merged[name] = value
	}
	for _, field := range nested.Fields {
		value, exists := next[field.Name]
		if exists {
			merged[field.Name] = mergeValue(field, previous[field.Name], value)
		}
	}
	return store.Object(merged)
}

func mergeRows(nested *schema.NestedField, blocks *schema.BlocksField, current, patch store.Value) store.Value {
	previousRows, previousOK := current.Values()
	nextRows, nextOK := patch.Values()
	if !previousOK || !nextOK {
		return patch
	}
	byKey := make(map[string]store.Values, len(previousRows))
	for _, row := range previousRows {
		object, ok := row.ObjectValue()
		if !ok {
			continue
		}
		key, _ := object["_key"].StringValue()
		if key != "" {
			byKey[key] = object
		}
	}
	for index, row := range nextRows {
		object, ok := row.ObjectValue()
		if !ok {
			continue
		}
		key, _ := object["_key"].StringValue()
		previous := byKey[key]
		merged := store.CloneValues(previous)
		for name, value := range object {
			merged[name] = value
		}
		fields := []schema.Field(nil)
		if nested != nil {
			fields = nested.Fields
		} else if blocks != nil {
			blockType, _ := object["blockType"].StringValue()
			for _, block := range blocks.Types {
				if block.Key == blockType {
					fields = block.Fields
					break
				}
			}
		}
		for _, field := range fields {
			value, exists := object[field.Name]
			if exists {
				merged[field.Name] = mergeValue(field, previous[field.Name], value)
			}
		}
		nextRows[index] = store.Object(merged)
	}
	return store.List(nextRows...)
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
