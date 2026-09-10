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

type liveEntry struct {
	binding  FieldBinding
	location fieldLocation
}
type liveScope struct {
	collection         Collection
	data, prior, input store.Values
	denied             map[string]bool
}

func liveEntries(collection Collection, values store.Values) map[string]liveEntry {
	result := map[string]liveEntry{}
	attached := map[schema.StableID]FieldBinding{}
	for _, binding := range collection.Bindings {
		attached[binding.Field.ID] = binding
	}
	var visit func([]schema.Field)
	visit = func(fields []schema.Field) {
		for _, field := range fields {
			binding, ok := attached[field.ID]
			if !ok {
				binding = FieldBinding{Field: field}
			}
			for _, location := range fieldLocationsAtPath(collection.Schema.Fields, values, field.Path.String(), false, true) {
				result[location.runtimePath] = liveEntry{binding, location}
			}
			if field.Nested != nil {
				visit(field.Nested.ResolvedFields())
			}
			if field.Blocks != nil {
				for _, block := range field.Blocks.ResolvedTypes() {
					visit(block.ResolvedFields())
				}
			}
			embedded.SchemaFields(field, visit)
		}
	}
	visit(collection.Schema.Fields)
	return result
}

func liveHasValidator(collection Collection, id schema.StableID) bool {
	for _, binding := range collection.Bindings {
		if binding.Field.ID == id && len(binding.LiveValidators) > 0 {
			return true
		}
	}
	return false
}
func livePriorLocations(binding FieldBinding, entries map[string]liveEntry, priorTokens, currentTokens map[string]string) map[string]fieldLocation {
	current := map[string]bool{}
	for _, token := range currentTokens {
		current[token] = true
	}
	result := map[string]fieldLocation{}
	for path, entry := range entries {
		if entry.binding.Field.ID == binding.Field.ID && current[priorTokens[path]] {
			result[entry.location.identity] = entry.location
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
	oldTokens := fieldIssueTargets(collection.Schema.Fields, prior, false)
	tokens := fieldIssueTargets(collection.Schema.Fields, data, false)
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
		scoped := scopedBindingContext(persisted, entry.binding, entry.location, nil)
		allowed, err := entry.binding.Access.Read(scoped)
		if err != nil {
			return result, capabilityAccessError("field read access rule failed", err)
		}
		if !allowed {
			unreadableTokens[oldTokens[path]] = true
			deleteValueAtRuntimePath(result.prior, path)
		}
	}
	for path, entry := range entries {
		scoped := scopedBindingContext(base, entry.binding, entry.location, livePriorLocations(entry.binding, oldEntries, oldTokens, tokens))
		unreadable := unreadableTokens[tokens[path]]
		if entry.binding.Access.Read != nil {
			read := scoped
			read.Operation = operation.Read
			allowed, err := entry.binding.Access.Read(read)
			if err != nil {
				return result, capabilityAccessError("field read access rule failed", err)
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
				return result, capabilityAccessError("field write access rule failed", err)
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
	if !ok || owner.binding.Field.Plugin == nil {
		return scope, liveBadRequest("embedded field is not available in this snapshot")
	}
	if livePathDenied(scope.denied, selector.Field) {
		return scope, liveDenied()
	}
	var tree *schema.EmbeddedTree
	var candidate *schema.EmbeddedTreeCase
	var variant *schema.BlockType
	for i := range owner.binding.Field.Plugin.EmbeddedTrees {
		current := &owner.binding.Field.Plugin.EmbeddedTrees[i]
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
			for k := range currentCase.ResolvedTypes() {
				if currentCase.ResolvedTypes()[k].Slug == selector.VariantSlug {
					variant = &currentCase.ResolvedTypes()[k]
					break
				}
			}
		}
	}
	if tree == nil || candidate == nil || variant == nil {
		return scope, liveBadRequest("embedded selector does not name a declared payload schema")
	}
	identity, identityOK := selector.Data[candidate.Identity].StringValue()
	kind, kindOK := selector.Data[candidate.Discriminator].StringValue()
	if !identityOK || identity != selector.Identity || !kindOK || kind != selector.VariantSlug {
		return scope, liveBadRequest("embedded payload identity or kind does not match its selector")
	}
	existing := false
	var occurrences []embedded.Occurrence
	var err error
	if owner.location.value.Kind() != "" {
		occurrences, err = embedded.Occurrences(owner.binding.Field, owner.location.value, selector.Field, nil)
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
		oldTokens := fieldIssueTargets(scope.collection.Schema.Fields, scope.prior, false)
		tokens := fieldIssueTargets(scope.collection.Schema.Fields, scope.data, false)
		for path, old := range oldEntries {
			if old.location.value.Kind() == "" || old.location.value.Kind() == store.ValueNull {
				continue
			}
			if old.binding.Field.ID != owner.binding.Field.ID || oldTokens[path] != tokens[selector.Field] {
				continue
			}
			oldOccurrences, err := embedded.Occurrences(old.binding.Field, old.location.value, path, nil)
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
	collection := scope.collection
	collection.Schema.Fields = variant.ResolvedFields()
	var bindings []FieldBinding
	ids := map[schema.StableID]bool{}
	var visit func([]schema.Field)
	visit = func(fields []schema.Field) {
		for _, f := range fields {
			ids[f.ID] = true
			if f.Nested != nil {
				visit(f.Nested.ResolvedFields())
			}
			if f.Blocks != nil {
				for _, block := range f.Blocks.ResolvedTypes() {
					visit(block.ResolvedFields())
				}
			}
			embedded.SchemaFields(f, visit)
		}
	}
	visit(variant.ResolvedFields())
	for _, binding := range collection.Bindings {
		if ids[binding.Field.ID] {
			bindings = append(bindings, binding)
		}
	}
	collection.Bindings = bindings
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
		if embedded.HasFields(field) && value.Kind() != "" && value.Kind() != store.ValueNull {
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
				for _, block := range field.Blocks.ResolvedTypes() {
					if block.Slug == tag {
						children = block.ResolvedFields()
						break
					}
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
func liveUnavailableField(fields []schema.Field, values store.Values, parts []string) (schema.Field, bool) {
	if len(parts) == 0 {
		return schema.Field{}, false
	}
	for _, field := range fields {
		if field.Name != parts[0] {
			continue
		}
		if len(parts) == 1 {
			return field, true
		}
		value := values[field.Name]
		if field.Nested != nil && field.Type == schema.FieldTypeGroup {
			object, _ := value.CopyObject()
			return liveUnavailableField(field.Nested.ResolvedFields(), object, parts[1:])
		}
		if field.Type == schema.FieldTypeArray || field.Type == schema.FieldTypeBlocks {
			index, err := strconv.Atoi(parts[1])
			row, ok := value.ListItem(index)
			if err != nil || !ok || len(parts) < 3 {
				return schema.Field{}, false
			}
			object, _ := row.CopyObject()
			if field.Nested != nil {
				return liveUnavailableField(field.Nested.ResolvedFields(), object, parts[2:])
			}
			if field.Blocks != nil {
				tag, _ := object["blockType"].StringValue()
				for _, block := range field.Blocks.ResolvedTypes() {
					if block.Slug == tag {
						return liveUnavailableField(block.ResolvedFields(), object, parts[2:])
					}
				}
			}
		}
	}
	return schema.Field{}, false
}

// Aggregate checks need an actual object/row list; null is not a completed
// enclosing scope. Scalar emptiness is instead part of the typed Value API.
func liveStructuredValueAvailable(field schema.Field, value store.Value) bool {
	if value.Kind() == "" || value.Kind() == store.ValueNull {
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
