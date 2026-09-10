package operation

import (
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Validation must see the same retained children as storage. Otherwise defaults
// can overwrite omitted protected values, or required hidden fields can reject
// a valid sibling edit. Only submitted top-level fields remain in the patch.
func completeUpdateForValidation(fields []schema.Field, current, patch store.Values, selection localization.Selection, projections *localization.Projector) (store.Values, error) {
	selection = exactUpdateSelection(selection)
	storagePatch, err := localization.StoragePatch(fields, patch, selection)
	if err != nil {
		return nil, err
	}
	completed, err := localization.MergeStorageUpdateChecked(fields, current, storagePatch)
	if err != nil {
		return nil, err
	}
	projected, err := projectValuesChecked(projections, fields, completed, selection)
	if err != nil {
		return nil, err
	}
	// Preserve undeclared input for the validator, and explicit null/empty values
	// that locale projection can omit. Projection must not silently sanitize input.
	for name, value := range patch {
		if _, exists := projected[name]; !exists {
			projected[name] = value
		}
	}
	return projected, nil
}

func exactUpdateSelection(selection localization.Selection) localization.Selection {
	// Fallback values are read projections, not writes to the selected locale.
	if selection.Locale != "" && !selection.All {
		selection.Chain = []schema.LocaleCode{selection.Locale}
	}
	selection.PreserveNull = true
	return selection
}
