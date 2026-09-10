package operation

import (
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestDuplicateLegacyRowKeysCannotCollapseChangedFieldLocations(t *testing.T) {
	path := func(value string) query.Path {
		parsed, err := query.ParsePath(value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	secret := func(value string) store.Value { return store.String(value) }
	tests := []struct {
		name       string
		fields     []schema.Field
		fieldPath  string
		beforeData store.Values
		afterData  store.Values
	}{
		{
			name: "array",
			fields: []schema.Field{{
				Name: "rows", Path: path("rows"), Type: schema.FieldTypeArray,
				Nested: &schema.NestedField{
					Fields: []schema.Field{{Name: "secret", Path: path("rows.secret"), Type: schema.FieldTypeText}},
				},
			}},
			fieldPath: "rows.secret",
			beforeData: store.Values{"rows": store.List(
				store.Object(store.Values{"_key": store.String("duplicate"), "secret": secret("first")}),
				store.Object(store.Values{"_key": store.String("duplicate"), "secret": secret("stable")}),
			)},
			afterData: store.Values{"rows": store.List(
				store.Object(store.Values{"_key": store.String("duplicate"), "secret": secret("changed")}),
				store.Object(store.Values{"_key": store.String("duplicate"), "secret": secret("stable")}),
			)},
		},
		{
			name: "blocks",
			fields: []schema.Field{{
				Name: "content", Path: path("content"), Type: schema.FieldTypeBlocks,
				Blocks: &schema.BlocksField{Types: []schema.BlockType{{
					Slug: "quote", Fields: []schema.Field{{Name: "secret", Path: path("content.quote.secret"), Type: schema.FieldTypeText}},
				}}},
			}},
			fieldPath: "content.quote.secret",
			beforeData: store.Values{"content": store.List(
				store.Object(store.Values{"_key": store.String("duplicate"), "blockType": store.String("quote"), "secret": secret("first")}),
				store.Object(store.Values{"_key": store.String("duplicate"), "blockType": store.String("quote"), "secret": secret("stable")}),
			)},
			afterData: store.Values{"content": store.List(
				store.Object(store.Values{"_key": store.String("duplicate"), "blockType": store.String("quote"), "secret": secret("changed")}),
				store.Object(store.Values{"_key": store.String("duplicate"), "blockType": store.String("quote"), "secret": secret("stable")}),
			)},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := fieldLocationsAtPath(test.fields, test.beforeData, test.fieldPath, false)
			after := fieldLocationsAtPath(test.fields, test.afterData, test.fieldPath, false)
			changed := changedFieldLocations(before, after)
			if len(changed) != 1 || changed[0].runtimePath != map[string]string{"array": "rows.0.secret", "blocks": "content.0.secret"}[test.name] {
				t.Fatalf("changed locations = %#v", changed)
			}
		})
	}
}
