package operation

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// collectionPlan is schema-derived traversal metadata computed once when the
// engine is constructed. Request paths consult it instead of rediscovering the
// same facts from the field tree for every returned document.
type collectionPlan struct {
	// fields and bindings are the exact slices the plan was derived from.
	// Indexes below follow bindings, so the plan is used only for a collection
	// that still holds both slices; see runtimePlan.
	fields   []schema.Field
	bindings []FieldBinding
	// readOutput describes the fields whose read output is checked. Its block
	// plans are derived from definitions and shared by every placement.
	readOutput *outputPlan
	// bindingPaths[i] is a resource's own Bindings[i]'s canonical path beneath
	// the collection's fields. A block definition's binding has none: it is
	// located at every placement of its definition.
	bindingPaths []string
	// ownIndex finds a resource's own binding by its canonical path, and
	// definitionIndex finds a block definition's binding by its
	// definition-relative field ID.
	ownIndex        map[string]int
	definitionIndex map[schema.StableID]int
	// bindingTrie finds every binding's locations in one document walk.
	bindingTrie *bindingTrie
	// bindingRoots[i] names the root members whose values can hold
	// Bindings[i]'s locations; it is empty when none can.
	bindingRoots [][]string
	// bindingRootFields[i] reports that Bindings[i] is a root member itself.
	bindingRootFields []bool
	// bindingLast[i] is the placementOrder key of Bindings[i]'s last
	// placement, or nil when it has none: a resource's own binding has one.
	bindingLast [][]int
	// fieldAfterRead reports that a binding has an after-read hook.
	fieldAfterRead bool
}

// outputPlan is the read-output contract of one field list: a resource's root
// fields, or a group's, array row's or block definition's fields. Only fields
// that are checked, or that contain checked descendants, have entries, in
// field order. A nil plan has nothing to check.
type outputPlan struct {
	entries []outputEntry
	// unconditional reports a check that applies even without read hooks: a
	// primitive list or the auth identity, here or in a descendant plan.
	unconditional bool
	// lists reports a primitive list here or in a descendant plan.
	lists bool
}

type outputEntry struct {
	field schema.Field
	// check reports that the field's own value is checked: a required field,
	// the auth identity, or a primitive list's element types.
	check    bool
	identity bool
	list     bool
	// children describes a group's or array row's fields.
	children *outputPlan
	// variants describes block rows by their blockType. Shared definitions
	// have one plan however often they are placed.
	variants map[string]*outputPlan
	// payloads describes embedded payloads by tree key, case tag and slug.
	payloads map[outputPayload]*outputPlan
}

type outputPayload struct{ tree, tag, slug string }

// outputPlanner derives each distinct field list's plan once. Shared block
// definitions are one slice at every placement, so a schema's plans are
// proportional to its definitions.
type outputPlanner struct {
	plans map[*schema.Field]*outputPlan
}

func newOutputPlanner() *outputPlanner {
	return &outputPlanner{plans: make(map[*schema.Field]*outputPlan)}
}

func (planner *outputPlanner) plan(fields []schema.Field, identityField string) *outputPlan {
	if len(fields) == 0 {
		return nil
	}
	// The auth identity applies only to the resource's root list.
	memoize := identityField == ""
	if memoize {
		if plan, known := planner.plans[&fields[0]]; known {
			return plan
		}
	}
	var entries []outputEntry
	unconditional, lists := false, false
	include := func(plan *outputPlan) *outputPlan {
		unconditional = unconditional || plan != nil && plan.unconditional
		lists = lists || plan != nil && plan.lists
		return plan
	}
	for _, field := range fields {
		entry := outputEntry{field: field}
		entry.identity = identityField != "" && field.Name == identityField
		entry.list = primitivefield.IsList(field)
		entry.check = field.Required || entry.identity || entry.list
		unconditional = unconditional || entry.identity || entry.list
		lists = lists || entry.list
		if embedded.HasFields(field) {
			for _, tree := range field.Plugin.EmbeddedTrees {
				for _, c := range tree.Cases {
					for _, block := range c.Definitions() {
						if plan := include(planner.plan(block.ResolvedFields(), "")); plan != nil {
							if entry.payloads == nil {
								entry.payloads = make(map[outputPayload]*outputPlan)
							}
							entry.payloads[outputPayload{tree: tree.Key, tag: c.TagValue, slug: block.Slug}] = plan
						}
					}
				}
			}
		}
		switch field.Type {
		case schema.FieldTypeGroup, schema.FieldTypeArray:
			if field.Nested != nil {
				entry.children = include(planner.plan(field.Nested.ResolvedFields(), ""))
			}
		case schema.FieldTypeBlocks:
			if field.Blocks != nil {
				for _, block := range field.Blocks.Definitions() {
					if plan := include(planner.plan(block.ResolvedFields(), "")); plan != nil {
						if entry.variants == nil {
							entry.variants = make(map[string]*outputPlan)
						}
						entry.variants[block.Slug] = plan
					}
				}
			}
		}
		if entry.check || entry.children != nil || entry.variants != nil || entry.payloads != nil {
			entries = append(entries, entry)
		}
	}
	var plan *outputPlan
	if len(entries) != 0 {
		plan = &outputPlan{entries: entries, unconditional: unconditional, lists: lists}
	}
	if memoize {
		planner.plans[&fields[0]] = plan
	}
	return plan
}

func newCollectionPlan(collection Collection, planner *outputPlanner) *collectionPlan {
	if planner == nil {
		planner = newOutputPlanner()
	}
	plan := &collectionPlan{
		fields: collection.Schema.Fields, bindings: collection.Bindings,
		bindingPaths: make([]string, len(collection.Bindings)), bindingRootFields: make([]bool, len(collection.Bindings)),
		ownIndex: map[string]int{}, definitionIndex: map[schema.StableID]int{},
	}
	identityField := ""
	if collection.Schema.Auth != nil && len(collection.placement.canonical) == 0 {
		identityField = collection.Schema.Auth.IdentityField
	}
	plan.readOutput = planner.plan(collection.Schema.Fields, identityField)
	plan.bindingTrie, plan.bindingLast = newBindingTrie(collection)
	plan.bindingRoots = bindingRoots(plan.bindingTrie, len(collection.Bindings))
	narrowed := collection.placement.shared
	for index, binding := range collection.Bindings {
		plan.fieldAfterRead = plan.fieldAfterRead || len(binding.Hooks.AfterRead) != 0
		if binding.Block != "" {
			plan.definitionIndex[binding.Field.ID] = index
			continue
		}
		path := binding.Field.Path.String()
		plan.ownIndex[path] = index
		if !narrowed {
			// A collection narrowed to a block definition's fields locates
			// none of the resource's own bindings.
			plan.bindingPaths[index] = path
			plan.bindingRootFields[index] = slicesIndexField(collection.Schema.Fields, path) >= 0
		}
	}
	return plan
}

// binding finds the binding of a located field: a resource's own field by its
// canonical path, a block definition's field by its definition-relative ID.
func (plan *collectionPlan) binding(location fieldLocation) (int, bool) {
	if location.shared() {
		index, found := plan.definitionIndex[location.field.ID]
		return index, found
	}
	index, found := plan.ownIndex[location.canonical()]
	return index, found
}

// runtimePlan returns the precomputed plan when it was derived from this
// collection's own fields and bindings. A copy whose fields or bindings were
// replaced, or a collection assembled outside New, derives its plan on demand,
// so a plan can never describe another collection's bindings.
func (collection Collection) runtimePlan() *collectionPlan {
	if plan := collection.plan; plan != nil && sameSlice(plan.fields, collection.Schema.Fields) && sameSlice(plan.bindings, collection.Bindings) {
		return plan
	}
	return newCollectionPlan(collection, nil)
}

// narrowed returns the collection restricted to a block definition's fields,
// placed at placement, with a plan derived from them once instead of on every
// use. The definition's bindings locate its fields; the resource's own
// bindings have no location among them.
func (collection Collection) narrowed(fields []schema.Field, placement fieldPlacement) Collection {
	collection.Schema.Fields, collection.placement = fields, placement
	collection.plan = newCollectionPlan(collection, nil)
	return collection
}

// sameSlice reports whether left and right are the same slice: equal length
// over one backing array. Equal contents in different arrays do not count.
func sameSlice[T any](left, right []T) bool {
	return len(left) == len(right) && (len(left) == 0 || &left[0] == &right[0])
}

// lazyObject snapshots a root map once, on its first location read, for
// traversals that only read it. The map must not change while it is in use.
type lazyObject struct {
	values store.Values
	object store.Value
}

func (lazy *lazyObject) has(name string) bool {
	_, present := lazy.values[name]
	return present
}

// value returns the snapshot, taking it on first use.
func (lazy *lazyObject) value() store.Value {
	if lazy.object.IsZero() {
		lazy.object = store.Object(lazy.values)
	}
	return lazy.object
}

// reset discards the snapshot after its map changed.
func (lazy *lazyObject) reset() {
	lazy.object = store.Value{}
}

// hasAny reports whether the map holds one of names.
func (lazy *lazyObject) hasAny(names []string) bool {
	for _, name := range names {
		if lazy.has(name) {
			return true
		}
	}
	return false
}

// readHooksChangeValues reports whether a read hook can replace values
// between the store and the response.
func readHooksChangeValues(collection Collection) bool {
	return len(collection.Hooks.AfterRead) != 0 || collection.runtimePlan().fieldAfterRead
}

// outputLocation is one checked value's place in a document. Paths are built
// only for the rare value that a check reports or remembers.
type outputLocation struct {
	parent *outputLocation
	// runtime is a member name, row index or locale; full marks a complete
	// runtime path, as embedded payloads report.
	runtime  string
	full     bool
	identity string
}

func (location *outputLocation) child(runtime, identity string) *outputLocation {
	return &outputLocation{parent: location, runtime: runtime, identity: identity}
}

func (location *outputLocation) runtimePath() string {
	var segments []string
	for current := location; current != nil; current = current.parent {
		segments = append(segments, current.runtime)
		if current.full {
			break
		}
	}
	for left, right := 0, len(segments)-1; left < right; left, right = left+1, right-1 {
		segments[left], segments[right] = segments[right], segments[left]
	}
	return strings.Join(segments, ".")
}

// identityPath names the location by field names, stable row identities and
// locales, so it survives a read hook that reorders rows.
func (location *outputLocation) identityPath() string {
	var segments []string
	for current := location; current != nil; current = current.parent {
		segments = append(segments, current.identity)
	}
	for left, right := 0, len(segments)-1; left < right; left, right = left+1, right-1 {
		segments[left], segments[right] = segments[right], segments[left]
	}
	return strings.Join(segments, ".")
}

// walkOutput visits the value of every checked field present in values,
// following document values through the plan in document order.
func walkOutput(plan *outputPlan, values store.Value, allLocales bool, parent *outputLocation, visit func(outputEntry, store.Value, *outputLocation) error) error {
	if plan == nil {
		return nil
	}
	for _, entry := range plan.entries {
		value, present := values.Lookup(entry.field.Name)
		if !present {
			continue
		}
		location := parent.child(entry.field.Name, entry.field.Name)
		if allLocales && entry.field.Localized {
			if value.Kind() != store.ValueObject {
				continue
			}
			codes := make([]string, 0, value.Len())
			for code := range value.Entries() {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			for _, code := range codes {
				if err := walkOutputValue(entry, value.Get(code), allLocales, location.child(code, code), visit); err != nil {
					return err
				}
			}
			continue
		}
		if err := walkOutputValue(entry, value, allLocales, location, visit); err != nil {
			return err
		}
	}
	return nil
}

func walkOutputValue(entry outputEntry, value store.Value, allLocales bool, location *outputLocation, visit func(outputEntry, store.Value, *outputLocation) error) error {
	if entry.check {
		if err := visit(entry, value, location); err != nil {
			return err
		}
	}
	switch {
	case entry.payloads != nil:
		var failure error
		err := embedded.Visit(entry.field, value, location.runtimePath(), nil, func(occurrence embedded.ReadOccurrence) error {
			plan := entry.payloads[outputPayload{tree: occurrence.Tree.Key, tag: occurrence.Case.TagValue, slug: occurrence.Type.Slug}]
			if plan == nil || failure != nil {
				return nil
			}
			payload := &outputLocation{parent: location, runtime: occurrence.RuntimePath, full: true, identity: occurrence.Identity}
			failure = walkOutput(plan, occurrence.Payload, allLocales, payload, visit)
			return nil
		})
		if err != nil {
			return nil // Operation preflight rejects malformed envelopes.
		}
		return failure
	case entry.children != nil && entry.field.Type == schema.FieldTypeGroup:
		if value.Kind() == store.ValueObject {
			return walkOutput(entry.children, value, allLocales, location, visit)
		}
	case entry.children != nil && entry.field.Type == schema.FieldTypeArray:
		for row := range identifiedRowsInOrder(value, nil) {
			if err := walkOutput(entry.children, row.value, allLocales, location.child(strconv.Itoa(row.index), row.identity), visit); err != nil {
				return err
			}
		}
	case entry.variants != nil:
		for row := range identifiedRowsInOrder(value, entry.field.Blocks) {
			if plan := entry.variants[row.kind]; plan != nil {
				if err := walkOutput(plan, row.value, allLocales, location.child(strconv.Itoa(row.index), row.identity), visit); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// storedRequiredNulls returns the identities of required output locations
// that already hold null before read hooks run. Writes admit stored content,
// and a migration audits it when a field becomes required, so a stored null
// never fails a read; only a hook may not introduce one.
func storedRequiredNulls(collection Collection, values store.Values, allLocales bool) map[string]struct{} {
	var nulls map[string]struct{}
	_ = walkOutput(collection.runtimePlan().readOutput, store.Object(values), allLocales, nil, func(entry outputEntry, value store.Value, location *outputLocation) error {
		if !entry.identity && entry.field.Required && value.Kind() == store.ValueNull {
			if nulls == nil {
				nulls = make(map[string]struct{})
			}
			nulls[location.identityPath()] = struct{}{}
		}
		return nil
	})
	return nulls
}

// validateReadOutput enforces the read-output contract after read hooks.
// Selection/redaction may omit a property, but a read hook cannot return
// explicit null for a required output field. A null the store returned is not
// a hook result: hooked reports that read hooks ran, and storedNulls lists
// the required locations that were null before them, so a document stored
// before its field became required stays readable. Working drafts may carry
// incomplete editorial values; an auth identity remains non-nullable.
// Primitive lists also retain their declared element type and finite numbers.
// Authoring length/range rules remain write validation, not output formatting
// rules. Only fields beneath members present in values can produce a location,
// so selected or redacted output is checked without visiting omitted members.
func validateReadOutput(collection Collection, values store.Values, allLocales, allowIncomplete, hooked bool, storedNulls map[string]struct{}) error {
	plan := collection.runtimePlan().readOutput
	if plan == nil || !hooked && !plan.unconditional {
		// Without hooks, only the auth identity and primitive lists are checked.
		return nil
	}
	return walkOutput(plan, store.Object(values), allLocales, nil, func(entry outputEntry, value store.Value, location *outputLocation) error {
		nullable := !entry.identity && (!entry.field.Required || allowIncomplete || !hooked)
		if nullable && !entry.list {
			return nil
		}
		if !nullable && value.Kind() == store.ValueNull {
			if _, stored := storedNulls[location.identityPath()]; !stored {
				return &Error{Code: "invalid_field_output", Status: 500, Message: fmt.Sprintf("required field %q returned null after read hooks", location.runtimePath())}
			}
		}
		if entry.list {
			return validatePrimitiveListReadOutput(entry.field, value, location.runtimePath())
		}
		return nil
	})
}
