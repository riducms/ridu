package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Graph is the private companion to a manifest. Authoring nodes retain their
// executable attachments here; both remain private. Application construction
// lowers the companion once; requests never query it.
//
// Each resource's own fields and each block definition's fields are recorded
// once. A definition is a scope of its own, with definition-relative paths,
// however many containers select it: the engine binds a definition's field
// once and resolves each placement from the document it walks, so the graph
// and everything lowered from it follow definitions, not placements.
type Graph struct {
	occurrences []*Occurrence
	bindings    map[string]field.View
}

// Occurrence is a field of a resource or of a block definition, not a concrete
// runtime row. ResourceKind is "collection", "global" or "block"; a block
// definition's occurrences name its slug as Resource, and their paths are
// relative to the definition. Repeated axes retain the identity property and
// case within that scope; LocaleOwner identifies an exact-locale dimension the
// scope declares. A placement additionally inherits its container's locale
// scope, which the engine reads from the document walk.
type Occurrence struct {
	ID           string                 `json:"id"`
	SchemaID     schema.StableID        `json:"schemaId,omitempty"`
	ResourceKind string                 `json:"resourceKind"`
	Resource     string                 `json:"resource"`
	Name         string                 `json:"name,omitempty"`
	Kind         field.Kind             `json:"kind,omitempty"`
	Boundary     string                 `json:"boundary"`
	Stored       bool                   `json:"stored"`
	AuthoredPath string                 `json:"authoredPath"`
	ResolvedPath string                 `json:"resolvedPath"`
	ParentID     string                 `json:"parentId,omitempty"`
	ScopeID      string                 `json:"scopeId"`
	LocaleOwner  string                 `json:"localeOwner,omitempty"`
	Repeated     []RepeatedAxis         `json:"repeated,omitempty"`
	Provenance   []string               `json:"provenance,omitempty"`
	References   []ReferenceBinding     `json:"references,omitempty"`
	Extensions   map[string]store.Value `json:"extensions,omitempty"`
}

// BlockResource is the ResourceKind of a block definition's occurrences.
const BlockResource = "block"

type RepeatedAxis struct {
	OccurrenceID string `json:"occurrenceId"`
	Identity     string `json:"identity"`
	Case         string `json:"case,omitempty"`
}

// ReferenceBinding is a visibility condition's bound target. A root reference
// from a block definition binds once for each resource that places the
// definition, since each resolves the path against its own root.
type ReferenceBinding struct {
	Policy       string               `json:"policy"`
	Scope        field.ReferenceScope `json:"scope"`
	Path         string               `json:"path"`
	TargetID     string               `json:"targetId"`
	ResolvedPath string               `json:"resolvedPath"`
}

func (g Graph) Occurrences() []Occurrence {
	result := make([]Occurrence, len(g.occurrences))
	for i, occurrence := range g.occurrences {
		result[i] = *occurrence
		result[i].Repeated = slices.Clone(result[i].Repeated)
		result[i].Provenance = slices.Clone(result[i].Provenance)
		result[i].References = slices.Clone(result[i].References)
		result[i].Extensions = maps.Clone(result[i].Extensions)
	}
	return result
}

// Binding returns an immutable view, never an executable dispatch table. Only
// fields with executable behavior, a visibility condition, or a reference
// target retain a view; the Occurrence alone describes every other field.
func (g Graph) Binding(id string) (field.View, bool) { d, ok := g.bindings[id]; return d, ok }

// ResolveGraph binds field-owned selectors before lowering the manifest and
// retains their symbolic declarations in immutable occurrence views.
func ResolveGraph(input Input) (schema.Manifest, Graph, error) {
	var bindErr error
	input, bindErr = bindInputBlocks(input)
	if bindErr != nil {
		return schema.Manifest{}, Graph{}, bindErr
	}
	b := graphBuilder{graph: Graph{bindings: make(map[string]field.View)}, scopes: make(map[string]map[string]string), byID: make(map[string]int), byPath: make(map[graphPathKey]int), places: make(map[graphOwner][]string)}
	for i, collection := range input.Collections {
		if collection.Upload {
			fields, err := UploadFields(collection.Fields, fmt.Sprintf("collections[%d].fields", i))
			if err != nil {
				return schema.Manifest{}, Graph{}, err
			}
			collection.Fields = fields
			input.Collections[i].Fields = collection.Fields
		}
		b.resource("collection", string(collection.Slug), fmt.Sprintf("collections[%d].fields", i), collection.Fields)
	}
	for i, global := range input.Globals {
		b.resource("global", string(global.Slug), fmt.Sprintf("globals[%d].fields", i), global.Fields)
	}
	// Binding interned every inline declaration into the registry, so each
	// definition is recorded exactly once, used or not, as its own scope.
	for _, block := range input.Blocks {
		b.definition(block)
	}
	b.validateGlobalDefinitions()
	b.bindReferences()
	// Symbolic and policy errors retain authored occurrence provenance.
	// Aggregate duplicate-field diagnostics come from static schema resolution.
	var graphIssues []schema.Issue
	for _, issue := range b.issues {
		if issue.Code != "duplicate_graph_occurrence" && issue.Code != "duplicate_graph_child" {
			graphIssues = append(graphIssues, issue)
		}
	}
	if len(graphIssues) != 0 {
		return schema.Manifest{}, Graph{}, schema.NewValidationError(graphIssues)
	}
	manifest, resolved, err := resolveBoundSnapshot(input)
	if err != nil {
		return schema.Manifest{}, Graph{}, err
	}
	if len(b.issues) != 0 {
		return schema.Manifest{}, Graph{}, schema.NewValidationError(b.issues)
	}
	b.schemaIDs(resolved)
	return manifest, b.graph, nil
}

type graphPosition struct {
	resourceKind, resource, rootScope, scope, parent, path, localeOwner string
	repeated                                                            []RepeatedAxis
}

func (p graphPosition) owner() graphOwner { return graphOwner{kind: p.resourceKind, slug: p.resource} }

type graphPathKey struct{ kind, resource, path string }

// graphOwner is a resource or block definition: a scope whose own fields the
// graph records.
type graphOwner struct{ kind, slug string }

type graphBuilder struct {
	graph  Graph
	scopes map[string]map[string]string
	byID   map[string]int
	byPath map[graphPathKey]int
	// resources lists collections and globals in declaration order.
	resources []graphOwner
	// places lists, in schema order, the block definitions that each resource's
	// or definition's own fields select.
	places map[graphOwner][]string
	issues []schema.Issue
}

func occurrenceID(kind, resource, boundary, path string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + resource + "\x00" + boundary + "\x00" + path))
	return "occ_" + hex.EncodeToString(sum[:])
}

func (b *graphBuilder) resource(kind, resource, authored string, definitions field.Fields) {
	scope := occurrenceID(kind, resource, "resource", "")
	b.scopes[scope] = make(map[string]string)
	b.resources = append(b.resources, graphOwner{kind: kind, slug: resource})
	b.fields(definitions, authored, graphPosition{resourceKind: kind, resource: resource, rootScope: scope, scope: scope})
}

// definition records a block definition's fields once, as a scope of their
// own: sibling conditions resolve within it, wherever it is placed.
func (b *graphBuilder) definition(block field.Block) {
	scope := occurrenceID(BlockResource, block.Slug, "resource", "")
	b.scopes[scope] = make(map[string]string)
	p := graphPosition{resourceKind: BlockResource, resource: block.Slug, rootScope: scope, scope: scope}
	authored := "blocks." + block.Slug + ".fields"
	b.fields(block.Fields, authored, p)
	// Manifest lowering adds this direct child when it was not authored.
	// Bind sibling conditions against the same scope before lowering runs.
	if b.scopes[scope]["blockName"] == "" {
		b.node(defaultBlockNameField(), fmt.Sprintf("%s[%d]", authored, len(block.Fields)), p)
	}
}

func (b *graphBuilder) fields(definitions field.Fields, authored string, p graphPosition) {
	for i, d := range definitions {
		b.node(field.Snapshot(d), fmt.Sprintf("%s[%d]", authored, i), p)
	}
}

func graphBoundary(d field.View) (string, bool) {
	switch d.Kind() {
	case field.KindGroup:
		if d.IsNamedTab() {
			return "named_tab", true
		}
		return "stored_object", true
	case field.KindArray:
		return "array", true
	case field.KindBlocks:
		return "blocks", true
	case field.KindRelationship, field.KindUpload:
		return "stored_reference", true
	case field.KindTabs, field.KindRow, field.KindCollapsible, field.KindUI:
		return "layout", false
	case field.KindVirtual, field.KindJoin:
		return "output", false
	case field.KindPlugin:
		return "plugin_value", true
	default:
		return "stored_scalar", true
	}
}

func (b *graphBuilder) add(name string, kind field.Kind, boundary string, stored bool, authored, path string, p graphPosition, provenance []string) string {
	identityPath := path
	if boundary == "layout" || boundary == "unnamed_tab" {
		identityPath = authored
	}
	id := occurrenceID(p.resourceKind, p.resource, boundary, identityPath)
	if prior, exists := b.byID[id]; exists {
		b.issues = append(b.issues, schema.Issue{Code: "duplicate_graph_occurrence", Path: authored, Message: fmt.Sprintf("resolved field %q conflicts with %s", path, b.graph.occurrences[prior].AuthoredPath)})
		return id
	}
	b.byID[id] = len(b.graph.occurrences)
	if stored || boundary == "output" {
		b.byPath[graphPathKey{p.resourceKind, p.resource, path}] = len(b.graph.occurrences)
	}
	// Sibling occurrences share their scope's repeated axes; the builder only
	// extends copies, and Occurrences detaches every slice for callers.
	b.graph.occurrences = append(b.graph.occurrences, &Occurrence{ID: id, ResourceKind: p.resourceKind, Resource: p.resource, Name: name, Kind: kind, Boundary: boundary, Stored: stored, AuthoredPath: authored, ResolvedPath: path, ParentID: p.parent, ScopeID: p.scope, LocaleOwner: p.localeOwner, Repeated: p.repeated, Provenance: provenance})
	if stored || boundary == "output" {
		if b.scopes[p.scope] == nil {
			b.scopes[p.scope] = make(map[string]string)
		}
		if prior, exists := b.scopes[p.scope][name]; exists {
			b.issues = append(b.issues, schema.Issue{Code: "duplicate_graph_child", Path: authored, Message: fmt.Sprintf("field %q in data scope conflicts with %s", path, b.graph.occurrences[b.byID[prior]].AuthoredPath)})
		} else {
			b.scopes[p.scope][name] = id
		}
	}
	return id
}

func appendGraphPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func (b *graphBuilder) node(d field.View, authored string, p graphPosition) {
	boundary, stored := graphBoundary(d)
	path := p.path
	if stored || boundary == "output" {
		path = appendGraphPath(path, d.Name())
	}
	id := b.add(d.Name(), d.Kind(), boundary, stored, authored, path, p, d.Provenance())
	admin := d.AdminPolicy()
	// Keep a view only where lowering or reference binding reads it.
	if d.HasBehavior() || !admin.VisibleWhen.IsZero() || boundary == "stored_reference" {
		b.graph.bindings[id] = d
	}
	b.graph.occurrences[b.byID[id]].Extensions = admin.Extensions
	b.validatePolicies(d, authored, path, boundary, p)
	if d.Localized() && p.localeOwner == "" {
		p.localeOwner = id
		b.graph.occurrences[b.byID[id]].LocaleOwner = id
	}
	p.parent = id
	if stored {
		p.path = path
	}
	switch d.Kind() {
	case field.KindGroup, field.KindArray:
		p.scope = id
		b.scopes[id] = make(map[string]string)
		if d.Kind() == field.KindArray {
			p.repeated = append(slices.Clone(p.repeated), RepeatedAxis{OccurrenceID: id, Identity: "_key"})
		}
		b.fields(d.Fields(), authored+".fields", p)
	case field.KindBlocks:
		// A container records which definitions it places. Their fields are
		// recorded once, by definition, never beneath each placement.
		for _, block := range d.Blocks() {
			b.place(p, block.Slug)
		}
	case field.KindTabs:
		if d.IsUnnamedTab() {
			b.fields(d.Fields(), authored+".fields", p)
			break
		}
		for i, tab := range d.Fields() {
			b.node(field.Snapshot(tab), fmt.Sprintf("%s.tabs[%d]", authored, i), p)
		}
	case field.KindRow, field.KindCollapsible:
		b.fields(d.Fields(), authored+".fields", p)
	case field.KindPlugin:
		for i, tree := range d.EmbeddedTrees() {
			treePath := fmt.Sprintf("%s.embeddedTrees[%d]", authored, i)
			q := p
			q.path = appendGraphPath(p.path, tree.Key)
			q.parent = b.add(tree.Key, "", "embedded_tree", false, treePath, q.path, p, d.Provenance())
			for j, c := range tree.Cases {
				casePath := fmt.Sprintf("%s.cases[%d]", treePath, j)
				r := q
				r.path = appendGraphPath(q.path, c.TagValue)
				r.parent = b.add(c.TagValue, "", "embedded_case", false, casePath, r.path, q, d.Provenance())
				for _, block := range c.Types {
					b.place(p, block.Slug)
				}
			}
		}
	}
}

// place records that p's resource or definition selects the definition slug.
func (b *graphBuilder) place(p graphPosition, slug string) {
	owner := p.owner()
	if !slices.Contains(b.places[owner], slug) {
		b.places[owner] = append(b.places[owner], slug)
	}
}

// reachable lists the definitions owner places directly or through other
// definitions, in the order a depth-first walk of its fields first meets them.
// The walk visits each definition once, so its cost follows definitions.
func (b *graphBuilder) reachable(owner graphOwner) []string {
	var order []string
	seen := map[string]bool{}
	var visit func(graphOwner)
	visit = func(current graphOwner) {
		for _, slug := range b.places[current] {
			if !seen[slug] {
				seen[slug] = true
				order = append(order, slug)
				visit(graphOwner{kind: BlockResource, slug: slug})
			}
		}
	}
	visit(owner)
	return order
}

// placedBy lists, for each definition, the resources that place it.
func (b *graphBuilder) placedBy() map[string][]graphOwner {
	result := map[string][]graphOwner{}
	for _, resource := range b.resources {
		for _, slug := range b.reachable(resource) {
			result[slug] = append(result[slug], resource)
		}
	}
	return result
}

// validateGlobalDefinitions applies the global field policy to every
// definition a global places, once per definition, however many globals and
// placements reach it.
func (b *graphBuilder) validateGlobalDefinitions() {
	inGlobal := map[string]bool{}
	for _, resource := range b.resources {
		if resource.kind == "global" {
			for _, slug := range b.reachable(resource) {
				inGlobal[slug] = true
			}
		}
	}
	for _, o := range b.graph.occurrences {
		if o.ResourceKind != BlockResource || !inGlobal[o.Resource] {
			continue
		}
		if d, bound := b.graph.bindings[o.ID]; bound {
			b.validateGlobalPolicies(d.BehaviorSummary(), o.AuthoredPath, graphFieldLabel(o.ResourceKind, o.Resource, o.ResolvedPath)+" is placed in a global")
		}
	}
}

// graphFieldLabel names a field in diagnostics: by its path in a resource,
// or by its path within a block definition.
func graphFieldLabel(kind, resource, path string) string {
	if kind == BlockResource {
		return fmt.Sprintf("field %q of block %q", path, resource)
	}
	return fmt.Sprintf("field %q", path)
}

func (b *graphBuilder) bindReferences() {
	placedBy := b.placedBy()
	for _, o := range b.graph.occurrences {
		d, ok := b.graph.bindings[o.ID]
		if !ok {
			continue
		}
		condition := d.AdminPolicy().VisibleWhen
		if condition.IsZero() || condition.Err() != nil {
			continue
		}
		// resolve binds one reference from the occurrence's own scope, or from
		// the root of resource, the resource it is placed in.
		resolve := func(reference field.Reference, policy string, resource graphOwner) {
			scope := o.ScopeID
			label := graphFieldLabel(o.ResourceKind, o.Resource, o.ResolvedPath)
			if reference.Scope() == field.RootScope {
				scope = occurrenceID(resource.kind, resource.slug, "resource", "")
				if o.ResourceKind == BlockResource {
					label += fmt.Sprintf(" placed in %s %q", resource.kind, resource.slug)
				}
			}
			issue := func(message string) {
				if len(o.Provenance) > 0 {
					message += "; provenance: " + strings.Join(o.Provenance, " -> ")
				}
				b.issues = append(b.issues, schema.Issue{Code: "invalid_field_condition_path", Path: o.AuthoredPath + "." + policy + ".reference.path", Message: fmt.Sprintf("%s: %s", label, message)})
			}
			segments := strings.Split(reference.Path(), ".")
			var targetID string
			for index, segment := range segments {
				targetID = b.scopes[scope][segment]
				if targetID == "" {
					issue(fmt.Sprintf("%s reference %q has no field named %q in the selected object; check the field names and use Root or Sibling for the intended scope", reference.Scope(), reference.Path(), segment))
					return
				}
				target := b.graph.occurrences[b.byID[targetID]]
				if index < len(segments)-1 {
					if target.Boundary != "stored_object" && target.Boundary != "named_tab" {
						issue(fmt.Sprintf("%s reference %q crosses %q (%s); paths may traverse only groups and named tabs, not repeated fields or scalar values", reference.Scope(), reference.Path(), target.ResolvedPath, target.Kind))
						return
					}
					scope = targetID
				} else if targetView := b.graph.bindings[targetID]; target.Boundary != "stored_scalar" &&
					!(target.Boundary == "stored_reference" && !targetView.RelationshipHasMany() && len(targetView.RelationshipTargets()) == 1) {
					issue(fmt.Sprintf("%s reference %q selects %q (%s); visibility conditions require a stored scalar field", reference.Scope(), reference.Path(), target.ResolvedPath, target.Kind))
					return
				}
			}
			target := b.graph.occurrences[b.byID[targetID]]
			o.References = append(o.References, ReferenceBinding{Policy: policy, Scope: reference.Scope(), Path: reference.Path(), TargetID: targetID, ResolvedPath: target.ResolvedPath})
		}
		var bind func(field.Condition, string)
		bind = func(condition field.Condition, policy string) {
			for index, child := range condition.Conditions() {
				bind(child, fmt.Sprintf("%s.conditions[%d]", policy, index))
			}
			if condition.Kind() != field.ConditionKindPredicate {
				return
			}
			reference := condition.Reference()
			if reference.Scope() == field.RootScope && o.ResourceKind == BlockResource {
				// Each placing resource resolves the path against its own root.
				for _, resource := range placedBy[o.Resource] {
					resolve(reference, policy, resource)
				}
				return
			}
			resolve(reference, policy, o.owner())
		}
		bind(condition, "admin.visibleWhen")
	}
}

// Occurrence.owner names the resource or definition whose fields include o.
func (o *Occurrence) owner() graphOwner { return graphOwner{kind: o.ResourceKind, slug: o.Resource} }

// schemaIDs records the stable field ID of every stored occurrence: a
// resource's field by its resolved path, and a definition's field by its
// definition-relative ID. Each lookup follows only its own path.
func (b *graphBuilder) schemaIDs(resolved schema.Snapshot) {
	fields := map[graphPathKey][]schema.Field{}
	for _, collection := range resolved.Collections {
		fields[graphPathKey{kind: "collection", resource: string(collection.Slug)}] = collection.Fields
	}
	for _, global := range resolved.Globals {
		fields[graphPathKey{kind: "global", resource: string(global.Slug)}] = global.Fields
	}
	for _, block := range resolved.Blocks {
		fields[graphPathKey{kind: BlockResource, resource: block.Slug}] = block.ResolvedFields()
	}
	for key, index := range b.byPath {
		if candidate, found := schema.FieldAtPath(fields[graphPathKey{kind: key.kind, resource: key.resource}], strings.Split(key.path, ".")); found {
			b.graph.occurrences[index].SchemaID = candidate.ID
		}
	}
}

// validatePolicies checks the field kinds that admit each policy, and, for a
// global's own field, the global field policy. A block definition's fields are
// checked against the global policy once all placements are known; see
// validateGlobalDefinitions.
func (b *graphBuilder) validatePolicies(d field.View, authored, path, boundary string, p graphPosition) {
	summary := d.BehaviorSummary()
	label := graphFieldLabel(p.resourceKind, p.resource, path)
	issue := func(policy, message string) {
		b.issues = append(b.issues, schema.Issue{Code: "incompatible_field_policy", Path: authored + "." + policy, Message: fmt.Sprintf("%s: %s", label, message)})
	}
	if p.resourceKind == "global" {
		b.validateGlobalPolicies(summary, authored, label)
	}
	phases := make([]string, 0, len(summary.Hooks))
	for phase := range summary.Hooks {
		phases = append(phases, phase)
	}
	slices.Sort(phases)
	if boundary == "output" {
		for _, access := range []struct {
			name string
			set  bool
		}{
			{"create", summary.CreateAccess}, {"update", summary.UpdateAccess},
		} {
			if access.set {
				issue("access."+access.name, "computed and join fields support read access only; configure Access.Read")
			}
		}
		if summary.Validators > 0 {
			issue("validators", "computed and join fields derive their values; validate their stored input fields instead")
		}
		for _, phase := range phases {
			if phase != "afterRead" {
				issue("hooks."+phase, "computed and join fields support afterRead hooks only; configure AfterRead")
			}
		}
	}
	if boundary == "layout" && d.HasBehavior() {
		message := "layout fields have no value; place access rules and value callbacks on a stored field"
		for _, access := range []struct {
			name string
			set  bool
		}{
			{"create", summary.CreateAccess}, {"read", summary.ReadAccess}, {"update", summary.UpdateAccess},
		} {
			if access.set {
				issue("access."+access.name, message)
			}
		}
		if summary.Validators > 0 {
			issue("validators", message)
		}
		if summary.Resolver {
			issue("resolver", message)
		}
		for _, phase := range phases {
			policy := "hooks." + phase
			if phase == "afterRead" {
				policy = "afterRead"
			}
			issue(policy, message)
		}
	}
}

// validateGlobalPolicies rejects policies a global's fields cannot run: globals
// initialize through update access and have no duplicate or delete phases.
func (b *graphBuilder) validateGlobalPolicies(summary field.PolicySummary, authored, label string) {
	issue := func(policy, message string) {
		b.issues = append(b.issues, schema.Issue{Code: "incompatible_field_policy", Path: authored + "." + policy, Message: fmt.Sprintf("%s: %s", label, message)})
	}
	if summary.CreateAccess {
		issue("access.create", "global fields use update access, including initialization; configure Access.Update")
	}
	for _, phase := range []string{"beforeDuplicate", "beforeDelete", "afterDelete"} {
		if summary.Hooks[phase] > 0 {
			issue("hooks."+phase, "global fields do not support "+phase+" hooks; remove this hook or attach it to a collection field")
		}
	}
}

// Layout wrappers have their existing finite presentation contract. Accepting
// a scalar's complete Admin group must not imply inherited hidden/conditional
// state, component rendering, or other behavior the current admin cannot honor.
// Public extensions remain valid companion metadata; private attachments remain
// private. UI nodes use ordinary field lowering and do not enter this function.
func (r *fieldResolver) validateLayoutDefinition(d field.View, path string) {
	for _, issue := range d.Issues() {
		r.resolver.issue(issue.Code, joinConfigPath(path, issue.Path), issue.Message)
	}
	a := d.AdminPolicy()
	componentSet := func(c field.ComponentRef) bool { return c.Key != "" || c.PluginKey != "" || !c.Config.IsZero() }
	checks := []struct {
		name string
		set  bool
	}{
		{"description", a.Description != "" || len(a.DescriptionTranslations) > 0},
		{"placeholder", a.Placeholder != "" || len(a.PlaceholderTranslations) > 0},
		{"readOnly", a.ReadOnly}, {"hidden", a.Hidden}, {"sidebar", a.Sidebar}, {"columns", a.Columns != 0},
		{"editor", componentSet(a.Editor)}, {"rowLabel", componentSet(a.RowLabel)}, {"rowLabelPath", a.RowLabelPath != ""},
		{"rowLabels", a.RowLabels.Singular != "" || a.RowLabels.Plural != "" || len(a.RowLabels.SingularTranslations) > 0 || len(a.RowLabels.PluralTranslations) > 0},
		{"tab", a.Tab != "" || len(a.TabTranslations) > 0}, {"codeLanguage", a.CodeLanguage != ""},
		{"visibleWhen", !a.VisibleWhen.IsZero()},
		{"initiallyCollapsed", a.InitiallyCollapsed && d.Kind() != field.KindCollapsible},
		{"label", (d.Label() != "" || len(a.LabelTranslations) > 0) && d.Kind() != field.KindCollapsible && !d.IsUnnamedTab()},
	}
	for _, check := range checks {
		if check.set {
			r.resolver.issue("unsupported_layout_admin", path+".admin."+check.name, fmt.Sprintf("%s layout does not support %s; configure supported presentation on its fields or tab declarations", d.Kind(), check.name))
		}
	}
}
