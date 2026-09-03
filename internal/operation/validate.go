package operation

import (
	"fmt"
	"math"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func validate(fields []schema.Field, values store.Values, create bool, pluginValidators map[string]PluginValidator) (store.Values, []schema.Issue) {
	issues := &validationIssueCollector{}
	validated := validateFields(fields, values, validationOptions{requireMissing: create}, pluginValidators, issues)
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
}

func validateFields(fields []schema.Field, values store.Values, options validationOptions, pluginValidators map[string]PluginValidator, issues *validationIssueCollector) store.Values {
	validated := store.CloneValues(values)
	byName := make(map[string]schema.Field, len(fields))
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation {
			continue
		}
		byName[field.Name] = field
	}
	for name := range values {
		if issues.full() {
			return validated
		}
		if name == "_key" || name == "blockType" {
			continue
		}
		if _, exists := byName[name]; !exists {
			issues.add(schema.Issue{Code: "unknown_field", Path: joinFieldPath(options.prefix, name), Message: fmt.Sprintf("field %q is not defined", name)})
		}
	}
	for _, field := range fields {
		if issues.full() {
			return validated
		}
		if field.Category == schema.FieldCategoryPresentation {
			delete(validated, field.Name)
			continue
		}
		path := joinFieldPath(options.prefix, field.Name)
		value, exists := values[field.Name]
		if !exists && options.requireMissing {
			if materialized, present := missingDefaultValue(field); present {
				value, exists = materialized, true
				validated[field.Name] = value
			}
		}
		if !exists {
			if options.requireMissing && field.Required {
				issues.add(requiredIssue(field, path))
			} else if options.requireMissing && field.Type == schema.FieldTypeArray && field.Nested != nil && field.Nested.MinRows > 0 {
				issues.add(schema.Issue{Code: "min_rows", Path: path, Message: fmt.Sprintf("%s must contain at least %d rows", field.Admin.Label, field.Nested.MinRows)})
			}
			continue
		}
		if value.Kind() == store.ValueNull {
			if field.Required {
				issues.add(requiredIssue(field, path))
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
				issues.add(schema.Issue{Code: "invalid_date", Path: path, Message: fmt.Sprintf("%s must match its configured date picker appearance", field.Admin.Label)})
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
				items, valid := value.Values()
				if !valid {
					issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)})
					continue
				}
				if field.Required && len(items) == 0 {
					issues.add(requiredIssue(field, path))
				}
				seen := make(map[string]bool, len(items))
				for index, item := range items {
					itemPath := fmt.Sprintf("%s.%d", path, index)
					text, valid := item.StringValue()
					if !valid {
						issues.add(schema.Issue{Code: "invalid_type", Path: itemPath, Message: fmt.Sprintf("%s choices must be strings", field.Admin.Label)})
						continue
					}
					if seen[text] {
						issues.add(schema.Issue{Code: "duplicate_choice", Path: itemPath, Message: fmt.Sprintf("%s contains the same choice more than once", field.Admin.Label)})
						continue
					}
					seen[text] = true
					found := false
					for _, choice := range field.Select.Choices {
						found = found || choice.Value == text
					}
					if !found {
						issues.add(schema.Issue{Code: "invalid_choice", Path: itemPath, Message: fmt.Sprintf("%s is not an allowed choice", field.Admin.Label)})
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
			for _, choice := range field.Select.Choices {
				found = found || choice.Value == text
			}
			if !found {
				issues.add(schema.Issue{Code: "invalid_choice", Path: path, Message: fmt.Sprintf("%s is not an allowed choice", field.Admin.Label)})
			}
		case schema.FieldTypePoint:
			coordinates, valid := value.Values()
			if !valid || len(coordinates) != 2 {
				issues.add(schema.Issue{Code: "invalid_point", Path: path, Message: fmt.Sprintf("%s must contain longitude and latitude", field.Admin.Label)})
				continue
			}
			longitude, longitudeValid := coordinates[0].NumberValue()
			latitude, latitudeValid := coordinates[1].NumberValue()
			if !longitudeValid || !latitudeValid || math.IsNaN(longitude) || math.IsNaN(latitude) || math.IsInf(longitude, 0) || math.IsInf(latitude, 0) || longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90 {
				issues.add(schema.Issue{Code: "invalid_point", Path: path, Message: fmt.Sprintf("%s must be a valid [longitude, latitude] pair", field.Admin.Label)})
			}
		case schema.FieldTypeGroup:
			object, valid := value.ObjectValue()
			if !valid || field.Nested == nil {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an object", field.Admin.Label)})
				continue
			}
			childValues := validateFields(field.Nested.Fields, object, validationOptions{prefix: path, requireMissing: true}, pluginValidators, issues)
			validated[field.Name] = store.Object(childValues)
		case schema.FieldTypeArray:
			items, valid := value.Values()
			if !valid || field.Nested == nil {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)})
				continue
			}
			if field.Required && len(items) == 0 {
				issues.add(schema.Issue{Code: "required", Path: path, Message: fmt.Sprintf("%s must contain at least one row", field.Admin.Label)})
			}
			if len(items) < field.Nested.MinRows && !(field.Required && len(items) == 0) {
				issues.add(schema.Issue{Code: "min_rows", Path: path, Message: fmt.Sprintf("%s must contain at least %d rows", field.Admin.Label, field.Nested.MinRows)})
			}
			if field.Nested.MaxRows > 0 && len(items) > field.Nested.MaxRows {
				issues.add(schema.Issue{Code: "max_rows", Path: path, Message: fmt.Sprintf("%s must contain no more than %d rows", field.Admin.Label, field.Nested.MaxRows)})
			}
			validatedItems := make([]store.Value, len(items))
			rowKeys := make(map[string]int, len(items))
			for index, item := range items {
				if issues.full() {
					break
				}
				itemPath := fmt.Sprintf("%s.%d", path, index)
				object, valid := item.ObjectValue()
				if !valid {
					issues.add(schema.Issue{Code: "invalid_type", Path: itemPath, Message: "array row must be an object"})
					continue
				}
				childValues := validateFields(field.Nested.Fields, object, validationOptions{prefix: itemPath, requireMissing: true}, pluginValidators, issues)
				issues.add(validateRowKey(object, childValues, rowKeys, itemPath, index)...)
				validatedItems[index] = store.Object(childValues)
			}
			validated[field.Name] = store.List(validatedItems...)
		case schema.FieldTypeBlocks:
			items, valid := value.Values()
			if !valid || field.Blocks == nil {
				issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)})
				continue
			}
			if field.Required && len(items) == 0 {
				issues.add(schema.Issue{Code: "required", Path: path, Message: fmt.Sprintf("%s must contain at least one block", field.Admin.Label)})
			}
			validatedItems := make([]store.Value, len(items))
			rowKeys := make(map[string]int, len(items))
			for index, item := range items {
				if issues.full() {
					break
				}
				itemPath := fmt.Sprintf("%s.%d", path, index)
				object, valid := item.ObjectValue()
				if !valid {
					issues.add(schema.Issue{Code: "invalid_type", Path: itemPath, Message: "block must be an object"})
					continue
				}
				blockTypeValue, exists := object["blockType"]
				blockKey, valid := blockTypeValue.StringValue()
				block := findBlock(field.Blocks.Types, blockKey)
				if !exists || !valid || block == nil {
					issues.add(schema.Issue{Code: "invalid_block", Path: itemPath + ".blockType", Message: "block type is not allowed"})
					continue
				}
				childValues := validateFields(block.Fields, object, validationOptions{prefix: itemPath, requireMissing: true}, pluginValidators, issues)
				childValues["blockType"] = store.String(blockKey)
				issues.add(validateRowKey(object, childValues, rowKeys, itemPath, index)...)
				validatedItems[index] = store.Object(childValues)
			}
			validated[field.Name] = store.List(validatedItems...)
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
		}
	}
	return validated
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
	validated["_key"] = store.String(key)
	return nil
}

func validDateValue(field schema.Field, value string) bool {
	if field.Date == nil {
		if _, err := time.Parse("2006-01-02", value); err == nil {
			return true
		}
		_, err := time.Parse(time.RFC3339, value)
		return err == nil
	}
	switch field.Date.PickerAppearance {
	case schema.DatePickerDayOnly:
		_, err := time.Parse("2006-01-02", value)
		return err == nil
	case schema.DatePickerDayAndTime:
		_, err := time.Parse(time.RFC3339, value)
		return err == nil
	case schema.DatePickerTimeOnly:
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
	values := []store.Value{value}
	if field.Upload.HasMany {
		items, valid := value.Values()
		if !valid {
			return []schema.Issue{{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)}}
		}
		if field.Required && len(items) == 0 {
			return []schema.Issue{{Code: "required", Path: path, Message: fmt.Sprintf("%s must contain at least one upload", field.Admin.Label)}}
		}
		if len(items) > MaxDocumentReferences {
			return []schema.Issue{*maxDocumentReferencesIssue(path)}
		}
		values = items
	}
	var issues []schema.Issue
	for index, item := range values {
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
	values := []store.Value{value}
	if field.Relationship.HasMany {
		items, valid := value.Values()
		if !valid {
			return []schema.Issue{{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)}}
		}
		if field.Required && len(items) == 0 {
			return []schema.Issue{{Code: "required", Path: path, Message: fmt.Sprintf("%s must contain at least one relationship", field.Admin.Label)}}
		}
		if len(items) > MaxDocumentReferences {
			return []schema.Issue{*maxDocumentReferencesIssue(path)}
		}
		values = items
	}
	var issues []schema.Issue
	for index, item := range values {
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
		object, valid := item.ObjectValue()
		if !valid {
			issues = append(issues, schema.Issue{Code: "invalid_relationship", Path: itemPath, Message: "polymorphic relationship reference must be an object"})
			continue
		}
		slug, slugValid := object["relationTo"].StringValue()
		id, idValid := object["id"].StringValue()
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
		for index, choice := range field.Select.DefaultValues {
			defaults[index] = store.String(choice)
		}
		return store.List(defaults...), true
	}
	if field.Type != schema.FieldTypeGroup || field.Nested == nil {
		return store.Value{}, false
	}
	values := store.Values{}
	for _, child := range field.Nested.Fields {
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
		if blocks[index].Key == key {
			return &blocks[index]
		}
	}
	return nil
}
