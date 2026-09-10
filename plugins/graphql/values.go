package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	enginegraphql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type requestState struct {
	rawVariables    map[string]interface{}
	actor           *store.Document
	actorCollection schema.CollectionSlug
	token           string
	clientIP        string
	userAgent       string
	admitAuth       func(context.Context, string, string) error
}

type requestStateKey struct{}

func requestFromContext(params enginegraphql.ResolveParams) requestState {
	state, _ := params.Context.Value(requestStateKey{}).(requestState)
	return state
}

type extendedError struct {
	message    string
	extensions map[string]interface{}
}

func (err extendedError) Error() string                      { return err.message }
func (err extendedError) Extensions() map[string]interface{} { return err.extensions }

type transportFailure struct {
	extendedError
	cause error
}

func (failure transportFailure) TrustedCause() error { return failure.cause }

func transportError(err error) error {
	var operationError *ridu.OperationError
	if errors.As(err, &operationError) {
		message := operationError.Message
		code := operationError.Code
		if operationError.Status >= 500 {
			message = "internal server error"
			code = "internal_error"
		}
		extensions := map[string]interface{}{"code": code, "status": operationError.Status}
		if operationError.Status < 500 && len(operationError.Issues) != 0 {
			extensions["issues"] = operationError.Issues
		}
		return transportFailure{extendedError: extendedError{message: message, extensions: extensions}, cause: err}
	}
	return transportFailure{extendedError: extendedError{message: "internal server error", extensions: map[string]interface{}{"code": "internal_error", "status": 500}}, cause: err}
}

func clientError(err error) error {
	return extendedError{message: err.Error(), extensions: map[string]interface{}{"code": "bad_query", "status": 400}}
}

func documentMap(document store.Document) map[string]interface{} {
	result := map[string]interface{}{
		"id": document.ID, "createdAt": document.CreatedAt.UTC().Format(time.RFC3339Nano), "updatedAt": document.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if document.Status != "" {
		result["_status"] = string(document.Status)
		result["_revision"] = document.Revision
	}
	for name, value := range document.Values {
		result[fieldName(name)] = valueInterface(value)
	}
	return result
}

func documentMapForArgs(document store.Document, args map[string]interface{}) map[string]interface{} {
	return documentMapWithLocalization(document, localeArg(args), fallbackArgs(args), boolArg(args, "disableFallback"), boolArg(args, "allLocales"))
}

func documentMapWithLocalization(document store.Document, locale schema.LocaleCode, fallback []schema.LocaleCode, disableFallback, allLocales bool) map[string]interface{} {
	result := documentMap(document)
	result["__riduLocale"] = string(locale)
	result["__riduFallbackLocales"] = append([]schema.LocaleCode(nil), fallback...)
	result["__riduDisableFallback"] = disableFallback
	result["__riduAllLocales"] = allLocales
	return result
}

func stringMapValue(values map[string]interface{}, name string) string {
	value, _ := values[name].(string)
	return value
}

func boolMapValue(values map[string]interface{}, name string) bool {
	value, _ := values[name].(bool)
	return value
}

func localeCodesValue(value interface{}) []schema.LocaleCode {
	switch values := value.(type) {
	case []schema.LocaleCode:
		return append([]schema.LocaleCode(nil), values...)
	case []string:
		result := make([]schema.LocaleCode, len(values))
		for index, value := range values {
			result[index] = schema.LocaleCode(value)
		}
		return result
	case []interface{}:
		result := make([]schema.LocaleCode, len(values))
		for index, value := range values {
			result[index] = schema.LocaleCode(fmt.Sprint(value))
		}
		return result
	default:
		return nil
	}
}

func valueInterface(value store.Value) interface{} {
	switch value.Kind() {
	case store.ValueNull:
		return nil
	case store.ValueString:
		result, _ := value.StringValue()
		return result
	case store.ValueNumber:
		result, _ := value.NumberValue()
		return result
	case store.ValueBoolean:
		result, _ := value.BooleanValue()
		return result
	case store.ValueObject:
		result := make(map[string]interface{}, value.Len())
		for name, child := range value.Entries() {
			result[fieldName(name)] = valueInterface(child)
		}
		return result
	case store.ValueDocument:
		document, _ := value.CopyDocument()
		return documentMap(document)
	case store.ValueList:
		result := make([]interface{}, 0, value.Len())
		for item := range value.Elements() {
			result = append(result, valueInterface(item))
		}
		return result
	default:
		return nil
	}
}

func valuesMap(values store.Values) map[string]interface{} {
	result := make(map[string]interface{}, len(values))
	for name, value := range values {
		result[fieldName(name)] = valueInterface(value)
	}
	return result
}

func valuesArg(args map[string]interface{}, name string, fields []schema.Field) store.Values {
	raw, _ := args[name].(map[string]interface{})
	if raw == nil {
		return store.Values{}
	}
	normalized := normalizeInputObject(raw, fields)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return store.Values{}
	}
	var result store.Values
	if err := json.Unmarshal(encoded, &result); err != nil {
		return store.Values{}
	}
	return result
}

func normalizeInputObject(raw map[string]interface{}, fields []schema.Field) map[string]interface{} {
	byName := make(map[string]schema.Field, len(fields))
	for _, field := range fields {
		byName[fieldName(field.Name)] = field
	}
	result := make(map[string]interface{}, len(raw))
	for name, value := range raw {
		field, exists := byName[name]
		if !exists {
			if name == "password" {
				continue
			}
			result[name] = value
			continue
		}
		result[field.Name] = normalizeInputValue(value, field)
	}
	return result
}

func dataStringArg(args map[string]interface{}, objectName, name string) string {
	object, _ := args[objectName].(map[string]interface{})
	return fmt.Sprint(object[name])
}

func stringArg(args map[string]interface{}, name, fallback string) string {
	value, exists := args[name]
	if !exists || value == nil || fmt.Sprint(value) == "" {
		return fallback
	}
	return fmt.Sprint(value)
}

func sessionMap(session ridu.AuthSession) map[string]interface{} {
	return map[string]interface{}{
		"token": session.Token, "refreshedToken": session.Token, "collection": string(session.Collection),
		"expiresAt": session.ExpiresAt.UTC().Format(time.RFC3339Nano), "exp": float64(session.ExpiresAt.Unix()),
		"user": documentMap(session.User),
	}
}

func normalizeInputValue(value interface{}, field schema.Field) interface{} {
	switch field.Type {
	case schema.FieldTypeGroup:
		object, _ := value.(map[string]interface{})
		if object != nil && field.Nested != nil {
			return normalizeInputObject(object, field.Nested.ResolvedFields())
		}
	case schema.FieldTypeArray:
		items, _ := value.([]interface{})
		if field.Nested != nil {
			for index, item := range items {
				object, _ := item.(map[string]interface{})
				if object != nil {
					items[index] = normalizeInputObject(object, field.Nested.ResolvedFields())
				}
			}
		}
		return items
	case schema.FieldTypeBlocks:
		items, _ := value.([]interface{})
		if field.Blocks == nil {
			return value
		}
		for index, item := range items {
			object, _ := item.(map[string]interface{})
			blockType, _ := object["blockType"].(string)
			for _, block := range field.Blocks.ResolvedTypes() {
				if block.Slug != blockType {
					continue
				}
				normalized := normalizeInputObject(object, block.ResolvedFields())
				normalized["blockType"] = blockType
				if key, exists := object["_key"]; exists {
					normalized["_key"] = key
				}
				items[index] = normalized
				break
			}
		}
		return items
	case schema.FieldTypeRelationship:
		if field.Relationship != nil && field.Relationship.Polymorphic {
			normalize := func(item interface{}) interface{} {
				object, _ := item.(map[string]interface{})
				if object == nil {
					return item
				}
				return map[string]interface{}{"relationTo": object["relationTo"], "id": object["value"]}
			}
			if field.Relationship.HasMany {
				items, _ := value.([]interface{})
				for index, item := range items {
					items[index] = normalize(item)
				}
				return items
			}
			return normalize(value)
		}
	}
	return value
}

func localeArg(args map[string]interface{}) schema.LocaleCode {
	value, _ := args["locale"].(string)
	return schema.LocaleCode(value)
}

func fallbackArgs(args map[string]interface{}) []schema.LocaleCode {
	raw, _ := args["fallbackLocale"].([]interface{})
	result := make([]schema.LocaleCode, len(raw))
	for index, value := range raw {
		result[index] = schema.LocaleCode(fmt.Sprint(value))
	}
	return result
}

func localeOptions(args map[string]interface{}) ridu.LocaleOptions {
	return ridu.LocaleOptions{Locale: localeArg(args), FallbackLocales: fallbackArgs(args), DisableFallback: boolArg(args, "disableFallback"), AllLocales: boolArg(args, "allLocales")}
}

func boolArg(args map[string]interface{}, name string) bool {
	value, _ := args[name].(bool)
	return value
}

func boolPointerArg(args map[string]interface{}, name string) *bool {
	value, exists := args[name]
	if !exists || value == nil {
		return nil
	}
	result, valid := value.(bool)
	if !valid {
		return nil
	}
	return &result
}

func intArg(args map[string]interface{}, name string, fallback int) int {
	value, ok := args[name].(int)
	if !ok {
		return fallback
	}
	return value
}

func boundedLimit(args map[string]interface{}, maximum int) int {
	fallback := 10
	if maximum < fallback {
		fallback = maximum
	}
	value := intArg(args, "limit", fallback)
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func pageMap(page store.Page) map[string]interface{} {
	documents := make([]interface{}, len(page.Documents))
	for index, document := range page.Documents {
		documents[index] = documentMap(document)
	}
	totalPages := 0
	if page.Limit > 0 {
		totalPages = int(math.Ceil(float64(page.Total) / float64(page.Limit)))
	}
	result := map[string]interface{}{
		"docs": documents, "page": page.Page, "limit": page.Limit, "totalDocs": page.Total, "totalPages": totalPages,
		"hasNextPage": page.Page < totalPages, "hasPrevPage": page.Page > 1, "pagingCounter": (page.Page-1)*page.Limit + 1,
	}
	if page.Page < totalPages {
		result["nextPage"] = page.Page + 1
	}
	if page.Page > 1 {
		result["prevPage"] = page.Page - 1
	}
	return result
}

func pageMapForArgs(page store.Page, args map[string]interface{}) map[string]interface{} {
	result := pageMap(page)
	documents := make([]interface{}, len(page.Documents))
	for index, document := range page.Documents {
		documents[index] = documentMapForArgs(document, args)
	}
	result["docs"] = documents
	return result
}

func versionMap(version store.Version) map[string]interface{} {
	return map[string]interface{}{
		"id": version.ID, "documentID": version.DocumentID, "revision": version.Revision,
		"status": string(version.Status), "createdAt": version.CreatedAt.UTC().Format(time.RFC3339Nano),
		"snapshot": documentMap(version.Snapshot),
	}
}

func versionPageMap(versions []store.Version, page, limit int) map[string]interface{} {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 10
	}
	start := (page - 1) * limit
	if start > len(versions) {
		start = len(versions)
	}
	end := start + limit
	if end > len(versions) {
		end = len(versions)
	}
	documents := make([]interface{}, end-start)
	for index, version := range versions[start:end] {
		documents[index] = versionMap(version)
	}
	totalPages := 0
	if len(versions) != 0 {
		totalPages = int(math.Ceil(float64(len(versions)) / float64(limit)))
	}
	return map[string]interface{}{"docs": documents, "page": page, "limit": limit, "totalDocs": len(versions), "totalPages": totalPages}
}

func jsonScalar() *enginegraphql.Scalar {
	return enginegraphql.NewScalar(enginegraphql.ScalarConfig{
		Name: "JSON", Description: "An arbitrary JSON value.",
		Serialize:    func(value interface{}) interface{} { return value },
		ParseValue:   func(value interface{}) interface{} { return value },
		ParseLiteral: parseJSONLiteral,
	})
}

func parseJSONLiteral(value ast.Value) interface{} {
	switch current := value.(type) {
	case *ast.EnumValue:
		if current.Value == "null" {
			return nil
		}
		return current.GetValue()
	case *ast.IntValue:
		parsed, _ := strconv.ParseFloat(current.Value, 64)
		return parsed
	case *ast.FloatValue:
		parsed, _ := strconv.ParseFloat(current.Value, 64)
		return parsed
	case *ast.StringValue, *ast.BooleanValue:
		return current.GetValue()
	case *ast.ListValue:
		result := make([]interface{}, len(current.Values))
		for index, item := range current.Values {
			result[index] = parseJSONLiteral(item)
		}
		return result
	case *ast.ObjectValue:
		result := make(map[string]interface{}, len(current.Fields))
		for _, field := range current.Fields {
			result[field.Name.Value] = parseJSONLiteral(field.Value)
		}
		return result
	default:
		return nil
	}
}
