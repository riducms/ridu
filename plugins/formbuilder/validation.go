package formbuilder

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	ridu "github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

var formFieldNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

type formFieldDefinition struct {
	Index int
	Type  FieldType
	Name  string
	Data  store.Values
}

func (plugin *Plugin) validateFormDefinition(context ridu.HookContext, allowedFieldTypes map[FieldType]struct{}) error {
	if context.Operation != operation.Create && context.Operation != operation.Duplicate && context.Operation != operation.Update {
		return nil
	}
	values := store.CloneValues(context.Data)
	if context.Operation == operation.Update && context.Original != nil {
		values = store.CloneValues(context.Original.Values)
		for name, value := range context.Data {
			values[name] = value
		}
	}
	var issues []schema.Issue
	if confirmationType, exists := stringValue(values, "confirmationType"); exists {
		switch confirmationType {
		case "message":
			if message, _ := stringValue(values, "confirmationMessage"); strings.TrimSpace(message) == "" {
				issues = append(issues, schema.Issue{Code: "required", Path: "confirmationMessage", Message: "confirmation message is required when confirmation type is message"})
			}
		case "redirect":
			redirect, _ := objectValue(values, "redirect")
			redirectType, _ := stringValue(redirect, "type")
			if redirectType == "reference" && len(plugin.config.RedirectRelationships) != 0 {
				if reference, exists := redirect["reference"]; !exists {
					issues = append(issues, schema.Issue{Code: "required", Path: "redirect.reference", Message: "redirect reference is required for an internal link"})
				} else if id, valid := relationshipID(reference); !valid || strings.TrimSpace(id) == "" {
					issues = append(issues, schema.Issue{Code: "invalid_relationship", Path: "redirect.reference", Message: "redirect reference must identify a document"})
				}
			} else if redirectURL, _ := stringValue(redirect, "url"); strings.TrimSpace(redirectURL) == "" {
				issues = append(issues, schema.Issue{Code: "required", Path: "redirect.url", Message: "redirect URL is required for a custom redirect"})
			}
		default:
			issues = append(issues, schema.Issue{Code: "invalid_option", Path: "confirmationType", Message: "confirmation type must be message or redirect"})
		}
	}
	definitions, fieldIssues := parseFormFields(values, allowedFieldTypes)
	issues = append(issues, fieldIssues...)
	knownNames := make(map[string]struct{}, len(definitions))
	paymentFields := 0
	for _, definition := range definitions {
		if definition.Type == FieldMessage {
			continue
		}
		path := fmt.Sprintf("fields.%d", definition.Index)
		if !formFieldNamePattern.MatchString(definition.Name) {
			issues = append(issues, schema.Issue{Code: "invalid_form_field_name", Path: path + ".name", Message: "field name must start with a letter or underscore and contain only letters, numbers, underscores, or hyphens"})
		}
		if _, duplicate := knownNames[definition.Name]; duplicate {
			issues = append(issues, schema.Issue{Code: "duplicate_form_field_name", Path: path + ".name", Message: fmt.Sprintf("form field name %q is used more than once", definition.Name)})
		}
		knownNames[definition.Name] = struct{}{}
		if definition.Type == FieldPayment {
			paymentFields++
			if paymentFields > 1 {
				issues = append(issues, schema.Issue{Code: "multiple_payment_fields", Path: path + ".blockType", Message: "a form may contain at most one payment field"})
			}
		}
	}
	for _, definition := range definitions {
		if definition.Type == FieldMessage {
			continue
		}
		issues = append(issues, plugin.validateConfiguredField(definition, knownNames)...)
	}
	if emails, exists := listValue(values, "emails"); exists {
		for index, value := range emails {
			email := value
			if email.Kind() != store.ValueObject {
				continue
			}
			to, _ := email.Get("emailTo").StringValue()
			if strings.TrimSpace(to) == "" && strings.TrimSpace(plugin.config.DefaultToEmail) == "" {
				issues = append(issues, schema.Issue{Code: "required", Path: fmt.Sprintf("emails.%d.emailTo", index), Message: "email recipient is required when no default recipient is configured"})
			}
			from, _ := email.Get("emailFrom").StringValue()
			if strings.TrimSpace(from) == "" {
				issues = append(issues, schema.Issue{Code: "required", Path: fmt.Sprintf("emails.%d.emailFrom", index), Message: "email sender is required"})
			}
		}
	}
	if len(issues) != 0 {
		return schema.NewValidationError(issues)
	}
	return nil
}

func (plugin *Plugin) validateConfiguredField(definition formFieldDefinition, knownNames map[string]struct{}) []schema.Issue {
	path := fmt.Sprintf("fields.%d", definition.Index)
	var issues []schema.Issue
	if width, exists := numberValue(definition.Data, "width"); exists && (width < 1 || width > 100) {
		issues = append(issues, schema.Issue{Code: "invalid_form_field_width", Path: path + ".width", Message: "field width must be between 1 and 100 percent"})
	}
	if definition.Type == FieldSelect || definition.Type == FieldRadio {
		options, _ := listValue(definition.Data, "options")
		seen := make(map[string]struct{}, len(options))
		for optionIndex, optionValue := range options {
			option := optionValue
			if option.Kind() != store.ValueObject {
				continue
			}
			value, _ := option.Get("value").StringValue()
			if _, duplicate := seen[value]; duplicate {
				issues = append(issues, schema.Issue{Code: "duplicate_form_field_option", Path: fmt.Sprintf("%s.options.%d.value", path, optionIndex), Message: fmt.Sprintf("option value %q is used more than once", value)})
			}
			seen[value] = struct{}{}
		}
	}
	if definition.Type == FieldUpload {
		collection, _ := stringValue(definition.Data, "uploadCollection")
		if !slices.Contains(plugin.config.UploadCollections, schema.CollectionSlug(collection)) {
			issues = append(issues, schema.Issue{Code: "invalid_form_upload_collection", Path: path + ".uploadCollection", Message: fmt.Sprintf("upload collection %q is not enabled for this form builder", collection)})
		}
	}
	if definition.Type == FieldPayment {
		processor, _ := stringValue(definition.Data, "paymentProcessor")
		if !optionContains(plugin.config.PaymentProcessors, processor) {
			issues = append(issues, schema.Issue{Code: "invalid_form_payment_processor", Path: path + ".paymentProcessor", Message: fmt.Sprintf("payment processor %q is not configured", processor)})
		}
		conditions, _ := listValue(definition.Data, "priceConditions")
		for conditionIndex, conditionValue := range conditions {
			condition := conditionValue
			if condition.Kind() != store.ValueObject {
				continue
			}
			fieldToUse, _ := condition.Get("fieldToUse").StringValue()
			if _, exists := knownNames[fieldToUse]; !exists {
				issues = append(issues, schema.Issue{Code: "unknown_payment_condition_field", Path: fmt.Sprintf("%s.priceConditions.%d.fieldToUse", path, conditionIndex), Message: fmt.Sprintf("payment condition references unknown earlier field %q", fieldToUse)})
			}
			valueType, _ := condition.Get("valueType").StringValue()
			if valueType == "valueOfField" {
				other, _ := condition.Get("valueForOperator").StringValue()
				if _, exists := knownNames[other]; !exists {
					issues = append(issues, schema.Issue{Code: "unknown_payment_value_field", Path: fmt.Sprintf("%s.priceConditions.%d.valueForOperator", path, conditionIndex), Message: fmt.Sprintf("payment operation references unknown earlier field %q", other)})
				}
			}
		}
	}
	return issues
}

func (plugin *Plugin) validateSubmission(context ridu.HookContext, allowedFieldTypes map[FieldType]struct{}) error {
	if context.Operation != operation.Create {
		return nil
	}
	formID, valid := relationshipID(context.Data["form"])
	if !valid || formID == "" {
		return schema.NewValidationError([]schema.Issue{{Code: "required", Path: "form", Message: "a valid form is required"}})
	}
	form, err := context.Local.Find(context.Context, string(plugin.config.FormsSlug), formID, ridu.FindOptions{
		Actor: context.Actor, ActorCollection: context.ActorCollection, Locale: context.Locale,
	})
	if err != nil {
		var operationError *ridu.OperationError
		if errors.As(err, &operationError) && (operationError.Code == "not_found" || operationError.Code == "access_denied") {
			return schema.NewValidationError([]schema.Issue{{Code: "invalid_form", Path: "form", Message: "form does not exist or is not readable"}})
		}
		return err
	}
	definitions, issues := parseFormFields(form.Values, allowedFieldTypes)
	dataRows, _ := listValue(context.Data, "submissionData")
	values, paths, rowIssues := submissionValues(dataRows)
	issues = append(issues, rowIssues...)
	known := make(map[string]formFieldDefinition, len(definitions))
	for _, definition := range definitions {
		if definition.Type == FieldMessage {
			continue
		}
		known[definition.Name] = definition
	}
	uploadRows, _ := listValue(context.Data, "submissionUploads")
	uploads, uploadPaths, uploadIssues := submissionUploadValues(uploadRows, known)
	issues = append(issues, uploadIssues...)
	for name, path := range paths {
		definition, exists := known[name]
		if !exists {
			issues = append(issues, schema.Issue{Code: "unknown_form_field", Path: path + ".field", Message: fmt.Sprintf("field %q is not part of the selected form", name)})
		} else if definition.Type == FieldUpload {
			issues = append(issues, schema.Issue{Code: "upload_in_submission_data", Path: path + ".field", Message: fmt.Sprintf("upload field %q must be submitted through submissionUploads", name)})
		}
	}
	for name, path := range uploadPaths {
		definition, exists := known[name]
		if !exists || definition.Type != FieldUpload {
			issues = append(issues, schema.Issue{Code: "unknown_form_upload_field", Path: path + ".field", Message: fmt.Sprintf("upload field %q is not part of the selected form", name)})
		}
	}
	for _, definition := range definitions {
		if definition.Type == FieldMessage {
			continue
		}
		required, _ := boolValue(definition.Data, "required")
		if definition.Type == FieldUpload {
			issues = append(issues, plugin.validateSubmittedUpload(context, definition, uploads[definition.Name], uploadPaths[definition.Name], required)...)
			continue
		}
		value, exists := values[definition.Name]
		path := paths[definition.Name]
		if !exists {
			if required {
				issues = append(issues, schema.Issue{Code: "required", Path: "submissionData", Message: fmt.Sprintf("required field %q is missing", definition.Name)})
			}
			continue
		}
		issues = append(issues, validateSubmittedValue(definition, value, path+".value", required)...)
	}
	if len(issues) != 0 {
		return schema.NewValidationError(issues)
	}
	return nil
}

func parseFormFields(values store.Values, allowedFieldTypes map[FieldType]struct{}) ([]formFieldDefinition, []schema.Issue) {
	rows, exists := listValue(values, "fields")
	if !exists {
		return nil, nil
	}
	definitions := make([]formFieldDefinition, 0, len(rows))
	var issues []schema.Issue
	for index, value := range rows {
		row, valid := value.CopyObject()
		if !valid {
			continue
		}
		blockType, _ := stringValue(row, "blockType")
		name, _ := stringValue(row, "name")
		if _, allowed := allowedFieldTypes[FieldType(blockType)]; !allowed {
			issues = append(issues, schema.Issue{Code: "invalid_form_field_type", Path: fmt.Sprintf("fields.%d.blockType", index), Message: fmt.Sprintf("form field type %q is not supported", blockType)})
			continue
		}
		definitions = append(definitions, formFieldDefinition{Index: index, Type: FieldType(blockType), Name: strings.TrimSpace(name), Data: row})
	}
	return definitions, issues
}

func submissionValues(rows []store.Value) (map[string]store.Value, map[string]string, []schema.Issue) {
	values := make(map[string]store.Value, len(rows))
	paths := make(map[string]string, len(rows))
	var issues []schema.Issue
	for index, value := range rows {
		row, valid := value.CopyObject()
		if !valid {
			continue
		}
		name, _ := stringValue(row, "field")
		name = strings.TrimSpace(name)
		path := fmt.Sprintf("submissionData.%d", index)
		if _, duplicate := values[name]; duplicate {
			issues = append(issues, schema.Issue{Code: "duplicate_submission_field", Path: path + ".field", Message: fmt.Sprintf("field %q was submitted more than once", name)})
			continue
		}
		values[name] = row["value"]
		paths[name] = path
	}
	return values, paths, issues
}

type uploadReference struct {
	Collection schema.CollectionSlug
	ID         string
}

func submissionUploadValues(rows []store.Value, definitions map[string]formFieldDefinition) (map[string][]uploadReference, map[string]string, []schema.Issue) {
	values := make(map[string][]uploadReference, len(rows))
	paths := make(map[string]string, len(rows))
	var issues []schema.Issue
	for index, value := range rows {
		row, valid := value.CopyObject()
		if !valid {
			continue
		}
		name, _ := stringValue(row, "field")
		name = strings.TrimSpace(name)
		path := fmt.Sprintf("submissionUploads.%d", index)
		if _, duplicate := values[name]; duplicate {
			issues = append(issues, schema.Issue{Code: "duplicate_submission_upload", Path: path + ".field", Message: fmt.Sprintf("upload field %q was submitted more than once", name)})
			continue
		}
		definition, exists := definitions[name]
		wantCollection, _ := stringValue(definition.Data, "uploadCollection")
		references, valid := uploadReferences(row["value"], schema.CollectionSlug(wantCollection))
		if !valid {
			issues = append(issues, schema.Issue{Code: "invalid_submission_upload", Path: path + ".value", Message: "upload value must use the generated Ridu relationship shape"})
		}
		if !exists || definition.Type != FieldUpload {
			references = nil
		}
		values[name] = references
		paths[name] = path
	}
	return values, paths, issues
}

func (plugin *Plugin) validateSubmittedUpload(context ridu.HookContext, definition formFieldDefinition, references []uploadReference, path string, required bool) []schema.Issue {
	if len(references) == 0 {
		if required {
			return []schema.Issue{{Code: "required", Path: "submissionUploads", Message: fmt.Sprintf("required upload field %q is missing", definition.Name)}}
		}
		return nil
	}
	multiple, _ := boolValue(definition.Data, "multiple")
	if !multiple && len(references) > 1 {
		return []schema.Issue{{Code: "too_many_uploads", Path: path + ".value", Message: fmt.Sprintf("upload field %q accepts one file", definition.Name)}}
	}
	wantCollection, _ := stringValue(definition.Data, "uploadCollection")
	maxSize, _ := numberValue(definition.Data, "maxFileSize")
	allowedMIMEs := mimeTypes(definition.Data)
	var issues []schema.Issue
	for index, reference := range references {
		referencePath := fmt.Sprintf("%s.value.%d", path, index)
		if string(reference.Collection) != wantCollection {
			issues = append(issues, schema.Issue{Code: "invalid_upload_collection", Path: referencePath, Message: fmt.Sprintf("upload field %q stores files in %q", definition.Name, wantCollection)})
			continue
		}
		document, err := context.Local.Find(context.Context, string(reference.Collection), reference.ID, ridu.FindOptions{Actor: context.Actor, ActorCollection: context.ActorCollection})
		if err != nil {
			issues = append(issues, schema.Issue{Code: "invalid_upload", Path: referencePath, Message: "uploaded document does not exist or is not readable"})
			continue
		}
		mimeType, mimeExists := stringValue(document.Values, "mimeType")
		if len(allowedMIMEs) != 0 && (!mimeExists || !mimeAllowed(mimeType, allowedMIMEs)) {
			issues = append(issues, schema.Issue{Code: "invalid_upload_mime_type", Path: referencePath, Message: fmt.Sprintf("MIME type %q is not allowed", mimeType)})
		}
		fileSize, fileSizeExists := numberValue(document.Values, "filesize")
		if maxSize > 0 {
			if !fileSizeExists || fileSize < 0 {
				issues = append(issues, schema.Issue{Code: "invalid_upload_metadata", Path: referencePath, Message: "uploaded document does not expose a valid file size"})
			} else if fileSize > maxSize {
				issues = append(issues, schema.Issue{Code: "upload_too_large", Path: referencePath, Message: fmt.Sprintf("file exceeds the %.0f byte limit", maxSize)})
			}
		}
	}
	return issues
}

func validateSubmittedValue(definition formFieldDefinition, value store.Value, path string, required bool) []schema.Issue {
	if value.Kind() == store.ValueNull {
		if required {
			return []schema.Issue{{Code: "required", Path: path, Message: fmt.Sprintf("field %q is required", definition.Name)}}
		}
		return nil
	}
	switch definition.Type {
	case FieldText, FieldTextarea, FieldCountry, FieldState, FieldSelect, FieldRadio, FieldEmail, FieldDate:
		text, valid := value.StringValue()
		if !valid {
			return []schema.Issue{{Code: "invalid_type", Path: path, Message: fmt.Sprintf("field %q must be a string", definition.Name)}}
		}
		if required && strings.TrimSpace(text) == "" {
			return []schema.Issue{{Code: "required", Path: path, Message: fmt.Sprintf("field %q is required", definition.Name)}}
		}
		if definition.Type == FieldEmail && strings.TrimSpace(text) != "" {
			if _, err := mail.ParseAddress(text); err != nil {
				return []schema.Issue{{Code: "invalid_email", Path: path, Message: fmt.Sprintf("field %q must be a valid email address", definition.Name)}}
			}
		}
		if definition.Type == FieldDate && strings.TrimSpace(text) != "" && !validDate(text) {
			return []schema.Issue{{Code: "invalid_date", Path: path, Message: fmt.Sprintf("field %q must be an ISO date or RFC 3339 timestamp", definition.Name)}}
		}
		if definition.Type == FieldSelect || definition.Type == FieldRadio {
			if !configuredOptionContains(definition.Data, text) {
				return []schema.Issue{{Code: "invalid_option", Path: path, Message: fmt.Sprintf("field %q contains an unavailable option", definition.Name)}}
			}
		}
	case FieldNumber, FieldPayment:
		if _, valid := value.NumberValue(); !valid {
			return []schema.Issue{{Code: "invalid_type", Path: path, Message: fmt.Sprintf("field %q must be a number", definition.Name)}}
		}
	case FieldCheckbox:
		checked, valid := value.BooleanValue()
		if !valid {
			return []schema.Issue{{Code: "invalid_type", Path: path, Message: fmt.Sprintf("field %q must be a boolean", definition.Name)}}
		}
		if required && !checked {
			return []schema.Issue{{Code: "required", Path: path, Message: fmt.Sprintf("field %q must be checked", definition.Name)}}
		}
	default:
		if required {
			if text, valid := value.StringValue(); valid && strings.TrimSpace(text) == "" {
				return []schema.Issue{{Code: "required", Path: path, Message: fmt.Sprintf("field %q is required", definition.Name)}}
			}
			if value.Kind() == store.ValueList && value.Len() == 0 {
				return []schema.Issue{{Code: "required", Path: path, Message: fmt.Sprintf("field %q is required", definition.Name)}}
			}
		}
	}
	return nil
}

func relationshipID(value store.Value) (string, bool) {
	if text, valid := value.StringValue(); valid {
		return text, true
	}
	if document, valid := value.CopyDocument(); valid {
		return document.ID, true
	}
	return "", false
}

func uploadReferences(value store.Value, defaultCollection schema.CollectionSlug) ([]uploadReference, bool) {
	if value.Kind() != store.ValueList {
		return nil, false
	}
	references := make([]uploadReference, 0, value.Len())
	for item := range value.Elements() {
		if id, valid := relationshipID(item); valid && id != "" && defaultCollection != "" {
			references = append(references, uploadReference{Collection: defaultCollection, ID: id})
			continue
		}
		if item.Kind() != store.ValueObject {
			return references, false
		}
		collection, collectionValid := item.Get("relationTo").StringValue()
		id, idValid := relationshipID(item.Get("id"))
		if !collectionValid || !idValid || collection == "" || id == "" {
			return references, false
		}
		references = append(references, uploadReference{Collection: schema.CollectionSlug(collection), ID: id})
	}
	return references, true
}

func mimeTypes(values store.Values) []string {
	rows, _ := listValue(values, "mimeTypes")
	result := make([]string, 0, len(rows))
	for _, rowValue := range rows {
		row := rowValue
		if row.Kind() != store.ValueObject {
			continue
		}
		if value, exists := row.Get("mimeType").StringValue(); exists && strings.TrimSpace(value) != "" {
			result = append(result, strings.ToLower(strings.TrimSpace(value)))
		}
	}
	return result
}

func mimeAllowed(candidate string, allowed []string) bool {
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	for _, pattern := range allowed {
		if pattern == candidate || (strings.HasSuffix(pattern, "/*") && strings.HasPrefix(candidate, strings.TrimSuffix(pattern, "*"))) {
			return true
		}
	}
	return false
}

func configuredOptionContains(values store.Values, candidate string) bool {
	rows, _ := listValue(values, "options")
	for _, rowValue := range rows {
		row := rowValue
		if row.Kind() != store.ValueObject {
			continue
		}
		if value, _ := row.Get("value").StringValue(); value == candidate {
			return true
		}
	}
	return false
}

func optionContains(options []field.Option, candidate string) bool {
	for _, option := range options {
		if option.Value == candidate {
			return true
		}
	}
	return false
}

func stringValue(values store.Values, key string) (string, bool) {
	value, exists := values[key]
	if !exists {
		return "", false
	}
	return value.StringValue()
}

func numberValue(values store.Values, key string) (float64, bool) {
	value, exists := values[key]
	if !exists {
		return 0, false
	}
	return value.NumberValue()
}

func boolValue(values store.Values, key string) (bool, bool) {
	value, exists := values[key]
	if !exists {
		return false, false
	}
	return value.BooleanValue()
}

func objectValue(values store.Values, key string) (store.Values, bool) {
	value, exists := values[key]
	if !exists {
		return nil, false
	}
	return value.CopyObject()
}

func listValue(values store.Values, key string) ([]store.Value, bool) {
	value, exists := values[key]
	if !exists {
		return nil, false
	}
	return value.CopyList()
}

func validDate(value string) bool {
	for _, layout := range []string{time.DateOnly, time.RFC3339, "2006-01-02T15:04"} {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

func numberFromValue(value store.Value) (float64, bool) {
	if number, valid := value.NumberValue(); valid {
		return number, true
	}
	if text, valid := value.StringValue(); valid {
		number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		return number, err == nil
	}
	return 0, false
}
