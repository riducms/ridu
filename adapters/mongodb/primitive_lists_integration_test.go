package mongodb

import (
	"fmt"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/primitivelists"
)

func TestPrimitiveListsMongoDBLifecycle(t *testing.T) {
	backend := mongoIntegrationStore(t)
	config := primitivelists.Config()
	config.Collections[0].Access.Publish = config.Collections[0].Access.Update
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.syncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	primitivelists.Exercise(t, app)
}

func TestPrimitiveListsMongoDBRepeatedQueries(t *testing.T) {
	backend := mongoIntegrationStore(t)
	config := primitivelists.RepeatedConfig()
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.syncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	primitivelists.ExerciseRepeatedQueries(t, app)
}

func TestPrimitiveListMongoDBNegativePredicatesExcludeCorruptShapes(t *testing.T) {
	for _, kind := range []string{"text", "number"} {
		t.Run(kind, func(t *testing.T) {
			backend := mongoIntegrationStore(t)
			var list field.Node = field.TextList("items")
			item := store.String("other")
			operand := query.String("oak")
			if kind == "number" {
				list = field.NumberList("items")
				item = store.Number(1)
				operand = query.Number(2)
			}
			manifest, err := core.Resolve(core.Config{Name: "Lists", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{field.Text("title"), list}}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := backend.syncIndexes(t.Context(), manifest); err != nil {
				t.Fatal(err)
			}
			collection := manifest.Snapshot().Collections[0]
			raw := backend.database.Collection(physicalCollectionName(collection.ID))
			for index, value := range []store.Value{store.List(item), store.List(), store.Number(123), store.List(item, store.Null()), store.List(store.Object(store.Values{}))} {
				encoded, err := encodeDocument(store.Document{ID: fmt.Sprintf("product-%d", index), Values: store.Values{"title": store.String("x"), "items": value}, CreatedAt: time.Now(), UpdatedAt: time.Now()})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := raw.InsertOne(t.Context(), encoded); err != nil {
					t.Fatal(err)
				}
			}
			path, _ := query.ParsePath("items")
			title, _ := query.ParsePath("title")
			negated := query.Not(query.In(path, operand))
			either := query.Or(query.Equal(title, "x"), query.In(path, operand))
			for _, expression := range []query.Expression{negated, either} {
				node := expression.Node()
				request := store.Request{Collection: collection, Filter: &node}
				for _, decoderFree := range []bool{false, true} {
					predicate, err := requestPredicate(request, false)
					if decoderFree {
						predicate, err = decoderFreeRequestPredicate(request, false)
					}
					if err != nil {
						t.Fatal(err)
					}
					count, err := raw.CountDocuments(t.Context(), predicate)
					if err != nil || count != 2 {
						t.Fatalf("decoderFree=%v matched %d including corrupt list: %v", decoderFree, count, err)
					}
				}
				transaction, err := backend.Begin(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				page, err := transaction.List(t.Context(), request)
				_ = transaction.Rollback(t.Context())
				if err != nil || *page.Total != 2 {
					t.Fatalf("list page %#v: %v", page, err)
				}
			}
		})
	}
}
