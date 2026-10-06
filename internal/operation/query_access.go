package operation

import (
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/membership"
	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/internal/querypath"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// authorizeQuery checks only caller-owned query inputs. Collection access
// predicates and framework-derived constraints must remain separate: they are
// trusted authorization, not a caller's opportunity to probe redacted values.
// A trusted (system) caller may query fields with read rules; path shape and
// operator validation still apply.
func authorizeQuery(collection Collection, trusted bool, filter query.Expression, sorts []query.Sort, paths ...query.Path) error {
	if filter != nil {
		if err := authorizeQueryNode(collection, trusted, filter.Node()); err != nil {
			return err
		}
	}
	for _, sort := range sorts {
		if err := authorizeQueryPath(collection, trusted, sort.Path); err != nil {
			return err
		}
		if err := validateSortPath(collection.Schema, sort.Path); err != nil {
			return err
		}
	}
	for _, path := range paths {
		if err := authorizeQueryPath(collection, trusted, path); err != nil {
			return err
		}
	}
	return nil
}

// validateSortPath rejects a sort path, before any store, with the shared
// rule configuration also applies to join default sorts, so each transport
// and adapter reports the same unsupported_path.
func validateSortPath(collection schema.Collection, path query.Path) error {
	if err := querypath.ValidateSort(collection, path); err != nil {
		return unsupportedPathError(path, err.Error(), nil)
	}
	return nil
}

func authorizeQueryNode(collection Collection, trusted bool, node query.Node) error {
	if node.Comparison != nil {
		// Paths that are not fields, such as id or a key in a JSON value, are
		// checked too: only a polymorphic relationship accepts a reference.
		field, _ := population.FieldAtPath(collection.Schema.Fields, node.Comparison.Path)
		if err := membership.ValidateComparison(field, *node.Comparison); err != nil {
			return queryOperatorError(node.Comparison.Path, err.Error())
		}
		if err := authorizeQueryPath(collection, trusted, node.Comparison.Path); err != nil {
			return err
		}
	}
	for _, child := range node.Children {
		if err := authorizeQueryNode(collection, trusted, child); err != nil {
			return err
		}
	}
	return nil
}

func authorizeQueryPath(collection Collection, trusted bool, path query.Path) error {
	switch path.String() {
	case "id", "createdAt", "updatedAt", "_status", "_revision":
		return nil
	}
	segments := path.Segments()
	resolved := false
	opaque := false
	embeddedTree := false
	for length := 1; length <= len(segments); length++ {
		prefix, err := query.NewPath(segments[:length]...)
		if err != nil {
			continue // Ordinary query validation owns invalid paths.
		}
		field, exists := population.FieldAtPath(collection.Schema.Fields, prefix)
		if !exists {
			continue // Variant/tree discriminator segments are not fields.
		}
		resolved = length == len(segments)
		// Only opaque values have unmodeled descendants. Managed Blocks/plugin
		// payloads require canonical variant paths, otherwise raw wire aliases
		// could reach a different field than the one whose rule was checked.
		opaque = field.Type == schema.FieldTypeJSON || field.Type == schema.FieldTypePlugin && !embedded.HasFields(field)
		embeddedTree = embeddedTree || length < len(segments) && embedded.HasFields(field)
		if field.QueryRestricted && !trusted {
			// Rules may depend on each document, value, siblings, or locale. A
			// request-only evaluation cannot prove that all queried rows are
			// readable; checking after filtering/counting/sorting is too late.
			return queryFieldAccessError(path)
		}
	}
	if embeddedTree {
		// Schema paths into a plugin's embedded trees describe validation and
		// generated types; they are not a query language over the tree.
		return unsupportedPathError(path, "paths inside a plugin field's embedded trees cannot be queried; model searchable content as ordinary fields", nil)
	}
	if !resolved && !opaque {
		return &Error{
			Code: "bad_query", Status: 400, Message: "query path must resolve to a canonical schema field",
			Issues: []schema.Issue{{Code: "invalid_path", Path: path.String(), Message: "use an authored field path, including block and embedded variant segments"}},
		}
	}
	return nil
}

// unsupportedPathError rejects a caller query path whose shape no store
// evaluates for the requested use. It never depends on stored values.
func unsupportedPathError(path query.Path, message string, cause error) error {
	return &Error{
		Code: "bad_query", Status: 400, Message: message, Cause: cause,
		Issues: []schema.Issue{{Code: "unsupported_path", Path: path.String(), Message: message}},
	}
}

func queryFieldAccessError(path query.Path) error {
	return &Error{
		Code: "field_access_denied", Status: 403,
		Message: "queries are not available for fields with read access rules",
		Issues:  []schema.Issue{{Code: "access_denied", Path: path.String(), Message: "queried values require document-level read authorization"}},
	}
}

// queryOperatorError rejects an operator or operand that a field's query
// contract does not define, such as equals on a has-many relationship.
func queryOperatorError(path query.Path, message string) error {
	return &Error{Code: "bad_query", Status: 400, Message: message, Issues: []schema.Issue{{Code: "unsupported_operator", Path: path.String(), Message: message}}}
}
