package sqlite

import (
	"fmt"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
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

func TestSQLitePhaseOneBlockBoundsValidation(t *testing.T) {
	field := phaseOneBlocks()
	transaction := sqliteMigrationDataTransaction{locales: map[string]struct{}{"en": {}}}
	validate := func(field schema.Field, values store.Values) error {
		return transaction.validateValues([]schema.Field{field}, values, "posts", nil)
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

func TestSQLitePhaseOneBlockSummaryIsMetadataOnly(t *testing.T) {
	before := phaseOneBlocks()
	after := phaseOneBlocks()
	after.Blocks.ResolvedTypes()[0].Admin = &schema.BlockAdmin{RowLabel: "heading"}
	if err := validateSQLiteAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
	after.Blocks.MinRows++
	if err := validateSQLiteAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err == nil {
		t.Fatal("tightened bounds admitted as metadata-only")
	}
}
