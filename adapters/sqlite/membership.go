package sqlite

import (
	"fmt"
	"strings"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// sqliteFieldStep is one field of a schema path. block is the block type a
// path selects below a Blocks field.
type sqliteFieldStep struct {
	field schema.Field
	block string
}

// sqliteFieldSteps resolves a schema path through groups, arrays and block
// types, as the matcher's schema walk does. A path into a plugin field's
// embedded trees has no SQL form.
func sqliteFieldSteps(fields []schema.Field, segments []string) ([]sqliteFieldStep, bool) {
	steps := make([]sqliteFieldStep, 0, len(segments))
	candidates := fields
	for index := 0; index < len(segments); index++ {
		var current *schema.Field
		for candidateIndex := range candidates {
			if candidates[candidateIndex].Name == segments[index] {
				current = &candidates[candidateIndex]
				break
			}
		}
		if current == nil {
			return nil, false
		}
		step := sqliteFieldStep{field: *current}
		if index == len(segments)-1 {
			return append(steps, step), true
		}
		switch current.Type {
		case schema.FieldTypeGroup, schema.FieldTypeArray:
			if current.Nested == nil {
				return nil, false
			}
			candidates = current.Nested.ResolvedFields()
		case schema.FieldTypeBlocks:
			if current.Blocks == nil || index+2 >= len(segments) {
				return nil, false
			}
			block, found := current.Blocks.Definition(segments[index+1])
			if !found {
				return nil, false
			}
			step.block = block.Slug
			candidates = block.ResolvedFields()
			index++
		default:
			return nil, false
		}
		steps = append(steps, step)
	}
	return nil, false
}

// sqliteJSONPathSQL is a JSON path written in SQL: literal segments below a
// base, which is the document root or an SQL expression that yields a path,
// such as a locale choice or a repeated row's fullkey.
type sqliteJSONPathSQL struct {
	base     string
	segments []string
}

func (path sqliteJSONPathSQL) child(segments ...string) sqliteJSONPathSQL {
	return sqliteJSONPathSQL{base: path.base, segments: append(append([]string(nil), path.segments...), segments...)}
}

func (path sqliteJSONPathSQL) sql() string {
	switch {
	case path.base == "":
		return quoteSQLiteLiteral(sqliteJSONPath(path.segments...))
	case len(path.segments) == 0:
		return path.base
	default:
		return path.base + " || " + quoteSQLiteLiteral(strings.TrimPrefix(sqliteJSONPath(path.segments...), "$"))
	}
}

// sqliteJSONLocation addresses the values a schema path reaches in one JSON
// document. Each array or Blocks field on the way contributes a json_each row
// source and the guards the matcher applies to its rows; path locates the
// value inside the innermost row.
//
// json_each gives array elements integer keys, object members text keys and a
// lone scalar a null key, so typeof(key) = 'integer' admits array elements
// only, as the matcher does, without reading the container again. A member
// lookup reaches nothing below a scalar or list, so rows and references need
// no separate object test.
type sqliteJSONLocation struct {
	document string
	rows     []string
	guards   []string
	path     sqliteJSONPathSQL
}

func sqliteLocateJSON(document string, steps []sqliteFieldStep, locales []schema.LocaleCode) (sqliteJSONLocation, bool) {
	location := sqliteJSONLocation{document: document}
	for index, step := range steps {
		location.path = location.path.child(step.field.Name)
		if step.field.Localized {
			if len(locales) == 0 {
				return sqliteJSONLocation{}, false
			}
			location.path = location.localeChoice(locales)
		}
		if index == len(steps)-1 || step.field.Type == schema.FieldTypeGroup {
			continue
		}
		row := fmt.Sprintf("ridu_row%d", len(location.rows))
		location.rows = append(location.rows, "json_each("+document+", "+location.path.sql()+") AS "+row)
		location.guards = append(location.guards, "typeof("+row+".key) = 'integer'")
		location.path = sqliteJSONPathSQL{base: row + ".fullkey"}
		if step.field.Type == schema.FieldTypeBlocks {
			// A block slug is an identifier, so only a JSON string equals it.
			location.guards = append(location.guards,
				"json_extract("+document+", "+location.path.child("blockType").sql()+") = "+quoteSQLiteLiteral(step.block))
		}
	}
	return location, true
}

func (location sqliteJSONLocation) typeAt(path sqliteJSONPathSQL) string {
	return "json_type(" + location.document + ", " + path.sql() + ")"
}

// localeChoice selects the locale the matcher reads: the first in the chain
// whose value is present, not null, and not an empty string unless it is the
// last. The value type is tested rather than assumed, so a retained snapshot
// from an older field shape selects the same locale as the matcher.
func (location sqliteJSONLocation) localeChoice(locales []schema.LocaleCode) sqliteJSONPathSQL {
	if len(locales) == 1 {
		return location.path.child(string(locales[0]))
	}
	var choice strings.Builder
	choice.WriteString("CASE")
	for index, locale := range locales {
		candidate := location.path.child(string(locale))
		if index == len(locales)-1 {
			choice.WriteString(" ELSE " + candidate.sql() + " END")
			break
		}
		valueType := location.typeAt(candidate)
		choice.WriteString(" WHEN " + valueType + " <> 'null' AND (" + valueType + " <> 'text' OR json_extract(" + location.document + ", " + candidate.sql() + ") <> '') THEN " + candidate.sql())
	}
	return sqliteJSONPathSQL{base: choice.String()}
}

// some holds when a reached row, joined with sources, satisfies conditions.
// Without repeated containers or sources it is the conditions themselves.
func (location sqliteJSONLocation) some(sources []string, conditions ...string) string {
	from := append(append([]string(nil), location.rows...), sources...)
	where := append(append([]string(nil), location.guards...), conditions...)
	if len(from) == 0 {
		return strings.Join(where, " AND ")
	}
	return "EXISTS (SELECT 1 FROM " + strings.Join(from, ", ") + " WHERE " + strings.Join(where, " AND ") + ")"
}

// compileSQLiteMembership compiles a comparison on a set-valued field. In
// holds when an item equals a candidate: items are the elements of a stored
// list, and a stored object is the one item a reference candidate can equal.
// Items are compared by the candidate's kind and their own JSON type, as
// membership.Matches compares them, so the SQL agrees with the matcher for
// every stored shape, including a retained snapshot of an older field shape.
// Exists and null equality test presence; an empty list is present.
func compileSQLiteMembership(fields []schema.Field, comparison query.Comparison, locales []schema.LocaleCode, source sqlitePredicateSource) (sqlitePredicate, bool) {
	steps, found := sqliteFieldSteps(fields, comparison.Path.Segments())
	if !found {
		return sqlitePredicate{}, false
	}
	location, supported := sqliteLocateJSON(source.valuesJSON, steps, locales)
	if !supported {
		return sqlitePredicate{}, false
	}
	valueType := location.typeAt(location.path)
	switch comparison.Operator {
	case query.OperatorIn:
		return location.membership(comparison.Value.Values()), true
	case query.OperatorExists:
		present := location.some(nil, "COALESCE("+valueType+", 'null') <> 'null'")
		if want, _ := comparison.Value.BooleanValue(); want {
			return sqlitePredicate{clause: present, exact: true}, true
		}
		return sqlitePredicate{clause: "NOT (" + present + ")", exact: true}, true
	case query.OperatorEqual, query.OperatorNotEqual:
		if comparison.Value.Kind() != query.ValueNull {
			return sqlitePredicate{}, false
		}
		// The matcher's null equality holds when the path reaches no value or
		// reaches a null.
		null := "NOT (" + location.some(nil, valueType+" IS NOT NULL") + ") OR (" + location.some(nil, valueType+" = 'null'") + ")"
		if comparison.Operator == query.OperatorNotEqual {
			null = "NOT (" + null + ")"
		}
		return sqlitePredicate{clause: null, exact: true}, true
	}
	return sqlitePredicate{}, false
}

func (location sqliteJSONLocation) membership(candidates []query.Value) sqlitePredicate {
	var texts, numbers, references []any
	for _, candidate := range candidates {
		switch candidate.Kind() {
		case query.ValueString:
			text, _ := candidate.StringValue()
			texts = append(texts, text)
		case query.ValueNumber:
			number, _ := candidate.NumberValue()
			numbers = append(numbers, number)
		case query.ValueReference:
			relationTo, id, _ := candidate.ReferenceValue()
			references = append(references, relationTo, id)
		}
	}
	// An item's atom is its SQL value when it is a JSON scalar and null
	// otherwise. Neither the atom nor a parameter has a type affinity, so
	// SQLite compares them without conversion and text equals text only. Each
	// comparison precedes the guards, which then run for matching items alone.
	const item = "ridu_item"
	var matches []string
	var arguments []any
	if len(texts) != 0 {
		matches = append(matches, item+".atom IN ("+sqlitePlaceholders(len(texts))+")")
		arguments = append(arguments, texts...)
	}
	if len(numbers) != 0 {
		// Go decodes stored numbers as float64, so compare in that space.
		matches = append(matches, "CAST("+item+".atom AS REAL) IN ("+sqlitePlaceholders(len(numbers))+") AND "+item+".type IN ('integer', 'real')")
		arguments = append(arguments, numbers...)
	}
	if len(references) != 0 {
		matches = append(matches, location.referenceIn(sqliteJSONPathSQL{base: item + ".fullkey"}, len(references)/2))
		arguments = append(arguments, references...)
	}
	if len(matches) == 0 {
		return sqlitePredicate{clause: "0", exact: true}
	}
	elements := "json_each(" + location.document + ", " + location.path.sql() + ") AS " + item
	clause := location.some([]string{elements}, "(("+strings.Join(matches, ") OR (")+"))", "typeof("+item+".key) = 'integer'")
	if len(references) != 0 {
		singular := location.some(nil, location.referenceIn(location.path, len(references)/2))
		clause = "(" + clause + ") OR (" + singular + ")"
		arguments = append(arguments, references...)
	}
	return sqlitePredicate{clause: clause, arguments: arguments, exact: true}
}

// referenceIn holds when the object at path is a {relationTo, id} reference
// equal to one of count candidate pairs.
func (location sqliteJSONLocation) referenceIn(path sqliteJSONPathSQL, count int) string {
	relationTo, id := path.child("relationTo"), path.child("id")
	pairs := make([]string, count)
	for index := range pairs {
		pairs[index] = "(?, ?)"
	}
	// json_extract returns an object or array as JSON text, so the members
	// must also be JSON strings.
	return "(json_extract(" + location.document + ", " + relationTo.sql() + "), json_extract(" + location.document + ", " + id.sql() + ")) IN (VALUES " + strings.Join(pairs, ", ") + ")" +
		" AND " + location.typeAt(relationTo) + " = 'text' AND " + location.typeAt(id) + " = 'text'"
}

func sqlitePlaceholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", count), ", ")
}
