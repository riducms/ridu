// Package referenceindex derives and reconciles current relationship and
// upload references from canonical store values. It is shared by official
// stores so the strict test adapter and PostgreSQL cannot silently diverge.
package referenceindex

import (
	"sort"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Entry is one current reference occurrence owned by a document. Occurrence
// is deterministic within the owner/field/target/locale tuple and permits
// duplicate list members without collapsing the derived index.
type Entry struct {
	Owner      store.DocumentReference
	FieldID    schema.StableID
	Target     store.DocumentReference
	Locale     schema.LocaleCode
	Occurrence int
}

// Collect derives every relationship and upload reference in one canonical
// current document, including nested containers and localized values.
func collectUnchecked(collection schema.Collection, document store.Document) []Entry {
	owner := store.DocumentReference{CollectionID: collection.ID, DocumentID: document.ID}
	var entries []Entry
	for _, field := range collection.Fields {
		value, exists := document.Values[field.Name]
		if !exists {
			continue
		}
		entries = append(entries, collectFieldUnchecked(owner, field, value, "")...)
	}
	assignOccurrences(entries)
	return entries
}

// CollectField derives references beneath one stored root field. For a
// localized physical column, locale identifies the column and value is the
// unwrapped locale value. An empty locale means value has canonical storage
// shape and may itself contain locale maps.
func collectFieldUnchecked(owner store.DocumentReference, field schema.Field, value store.Value, locale schema.LocaleCode) []Entry {
	var entries []Entry
	if locale != "" && field.Localized {
		field.Localized = false
		collectFieldValue(owner, field, value, locale, &entries)
	} else {
		collectField(owner, field, value, locale, &entries)
	}
	assignOccurrences(entries)
	return entries
}

func collectFields(owner store.DocumentReference, fields []schema.Field, values store.Values, locale schema.LocaleCode, entries *[]Entry) {
	for _, field := range fields {
		value, exists := values[field.Name]
		if !exists {
			continue
		}
		collectField(owner, field, value, locale, entries)
	}
}

func collectField(owner store.DocumentReference, field schema.Field, value store.Value, inheritedLocale schema.LocaleCode, entries *[]Entry) {
	if value.Kind() == store.ValueNull {
		return
	}
	if field.Localized {
		if value.Kind() != store.ValueObject {
			return
		}
		locales := make([]string, 0, value.Len())
		for locale := range value.Entries() {
			locales = append(locales, locale)
		}
		sort.Strings(locales)
		field.Localized = false
		for _, locale := range locales {
			collectFieldValue(owner, field, value.Get(locale), schema.LocaleCode(locale), entries)
		}
		return
	}
	collectFieldValue(owner, field, value, inheritedLocale, entries)
}

func collectFieldValue(owner store.DocumentReference, field schema.Field, value store.Value, locale schema.LocaleCode, entries *[]Entry) {
	if value.Kind() == store.ValueNull {
		return
	}
	if embedded.HasFields(field) {
		_ = embedded.Visit(field, value, field.Name, embedded.NewBudget(), func(o embedded.ReadOccurrence) error {
			collectObjectFields(owner, o.Fields, o.Payload, locale, entries)
			return nil
		})
		return
	}

	switch field.Type {
	case schema.FieldTypeRelationship:
		collectRelationship(owner, field, value, locale, entries)
	case schema.FieldTypeUpload:
		collectUpload(owner, field, value, locale, entries)
	case schema.FieldTypeGroup:
		if value.Kind() == store.ValueObject && field.Nested != nil {
			collectObjectFields(owner, field.Nested.ResolvedFields(), value, locale, entries)
		}
	case schema.FieldTypeArray:
		if value.Kind() != store.ValueList || field.Nested == nil {
			return
		}
		for item := range value.Elements() {
			if item.Kind() == store.ValueObject {
				collectObjectFields(owner, field.Nested.ResolvedFields(), item, locale, entries)
			}
		}
	case schema.FieldTypeBlocks:
		if value.Kind() != store.ValueList || field.Blocks == nil {
			return
		}
		for item := range value.Elements() {
			if item.Kind() != store.ValueObject {
				continue
			}
			blockType, _ := item.Get("blockType").StringValue()
			for _, block := range field.Blocks.ResolvedTypes() {
				if block.Slug == blockType {
					collectObjectFields(owner, block.ResolvedFields(), item, locale, entries)
					break
				}
			}
		}
	}
}

func collectObjectFields(owner store.DocumentReference, fields []schema.Field, object store.Value, locale schema.LocaleCode, entries *[]Entry) {
	for _, field := range fields {
		if value, exists := object.Lookup(field.Name); exists {
			collectField(owner, field, value, locale, entries)
		}
	}
}

func collectRelationship(owner store.DocumentReference, field schema.Field, value store.Value, locale schema.LocaleCode, entries *[]Entry) {
	relationship := field.Relationship
	if relationship == nil {
		return
	}
	collect := func(item store.Value) {
		if !relationship.Polymorphic {
			id, valid := item.StringValue()
			if valid && id != "" {
				*entries = append(*entries, Entry{
					Owner: owner, FieldID: field.ID, Locale: locale,
					Target: store.DocumentReference{CollectionID: relationship.CollectionID, DocumentID: id},
				})
			}
			return
		}
		slug, slugValid := item.Get("relationTo").StringValue()
		id, idValid := item.Get("id").StringValue()
		if !slugValid || !idValid || id == "" {
			return
		}
		for _, target := range relationship.Targets {
			if string(target.CollectionSlug) == slug {
				*entries = append(*entries, Entry{
					Owner: owner, FieldID: field.ID, Locale: locale,
					Target: store.DocumentReference{CollectionID: target.CollectionID, DocumentID: id},
				})
				break
			}
		}
	}
	if relationship.HasMany {
		for item := range value.Elements() {
			collect(item)
		}
	} else {
		collect(value)
	}
}

func collectUpload(owner store.DocumentReference, field schema.Field, value store.Value, locale schema.LocaleCode, entries *[]Entry) {
	upload := field.Upload
	if upload == nil {
		return
	}
	collect := func(item store.Value) {
		id, valid := item.StringValue()
		if valid && id != "" {
			*entries = append(*entries, Entry{
				Owner: owner, FieldID: field.ID, Locale: locale,
				Target: store.DocumentReference{CollectionID: upload.CollectionID, DocumentID: id},
			})
		}
	}
	if upload.HasMany {
		for item := range value.Elements() {
			collect(item)
		}
	} else {
		collect(value)
	}
}

func assignOccurrences(entries []Entry) {
	counts := make(map[string]int, len(entries))
	for index := range entries {
		entry := &entries[index]
		key := string(entry.Owner.CollectionID) + "\x00" + entry.Owner.DocumentID + "\x00" + string(entry.FieldID) + "\x00" +
			string(entry.Target.CollectionID) + "\x00" + entry.Target.DocumentID + "\x00" + string(entry.Locale)
		entry.Occurrence = counts[key]
		counts[key]++
	}
}

// FindReferenceField locates a relationship/upload field and its stored root
// field by stable ID.
func FindReferenceField(collection schema.Collection, fieldID schema.StableID) (reference schema.Field, root schema.Field, found bool) {
	for _, candidate := range collection.Fields {
		if reference, found = findReferenceField(candidate, fieldID); found {
			return reference, candidate, true
		}
	}
	return schema.Field{}, schema.Field{}, false
}

func findReferenceField(field schema.Field, fieldID schema.StableID) (schema.Field, bool) {
	if field.ID == fieldID && (field.Relationship != nil || field.Upload != nil) {
		return field, true
	}
	for _, child := range schema.ChildFields(field) {
		if found, ok := findReferenceField(child, fieldID); ok {
			return found, true
		}
	}

	return schema.Field{}, false
}

// ReferenceFieldIDs returns every relationship/upload field ID beneath one
// stored root field.
func ReferenceFieldIDs(root schema.Field) []schema.StableID {
	var ids []schema.StableID
	collectReferenceFieldIDs(root, &ids)
	return ids
}

func collectReferenceFieldIDs(field schema.Field, ids *[]schema.StableID) {
	if field.Relationship != nil || field.Upload != nil {
		*ids = append(*ids, field.ID)
	}
	for _, child := range schema.ChildFields(field) {
		collectReferenceFieldIDs(child, ids)
	}
}

// TargetsAnyResource reports whether the collection's current reference or
// upload topology can address one of the supplied resource IDs. Migration
// rollback uses this to purge version snapshots that could otherwise restore
// an identity after its resource is reintroduced.
func TargetsAnyResource(collection schema.Collection, resourceIDs []schema.StableID) bool {
	targets := stableIDSet(resourceIDs)
	for _, field := range collection.Fields {
		if fieldTargetsAnyResource(field, targets) {
			return true
		}
	}
	return false
}

func fieldTargetsAnyResource(field schema.Field, targets map[schema.StableID]struct{}) bool {
	if relationship := field.Relationship; relationship != nil {
		if !relationship.Polymorphic {
			if _, found := targets[relationship.CollectionID]; found {
				return true
			}
		} else {
			for _, target := range relationship.Targets {
				if _, found := targets[target.CollectionID]; found {
					return true
				}
			}
		}
	}
	if upload := field.Upload; upload != nil {
		if _, found := targets[upload.CollectionID]; found {
			return true
		}
	}
	for _, child := range schema.ChildFields(field) {
		if fieldTargetsAnyResource(child, targets) {
			return true
		}
	}

	return false
}

// RemoveResourceTargets removes every current relationship or upload value
// that addresses a retired resource, regardless of the field's ordinary
// document-delete policy. A schema rollback removes the target type itself,
// so retaining restrict-policy values would only leave dormant identities that
// could reappear on a later up migration.
func removeResourceTargetsUnchecked(collection schema.Collection, values store.Values, resourceIDs []schema.StableID) (store.Values, bool) {
	targets := stableIDSet(resourceIDs)
	result := store.CloneValues(values)
	changed := false
	for _, field := range collection.Fields {
		value, exists := result[field.Name]
		if !exists {
			continue
		}
		updated, fieldChanged := removeResourceField(field, value, targets)
		if fieldChanged {
			result[field.Name] = updated
			changed = true
		}
	}
	return result, changed
}

func stableIDSet(values []schema.StableID) map[schema.StableID]struct{} {
	result := make(map[schema.StableID]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func removeResourceFields(fields []schema.Field, values store.Values, targets map[schema.StableID]struct{}) (store.Values, bool) {
	result := store.CloneValues(values)
	changed := false
	for _, field := range fields {
		value, exists := result[field.Name]
		if !exists {
			continue
		}
		updated, fieldChanged := removeResourceField(field, value, targets)
		if fieldChanged {
			result[field.Name] = updated
			changed = true
		}
	}
	return result, changed
}

func removeResourceField(field schema.Field, value store.Value, targets map[schema.StableID]struct{}) (store.Value, bool) {
	if field.Localized {
		localized, valid := value.CopyObject()
		if !valid {
			return value, false
		}
		result := localized
		field.Localized = false
		changed := false
		for locale, localizedValue := range localized {
			updated, localeChanged := removeResourceFieldValue(field, localizedValue, targets)
			if localeChanged {
				result[locale] = updated
				changed = true
			}
		}
		if changed {
			return store.Object(result), true
		}
		return value, false
	}
	return removeResourceFieldValue(field, value, targets)
}

func removeResourceFieldValue(field schema.Field, value store.Value, targets map[schema.StableID]struct{}) (store.Value, bool) {
	if embedded.HasFields(field) {
		changed := false
		updated, err := embedded.Transform(field, value, field.Name, embedded.NewBudget(), func(o embedded.Occurrence) (store.Values, error) {
			payload, didChange := removeResourceFields(o.Fields, o.Payload, targets)
			changed = changed || didChange
			return payload, nil
		})
		if err != nil {
			return value, false
		}
		return updated, changed
	}

	switch field.Type {
	case schema.FieldTypeRelationship:
		return removeResourceRelationship(field, value, targets)
	case schema.FieldTypeUpload:
		return removeResourceUpload(field, value, targets)
	case schema.FieldTypeGroup:
		object, valid := value.CopyObject()
		if !valid || field.Nested == nil {
			return value, false
		}
		updated, changed := removeResourceFields(field.Nested.ResolvedFields(), object, targets)
		if changed {
			return store.Object(updated), true
		}
	case schema.FieldTypeArray:
		items, valid := value.CopyList()
		if !valid || field.Nested == nil {
			return value, false
		}
		updated := items
		changed := false
		for index, item := range items {
			object, valid := item.CopyObject()
			if !valid {
				continue
			}
			itemValues, itemChanged := removeResourceFields(field.Nested.ResolvedFields(), object, targets)
			if itemChanged {
				updated[index] = store.Object(itemValues)
				changed = true
			}
		}
		if changed {
			return store.List(updated...), true
		}
	case schema.FieldTypeBlocks:
		items, valid := value.CopyList()
		if !valid || field.Blocks == nil {
			return value, false
		}
		updated := items
		changed := false
		for index, item := range items {
			object, valid := item.CopyObject()
			if !valid {
				continue
			}
			blockType, _ := object["blockType"].StringValue()
			for _, block := range field.Blocks.ResolvedTypes() {
				if block.Slug != blockType {
					continue
				}
				itemValues, itemChanged := removeResourceFields(block.ResolvedFields(), object, targets)
				if itemChanged {
					updated[index] = store.Object(itemValues)
					changed = true
				}
				break
			}
		}
		if changed {
			return store.List(updated...), true
		}
	}
	return value, false
}

func removeResourceRelationship(field schema.Field, value store.Value, targets map[schema.StableID]struct{}) (store.Value, bool) {
	relationship := field.Relationship
	if relationship == nil {
		return value, false
	}
	if !relationship.Polymorphic {
		if _, retired := targets[relationship.CollectionID]; !retired {
			return value, false
		}
		if relationship.HasMany {
			if value.Kind() != store.ValueList || value.Len() == 0 {
				return value, false
			}
			return store.List(), true
		}
		if value.Kind() != store.ValueNull {
			return store.Null(), true
		}
		return value, false
	}
	if relationship.HasMany {
		if value.Kind() != store.ValueList {
			return value, false
		}
		filtered := make([]store.Value, 0, value.Len())
		for item := range value.Elements() {
			if polymorphicValueTargetsResource(*relationship, item, targets) {
				continue
			}
			filtered = append(filtered, item)
		}
		if len(filtered) != value.Len() {
			return store.List(filtered...), true
		}
		return value, false
	}
	if polymorphicValueTargetsResource(*relationship, value, targets) {
		return store.Null(), true
	}
	return value, false
}

func polymorphicValueTargetsResource(relationship schema.RelationshipField, value store.Value, targets map[schema.StableID]struct{}) bool {
	slug, valid := value.Get("relationTo").StringValue()
	if !valid {
		return false
	}
	for _, target := range relationship.Targets {
		if string(target.CollectionSlug) != slug {
			continue
		}
		_, retired := targets[target.CollectionID]
		return retired
	}
	return false
}

func removeResourceUpload(field schema.Field, value store.Value, targets map[schema.StableID]struct{}) (store.Value, bool) {
	upload := field.Upload
	if upload == nil {
		return value, false
	}
	if _, retired := targets[upload.CollectionID]; !retired {
		return value, false
	}
	if upload.HasMany {
		if value.Kind() != store.ValueList || value.Len() == 0 {
			return value, false
		}
		return store.List(), true
	}
	if value.Kind() != store.ValueNull {
		return store.Null(), true
	}
	return value, false
}

// NullifyTarget reconciles every nullify-policy reference in a current
// document. Callers must plan restrict matches across the whole transaction
// before invoking it.
func nullifyTargetUnchecked(collection schema.Collection, values store.Values, target store.DocumentReference) (store.Values, bool) {
	result := store.CloneValues(values)
	changed := false
	for _, field := range collection.Fields {
		value, exists := result[field.Name]
		if !exists {
			continue
		}
		updated, fieldChanged := nullifyField(field, value, target)
		if fieldChanged {
			result[field.Name] = updated
			changed = true
		}
	}
	return result, changed
}

// NullifyField reconciles a target beneath one stored root field. locale is
// non-empty only when value came from one localized physical column.
func nullifyFieldUnchecked(field schema.Field, value store.Value, target store.DocumentReference, locale schema.LocaleCode) (store.Value, bool) {
	if locale != "" && field.Localized {
		field.Localized = false
		return nullifyFieldValue(field, value, target)
	}
	return nullifyField(field, value, target)
}

func nullifyFields(fields []schema.Field, values store.Values, target store.DocumentReference) (store.Values, bool) {
	result := store.CloneValues(values)
	changed := false
	for _, field := range fields {
		value, exists := result[field.Name]
		if !exists {
			continue
		}
		updated, fieldChanged := nullifyField(field, value, target)
		if fieldChanged {
			result[field.Name] = updated
			changed = true
		}
	}
	return result, changed
}

func nullifyField(field schema.Field, value store.Value, target store.DocumentReference) (store.Value, bool) {
	if field.Localized {
		localized, valid := value.CopyObject()
		if !valid {
			return value, false
		}
		result := localized
		field.Localized = false
		changed := false
		for locale, localizedValue := range localized {
			updated, localeChanged := nullifyFieldValue(field, localizedValue, target)
			if localeChanged {
				result[locale] = updated
				changed = true
			}
		}
		if changed {
			return store.Object(result), true
		}
		return value, false
	}
	return nullifyFieldValue(field, value, target)
}

func nullifyFieldValue(field schema.Field, value store.Value, target store.DocumentReference) (store.Value, bool) {
	if embedded.HasFields(field) {
		changed := false
		updated, err := embedded.Transform(field, value, field.Name, embedded.NewBudget(), func(o embedded.Occurrence) (store.Values, error) {
			payload, didChange := nullifyFields(o.Fields, o.Payload, target)
			changed = changed || didChange
			return payload, nil
		})
		if err != nil {
			return value, false
		}
		return updated, changed
	}

	switch field.Type {
	case schema.FieldTypeRelationship:
		return nullifyRelationship(field, value, target)
	case schema.FieldTypeUpload:
		return nullifyUpload(field, value, target)
	case schema.FieldTypeGroup:
		object, valid := value.CopyObject()
		if !valid || field.Nested == nil {
			return value, false
		}
		updated, changed := nullifyFields(field.Nested.ResolvedFields(), object, target)
		if changed {
			return store.Object(updated), true
		}
	case schema.FieldTypeArray:
		items, valid := value.CopyList()
		if !valid || field.Nested == nil {
			return value, false
		}
		updated := items
		changed := false
		for index, item := range items {
			object, valid := item.CopyObject()
			if !valid {
				continue
			}
			itemValues, itemChanged := nullifyFields(field.Nested.ResolvedFields(), object, target)
			if itemChanged {
				updated[index] = store.Object(itemValues)
				changed = true
			}
		}
		if changed {
			return store.List(updated...), true
		}
	case schema.FieldTypeBlocks:
		items, valid := value.CopyList()
		if !valid || field.Blocks == nil {
			return value, false
		}
		updated := items
		changed := false
		for index, item := range items {
			object, valid := item.CopyObject()
			if !valid {
				continue
			}
			blockType, _ := object["blockType"].StringValue()
			for _, block := range field.Blocks.ResolvedTypes() {
				if block.Slug != blockType {
					continue
				}
				itemValues, itemChanged := nullifyFields(block.ResolvedFields(), object, target)
				if itemChanged {
					updated[index] = store.Object(itemValues)
					changed = true
				}
				break
			}
		}
		if changed {
			return store.List(updated...), true
		}
	}
	return value, false
}

func nullifyRelationship(field schema.Field, value store.Value, target store.DocumentReference) (store.Value, bool) {
	relationship := field.Relationship
	if relationship == nil || relationship.OnDelete != schema.ReferenceDeleteNullify {
		return value, false
	}
	if relationship.HasMany {
		if value.Kind() != store.ValueList {
			return value, false
		}
		filtered := make([]store.Value, 0, value.Len())
		for item := range value.Elements() {
			if relationshipValueMatches(*relationship, item, target) {
				continue
			}
			filtered = append(filtered, item)
		}
		if len(filtered) != value.Len() {
			return store.List(filtered...), true
		}
		return value, false
	}
	if relationshipValueMatches(*relationship, value, target) {
		return store.Null(), true
	}
	return value, false
}

func relationshipValueMatches(relationship schema.RelationshipField, value store.Value, target store.DocumentReference) bool {
	if !relationship.Polymorphic {
		id, valid := value.StringValue()
		return valid && relationship.CollectionID == target.CollectionID && id == target.DocumentID
	}
	slug, slugValid := value.Get("relationTo").StringValue()
	id, idValid := value.Get("id").StringValue()
	if !slugValid || !idValid || id != target.DocumentID {
		return false
	}
	for _, candidate := range relationship.Targets {
		if candidate.CollectionID == target.CollectionID && string(candidate.CollectionSlug) == slug {
			return true
		}
	}
	return false
}

func nullifyUpload(field schema.Field, value store.Value, target store.DocumentReference) (store.Value, bool) {
	upload := field.Upload
	if upload == nil || upload.OnDelete != schema.ReferenceDeleteNullify || upload.CollectionID != target.CollectionID {
		return value, false
	}
	if upload.HasMany {
		if value.Kind() != store.ValueList {
			return value, false
		}
		filtered := make([]store.Value, 0, value.Len())
		for item := range value.Elements() {
			id, valid := item.StringValue()
			if valid && id == target.DocumentID {
				continue
			}
			filtered = append(filtered, item)
		}
		if len(filtered) != value.Len() {
			return store.List(filtered...), true
		}
		return value, false
	}
	id, valid := value.StringValue()
	if valid && id == target.DocumentID {
		return store.Null(), true
	}
	return value, false
}
