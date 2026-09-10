package postgres

import (
	"encoding/json"
	"io"
	"math/big"
	"reflect"
	"strings"

	atlaspostgres "ariga.io/atlas/sql/postgres"
	atlasschema "ariga.io/atlas/sql/schema"
)

// PostgreSQL rewrites jsonb literals (spacing, object order, and number spelling).
// Compare literal values exactly before Atlas compares their SQL spelling. Do not
// evaluate expressions: a changed function default must remain physical drift.
func normalizePostgresJSONBDefaults(actual, expected *atlasschema.Schema) {
	for _, table := range actual.Tables {
		expectedTable, exists := expected.Table(table.Name)
		if !exists {
			continue
		}
		for _, column := range table.Columns {
			expectedColumn, exists := expectedTable.Column(column.Name)
			if !exists || !isJSONBColumn(column) || !isJSONBColumn(expectedColumn) {
				continue
			}
			from, fromOK := canonicalJSONBDefault(column.Default)
			to, toOK := canonicalJSONBDefault(expectedColumn.Default)
			if fromOK && toOK && reflect.DeepEqual(from, to) {
				column.Default = expectedColumn.Default
			}
		}
	}
}

func isJSONBColumn(column *atlasschema.Column) bool {
	if column.Type == nil {
		return false
	}
	typeInfo, ok := column.Type.Type.(*atlasschema.JSONType)
	return ok && typeInfo.T == atlaspostgres.TypeJSONB
}

func canonicalJSONBDefault(expression atlasschema.Expr) (any, bool) {
	var value string
	switch expression := expression.(type) {
	case *atlasschema.Literal:
		value = strings.TrimSpace(expression.V)
	case *atlasschema.RawExpr:
		value = strings.TrimSpace(expression.X)
		if !strings.HasSuffix(value, "::jsonb") {
			return nil, false
		}
		value = strings.TrimSpace(strings.TrimSuffix(value, "::jsonb"))
		if !strings.HasPrefix(value, "'") {
			return nil, false
		}
	default:
		return nil, false
	}
	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return nil, false
		}
		value = value[1 : len(value)-1]
		if strings.Contains(strings.ReplaceAll(value, "''", ""), "'") {
			return nil, false
		}
		value = strings.ReplaceAll(value, "''", "'")
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if decoder.Decode(&decoded) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return canonicalJSONNumbers(decoded)
}

// A distinct type keeps the JSON number 1 separate from the JSON string "1".
type canonicalJSONNumber string

func canonicalJSONNumbers(value any) (any, bool) {
	switch value := value.(type) {
	case json.Number:
		number, ok := new(big.Rat).SetString(string(value))
		if !ok {
			return nil, false
		}
		return canonicalJSONNumber(number.RatString()), true
	case []any:
		for index, item := range value {
			canonical, ok := canonicalJSONNumbers(item)
			if !ok {
				return nil, false
			}
			value[index] = canonical
		}
		return value, true
	case map[string]any:
		for key, item := range value {
			canonical, ok := canonicalJSONNumbers(item)
			if !ok {
				return nil, false
			}
			value[key] = canonical
		}
		return value, true
	default:
		return value, true
	}
}
