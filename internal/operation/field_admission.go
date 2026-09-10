package operation

import (
	"sort"

	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// authorizeBoundFields admits caller-owned writes using the immutable submission
// captured before normalization or hooks. The final candidate determines policy
// context, while submission membership determines whether admission is required.
// A trusted default or transform of an omitted field is not caller input.
func authorizeBoundFields(collection Collection, ctx Context, kind operation.Kind, before, after store.Values, authorizeEveryUpdateValue bool) error {
	if kind != operation.Create && kind != operation.Duplicate && kind != operation.Update {
		return nil
	}
	hasRules := false
	for _, binding := range collection.Bindings {
		if kind == operation.Update && binding.Access.Update != nil || kind != operation.Update && binding.Access.Create != nil {
			hasRules = true
			break
		}
	}
	if !hasRules {
		return nil
	}
	ctx.Data, ctx.Document = after, nil
	submitted, callerCandidate, err := fieldSubmissionValues(collection, ctx, before)
	if err != nil {
		return err
	}
	for _, binding := range collection.Bindings {
		rule := binding.Access.Create
		if kind == operation.Update {
			rule = binding.Access.Update
		}
		if rule == nil {
			continue
		}
		path := binding.Field.Path.String()
		current := fieldLocationsAtPath(collection.Schema.Fields, after, path, ctx.AllLocales)
		previous := fieldLocationsAtPath(collection.Schema.Fields, before, path, ctx.AllLocales)
		priorByIdentity := indexFieldLocations(previous)
		var requested []fieldLocation
		if kind == operation.Duplicate || kind == operation.Update && authorizeEveryUpdateValue {
			requested = append(requested, current...)
			requested = append(requested, removedFieldLocations(changedFieldLocations(previous, current), current)...)
		} else {
			requested = fieldLocationsAtPath(collection.Schema.Fields, submitted, path, ctx.AllLocales)
			if kind == operation.Update {
				// Removing a submitted container or a repeated member writes its
				// protected descendants even when no child property was supplied.
				// Compare the immutable caller patch, so hooks cannot erase intent.
				callerLocations := fieldLocationsAtPath(collection.Schema.Fields, callerCandidate, path, ctx.AllLocales)
				requested = append(requested, removedFieldLocations(changedFieldLocations(previous, callerLocations), callerLocations)...)
			}
		}
		currentByIdentity := indexFieldLocations(current)
		requestedByIdentity := indexFieldLocations(requested)
		identities := make([]string, 0, len(requestedByIdentity))
		for identity := range requestedByIdentity {
			identities = append(identities, identity)
		}
		sort.Strings(identities)
		for _, identity := range identities {
			location := requestedByIdentity[identity]
			if final, exists := currentByIdentity[identity]; exists {
				location = final
			} else {
				// An erased input still requires permission. It has no final
				// value; preserve only its identity and diagnostic location.
				location.value = store.Null()
			}
			fieldContext := scopedBindingContext(ctx, binding, location, priorByIdentity)
			allowed, accessError := rule(fieldContext)
			if accessError != nil {
				if failure, ok := accessError.(*Error); ok && failure.Code == "block_recovery_required" {
					return failure
				}
				return &Error{Code: "access_failed", Status: 500, Message: "field access rule failed", Cause: accessError}
			}
			if !allowed {
				return &Error{Code: "field_access_denied", Status: 403, Message: "field write is not permitted", Issues: []schema.Issue{{Code: "access_denied", Path: location.runtimePath, Message: "field may not be changed"}}}
			}
		}
	}
	return nil
}

// fieldSubmissionValues projects the original submission and its hypothetical
// patch result into the admission checkpoint's exact locale shape. The canonical
// merge preserves omitted children and matches repeated membership by stable key.
// Fallback output is never used to manufacture submitted target-locale values.
func fieldSubmissionValues(collection Collection, ctx Context, before store.Values) (store.Values, store.Values, error) {
	selection := localization.Selection{
		Locale: ctx.submittedLocale, Chain: []schema.LocaleCode{ctx.submittedLocale},
		Configured: append([]schema.LocaleCode(nil), ctx.Locales...), PreserveNull: true,
	}
	submittedCanonical := store.CloneValues(ctx.submittedData)
	if !ctx.submittedAllLocales {
		var err error
		submittedCanonical, err = localization.StoragePatch(collection.Schema.Fields, submittedCanonical, selection)
		if err != nil {
			return nil, nil, embeddedOperationError(err, false)
		}
	}
	beforeCanonical := store.CloneValues(before)
	if ctx.originalCanonical != nil {
		beforeCanonical = store.CloneValues(ctx.originalCanonical.Values)
	} else if !ctx.AllLocales {
		var err error
		beforeCanonical, err = localization.StoragePatch(collection.Schema.Fields, beforeCanonical, selection)
		if err != nil {
			return nil, nil, embeddedOperationError(err, false)
		}
	}
	callerCanonical := localization.MergeStoragePatch(collection.Schema.Fields, beforeCanonical, submittedCanonical)
	if ctx.AllLocales {
		return submittedCanonical, callerCanonical, nil
	}
	selection.Locale = ctx.Locale
	selection.Chain = []schema.LocaleCode{ctx.Locale}
	submitted := projectValues(ctx.projections, collection.Schema.Fields, submittedCanonical, selection)
	caller := projectValues(ctx.projections, collection.Schema.Fields, callerCanonical, selection)
	return submitted, caller, nil
}

func indexFieldLocations(locations []fieldLocation) map[string]fieldLocation {
	result := make(map[string]fieldLocation, len(locations))
	for _, location := range locations {
		result[location.identity] = location
	}
	return result
}

func redactBoundFields(collection Collection, ctx Context, document *store.Document) error {
	ctx.Document = document
	ctx.Data = document.Values
	for _, binding := range collection.Bindings {
		if binding.Access.Read == nil {
			continue
		}
		for _, location := range fieldLocationsAtPath(collection.Schema.Fields, document.Values, binding.Field.Path.String(), ctx.AllLocales) {
			fieldContext := scopedBindingContext(ctx, binding, location, nil)
			allowed, err := binding.Access.Read(fieldContext)
			if err != nil {
				return err
			}
			if !allowed {
				deleteValueAtRuntimePath(document.Values, location.runtimePath)
				deleteLocalizationSources(document.LocalizationSources, location.runtimePath)
			}
		}
	}
	return nil
}
