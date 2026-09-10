package operation

import (
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Helpers outside Execute also serve independently scoped advisory operations.
// Their nil projector preserves the ordinary detached projection contract.
func projectValues(projections *localization.Projector, fields []schema.Field, values store.Values, selection localization.Selection) store.Values {
	if projections != nil {
		return projections.Values(values, selection)
	}
	return localization.ProjectDocument(store.Document{Values: values}, fields, selection).Values
}

func projectValuesChecked(projections *localization.Projector, fields []schema.Field, values store.Values, selection localization.Selection) (store.Values, error) {
	if projections != nil {
		return projections.ValuesChecked(values, selection)
	}
	document, err := localization.ProjectDocumentChecked(store.Document{Values: values}, fields, selection)
	return document.Values, err
}
