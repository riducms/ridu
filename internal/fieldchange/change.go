// Package fieldchange owns schema-addressed field-kind recovery for development
// and the matching fail-closed admission for immutable migration planning.
//
// A block definition is compared once: a kind change inside it is one change,
// counted and cleared in every stored block of the definition, however many
// placements the block graph gives it.
package fieldchange

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/riducms/ridu/internal/blockgraph"
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
	// Block names the block definition of a change inside it, whose Path is
	// relative to the definition; ResourceID and ResourceSlug are then empty.
	Block string
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
	// Resource holds a change of a resource's own field. It is zero for a
	// change inside a block definition.
	Resource schema.Collection
	// Block names the definition of a change inside one; Containers and the
	// fields are then relative to the definition, and the change applies to
	// every stored block of it in Resources.
	Block string
	// Localized limits a definition change to blocks stored beneath a
	// localized ancestor (true) or not (false). Nil applies to both.
	Localized *bool
	// Resources are the resources whose stored values can hold the
	// definition's blocks, in the old schema.
	Resources  []schema.Collection
	Before     schema.Field
	After      schema.Field
	Containers []Container
	// Payload is set when the changed field is inside Before's embedded plugin
	// tree. Before and After are then the unchanged plugin field.
	Payload *PayloadChange
	graph   *blockgraph.Graph
}

// TopLevel reports a change of a resource's own top-level field.
func (change Change) TopLevel() bool {
	return change.Block == "" && len(change.Containers) == 0
}

// AppliesTo reports whether stored documents of resource can hold the change.
func (change Change) AppliesTo(resource schema.StableID) bool {
	if change.Block == "" {
		return change.Resource.ID == resource
	}
	for _, candidate := range change.Resources {
		if candidate.ID == resource {
			return true
		}
	}
	return false
}

// Roots returns the top-level fields of resource whose stored values the
// change reads and clears.
func (change Change) Roots(resource schema.Collection) []schema.Field {
	if change.Block == "" {
		if len(change.Containers) != 0 {
			return []schema.Field{change.Containers[0].Field}
		}
		return []schema.Field{change.Before}
	}
	node, found := change.graph.ResourceOf(resource)
	if !found {
		return nil
	}
	var roots []schema.Field
	for _, root := range node.Roots {
		if change.graph.RootPlaces(root, map[string]bool{change.Block: true}) {
			roots = append(roots, root.Field)
		}
	}
	return roots
}

// PayloadChange is a kind change of a field declared by a plugin field's
// embedded tree, addressed as block.field within the embedded block definition.
type PayloadChange struct {
	Path   string
	Before schema.Field
	After  schema.Field
}

type Container struct {
	Field schema.Field
	Block string
}

// Detect compares stored fields at the same literal address. A kind change is
// compatible only when every value the old kind can store is read identically
// and remains valid under the new kind; a plugin or relationship's semantic
// meaning cannot be inferred from JSON shape.
//
// Each resource's own fields are compared, then each block definition both
// schemas place beneath a resource they share, once in each localization
// scope. A changed field inside a definition placed in an embedded plugin
// tree is a change of the stored plugin field that holds the tree.
func Detect(before, after schema.Snapshot) []Change {
	previous, current := blockgraph.New(before), blockgraph.New(after)
	detector := detector{before: previous, after: current, payloads: make(map[blockgraph.Key][]PayloadChange)}
	var changes []Change
	// resources lists, per definition, the surviving resources that place it
	// before and after the change, in the old schema.
	resources := make(map[string][]schema.Collection)
	for _, pair := range blockgraph.Survivors(previous, current, nil) {
		owner := Change{Resource: pair.Before.Resource}
		detector.compare(owner, pair.Before.Resource.Fields, pair.After.Resource.Fields, nil, false, &changes)
		placed := pair.After.Placed(current, false)
		for _, key := range blockgraph.SortedKeys(pair.Before.Placed(previous, false)) {
			slugResources := resources[key.Slug]
			if placed[key] && (len(slugResources) == 0 || slugResources[len(slugResources)-1].ID != pair.Before.Resource.ID) {
				resources[key.Slug] = append(slugResources, pair.Before.Resource)
			}
		}
	}
	shared := blockgraph.SortedKeys(blockgraph.Shared(previous, current, nil, false))
	for index := 0; index < len(shared); {
		slug := shared[index].Slug
		var scopes [][]Change
		var keys []blockgraph.Key
		for ; index < len(shared) && shared[index].Slug == slug; index++ {
			key := shared[index]
			beforeView, _ := previous.View(key)
			afterView, _ := current.View(key)
			owner := Change{Block: slug, Resources: resources[slug], graph: previous}
			var found []Change
			detector.compare(owner, beforeView.ResolvedFields(), afterView.ResolvedFields(), nil, key.Localized, &found)
			scopes, keys = append(scopes, found), append(keys, key)
		}
		changes = append(changes, mergeScopes(scopes, keys)...)
	}
	return changes
}

// mergeScopes combines a definition's changes found in each placed
// localization scope. A change found in only one of two scopes applies only
// to blocks stored in that scope.
func mergeScopes(scopes [][]Change, keys []blockgraph.Key) []Change {
	if len(scopes) == 1 {
		return scopes[0]
	}
	describe := func(change Change) string {
		description := change.Before.Path.String()
		if change.Payload != nil {
			description += "\x00" + change.Payload.Path
		}
		return description
	}
	index := func(changes []Change) map[string]bool {
		result := make(map[string]bool, len(changes))
		for _, change := range changes {
			result[describe(change)] = true
		}
		return result
	}
	first, second := index(scopes[0]), index(scopes[1])
	var merged []Change
	for _, change := range scopes[0] {
		if !second[describe(change)] {
			localized := keys[0].Localized
			change.Localized = &localized
		}
		merged = append(merged, change)
	}
	for _, change := range scopes[1] {
		if !first[describe(change)] {
			localized := keys[1].Localized
			change.Localized = &localized
			merged = append(merged, change)
		}
	}
	return merged
}

type detector struct {
	before, after *blockgraph.Graph
	payloads      map[blockgraph.Key][]PayloadChange
}

// compare reports kind changes among fields with the same name in the same
// container, recursing through unchanged groups and arrays. A Blocks container
// does not enter its definitions, which are compared on their own.
func (detector detector) compare(owner Change, before, after []schema.Field, containers []Container, localized bool, changes *[]Change) {
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
			change := owner
			change.Before, change.After, change.Containers = previous, current, append([]Container(nil), containers...)
			*changes = append(*changes, change)
			continue
		}
		if previous.Nested != nil && current.Nested != nil {
			detector.compare(owner, previous.Nested.ResolvedFields(), current.Nested.ResolvedFields(), appendContainer(containers, Container{Field: previous}), localized || previous.Localized, changes)
		}
		if previous.Plugin != nil && current.Plugin != nil && detector.payloads != nil {
			for _, payload := range detector.embeddedChanges(previous, current, localized) {
				change := owner
				change.Before, change.After, change.Containers = previous, current, append([]Container(nil), containers...)
				payload := payload
				change.Payload = &payload
				*changes = append(*changes, change)
			}
		}
	}
}

// embeddedChanges lists the kind changes inside the block definitions a
// plugin field's embedded trees place, at any depth, before and after the
// change. Each definition is compared once.
func (detector detector) embeddedChanges(previous, current schema.Field, localized bool) []PayloadChange {
	_, beforeKeys := blockgraph.FieldSelections(previous, localized)
	_, afterKeys := blockgraph.FieldSelections(current, localized)
	placed := detector.before.Closure(beforeKeys, true)
	var result []PayloadChange
	for _, key := range blockgraph.SortedKeys(detector.after.Closure(afterKeys, true)) {
		if !placed[key] {
			continue
		}
		changes, compared := detector.payloads[key]
		if !compared {
			beforeView, _ := detector.before.View(key)
			afterView, _ := detector.after.View(key)
			// The closure already holds every definition nested in this one,
			// so its own plugin fields need no separate payload comparison.
			own := detector
			own.payloads = nil
			var found []Change
			own.compare(Change{}, beforeView.ResolvedFields(), afterView.ResolvedFields(), nil, key.Localized, &found)
			for _, change := range found {
				changes = append(changes, PayloadChange{Path: key.Slug + "." + change.Before.Path.String(), Before: change.Before, After: change.After})
			}
			detector.payloads[key] = changes
		}
		result = append(result, changes...)
	}
	return result
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
		report := Report{ResourceID: change.Resource.ID, ResourceSlug: change.Resource.Slug, Block: change.Block, Path: change.Before.Path.String(), Before: kind(change.Before), After: kind(change.After)}
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
// values are a stored document of resource in the old schema.
func Process(changes []Change, reports []Report, resource schema.Collection, values store.Values, snapshot, clear bool) (store.Values, bool, error) {
	result := values
	changed := false
	for index, change := range changes {
		if !change.AppliesTo(resource.ID) {
			continue
		}
		if clear && change.Payload != nil {
			return nil, false, fmt.Errorf("clear %s: an embedded field change cannot be cleared", Description(reports[index]))
		}
		var found bool
		var err error
		if change.Block == "" {
			working := store.CloneValues(result)
			found, err = processContainers(working, change.Containers, change.Before, clear)
			if found && clear {
				result = working
			}
		} else {
			result, found, err = change.processBlocks(resource, result, clear)
		}
		if err != nil {
			return nil, false, fmt.Errorf("inspect %s: %w", Description(reports[index]), err)
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
	if !changed {
		return store.CloneValues(values), false, nil
	}
	return result, true, nil
}

// processBlocks processes a definition change in every stored block of it.
func (change Change) processBlocks(resource schema.Collection, values store.Values, clear bool) (store.Values, bool, error) {
	walker := change.graph.Walker(map[string]bool{change.Block: true})
	found := false
	updated, _, err := walker.Rewrite(resource.Fields, values, "", func(row blockgraph.Row, item store.Values) (bool, error) {
		if change.Localized != nil && *change.Localized != (row.Locale != "") {
			return false, nil
		}
		rowFound, err := processContainers(item, change.Containers, change.Before, clear)
		found = found || rowFound
		return rowFound && clear, err
	})
	if err != nil || !found {
		return values, found, err
	}
	return updated, true, nil
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

// AffectedResources returns the resources whose stored documents can hold a
// change, in order of first appearance.
func AffectedResources(changes []Change) []schema.Collection {
	var result []schema.Collection
	seen := make(map[schema.StableID]bool)
	add := func(resource schema.Collection) {
		if !seen[resource.ID] {
			result = append(result, resource)
			seen[resource.ID] = true
		}
	}
	for _, change := range changes {
		if change.Block == "" {
			add(change.Resource)
			continue
		}
		for _, resource := range change.Resources {
			add(resource)
		}
	}
	return result
}

// ValidateClear refuses clearing that would remove more than the changed
// values, bypass a framework lifecycle, or leave a required value empty.
func ValidateClear(changes []Change) error {
	for _, change := range changes {
		address := Description(Reports([]Change{change})[0])
		if change.Payload != nil {
			address = strings.TrimSuffix(address, " embedded field "+change.Payload.Path)
			return fmt.Errorf("embedded field %s of %s changes kind; clearing would remove every complete %s value, not only that embedded field; restore the previous embedded field kind", change.Payload.Path, address, address)
		}
		if change.Before.Localized != change.After.Localized {
			return fmt.Errorf("changing localization of %s requires a reviewed migration", address)
		}
		resources := change.Resources
		if change.Block == "" {
			resources = []schema.Collection{change.Resource}
		}
		for _, resource := range resources {
			if resource.Auth != nil || resource.Upload != nil {
				return fmt.Errorf("clearing changed fields on auth or upload resource %q requires an application-owned migration", resource.Slug)
			}
		}
		if !stored(change.After) {
			return fmt.Errorf("changing stored field %s to a non-stored field requires a reviewed migration", address)
		}
		// A required top-level, non-localized field is a NOT NULL column on
		// PostgreSQL and a required value on every adapter: clearing it would
		// leave documents that the new schema cannot store.
		if change.TopLevel() && !change.After.Localized && change.After.Required {
			return fmt.Errorf("%s stays required after the change, so clearing would leave required values empty; make the new field optional for the clearing save, then require it again after entering values", address)
		}
	}
	return nil
}

// Description is a stable address without any application value.
func Description(report Report) string {
	address := strings.Join([]string{string(report.ResourceSlug), report.Path}, ".")
	if report.Block != "" {
		address = "block " + report.Block + "." + report.Path
	}
	if report.Payload != "" {
		return address + " embedded field " + report.Payload
	}
	return address
}
