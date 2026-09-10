package localization

import (
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// ProjectDocumentChecked rejects malformed or unbounded embedded envelopes
// before projecting any portion of a document.
func ProjectDocumentChecked(document store.Document, fields []schema.Field, selection Selection) (store.Document, error) {
	if err := embedded.ValidateValues(fields, document.Values, "", true, nil); err != nil {
		return store.Document{}, err
	}
	return ProjectDocument(document, fields, selection), nil
}

func MergeStoragePatchChecked(fields []schema.Field, current, patch store.Values) (store.Values, error) {
	if err := validateMerge(fields, current, patch); err != nil {
		return nil, err
	}
	return MergeStoragePatch(fields, current, patch), nil
}
func MergeStorageUpdateChecked(fields []schema.Field, current, patch store.Values) (store.Values, error) {
	if err := validateMerge(fields, current, patch); err != nil {
		return nil, err
	}
	return MergeStorageUpdate(fields, current, patch), nil
}
func validateMerge(fields []schema.Field, current, patch store.Values) error {
	budget := embedded.NewBudget()
	if err := embedded.ValidateValues(fields, current, "", true, budget); err != nil {
		return err
	}
	return embedded.ValidateValues(fields, patch, "", true, budget)
}
func LocalizedValuesChecked(fields []schema.Field, values store.Values) (store.Values, error) {
	if err := embedded.ValidateValues(fields, values, "", false, nil); err != nil {
		return nil, err
	}
	return LocalizedValues(fields, values), nil
}
