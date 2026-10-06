// Package requiredfield owns the stored-value audit that admits a field
// becoming required. Planners detect the fields a schema change newly
// requires, and migration runners and development synchronization inspect
// stored documents with the same rule, so a missing value stops the schema
// change instead of surfacing later in a read or an unrelated edit.
//
// A block definition is compared once: a field it newly requires is one
// requirement, checked in every stored block of the definition, however many
// placements the block graph gives it.
package requiredfield

import (
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Code is the stable error code of a refused required-field change.
const Code = "RIDU_REQUIRED_VALUES_MISSING"

// RiskCode identifies the planned audit in a migration's review findings.
const RiskCode = "RIDU_REQUIRED_FIELD_AUDIT"

// StepName labels the planned audit step.
const StepName = "audit stored values of newly required fields"

// Step is one segment of a requirement's path. Name is a field name, or a
// block slug: after a Blocks container's name it selects that container's
// stored blocks of the slug, and with Descend it finds the slug's stored blocks
// at any depth beneath the preceding segment.
type Step struct {
	Name    string
	Descend bool
}

// Scope limits a requirement's first descent to blocks stored beneath a
// localized ancestor, or to the others. A definition is compared once for each
// of the two, and its fields' localization differs only between them.
type Scope string

const (
	ScopeAll         Scope = ""
	ScopeLocalized   Scope = migration.RequiredScopeLocalized
	ScopeUnlocalized Scope = migration.RequiredScopeUnlocalized
)

// Requirement is one stored field that the after schema requires where the
// before schema did not. An anchored requirement names a resource and a path
// from its fields; an unanchored one starts with a descent to a block
// definition and holds in every stored block of it.
type Requirement struct {
	// Resource is the anchor, or the zero value for an unanchored requirement.
	Resource schema.Collection
	// Steps lead from the anchor to Field, whose name is the last step.
	Steps []Step
	// Field is the required field as the after schema declares it.
	Field schema.Field
	Scope Scope
	graph *blockgraph.Graph
}

// Anchored reports whether the requirement names one resource.
func (requirement Requirement) Anchored() bool {
	return requirement.Resource.ID != ""
}

// TopLevel reports a requirement on a resource's own top-level field.
func (requirement Requirement) TopLevel() bool {
	return requirement.Anchored() && len(requirement.Steps) == 1
}

// Path is the requirement's canonical path from its anchor.
func (requirement Requirement) Path() string {
	parts := make([]string, 0, len(requirement.Steps)+1)
	for _, step := range requirement.Steps {
		if step.Descend {
			parts = append(parts, "**")
		}
		parts = append(parts, step.Name)
	}
	return strings.Join(parts, ".")
}

// Address is the requirement's public address: posts.seo.title for a resource
// field, or block hero.heading for a field of every hero block.
func (requirement Requirement) Address() string {
	if requirement.Anchored() {
		return string(requirement.Resource.Slug) + "." + requirement.Path()
	}
	address := "block " + strings.TrimPrefix(requirement.Path(), "**.")
	switch requirement.Scope {
	case ScopeLocalized:
		address += " (beneath a localized field)"
	case ScopeUnlocalized:
		address += " (outside localized fields)"
	}
	return address
}

// Renames binds confirmed renames so a renamed field keeps its requiredness.
// Resources maps a before resource ID to its after ID; Fields maps an after
// field ID to the before field whose content it keeps, and Blocks does the
// same for the definition-relative IDs of each block definition's fields.
type Renames struct {
	Resources map[schema.StableID]schema.StableID
	Fields    map[schema.StableID]schema.Field
	Blocks    map[string]map[schema.StableID]schema.Field
}

// Detect lists the stored fields that after requires and before did not:
// fields made required, required fields added to existing resources or
// containers, required fields whose stored kind or localization changed, and
// every required field of a resource that stops deferring drafts. A new
// resource, field container or block type holds no stored values yet, so its
// fields need no audit. Embedded plugin payloads keep their own evolution rules.
//
// Each resource's own fields are compared, then each block definition placed
// before and after the change beneath a surviving resource, once in each of
// its localization scopes. Blocks stored beneath a container whose stored
// shape changes are all newly constrained; that container contributes one
// requirement per required field of each definition placed beneath it.
func Detect(before, after schema.Snapshot, renames Renames) []Requirement {
	previous, current := blockgraph.New(before), blockgraph.New(after)
	var result []Requirement
	for _, pair := range blockgraph.Survivors(previous, current, renames.Resources) {
		fresh := defersDrafts(pair.Before.Resource) && !defersDrafts(pair.After.Resource)
		detector := detector{before: previous, after: current, renames: renames.Fields, anchor: pair.After.Resource, result: &result}
		detector.fields(pair.Before.Resource.Fields, pair.After.Resource.Fields, nil, fresh, false)
	}
	shared := blockgraph.SortedKeys(blockgraph.Shared(previous, current, renames.Resources, false))
	for index := 0; index < len(shared); {
		slug := shared[index].Slug
		var scopes [][]Requirement
		var keys []blockgraph.Key
		for ; index < len(shared) && shared[index].Slug == slug; index++ {
			key := shared[index]
			beforeView, _ := previous.View(key)
			afterView, _ := current.View(key)
			var found []Requirement
			detector := detector{before: previous, after: current, renames: renames.Blocks[slug], result: &found}
			detector.fields(beforeView.ResolvedFields(), afterView.ResolvedFields(), []Step{{Name: slug, Descend: true}}, false, key.Localized)
			scopes, keys = append(scopes, found), append(keys, key)
		}
		result = append(result, mergeScopes(scopes, keys)...)
	}
	for index := range result {
		result[index].graph = current
	}
	return result
}

// mergeScopes combines a definition's requirements found in each placed
// localization scope. A requirement found in only one of two scopes holds
// only there.
func mergeScopes(scopes [][]Requirement, keys []blockgraph.Key) []Requirement {
	if len(scopes) == 1 {
		return scopes[0]
	}
	paths := func(requirements []Requirement) map[string]bool {
		result := make(map[string]bool, len(requirements))
		for _, requirement := range requirements {
			result[requirement.Path()] = true
		}
		return result
	}
	first, second := paths(scopes[0]), paths(scopes[1])
	scopeOf := func(key blockgraph.Key) Scope {
		if key.Localized {
			return ScopeLocalized
		}
		return ScopeUnlocalized
	}
	var merged []Requirement
	for _, requirement := range scopes[0] {
		if !second[requirement.Path()] {
			requirement.Scope = scopeOf(keys[0])
		}
		merged = append(merged, requirement)
	}
	for _, requirement := range scopes[1] {
		if !first[requirement.Path()] {
			requirement.Scope = scopeOf(keys[1])
			merged = append(merged, requirement)
		}
	}
	return merged
}

type detector struct {
	before, after *blockgraph.Graph
	renames       map[schema.StableID]schema.Field
	anchor        schema.Collection
	result        *[]Requirement
}

func (detector detector) emit(steps []Step, field schema.Field) {
	*detector.result = append(*detector.result, Requirement{Resource: detector.anchor, Steps: steps, Field: field})
}

func (detector detector) fields(before, after []schema.Field, steps []Step, fresh, localized bool) {
	byName := make(map[string]schema.Field, len(before))
	for _, field := range before {
		if stored(field) {
			byName[field.Name] = field
		}
	}
	for _, field := range after {
		if !stored(field) {
			continue
		}
		previous, found := detector.renames[field.ID]
		if !found {
			previous, found = byName[field.Name]
		}
		path := appendStep(steps, Step{Name: field.Name})
		changed := !found || storageChanged(previous, field)
		if field.Required && (fresh || changed || !previous.Required) {
			detector.emit(path, field)
		}
		if !found {
			continue
		}
		fresh := fresh || changed
		childLocalized := localized || field.Localized
		if field.Nested != nil && previous.Nested != nil {
			detector.fields(previous.Nested.ResolvedFields(), field.Nested.ResolvedFields(), path, fresh, childLocalized)
		}
		if field.Blocks != nil && previous.Blocks != nil && fresh {
			detector.descendants(previous, field, path, localized)
		}
	}
}

// descendants lists every required field of each definition placed beneath a
// container whose blocks are all newly constrained: the definitions the
// container placed before the change and still places after it, at any depth.
func (detector detector) descendants(previous, current schema.Field, steps []Step, localized bool) {
	before, _ := blockgraph.FieldSelections(previous, localized)
	after, _ := blockgraph.FieldSelections(current, localized)
	placed := detector.before.Closure(before, false)
	slugs := make(map[string]blockgraph.Key)
	for key := range detector.after.Closure(after, false) {
		if _, kept := slugs[key.Slug]; placed[key] && !kept {
			slugs[key.Slug] = key
		}
	}
	ordered := make([]string, 0, len(slugs))
	for slug := range slugs {
		ordered = append(ordered, slug)
	}
	sort.Strings(ordered)
	for _, slug := range ordered {
		view, _ := detector.after.View(slugs[slug])
		detector.required(view.ResolvedFields(), appendStep(steps, Step{Name: slug, Descend: true}))
	}
}

// required lists the required stored fields of one definition, through its own
// groups and arrays; the definitions it places are listed on their own.
func (detector detector) required(fields []schema.Field, steps []Step) {
	for _, field := range fields {
		if !stored(field) {
			continue
		}
		path := appendStep(steps, Step{Name: field.Name})
		if field.Required {
			detector.emit(path, field)
		}
		if field.Nested != nil {
			detector.required(field.Nested.ResolvedFields(), path)
		}
	}
}

func appendStep(steps []Step, step Step) []Step {
	return append(append(make([]Step, 0, len(steps)+1), steps...), step)
}

func defersDrafts(resource schema.Collection) bool {
	return resource.Versions != nil && resource.Versions.Drafts
}

func stored(field schema.Field) bool {
	return field.Category != schema.FieldCategoryPresentation && field.Type != schema.FieldTypeVirtual &&
		field.Type != schema.FieldTypeJoin && field.Type != schema.FieldTypeUI
}

// storageChanged reports a change in how stored values are read, after which
// existing values cannot prove the field holds a value.
func storageChanged(before, after schema.Field) bool {
	return before.Type != after.Type || before.Localized != after.Localized || (before.List == nil) != (after.List == nil) ||
		hasMany(before) != hasMany(after)
}

func hasMany(field schema.Field) bool {
	return field.Select != nil && field.Select.HasMany || field.Relationship != nil && field.Relationship.HasMany ||
		field.Upload != nil && field.Upload.HasMany
}

// Addresses returns the audit step payload addresses of requirements.
func Addresses(requirements []Requirement) []migration.RequiredFieldAddress {
	addresses := make([]migration.RequiredFieldAddress, len(requirements))
	for index, requirement := range requirements {
		addresses[index] = migration.RequiredFieldAddress{ResourceID: requirement.Resource.ID, Path: requirement.Path(), Scope: string(requirement.Scope)}
	}
	return addresses
}

// Payload returns the audit step payload of requirements.
func Payload(requirements []Requirement) migration.AuditRequiredValuesPayload {
	return migration.AuditRequiredValuesPayload{Fields: Addresses(requirements)}
}

// Risks returns one review finding per requirement.
func Risks(requirements []Requirement) []migration.Risk {
	risks := make([]migration.Risk, len(requirements))
	for index, requirement := range requirements {
		risks[index] = migration.Risk{
			Code: RiskCode, Level: migration.RiskWarning,
			Message: fmt.Sprintf("require %s; the migration audits stored documents after its data transforms and fails with %s if any document that must be complete has no value", requirement.Address(), Code),
		}
	}
	return risks
}

// Resolve locates audit payload addresses in an artifact's after manifest.
func Resolve(after schema.Snapshot, addresses []migration.RequiredFieldAddress) ([]Requirement, error) {
	graph := blockgraph.New(after)
	requirements := make([]Requirement, 0, len(addresses))
	for _, address := range addresses {
		requirement := Requirement{Scope: Scope(address.Scope), graph: graph}
		var fields []schema.Field
		if address.ResourceID != "" {
			resource, found := findResource(after, address.ResourceID)
			if !found {
				return nil, fmt.Errorf("required-value audit names resource %s, which the after manifest does not contain", address.ResourceID)
			}
			requirement.Resource, fields = resource, resource.Fields
		}
		steps, field, err := resolvePath(graph, fields, address.Path)
		if err != nil {
			return nil, fmt.Errorf("required-value audit names %s: %w", strings.TrimPrefix(string(requirement.Resource.Slug)+"."+address.Path, "."), err)
		}
		if !requirement.Anchored() && !steps[0].Descend {
			return nil, fmt.Errorf("required-value audit names %s without a resource or a block definition", address.Path)
		}
		requirement.Steps, requirement.Field = steps, field
		requirements = append(requirements, requirement)
	}
	return requirements, nil
}

func findResource(snapshot schema.Snapshot, id schema.StableID) (schema.Collection, bool) {
	for _, resources := range [][]schema.Collection{snapshot.Collections, snapshot.Globals} {
		for _, resource := range resources {
			if resource.ID == id {
				return resource, true
			}
		}
	}
	return schema.Collection{}, false
}

// resolvePath parses a canonical path pattern against the after schema and
// returns its steps and the required field it ends at.
func resolvePath(graph *blockgraph.Graph, fields []schema.Field, path string) ([]Step, schema.Field, error) {
	segments := strings.Split(path, ".")
	var steps []Step
	var container *schema.Field
	for index := 0; index < len(segments); index++ {
		segment := segments[index]
		if segment == "**" {
			index++
			if index >= len(segments)-1 {
				return nil, schema.Field{}, fmt.Errorf("the path ends at a block without a field")
			}
			slug := segments[index]
			view, found := definitionView(graph, slug)
			if !found {
				return nil, schema.Field{}, fmt.Errorf("block type %q is not placed", slug)
			}
			if container != nil && container.Blocks == nil {
				return nil, schema.Field{}, fmt.Errorf("field %q has no blocks", container.Name)
			}
			steps = append(steps, Step{Name: slug, Descend: true})
			fields, container = view.ResolvedFields(), nil
			continue
		}
		if container != nil && container.Blocks != nil {
			view, found := container.Blocks.Definition(segment)
			if !found || index == len(segments)-1 {
				return nil, schema.Field{}, fmt.Errorf("block type %q does not exist or has no field", segment)
			}
			steps = append(steps, Step{Name: segment})
			fields, container = view.ResolvedFields(), nil
			continue
		}
		if container != nil {
			fields = container.Nested.ResolvedFields()
		}
		field, found := storedField(fields, segment)
		if !found {
			return nil, schema.Field{}, fmt.Errorf("field %q does not exist", segment)
		}
		steps = append(steps, Step{Name: segment})
		if index == len(segments)-1 {
			if !field.Required {
				return nil, schema.Field{}, fmt.Errorf("the field is not a required stored field")
			}
			return steps, field, nil
		}
		if field.Nested == nil && field.Blocks == nil {
			return nil, schema.Field{}, fmt.Errorf("field %q has no stored children", segment)
		}
		container = &field
	}
	return nil, schema.Field{}, fmt.Errorf("the path is empty")
}

// definitionView returns a placed view of slug; both of a definition's views
// declare the same fields.
func definitionView(graph *blockgraph.Graph, slug string) (schema.BlockType, bool) {
	for _, localized := range []bool{false, true} {
		if view, found := graph.View(blockgraph.Key{Slug: slug, Localized: localized}); found {
			return view, true
		}
	}
	return schema.BlockType{}, false
}

func storedField(fields []schema.Field, name string) (schema.Field, bool) {
	for _, field := range fields {
		if field.Name == name && stored(field) {
			return field, true
		}
	}
	return schema.Field{}, false
}

// Group is the requirements of one resource with the distinct top-level
// fields an audit must read to inspect them.
type Group struct {
	Resource     schema.Collection
	Requirements []Requirement
	Roots        []schema.Field
}

// Groups orders requirements by resource, preserving their order. An
// unanchored requirement joins the group of every resource that places its
// definition, reading the top-level fields that can hold its blocks.
func Groups(requirements []Requirement) []Group {
	grouped := groups(requirements)
	result := make([]Group, len(grouped))
	for index, group := range grouped {
		result[index] = group.Group
		for _, requirement := range group.indexes {
			result[index].Requirements = append(result[index].Requirements, requirements[requirement])
		}
	}
	return result
}

type indexedGroup struct {
	Group
	indexes []int
}

func groups(requirements []Requirement) []indexedGroup {
	var groups []indexedGroup
	positions := make(map[schema.StableID]int)
	add := func(resource schema.Collection, requirement int, roots []schema.Field) {
		position, exists := positions[resource.ID]
		if !exists {
			position = len(groups)
			positions[resource.ID] = position
			groups = append(groups, indexedGroup{Group: Group{Resource: resource}})
		}
		group := &groups[position]
		group.indexes = append(group.indexes, requirement)
		for _, root := range roots {
			known := false
			for _, candidate := range group.Roots {
				known = known || candidate.Name == root.Name
			}
			if !known {
				group.Roots = append(group.Roots, root)
			}
		}
	}
	for index, requirement := range requirements {
		if requirement.Anchored() {
			if requirement.Steps[0].Descend {
				add(requirement.Resource, index, requirement.blockRoots(requirement.Resource))
				continue
			}
			root, _ := storedField(requirement.Resource.Fields, requirement.Steps[0].Name)
			add(requirement.Resource, index, []schema.Field{root})
			continue
		}
		for _, resource := range requirement.graph.Resources() {
			if roots := requirement.blockRoots(resource.Resource); len(roots) != 0 {
				add(resource.Resource, index, roots)
			}
		}
	}
	return groups
}

// blockRoots returns the top-level fields of resource that can hold blocks of
// the requirement's first descent.
func (requirement Requirement) blockRoots(resource schema.Collection) []schema.Field {
	node, found := requirement.graph.ResourceOf(resource)
	if !found {
		return nil
	}
	slugs := map[string]bool{requirement.Steps[0].Name: true}
	var roots []schema.Field
	for _, root := range node.Roots {
		if stored(root.Field) && requirement.graph.RootPlaces(root, slugs) {
			roots = append(roots, root.Field)
		}
	}
	return roots
}

// Missing reports whether a value fails a required field the way write
// validation does: absent or null, an empty string for string-valued kinds,
// and an empty list for list-valued kinds.
func Missing(field schema.Field, value store.Value) bool {
	if value.IsZero() || value.Kind() == store.ValueNull {
		return true
	}
	switch value.Kind() {
	case store.ValueString:
		switch field.Type {
		case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeCode, schema.FieldTypeEmail, schema.FieldTypeDate,
			schema.FieldTypeSelect, schema.FieldTypeRadio, schema.FieldTypeRelationship, schema.FieldTypeUpload:
			text, _ := value.StringValue()
			return text == ""
		}
	case store.ValueList:
		switch field.Type {
		case schema.FieldTypeSelect, schema.FieldTypeTextList, schema.FieldTypeNumberList, schema.FieldTypeRelationship,
			schema.FieldTypeUpload, schema.FieldTypeArray, schema.FieldTypeBlocks:
			return value.Len() == 0
		}
	}
	return false
}

// inspection checks one requirement in one stored document. A localized value
// is checked per translation: an untranslated locale is optional, as it is for
// writes, but a stored empty translation is missing, and so is a value with no
// translation at all. Containers absent from the document hold nothing.
type inspection struct {
	requirement Requirement
	walkers     map[string]*blockgraph.Walker
	report      func(locale schema.LocaleCode, everyLocale bool)
}

func (inspection *inspection) walker(slug string) *blockgraph.Walker {
	walker, found := inspection.walkers[slug]
	if !found {
		walker = inspection.requirement.graph.Walker(map[string]bool{slug: true})
		inspection.walkers[slug] = walker
	}
	return walker
}

// object checks steps beneath one stored object of fields.
func (inspection *inspection) object(fields []schema.Field, lookup func(string) (store.Value, bool), steps []Step, locale schema.LocaleCode, first bool) {
	step := steps[0]
	if step.Descend {
		_ = inspection.walker(step.Name).Visit(fields, lookup, locale, func(row blockgraph.Row, item store.Value) error {
			if inspection.inScope(row, first) {
				inspection.object(row.Block.ResolvedFields(), item.Lookup, steps[1:], row.Locale, false)
			}
			return nil
		})
		return
	}
	field, found := storedField(fields, step.Name)
	if !found {
		return
	}
	value, _ := lookup(field.Name)
	if len(steps) == 1 {
		inspection.leaf(field, value, locale)
		return
	}
	if field.Localized && locale == "" {
		field.Localized = false
		for code, translation := range translations(value) {
			inspection.value(field, translation, steps[1:], code, first)
		}
		return
	}
	inspection.value(field, value, steps[1:], locale, first)
}

// value checks steps beneath one stored container value.
func (inspection *inspection) value(field schema.Field, value store.Value, steps []Step, locale schema.LocaleCode, first bool) {
	switch {
	case field.Blocks != nil && steps[0].Descend:
		_ = inspection.walker(steps[0].Name).VisitField(field, value, locale, func(row blockgraph.Row, item store.Value) error {
			if inspection.inScope(row, first) {
				inspection.object(row.Block.ResolvedFields(), item.Lookup, steps[1:], row.Locale, false)
			}
			return nil
		})
	case field.Blocks != nil:
		view, found := field.Blocks.Definition(steps[0].Name)
		if !found || value.Kind() != store.ValueList {
			return
		}
		for item := range value.Elements() {
			if item.Kind() != store.ValueObject {
				continue
			}
			if block, _ := item.Get("blockType").StringValue(); block == view.Slug {
				inspection.object(view.ResolvedFields(), item.Lookup, steps[1:], locale, first)
			}
		}
	case field.Nested != nil && field.Type == schema.FieldTypeGroup:
		if value.Kind() == store.ValueObject {
			inspection.object(field.Nested.ResolvedFields(), value.Lookup, steps, locale, first)
		}
	case field.Nested != nil:
		if value.Kind() != store.ValueList {
			return
		}
		for item := range value.Elements() {
			if item.Kind() == store.ValueObject {
				inspection.object(field.Nested.ResolvedFields(), item.Lookup, steps, locale, first)
			}
		}
	}
}

func (inspection *inspection) inScope(row blockgraph.Row, first bool) bool {
	switch {
	case !first || inspection.requirement.Scope == ScopeAll:
		return true
	case inspection.requirement.Scope == ScopeLocalized:
		return row.Locale != ""
	default:
		return row.Locale == ""
	}
}

func (inspection *inspection) leaf(field schema.Field, value store.Value, locale schema.LocaleCode) {
	if !field.Localized || locale != "" {
		if Missing(field, value) {
			inspection.report(locale, false)
		}
		return
	}
	translated := false
	var empty []schema.LocaleCode
	for code, translation := range translations(value) {
		translated = true
		if Missing(field, translation) {
			empty = append(empty, code)
		}
	}
	if !translated {
		inspection.report("", true)
		return
	}
	sort.Slice(empty, func(left, right int) bool { return empty[left] < empty[right] })
	for _, code := range empty {
		inspection.report(code, false)
	}
}

// translations yields the stored translations of a locale-keyed value. A null
// translation is an absent one, as PostgreSQL stores it.
func translations(value store.Value) func(func(schema.LocaleCode, store.Value) bool) {
	return func(yield func(schema.LocaleCode, store.Value) bool) {
		if value.Kind() != store.ValueObject {
			return
		}
		for code, translation := range value.Entries() {
			if translation.IsZero() || translation.Kind() == store.ValueNull {
				continue
			}
			if !yield(schema.LocaleCode(code), translation) {
				return
			}
		}
	}
}
