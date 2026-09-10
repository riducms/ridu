package field

import (
	"fmt"
	"slices"
	"strings"
)

// Boundary distinguishes storage shape from presentation and embedded hosts.
type Boundary string

const (
	StoredScalar     Boundary = "stored_scalar"
	StoredReference  Boundary = "stored_reference"
	StoredObject     Boundary = "stored_object"
	RepeatedArray    Boundary = "repeated_array"
	BlockSet         Boundary = "blocks"
	BlockCase        Boundary = "block_case"
	NamedTabBoundary Boundary = "named_tab"
	Layout           Boundary = "layout"
	PluginValue      Boundary = "plugin_value"
	EmbeddedCase     Boundary = "embedded_case"
	Output           Boundary = "output"
)

// Boundary reports a node's value/traversal semantics, not just its broad kind.
func (d View) Boundary() Boundary {
	if d.IsNamedTab() {
		return NamedTabBoundary
	}
	switch d.kind {
	case KindGroup:
		return StoredObject
	case KindArray:
		return RepeatedArray
	case KindBlocks:
		return BlockSet
	case KindRelationship, KindUpload:
		return StoredReference
	case KindTabs, KindRow, KindUI, KindCollapsible:
		return Layout
	case KindVirtual, KindJoin:
		return Output
	case KindPlugin:
		return PluginValue
	default:
		return StoredScalar
	}
}

// BranchSelector selects one structural child boundary, never a flattened path.
// Index identifies an authored tab; Tree/Case/Key identify plugin payload cases.
type BranchSelector struct {
	Boundary Boundary
	Index    int
	Tree     string
	Case     string
	Slug     string
}

// Branch is a detached traversal view of one child scope.
type Branch struct {
	Selector   BranchSelector
	Name       string
	Referenced bool
	Fields     Fields
}

// Branches exposes only schema-owned children, including published embedded
// payload cases. Arbitrary plugin JSON is never interpreted as fields.
func (d View) Branches() []Branch {
	var out []Branch
	switch d.kind {
	case KindGroup, KindArray, KindRow, KindCollapsible, KindTabs:
		out = append(out, Branch{Selector: BranchSelector{Boundary: d.Boundary()}, Fields: d.fields.Snapshot()})
	case KindBlocks:
		for _, b := range d.blocks {
			out = append(out, Branch{Selector: BranchSelector{Boundary: BlockCase, Slug: b.Slug}, Name: b.Slug, Referenced: len(d.blockReferences) > 0, Fields: b.Fields.Snapshot()})
		}

	}
	for _, t := range d.pluginTrees {
		for _, c := range t.Cases {
			for _, b := range c.Types {
				out = append(out, Branch{Selector: BranchSelector{Boundary: EmbeddedCase, Tree: t.Key, Case: c.TagValue, Slug: b.Slug}, Name: b.Slug, Referenced: len(c.BlockReferences) > 0, Fields: b.Fields.Snapshot()})
			}
		}
	}
	return out
}
func replaceBranch(d View, s BranchSelector, children Fields) (View, error) {
	found := false
	for _, b := range d.Branches() {
		if b.Selector == s {
			if b.Referenced {
				return d, &EditError{Code: "referenced_block_immutable", Name: s.Slug}
			}
			found = true
			break
		}
	}
	if !found {
		return d, &EditError{Code: "unknown_branch", Name: d.name}
	}
	ds := children.Snapshot()
	switch s.Boundary {
	case BlockCase:
		d.blocks = slices.Clone(d.blocks)
		for i, b := range d.blocks {
			if b.Slug == s.Slug {
				d.blocks[i].Fields = ds
				return d, nil
			}
		}
	case EmbeddedCase:
		d.pluginTrees = cloneEmbeddedTrees(d.pluginTrees)
		for i, t := range d.pluginTrees {
			if t.Key != s.Tree {
				continue
			}
			for j, c := range t.Cases {
				if c.TagValue != s.Case {
					continue
				}
				for k, b := range c.Types {
					if b.Slug == s.Slug {
						d.pluginTrees[i].Cases[j].Types[k].Fields = ds
						return d, nil
					}
				}
			}
		}
	default:
		d.fields = ds
		return d, nil
	}
	return d, &EditError{Code: "unknown_branch", Name: d.name}
}

// Visit carries authored structural breadcrumbs; occurrence binding happens later.
type Visit struct {
	Node     Node
	Branches []BranchSelector
	Index    int
	// Path is the stored schema placement, including block slugs and embedded branches.
	Path []string
	// DefinitionSlug identifies the nearest shared definition, if any.
	DefinitionSlug string
}

// Walk visits nodes in declaration order and crosses only explicit branches.
func (fields Fields) Walk(visit func(Visit) error) error {
	var walk func(Fields, []BranchSelector, []string, string) error
	walk = func(fs Fields, path []BranchSelector, placement []string, definition string) error {
		for i, n := range fs.Snapshot() {
			d := Snapshot(n)
			current := slices.Clone(placement)
			if d.Boundary() != Layout && d.Name() != "" {
				current = append(current, d.Name())
			}
			if err := visit(Visit{Node: d, Branches: slices.Clone(path), Index: i, Path: current, DefinitionSlug: definition}); err != nil {
				return err
			}
			for _, b := range d.Branches() {
				childPath, childDefinition := slices.Clone(current), definition
				if b.Selector.Boundary == EmbeddedCase {
					childPath = append(childPath, b.Selector.Tree, b.Selector.Case)
				}
				if b.Selector.Slug != "" {
					childPath = append(childPath, b.Selector.Slug)
				}
				if b.Referenced {
					childDefinition = b.Selector.Slug
				}
				if err := walk(b.Fields, append(slices.Clone(path), b.Selector), childPath, childDefinition); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(fields, nil, nil, "")
}

// EditError identifies a failed explicit authoring edit. Failed transactions
// return the original graph and publish no partial changes.
type EditError struct {
	Code string
	Name string
	// Expected and Actual name the concrete field shapes for incompatible edits.
	Expected string
	Actual   string
}

func (e *EditError) Error() string {
	switch e.Code {
	case "incompatible_kind":
		return fmt.Sprintf("field edit %s for %q: expected %s, got %s; choose the matching As or Edit operation", e.Code, e.Name, e.Expected, e.Actual)
	case "duplicate_name":
		return fmt.Sprintf("field edit %s: field %q already exists in this object; choose a distinct child name", e.Code, e.Name)
	case "invalid_name":
		return fmt.Sprintf("field edit %s: %q is not a valid field name", e.Code, e.Name)
	case "not_found":
		return fmt.Sprintf("field edit %s: no direct child named %q exists", e.Code, e.Name)
	case "ambiguous_name":
		return fmt.Sprintf("field edit %s: more than one direct child is named %q; select an explicit child position", e.Code, e.Name)
	case "missing_child_editor":
		return fmt.Sprintf("field edit %s for %q: supply a checked As converter and a refinement callback", e.Code, e.Name)
	case "rename_requires_explicit_edit":
		return fmt.Sprintf("field edit %s for %q: use Rename or RenameRoot to rename a child and update its references", e.Code, e.Name)
	case "unknown_branch":
		return fmt.Sprintf("field edit %s for %q: the selected child branch does not exist; inspect Branches before editing", e.Code, e.Name)
	case "invalid_index":
		return "field edit invalid_index: child position is outside the available fields"
	}
	return fmt.Sprintf("field edit %s for %q", e.Code, e.Name)
}

func incompatibleEdit(expected string, actual View) *EditError {
	return &EditError{Code: "incompatible_kind", Name: actual.Name(), Expected: expected, Actual: concreteFieldShape(actual)}
}

func concreteFieldShape(d View) string {
	switch d.kind {
	case KindTextList:
		return "TextListField"
	case KindNumberList:
		return "NumberListField"
	case KindSelect:
		if d.selectMany {
			return "MultiSelectField"
		}
		return "SelectField"
	case KindRelationship:
		if len(d.relationTo) > 1 {
			if d.relationMany {
				return "PolymorphicRelationshipsField"
			}
			return "PolymorphicRelationshipField"
		}
		if d.relationMany {
			return "RelationshipsField"
		}
		return "RelationshipField"
	case KindUpload:
		if d.relationMany {
			return "UploadsField"
		}
		return "UploadField"
	case KindVirtual:
		return "OutputField"
	case KindRow, KindTabs, KindCollapsible, KindUI:
		return "LayoutField"
	case KindJSON:
		return "JSONField"
	}
	kind := string(d.kind)
	if kind == "" {
		return "unspecified field"
	}
	return strings.ToUpper(kind[:1]) + kind[1:] + "Field"
}

// ChildrenDraft owns a detached slice. Committing snapshots again, so retained
// drafts cannot mutate published graphs. Structural edits are explicit.
type ChildrenDraft struct{ fields Fields }

// Edit applies checked changes to a detached draft and returns a new field list.
// Use it to refine a reusable graph without rebuilding it. The result is
// published only when the callback and graph validation succeed; on error, Edit
// returns an unchanged snapshot of the original fields.
func (fields Fields) Edit(edit func(*ChildrenDraft) error) (Fields, error) {
	draft := &ChildrenDraft{fields: fields.Snapshot()}
	if edit != nil {
		if err := edit(draft); err != nil {
			return fields.Snapshot(), err
		}
	}
	if err := validateDraft(draft.fields); err != nil {
		return fields.Snapshot(), err
	}
	return draft.fields.Snapshot(), nil
}
func validateDraft(fs Fields) error {
	seen := map[string]bool{}
	add := func(name string) error {
		if seen[name] {
			return &EditError{Code: "duplicate_name", Name: name}
		}
		seen[name] = true
		return nil
	}
	var scope func(Fields) error
	scope = func(fields Fields) error {
		for _, n := range fields {
			d := Snapshot(n)
			if err := checkName(d); err != nil {
				return &EditError{Code: "invalid_name", Name: d.name}
			}
			if err := d.AdminPolicy().VisibleWhen.Err(); err != nil {
				return fmt.Errorf("field %q: %w", d.name, err)
			}
			switch d.kind {
			case KindRow, KindCollapsible:
				if err := scope(d.fields.Snapshot()); err != nil {
					return err
				}
			case KindTabs:
				if err := scope(d.fields); err != nil {
					return err
				}
			default:
				if err := add(d.name); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return scope(fs)
}

// Fields returns a detached inspection snapshot of the draft's current state.
func (d *ChildrenDraft) Fields() Fields { return d.fields.Snapshot() }
func (d *ChildrenDraft) index(name string) (int, error) {
	index := -1
	for i, n := range d.fields {
		if n.Name() == name {
			if index >= 0 {
				return 0, &EditError{Code: "ambiguous_name", Name: name}
			}
			index = i
		}
	}
	if index < 0 {
		return 0, &EditError{Code: "not_found", Name: name}
	}
	return index, nil
}

// EditText refines the actual node, preserving hidden attachments.
func (d *ChildrenDraft) EditText(name string, edit func(TextField) TextField) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	f, err := AsText(d.fields[i])
	if err != nil {
		return err
	}
	next := edit(f)
	if next.Name() != name {
		return &EditError{Code: "rename_requires_explicit_edit", Name: name}
	}
	d.fields[i] = next
	return nil
}

// EditNumber refines a known numeric node and diagnoses incompatible kinds.
func (d *ChildrenDraft) EditNumber(name string, edit func(NumberField) NumberField) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	f := Snapshot(d.fields[i])
	if f.kind != KindNumber {
		return incompatibleEdit("NumberField", f)
	}
	next := edit(NumberField{nodeView{f}})
	if next.Name() != name {
		return &EditError{Code: "rename_requires_explicit_edit", Name: name}
	}
	d.fields[i] = next
	return nil
}

// EditBranch targets an explicit object, row, block case, tab or embedded case.
func (d *ChildrenDraft) EditBranch(name string, selector BranchSelector, edit func(*ChildrenDraft) error) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	return d.EditBranchAt(i, selector, edit)
}

// EditBranchAt addresses a layout by its authored position when names are
// absent or ambiguous. The selector still identifies a structural boundary.
func (d *ChildrenDraft) EditBranchAt(index int, selector BranchSelector, edit func(*ChildrenDraft) error) error {
	if index < 0 || index >= len(d.fields) {
		return &EditError{Code: "invalid_index"}
	}
	node := Snapshot(d.fields[index])
	for _, branch := range node.Branches() {
		if branch.Selector == selector {
			children, err := branch.Fields.Edit(edit)
			if err != nil {
				return err
			}
			edited, err := replaceBranch(node, selector, children)
			if err != nil {
				return err
			}
			return d.ReplaceAt(index, edited)
		}
	}
	return &EditError{Code: "unknown_branch", Name: node.name}
}

// EditChildren targets the one direct children branch of an object/array/layout.
// Blocks, tabs and embedded payloads require their explicit branch selector.
func (d *ChildrenDraft) EditChildren(name string, edit func(*ChildrenDraft) error) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	n := Snapshot(d.fields[i])
	switch n.kind {
	case KindGroup, KindArray, KindRow, KindCollapsible:
		return d.EditBranch(name, BranchSelector{Boundary: n.Boundary()}, edit)
	default:
		return incompatibleEdit("GroupField", n)
	}
}
func (d *ChildrenDraft) EditBlock(name, slug string, edit func(*ChildrenDraft) error) error {
	return d.EditBranch(name, BranchSelector{Boundary: BlockCase, Slug: slug}, edit)
}
func (d *ChildrenDraft) Insert(index int, node Node) error {
	n := Snapshot(node)
	if index < 0 || index > len(d.fields) {
		return &EditError{Code: "invalid_index", Name: n.name}
	}
	next := slices.Insert(d.fields.Snapshot(), index, Node(n))
	if err := validateDraft(next); err != nil {
		return err
	}
	d.fields = next
	return nil
}

// Replace deliberately replaces a whole node, including behavior and kind.
func (d *ChildrenDraft) Replace(name string, node Node) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	return d.ReplaceAt(i, node)
}

// ReplaceAt deliberately replaces an authored child position, including unnamed
// or same-named layout wrappers. It never merges or reconstructs a node.
func (d *ChildrenDraft) ReplaceAt(index int, node Node) error {
	if index < 0 || index >= len(d.fields) {
		return &EditError{Code: "invalid_index"}
	}
	next := d.fields.Snapshot()
	next[index] = Snapshot(node)
	if err := validateDraft(next); err != nil {
		return err
	}
	d.fields = next
	return nil
}
func (d *ChildrenDraft) Remove(name string) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	d.fields = slices.Delete(d.fields, i, i+1)
	return nil
}

// Rename changes one direct local name. Sibling references in this same stored
// scope are rebound explicitly; root references require RenameRoot at the root.
// Calling Rename on a facade merely changes that value's own name.
func (d *ChildrenDraft) Rename(name, replacement string) error {
	return d.rename(name, replacement, false)
}

// RenameRoot is the explicit root edit that also rewrites root selectors in
// descendants. It must be used on the resource's root draft.
func (d *ChildrenDraft) RenameRoot(name, replacement string) error {
	return d.rename(name, replacement, true)
}
func (d *ChildrenDraft) rename(name, replacement string, root bool) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	if name == replacement {
		return nil
	}
	node := rename(Snapshot(d.fields[i]), replacement)
	if err := checkName(node); err != nil {
		return &EditError{Code: "invalid_name", Name: replacement}
	}
	next := d.fields.Snapshot()
	next[i] = node
	if err := validateDraft(next); err != nil {
		return err
	}
	var rewrite func(Fields, bool) (Fields, bool, error)
	rewrite = func(fs Fields, sameScope bool) (Fields, bool, error) {
		changed := false
		out := fs.Snapshot()
		for i, n := range out {
			f := Snapshot(n)
			a := f.AdminPolicy()
			var rewriteCondition func(Condition) Condition
			rewriteCondition = func(condition Condition) Condition {
				condition = cloneCondition(condition)
				for i, child := range condition.conditions {
					condition.conditions[i] = rewriteCondition(child)
				}
				ref := condition.reference
				if (sameScope && ref.scope == SiblingScope) || (root && ref.scope == RootScope) {
					if ref.path == name || strings.HasPrefix(ref.path, name+".") {
						ref.path = replacement + strings.TrimPrefix(ref.path, name)
						condition.reference = ref
						changed = true
					}
				}
				return condition
			}
			a.VisibleWhen = rewriteCondition(a.VisibleWhen)
			f = f.setAdmin(a)
			for _, b := range f.Branches() {
				flat := sameScope && b.Selector.Boundary == Layout
				children, rewritten, err := rewrite(b.Fields, flat)
				if err != nil {
					return nil, false, err
				}
				if rewritten {
					f, err = replaceBranch(f, b.Selector, children)
					if err != nil {
						return nil, false, err
					}
					changed = true
				}
			}
			out[i] = f
		}
		return out, changed, nil
	}
	rewritten, _, err := rewrite(next, true)
	if err != nil {
		return err
	}
	d.fields = rewritten
	return nil
}

// EditTextList refines a direct primitive-list child while preserving its policies.
func (d *ChildrenDraft) EditTextList(name string, edit func(TextListField) TextListField) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	f, err := AsTextList(d.fields[i])
	if err != nil {
		return err
	}
	if edit == nil {
		return &EditError{Code: "missing_child_editor", Name: name}
	}
	next := edit(f)
	if next.Name() != name {
		return &EditError{Code: "rename_requires_explicit_edit", Name: name}
	}
	d.fields[i] = next
	return nil
}

// EditNumberList refines a direct primitive-list child while preserving its policies.
func (d *ChildrenDraft) EditNumberList(name string, edit func(NumberListField) NumberListField) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	f, err := AsNumberList(d.fields[i])
	if err != nil {
		return err
	}
	if edit == nil {
		return &EditError{Code: "missing_child_editor", Name: name}
	}
	next := edit(f)
	if next.Name() != name {
		return &EditError{Code: "rename_requires_explicit_edit", Name: name}
	}
	d.fields[i] = next
	return nil
}
