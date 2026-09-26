package schema

import (
	"fmt"
	"regexp"
)

var adminDataProperty = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// AdminLoader describes a compiled, authenticated admin read. Executable code
// remains in core; these JSON shapes drive generated browser contracts.
type AdminLoader struct {
	Key    string        `json:"key"`
	Input  AdminDataType `json:"input"`
	Output AdminDataType `json:"output"`
}

// AdminDataType is the JSON subset supported by typed admin loaders. Inputs
// are objects of scalar query parameters; outputs may contain nested objects
// and arrays. Optional marks an omitted object property, Nullable a JSON null.
type AdminDataType struct {
	Kind     string                   `json:"kind"`
	Nullable bool                     `json:"nullable,omitempty"`
	Optional bool                     `json:"optional,omitempty"`
	Fields   map[string]AdminDataType `json:"fields,omitempty"`
	Element  *AdminDataType           `json:"element,omitempty"`
}

func cloneAdminDataType(value AdminDataType) AdminDataType {
	if value.Fields != nil {
		fields := make(map[string]AdminDataType, len(value.Fields))
		for key, field := range value.Fields {
			fields[key] = cloneAdminDataType(field)
		}
		value.Fields = fields
	}
	if value.Element != nil {
		element := cloneAdminDataType(*value.Element)
		value.Element = &element
	}
	return value
}

// ValidateAdminLoaders checks serialized contracts at manifest and generator boundaries.
func ValidateAdminLoaders(loaders []AdminLoader) error {
	seen := make(map[string]bool)
	for index, loader := range loaders {
		path := fmt.Sprintf("application.adminLoaders[%d]", index)
		fail := func(message string) error {
			return NewValidationError([]Issue{{Code: "invalid_admin_loader", Path: path, Message: message}})
		}
		if len(loader.Key) > 80 || !IsValidCollectionSlug(loader.Key) || seen[loader.Key] {
			return fail("loader key must be unique and URL-safe")
		}
		seen[loader.Key] = true
		if loader.Input.Kind != "object" || loader.Input.Nullable || loader.Input.Optional {
			return fail("input must be an object of scalar query parameters")
		}
		for name, field := range loader.Input.Fields {
			if field.Kind != "string" && field.Kind != "number" && field.Kind != "boolean" {
				return fail("input." + name + " must be a scalar query parameter")
			}
		}
		if err := validateAdminDataType(loader.Input, 0); err != nil {
			return fail("input: " + err.Error())
		}
		if loader.Output.Optional {
			return fail("output cannot be optional")
		}
		if err := validateAdminDataType(loader.Output, 0); err != nil {
			return fail("output: " + err.Error())
		}
	}
	return nil
}

func validateAdminDataType(value AdminDataType, depth int) error {
	// Manifests may come from disk rather than the Go resolver. Bound recursive
	// validation before generators or admin bootstrap consume an authored shape.
	if depth > 32 {
		return fmt.Errorf("JSON shape exceeds 32 levels")
	}
	if value.Kind != "object" && value.Fields != nil {
		return fmt.Errorf("only objects can declare fields")
	}
	if value.Kind != "array" && value.Element != nil {
		return fmt.Errorf("only arrays can declare elements")
	}
	switch value.Kind {
	case "string", "number", "boolean":
		return nil
	case "array":
		if value.Element == nil || value.Element.Optional {
			return fmt.Errorf("array requires a non-optional element type")
		}
		return validateAdminDataType(*value.Element, depth+1)
	case "object":
		for key, field := range value.Fields {
			// __proto__ is a valid-looking identifier but unsafe when generated
			// browser code materializes the descriptor as an ordinary object.
			if !adminDataProperty.MatchString(key) || key == "__proto__" {
				return fmt.Errorf("invalid JSON property %q", key)
			}
			if err := validateAdminDataType(field, depth+1); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown JSON kind %q", value.Kind)
	}
}
