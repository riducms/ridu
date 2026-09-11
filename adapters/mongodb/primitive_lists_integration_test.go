package mongodb

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	primitivelists.Exercise(t, app)
}

func TestPrimitiveListsMongoDBRepeatedQueryBoundary(t *testing.T) {
	backend := mongoIntegrationStore(t)
	config := primitivelists.Config()
	config.Collections[0].Fields = append(config.Collections[0].Fields, field.Group("localizedDetails", field.Fields{field.TextList("points")}).Localized())
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Local().Create(t.Context(), "primitive-products", primitivelists.Values(), core.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path, diagnostic string
		item             query.Value
	}{
		{"variants.points", "unsupported non-scalar field type", query.String("Oak")},
		{"variants.sizes", "unsupported non-scalar field type", query.Number(0)},
		{"content.card.points", "unsupported non-scalar field type", query.String("Oak")},
		{"content.card.sizes", "unsupported non-scalar field type", query.Number(0)},
		{"localizedDetails.points", "localized containers are not supported", query.String("Oak")},
	} {
		path, _ := query.ParsePath(test.path)
		for _, negate := range []bool{false, true} {
			expression := query.In(path, test.item)
			if negate {
				expression, _ = query.Not(expression)
			}
			_, err := app.Local().List(t.Context(), "primitive-products", core.ListOptions{Where: expression})
			var failure *core.OperationError
			if !errors.As(err, &failure) || failure.Code != "bad_query" || failure.Status != 400 || len(failure.Issues) != 1 || failure.Issues[0].Code != "unsupported_path" || failure.Issues[0].Path != test.path || !strings.Contains(err.Error(), test.diagnostic) {
				t.Fatalf("path %s negate=%v must reject explicitly: %v", test.path, negate, err)
			}
		}
		operand, _ := json.Marshal(test.item)
		where := fmt.Sprintf(`{%q:{"in":[%s]}}`, test.path, operand)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/collections/primitive-products?where="+url.QueryEscape(where), nil)
		app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
		if response.Code != 400 || !strings.Contains(response.Body.String(), `"code":"bad_request"`) || !strings.Contains(response.Body.String(), fmt.Sprintf(`"path":%q`, test.path)) || !strings.Contains(response.Body.String(), `"code":"unsupported_path"`) {
			t.Fatalf("REST path %s %d: %s", test.path, response.Code, response.Body.String())
		}
	}
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
			if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
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
			negated, _ := query.Not(query.In(path, operand))
			either, _ := query.Or(query.Equal(title, query.String("x")), query.In(path, operand))
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
				if err != nil || page.Total != 2 {
					t.Fatalf("list page %#v: %v", page, err)
				}
			}
		})
	}
}
