package operation

import (
	"fmt"
	"testing"

	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestBlocksBoundsPresenceSemantics(t *testing.T) {
	row := store.Object(store.Values{"blockType": store.String("hero")})
	for _, required := range []bool{false, true} {
		fields := schematest.Bind(t, "pages", []schema.BlockType{{Slug: "hero", TypeName: "Hero"}}, schema.Field{Name: "layout", Type: schema.FieldTypeBlocks, Required: required, Blocks: &schema.BlocksField{MinRows: 2, MaxRows: 3, BlockReferences: []string{"hero"}}})
		for _, test := range []struct {
			name   string
			values store.Values
			code   string
		}{
			{"absent", store.Values{}, ""}, {"null", store.Values{"layout": store.Null()}, ""}, {"empty", store.Values{"layout": store.List()}, "min_rows"}, {"one", store.Values{"layout": store.List(row)}, "min_rows"}, {"minimum", store.Values{"layout": store.List(row, row)}, ""}, {"maximum", store.Values{"layout": store.List(row, row, row)}, ""}, {"over", store.Values{"layout": store.List(row, row, row, row)}, "max_rows"},
		} {
			t.Run(test.name+map[bool]string{true: "Required", false: "Optional"}[required], func(t *testing.T) {
				code := test.code
				if required && (test.name == "absent" || test.name == "null" || test.name == "empty") {
					code = "required"
				}
				_, issues := validate(fields, test.values, true, nil)
				if code == "" {
					if len(issues) != 0 {
						t.Fatalf("unexpected issues %#v", issues)
					}
				} else {
					assertIssue(t, issues, code, "layout")
				}
			})
		}
	}
}

func BenchmarkPrepareRowIdentities(b *testing.B) {
	hero := schema.BlockType{Slug: "hero", TypeName: "Hero", Fields: []schema.Field{{Name: "content", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "heading", Type: schema.FieldTypeText}}}}}}
	fields := schematest.Bind(b, "pages", []schema.BlockType{hero}, schema.Field{Name: "layout", Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{BlockReferences: []string{"hero"}}})
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			rows := make([]store.Value, count)
			for i := range rows {
				rows[i] = store.Object(store.Values{"_key": store.String(fmt.Sprintf("row-%d", i)), "blockType": store.String("hero"), "content": store.Object(store.Values{"heading": store.String("Nested heading")})})
			}
			values := store.Values{"layout": store.List(rows...)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := prepareRowIdentities(fields, values, true, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestBlockIdentityIssuesUseValidationEnvelopeLimit(t *testing.T) {
	fields := schematest.Bind(t, "pages", []schema.BlockType{{Slug: "hero", TypeName: "Hero"}}, schema.Field{Name: "layout", Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{BlockReferences: []string{"hero"}}})
	rows := make([]store.Value, MaxValidationIssues*2)
	for i := range rows {
		rows[i] = store.Object(store.Values{"_key": store.String(""), "blockType": store.String("hero")})
	}
	_, err := prepareRowIdentities(fields, store.Values{"layout": store.List(rows...)}, false, false)
	failure, ok := err.(*Error)
	if !ok || len(failure.Issues) != MaxValidationIssues {
		t.Fatalf("identity failure = %#v", err)
	}
	if failure.Issues[0].Path != "layout.0._key" {
		t.Fatalf("first path = %q", failure.Issues[0].Path)
	}
}
