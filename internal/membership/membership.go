// Package membership owns the query contract for fields whose value is a set
// of items: text and number lists, has-many selects, has-many relationships
// and uploads, and polymorphic relationships. Such a field is filtered by
// membership alone: In matches when any stored item equals any candidate,
// Not negates that, and Exists and null equality test presence. Scalar
// equality, substring and range comparisons, and sorting are unavailable.
//
// The operation engine checks caller filters with this package before any
// store runs, so every transport rejects the same queries. Store adapters
// check whole requests, including trusted access predicates, and the
// in-process matchers compare items with Matches.
package membership

import (
	"fmt"
	"math"

	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Kind is the item type of a membership field.
type Kind uint8

const (
	// None is a field compared as one scalar or opaque value.
	None Kind = iota
	// Strings items are text: text lists, has-many selects, and the document
	// IDs of has-many single-collection relationships and uploads.
	Strings
	// Numbers items are finite numbers: number lists.
	Numbers
	// References items are {relationTo, id} references: a polymorphic
	// relationship, which holds at most one when it is singular.
	References
)

// KindOf classifies a field's stored value.
func KindOf(field schema.Field) Kind {
	switch field.Type {
	case schema.FieldTypeTextList:
		return Strings
	case schema.FieldTypeNumberList:
		return Numbers
	case schema.FieldTypeSelect:
		if field.Select != nil && field.Select.HasMany {
			return Strings
		}
	case schema.FieldTypeRelationship:
		if field.Relationship != nil && field.Relationship.Polymorphic {
			return References
		}
		if field.Relationship != nil && field.Relationship.HasMany {
			return Strings
		}
	case schema.FieldTypeUpload:
		if field.Upload != nil && field.Upload.HasMany {
			return Strings
		}
	}
	return None
}

// ValidateComparison rejects a comparison the membership contract does not
// define for field, and a reference operand for any field that is not a
// polymorphic relationship. A field outside the contract is otherwise left to
// ordinary query validation.
func ValidateComparison(field schema.Field, comparison query.Comparison) error {
	path := comparison.Path.String()
	kind := KindOf(field)
	if kind != References && holdsReference(comparison.Value) {
		return fmt.Errorf("%q is not a polymorphic relationship; only a polymorphic relationship is compared with {relationTo, id} references", path)
	}
	if kind == None {
		return nil
	}
	switch comparison.Operator {
	case query.OperatorExists:
		if comparison.Value.Kind() == query.ValueBoolean {
			return nil
		}
	case query.OperatorEqual, query.OperatorNotEqual:
		if comparison.Value.Kind() == query.ValueNull {
			return nil
		}
	case query.OperatorIn:
		if comparison.Value.Kind() == query.ValueList {
			for _, candidate := range comparison.Value.Values() {
				if err := validateCandidate(field, kind, path, candidate); err != nil {
					return err
				}
			}
			return nil
		}
	}
	return fmt.Errorf("%s %q does not support %q with this operand; use in to match any of its items (inside not to exclude them), exists, or equals and notEquals with null", describe(field), path, comparison.Operator)
}

func validateCandidate(field schema.Field, kind Kind, path string, candidate query.Value) error {
	switch kind {
	case Strings:
		if candidate.Kind() == query.ValueString {
			return nil
		}
		return fmt.Errorf("%s %q membership requires string candidates; null is not an item", describe(field), path)
	case Numbers:
		number, ok := candidate.NumberValue()
		if !ok {
			return fmt.Errorf("%s %q membership requires number candidates; null is not an item", describe(field), path)
		}
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("%s %q membership requires finite numbers", describe(field), path)
		}
		return nil
	default:
		relationTo, id, ok := candidate.ReferenceValue()
		if !ok {
			return fmt.Errorf("%s %q membership requires {relationTo, id} reference candidates; null is not an item", describe(field), path)
		}
		if id == "" {
			return fmt.Errorf("%s %q reference to %q requires a non-empty id", describe(field), path, relationTo)
		}
		for _, target := range field.Relationship.Targets {
			if string(target.CollectionSlug) == relationTo {
				return nil
			}
		}
		return fmt.Errorf("%s %q cannot reference collection %q; relationTo must be one of its target collections", describe(field), path, relationTo)
	}
}

func holdsReference(value query.Value) bool {
	if value.Kind() == query.ValueReference {
		return true
	}
	if value.Kind() != query.ValueList {
		return false
	}
	for _, item := range value.Values() {
		if item.Kind() == query.ValueReference {
			return true
		}
	}
	return false
}

func describe(field schema.Field) string {
	switch field.Type {
	case schema.FieldTypeTextList:
		return "text list"
	case schema.FieldTypeNumberList:
		return "number list"
	case schema.FieldTypeSelect:
		return "has-many select"
	case schema.FieldTypeUpload:
		return "has-many upload"
	}
	if field.Relationship != nil && field.Relationship.Polymorphic {
		return "polymorphic relationship"
	}
	return "has-many relationship"
}

// ValidateNode validates every comparison in node against fields.
func ValidateNode(fields []schema.Field, node *query.Node) error {
	if node == nil {
		return nil
	}
	if node.Comparison != nil {
		// A path that is not a field, such as id or a key inside a JSON value,
		// still must not receive a reference operand.
		field, _ := population.FieldAtPath(fields, node.Comparison.Path)
		if err := ValidateComparison(field, *node.Comparison); err != nil {
			return err
		}
	}
	for index := range node.Children {
		if err := ValidateNode(fields, &node.Children[index]); err != nil {
			return err
		}
	}
	return nil
}

// ValidateRequest validates a store request's filter, access predicate and
// sort. A membership field has no single value to order by.
func ValidateRequest(request store.Request) error {
	if err := ValidateNode(request.Collection.Fields, request.Filter); err != nil {
		return err
	}
	if err := ValidateNode(request.Collection.Fields, request.Access); err != nil {
		return err
	}
	for _, sort := range request.Sort {
		if field, ok := population.FieldAtPath(request.Collection.Fields, sort.Path); ok && KindOf(field) != None {
			return fmt.Errorf("%s %q cannot be sorted; choose a singular scalar field", describe(field), sort.Path.String())
		}
	}
	return nil
}

// Matches reports whether a stored membership value holds an item equal to
// one of candidates, a list operand. A list's items are compared in turn; a
// singular polymorphic reference is its only item. Comparison is exact and
// never sorts or deduplicates values.
func Matches(value store.Value, candidates query.Value) bool {
	items := candidates.Values()
	switch value.Kind() {
	case store.ValueList:
		for item := range value.Elements() {
			if itemMatches(item, items) {
				return true
			}
		}
	case store.ValueObject:
		return itemMatches(value, items)
	}
	return false
}

func itemMatches(item store.Value, candidates []query.Value) bool {
	for _, candidate := range candidates {
		switch candidate.Kind() {
		case query.ValueString:
			actual, valid := item.StringValue()
			expected, _ := candidate.StringValue()
			if valid && actual == expected {
				return true
			}
		case query.ValueNumber:
			actual, valid := item.NumberValue()
			expected, _ := candidate.NumberValue()
			if valid && actual == expected {
				return true
			}
		case query.ValueReference:
			if item.Kind() != store.ValueObject {
				continue
			}
			relationTo, id, _ := candidate.ReferenceValue()
			actualRelationTo, relationValid := item.Get("relationTo").StringValue()
			actualID, idValid := item.Get("id").StringValue()
			if relationValid && idValid && actualRelationTo == relationTo && actualID == id {
				return true
			}
		}
	}
	return false
}
