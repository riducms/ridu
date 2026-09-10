package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestLiveValidationTransportLimitsAndFailurePrivacy(t *testing.T) {
	path, _ := query.NewPath("sku")
	field := schema.Field{ID: "sku", Name: "sku", Type: schema.FieldTypeText, Path: path}
	collection := schema.Collection{ID: "products", Slug: "products", Fields: []schema.Field{field}}
	calls := 0
	fail := false
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Collections: []operationengine.Collection{{Schema: collection, Bindings: []operationengine.FieldBinding{{Field: field, LiveValidators: []operationengine.FieldLiveValidator{func(ctx operationengine.Context) ([]schema.Issue, bool, error) {
		calls++
		if fail {
			return nil, true, errors.New("private database DSN password")
		}
		return nil, true, nil
	}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Config{Engine: engine, Manifest: schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Collections: []schema.Collection{collection}}), MaxBodyBytes: 2 << 20})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	for _, test := range []struct {
		name, path, body string
		status           int
	}{
		{"oversized", "/api/collections/products/validate", `{"data":{"sku":"` + strings.Repeat("x", 1<<20) + `"},"fields":["sku"]}`, 413},
		{"unknown option", "/api/collections/products/validate", `{"data":{},"fields":["sku"],"actor":{"id":"admin"}}`, 400},
		{"duplicate input", "/api/collections/products/validate", `{"data":{"sku":"a","sku":"b"},"fields":["sku"]}`, 400},
		{"missing data", "/api/collections/products/validate", `{"fields":["sku"]}`, 400},
		{"all locales", "/api/collections/products/validate?locale=all", `{"data":{},"fields":["sku"]}`, 400},
		{"fallback", "/api/collections/products/validate?fallback-locale=en", `{"data":{},"fields":["sku"]}`, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := request(http.MethodPost, test.path, test.body)
			if response.Code != test.status {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
		})
	}
	if calls != 0 {
		t.Fatal("invalid request invoked callbacks")
	}
	response := request(http.MethodGet, "/api/collections/products/validate", "")
	if response.Code != 405 {
		t.Fatalf("method=%d", response.Code)
	}
	response = request(http.MethodPost, "/api/collections/products/validate", `{"data":{},"fields":["sku"]}`)
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("valid=%d %s", response.Code, response.Body.String())
	}
	fail = true
	response = request(http.MethodPost, "/api/collections/products/validate", `{"data":{},"fields":["sku"]}`)
	if response.Code != 500 || strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), "checked") {
		t.Fatalf("failure=%d %s", response.Code, response.Body.String())
	}
}
