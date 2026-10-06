package population_test

import (
	"fmt"
	"testing"

	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

var populationBenchmarkValues store.Values

// The same response-shaped fixture exercises an observation, a field mapping,
// and the populated-document pass used by response redaction. Setup is excluded.
func BenchmarkPopulationTraversal(b *testing.B) {
	for _, size := range []int{100, 1000, 5000} {
		for _, populated := range []bool{false, true} {
			name := "Unpopulated"
			if populated {
				name = "Populated"
			}
			b.Run(fmt.Sprintf("%s/%d", name, size), func(b *testing.B) {
				fields, values, path := populationBenchmarkFixture(b, size, populated)
				b.Run("VisitAtPath", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						count := 0
						matched := population.VisitAtPath(fields, values, path, population.LocaleSelection{All: true}, func(_ schema.Field, _ store.Value) {
							count++
						})
						if !matched || count != size*2 {
							b.Fatalf("matched=%v callbacks=%d, want %d", matched, count, size*2)
						}
					}
				})
				b.Run("MapAtPath", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						count := 0
						mapped, matched := population.MapAtPath(fields, values, path, population.LocaleSelection{All: true}, func(_ schema.Field, value store.Value) store.Value {
							count++
							return value
						})
						if !matched || count != size*2 {
							b.Fatalf("matched=%v callbacks=%d, want %d", matched, count, size*2)
						}
						populationBenchmarkValues = mapped
					}
				})
				b.Run("MapPopulatedDocuments", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						count := 0
						populationBenchmarkValues = population.MapPopulatedDocuments(fields, values, true, func(_ schema.StableID, _ schema.LocaleCode, document store.Document) store.Document {
							count++
							return document
						})
						want := 0
						if populated {
							want = size * 2
						}
						if count != want {
							b.Fatalf("callbacks=%d, want %d", count, want)
						}
					}
				})
			})
		}
	}
}

func populationBenchmarkFixture(tb testing.TB, size int, populated bool) ([]schema.Field, store.Values, query.Path) {
	mustPath := func(segments ...string) query.Path {
		path, err := query.NewPath(segments...)
		if err != nil {
			tb.Fatal(err)
		}
		return path
	}
	path := mustPath("sections", "content", "quote", "credit", "author")
	quote := schema.BlockType{Slug: "quote", TypeName: "Quote", Fields: []schema.Field{{
		Name: "credit", Path: mustPath("credit"), Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{{
			Name: "author", Path: mustPath("credit", "author"), Type: schema.FieldTypeRelationship, Localized: true,
			Relationship: &schema.RelationshipField{CollectionID: "people", CollectionSlug: "people"},
		}}},
	}}}
	plain := schema.BlockType{Slug: "plain", TypeName: "Plain", Fields: []schema.Field{{Name: "title", Path: mustPath("title"), Type: schema.FieldTypeText}}}
	fields := schematest.Bind(tb, "pages", []schema.BlockType{quote, plain}, schema.Field{
		Name: "sections", Path: mustPath("sections"), Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{{
			Name: "content", Path: mustPath("sections", "content"), Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{BlockReferences: []string{"quote", "plain"}},
		}}},
	})
	rows := make([]store.Value, size)
	for index := range rows {
		locales := store.Values{}
		for _, locale := range []string{"en", "fr"} {
			id := fmt.Sprintf("%s-%d", locale, index)
			reference := store.String(id)
			if populated {
				reference = store.Populated(store.Document{ID: id, Values: store.Values{"name": store.String(id)}})
			}
			locales[locale] = reference
		}
		rows[index] = store.Object(store.Values{
			"_key": store.String(fmt.Sprint(index)),
			"content": store.List(
				store.Object(store.Values{
					"blockType": store.String("quote"),
					"credit":    store.Object(store.Values{"author": store.Object(locales)}),
				}),
				store.Object(store.Values{"blockType": store.String("plain"), "title": store.String("unchanged")}),
			),
		})
	}
	return fields, store.Values{"sections": store.List(rows...), "ordinary": store.String("untouched")}, path
}
