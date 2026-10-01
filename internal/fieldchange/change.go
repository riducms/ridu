// Package fieldchange owns schema-addressed field-kind recovery for development
// and the matching fail-closed admission for immutable migration planning.
package fieldchange

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Report describes one field-kind change and the persisted values whose
// interpretation would change. Counts include trash and all retained
// snapshots, and count each document once even for an array or localized
// field. Reports never carry document payloads.
type Report struct {
	ResourceID   schema.StableID
	ResourceSlug schema.CollectionSlug
	// Path is the stored field whose values are counted and cleared.
	Path string
	// Payload names a changed field inside the embedded plugin tree stored at
	// Path. Adapters do not interpret plugin values, so the documents counted
	// are those with any stored value at Path.
	Payload   string
	Before    string
	After     string
	Documents int
	Snapshots int
}

// Change binds one field to its unchanged containing fields in the old schema.
// Block identifies a block discriminator on the immediately preceding container.
type Change struct {
	Resource   schema.Collection
	Before     schema.Field
	After      schema.Field
	Containers []Container
	// Payload is set when the changed field is inside Before's embedded plugin
	// tree. Before and After are then the unchanged plugin field.
	Payload *PayloadChange
}

// PayloadChange is a kind change of a field declared by a plugin field's
// embedded tree, addressed as tree.case.block.field.
type PayloadChange struct {
	Path   string
	Before schema.Field
	After  schema.Field
}

type Container struct {
	Field schema.Field
	Block string
}

// embedding attributes changes found inside an embedded plugin tree to the
// outermost stored plugin field.
type embedding struct {
	owner  Change
	prefix string
}

// Detect compares stored fields at the same literal address. A kind change is
// compatible only when every value the old kind can store is read identically
// and remains valid under the new kind; a plugin or relationship's semantic
// meaning cannot be inferred from JSON shape.
func Detect(before, after schema.Snapshot) []Change {
	var changes []Change
	targets := make(map[schema.StableID]schema.Collection)
	for _, resource := range resources(after) {
		targets[resource.ID] = resource
	}
	for _, resource := range resources(before) {
		if target, exists := targets[resource.ID]; exists {
			compare(resource, resource.Fields, target.Fields, nil, nil, &changes)
		}
	}
	return changes
}

func resources(snapshot schema.Snapshot) []schema.Collection {
	return append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...)
}

func compare(resource schema.Collection, before, after []schema.Field, containers []Container, embedded *embedding, changes *[]Change) {
	targets := make(map[string]schema.Field)
	for _, candidate := range after {
		targets[candidate.Name] = candidate
	}
	for _, previous := range before {
		current, exists := targets[previous.Name]
		if !exists || !stored(previous) {
			continue
		}
		if !storedValuesFit(previous, current) {
			change := Change{Resource: resource, Before: previous, After: current, Containers: append([]Container(nil), containers...)}
			if embedded != nil {
				change = embedded.owner
				change.Payload = &PayloadChange{Path: embedded.prefix + previous.Name, Before: previous, After: current}
			}
			*changes = append(*changes, change)
			continue
		}
		within := func(segment string) *embedding {
			if embedded == nil {
				return nil
			}
			return &embedding{owner: embedded.owner, prefix: embedded.prefix + segment + "."}
		}
		if previous.Nested != nil && current.Nested != nil {
			compare(resource, previous.Nested.ResolvedFields(), current.Nested.ResolvedFields(), appendContainer(containers, Container{Field: previous}), within(previous.Name), changes)
		}
		if previous.Blocks != nil && current.Blocks != nil {
			blocks := make(map[string]schema.BlockType)
			for _, block := range current.Blocks.ResolvedTypes() {
				blocks[block.Slug] = block
			}
			for _, block := range previous.Blocks.ResolvedTypes() {
				if target, exists := blocks[block.Slug]; exists {
					compare(resource, block.ResolvedFields(), target.ResolvedFields(), appendContainer(containers, Container{Field: previous, Block: block.Slug}), within(previous.Name+"."+block.Slug), changes)
				}
			}
		}
		if previous.Plugin != nil && current.Plugin != nil {
			owner := embedding{owner: Change{Resource: resource, Before: previous, After: current, Containers: append([]Container(nil), containers...)}}
			if embedded != nil {
				owner = embedding{owner: embedded.owner, prefix: embedded.prefix + previous.Name + "."}
			}
			comparePayloads(resource, previous.Plugin.EmbeddedTrees, current.Plugin.EmbeddedTrees, owner, changes)
		}
	}
}

// comparePayloads matches embedded payload schemas by tree key, case tag value
// and block slug. A plugin owns the rest of its stored value, so only these
// ordinary schemas can be compared.
func comparePayloads(resource schema.Collection, before, after []schema.EmbeddedTree, owner embedding, changes *[]Change) {
	trees := make(map[string]schema.EmbeddedTree)
	for _, tree := range after {
		trees[tree.Key] = tree
	}
	for _, tree := range before {
		target, exists := trees[tree.Key]
		if !exists {
			continue
		}
		cases := make(map[string]schema.EmbeddedTreeCase)
		for _, candidate := range target.Cases {
			cases[candidate.TagValue] = candidate
		}
		for _, previous := range tree.Cases {
			current, exists := cases[previous.TagValue]
			if !exists {
				continue
			}
			blocks := make(map[string]schema.BlockType)
			for _, block := range current.ResolvedTypes() {
				blocks[block.Slug] = block
			}
			for _, block := range previous.ResolvedTypes() {
				if target, exists := blocks[block.Slug]; exists {
					payload := embedding{owner: owner.owner, prefix: owner.prefix + tree.Key + "." + previous.TagValue + "." + block.Slug + "."}
					compare(resource, block.ResolvedFields(), target.ResolvedFields(), nil, &payload, changes)
				}
			}
		}
	}
}

func appendContainer(containers []Container, container Container) []Container {
	return append(append([]Container(nil), containers...), container)
}

func stored(field schema.Field) bool {
	return field.Category != schema.FieldCategoryPresentation && field.Type != schema.FieldTypeVirtual && field.Type != schema.FieldTypeJoin && field.Type != schema.FieldTypeUI
}

// storedValuesFit reports whether every value stored under before is read
// identically and stays valid under after. The relation is directional:
// select → text fits, while text → select does not.
func storedValuesFit(before, after schema.Field) bool {
	if before.Category != after.Category || before.Localized != after.Localized || (before.List == nil) != (after.List == nil) {
		return false
	}
	if before.Type != after.Type {
		return stringValuesFit(before, after)
	}
	if before.Plugin != nil && after.Plugin != nil && before.Plugin.Key != after.Plugin.Key {
		return false
	}
	if before.Relationship != nil && after.Relationship != nil && (before.Relationship.HasMany != after.Relationship.HasMany || before.Relationship.Polymorphic != after.Relationship.Polymorphic) {
		return false
	}
	if before.Upload != nil && after.Upload != nil && before.Upload.HasMany != after.Upload.HasMany {
		return false
	}
	if before.Select != nil && after.Select != nil && before.Select.HasMany != after.Select.HasMany {
		return false
	}
	return true
}

// stringValuesFit admits kinds that store one plain string identically in
// every adapter: a PostgreSQL text column and a JSON or BSON string. Text,
// textarea and code accept any string. Email and date validate their format,
// so nothing else fits them. Select and radio admit only their options.
func stringValuesFit(before, after schema.Field) bool {
	if !singleString(before) || !singleString(after) {
		return false
	}
	switch after.Type {
	case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeCode:
		return true
	case schema.FieldTypeSelect, schema.FieldTypeRadio:
		return (before.Type == schema.FieldTypeSelect || before.Type == schema.FieldTypeRadio) && optionsInclude(after.Select, before.Select)
	}
	return false
}

func singleString(field schema.Field) bool {
	switch field.Type {
	case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeCode, schema.FieldTypeEmail, schema.FieldTypeDate, schema.FieldTypeRadio:
		return true
	case schema.FieldTypeSelect:
		return field.Select != nil && !field.Select.HasMany
	}
	return false
}

func optionsInclude(target, source *schema.SelectField) bool {
	if target == nil || source == nil {
		return false
	}
	allowed := make(map[string]bool, len(target.Options))
	for _, option := range target.Options {
		allowed[option.Value] = true
	}
	for _, option := range source.Options {
		if !allowed[option.Value] {
			return false
		}
	}
	return true
}

func kind(field schema.Field) string {
	label := string(field.Type)
	if field.Plugin != nil {
		label = "plugin:" + field.Plugin.Key
	}
	if field.List != nil || field.Select != nil && field.Select.HasMany || field.Relationship != nil && field.Relationship.HasMany || field.Upload != nil && field.Upload.HasMany {
		label += "[]"
	}
	if field.Relationship != nil && field.Relationship.Polymorphic {
		label = "polymorphic " + label
	}
	if field.Localized {
		label = "localized " + label
	}
	return label
}

func Reports(changes []Change) []Report {
	reports := make([]Report, len(changes))
	for index, change := range changes {
		report := Report{ResourceID: change.Resource.ID, ResourceSlug: change.Resource.Slug, Path: change.Before.Path.String(), Before: kind(change.Before), After: kind(change.After)}
		if change.Payload != nil {
			report.Payload = change.Payload.Path
			report.Before, report.After = kind(change.Payload.Before), kind(change.Payload.After)
		}
		reports[index] = report
	}
	return reports
}

// RequireEmpty admits a development kind change only when every affected
// current value and retained snapshot is empty at the synchronization boundary.
func RequireEmpty(reports []Report) error {
	for _, report := range reports {
		if report.Documents != 0 || report.Snapshots != 0 {
			return fmt.Errorf("RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM: %s changes from %s to %s; %d current documents and %d version snapshots contain stored values; review clearing in an interactive ridu dev or restore the previous field kind before synchronizing", Description(report), report.Before, report.After, report.Documents, report.Snapshots)
		}
	}
	return nil
}

// RequireTransform rejects offline schema-only plans: there is no database to
// prove the old address is empty. Destructive approval cannot replace a transform.
func RequireTransform(before, after schema.Snapshot) error {
	changes := Detect(before, after)
	if len(changes) == 0 {
		return nil
	}
	report := Reports(changes[:1])[0]
	return fmt.Errorf("RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM: %s changes from %s to %s; register a compiled data transform before ridu migrate create; --allow-destructive cannot make stored documents or version snapshots readable", Description(report), report.Before, report.After)
}

// Process counts one document per changed field and, when clear is true,
// removes precisely that field at every schema-declared occurrence. Null and
// absent values need no recovery. Unrelated JSON/plugin properties are opaque.
func Process(changes []Change, reports []Report, resource schema.StableID, values store.Values, snapshot, clear bool) (store.Values, bool, error) {
	result := store.CloneValues(values)
	changed := false
	for index, change := range changes {
		if change.Resource.ID != resource {
			continue
		}
		if clear && change.Payload != nil {
			return nil, false, fmt.Errorf("clear %s: an embedded field change cannot be cleared", Description(reports[index]))
		}
		found, err := processContainers(result, change.Containers, change.Before, clear)
		if err != nil {
			return nil, false, fmt.Errorf("inspect %s.%s: %w", change.Resource.Slug, change.Before.Path.String(), err)
		}
		if !found {
			continue
		}
		if snapshot {
			reports[index].Snapshots++
		} else {
			reports[index].Documents++
		}
		changed = true
	}
	return result, changed, nil
}

func processContainers(values store.Values, containers []Container, leaf schema.Field, clear bool) (bool, error) {
	if len(containers) == 0 {
		value, exists := values[leaf.Name]
		found := exists && hasStoredValue(value, leaf.Localized)
		if found && clear {
			delete(values, leaf.Name)
		}
		return found, nil
	}
	container := containers[0]
	value, exists := values[container.Field.Name]
	if !exists || value.Kind() == store.ValueNull {
		return false, nil
	}
	updated, found, err := processContainer(value, container, containers[1:], leaf, clear, container.Field.Localized)
	if found && clear {
		values[container.Field.Name] = updated
	}
	return found, err
}

func hasStoredValue(value store.Value, localized bool) bool {
	if value.IsZero() || value.Kind() == store.ValueNull {
		return false
	}
	if !localized || value.Kind() != store.ValueObject {
		return true
	}
	for _, localizedValue := range value.Entries() {
		if !localizedValue.IsZero() && localizedValue.Kind() != store.ValueNull {
			return true
		}
	}
	return false
}

func processContainer(value store.Value, container Container, remaining []Container, leaf schema.Field, clear, localized bool) (store.Value, bool, error) {
	if localized {
		object, ok := value.CopyObject()
		if !ok {
			return value, false, fmt.Errorf("localized container must be an object")
		}
		found := false
		for locale, localizedValue := range object {
			updated, localFound, err := processContainer(localizedValue, container, remaining, leaf, clear, false)
			if err != nil {
				return value, false, err
			}
			if localFound {
				object[locale] = updated
				found = true
			}
		}
		return store.Object(object), found, nil
	}
	if value.Kind() == store.ValueNull {
		return value, false, nil
	}
	if container.Field.Type == schema.FieldTypeGroup {
		object, ok := value.CopyObject()
		if !ok {
			return value, false, fmt.Errorf("group container must be an object")
		}
		found, err := processContainers(object, remaining, leaf, clear)
		return store.Object(object), found, err
	}
	items, ok := value.CopyList()
	if !ok {
		return value, false, fmt.Errorf("array or block container must be a list")
	}
	found := false
	for index, item := range items {
		object, ok := item.CopyObject()
		if !ok {
			return value, false, fmt.Errorf("array or block item must be an object")
		}
		if container.Block != "" {
			block, _ := object["blockType"].StringValue()
			if block != container.Block {
				continue
			}
		}
		itemFound, err := processContainers(object, remaining, leaf, clear)
		if err != nil {
			return value, false, err
		}
		if itemFound {
			items[index] = store.Object(object)
			found = true
		}
	}
	return store.List(items...), found, nil
}

// ConfirmCounts runs inside the clearing transaction. Changed counts require
// a new confirmation, so writes made while the developer was answering cannot
// silently enlarge the confirmed loss.
func ConfirmCounts(expected, actual []Report) error {
	if !reflect.DeepEqual(expected, actual) {
		return fmt.Errorf("stored field values changed while confirmation was pending; no values were cleared; save again to review fresh document and snapshot counts")
	}
	return nil
}

func AffectedResources(changes []Change) []schema.Collection {
	var result []schema.Collection
	seen := make(map[schema.StableID]bool)
	for _, change := range changes {
		if !seen[change.Resource.ID] {
			result = append(result, change.Resource)
			seen[change.Resource.ID] = true
		}
	}
	return result
}

// ValidateClear refuses clearing that would remove more than the changed
// values, bypass a framework lifecycle, or leave a required value empty.
func ValidateClear(changes []Change) error {
	for _, change := range changes {
		address := string(change.Resource.Slug) + "." + change.Before.Path.String()
		if change.Payload != nil {
			return fmt.Errorf("embedded field %s of %s changes kind; clearing would remove every complete %s value, not only that embedded field; restore the previous embedded field kind", change.Payload.Path, address, address)
		}
		if change.Before.Localized != change.After.Localized {
			return fmt.Errorf("changing localization of %s requires a reviewed migration", address)
		}
		if change.Resource.Auth != nil || change.Resource.Upload != nil {
			return fmt.Errorf("clearing changed fields on auth or upload resource %q requires an application-owned migration", change.Resource.Slug)
		}
		if !stored(change.After) {
			return fmt.Errorf("changing stored field %s to a non-stored field requires a reviewed migration", address)
		}
		// A required top-level, non-localized field is a NOT NULL column on
		// PostgreSQL and a required value on every adapter: clearing it would
		// leave documents that the new schema cannot store.
		if len(change.Containers) == 0 && !change.After.Localized && change.After.Required {
			return fmt.Errorf("%s stays required after the change, so clearing would leave required values empty; make the new field optional for the clearing save, then require it again after entering values", address)
		}
	}
	return nil
}

// Description is a stable address without any application value.
func Description(report Report) string {
	address := strings.Join([]string{string(report.ResourceSlug), report.Path}, ".")
	if report.Payload != "" {
		return address + " embedded field " + report.Payload
	}
	return address
}
