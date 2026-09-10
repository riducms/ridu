package config

import (
	"fmt"
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

// Managed upload fields have one canonical declaration. Their storage shape is
// framework-owned; applications can attach access, callbacks and presentation
// through the same field graph as every other field.
var managedUploadFields = []struct {
	id   schema.StableID
	node field.Node
}{
	{"upload-filename", field.Text("filename").Label("Filename").Required()},
	{"upload-mime-type", field.Text("mimeType").Label("MIME type").Required()},
	{"upload-filesize", field.Number("filesize").Label("File size").Required()},
	{"upload-url", field.Text("url").Label("URL").Required()},
	{"upload-object-key", field.Text("objectKey").Label("Object key").Required().Index()},
	{"upload-width", field.Number("width").Label("Width")},
	{"upload-height", field.Number("height").Label("Height")},
	{"upload-sizes", field.JSON("sizes").Label("Generated sizes")},
	{"upload-focal-x", field.Number("focalX").Label("Focal X")},
	{"upload-focal-y", field.Number("focalY").Label("Focal Y")},
	{"upload-crop-x", field.Number("cropX").Label("Crop X")},
	{"upload-crop-y", field.Number("cropY").Label("Crop Y")},
	{"upload-crop-width", field.Number("cropWidth").Label("Crop width")},
	{"upload-crop-height", field.Number("cropHeight").Label("Crop height")},
}

// UploadFields completes the graph before plugin edits and occurrence binding.
// Managed metadata retains its required/index/read-only settings. Declarations
// may add field-owned behavior and presentation, but cannot change its storage
// shape or introduce static value constraints that uploads do not support.
func UploadFields(authored field.Fields, path string) (field.Fields, error) {
	result := authored.Snapshot()
	var issues []schema.Issue
	for _, managed := range managedUploadFields {
		shape := field.Snapshot(managed.node)
		found := false
		for index, node := range result {
			view := field.Snapshot(node)
			if view.Name() != shape.Name() {
				continue
			}
			found = true
			fieldPath := fmt.Sprintf("%s[%d]", path, index)
			if view.Kind() != shape.Kind() || view.Localized() {
				issues = append(issues, schema.Issue{Code: "invalid_upload_metadata_field", Path: fieldPath, Message: fmt.Sprintf("upload metadata %q requires its fixed nonlocalized %s shape", shape.Name(), shape.Kind())})
				continue
			}
			_, hasDefault := view.Default()
			_, hasSlug := view.SlugSource()
			_, hasMinLength := view.MinLength()
			_, hasMaxLength := view.MaxLength()
			_, hasMin := view.Min()
			_, hasMax := view.Max()
			_, hasStep := view.Step()
			for _, constraint := range []struct {
				name string
				set  bool
			}{
				{"required", view.Required() && !shape.Required()},
				{"index", view.Index() && !shape.Index()},
				{"unique", view.Unique()},
				{"default", hasDefault},
				{"slug", hasSlug},
				{"minLength", hasMinLength},
				{"maxLength", hasMaxLength},
				{"min", hasMin},
				{"max", hasMax},
				{"step", hasStep},
			} {
				if constraint.set {
					issues = append(issues, schema.Issue{Code: "unsupported_upload_metadata_constraint", Path: fieldPath + "." + constraint.name, Message: fmt.Sprintf("upload metadata %q has a framework-owned storage shape and does not support %s constraints", shape.Name(), constraint.name)})
				}
			}
			result[index] = normalizeUploadField(view, shape)
		}
		if !found {
			result = append(result, normalizeUploadField(shape, shape))
		}
	}
	if len(issues) != 0 {
		return result, schema.NewValidationError(issues)
	}
	return result, nil
}

func normalizeUploadField(view, shape field.View) field.Node {
	label := view.Label()
	if strings.TrimSpace(label) == "" {
		label = shape.Label()
	}
	readOnly := func(admin *field.Admin) { admin.ReadOnly = true }
	switch shape.Kind() {
	case field.KindText:
		node, _ := field.AsText(view)
		return node.Required(shape.Required()).Index(shape.Index()).Label(label).EditAdmin(readOnly)
	case field.KindNumber:
		node, _ := field.AsNumber(view)
		return node.Required(shape.Required()).Index(shape.Index()).Label(label).EditAdmin(readOnly)
	case field.KindJSON:
		node, _ := field.AsJSON(view)
		return node.Required(shape.Required()).Label(label).EditAdmin(readOnly)
	default:
		return view
	}
}

// The manifest adds storage identity to the resolved graph; it does not replace
// fields with a second schema declaration or discard their policies.
func markUploadMetadata(fields []schema.Field) {
	for index := range fields {
		for _, managed := range managedUploadFields {
			if fields[index].Name != managed.node.Name() {
				continue
			}
			fields[index].ID = managed.id
			fields[index].Category = schema.FieldCategoryUpload
			// Unconstrained managed numbers have no numeric schema descriptor.
			// This is the upload storage contract consumed by adapter manifests.
			if fields[index].Type == schema.FieldTypeNumber {
				fields[index].Number = nil
			}
			break
		}
	}
}
