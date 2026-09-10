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
// executable attachments and placement metadata here; both remain private.
// Application construction lowers the companion once; requests never query it.
type Graph struct {
	occurrences []Occurrence
	bindings    map[string]field.View
}

// Occurrence is a schema placement, not a concrete runtime row. Repeated axes
// retain the identity property and case needed by a later operation binding;
// LocaleOwner identifies the existing exact-locale dimension without inventing
// runtime rows or expanding localization semantics.
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

type RepeatedAxis struct {
	OccurrenceID string `json:"occurrenceId"`
	Identity     string `json:"identity"`
	Case         string `json:"case,omitempty"`
}

type ReferenceBinding struct {
	Policy       string               `json:"policy"`
	Scope        field.ReferenceScope `json:"scope"`
	Path         string               `json:"path"`
	TargetID     string               `json:"targetId"`
	ResolvedPath string               `json:"resolvedPath"`
}

func (g Graph) Occurrences() []Occurrence {
	result := slices.Clone(g.occurrences)
	for i := range result {
		result[i].Repeated = slices.Clone(result[i].Repeated)
		result[i].Provenance = slices.Clone(result[i].Provenance)
		result[i].References = slices.Clone(result[i].References)
		result[i].Extensions = maps.Clone(result[i].Extensions)
	}
	return result
}

// Binding returns an immutable view, never an executable dispatch table.
func (g Graph) Binding(id string) (field.View, bool) { d, ok := g.bindings[id]; return d, ok }

// ResolveGraph binds field-owned selectors before lowering the manifest and
// retains their symbolic declarations in immutable occurrence views.
func ResolveGraph(input Input) (schema.Manifest, Graph, error) {
	var bindErr error
	input, bindErr = bindInputBlocks(input)
	if bindErr != nil {
		return schema.Manifest{}, Graph{}, bindErr
	}
	b := graphBuilder{graph: Graph{bindings: make(map[string]field.View)}, scopes: make(map[string]map[string]string), byID: make(map[string]int), byPath: make(map[string]int)}
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
	manifest, err := resolveBound(input)
	if err != nil {
		return schema.Manifest{}, Graph{}, err
	}
	if len(b.issues) != 0 {
		return schema.Manifest{}, Graph{}, schema.NewValidationError(b.issues)
	}
	snapshot := manifest.Snapshot()
	for _, collection := range snapshot.Collections {
		b.schemaIDs("collection", string(collection.Slug), collection.Fields)
	}
	for _, global := range snapshot.Globals {
		b.schemaIDs("global", string(global.Slug), global.Fields)
	}
	return manifest, b.graph, nil
}

type graphPosition struct {
	resourceKind, resource, rootScope, scope, parent, path, localeOwner string
	repeated                                                            []RepeatedAxis
}

type graphBuilder struct {
	graph  Graph
	scopes map[string]map[string]string
	byID   map[string]int
	byPath map[string]int
	issues []schema.Issue
}

func occurrenceID(kind, resource, boundary, path string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + resource + "\x00" + boundary + "\x00" + path))
	return "occ_" + hex.EncodeToString(sum[:])
}

func (b *graphBuilder) resource(kind, resource, authored string, definitions field.Fields) {
	scope := occurrenceID(kind, resource, "resource", "")
	b.scopes[scope] = make(map[string]string)
	b.fields(definitions, authored, graphPosition{resourceKind: kind, resource: resource, rootScope: scope, scope: scope})
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
		b.byPath[p.resourceKind+"\x00"+p.resource+"\x00"+path] = len(b.graph.occurrences)
	}
	b.graph.occurrences = append(b.graph.occurrences, Occurrence{ID: id, ResourceKind: p.resourceKind, Resource: p.resource, Name: name, Kind: kind, Boundary: boundary, Stored: stored, AuthoredPath: authored, ResolvedPath: path, ParentID: p.parent, ScopeID: p.scope, LocaleOwner: p.localeOwner, Repeated: slices.Clone(p.repeated), Provenance: slices.Clone(provenance)})
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
	b.graph.bindings[id] = d
	b.graph.occurrences[b.byID[id]].Extensions = d.AdminPolicy().Extensions
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
		p.repeated = append(slices.Clone(p.repeated), RepeatedAxis{OccurrenceID: id, Identity: "_key"})
		b.blocks(d.Blocks(), authored+".blocks", p)
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
				r.repeated = append(slices.Clone(q.repeated), RepeatedAxis{OccurrenceID: r.parent, Identity: c.Identity, Case: c.TagValue})
				b.blocks(c.Types, casePath+".types", r)
			}
		}
	}
}

func (b *graphBuilder) blocks(blocks []field.Block, authored string, p graphPosition) {
	for i, block := range blocks {
		q := p
		q.path = appendGraphPath(p.path, block.Slug)
		blockPath := fmt.Sprintf("%s[%d]", authored, i)
		q.parent = b.add(block.Slug, "", "block_case", false, blockPath, q.path, p, nil)
		q.scope = q.parent
		b.scopes[q.scope] = make(map[string]string)
		q.repeated = slices.Clone(p.repeated)
		if len(q.repeated) != 0 {
			q.repeated[len(q.repeated)-1].Case = block.Slug
		}
		b.fields(block.Fields, blockPath+".fields", q)
	}
}

func (b *graphBuilder) bindReferences() {
	for i := range b.graph.occurrences {
		o := &b.graph.occurrences[i]
		d, ok := b.graph.bindings[o.ID]
		if !ok {
			continue
		}
		condition := d.AdminPolicy().VisibleWhen
		if condition.IsZero() || condition.Err() != nil {
			continue
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
			issue := func(message string) {
				if len(o.Provenance) > 0 {
					message += "; provenance: " + strings.Join(o.Provenance, " -> ")
				}
				b.issues = append(b.issues, schema.Issue{Code: "invalid_field_condition_path", Path: o.AuthoredPath + "." + policy + ".reference.path", Message: fmt.Sprintf("field %q: %s", o.ResolvedPath, message)})
			}
			scope := o.ScopeID
			if reference.Scope() == field.RootScope {
				scope = occurrenceID(o.ResourceKind, o.Resource, "resource", "")
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
		bind(condition, "admin.visibleWhen")
	}
}

func (b *graphBuilder) schemaIDs(kind, resource string, fields []schema.Field) {
	for _, candidate := range fields {
		if i, ok := b.byPath[kind+"\x00"+resource+"\x00"+candidate.Path.String()]; ok {
			b.graph.occurrences[i].SchemaID = candidate.ID
		}
		b.schemaIDs(kind, resource, schema.ChildFields(candidate))
	}
}

func (b *graphBuilder) validatePolicies(d field.View, authored, path, boundary string, p graphPosition) {
	summary := d.BehaviorSummary()
	issue := func(policy, message string) {
		b.issues = append(b.issues, schema.Issue{Code: "incompatible_field_policy", Path: authored + "." + policy, Message: fmt.Sprintf("field %q: %s", path, message)})
	}
	if p.resourceKind == "global" {
		if summary.CreateAccess {
			issue("access.create", "global fields use update access, including initialization; configure Access.Update")
		}
		for _, phase := range []string{"beforeDuplicate", "beforeDelete", "afterDelete"} {
			if summary.Hooks[phase] > 0 {
				issue("hooks."+phase, "global fields do not support "+phase+" hooks; remove this hook or attach it to a collection field")
			}
		}
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
				issue("hooks."+phase, "computed and join fields support afterRead hooks only; configure ReadHooks.AfterRead")
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
				policy = "readHooks.afterRead"
			}
			issue(policy, message)
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
	componentSet := func(c field.ComponentRef) bool { return c.Key != "" || c.PluginKey != "" || c.Config.Kind() != "" }
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
