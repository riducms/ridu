package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
)

func TestAdminLoaderPreparationSelectionSizeAndHead(t *testing.T) {
	assets := adminInitialAssets(t)
	var metadata adminBootstrapMetadata
	if err := json.Unmarshal(assets["ridu-admin-bootstrap.json"].Data, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.DashboardLoaders = []string{"counts"}
	writeMetadata := func() {
		metadata.BuildID = adminBootstrapBuildID(metadata)
		assets["ridu-admin-bootstrap.json"].Data, _ = json.Marshal(metadata)
	}
	writeMetadata()
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New()})
	if err != nil {
		t.Fatal(err)
	}
	manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "Dashboard", AdminLoaders: []schema.AdminLoader{
		{Key: "counts", Input: schema.AdminDataType{Kind: "object"}, Output: schema.AdminDataType{Kind: "string"}},
		{Key: "panic", Input: schema.AdminDataType{Kind: "object"}, Output: schema.AdminDataType{Kind: "string"}},
	}}})
	calls := 0
	output := `"ready"`
	panicKey := ""
	handler := New(Config{Engine: engine, Manifest: manifest, AdminAssets: assets,
		AdminLoad: func(_ context.Context, request AdminLoaderRequest) (json.RawMessage, error) {
			calls++
			if request.Key == panicKey {
				panic("private callback detail")
			}
			return json.RawMessage(output), nil
		},
	})
	read := func(method, path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		return response
	}
	response := read(http.MethodGet, "/admin/")
	state := decodeAdminInitialTemplate(t, response.Body.String())
	if calls != 1 || state.Outcome != "prepared" || state.Loaders["counts"].Value == nil {
		t.Fatalf("calls=%d state=%#v", calls, state)
	}
	for _, path := range []string{"/admin/", "/api/admin/loaders/counts"} {
		before := calls
		response = read(http.MethodHead, path)
		if response.Body.Len() != 0 || calls != before {
			t.Fatal("HEAD did work")
		}
	}
	before := calls
	read(http.MethodGet, "/admin/no-such-page")
	if calls != before {
		t.Fatal("unselected loader executed")
	}
	output = `"` + strings.Repeat("x", maxAdminPreparedRouteBytes) + `"`
	state = decodeAdminInitialTemplate(t, read(http.MethodGet, "/admin/").Body.String())
	if state.Outcome != protocol.AdminPreparedRouteFallback || state.Loaders != nil || state.Route != nil {
		t.Fatal("oversized loader leaked into fallback")
	}
	metadata.ReplacedCoreViews = []string{"dashboard:*"}
	writeMetadata()
	before = calls
	state = decodeAdminInitialTemplate(t, read(http.MethodGet, "/admin/").Body.String())
	if state.Outcome != "fallback" || calls != before {
		t.Fatal("replacement triggered a default loader")
	}
	metadata.ReplacedCoreViews = []string{}
	metadata.DashboardLoaders = []string{"counts", "counts"}
	writeMetadata()
	before = calls
	read(http.MethodGet, "/admin/")
	if calls != before {
		t.Fatal("duplicate metadata triggered loader reads")
	}
	metadata.DashboardLoaders = []string{"counts", "panic"}
	writeMetadata()
	panicKey, output = "panic", `"partial-secret"`
	before = calls
	response = read(http.MethodGet, "/admin/")
	state = decodeAdminInitialTemplate(t, response.Body.String())
	if calls != before+2 || state.Outcome != protocol.AdminPreparedRouteFallback || state.Loaders != nil || state.Route != nil || strings.Contains(response.Body.String(), "partial-secret") || strings.Contains(response.Body.String(), "private callback detail") {
		t.Fatalf("streamed panic retained partial output: calls=%d state=%#v", calls-before, state)
	}
}

func TestAdminLoaderRequiresAdminAccess(t *testing.T) {
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New()})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	manifest := schema.NewManifest(schema.Snapshot{Application: schema.Application{Name: "Private", Admin: &schema.AdminSettings{UserCollectionSlug: "users"}, AdminLoaders: []schema.AdminLoader{{Key: "secret"}}}})
	handler := New(Config{Engine: engine, Manifest: manifest, AdminLoad: func(context.Context, AdminLoaderRequest) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`"secret"`), nil
	}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/loaders/secret?route=%2F", nil))
	if calls != 0 || response.Code != http.StatusForbidden {
		t.Fatalf("calls=%d status=%d", calls, response.Code)
	}
}
