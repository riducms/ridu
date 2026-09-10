package field

import "fmt"

// EditError describes authoring edits only, never runtime paths or occurrences.
type EditError struct {
	Code     string
	Name     string
	Expected Kind
	Actual   Kind
}

func (e *EditError) Error() string {
	return fmt.Sprintf("field edit %s for %q (expected %s, got %s)", e.Code, e.Name, e.Expected, e.Actual)
}

// ChildrenDraft owns a detached slice. It is usable only through explicit
// operations; publishing snapshots it again so a retained draft cannot mutate
// the returned field. On an edit error the input remains unchanged.
type ChildrenDraft struct{ fields Fields }

func (fields Fields) Edit(edit func(*ChildrenDraft) error) (Fields, error) {
	draft := &ChildrenDraft{fields: cloneFields(fields)}
	if err := edit(draft); err != nil {
		return cloneFields(fields), err
	}
	return cloneFields(draft.fields), nil
}

// Walk visits the prototype's published child boundary in declaration order.
// It does no occurrence binding and does not inspect callback/private state.
func (fields Fields) Walk(visit func(Node) error) error {
	for _, node := range cloneFields(fields) {
		if err := visit(node); err != nil {
			return err
		}
		if err := node.Children().Walk(visit); err != nil {
			return err
		}
	}
	return nil
}

func (d *ChildrenDraft) index(name string) (int, error) {
	for i, node := range d.fields {
		if node.Name() == name {
			return i, nil
		}
	}
	return 0, &EditError{Code: "not_found", Name: name}
}

// EditText passes the actual concrete immutable field to the decorator;
// reconstructing a new Text from metadata would lose its private attachments.
func (d *ChildrenDraft) EditText(name string, edit func(TextField) TextField) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	f, ok := d.fields[i].(TextField)
	if !ok {
		return &EditError{Code: "incompatible_kind", Name: name, Expected: TextKind, Actual: d.fields[i].Kind()}
	}
	edited := edit(f)
	if edited.Name() != name {
		return &EditError{Code: "rename_requires_replace", Name: name}
	}
	d.fields[i] = edited
	return nil
}

// EditChildren traverses one known group using its public editing contract.
// Other composite/embedded hosts are intentionally outside this small probe.
func (d *ChildrenDraft) EditChildren(name string, edit func(*ChildrenDraft) error) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	f, ok := d.fields[i].(GroupField)
	if !ok {
		return &EditError{Code: "incompatible_kind", Name: name, Expected: GroupKind, Actual: d.fields[i].Kind()}
	}
	edited, err := f.EditChildren(edit)
	if err != nil {
		return err
	}
	d.fields[i] = edited
	return nil
}

func (d *ChildrenDraft) Insert(index int, node Node) error {
	if index < 0 || index > len(d.fields) {
		return &EditError{Code: "invalid_index", Name: node.Name()}
	}
	if _, err := d.index(node.Name()); err == nil {
		return &EditError{Code: "duplicate_name", Name: node.Name()}
	}
	result := make(Fields, len(d.fields)+1)
	copy(result, d.fields[:index])
	result[index] = node.snapshot()
	copy(result[index+1:], d.fields[index:])
	d.fields = result
	return nil
}

// Replace deliberately replaces a whole node, including its kind and behavior.
// Use typed edits to preserve attachments; replacement never implies a merge.
func (d *ChildrenDraft) Replace(name string, node Node) error {
	i, err := d.index(name)
	if err != nil {
		return err
	}
	if other, err := d.index(node.Name()); err == nil && other != i {
		return &EditError{Code: "duplicate_name", Name: node.Name()}
	}
	d.fields[i] = node.snapshot()
	return nil
}
