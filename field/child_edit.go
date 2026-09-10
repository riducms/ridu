package field

// EditChild refines one direct child of a group or array while retaining the
// parent's concrete type. Pass a checked converter such as AsText or AsSelect
// and return the refined field from edit. Existing policies and attachments
// remain on that field unless the edit explicitly replaces them.
//
// A missing child, incompatible concrete kind, or attempted rename returns the
// original parent and an error. Use EditChildren for nested or multiple edits.
func EditChild[Parent interface {
	Node
	EditChildren(func(*ChildrenDraft) error) (Parent, error)
}, Child Node](parent Parent, name string, as func(Node) (Child, error), edit func(Child) Child) (Parent, error) {
	if as == nil || edit == nil {
		return parent, &EditError{Code: "missing_child_editor", Name: name}
	}
	return parent.EditChildren(func(draft *ChildrenDraft) error {
		index, err := draft.index(name)
		if err != nil {
			return err
		}
		child, err := as(draft.fields[index])
		if err != nil {
			return err
		}
		next := edit(child)
		if Snapshot(next).Name() != name {
			return &EditError{Code: "rename_requires_explicit_edit", Name: name}
		}
		return draft.Replace(name, next)
	})
}

// ReplaceChild deliberately replaces one direct child, including its kind,
// policies, and attachments. Unrelated children and the parent's own policies
// are preserved. A failed replacement returns the original parent and an error.
func ReplaceChild[Parent interface {
	Node
	EditChildren(func(*ChildrenDraft) error) (Parent, error)
}](parent Parent, name string, replacement Node) (Parent, error) {
	return parent.EditChildren(func(draft *ChildrenDraft) error {
		return draft.Replace(name, replacement)
	})
}

// AppendChild appends one direct child to a group or array. It snapshots the
// child and preserves existing parent and child policies. Duplicate or invalid
// names return the original parent and an error.
func AppendChild[Parent interface {
	Node
	EditChildren(func(*ChildrenDraft) error) (Parent, error)
}](parent Parent, child Node) (Parent, error) {
	return parent.EditChildren(func(draft *ChildrenDraft) error {
		return draft.Insert(len(draft.fields), child)
	})
}
