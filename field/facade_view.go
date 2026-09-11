package field

// AsText provides a checked concrete view of an immutable node.
func AsText(node Node) (TextField, error) {
	d := Snapshot(node)
	if d.kind != KindText && d.kind != KindCode && d.kind != KindTextarea {
		return TextField{}, incompatibleEdit("TextField", d)
	}
	return TextField{nodeView{d}}, nil
}

// AsEmail provides a checked concrete view of an immutable node.
func AsEmail(node Node) (EmailField, error) {
	d := Snapshot(node)
	if d.kind != KindEmail {
		return EmailField{}, incompatibleEdit("EmailField", d)
	}
	return EmailField{nodeView{d}}, nil
}

// AsDate provides a checked concrete view of an immutable node.
func AsDate(node Node) (DateField, error) {
	d := Snapshot(node)
	if d.kind != KindDate {
		return DateField{}, incompatibleEdit("DateField", d)
	}
	return DateField{nodeView{d}}, nil
}

// AsNumber provides a checked concrete view of an immutable node.
func AsNumber(node Node) (NumberField, error) {
	d := Snapshot(node)
	if d.kind != KindNumber {
		return NumberField{}, incompatibleEdit("NumberField", d)
	}
	return NumberField{nodeView{d}}, nil
}

// AsCheckbox provides a checked concrete view of an immutable node.
func AsCheckbox(node Node) (CheckboxField, error) {
	d := Snapshot(node)
	if d.kind != KindCheckbox {
		return CheckboxField{}, incompatibleEdit("CheckboxField", d)
	}
	return CheckboxField{nodeView{d}}, nil
}

// AsJSON provides a checked concrete view of an immutable node.
func AsJSON(node Node) (JSONField, error) {
	d := Snapshot(node)
	if d.kind != KindJSON {
		return JSONField{}, incompatibleEdit("JSONField", d)
	}
	return JSONField{nodeView{d}}, nil
}

// AsPoint provides a checked concrete view of an immutable node.
func AsPoint(node Node) (PointField, error) {
	d := Snapshot(node)
	if d.kind != KindPoint {
		return PointField{}, incompatibleEdit("PointField", d)
	}
	return PointField{nodeView{d}}, nil
}

// AsSelect provides a checked concrete view of an immutable node.
func AsSelect(node Node) (SelectField, error) {
	d := Snapshot(node)
	if (d.kind != KindSelect && d.kind != KindRadio) || d.selectMany {
		return SelectField{}, incompatibleEdit("SelectField", d)
	}
	return SelectField{nodeView{d}}, nil
}

// AsMultiSelect provides a checked concrete view of an immutable node.
func AsMultiSelect(node Node) (MultiSelectField, error) {
	d := Snapshot(node)
	if d.kind != KindSelect || d.selectMany != true {
		return MultiSelectField{}, incompatibleEdit("MultiSelectField", d)
	}
	return MultiSelectField{nodeView{d}}, nil
}

// AsRelationship provides a checked concrete view of an immutable node.
func AsRelationship(node Node) (RelationshipField, error) {
	d := Snapshot(node)
	if d.kind != KindRelationship || d.relationMany != false || (len(d.relationTo) > 1) != false {
		return RelationshipField{}, incompatibleEdit("RelationshipField", d)
	}
	return RelationshipField{nodeView{d}}, nil
}

// AsRelationships provides a checked concrete view of an immutable node.
func AsRelationships(node Node) (RelationshipsField, error) {
	d := Snapshot(node)
	if d.kind != KindRelationship || d.relationMany != true || (len(d.relationTo) > 1) != false {
		return RelationshipsField{}, incompatibleEdit("RelationshipsField", d)
	}
	return RelationshipsField{nodeView{d}}, nil
}

// AsUpload provides a checked concrete view of an immutable node.
func AsUpload(node Node) (UploadField, error) {
	d := Snapshot(node)
	if d.kind != KindUpload || d.relationMany != false || (len(d.relationTo) > 1) != false {
		return UploadField{}, incompatibleEdit("UploadField", d)
	}
	return UploadField{nodeView{d}}, nil
}

// AsUploads provides a checked concrete view of an immutable node.
func AsUploads(node Node) (UploadsField, error) {
	d := Snapshot(node)
	if d.kind != KindUpload || d.relationMany != true || (len(d.relationTo) > 1) != false {
		return UploadsField{}, incompatibleEdit("UploadsField", d)
	}
	return UploadsField{nodeView{d}}, nil
}

// AsPolymorphicRelationship provides a checked concrete view of an immutable node.
func AsPolymorphicRelationship(node Node) (PolymorphicRelationshipField, error) {
	d := Snapshot(node)
	if d.kind != KindRelationship || d.relationMany != false || (len(d.relationTo) > 1) != true {
		return PolymorphicRelationshipField{}, incompatibleEdit("PolymorphicRelationshipField", d)
	}
	return PolymorphicRelationshipField{nodeView{d}}, nil
}

// AsPolymorphicRelationships provides a checked concrete view of an immutable node.
func AsPolymorphicRelationships(node Node) (PolymorphicRelationshipsField, error) {
	d := Snapshot(node)
	if d.kind != KindRelationship || d.relationMany != true || (len(d.relationTo) > 1) != true {
		return PolymorphicRelationshipsField{}, incompatibleEdit("PolymorphicRelationshipsField", d)
	}
	return PolymorphicRelationshipsField{nodeView{d}}, nil
}

// AsGroup provides a checked concrete view of an immutable node.
func AsGroup(node Node) (GroupField, error) {
	d := Snapshot(node)
	if d.kind != KindGroup {
		return GroupField{}, incompatibleEdit("GroupField", d)
	}
	return GroupField{nodeView{d}}, nil
}

// AsArray provides a checked concrete view of an immutable node.
func AsArray(node Node) (ArrayField, error) {
	d := Snapshot(node)
	if d.kind != KindArray {
		return ArrayField{}, incompatibleEdit("ArrayField", d)
	}
	return ArrayField{nodeView{d}}, nil
}

// AsBlocks provides a checked concrete view of an immutable node.
func AsBlocks(node Node) (BlocksField, error) {
	d := Snapshot(node)
	if d.kind != KindBlocks {
		return BlocksField{}, incompatibleEdit("BlocksField", d)
	}
	return BlocksField{nodeView{d}}, nil
}

// AsPlugin provides a checked concrete view of an immutable node.
func AsPlugin(node Node) (PluginField, error) {
	d := Snapshot(node)
	if d.kind != KindPlugin {
		return PluginField{}, incompatibleEdit("PluginField", d)
	}
	return PluginField{nodeView{d}}, nil
}

// AsOutput provides a checked concrete view of an immutable node.
func AsOutput(node Node) (OutputField, error) {
	d := Snapshot(node)
	if d.kind != KindVirtual {
		return OutputField{}, incompatibleEdit("OutputField", d)
	}
	return OutputField{nodeView{d}}, nil
}

// AsJoin provides a checked inverse join output facade.
func AsJoin(node Node) (JoinField, error) {
	d := Snapshot(node)
	if d.kind != KindJoin {
		return JoinField{}, incompatibleEdit("JoinField", d)
	}
	return JoinField{nodeView{d}}, nil
}

// AsTextList provides a checked concrete view of an immutable primitive-list node.
func AsTextList(node Node) (TextListField, error) {
	d := Snapshot(node)
	if d.kind != KindTextList {
		return TextListField{}, incompatibleEdit("TextListField", d)
	}
	return TextListField{nodeView{d}}, nil
}

// AsNumberList provides a checked concrete view of an immutable primitive-list node.
func AsNumberList(node Node) (NumberListField, error) {
	d := Snapshot(node)
	if d.kind != KindNumberList {
		return NumberListField{}, incompatibleEdit("NumberListField", d)
	}
	return NumberListField{nodeView{d}}, nil
}
