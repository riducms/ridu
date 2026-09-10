package operation

import (
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"math"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func validate(fields []schema.Field, values store.Values, create bool, pluginValidators map[string]PluginValidator) (store.Values, []schema.Issue) {
	return validateWithOptions(fields, values, validationOptions{requireMissing: create}, pluginValidators)
}

func validateWithOptions(fields []schema.Field, values store.Values, options validationOptions, pluginValidators map[string]PluginValidator) (store.Values, []schema.Issue) {
	issues := &validationIssueCollector{}
	if options.budget == nil {
		options.budget = embedded.NewBudget()
	}
	validated := validateFields(fields, values, options, pluginValidators, issues)
	return validated, issues.values
}

// MaxValidationIssues bounds error-envelope construction for malformed nested
// documents. Validation stops once the cap is reached because the mutation is
// already inadmissible and collecting more paths only consumes resources.
const MaxValidationIssues = 128

type validationIssueCollector struct {
	values []schema.Issue
}

func (collector *validationIssueCollector) add(additions ...schema.Issue) {
	remaining := MaxValidationIssues - len(collector.values)
	if remaining <= 0 || len(additions) == 0 {
		return
	}
	if len(additions) > remaining {
		additions = additions[:remaining]
	}
	collector.values = append(collector.values, additions...)
}

func (collector *validationIssueCollector) full() bool {
	return len(collector.values) >= MaxValidationIssues
}

type validationOptions struct {
	prefix         string
	requireMissing bool
	// previous is the stored occurrence projected to the exact write locale.
	// A nil map means a new occurrence, whose missing children need validation.
	previous store.Values
	// Recursive scopes borrow immutable prior rows instead of detaching their maps.
	previousValue store.Value
	previousScope bool
	budget        *embedded.Budget
	// Ordinary row metadata belongs only to its immediate array/block container.
	rowMetadata      schema.FieldType
	embeddedMetadata [2]string
}

func (options validationOptions) retained() bool {
	return options.previous != nil || options.previousValue.Kind() == store.ValueObject || options.previousScope
}

func (options validationOptions) prior(name string) store.Value {
	if options.previous != nil {
		return options.previous[name]
	}
	value, _ := options.previousValue.Lookup(name)
	return value
}

// validationObject borrows its immutable source for this synchronous traversal.
// Only a mutation creates a detached map; unchanged nested objects retain storage.
type validationObject struct {
	values store.Values
	value  store.Value
	edited store.Values
}

func (object *validationObject) lookup(name string) (store.Value, bool) {
	if object.values != nil {
		value, exists := object.values[name]
		return value, exists
	}
	return object.value.Lookup(name)
}

func (object *validationObject) entries() iter.Seq2[string, store.Value] {
	if object.values != nil {
		return maps.All(object.values)
	}
	return object.value.Entries()
}

func (object *validationObject) copy() store.Values {
	if object.values != nil {
		return store.CloneValues(object.values)
	}
	values, _ := object.value.CopyObject()
	return values
}

func (object *validationObject) set(name string, value store.Value) {
	if object.edited == nil {
		object.edited = object.copy()
	}
	object.edited[name] = value
}

func (object *validationObject) remove(name string) {
	if _, exists := object.lookup(name); !exists {
		return
	}
	if object.edited == nil {
		object.edited = object.copy()
	}
	delete(object.edited, name)
}

func (object *validationObject) result() (store.Value, bool) {
	if object.edited == nil {
		return object.value, false
	}
	return store.Object(object.edited), true
}

func validateObject(fields []schema.Field, value store.Value, options validationOptions, pluginValidators map[string]PluginValidator, issues *validationIssueCollector) (store.Value, bool) {
	object := validationObject{value: value}
	validateMembers(fields, &object, options, pluginValidators, issues)
	return object.result()
}

func validateFields(fields []schema.Field, values store.Values, options validationOptions, pluginValidators map[string]PluginValidator, issues *validationIssueCollector) store.Values {
	object := validationObject{values: values}
	validateMembers(fields, &object, options, pluginValidators, issues)
	if object.edited != nil {
		return object.edited
	}
	return store.CloneValues(values)
}

func validateMembers(fields []schema.Field, object *validationObject, options validationOptions, pluginValidators map[string]PluginValidator, issues *validationIssueCollector) {
	byName := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation {
			continue
		}
		byName[field.Name] = struct{}{}
	}
	for name := range object.entries() {
		if issues.full() {
			return
		}
		if name == "_key" && (options.rowMetadata == schema.FieldTypeArray || options.rowMetadata == schema.FieldTypeBlocks) ||
			name == "blockType" && options.rowMetadata == schema.FieldTypeBlocks {
			continue
		}
		if options.embeddedMetadata[0] != "" && name == options.embeddedMetadata[0] || options.embeddedMetadata[1] != "" && name == options.embeddedMetadata[1] {
			continue
		}
		if _, exists := byName[name]; !exists {
			issues.add(schema.Issue{Code: "unknown_field", Path: joinFieldPath(options.prefix, name), Message: fmt.Sprintf("field %q is not defined", name)})
		}
	}
	for _, field := range fields {
		if issues.full() {
			return
		}
		if field.Category == schema.FieldCategoryPresentation {
			object.remove(field.Name)
			continue
		}
		path := joinFieldPath(options.prefix, field.Name)
		value, exists := object.lookup(field.Name)
		// An omitted translation on a retained occurrence remains absent. Neither
		// defaults nor fallback values may become a translation during a sibling edit.
		if !exists && field.Localized && options.retained() {
			continue
		}
		if !exists && options.requireMissing {
			if materialized, present := missingDefaultValue(field); present {
				value, exists = materialized, true
				object.set(field.Name, value)
			}
		}
		if !exists {
			if options.requireMissing && field.Required {
				issues.add(requiredIssue(field, path))
			} else if options.requireMissing && field.List != nil && field.List.MinRows > 0 {
				issues.add(schema.Issue{Code: "min_rows", Path: path, Message: fmt.Sprintf("%s must contain at least %d items", field.Admin.Label, field.List.MinRows)})
			} else if options.requireMissing && field.Type == schema.FieldTypeArray && field.Nested != nil && field.Nested.MinRows > 0 {
				issues.add(schema.Issue{Code: "min_rows", Path: path, Message: fmt.Sprintf("%s must contain at least %d rows", field.Admin.Label, field.Nested.MinRows)})
			}
			continue
		}
		if value.Kind() == store.ValueNull {
			if field.Required {
				issues.add(requiredIssue(field, path))
			} else if field.List != nil && field.List.MinRows > 0 {
				issues.add(schema.Issue{Code: "min_rows", Path: path, Message: fmt.Sprintf("%s must contain at least %d items", field.Admin.Label, field.List.MinRows)})
			} else if field.Type == schema.FieldTypeArray && field.Nested != nil && field.Nested.MinRows > 0 {
				issues.add(schema.Issue{Code: "min_rows", Path: path, Message: fmt.Sprintf("%s must contain at least %d rows", field.Admin.Label, field.Nested.MinRows)})
			}
			continue
		}
		switch field.Type {
		case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeCode:
			text, valid := value.StringValue()
			if !valid {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be a string", field.Admin.Label)})
			} else if field.Required && text == "" {
				issues.add(requiredIssue(field, path))
			} else {
				minLength, maxLength := stringLengthConstraints(field)
				length := utf8.RuneCountInString(text)
				if minLength != nil && length < *minLength {
					issues.add(schema.Issue{Code: "min_length", Path: path, Message: fmt.Sprintf("%s must contain at least %d characters", field.Admin.Label, *minLength)})
				}
				if maxLength != nil && length > *maxLength {
					issues.add(schema.Issue{Code: "max_length", Path: path, Message: fmt.Sprintf("%s must contain no more than %d characters", field.Admin.Label, *maxLength)})
				}
			}
		case schema.FieldTypeTextList, schema.FieldTypeNumberList:
			validatePrimitiveList(field, value, path, issues)
		case schema.FieldTypeRelationship:
			issues.add(validateRelationship(field, value, path)...)
		case schema.FieldTypeUpload:
			issues.add(validateUpload(field, value, path)...)
		case schema.FieldTypeEmail:
			text, valid := value.StringValue()
			if !valid {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be a string", field.Admin.Label)})
			} else if text == "" {
				if field.Required {
					issues.add(requiredIssue(field, path))
				}
			} else if address, err := mail.ParseAddress(text); err != nil || address.Address != text {
				issues.add(schema.Issue{Code: "invalid_email", Path: path, Message: fmt.Sprintf("%s must be a valid email address", field.Admin.Label)})
			}
		case schema.FieldTypeDate:
			text, valid := value.StringValue()
			if !valid {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be a string", field.Admin.Label)})
			} else if text == "" {
				if field.Required {
					issues.add(requiredIssue(field, path))
				}
			} else if !validDateValue(field, text) {
				issues.add(schema.Issue{Code: "invalid_date", Path: path, Message: fmt.Sprintf("%s must match its configured date format", field.Admin.Label)})
			}
		case schema.FieldTypeNumber:
			number, valid := value.NumberValue()
			if !valid || math.IsNaN(number) || math.IsInf(number, 0) {
				issues.add(schema.Issue{Code: "invalid_number", Path: path, Message: fmt.Sprintf("%s must be a finite number", field.Admin.Label)})
			} else if field.Number != nil {
				if field.Number.Min != nil && number < *field.Number.Min {
					issues.add(schema.Issue{Code: "min_value", Path: path, Message: fmt.Sprintf("%s must be at least %s", field.Admin.Label, strconv.FormatFloat(*field.Number.Min, 'g', -1, 64))})
				}
				if field.Number.Max != nil && number > *field.Number.Max {
					issues.add(schema.Issue{Code: "max_value", Path: path, Message: fmt.Sprintf("%s must be no more than %s", field.Admin.Label, strconv.FormatFloat(*field.Number.Max, 'g', -1, 64))})
				}
			}
		case schema.FieldTypeCheckbox:
			if _, valid := value.BooleanValue(); !valid {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be a boolean", field.Admin.Label)})
			}
		case schema.FieldTypeJSON:
			if value.Kind() == store.ValueDocument {
				issues.add(schema.Issue{Code: "invalid_json", Path: path, Message: fmt.Sprintf("%s must contain JSON data", field.Admin.Label)})
			}
		case schema.FieldTypeSelect, schema.FieldTypeRadio:
			if field.Type == schema.FieldTypeSelect && field.Select != nil && field.Select.HasMany {
				if value.Kind() != store.ValueList {
					issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)})
					continue
				}
				if field.Required && value.Len() == 0 {
					issues.add(requiredIssue(field, path))
				}
				seen := make(map[string]bool, value.Len())
				index := -1
				for item := range value.Elements() {
					index++
					itemPath := fmt.Sprintf("%s.%d", path, index)
					text, valid := item.StringValue()
					if !valid {
						issues.add(schema.Issue{Code: "invalid_type", Path: itemPath, Message: fmt.Sprintf("%s options must be strings", field.Admin.Label)})
						continue
					}
					if seen[text] {
						issues.add(schema.Issue{Code: "duplicate_option", Path: itemPath, Message: fmt.Sprintf("%s contains the same option more than once", field.Admin.Label)})
						continue
					}
					seen[text] = true
					found := false
					for _, option := range field.Select.Options {
						found = found || option.Value == text
					}
					if !found {
						issues.add(schema.Issue{Code: "invalid_option", Path: itemPath, Message: fmt.Sprintf("%s is not an allowed option", field.Admin.Label)})
					}
				}
				continue
			}
			text, valid := value.StringValue()
			if !valid {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be a string", field.Admin.Label)})
				continue
			}
			if text == "" {
				if field.Required {
					issues.add(requiredIssue(field, path))
				}
				continue
			}
			found := false
			for _, option := range field.Select.Options {
				found = found || option.Value == text
			}
			if !found {
				issues.add(schema.Issue{Code: "invalid_option", Path: path, Message: fmt.Sprintf("%s is not an allowed option", field.Admin.Label)})
			}
		case schema.FieldTypePoint:
			if value.Kind() != store.ValueList || value.Len() != 2 {
				issues.add(schema.Issue{Code: "invalid_point", Path: path, Message: fmt.Sprintf("%s must contain longitude and latitude", field.Admin.Label)})
				continue
			}
			longitudeValue, _ := value.ListItem(0)
			latitudeValue, _ := value.ListItem(1)
			longitude, longitudeValid := longitudeValue.NumberValue()
			latitude, latitudeValid := latitudeValue.NumberValue()
			if !longitudeValid || !latitudeValid || math.IsNaN(longitude) || math.IsNaN(latitude) || math.IsInf(longitude, 0) || math.IsInf(latitude, 0) || longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90 {
				issues.add(schema.Issue{Code: "invalid_point", Path: path, Message: fmt.Sprintf("%s must be a valid [longitude, latitude] pair", field.Admin.Label)})
			}
		case schema.FieldTypeGroup:
			if value.Kind() != store.ValueObject || field.Nested == nil {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an object", field.Admin.Label)})
				continue
			}
			child, changed := validateObject(field.Nested.ResolvedFields(), value, validationOptions{prefix: path, requireMissing: true, previousValue: options.prior(field.Name), budget: options.budget}, pluginValidators, issues)
			if changed {
				object.set(field.Name, child)
			}
		case schema.FieldTypeArray:
			if value.Kind() != store.ValueList || field.Nested == nil {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)})
				continue
			}
			if field.Required && value.Len() == 0 {
				issues.add(schema.Issue{Code: "required", Path: path, Message: fmt.Sprintf("%s must contain at least one row", field.Admin.Label)})
			}
			if value.Len() < field.Nested.MinRows && !(field.Required && value.Len() == 0) {
				issues.add(schema.Issue{Code: "min_rows", Path: path, Message: fmt.Sprintf("%s must contain at least %d rows", field.Admin.Label, field.Nested.MinRows)})
			}
			if field.Nested.MaxRows > 0 && value.Len() > field.Nested.MaxRows {
				issues.add(schema.Issue{Code: "max_rows", Path: path, Message: fmt.Sprintf("%s must contain no more than %d rows", field.Admin.Label, field.Nested.MaxRows)})
			}
			var validatedItems []store.Value
			rowKeys := make(map[string]int, value.Len())
			previousRows := validationRowsByKey(options.prior(field.Name))
			index := -1
			for item := range value.Elements() {
				index++
				if issues.full() {
					clearValidationTail(value, &validatedItems, index)
					break
				}
				itemPath := fmt.Sprintf("%s.%d", path, index)
				if item.Kind() != store.ValueObject {
					issues.add(schema.Issue{Code: "invalid_type", Path: itemPath, Message: "array row must be an object"})
					replaceValidationItem(value, &validatedItems, index, store.Value{})
					continue
				}
				key, _ := item.Get("_key").StringValue()
				child, changed := validateObject(field.Nested.ResolvedFields(), item, validationOptions{prefix: itemPath, requireMissing: true, previousValue: previousRows[key], budget: options.budget, rowMetadata: schema.FieldTypeArray}, pluginValidators, issues)
				keyValue, exists := item.Lookup("_key")
				issues.add(validateRowKeyValue(keyValue, exists, rowKeys, itemPath, index)...)
				if changed {
					replaceValidationItem(value, &validatedItems, index, child)
				}
			}
			if validatedItems != nil {
				object.set(field.Name, store.List(validatedItems...))
			}
		case schema.FieldTypeBlocks:
			if value.Kind() != store.ValueList || field.Blocks == nil {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)})
				continue
			}
			if field.Required && value.Len() == 0 {
				issues.add(schema.Issue{Code: "required", Path: path, Message: fmt.Sprintf("%s must contain at least one block", field.Admin.Label)})
			}
			if value.Len() < field.Blocks.MinRows && !(field.Required && value.Len() == 0) {
				issues.add(schema.Issue{Code: "min_rows", Path: path, Message: fmt.Sprintf("%s must contain at least %d blocks", field.Admin.Label, field.Blocks.MinRows)})
			}
			if field.Blocks.MaxRows > 0 && value.Len() > field.Blocks.MaxRows {
				issues.add(schema.Issue{Code: "max_rows", Path: path, Message: fmt.Sprintf("%s must contain no more than %d blocks", field.Admin.Label, field.Blocks.MaxRows)})
			}
			var validatedItems []store.Value
			rowKeys := make(map[string]int, value.Len())
			previousRows := validationRowsByKey(options.prior(field.Name))
			index := -1
			for item := range value.Elements() {
				index++
				if issues.full() {
					clearValidationTail(value, &validatedItems, index)
					break
				}
				itemPath := fmt.Sprintf("%s.%d", path, index)
				if item.Kind() != store.ValueObject {
					issues.add(schema.Issue{Code: "invalid_type", Path: itemPath, Message: "block must be an object"})
					replaceValidationItem(value, &validatedItems, index, store.Value{})
					continue
				}
				blockTypeValue, exists := item.Lookup("blockType")
				blockKey, valid := blockTypeValue.StringValue()
				block := findBlock(field.Blocks.ResolvedTypes(), blockKey)
				if !exists || !valid || block == nil {
					issues.add(schema.Issue{Code: "invalid_block", Path: itemPath + ".blockType", Message: "block type is not allowed"})
					replaceValidationItem(value, &validatedItems, index, store.Value{})
					continue
				}
				key, _ := item.Get("_key").StringValue()
				previous := previousRows[key]
				if previousType, _ := previous.Get("blockType").StringValue(); previousType != blockKey {
					previous = store.Value{}
				}
				child, changed := validateObject(block.ResolvedFields(), item, validationOptions{prefix: itemPath, requireMissing: true, previousValue: previous, budget: options.budget, rowMetadata: schema.FieldTypeBlocks}, pluginValidators, issues)
				keyValue, exists := item.Lookup("_key")
				issues.add(validateRowKeyValue(keyValue, exists, rowKeys, itemPath, index)...)
				if changed {
					replaceValidationItem(value, &validatedItems, index, child)
				}
			}
			if validatedItems != nil {
				object.set(field.Name, store.List(validatedItems...))
			}
		case schema.FieldTypePlugin:
			if field.Plugin == nil {
				issues.add(schema.Issue{Code: "invalid_plugin_field", Path: path, Message: "plugin field contract is missing"})
				continue
			}
			validator := pluginValidators[field.Plugin.Key]
			if validator == nil {
				issues.add(schema.Issue{Code: "missing_plugin_validator", Path: path, Message: fmt.Sprintf("plugin field %q has no runtime validator", field.Plugin.Key)})
				continue
			}
			issues.add(validator(field, value, path)...)
			if embedded.HasFields(field) && !issues.full() {
				previous := validationEmbeddedByIdentity(field, options.prior(field.Name), path, options.budget)
				changed := false
				updated, err := embedded.TransformValue(field, value, path, options.budget, func(occurrence embedded.ReadOccurrence) (store.Value, bool, error) {
					// Envelope identity/discriminator are metadata, not authored children.
					metadata := [2]string{occurrence.Case.Identity, occurrence.Case.Discriminator}
					child, childChanged := validateObject(occurrence.Fields, occurrence.Payload, validationOptions{prefix: occurrence.RuntimePath, requireMissing: true, previousValue: previous[occurrence.Identity], budget: options.budget, embeddedMetadata: metadata}, pluginValidators, issues)
					// Legacy validation restores both metadata entries even when absent.
					for _, name := range metadata {
						if _, present := occurrence.Payload.Lookup(name); !present {
							members := validationObject{value: child}
							members.set(name, store.Value{})
							child, childChanged = members.result()
						}
					}
					changed = changed || childChanged
					return child, childChanged, nil
				})
				if err != nil {
					if failure, ok := err.(*embedded.Error); ok {
						issues.add(failure.Issue)
					} else {
						issues.add(schema.Issue{Code: "invalid_embedded", Path: path, Message: err.Error()})
					}
				} else if changed {
					object.set(field.Name, updated)
				}
			}
		}
	}
}

// Lists allocate replacement storage only after a child actually changes.
func replaceValidationItem(value store.Value, items *[]store.Value, index int, replacement store.Value) {
	if *items == nil {
		*items, _ = value.CopyList()
	}
	(*items)[index] = replacement
}

// Failed validation historically leaves unvisited result rows at the zero value.
func clearValidationTail(value store.Value, items *[]store.Value, index int) {
	if *items == nil {
		*items, _ = value.CopyList()
	}
	clear((*items)[index:])
}

func validationRowsByKey(value store.Value) map[string]store.Value {
	if value.Kind() != store.ValueList {
		return nil
	}
	byKey := make(map[string]store.Value, value.Len())
	for row := range value.Elements() {
		key, _ := row.Get("_key").StringValue()
		if row.Kind() == store.ValueObject && key != "" {
			byKey[key] = row
		}
	}
	return byKey
}

func validationEmbeddedByIdentity(field schema.Field, value store.Value, path string, budget *embedded.Budget) map[string]store.Value {
	previous := map[string]store.Value{}
	err := embedded.Visit(field, value, path, budget, func(occurrence embedded.ReadOccurrence) error {
		previous[occurrence.Identity] = occurrence.Payload
		return nil
	})
	if err != nil {
		return nil
	}
	return previous
}

func stringLengthConstraints(field schema.Field) (*int, *int) {
	switch field.Type {
	case schema.FieldTypeText:
		if field.Text != nil {
			return field.Text.MinLength, field.Text.MaxLength
		}
	case schema.FieldTypeTextarea:
		if field.Textarea != nil {
			return field.Textarea.MinLength, field.Textarea.MaxLength
		}
	case schema.FieldTypeCode:
		if field.Code != nil {
			return field.Code.MinLength, field.Code.MaxLength
		}
	}
	return nil, nil
}

func validateRowKey(input, validated store.Values, seen map[string]int, rowPath string, rowIndex int) []schema.Issue {
	value, exists := input["_key"]
	issues := validateRowKeyValue(value, exists, seen, rowPath, rowIndex)
	if exists && len(issues) == 0 {
		validated["_key"] = value
	}
	return issues
}

func validateRowKeyValue(value store.Value, exists bool, seen map[string]int, rowPath string, rowIndex int) []schema.Issue {
	if !exists {
		return nil
	}
	key, valid := value.StringValue()
	if !valid || strings.TrimSpace(key) == "" {
		return []schema.Issue{{Code: "invalid_row_key", Path: rowPath + "._key", Message: "row key must be a non-empty string"}}
	}
	if firstIndex, duplicate := seen[key]; duplicate {
		return []schema.Issue{{
			Code: "duplicate_row_key", Path: rowPath + "._key",
			Message: fmt.Sprintf("row key %q duplicates row %d", key, firstIndex),
		}}
	}
	seen[key] = rowIndex
	return nil
}

func validDateValue(field schema.Field, value string) bool {
	if field.Date == nil {
		_, err := time.Parse("2006-01-02", value)
		return err == nil
	}
	switch field.Date.Format {
	case schema.DateOnly:
		_, err := time.Parse("2006-01-02", value)
		return err == nil
	case schema.DateTime:
		_, err := time.Parse(time.RFC3339, value)
		return err == nil
	case schema.TimeOnly:
		if _, err := time.Parse("15:04", value); err == nil {
			return true
		}
		_, err := time.Parse("15:04:05", value)
		return err == nil
	default:
		return false
	}
}

func requiredIssue(field schema.Field, path string) schema.Issue {
	return schema.Issue{Code: "required", Path: path, Message: fmt.Sprintf("%s is required", field.Admin.Label)}
}

func validateUpload(field schema.Field, value store.Value, path string) []schema.Issue {
	if field.Upload == nil {
		return []schema.Issue{{Code: "invalid_upload", Path: path, Message: "upload contract is missing"}}
	}
	if field.Upload.HasMany {
		if value.Kind() != store.ValueList {
			return []schema.Issue{{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)}}
		}
		if field.Required && value.Len() == 0 {
			return []schema.Issue{{Code: "required", Path: path, Message: fmt.Sprintf("%s must contain at least one upload", field.Admin.Label)}}
		}
		if value.Len() > MaxDocumentReferences {
			return []schema.Issue{*maxDocumentReferencesIssue(path)}
		}
	}
	var issues []schema.Issue
	index := -1
	for item := range referenceElements(value, field.Upload.HasMany) {
		index++
		if len(issues) >= MaxValidationIssues {
			break
		}
		id, valid := item.StringValue()
		if valid && id == "" && !field.Upload.HasMany {
			if field.Required {
				issues = append(issues, requiredIssue(field, path))
			}
			continue
		}
		if !valid || id == "" {
			itemPath := path
			if field.Upload.HasMany {
				itemPath = fmt.Sprintf("%s.%d", path, index)
			}
			issues = append(issues, schema.Issue{Code: "invalid_upload", Path: itemPath, Message: "upload reference must be a non-empty ID string"})
		}
	}
	return issues
}

func validateRelationship(field schema.Field, value store.Value, path string) []schema.Issue {
	if field.Relationship == nil {
		return []schema.Issue{{Code: "invalid_relationship", Path: path, Message: "relationship contract is missing"}}
	}
	if field.Relationship.HasMany {
		if value.Kind() != store.ValueList {
			return []schema.Issue{{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)}}
		}
		if field.Required && value.Len() == 0 {
			return []schema.Issue{{Code: "required", Path: path, Message: fmt.Sprintf("%s must contain at least one relationship", field.Admin.Label)}}
		}
		if value.Len() > MaxDocumentReferences {
			return []schema.Issue{*maxDocumentReferencesIssue(path)}
		}
	}
	var issues []schema.Issue
	index := -1
	for item := range referenceElements(value, field.Relationship.HasMany) {
		index++
		if len(issues) >= MaxValidationIssues {
			break
		}
		itemPath := path
		if field.Relationship.HasMany {
			itemPath = fmt.Sprintf("%s.%d", path, index)
		}
		if !field.Relationship.Polymorphic {
			id, valid := item.StringValue()
			if valid && id == "" && !field.Relationship.HasMany {
				if field.Required {
					issues = append(issues, requiredIssue(field, path))
				}
				continue
			}
			if !valid || id == "" {
				issues = append(issues, schema.Issue{Code: "invalid_relationship", Path: itemPath, Message: "relationship reference must be a non-empty ID string"})
			}
			continue
		}
		if item.Kind() != store.ValueObject {
			issues = append(issues, schema.Issue{Code: "invalid_relationship", Path: itemPath, Message: "polymorphic relationship reference must be an object"})
			continue
		}
		slug, slugValid := item.Get("relationTo").StringValue()
		id, idValid := item.Get("id").StringValue()
		if !slugValid || !idValid || id == "" || !relationshipTargetExists(field.Relationship.Targets, slug) {
			issues = append(issues, schema.Issue{Code: "invalid_relationship", Path: itemPath, Message: "polymorphic reference requires an allowed relationTo and non-empty ID"})
		}
	}
	return issues
}

func relationshipTargetExists(targets []schema.RelationshipTarget, slug string) bool {
	for _, target := range targets {
		if string(target.CollectionSlug) == slug {
			return true
		}
	}
	return false
}

func defaultValue(field schema.Field, value string) store.Value {
	switch field.Type {
	case schema.FieldTypeTextList, schema.FieldTypeNumberList:
		var decoded store.Value
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			return decoded
		}
		return store.Null()
	case schema.FieldTypeNumber:
		parsed, _ := strconv.ParseFloat(value, 64)
		return store.Number(parsed)
	case schema.FieldTypeCheckbox:
		parsed, _ := strconv.ParseBool(value)
		return store.Boolean(parsed)
	default:
		return store.String(value)
	}
}

func missingDefaultValue(field schema.Field) (store.Value, bool) {
	if field.Default != nil {
		return defaultValue(field, *field.Default), true
	}
	if field.Select != nil && field.Select.HasMany && len(field.Select.DefaultValues) != 0 {
		defaults := make([]store.Value, len(field.Select.DefaultValues))
		for index, option := range field.Select.DefaultValues {
			defaults[index] = store.String(option)
		}
		return store.List(defaults...), true
	}
	if field.Type != schema.FieldTypeGroup || field.Nested == nil {
		return store.Value{}, false
	}
	values := store.Values{}
	for _, child := range field.Nested.ResolvedFields() {
		if child.Category == schema.FieldCategoryPresentation {
			continue
		}
		if value, present := missingDefaultValue(child); present {
			values[child.Name] = value
		}
	}
	if len(values) == 0 {
		return store.Value{}, false
	}
	return store.Object(values), true
}

func findBlock(blocks []schema.BlockType, key string) *schema.BlockType {
	for index := range blocks {
		if blocks[index].Slug == key {
			return &blocks[index]
		}
	}
	return nil
}
