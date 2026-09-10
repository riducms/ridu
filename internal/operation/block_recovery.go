package operation

import (
	"fmt"
	"sort"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Unknown stored variants cannot be safely authorized through an absent schema.
// Keep their bytes in storage and require schema restoration or an explicit data
// migration. Errors contain schema paths only, never an opaque row's payload.
func unknownBlockRecoveryError(fields []schema.Field, values store.Values, canonical bool) error {
	issues := &validationIssueCollector{}
	if err := embedded.ValidateValues(fields, values, "", canonical, nil); err != nil {
		return embeddedOperationError(err, true)
	}
	var walk func([]schema.Field, store.Values, string)
	var walkObject func([]schema.Field, store.Value, string)
	var visit func(schema.Field, store.Value, string)
	walk = func(fields []schema.Field, values store.Values, prefix string) {
		for _, field := range fields {
			if issues.full() {
				return
			}
			if value, exists := values[field.Name]; exists {
				visit(field, value, joinFieldPath(prefix, field.Name))
			}
		}
	}
	walkObject = func(fields []schema.Field, object store.Value, prefix string) {
		for _, field := range fields {
			if issues.full() {
				return
			}
			if value, exists := object.Lookup(field.Name); exists {
				visit(field, value, joinFieldPath(prefix, field.Name))
			}
		}
	}
	visit = func(field schema.Field, value store.Value, path string) {
		if !fieldContainsRowIdentities(field) || value.Kind() == store.ValueNull {
			return
		}
		if canonical && field.Localized {
			if value.Kind() != store.ValueObject {
				return
			}
			codes := make([]string, 0, value.Len())
			for code := range value.Entries() {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			plain := field
			plain.Localized = false
			for _, code := range codes {
				if issues.full() {
					return
				}
				visit(plain, value.Get(code), joinFieldPath(path, code))
			}
			return
		}
		switch field.Type {
		case schema.FieldTypePlugin:
			_ = embedded.Visit(field, value, path, nil, func(occurrence embedded.ReadOccurrence) error {
				if occurrence.Key == "" {
					issues.add(schema.Issue{Code: "missing_embedded_identity", Path: occurrence.RuntimePath + "." + occurrence.Case.Identity, Message: "Migrate this stored payload to assign a stable occurrence identity before using it."})
					return nil
				}
				walkObject(occurrence.Fields, occurrence.Payload, occurrence.RuntimePath)
				return nil
			}) // Already checked before recursion.
		case schema.FieldTypeGroup:
			if value.Kind() == store.ValueObject && field.Nested != nil {
				walkObject(field.Nested.ResolvedFields(), value, path)
			}
		case schema.FieldTypeArray, schema.FieldTypeBlocks:
			index := -1
			for row := range value.Elements() {
				index++
				if issues.full() {
					return
				}
				valid := row.Kind() == store.ValueObject
				rowPath := fmt.Sprintf("%s.%d", path, index)
				if field.Type == schema.FieldTypeArray {
					if valid && field.Nested != nil {
						walkObject(field.Nested.ResolvedFields(), row, rowPath)
					}
					continue
				}
				kind, _ := row.Get("blockType").StringValue()
				var block *schema.BlockType
				if valid && field.Blocks != nil {
					block = findBlock(field.Blocks.ResolvedTypes(), kind)
				}
				if block == nil {
					issues.add(schema.Issue{Code: "unknown_block_schema", Path: rowPath + ".blockType", Message: "Restore the missing block schema or migrate the stored document before using it."})
					continue
				}
				walkObject(block.ResolvedFields(), row, rowPath)
			}
		}
	}
	walk(fields, values, "")
	if len(issues.values) == 0 {
		return nil
	}
	return &Error{Code: "block_recovery_required", Status: 409, Message: "This document requires block schema recovery. Restore the missing block schema or migrate the stored document before using it.", Issues: issues.values}
}

// Validate populated targets against their own schemas before exposing content.
func (engine *Engine) validateBlockResponse(collection Collection, document *store.Document, allLocales, canonical bool) error {
	if err := unknownBlockRecoveryError(collection.Schema.Fields, document.Values, canonical); err != nil {
		return err
	}
	var nestedError error
	population.MapPopulatedDocuments(collection.Schema.Fields, document.Values, canonical, func(targetID schema.StableID, _ schema.LocaleCode, targetDocument store.Document) store.Document {
		if nestedError != nil {
			return targetDocument
		}
		if target, exists := engine.collections[string(targetID)]; exists {
			nestedError = engine.validateBlockResponse(target, &targetDocument, allLocales, allLocales)
		}
		return targetDocument
	})
	return nestedError
}
