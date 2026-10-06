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
	if ctx.System || kind != operation.Create && kind != operation.Duplicate && kind != operation.Update {
		return nil
	}
	ruled := func(index int) bool {
		if kind == operation.Update {
			return collection.Bindings[index].Access.Update != nil
		}
		return collection.Bindings[index].Access.Create != nil
	}
	if !anyBinding(collection, ruled) {
		return nil
	}
	ctx.Data, ctx.Document = after, nil
	submitted, callerCandidate, err := fieldSubmissionValues(collection, ctx, before)
	if err != nil {
		return err
	}
	// Location reads share one immutable snapshot of each candidate; none of
	// these maps changes while rules run. One walk of each finds every ruled
	// binding's locations.
	afterRoot, beforeRoot := lazyObject{values: after}, lazyObject{values: before}
	submittedRoot, callerRoot := lazyObject{values: submitted}, lazyObject{values: callerCandidate}
	afterLocations := lazyBindingLocations{collection: collection, root: &afterRoot, allLocales: ctx.AllLocales, want: ruled}
	beforeLocations := lazyBindingLocations{collection: collection, root: &beforeRoot, allLocales: ctx.AllLocales, want: ruled}
	submittedLocations := lazyBindingLocations{collection: collection, root: &submittedRoot, allLocales: ctx.AllLocales, want: ruled}
	callerLocations := lazyBindingLocations{collection: collection, root: &callerRoot, allLocales: ctx.AllLocales, want: ruled}
	plan := collection.runtimePlan()
	every := kind == operation.Duplicate || kind == operation.Update && authorizeEveryUpdateValue
	// Requested locations are gathered for every ruled binding, then checked
	// placement by placement in schema order, each placement's in identity order.
	requests := make([][]fieldLocation, len(collection.Bindings))
	priors := make([]map[string]fieldLocation, len(collection.Bindings))
	for index, binding := range collection.Bindings {
		roots := plan.bindingRoots[index]
		if !ruled(index) || !afterRoot.hasAny(roots) && !beforeRoot.hasAny(roots) && !submittedRoot.hasAny(roots) && !callerRoot.hasAny(roots) {
			continue
		}
		if plan.bindingRootFields[index] && !(ctx.AllLocales && binding.Field.Localized) {
			// A root field's only location is its present member, so membership
			// decides whether the request below would contain an occurrence.
			root := roots[0]
			if every && !afterRoot.has(root) && !beforeRoot.has(root) ||
				!every && !submittedRoot.has(root) && !(kind == operation.Update && beforeRoot.has(root) && !callerRoot.has(root)) {
				continue
			}
		}
		current := afterLocations.get(index)
		previous := beforeLocations.get(index)
		priors[index] = indexFieldLocations(previous)
		var requested []fieldLocation
		if every {
			requested = append(requested, current...)
			requested = append(requested, removedFieldLocations(changedFieldLocations(previous, current), current)...)
		} else {
			requested = append(requested, submittedLocations.get(index)...)
			if kind == operation.Update {
				// Removing a submitted container or a repeated member writes its
				// protected descendants even when no child property was supplied.
				// Compare the immutable caller patch, so hooks cannot erase intent.
				caller := callerLocations.get(index)
				requested = append(requested, removedFieldLocations(changedFieldLocations(previous, caller), caller)...)
			}
		}
		currentByIdentity := indexFieldLocations(current)
		requestedByIdentity := indexFieldLocations(requested)
		identities := make([]string, 0, len(requestedByIdentity))
		for identity := range requestedByIdentity {
			identities = append(identities, identity)
		}
		sort.Strings(identities)
		// requested is this binding's own copy, so its deduplicated checks
		// reuse it; the index above holds the values being reordered.
		checks := requested[:0]
		for _, identity := range identities {
			location := requestedByIdentity[identity]
			if final, exists := currentByIdentity[identity]; exists {
				location = final
			} else {
				// An erased input still requires permission. It has no final
				// value; preserve only its identity and diagnostic location.
				location.value = store.Null()
			}
			checks = append(checks, location)
		}
		requests[index] = checks
	}
	views := callbackViews{}
	return eachDispatched(dispatchOrder(newPlacementOrders(collection), requests), requests, func(index int, location fieldLocation) error {
		binding := collection.Bindings[index]
		rule := binding.Access.Create
		if kind == operation.Update {
			rule = binding.Access.Update
		}
		views.root = afterRoot.value()
		fieldContext := scopedBindingContext(ctx, binding, location, priors[index], &views)
		allowed, accessError := rule(fieldContext)
		if accessError != nil {
			if failure, ok := accessError.(*Error); ok && failure.Code == "block_recovery_required" {
				return failure
			}
			return accessRuleError("field access rule failed", accessError)
		}
		if !allowed {
			return &Error{Code: "field_access_denied", Status: 403, Message: "field write is not permitted", Issues: []schema.Issue{{Code: "access_denied", Path: location.runtimePath, Message: "field may not be changed"}}}
		}
		return nil
	})
}

// authorizeDiscardFields checks only values that a reset would actually
// change. A saved live head may contain protected fields; their unchanged
// presence must not prevent discarding unrelated working edits.
func authorizeDiscardFields(collection Collection, ctx Context, before, after store.Values) error {
	if ctx.System {
		return nil
	}
	// Rules receive immutable views, so the candidate map itself is shared.
	ctx.Operation, ctx.Data, ctx.Document, ctx.AllLocales = operation.Update, after, nil, true
	beforeRoot, afterRoot := lazyObject{values: before}, lazyObject{values: after}
	ruled := func(index int) bool { return collection.Bindings[index].Access.Update != nil }
	beforeLocations := lazyBindingLocations{collection: collection, root: &beforeRoot, allLocales: true, want: ruled}
	afterLocations := lazyBindingLocations{collection: collection, root: &afterRoot, allLocales: true, want: ruled}
	changes := make([][]fieldLocation, len(collection.Bindings))
	priors := make([]map[string]fieldLocation, len(collection.Bindings))
	for index := range collection.Bindings {
		if !ruled(index) {
			continue
		}
		previous := beforeLocations.get(index)
		priors[index] = indexFieldHookLocations(previous)
		changes[index] = changedFieldLocations(previous, afterLocations.get(index))
	}
	views := callbackViews{}
	return eachDispatched(dispatchOrder(newPlacementOrders(collection), changes), changes, func(index int, location fieldLocation) error {
		binding := collection.Bindings[index]
		views.root = afterRoot.value()
		fieldContext := scopedBindingContext(ctx, binding, location, priors[index], &views)
		allowed, err := binding.Access.Update(fieldContext)
		if err != nil {
			return accessRuleError("field update access rule failed", err)
		}
		if !allowed {
			return &Error{Code: "field_access_denied", Status: 403, Message: "discard would change a protected field", Issues: []schema.Issue{{Code: "access_denied", Path: location.runtimePath, Message: "field may not be changed"}}}
		}
		return nil
	})
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
	// Admission only reads these maps; StoragePatch and MergeStoragePatch return
	// new maps without changing their inputs.
	submittedCanonical := ctx.submittedData
	if !ctx.submittedAllLocales {
		var err error
		submittedCanonical, err = localization.StoragePatch(collection.Schema.Fields, submittedCanonical, selection)
		if err != nil {
			return nil, nil, embeddedOperationError(err, false)
		}
	}
	beforeCanonical := before
	if ctx.originalCanonical != nil {
		beforeCanonical = ctx.originalCanonical.Values
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
	if ctx.System {
		return nil
	}
	ctx.Document = document
	ctx.Data = document.Values
	plan := collection.runtimePlan()
	ruled := func(index int) bool { return collection.Bindings[index].Access.Read != nil }
	present := false
	for index := range collection.Bindings {
		if ruled(index) {
			for _, root := range plan.bindingRoots[index] {
				_, found := document.Values[root]
				present = present || found
			}
		}
	}
	if !present {
		return nil
	}
	// One immutable snapshot serves every rule until a redaction changes the
	// document; later placements are then located again in the redacted
	// values, so their rules observe them, as before. Until then, one walk of
	// the snapshot finds every ruled binding's locations.
	views := callbackViews{root: store.Object(document.Values)}
	unredacted := bindingLocations(collection, views.root, ctx.AllLocales, false, ruled)
	redacted := false
	for _, group := range dispatchOrder(newPlacementOrders(collection), unredacted) {
		index := group.binding
		binding := collection.Bindings[index]
		locations := group.locations(unredacted)
		if redacted {
			if views.root.IsZero() {
				views.root = store.Object(document.Values)
			}
			locations = placementLocations(collection, views.root, ctx.AllLocales, false, group.canonical)
		}
		for _, location := range locations {
			fieldContext := scopedBindingContext(ctx, binding, location, nil, &views)
			allowed, err := binding.Access.Read(fieldContext)
			if err != nil {
				return err
			}
			if !allowed {
				deleteValueAtRuntimePath(document.Values, location.runtimePath)
				deleteLocalizationSources(document.LocalizationSources, location.runtimePath)
				views.root = store.Value{}
				redacted = true
			}
		}
	}
	return nil
}
