// Package queryaccess exercises caller query confidentiality through the shared
// operation engine and REST against each real database adapter.
package queryaccess

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/operation"
	fieldoperation "github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type Factory func(*testing.T, ridu.Config) (store.Store, *ridu.App)

// Options identifies existing adapter query capabilities; confidentiality does
// not introduce opaque JSON descendant querying where it is not supported.
type Options struct {
	OpaqueJSONDescendants bool
}

// Run verifies that denied query admission is independent of stored values,
// actors and adapter query compilation, while trusted access stays atomic.
func Run(t *testing.T, factory Factory, options Options) {
	t.Helper()
	_, app := factory(t, configuration())
	for i, name := range []string{"Alpha", "Beta", "Classified"} {
		audience := "public"
		if i == 2 {
			audience = "private"
		}
		_, err := app.Local().Create(t.Context(), "employees", store.Values{
			"name": store.String(name), "audience": store.String(audience),
			"salary": store.Number(float64(73500 + i*51500)), "secret": store.String("launch-orchid"),
			"profile":      store.Object(store.Values{"secret": store.String("orchid")}),
			"vault":        store.Object(store.Values{"value": store.String("orchid")}),
			"rows":         store.List(store.Object(store.Values{"secret": store.String("orchid")})),
			"lockedRows":   store.List(store.Object(store.Values{"value": store.String("orchid")})),
			"layout":       store.List(store.Object(store.Values{"blockType": store.String("hero"), "secret": store.String("orchid")})),
			"lockedLayout": store.List(store.Object(store.Values{"blockType": store.String("hero"), "value": store.String("orchid")})),
			"body":         richTextSecret(t),
			"translation":  store.String("orchid"),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	handler := app.Handler(ridu.HandlerOptions{})
	t.Run("noncanonical-paths-cannot-bypass-field-access", func(t *testing.T) {
		for _, name := range []string{"layout.secret", "body.root.children.fields.secret"} {
			_, err := app.Local().List(t.Context(), "employees", ridu.ListOptions{Where: query.Contains(path(name), "orch")})
			var failure *operation.Error
			if !errors.As(err, &failure) || failure.Status != http.StatusBadRequest || failure.Code != "bad_query" {
				t.Fatalf("noncanonical query %s = %#v, want bad_query (400)", name, err)
			}
		}
	})
	t.Run("public-block-path-cannot-match-private-variant-shape", func(t *testing.T) {
		variantIsolation(t, factory, options)
	})
	t.Run("internal-upload-lookup-preserves-collection-access", func(t *testing.T) {
		uploadLookup(t, factory)
	})
	t.Run("binary-search-substrings-order-and-count", func(t *testing.T) {
		for _, threshold := range []float64{0, 65536, 73728, 73500, 73501, 1000000} {
			t.Run(fmt.Sprint(threshold), func(t *testing.T) {
				_, err := app.Local().List(t.Context(), "employees", ridu.ListOptions{Where: query.GreaterThanEqual(path("salary"), query.Number(threshold)), Limit: 1})
				denied(t, err, "salary")
				where := fmt.Sprintf(`{"salary":{"greaterThanEqual":%g}}`, threshold)
				for _, route := range []string{"", "/count"} {
					response := request(handler, http.MethodGet, "/api/collections/employees"+route+"?where="+url.QueryEscape(where), "")
					deniedHTTP(t, response, "salary")
				}
			})
		}
		for _, fragment := range []string{"orch", "xyz", "launch-"} {
			or, err := query.Or(query.Equal(path("name"), query.String("Alpha")), query.Contains(path("secret"), fragment))
			if err != nil {
				t.Fatal(err)
			}
			not, err := query.Not(query.Contains(path("secret"), fragment))
			if err != nil {
				t.Fatal(err)
			}
			for _, where := range []query.Expression{
				query.Contains(path("secret"), fragment),
				or, not,
			} {
				_, err := app.Local().List(t.Context(), "employees", ridu.ListOptions{Where: where})
				denied(t, err, "secret")
			}
			response := request(handler, http.MethodGet, "/api/collections/employees/count?where="+url.QueryEscape(fmt.Sprintf(`{"secret":{"contains":%q}}`, fragment)), "")
			deniedHTTP(t, response, "secret")
		}
		for _, direction := range []query.Direction{query.Ascending, query.Descending} {
			_, err := app.Local().List(t.Context(), "employees", ridu.ListOptions{Sort: []query.Sort{{Path: path("salary"), Direction: direction}}})
			denied(t, err, "salary")
		}
		// PostgreSQL can order an entire JSONB container, comparing hidden
		// descendants even though its parent has no Read rule of its own.
		for _, container := range []string{"profile", "rows", "layout", "body"} {
			for _, direction := range []query.Direction{query.Ascending, query.Descending} {
				_, err := app.Local().List(t.Context(), "employees", ridu.ListOptions{Sort: []query.Sort{{Path: path(container), Direction: direction}}, Limit: 1})
				denied(t, err, container)
			}
		}
		for _, sort := range []string{"salary", "-salary", "name&sort=-salary"} {
			deniedHTTP(t, request(handler, http.MethodGet, "/api/collections/employees?sort="+sort, ""), "salary")
		}
	})
	t.Run("schema-paths-ancestors-locales-and-actors", func(t *testing.T) {
		for _, name := range []string{"profile.secret", "vault.value", "rows.secret", "lockedRows.value", "layout.hero.secret", "lockedLayout.hero.value", "layout.private.value.label", "body.blocks.block.hero.secret", "body.blocks.block.hero.vault.value", "translation"} {
			for _, locale := range []schema.LocaleCode{"en", "fr"} {
				_, err := app.Local().List(t.Context(), "employees", ridu.ListOptions{Where: query.Equal(path(name), query.String("orchid")), Locale: locale})
				denied(t, err, name)
			}
		}
		// Even a rule that allows this actor remains document-aware; admission
		// must not speculate by evaluating it once without the target rows.
		_, err := app.Local().List(t.Context(), "employees", ridu.ListOptions{Where: query.Equal(path("secret"), query.String("launch-orchid")), Actor: &store.Document{ID: "staff"}, ActorCollection: "users"})
		denied(t, err, "secret")
		for _, name := range []string{"profile.secret", "vault.value", "rows.secret", "layout.hero.secret", "translation"} {
			where := fmt.Sprintf(`{%q:{"equals":"orchid"}}`, name)
			deniedHTTP(t, request(handler, http.MethodGet, "/api/collections/employees?locale=fr&where="+url.QueryEscape(where), ""), name)
		}
	})
	t.Run("distinct-selection-and-window", func(t *testing.T) {
		_, err := app.Local().Distinct(t.Context(), "employees", ridu.DistinctOptions{Field: path("secret")})
		denied(t, err, "secret")
		_, err = app.Local().Distinct(t.Context(), "employees", ridu.DistinctOptions{Field: path("name"), Where: query.Contains(path("secret"), "orch")})
		denied(t, err, "secret")
		deniedHTTP(t, request(handler, http.MethodPost, "/api/access/collections/employees/selection", `{"where":{"salary":{"greaterThan":100000}}}`), "salary")
		_, err = app.Local().ListWindow(t.Context(), "windows", ridu.ListWindowOptions{Index: path("privateKey"), LowerBound: "a", UpperBound: "z", Limit: 1})
		denied(t, err, "privateKey")
	})
	t.Run("trusted-hidden-access-predicate-preserves-pagination", func(t *testing.T) {
		for pageNumber, want := range []string{"Alpha", "Beta"} {
			page, err := app.Local().List(t.Context(), "employees", ridu.ListOptions{Where: query.Contains(path("name"), "a"), Sort: []query.Sort{{Path: path("name"), Direction: query.Ascending}}, Page: pageNumber + 1, Limit: 1})
			if err != nil || page.Total != 2 || len(page.Documents) != 1 {
				t.Fatalf("trusted access pagination: %#v, %v", page, err)
			}
			got, _ := page.Documents[0].Values["name"].StringValue()
			if got != want {
				t.Fatalf("page %d = %q, want %q", pageNumber+1, got, want)
			}
			for _, secret := range []string{"salary", "secret", "audience", "vault", "translation"} {
				if _, exists := page.Documents[0].Values[secret]; exists {
					t.Fatalf("read leaked %s", secret)
				}
			}
		}
		values, err := app.Local().Distinct(t.Context(), "employees", ridu.DistinctOptions{Field: path("name")})
		if err != nil || values.Total != 2 {
			t.Fatalf("trusted distinct: %#v, %v", values, err)
		}
		response := request(handler, http.MethodGet, "/api/collections/employees/count?where="+url.QueryEscape(`{"name":{"contains":"a"}}`), "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"totalDocs":2`) {
			t.Fatalf("trusted count: %d %s", response.Code, response.Body.String())
		}
		response = request(handler, http.MethodPost, "/api/access/collections/employees/selection", `{"where":{"name":{"equals":"Alpha"}}}`)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"totalDocs":1`) {
			t.Fatalf("trusted selection: %d %s", response.Code, response.Body.String())
		}
		_, err = app.Local().List(t.Context(), "employees", ridu.ListOptions{Where: query.Equal(path("audience"), query.String("private"))})
		denied(t, err, "audience")
	})
	t.Run("authored-auth-secrets-and-framework-credentials", func(t *testing.T) {
		_, err := app.CreateAuthUser(t.Context(), "users", store.Values{"email": store.String("editor@example.test"), "privateToken": store.String("orchid-token")}, "Str0ng-test-password!", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := app.Login(t.Context(), "users", "editor@example.test", "Str0ng-test-password!"); err != nil {
			t.Fatalf("credential lookup broken: %v", err)
		}
		_, err = app.Local().List(t.Context(), "users", ridu.ListOptions{Where: query.Contains(path("privateToken"), "orch")})
		denied(t, err, "privateToken")
		_, err = app.Local().Distinct(t.Context(), "users", ridu.DistinctOptions{Field: path("privateToken")})
		denied(t, err, "privateToken")
		deniedHTTP(t, request(handler, http.MethodGet, "/api/collections/users?sort=-privateToken", ""), "privateToken")
		for _, route := range []string{"", "/count"} {
			deniedHTTP(t, request(handler, http.MethodGet, "/api/collections/users"+route+"?where="+url.QueryEscape(`{"privateToken":{"contains":"orch"}}`), ""), "privateToken")
		}
		for _, name := range []string{"password", "passwordHash", "hash", "salt"} {
			response := request(handler, http.MethodGet, "/api/collections/users?where="+url.QueryEscape(fmt.Sprintf(`{%q:{"equals":"guess"}}`, name)), "")
			if response.Code != http.StatusBadRequest {
				t.Fatalf("credential path %s: %d %s", name, response.Code, response.Body.String())
			}
		}
	})
}

func configuration() ridu.Config {
	deny := field.Access{Read: func(fieldoperation.AccessContext) (bool, error) { return false, nil }}
	return ridu.Config{Name: "Query confidentiality", Plugins: []ridu.Plugin{richtext.New()}, Admin: ridu.AdminConfig{User: "users"},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}},
		Collections: []ridu.Collection{
			{Slug: "employees", Fields: field.Fields{
				field.Text("name"), field.Number("salary").Access(deny),
				field.Text("secret").Access(field.Access{Read: func(ctx fieldoperation.AccessContext) (bool, error) {
					return ctx.Actor.ID != "" && ctx.Actor.Collection == "users", nil
				}}),
				field.Text("audience").Access(deny), field.Text("translation").Localized().Access(deny),
				field.Group("profile", field.Fields{field.Text("secret").Access(deny)}),
				field.Group("vault", field.Fields{field.Text("value")}).Access(deny),
				field.Array("rows", field.Fields{field.Text("secret").Access(deny)}),
				field.Array("lockedRows", field.Fields{field.Text("value")}).Access(deny),
				field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("secret").Access(deny)}}, field.Block{Slug: "private", Fields: field.Fields{field.Group("value", field.Fields{field.Text("label")}).Access(deny)}}),
				field.Blocks("lockedLayout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("value")}}).Access(deny),
				richtext.Field("body", richtext.Config{Blocks: []field.Block{{Slug: "hero", Fields: field.Fields{field.Text("secret").Access(deny), field.Group("vault", field.Fields{field.Text("value")}).Access(deny)}}}}),
			}, Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Where(query.Equal(path("audience"), query.String("public"))), nil
			}}},
			{Slug: "windows", Fields: field.Fields{field.Text("privateKey").Unique().Index().Access(deny)}},
			{Slug: "users", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique(), field.Text("privateToken").Access(deny)}},
		},
	}
}

func path(value string) query.Path {
	path, err := query.ParsePath(value)
	if err != nil {
		panic(err)
	}
	return path
}

func richTextSecret(t *testing.T) store.Value {
	t.Helper()
	var value store.Value
	if err := json.Unmarshal([]byte(`{"version":1,"root":{"type":"root","version":1,"format":"","indent":0,"direction":null,"children":[{"type":"block","version":1,"fields":{"blockType":"hero","secret":"orchid","vault":{"value":"orchid"}}}]}}`), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func denied(t *testing.T, err error, path string) {
	t.Helper()
	var failure *operation.Error
	if !errors.As(err, &failure) || failure.Status != http.StatusForbidden || failure.Code != "field_access_denied" || len(failure.Issues) != 1 || failure.Issues[0].Path != path {
		t.Fatalf("query %s error = %#v, want field_access_denied (403) with field path", path, err)
	}
}

func request(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	req := httptest.NewRequest(method, "http://ridu.test"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, req)
	return response
}

func deniedHTTP(t *testing.T, response *httptest.ResponseRecorder, path string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code   string         `json:"code"`
			Issues []schema.Issue `json:"issues"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusForbidden || envelope.Error.Code != "access_denied" || len(envelope.Error.Issues) != 1 || envelope.Error.Issues[0].Path != path {
		t.Fatalf("query %s response = %d %s", path, response.Code, response.Body.String())
	}
}
