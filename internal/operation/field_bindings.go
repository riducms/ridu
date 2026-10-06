package operation

import (
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// FieldBinding is private runtime configuration, lowered once from one field
// of a resource or of a block definition. A definition's binding applies at
// every placement of the definition: a document walk supplies each location's
// placement, so bindings follow definitions, not placements. No request
// contains an authoring node or resolves a callback from a document path.
// Field paths are only schema navigation and diagnostics.
type FieldBinding struct {
	ID string
	// Block names the block definition that declares Field, whose path and ID
	// are then definition-relative. It is empty for a resource's own field.
	Block          string
	Field          schema.Field
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

// scopedBindingContext describes one field callback. Its Root, Siblings and
// Prior are immutable views, so no callback can change another callback's view,
// the operation's working values or a retained snapshot. views shares views
// across a dispatch pass and may be nil.
func scopedBindingContext(ctx Context, binding FieldBinding, location fieldLocation, previous map[string]fieldLocation, views *callbackViews) Context {
	if views == nil {
		views = &callbackViews{}
	}
	ctx.bound, ctx.RuntimePath = location.bound(), location.runtimePath
	ctx.Value, ctx.ValuePresent = location.value, !location.value.IsZero()
	// Leaf hooks refresh their own cached value without copying the immutable
	// sibling snapshot, so the view applies it. Exact-locale locations are
	// reprojected after changes and keep that projection authoritative: a
	// cleared structured sibling may intentionally be absent.
	own := ownMember{}
	if !ctx.AllLocales || location.locale == "" {
		own = ownMember{name: location.field.Name, value: location.value, present: ctx.ValuePresent, apply: true}
	}
	ctx.Siblings = views.resolve(location.fields, location.siblings, false, own)
	ctx.Prior = store.Value{}
	if old, ok := previous[location.identity]; ok {
		ctx.Prior = views.resolve(location.fields, old.siblings, false, ownMember{})
	}
	ctx.Input, ctx.Replacement = store.Value{}, nil
	if location.locale != "" {
		ctx.Locale = location.locale
	}
	locale := schema.LocaleCode("")
	if location.localeOwned {
		locale = ctx.Locale
	}
	ctx.SchemaOccurrenceID = views.schemaOccurrence(binding, location)
	// Length-delimited components cannot collide even when repeated keys contain
	// diagnostic separators. Location identity already uses length-prefixed keys.
	ctx.OccurrenceID = ctx.SchemaOccurrenceID + "/" + strconv.Itoa(len(location.bindingIdentity)) + ":" + location.bindingIdentity + "/" + strconv.Itoa(len(locale)) + ":" + string(locale)
	if !ctx.enclosingRoot.IsZero() {
		ctx.Root = ctx.enclosingRoot
	} else if ctx.AllLocales && location.locale != "" {
		selection := localization.Selection{Locale: location.locale, Chain: []schema.LocaleCode{location.locale}, Configured: ctx.Locales, PreserveNull: true}
		projected := projectValues(ctx.projections, ctx.Collection.Fields, boundValues(ctx), selection)
		ctx.Root = callbackObject(ctx.Collection.Fields, store.Object(projected), false, ownMember{})
	} else {
		if views.root.IsZero() {
			views.root = store.Object(boundValues(ctx))
		}
		ctx.Root = views.resolve(ctx.Collection.Fields, views.root, ctx.AllLocales, ownMember{})
	}
	// Callbacks read the views above, never the operation's working map.
	ctx.Data = nil
	// The bounded callback describes the view it actually receives, not merely
	// the incoming request. Translated occurrences above have an exact view.
	if location.locale != "" {
		ctx.AllLocales = false
	}
	return ctx
}

// schemaOccurrence names the configured field at the location's placement. A
// resource's own field has one placement; a block definition's binding names
// each placement by its canonical path. A dispatch visits a placement's
// locations together, so the last name is reused.
func (views *callbackViews) schemaOccurrence(binding FieldBinding, location fieldLocation) string {
	if binding.Block == "" {
		return binding.ID
	}
	last := &views.occurrence
	if last.id == "" || last.binding != binding.ID || last.field != location.field || last.base != location.base {
		canonical := location.canonical()
		*last = schemaOccurrence{binding: binding.ID, field: location.field, base: location.base, id: binding.ID + "@" + strconv.Itoa(len(canonical)) + ":" + canonical}
	}
	return last.id
}

// callbackViews shares the immutable views field callbacks receive across one
// dispatch pass. Views are keyed by the immutable values they present, so a
// changed candidate misses the cache instead of exposing a stale view.
type callbackViews struct {
	// root is an immutable snapshot of the pass's bound values. When zero, the
	// first callback snapshots them.
	root store.Value
	// recent holds the latest views, most recent first: typically the root,
	// an enclosing object and a prior object. A root field's siblings are the
	// root itself and share its view.
	recent [3]callbackView
	// occurrence is the last schema occurrence named; see schemaOccurrence.
	occurrence schemaOccurrence
}

type schemaOccurrence struct {
	binding string
	field   *schema.Field
	base    string
	id      string
}

type callbackView struct {
	origin        store.Value
	fields        []schema.Field
	skipLocalized bool
	view          store.Value
}

// ownMember is the bound field's current value, applied to its sibling view.
type ownMember struct {
	name    string
	value   store.Value
	present bool
	apply   bool
}

// resolve returns the callback view of origin, applying own when it differs
// from origin's member. Views of unchanged origins are shared.
func (views *callbackViews) resolve(fields []schema.Field, origin store.Value, skipLocalized bool, own ownMember) store.Value {
	if own.apply {
		current, exists := origin.Lookup(own.name)
		if exists != own.present || own.present && !current.SameBacking(own.value) {
			return callbackObject(fields, origin, skipLocalized, own)
		}
	}
	for index, cached := range views.recent {
		if !cached.view.IsZero() && cached.skipLocalized == skipLocalized && sameSlice(cached.fields, fields) && cached.origin.SameBacking(origin) {
			copy(views.recent[1:index+1], views.recent[:index])
			views.recent[0] = cached
			return cached.view
		}
	}
	view := callbackObject(fields, origin, skipLocalized, ownMember{})
	copy(views.recent[1:], views.recent[:len(views.recent)-1])
	views.recent[0] = callbackView{origin: origin, fields: fields, skipLocalized: skipLocalized, view: view}
	return view
}

// callbackObject is the object a field callback reads. Ordinary scalar
// selectors expose the portable empty state inside an existing object. Raw
// carriers retain submission membership, and plugin/JSON payloads retain their
// codec-owned sparse structure. An unchanged complete object is shared.
func callbackObject(fields []schema.Field, origin store.Value, skipLocalized bool, own ownMember) store.Value {
	if own.apply && !own.present {
		if _, exists := origin.Lookup(own.name); exists {
			// Removing a member needs a mutable copy.
			object, _ := origin.CopyObject()
			delete(object, own.name)
			return callbackObject(fields, store.Object(object), skipLocalized, ownMember{})
		}
	}
	setsOwn := own.apply && own.present
	missing := 0
	for _, field := range fields {
		if callbackScalar(field, skipLocalized) && !(setsOwn && field.Name == own.name) {
			if _, exists := origin.Lookup(field.Name); !exists {
				missing++
			}
		}
	}
	if missing == 0 && !setsOwn && origin.Kind() == store.ValueObject {
		return origin
	}
	additions := make(store.Values, missing+1)
	if setsOwn {
		additions[own.name] = own.value
	}
	for _, field := range fields {
		if callbackScalar(field, skipLocalized) && !(setsOwn && field.Name == own.name) {
			if _, exists := origin.Lookup(field.Name); !exists {
				additions[field.Name] = store.Null()
			}
		}
	}
	if updated, isObject := origin.WithMembers(additions); isObject {
		return updated
	}
	return store.Object(additions)
}

func callbackScalar(field schema.Field, skipLocalized bool) bool {
	if skipLocalized && field.Localized {
		return false
	}
	switch field.Type {
	case schema.FieldTypeGroup, schema.FieldTypeArray, schema.FieldTypeBlocks,
		schema.FieldTypePlugin, schema.FieldTypeJSON, schema.FieldTypeUI,
		schema.FieldTypeJoin, schema.FieldTypeVirtual:
		return false
	}
	return true
}

// actorView lazily snapshots one operation's actor values. The engine never
// changes an operation's actor, so every callback can share the snapshot.
type actorView struct {
	actor  *store.Document
	once   sync.Once
	values store.Value
}

func newActorView(actor *store.Document) *actorView {
	if actor == nil {
		return nil
	}
	return &actorView{actor: actor}
}

// ActorValues returns an immutable view of the actor document's values, or the
// zero Value without an actor.
func (ctx Context) ActorValues() store.Value {
	if ctx.Actor == nil {
		return store.Value{}
	}
	if view := ctx.actorView; view != nil && view.actor == ctx.Actor {
		view.once.Do(func() { view.values = store.Object(view.actor.Values) })
		return view.values
	}
	return store.Object(ctx.Actor.Values)
}

// FieldReplacement records a field hook's new value for its own field.
type FieldReplacement struct {
	Value store.Value
	Set   bool
}

// ReplaceValue sets a field hook's own value. Only field hooks the engine
// dispatches receive a replacement slot. Elsewhere, such as in a validator,
// access rule, default or deferred hook, the value could not be applied, so
// asking to replace it is an error rather than a silently lost change.
func (ctx Context) ReplaceValue(value store.Value) error {
	if ctx.Replacement == nil {
		return &Error{Code: "invalid_field_replacement", Status: 500, Message: "field " + strconv.Quote(ctx.RuntimePath) + " cannot replace its value in this callback; only field hooks may"}
	}
	ctx.Replacement.Value, ctx.Replacement.Set = value, true
	return nil
}

// runFieldHooks dispatches each retained occurrence at this lifecycle
// checkpoint, placement by placement in schema order (see dispatchOrder).
func runFieldHooks(collection Collection, ctx Context, selectHooks func(Hooks) []Hook, flags ...bool) error {
	prepare := len(flags) > 0 && flags[0]
	typedWrite := len(flags) > 1 && flags[1]
	budget := embedded.NewWorkBudget()
	hooked := func(index int) bool { return len(selectHooks(collection.Bindings[index].Hooks)) != 0 }
	if !anyBinding(collection, hooked) {
		return nil
	}
	var priors []map[string]fieldLocation
	// boundRoot snapshots boundValues(ctx) for location reads and callback
	// views until a callback result or identity preparation writes that map.
	boundRoot := lazyObject{values: boundValues(ctx)}
	var views callbackViews
	orders := newPlacementOrders(collection)
	// The candidate is derived and admitted once, and one walk of it finds
	// every hooked binding's locations; both happen again only after a
	// callback changed the candidate, rather than once for every placement.
	var values store.Values
	var candidateContext Context
	var valuesRoot *lazyObject
	// candidate holds every hooked binding's locations in the candidate, and
	// pending the placements not yet dispatched, in dispatch order;
	// dispatched is the last placement dispatched.
	var candidate [][]fieldLocation
	var pending []placementGroup
	var dispatched *placementGroup
	// prepared records that row identities were prepared in the current
	// candidate. Preparation that adds no key leaves the candidate and its
	// locations valid, so only a change re-derives and re-admits it.
	prepared := false
	// follows reports whether a hooked binding has a placement after the last
	// one dispatched, whose locations a callback may have changed or created.
	follows := func() bool {
		key := orders.key(*dispatched)
		for index := range collection.Bindings {
			if last := orders.plan.bindingLast[index]; hooked(index) && last != nil && compareOrder(last, key) > 0 {
				return true
			}
		}
		return false
	}
	stale := true
	for {
		if stale {
			if dispatched != nil && !follows() {
				return nil
			}
			values = boundValues(ctx)
			candidateContext = ctx
			valuesRoot = &boundRoot
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
				valuesRoot = &lazyObject{values: values}
			}
			if err := embedded.ValidateValues(collection.Schema.Fields, values, "", ctx.AllLocales, budget); err != nil {
				return embeddedOperationError(err, false)
			}
			stale, prepared = false, false
			// Placements up to the last one dispatched have run; a placement
			// whose locations a callback created there is not revisited.
			candidate = bindingLocations(collection, valuesRoot.value(), ctx.AllLocales, true, hooked)
			pending = dispatchOrder(orders, candidate)
			if dispatched != nil {
				for len(pending) != 0 && orders.compare(pending[0], *dispatched) <= 0 {
					pending = pending[1:]
				}
			}
		}
		if len(pending) == 0 {
			return nil
		}
		group := pending[0]
		pending, dispatched = pending[1:], &group
		index, placement := group.binding, group.canonical
		initial := group.locations(candidate)
		binding := collection.Bindings[index]
		hooks := selectHooks(binding.Hooks)
		locations := indexFieldHookLocations(initial)
		if priors == nil {
			// Prior is fixed for the dispatch; current values still refresh after
			// every hook and are indexed separately for each placement.
			priors = bindingPriors(collection, originalFieldRoot(ctx), ctx.AllLocales, hooked)
		}
		previous := priors[index]
		containsRows := fieldContainsRowIdentities(binding.Field)
		prepareWrites := prepare && changesDocument(ctx.Operation)
		// Snapshot identities at this owning field's phase entry. Each callback sees
		// the current candidate; deleted identities cannot receive stale writeback.
		for _, entry := range initial {
			for _, hook := range hooks {
				current, found := locations[entry.identity]
				if !found {
					break
				}
				views.root = valuesRoot.value()
				scoped := scopedBindingContext(candidateContext, binding, current, previous, &views)
				replacement := &FieldReplacement{}
				scoped.Replacement = replacement
				if err := budget.Enter(current.runtimePath); err != nil {
					return embeddedOperationError(err, false)
				}
				err := runHooks([]Hook{hook}, scoped)
				budget.Leave()
				if err != nil {
					return err
				}
				// The adapter can replace only its own value. Neither sibling views nor
				// the callback's immutable root view grant an ambient document mutation.
				before, existed := scoped.Siblings.Lookup(binding.Field.Name)
				value := replacement.Value
				changed := replacement.Set && (!existed || !reflect.DeepEqual(before, value))
				if changed {
					patch := &runtimePatch{}
					patch.add(current.runtimePath, value, false)
					patch.apply(values)
				}
				// Preserve preparation after the first callback, including raw phases
				// whose input may not have keys yet. Thereafter only a replacement
				// that can contain rows can introduce identity or envelope work.
				keyed := false
				if prepareWrites && (!prepared || (changed && containsRows)) {
					var err error
					if keyed, err = prepareRowIdentities(collection.Schema.Fields, values, ctx.AllLocales, false); err != nil {
						return err
					}
					prepared = true
				}
				if changed && typedWrite && changesDocument(ctx.Operation) && ctx.originalCanonical != nil {
					// Publish the changed root only after identity preparation;
					// later placements and persistence must observe the same new keys.
					root, _, _ := strings.Cut(current.runtimePath, ".")
					ctx.Data[root] = values[root]
				}
				if changed || keyed {
					valuesRoot.reset()
					boundRoot.reset()
				}
				projectedSiblings := ctx.AllLocales && current.locale != ""
				// A scalar replacement changes only its own location, which the
				// patch above already wrote, so the admitted candidate and every
				// later placement's locations stay valid. New keys, a replacement
				// with descendant fields or a reprojected locale view re-derive and
				// re-admit the candidate before the next placement; doing so after
				// every change made a write quadratic in its hooked placements.
				structural := keyed || (changed && (containsRows || binding.Field.Nested != nil || projectedSiblings))
				if structural {
					stale = true
				}
				if keyed || (changed && (containsRows || projectedSiblings)) {
					// Structural transforms and key preparation can invalidate paths
					// or identities. Reindex the current candidate, retaining only the
					// phase-entry dispatch order above, even for read transforms.
					// Exact-locale siblings must also be reprojected: a cleared JSON
					// value, for example, disappears from that view instead of being
					// exposed as a present null. Keep this projection authoritative.
					locations = indexFieldHookLocations(placementLocations(collection, valuesRoot.value(), ctx.AllLocales, true, placement))
				} else if changed {
					// A bounded field callback cannot write its parent, siblings or
					// enclosing identity. A leaf replacement therefore changes only
					// this cached location. Root still comes from the eagerly patched
					// candidate at the next callback; earlier snapshots stay immutable.
					current.value = value
					locations[entry.identity] = current
				}
			}
		}
	}
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
	validated := func(index int) bool { return len(collection.Bindings[index].Validators) != 0 }
	if !anyBinding(collection, validated) {
		return nil
	}
	// Validators only read the candidate, so every location read shares one
	// snapshot, and one walk finds every validated binding's locations.
	valuesRoot := lazyObject{values: values}
	current := bindingLocations(collection, valuesRoot.value(), ctx.AllLocales, true, validated)
	priors := bindingPriors(collection, originalFieldRoot(ctx), ctx.AllLocales, validated)
	views := callbackViews{}
	err := eachDispatched(dispatchOrder(newPlacementOrders(collection), current), current, func(index int, location fieldLocation) error {
		binding := collection.Bindings[index]
		views.root = valuesRoot.value()
		scoped := scopedBindingContext(ctx, binding, location, priors[index], &views)
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
		return nil
	})
	if err != nil {
		return err
	}
	if len(issues.values) != 0 {
		targets := fieldIssueTargets(collection, values, ctx.AllLocales)
		for i := range issues.values {
			issues.values[i].Target = targets[issues.values[i].Path]
		}
		return &Error{Code: "validation", Status: 422, Message: "document custom validation failed", Issues: issues.values}
	}
	return nil
}

func queueBoundAfterCommit(state *transactionState, collection Collection, ctx Context) {
	committed := func(index int) bool { return len(collection.Bindings[index].Hooks.AfterCommit) != 0 }
	if !anyBinding(collection, committed) {
		return
	}
	valuesRoot := lazyObject{values: boundValues(ctx)}
	current := bindingLocations(collection, valuesRoot.value(), ctx.AllLocales, true, committed)
	priors := bindingPriors(collection, originalFieldRoot(ctx), ctx.AllLocales, committed)
	views := callbackViews{}
	_ = eachDispatched(dispatchOrder(newPlacementOrders(collection), current), current, func(index int, location fieldLocation) error {
		binding := collection.Bindings[index]
		views.root = valuesRoot.value()
		scoped := scopedBindingContext(ctx, binding, location, priors[index], &views)
		// Deferred callbacks own these snapshots, not the operation's working
		// projection cache or any source values it happens to retain.
		scoped.projections = nil
		for _, hook := range binding.Hooks.AfterCommit {
			state.afterCommit = append(state.afterCommit, deferredHook{hook: hook, context: scoped})
		}
		return nil
	})
}
