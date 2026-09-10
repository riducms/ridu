package operation

import (
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// authorizeQuery checks only caller-owned query inputs. Collection access
// predicates and framework-derived constraints must remain separate: they are
// trusted authorization, not a caller's opportunity to probe redacted values.
func authorizeQuery(collection Collection, filter query.Expression, sorts []query.Sort, paths ...query.Path) error {
	if filter != nil {
		if err := authorizeQueryNode(collection, filter.Node()); err != nil {
			return err
		}
	}
	for _, sort := range sorts {
		if err := authorizeQueryPath(collection, sort.Path); err != nil {
			return err
		}
		if field, found := population.FieldAtPath(collection.Schema.Fields, sort.Path); found {
			if primitivefield.IsList(field) {
				return primitiveListQueryError(sort.Path, "primitive lists cannot be sorted; choose a singular scalar field")
			}
			if queryDescendantsRequireRead(collection, schema.ChildFields(field)) {
				return queryFieldAccessError(sort.Path)
			}
		}
	}
	for _, path := range paths {
		if err := authorizeQueryPath(collection, path); err != nil {
			return err
		}
	}
	return nil
}

func authorizeQueryNode(collection Collection, node query.Node) error {
	if node.Comparison != nil {
		if field, found := population.FieldAtPath(collection.Schema.Fields, node.Comparison.Path); found {
			if err := primitivefield.ValidateComparison(field, *node.Comparison); err != nil {
				return primitiveListQueryError(node.Comparison.Path, err.Error())
			}
		}
		if err := authorizeQueryPath(collection, node.Comparison.Path); err != nil {
			return err
		}
	}
	for _, child := range node.Children {
		if err := authorizeQueryNode(collection, child); err != nil {
			return err
		}
	}
	return nil
}

func authorizeQueryPath(collection Collection, path query.Path) error {
	switch path.String() {
	case "id", "createdAt", "updatedAt", "_status", "_revision":
		return nil
	}
	segments := path.Segments()
	resolved := false
	opaque := false
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
		if field.QueryRestricted {
			// Rules may depend on each document, value, siblings, or locale. A
			// request-only evaluation cannot prove that all queried rows are
			// readable; checking after filtering/counting/sorting is too late.
			return queryFieldAccessError(path)
		}
	}
	if !resolved && !opaque {
		return &Error{
			Code: "bad_query", Status: 400, Message: "query path must resolve to a canonical schema field",
			Issues: []schema.Issue{{Code: "invalid_path", Path: path.String(), Message: "use an authored field path, including block and embedded variant segments"}},
		}
	}
	return nil
}

// Ordering a container compares its stored children in some adapters. A caller
// cannot use the readable parent as an alias for otherwise protected values.
func queryDescendantsRequireRead(collection Collection, fields []schema.Field) bool {
	for _, field := range fields {
		if field.QueryRestricted || queryDescendantsRequireRead(collection, schema.ChildFields(field)) {
			return true
		}
	}
	return false
}

func queryFieldAccessError(path query.Path) error {
	return &Error{
		Code: "field_access_denied", Status: 403,
		Message: "queries are not available for fields with read access rules",
		Issues:  []schema.Issue{{Code: "access_denied", Path: path.String(), Message: "queried values require document-level read authorization"}},
	}
}

func primitiveListQueryError(path query.Path, message string) error {
	return &Error{Code: "bad_query", Status: 400, Message: message, Issues: []schema.Issue{{Code: "unsupported_operator", Path: path.String(), Message: message}}}
}
