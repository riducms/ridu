package content

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/store"
)

var updateAdminClient = flag.Bool("update-admin-client", false, "regenerate the admin loader documentation client")

func TestAdminDataGeneratedClient(t *testing.T) {
	manifest, err := ridu.Resolve(Config())
	if err != nil {
		t.Fatal(err)
	}
	want, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "generated", "ridu.generated.ts")
	if *updateAdminClient {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("admin loader client is stale; run go test ./examples/documentation/custom-components/content -run TestAdminDataGeneratedClient -update-admin-client")
	}
}

func TestPostSummaryCountsAndFilters(t *testing.T) {
	config := Config()
	config.Localization = ridu.LocalizationConfig{
		DefaultLocale: "en",
		Locales:       []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}},
	}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	user, err := app.Local().Create(ctx, "users", store.Values{
		"email": store.String("editor@example.test"),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SetPassword(ctx, "users", user.ID, "loader-example-password"); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Welcome to the site", "Editorial checklist"} {
		if _, err := app.Local().Create(ctx, "posts", store.Values{"title": store.String(title)}, ridu.MutationOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	handler := app.Handler(ridu.HandlerOptions{})
	login := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/users/login", strings.NewReader(`{"email":"editor@example.test","password":"loader-example-password"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(login, request)
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	for _, test := range []struct {
		search string
		total  int
		locale string
	}{{"", 2, "en"}, {"Welcome", 1, "en"}, {"Missing", 0, "en"}, {"Welcome", 1, "fr"}} {
		route := "/post-report?" + url.Values{"q": {test.search}, "locale": {test.locale}}.Encode()
		request := httptest.NewRequest(http.MethodGet, "/api/admin/loaders/post-summary?route="+url.QueryEscape(route), nil)
		for _, cookie := range login.Result().Cookies() {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var data PostSummaryData
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &data) != nil {
			t.Fatalf("summary: %d %s", response.Code, response.Body.String())
		}
		if data.Total != test.total || data.Search != test.search || data.Locale != test.locale {
			t.Fatalf("search %q: %#v", test.search, data)
		}
	}
}
