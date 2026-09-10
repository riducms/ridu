package mongodb

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const (
	maxMongoUploadImageSizes = 64
	maxMongoUploadFileBytes  = 256 << 20
)

type mongoUploadMetadataField struct {
	id       schema.StableID
	name     string
	label    string
	typeName schema.FieldType
	required bool
	indexed  bool
}

var mongoUploadMetadataFields = []mongoUploadMetadataField{
	{id: "upload-filename", name: "filename", label: "Filename", typeName: schema.FieldTypeText, required: true},
	{id: "upload-mime-type", name: "mimeType", label: "MIME type", typeName: schema.FieldTypeText, required: true},
	{id: "upload-filesize", name: "filesize", label: "File size", typeName: schema.FieldTypeNumber, required: true},
	{id: "upload-url", name: "url", label: "URL", typeName: schema.FieldTypeText, required: true},
	{id: "upload-object-key", name: "objectKey", label: "Object key", typeName: schema.FieldTypeText, required: true, indexed: true},
	{id: "upload-width", name: "width", label: "Width", typeName: schema.FieldTypeNumber},
	{id: "upload-height", name: "height", label: "Height", typeName: schema.FieldTypeNumber},
	{id: "upload-sizes", name: "sizes", label: "Generated sizes", typeName: schema.FieldTypeJSON},
	{id: "upload-focal-x", name: "focalX", label: "Focal X", typeName: schema.FieldTypeNumber},
	{id: "upload-focal-y", name: "focalY", label: "Focal Y", typeName: schema.FieldTypeNumber},
	{id: "upload-crop-x", name: "cropX", label: "Crop X", typeName: schema.FieldTypeNumber},
	{id: "upload-crop-y", name: "cropY", label: "Crop Y", typeName: schema.FieldTypeNumber},
	{id: "upload-crop-width", name: "cropWidth", label: "Crop width", typeName: schema.FieldTypeNumber},
	{id: "upload-crop-height", name: "cropHeight", label: "Crop height", typeName: schema.FieldTypeNumber},
}

func validateMongoUploadCollectionMetadata(collection schema.Collection) error {
	if collection.Capabilities.Upload != (collection.Upload != nil) {
		return fmt.Errorf("MongoDB collection %q has inconsistent upload capability metadata", collection.ID)
	}
	if collection.Upload == nil {
		for _, field := range collection.Fields {
			if mongoFrameworkUploadMetadataField(field) {
				return fmt.Errorf("MongoDB non-upload collection %q contains framework upload metadata", collection.ID)
			}
		}
		return nil
	}
	settings := collection.Upload
	if settings.MaxFileSize < 1 || settings.MaxFileSize > maxMongoUploadFileBytes {
		return fmt.Errorf("MongoDB upload collection %q has invalid maximum file size", collection.ID)
	}
	if len(settings.MimeTypes) == 0 {
		return fmt.Errorf("MongoDB upload collection %q has no MIME types", collection.ID)
	}
	seenMIME := make(map[string]struct{}, len(settings.MimeTypes))
	for _, mimeType := range settings.MimeTypes {
		if mimeType == "" || mimeType != strings.ToLower(strings.TrimSpace(mimeType)) || !strings.Contains(mimeType, "/") || stringsContainNUL(mimeType) {
			return fmt.Errorf("MongoDB upload collection %q has invalid MIME type metadata", collection.ID)
		}
		if _, duplicate := seenMIME[mimeType]; duplicate {
			return fmt.Errorf("MongoDB upload collection %q repeats MIME type metadata", collection.ID)
		}
		seenMIME[mimeType] = struct{}{}
	}
	if len(settings.ImageSizes) > maxMongoUploadImageSizes {
		return fmt.Errorf("MongoDB upload collection %q exceeds %d image sizes", collection.ID, maxMongoUploadImageSizes)
	}
	seenSizes := make(map[string]struct{}, len(settings.ImageSizes))
	var aggregatePixels int64
	for _, size := range settings.ImageSizes {
		pixels := int64(size.Width) * int64(size.Height)
		if !schema.IsValidPluginKey(size.Name) || size.Width < 1 || size.Height < 1 || size.Width > 20_000 || size.Height > 20_000 || pixels > 40_000_000 || size.Fit != "cover" && size.Fit != "contain" {
			return fmt.Errorf("MongoDB upload collection %q has invalid image-size metadata", collection.ID)
		}
		if _, duplicate := seenSizes[size.Name]; duplicate {
			return fmt.Errorf("MongoDB upload collection %q repeats image-size metadata", collection.ID)
		}
		seenSizes[size.Name] = struct{}{}
		aggregatePixels += pixels
	}
	if aggregatePixels > 100_000_000 {
		return fmt.Errorf("MongoDB upload collection %q exceeds the image-size pixel budget", collection.ID)
	}

	byName := make(map[string]schema.Field, len(collection.Fields))
	for _, field := range collection.Fields {
		if _, duplicate := byName[field.Name]; duplicate {
			return fmt.Errorf("MongoDB upload collection %q repeats field %q", collection.ID, field.Name)
		}
		byName[field.Name] = field
	}
	for _, definition := range mongoUploadMetadataFields {
		field, exists := byName[definition.name]
		if !exists || !matchesMongoUploadMetadataField(field, definition) {
			return fmt.Errorf("MongoDB upload collection %q is missing exact framework metadata field %q", collection.ID, definition.name)
		}
	}
	return nil
}

func expectedMongoUploadMetadataField(definition mongoUploadMetadataField) schema.Field {
	path, _ := query.NewPath(definition.name)
	field := schema.Field{
		ID: definition.id, Name: definition.name, Path: path, Type: definition.typeName,
		Category: schema.FieldCategoryUpload, Required: definition.required, Index: definition.indexed,
		Admin: schema.FieldAdmin{Label: definition.label, ReadOnly: true},
	}
	if definition.typeName == schema.FieldTypeText {
		field.Text = &schema.TextField{}
	}
	return field
}

func mongoFrameworkUploadMetadataField(field schema.Field) bool {
	for _, definition := range mongoUploadMetadataFields {
		if field.Name == definition.name && matchesMongoUploadMetadataField(field, definition) {
			return true
		}
	}
	return false
}

func matchesMongoUploadMetadataField(field schema.Field, definition mongoUploadMetadataField) bool {
	expected := expectedMongoUploadMetadataField(definition)
	// Field-owned presentation and access do not alter the adapter's fixed
	// upload storage contract. Keep the framework's read-only requirement.
	expected.Admin = field.Admin
	expected.Admin.ReadOnly = true
	expected.QueryRestricted = field.QueryRestricted
	return reflect.DeepEqual(field, expected)
}

func validateMongoUploadEnvelope(field schema.Field, path string) error {
	if field.Category != schema.FieldCategoryUpload || field.Upload == nil || field.Relationship != nil {
		return fmt.Errorf("MongoDB upload field %q does not have an upload contract", path)
	}
	upload := field.Upload
	switch upload.OnDelete {
	case schema.ReferenceDeleteNullify, schema.ReferenceDeleteRestrict:
	default:
		return fmt.Errorf("MongoDB upload field %q has unsupported delete action %q", path, upload.OnDelete)
	}
	if field.Required && upload.OnDelete == schema.ReferenceDeleteNullify {
		return fmt.Errorf("MongoDB required upload field %q cannot use nullify on delete", path)
	}
	if !schema.IsValidStableID(string(upload.CollectionID)) || !schema.IsValidCollectionSlug(string(upload.CollectionSlug)) {
		return fmt.Errorf("MongoDB upload field %q has invalid target metadata", path)
	}
	return nil
}

func validateMongoUploadValue(field schema.Field, value store.Value, path string) error {
	if field.Upload == nil {
		return fmt.Errorf("MongoDB upload field %q does not have an upload contract", path)
	}
	items := []store.Value{value}
	if field.Upload.HasMany {
		var valid bool
		items, valid = value.CopyList()
		if !valid {
			return fmt.Errorf("MongoDB upload value %q must be a list", path)
		}
		if field.Required && len(items) == 0 {
			return fmt.Errorf("MongoDB document is missing required field %q", path)
		}
		if len(items) > maxMongoDocumentReferences {
			return fmt.Errorf("MongoDB upload value %q exceeds %d references", path, maxMongoDocumentReferences)
		}
	}
	for index, item := range items {
		itemPath := path
		if field.Upload.HasMany {
			itemPath = fmt.Sprintf("%s.%d", path, index)
		}
		id, valid := item.StringValue()
		if !valid {
			return fmt.Errorf("MongoDB upload value %q must be a document ID string", itemPath)
		}
		if id == "" && !field.Required && !field.Upload.HasMany {
			continue
		}
		if err := store.ValidateDocumentID(id); err != nil {
			return fmt.Errorf("MongoDB upload value %q: %w", itemPath, err)
		}
	}
	return nil
}

func validateMongoUploadSizes(value store.Value, path string) error {
	sizes := value
	if sizes.Kind() != store.ValueObject {
		return fmt.Errorf("MongoDB value %q does not match framework upload-size metadata", path)
	}
	if sizes.Len() > maxMongoUploadImageSizes {
		return fmt.Errorf("MongoDB value %q exceeds %d image variants", path, maxMongoUploadImageSizes)
	}
	names := make([]string, 0, sizes.Len())
	for name := range sizes.Entries() {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "" || !utf8.ValidString(name) || stringsContainNUL(name) {
			return fmt.Errorf("MongoDB value %q contains an invalid image-size name", path)
		}
		metadata := sizes.Get(name)
		if metadata.Kind() != store.ValueObject {
			return fmt.Errorf("MongoDB value %q contains invalid image-size metadata", path)
		}
		if objectKey, exists := metadata.Lookup("objectKey"); exists {
			text, valid := objectKey.StringValue()
			if !valid || text == "" || !utf8.ValidString(text) || stringsContainNUL(text) {
				return fmt.Errorf("MongoDB value %q contains invalid image-size objectKey", path)
			}
		}
		if _, err := encodeValue(metadata); err != nil {
			return fmt.Errorf("MongoDB value %q contains invalid image-size metadata: %w", path, err)
		}
	}
	if _, err := encodeValue(value); err != nil {
		return fmt.Errorf("MongoDB value %q: %w", path, err)
	}
	return nil
}

func mongoUploadSizesField(field schema.Field) bool {
	for _, definition := range mongoUploadMetadataFields {
		if definition.name == "sizes" {
			return matchesMongoUploadMetadataField(field, definition)
		}
	}
	return false
}
