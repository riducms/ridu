package operation

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// FieldBinding is private runtime configuration, lowered once from one resolved
// graph placement. No request contains an authoring node or resolves a callback
// from a document path. Field paths are only schema navigation and diagnostics.
type FieldBinding struct {
	ID             string
	Field          schema.Field
	LocaleOwned    bool
	Hooks          Hooks
	Access         FieldRules
	Validators     []FieldValidator
	LiveValidators []FieldLiveValidator
	Computed       Computed
	Default        FieldDefault
}

type FieldValidator func(Context) ([]schema.Issue, error)

// FieldLiveValidator reports whether its typed input was available separately
// from an empty, successful set of advisory issues.
type FieldLiveValidator func(Context) ([]schema.Issue, bool, error)

// FieldDefault supplies one absent value during eligible initialization.
type FieldDefault func(Context) (store.Value, bool, error)

func boundValues(ctx Context) store.Values {
	if ctx.Document != nil {
		return ctx.Document.Values
	}
	if ctx.Data != nil {
		return ctx.Data
	}
	if ctx.Original != nil {
		return ctx.Original.Values
	}
	return nil
}

func scopedBindingContext(ctx Context, binding FieldBinding, location fieldLocation, previous map[string]fieldLocation) Context {
	values := boundValues(ctx)
	ctx.Data = store.CloneValues(values)
	ctx.FieldPath, ctx.RuntimePath = binding.Field.Path.String(), location.runtimePath
	ctx.Value, ctx.ValuePresent = location.value, location.value.Kind() != ""
	ctx.SiblingData, _ = location.siblings.CopyObject()
	// Leaf hooks refresh their own cached value without copying the immutable
	// sibling snapshot. Apply that value to this callback's detached map. Exact-
	// locale locations are reprojected after changes and must keep that projection
	// authoritative: a cleared structured sibling may intentionally be absent.
	if !ctx.AllLocales || location.locale == "" {
		if ctx.ValuePresent {
			ctx.SiblingData[binding.Field.Name] = location.value
		} else {
			delete(ctx.SiblingData, binding.Field.Name)
		}
	}
	ctx.OriginalValue, ctx.OriginalSiblingData = store.Value{}, nil
	if old, ok := previous[location.identity]; ok {
		ctx.OriginalValue = old.value
		ctx.OriginalSiblingData, _ = old.siblings.CopyObject()
	}
	if location.locale != "" {
		ctx.Locale = location.locale
	}
	locale := schema.LocaleCode("")
	if binding.LocaleOwned {
		locale = ctx.Locale
	}
	// Length-delimited components cannot collide even when repeated keys contain
	// diagnostic separators. Location identity already uses length-prefixed keys.
	ctx.OccurrenceID = fmt.Sprintf("%s/%d:%s/%d:%s", binding.ID, len(location.bindingIdentity), location.bindingIdentity, len(locale), locale)
	if ctx.AllLocales && location.locale != "" {
		selection := localization.Selection{Locale: location.locale, Chain: []schema.LocaleCode{location.locale}, Configured: ctx.Locales, PreserveNull: true}
		ctx.Data = projectValues(ctx.projections, ctx.Collection.Fields, values, selection)
	}
	ctx.Data = scalarCallbackValues(ctx.Collection.Fields, ctx.Data, ctx.AllLocales && location.locale == "")
	ctx.SiblingData = scalarCallbackValues(location.fields, ctx.SiblingData, false)
	if ctx.OriginalSiblingData != nil {
		ctx.OriginalSiblingData = scalarCallbackValues(location.fields, ctx.OriginalSiblingData, false)
	}
	// The bounded callback describes the view it actually receives, not merely
	// the incoming request. Translated occurrences above have an exact view.
	if location.locale != "" {
		ctx.AllLocales = false
	}
	return ctx
}

// Ordinary scalar selectors expose the portable empty state inside an existing
// object. Raw carriers retain submission membership, and plugin/JSON payloads
// retain their codec-owned sparse structure. New occurrences have no prior view.
func scalarCallbackValues(fields []schema.Field, values store.Values, allLocales bool) store.Values {
	result := store.CloneValues(values)
	if result == nil {
		result = store.Values{}
	}
	for _, f := range fields {
		if allLocales && f.Localized {
			continue
		}
		switch f.Type {
		case schema.FieldTypeGroup, schema.FieldTypeArray, schema.FieldTypeBlocks,
			schema.FieldTypePlugin, schema.FieldTypeJSON, schema.FieldTypeUI,
			schema.FieldTypeJoin, schema.FieldTypeVirtual:
			continue
		}
		if _, exists := result[f.Name]; !exists {
			result[f.Name] = store.Null()
		}
	}
	return result
}

// runFieldHooks dispatches each retained occurrence at this lifecycle checkpoint.
func runFieldHooks(collection Collection, ctx Context, selectHooks func(Hooks) []Hook, flags ...bool) error {
	prepare := len(flags) > 0 && flags[0]
	typedWrite := len(flags) > 1 && flags[1]
	budget := embedded.NewWorkBudget()
	var priorValues store.Values
	priorLoaded := false
	for _, binding := range collection.Bindings {
		hooks := selectHooks(binding.Hooks)
		if len(hooks) == 0 {
			continue
		}
		values := boundValues(ctx)
		candidateContext := ctx
		if typedWrite && changesDocument(ctx.Operation) && ctx.originalCanonical != nil {
			selection := localization.Selection{Locale: ctx.Locale, All: ctx.AllLocales, Configured: ctx.Locales, PreserveNull: true}
			if ctx.Locale != "" {
				selection.Chain = []schema.LocaleCode{ctx.Locale}
			}
			patch, err := localization.StoragePatch(collection.Schema.Fields, values, selection)
			if err != nil {
				return err
			}
			canonical := localization.MergeStoragePatch(collection.Schema.Fields, ctx.originalCanonical.Values, patch)
			values = projectValues(ctx.projections, collection.Schema.Fields, canonical, selection)
			candidateContext.Data, candidateContext.Document = values, nil
		}
		if err := embedded.ValidateValues(collection.Schema.Fields, values, "", ctx.AllLocales, budget); err != nil {
			return embeddedOperationError(err, false)
		}
		path := binding.Field.Path.String()
		initial := fieldLocationsAtPath(collection.Schema.Fields, values, path, ctx.AllLocales, true)
		locations := indexFieldHookLocations(initial)
		if !priorLoaded {
			// Prior is fixed for the dispatch; current values still refresh after
			// every hook and are indexed separately for each binding.
			priorValues, priorLoaded = originalFieldValues(ctx), true
		}
		previous := originalFieldLocations(ctx, path, priorValues)
		containsRows := fieldContainsRowIdentities(binding.Field)
		prepareWrites := prepare && changesDocument(ctx.Operation)
		prepared := false
		// Snapshot identities at this owning field's phase entry. Each callback sees
		// the current candidate; deleted identities cannot receive stale writeback.
		for _, entry := range initial {
			for _, hook := range hooks {
				current, found := locations[entry.identity]
				if !found {
					break
				}
				scoped := scopedBindingContext(candidateContext, binding, current, previous)
				if err := budget.Enter(current.runtimePath); err != nil {
					return embeddedOperationError(err, false)
				}
				before, existed := scoped.SiblingData[binding.Field.Name]
				err := runHooks([]Hook{hook}, scoped)
				budget.Leave()
				if err != nil {
					return err
				}
				// The adapter can replace only its own value. Neither sibling snapshots nor
				// the callback's immutable root view grant an ambient document mutation.
				value, present := scoped.SiblingData[binding.Field.Name]
				changed := present != existed || !reflect.DeepEqual(before, value)
				if changed {
					patch := &runtimePatch{}
					patch.add(current.runtimePath, value, !present)
					patch.apply(values)
				}
				// Preserve preparation after the first callback, including raw phases
				// whose input may not have keys yet. Thereafter only a replacement
				// that can contain rows can introduce identity or envelope work.
				prepareNow := prepareWrites && (!prepared || (changed && containsRows))
				if prepareNow {
					if err := prepareRowIdentities(collection.Schema.Fields, values, ctx.AllLocales, false); err != nil {
						return err
					}
					prepared = true
				}
				if changed && typedWrite && changesDocument(ctx.Operation) && ctx.originalCanonical != nil {
					// Publish the changed root only after identity preparation;
					// later bindings and persistence must observe the same new keys.
					root, _, _ := strings.Cut(current.runtimePath, ".")
					ctx.Data[root] = values[root]
				}
				projectedSiblings := ctx.AllLocales && current.locale != ""
				if prepareNow || (changed && (containsRows || projectedSiblings)) {
					// Structural transforms and key preparation can invalidate paths
					// or identities. Reindex the current candidate, retaining only the
					// phase-entry dispatch order above, even for read transforms.
					// Exact-locale siblings must also be reprojected: a cleared JSON
					// value, for example, disappears from that view instead of being
					// exposed as a present null. Keep this projection authoritative.
					locations = indexFieldHookLocations(fieldLocationsAtPath(collection.Schema.Fields, values, path, ctx.AllLocales, true))
				} else if changed {
					// A bounded field callback cannot write its parent, siblings or
					// enclosing identity. A leaf replacement therefore changes only
					// this cached location. Root still comes from the eagerly patched
					// candidate at the next callback; earlier snapshots stay immutable.
					current.value = value
					if !present {
						current.value = store.Value{}
					}
					locations[entry.identity] = current
				}
			}
		}
	}
	return nil
}

func indexFieldHookLocations(locations []fieldLocation) map[string]fieldLocation {
	result := make(map[string]fieldLocation, len(locations))
	for _, location := range locations {
		// Before preparation, missing embedded keys can share an identity. The
		// previous linear lookup selected the first occurrence in that case.
		if _, exists := result[location.identity]; !exists {
			result[location.identity] = location
		}
	}
	return result
}

func validateBoundFields(collection Collection, ctx Context) error {
	if len(collection.Bindings) == 0 {
		return nil
	}
	issues := &validationIssueCollector{}
	values := boundValues(ctx)
	budget := embedded.NewWorkBudget()
	if err := embedded.ValidateValues(collection.Schema.Fields, values, "", ctx.AllLocales, budget); err != nil {
		return embeddedOperationError(err, false)
	}
	var priorValues store.Values
	priorLoaded := false
	for _, binding := range collection.Bindings {
		if len(binding.Validators) == 0 {
			continue
		}
		path := binding.Field.Path.String()
		if !priorLoaded {
			priorValues, priorLoaded = originalFieldValues(ctx), true
		}
		previous := originalFieldLocations(ctx, path, priorValues)
		for _, location := range fieldLocationsAtPath(collection.Schema.Fields, values, path, ctx.AllLocales, true) {
			scoped := scopedBindingContext(ctx, binding, location, previous)
			for _, validator := range binding.Validators {
				if err := budget.Enter(location.runtimePath); err != nil {
					return embeddedOperationError(err, false)
				}
				found, err := validator(scoped)
				budget.Leave()
				if err != nil {
					return hookError("field validation", err)
				}
				issues.add(found...)
			}
		}
	}
	if len(issues.values) != 0 {
		targets := fieldIssueTargets(collection.Schema.Fields, values, ctx.AllLocales)
		for i := range issues.values {
			issues.values[i].Target = targets[issues.values[i].Path]
		}
		return &Error{Code: "validation", Status: 422, Message: "document custom validation failed", Issues: issues.values}
	}
	return nil
}

func queueBoundAfterCommit(state *transactionState, collection Collection, ctx Context) {
	var priorValues store.Values
	priorLoaded := false
	for _, binding := range collection.Bindings {
		if len(binding.Hooks.AfterCommit) == 0 {
			continue
		}
		path := binding.Field.Path.String()
		if !priorLoaded {
			priorValues, priorLoaded = originalFieldValues(ctx), true
		}
		previous := originalFieldLocations(ctx, path, priorValues)
		for _, location := range fieldLocationsAtPath(collection.Schema.Fields, boundValues(ctx), path, ctx.AllLocales, true) {
			scoped := scopedBindingContext(ctx, binding, location, previous)
			// Deferred callbacks own these snapshots, not the operation's working
			// projection cache or any source values it happens to retain.
			scoped.projections = nil
			for _, hook := range binding.Hooks.AfterCommit {
				state.afterCommit = append(state.afterCommit, deferredHook{hook: hook, context: scoped})
			}
		}
	}
}
