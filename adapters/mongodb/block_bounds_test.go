package mongodb

import (
	"encoding/json"
	"fmt"
	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"reflect"
	"strings"
	"testing"
)

// phaseOneBlocks places the hero definition, after change edits it when given.
func phaseOneBlocks(t *testing.T, change func(*schema.BlockType)) schema.Field {
	path, _ := query.ParsePath("layout")
	hero := schema.BlockType{Slug: "hero", TypeName: "Hero", Labels: schema.BlockLabels{Singular: "Hero"}, Fields: []schema.Field{}}
	if change != nil {
		change(&hero)
	}
	return schematest.Bind(t, "posts", []schema.BlockType{hero}, schema.Field{ID: "layout", Name: "layout", Path: path, Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested,
		Blocks: &schema.BlocksField{MinRows: 2, MaxRows: 3, BlockReferences: []string{"hero"}}})[0]
}
func phaseOneBlockRows(count int) store.Value {
	rows := make([]store.Value, count)
	for i := range rows {
		rows[i] = store.Object(store.Values{"_key": store.String(fmt.Sprintf("row-%d", i)), "blockType": store.String("hero")})
	}
	return store.List(rows...)
}

// The stored shape keeps the upper bound. The minimum row count, like
// requiredness, is a completeness rule that drafts defer, so storage admits
// fewer rows.
func TestMongoDBPhaseOneBlockBoundsValidation(t *testing.T) {
	field := phaseOneBlocks(t, nil)
	field.Required = true
	validate := func(field schema.Field, values store.Values) error {
		return validateMongoFieldValues("posts", []schema.Field{field}, values, nil, map[string]struct{}{"en": {}}, nil)
	}
	for _, values := range []store.Values{{}, {"layout": store.Null()}} {
		if err := validate(field, values); err != nil {
			t.Fatalf("absent/null required blocks: %v", err)
		}
	}
	for _, count := range []int{0, 1, 2, 3, 4} {
		err := validate(field, store.Values{"layout": phaseOneBlockRows(count)})
		invalid := count > 3
		if (err != nil) != invalid {
			t.Fatalf("%d rows: %v", count, err)
		}
		if err != nil && !strings.Contains(err.Error(), "layout") {
			t.Fatalf("missing precise field path: %v", err)
		}
	}
	field.Localized = true
	if err := validate(field, store.Values{"layout": store.Object(store.Values{"en": phaseOneBlockRows(4)})}); err == nil || !strings.Contains(err.Error(), "layout.en") {
		t.Fatalf("localized bounds: %v", err)
	}
}

func TestMongoDBPhaseOneBlockSummaryIsMetadataOnly(t *testing.T) {
	before := phaseOneBlocks(t, nil)
	after := phaseOneBlocks(t, func(hero *schema.BlockType) { hero.Admin = &schema.BlockAdmin{RowLabel: "heading"} })
	if err := validateMongoDBAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
	after.Blocks.MinRows++
	if err := validateMongoDBAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err == nil {
		t.Fatal("tightened bounds admitted as metadata-only")
	}
}

// The decoder-free shape guard mirrors the stored shape: an upper bound, but
// no minimum or non-null requirement, whether or not the field is required.
func TestMongoDBPhaseOneBlockValidatorCarriesBoundsAndNullability(t *testing.T) {
	field := phaseOneBlocks(t, nil)
	for _, required := range []bool{false, true} {
		field.Required = required
		encoded, err := bson.MarshalExtJSON(mongoRepeatedRootJSONSchema(field, nil, mongoRowFields), false, false)
		if err != nil {
			t.Fatal(err)
		}
		var actual map[string]any
		if err := json.Unmarshal(encoded, &actual); err != nil {
			t.Fatal(err)
		}
		if _, minimum := actual["minItems"]; minimum || actual["maxItems"] != float64(3) {
			t.Fatalf("bounds schema = %s", encoded)
		}
		if !reflect.DeepEqual(actual["bsonType"], []any{"array", "null"}) {
			t.Fatalf("required=%t schema = %s", required, encoded)
		}
	}
}
