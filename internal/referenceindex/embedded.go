package referenceindex

import (
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Collect validates the complete declared envelope before deriving any index
// entries. A malformed or over-budget tree must never yield a partial index.
func Collect(collection schema.Collection, document store.Document) ([]Entry, error) {
	if err := embedded.ValidateValues(collection.Fields, document.Values, "", true, nil); err != nil {
		return nil, err
	}
	return collectUnchecked(collection, document), nil
}
func CollectField(owner store.DocumentReference, field schema.Field, value store.Value, locale schema.LocaleCode) ([]Entry, error) {
	unwrapped := field
	if locale != "" {
		unwrapped.Localized = false
	}
	if err := embedded.ValidateValue(unwrapped, value, field.Name, true, nil); err != nil {
		return nil, err
	}
	return collectFieldUnchecked(owner, field, value, locale), nil
}
func NullifyTarget(collection schema.Collection, values store.Values, target store.DocumentReference) (store.Values, bool, error) {
	if err := embedded.ValidateValues(collection.Fields, values, "", true, nil); err != nil {
		return nil, false, err
	}
	updated, changed := nullifyTargetUnchecked(collection, values, target)
	return updated, changed, nil
}
func NullifyField(field schema.Field, value store.Value, target store.DocumentReference, locale schema.LocaleCode) (store.Value, bool, error) {
	unwrapped := field
	if locale != "" {
		unwrapped.Localized = false
	}
	if err := embedded.ValidateValue(unwrapped, value, field.Name, true, nil); err != nil {
		return store.Value{}, false, err
	}
	updated, changed := nullifyFieldUnchecked(field, value, target, locale)
	return updated, changed, nil
}
func RemoveResourceTargets(collection schema.Collection, values store.Values, resources []schema.StableID) (store.Values, bool, error) {
	if err := embedded.ValidateValues(collection.Fields, values, "", true, nil); err != nil {
		return nil, false, err
	}
	updated, changed := removeResourceTargetsUnchecked(collection, values, resources)
	return updated, changed, nil
}
