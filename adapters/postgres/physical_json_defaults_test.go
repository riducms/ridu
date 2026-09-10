package postgres

import (
	"strings"
	"testing"

	atlaspostgres "ariga.io/atlas/sql/postgres"
	atlasschema "ariga.io/atlas/sql/schema"
)

func TestPostgresJSONBDefaultNormalizationPreservesMeaning(t *testing.T) {
	for _, test := range []struct {
		name     string
		actual   string
		expected string
		equal    bool
	}{
		{"array spacing", `["Fixed", "Fixed"]`, `["Fixed","Fixed"]`, true},
		{"object order", `{"long": 2, "a": 1}`, `{"a":1,"long":2}`, true},
		{"SQL and JSON quotes", `["O'Reilly", "a\\b", "\"quoted\"", "\n"]`, `["O'Reilly","a\\b","\"quoted\"","\n"]`, true},
		{"exponents and decimals", `[1000000000000000000000, 0.0000001, 1.2500, 0]`, `[1e21,1e-7,1.25,-0]`, true},
		{"different large integers", `[9007199254740992]`, `[9007199254740993]`, false},
		{"string is not number", `["1"]`, `[1]`, false},
		{"array order", `[1,2]`, `[2,1]`, false},
		{"array duplicates", `[1,1]`, `[1]`, false},
		{"object changed", `{"a":1}`, `{"a":2}`, false},
		{"null is not empty array", `null`, `[]`, false},
		{"trailing JSON value", `[1] [2]`, `[1]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, cast := range []bool{false, true} {
				literal := "'" + strings.ReplaceAll(test.actual, "'", "''") + "'"
				var actual atlasschema.Expr = &atlasschema.Literal{V: literal}
				if cast {
					actual = &atlasschema.RawExpr{X: literal + "::jsonb"}
				}
				assertJSONBDefaultNormalization(t, actual, &atlasschema.Literal{V: test.expected}, atlaspostgres.TypeJSONB, test.equal)
			}
		})
	}
}

func TestPostgresJSONBDefaultNormalizationLeavesOtherSQLUntouched(t *testing.T) {
	for _, expression := range []atlasschema.Expr{
		nil,
		&atlasschema.RawExpr{X: "jsonb_build_array(1)"},
		&atlasschema.RawExpr{X: "jsonb_build_array(1)::jsonb"},
		&atlasschema.RawExpr{X: "'[1]'::jsonb || '[2]'::jsonb"},
		&atlasschema.RawExpr{X: "'[1]'::text"},
	} {
		assertJSONBDefaultNormalization(t, expression, &atlasschema.Literal{V: "[1]"}, atlaspostgres.TypeJSONB, false)
	}
	assertJSONBDefaultNormalization(t, &atlasschema.Literal{V: "'[1, 2]'"}, &atlasschema.Literal{V: "[1,2]"}, atlaspostgres.TypeJSON, false)
	assertJSONBDefaultNormalization(t, &atlasschema.Literal{V: "'null'"}, nil, atlaspostgres.TypeJSONB, false)
}

func assertJSONBDefaultNormalization(t *testing.T, actualDefault, expectedDefault atlasschema.Expr, kind string, equal bool) {
	t.Helper()
	actualColumn := atlasschema.NewJSONColumn("value", kind).SetDefault(actualDefault)
	expectedColumn := atlasschema.NewJSONColumn("value", kind).SetDefault(expectedDefault)
	actual := atlasschema.New("public").AddTables(atlasschema.NewTable("records").AddColumns(actualColumn))
	expected := atlasschema.New("public").AddTables(atlasschema.NewTable("records").AddColumns(expectedColumn))
	normalizePostgresJSONBDefaults(actual, expected)
	if equal && actualColumn.Default != expectedDefault {
		t.Fatalf("equivalent JSONB defaults were not normalized: %#v / %#v", actualDefault, expectedDefault)
	}
	if !equal && actualColumn.Default != actualDefault {
		t.Fatalf("changed or nonliteral default was normalized: %#v / %#v", actualDefault, expectedDefault)
	}
}
