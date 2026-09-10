package mongodb

import (
	"encoding/json"
	"fmt"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"reflect"
	"strings"
	"testing"
)

func phaseOneBlocks() schema.Field {
	path, _ := query.ParsePath("layout")
	return schema.Field{ID: "layout", Name: "layout", Path: path, Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested,
		Blocks: &schema.BlocksField{MinRows: 2, MaxRows: 3, Types: []schema.BlockType{{Slug: "hero", Labels: schema.BlockLabels{Singular: "Hero"}, Fields: []schema.Field{}}}}}
}
func phaseOneBlockRows(count int) store.Value {
	rows := make([]store.Value, count)
	for i := range rows {
		rows[i] = store.Object(store.Values{"_key": store.String(fmt.Sprintf("row-%d", i)), "blockType": store.String("hero")})
	}
	return store.List(rows...)
}

func TestMongoDBPhaseOneBlockBoundsValidation(t *testing.T) {
	field := phaseOneBlocks()
	validate := func(field schema.Field, values store.Values) error {
		return validateMongoFieldValues("posts", []schema.Field{field}, values, nil, true, false, map[string]struct{}{"en": {}}, nil)
	}
	for _, values := range []store.Values{{}, {"layout": store.Null()}} {
		if err := validate(field, values); err != nil {
			t.Fatalf("optional absent/null: %v", err)
		}
	}
	for _, count := range []int{0, 1, 2, 3, 4} {
		err := validate(field, store.Values{"layout": phaseOneBlockRows(count)})
		invalid := count < 2 || count > 3
		if (err != nil) != invalid {
			t.Fatalf("%d rows: %v", count, err)
		}
		if err != nil && !strings.Contains(err.Error(), "layout") {
			t.Fatalf("missing precise field path: %v", err)
		}
	}
	field.Localized = true
	if err := validate(field, store.Values{"layout": store.Object(store.Values{"en": phaseOneBlockRows(1)})}); err == nil || !strings.Contains(err.Error(), "layout.en") {
		t.Fatalf("localized bounds: %v", err)
	}
}

func TestMongoDBPhaseOneBlockSummaryIsMetadataOnly(t *testing.T) {
	before := phaseOneBlocks()
	after := phaseOneBlocks()
	after.Blocks.ResolvedTypes()[0].Admin = &schema.BlockAdmin{RowLabel: "heading"}
	if err := validateMongoDBAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
	after.Blocks.MinRows++
	if err := validateMongoDBAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err == nil {
		t.Fatal("tightened bounds admitted as metadata-only")
	}
}

func TestMongoDBPhaseOneBlockValidatorCarriesBoundsAndNullability(t *testing.T) {
	field := phaseOneBlocks()
	for _, required := range []bool{false, true} {
		field.Required = required
		encoded, err := bson.MarshalExtJSON(mongoRepeatedRootJSONSchema(field, nil), false, false)
		if err != nil {
			t.Fatal(err)
		}
		var actual map[string]any
		if err := json.Unmarshal(encoded, &actual); err != nil {
			t.Fatal(err)
		}
		if actual["minItems"] != float64(2) || actual["maxItems"] != float64(3) {
			t.Fatalf("bounds schema = %s", encoded)
		}
		if required {
			if actual["bsonType"] != "array" {
				t.Fatalf("required schema = %s", encoded)
			}
		} else if !reflect.DeepEqual(actual["bsonType"], []any{"array", "null"}) {
			t.Fatalf("optional schema = %s", encoded)
		}
	}
	field.Blocks.MinRows = 0
	encoded, err := bson.MarshalExtJSON(mongoRepeatedRootJSONSchema(field, nil), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"minItems":1`) {
		t.Fatalf("required blocks must still reject empty lists: %s", encoded)
	}
}
