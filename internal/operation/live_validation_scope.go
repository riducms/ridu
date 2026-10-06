package operation

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// liveEntry is one field location of a live-validation snapshot: its field,
// named by its placement, and its binding. An unbound field has a zero binding.
type liveEntry struct {
	field     schema.Field
	binding   FieldBinding
	location  fieldLocation
	placement fieldPlacement
}
type liveScope struct {
	collection         Collection
	data, prior, input store.Values
	denied             map[string]bool
}

// liveEntries lists every field location in values with its binding. One walk
// follows the values, so the cost follows the snapshot rather than every
// placement of the schema.
func liveEntries(collection Collection, values store.Values) map[string]liveEntry {
	result := map[string]liveEntry{}
	plan := collection.runtimePlan()
	eachFieldLocation(collection, store.Object(values), false, func(location fieldLocation) {
		placement := fieldPlacement{canonical: strings.Split(location.canonical(), "."), shared: location.shared()}
		entry := liveEntry{field: placementField(collection.Schema.ID, *location.field, placement.canonical, placement.shared), location: location, placement: placement}
		if bound, ok := plan.binding(location); ok {
			entry.binding = collection.Bindings[bound]
		}
		result[location.runtimePath] = entry
	})
	return result
}

// liveHasValidator reports whether the located field has a live validator.
func liveHasValidator(collection Collection, location fieldLocation) bool {
	index, bound := collection.runtimePlan().binding(location)
	return bound && len(collection.Bindings[index].LiveValidators) > 0
}

func livePriorLocations(entry liveEntry, entries map[string]liveEntry, priorTokens, currentTokens map[string]string) map[string]fieldLocation {
	current := map[string]bool{}
	for _, token := range currentTokens {
		current[token] = true
	}
	result := map[string]fieldLocation{}
	for path, prior := range entries {
		if prior.field.ID == entry.field.ID && current[priorTokens[path]] {
			result[prior.location.identity] = prior.location
		}
	}
	return result
}
func livePathDenied(denied map[string]bool, path string) bool {
	for key := range denied {
		if path == key || strings.HasPrefix(path, key+".") {
			return true
		}
	}
	return false
}

// Read permission must hold for persisted and prospective views. A submitted
// flag cannot expose retained protected content by changing its own read rule.
func liveScopeViews(collection Collection, base Context, data, prior, input store.Values) (liveScope, error) {
	result := liveScope{collection: collection, data: store.CloneValues(data), prior: store.CloneValues(prior), input: store.CloneValues(input), denied: map[string]bool{}}
	entries := liveEntries(collection, data)
	oldEntries := liveEntries(collection, prior)
	oldTokens := fieldIssueTargets(collection, prior, false)
	tokens := fieldIssueTargets(collection, data, false)
	unreadableTokens := map[string]bool{}
	base.Data, base.Document, base.originalCanonical = data, nil, nil
	base.Original = &store.Document{Values: prior}
	for path, entry := range oldEntries {
		if entry.binding.Access.Read == nil {
			continue
		}
		persisted := base
		persisted.Operation = operation.Read
		persisted.Data = prior
		scoped := scopedBindingContext(persisted, entry.binding, entry.location, nil, nil)
		allowed, err := entry.binding.Access.Read(scoped)
		if err != nil {
			return result, accessRuleError("field read access rule failed", err)
		}
		if !allowed {
			unreadableTokens[oldTokens[path]] = true
			deleteValueAtRuntimePath(result.prior, path)
		}
	}
	for path, entry := range entries {
		scoped := scopedBindingContext(base, entry.binding, entry.location, livePriorLocations(entry, oldEntries, oldTokens, tokens), nil)
		unreadable := unreadableTokens[tokens[path]]
		if entry.binding.Access.Read != nil {
			read := scoped
			read.Operation = operation.Read
			allowed, err := entry.binding.Access.Read(read)
			if err != nil {
				return result, accessRuleError("field read access rule failed", err)
			}
			unreadable = unreadable || !allowed
		}
		if unreadable {
			result.denied[path] = true
			deleteValueAtRuntimePath(result.data, path)
			deleteValueAtRuntimePath(result.input, path)
		}
		rule := entry.binding.Access.Create
		if base.Operation == operation.Update {
			rule = entry.binding.Access.Update
		}
		if rule != nil {
			allowed, err := rule(scoped)
			if err != nil {
				return result, accessRuleError("field write access rule failed", err)
			}
			if !allowed {
				result.denied[path] = true
			}
		}
	}
	return result, nil
}

func liveDescendScope(scope liveScope, base Context, selector LiveValidationEmbeddedScope) (liveScope, error) {
	if !liveValidPath(selector.Field) || strings.TrimSpace(selector.Identity) == "" || len(selector.Identity) > 512 || !utf8.ValidString(selector.Identity) || selector.Data == nil {
		return scope, liveBadRequest("embedded validation requires a declared field, identity, and payload")
	}
	owner, ok := liveEntries(scope.collection, scope.data)[selector.Field]
	if !ok || owner.field.Plugin == nil {
		return scope, liveBadRequest("embedded field is not available in this snapshot")
	}
	if livePathDenied(scope.denied, selector.Field) {
		return scope, liveDenied()
	}
	var tree *schema.EmbeddedTree
	var candidate *schema.EmbeddedTreeCase
	var variant schema.BlockType
	found := false
	for i := range owner.field.Plugin.EmbeddedTrees {
		current := &owner.field.Plugin.EmbeddedTrees[i]
		if current.Key != selector.TreeKey {
			continue
		}
		tree = current
		for j := range current.Cases {
			currentCase := &current.Cases[j]
			if currentCase.TagValue != selector.CaseTag {
				continue
			}
			candidate = currentCase
			variant, found = currentCase.Definition(selector.VariantSlug)
		}
	}
	if tree == nil || candidate == nil || !found {
		return scope, liveBadRequest("embedded selector does not name a declared payload schema")
	}
	// The payload's fields sit beneath the owner's placement; a registered
	// payload block is its shared definition.
	payload := owner.placement.enter(selector.TreeKey, selector.CaseTag, selector.VariantSlug)
	identity, identityOK := selector.Data[candidate.Identity].StringValue()
	kind, kindOK := selector.Data[candidate.Discriminator].StringValue()
	if !identityOK || identity != selector.Identity || !kindOK || kind != selector.VariantSlug {
		return scope, liveBadRequest("embedded payload identity or kind does not match its selector")
	}
	existing := false
	var occurrences []embedded.Occurrence
	var err error
	if !owner.location.value.IsZero() {
		occurrences, err = embedded.Occurrences(owner.field, owner.location.value, selector.Field, nil)
	}
	if err != nil {
		return scope, liveBadRequest("embedded field structure is unavailable")
	}
	for _, occurrence := range occurrences {
		if occurrence.Tree.Key == selector.TreeKey && occurrence.Key == selector.Identity {
			if occurrence.Case.TagValue != selector.CaseTag || occurrence.Type.Slug != selector.VariantSlug {
				return scope, liveBadRequest("embedded identity belongs to a different payload kind")
			}
			existing = true
		}
	}
	prior := store.Values{}
	if existing {
		oldEntries := liveEntries(scope.collection, scope.prior)
		oldTokens := fieldIssueTargets(scope.collection, scope.prior, false)
		tokens := fieldIssueTargets(scope.collection, scope.data, false)
		for path, old := range oldEntries {
			if old.location.value.IsZero() || old.location.value.Kind() == store.ValueNull {
				continue
			}
			if old.field.ID != owner.field.ID || oldTokens[path] != tokens[selector.Field] {
				continue
			}
			oldOccurrences, err := embedded.Occurrences(old.field, old.location.value, path, nil)
			if err != nil {
				return scope, liveBadRequest("stored embedded structure is unavailable")
			}
			for _, occurrence := range oldOccurrences {
				if occurrence.Tree.Key == selector.TreeKey && occurrence.Case.TagValue == selector.CaseTag && occurrence.Type.Slug == selector.VariantSlug && occurrence.Key == selector.Identity {
					prior = occurrence.Payload
				}
			}
		}
	}
	// The payload scope validates the variant's own fields and bindings: its
	// plan locates the variant definition's bindings, never the owner's.
	collection := scope.collection.narrowed(variant.ResolvedFields(), payload)
	// The payload is already an exact-locale form. Convert its prior to canonical
	// shape solely to reuse the existing retained-child update merge.
	selection := localization.Selection{Locale: base.Locale, Chain: []schema.LocaleCode{base.Locale}, Configured: base.Locales, PreserveNull: true}
	priorCanonical, err := localization.StoragePatch(variant.ResolvedFields(), prior, selection)
	if err != nil {
		return scope, liveBadRequest("embedded prior is unavailable")
	}
	data, err := completeUpdateForValidation(variant.ResolvedFields(), priorCanonical, selector.Data, selection, nil)
	if err != nil {
		return scope, liveBadRequest("embedded snapshot is malformed")
	}
	completed := data
	data = store.CloneValues(prior)
	for name, value := range completed {
		data[name] = value
	}
	if err := liveValidateStructure(variant.ResolvedFields(), data, "", embedded.NewBudget()); err != nil {
		return scope, err
	}
	base.Collection = collection.Schema
	base.Data = data
	base.Original = &store.Document{Values: prior}
	base.originalCanonical = &store.Document{Values: priorCanonical}
	base.submittedData = selector.Data
	base.submittedLocale = base.Locale
	if err := authorizeBoundFields(collection, base, base.Operation, prior, data, false); err != nil {
		return scope, err
	}
	return liveScopeViews(collection, base, data, prior, selector.Data)
}

// Invalid repeated identities cannot be converted to display-index identities.
// Ordinary malformed scalar values are left for the typed adapter to skip.
func liveValidateStructure(fields []schema.Field, values store.Values, path string, budget *embedded.Budget) error {
	if err := budget.Enter(path); err != nil {
		return liveBadRequest("snapshot exceeds live validation work limits")
	}
	defer budget.Leave()
	for _, field := range fields {
		value := values[field.Name]
		current := joinFieldPath(path, field.Name)
		if embedded.HasFields(field) && !value.IsZero() && value.Kind() != store.ValueNull {
			occurrences, err := embedded.Occurrences(field, value, current, budget)
			if err != nil {
				return liveBadRequest("snapshot contains an invalid embedded structure")
			}
			for _, o := range occurrences {
				if o.Key == "" {
					return liveBadRequest("embedded payloads require stable identities")
				}
				if err := liveValidateStructure(o.Fields, o.Payload, o.RuntimePath, budget); err != nil {
					return err
				}
			}
		}
		if field.Type == schema.FieldTypeGroup && field.Nested != nil {
			if object, ok := value.CopyObject(); ok {
				if err := liveValidateStructure(field.Nested.ResolvedFields(), object, current, budget); err != nil {
					return err
				}
			}
		}
		if field.Type != schema.FieldTypeArray && field.Type != schema.FieldTypeBlocks {
			continue
		}
		if value.Kind() != store.ValueList {
			continue
		}
		seen := map[string]bool{}
		index := -1
		for row := range value.Elements() {
			index++
			object, ok := row.CopyObject()
			if !ok {
				continue
			}
			key, ok := object["_key"].StringValue()
			if !ok || strings.TrimSpace(key) == "" || !utf8.ValidString(key) || len(key) > 512 || seen[key] {
				return liveBadRequest("repeated rows require distinct nonempty stable keys of at most 512 bytes")
			}
			seen[key] = true
			children := []schema.Field(nil)
			if field.Nested != nil {
				children = field.Nested.ResolvedFields()
			}
			if field.Blocks != nil {
				tag, _ := object["blockType"].StringValue()
				if block, found := field.Blocks.Definition(tag); found {
					children = block.ResolvedFields()
				}
				if children == nil {
					return liveBadRequest("Block row does not name a declared kind")
				}
			}
			if err := liveValidateStructure(children, object, joinFieldPath(current, strconv.Itoa(index)), budget); err != nil {
				return err
			}
		}
	}
	return nil
}

// Resolve only ordinary missing enclosing structures. Existing occurrences,
// including plugin payloads, are always selected from server enumeration above.
// The location names the field as its list declares it and its placement.
func liveUnavailableField(collection Collection, values store.Values, parts []string) (fieldLocation, bool) {
	var resolve func([]schema.Field, store.Values, []string, walkPosition) (fieldLocation, bool)
	resolve = func(fields []schema.Field, values store.Values, parts []string, at walkPosition) (fieldLocation, bool) {
		if len(parts) == 0 {
			return fieldLocation{}, false
		}
		for index := range fields {
			field := &fields[index]
			if field.Name != parts[0] {
				continue
			}
			member := at.member(field)
			if len(parts) == 1 {
				return member.location(fields, store.Value{}, store.Value{}, at.runtime), true
			}
			value := values[field.Name]
			if field.Nested != nil && field.Type == schema.FieldTypeGroup {
				object, _ := value.CopyObject()
				return resolve(field.Nested.ResolvedFields(), object, parts[1:], member)
			}
			if field.Type == schema.FieldTypeArray || field.Type == schema.FieldTypeBlocks {
				row, err := strconv.Atoi(parts[1])
				item, ok := value.ListItem(row)
				if err != nil || !ok || len(parts) < 3 {
					return fieldLocation{}, false
				}
				object, _ := item.CopyObject()
				if field.Nested != nil {
					return resolve(field.Nested.ResolvedFields(), object, parts[2:], member)
				}
				if field.Blocks != nil {
					tag, _ := object["blockType"].StringValue()
					if block, found := field.Blocks.Definition(tag); found {
						return resolve(block.ResolvedFields(), object, parts[2:], member.enter(tag))
					}
				}
			}
		}
		return fieldLocation{}, false
	}
	return resolve(collection.Schema.Fields, values, parts, rootPosition(collection))
}

// Aggregate checks need an actual object/row list; null is not a completed
// enclosing scope. Scalar emptiness is instead part of the typed Value API.
func liveStructuredValueAvailable(field schema.Field, value store.Value) bool {
	if value.IsZero() || value.Kind() == store.ValueNull {
		return field.Type != schema.FieldTypeGroup && field.Type != schema.FieldTypeArray && field.Type != schema.FieldTypeBlocks
	}
	switch field.Type {
	case schema.FieldTypeGroup:
		return value.Kind() == store.ValueObject
	case schema.FieldTypeArray, schema.FieldTypeBlocks:
		if value.Kind() != store.ValueList {
			return false
		}
		for row := range value.Elements() {
			if row.Kind() != store.ValueObject {
				return false
			}
		}
	case schema.FieldTypePoint:
		if value.Kind() != store.ValueList || value.Len() != 2 {
			return false
		}
		for coordinate := range value.Elements() {
			number, ok := coordinate.NumberValue()
			if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
				return false
			}
		}
	case schema.FieldTypeRelationship:
		if field.Relationship == nil || !field.Relationship.Polymorphic {
			return true
		}
		if field.Relationship.HasMany && value.Kind() != store.ValueList {
			return false
		}
		for item := range referenceElements(value, field.Relationship.HasMany) {
			if item.Kind() != store.ValueObject {
				return false
			}
			slug, slugOK := item.Get("relationTo").StringValue()
			id, idOK := item.Get("id").StringValue()
			if !slugOK || !idOK || id == "" || !relationshipTargetExists(field.Relationship.Targets, slug) {
				return false
			}
		}
	}
	return true
}
