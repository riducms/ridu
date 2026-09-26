package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestAdminInitialCustomLoaderSelection(t *testing.T) {
	assets := adminInitialAssets(t)
	var metadata adminBootstrapMetadata
	if err := json.Unmarshal(assets["ridu-admin-bootstrap.json"].Data, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.ExtensionRoutes = []string{"report", "reports/latest"}
	metadata.RouteLoaders = map[string]string{"report": "report", "reports/latest": "report"}
	metadata.ReplacedCoreViews = []string{"collectionCreate:posts", "collectionEdit:posts", "collectionList:*", "collectionList:pages", "collectionList:posts", "global:settings", "notFound:*"}
	metadata.ViewLoaders = map[string]string{"collectionCreate:posts": "create", "collectionEdit:posts": "edit", "collectionList:*": "wildcard", "collectionList:posts": "specific", "global:settings": "global", "notFound:*": "missing"}
	metadata.BuildID = adminBootstrapBuildID(metadata)
	assets["ridu-admin-bootstrap.json"].Data, _ = json.Marshal(metadata)
	snapshot := schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "Views"}, Collections: []schema.Collection{{Slug: "posts"}, {Slug: "pages"}, {Slug: "articles"}}, Globals: []schema.Global{{Slug: "settings"}}}
	for _, key := range []string{"report", "specific", "create", "edit", "global", "wildcard", "missing"} {
		snapshot.Application.AdminLoaders = append(snapshot.Application.AdminLoaders, schema.AdminLoader{Key: key})
	}
	var collections []operationengine.Collection
	for _, collection := range snapshot.Collections {
		collections = append(collections, operationengine.Collection{Key: string(collection.Slug), Schema: collection})
	}
	collections = append(collections, operationengine.Collection{Key: "global:settings", Schema: schema.Collection{Slug: "settings"}})
	backend := teststore.New()
	tx, err := backend.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Create(t.Context(), store.CreateRequest{Collection: schema.Collection{Slug: "settings"}, ID: "settings"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	engine, err := operationengine.New(operationengine.Config{Store: backend, Collections: collections})
	if err != nil {
		t.Fatal(err)
	}
	var calls []AdminLoaderRequest
	handler := New(Config{Engine: engine, Manifest: schema.NewManifest(snapshot), AdminAssets: assets, AdminLoad: func(_ context.Context, request AdminLoaderRequest) (json.RawMessage, error) {
		calls = append(calls, request)
		return json.RawMessage(`{"complete":true}`), nil
	}})
	for _, tc := range []struct{ path, key string }{
		{"/report", "report"}, {"/REPORT/", "report"}, {"/r%65port", "report"}, {"/report/unknown", "missing"},
		{"/reports/latest", "report"}, {"/reports%2Flatest", "missing"},
		{"/collections/posts", "specific"}, {"/collections/articles", "wildcard"}, {"/collections/pages", ""},
		{"/COLLECTIONS/posts", "specific"}, {"/collections/POSTS", ""},
		{"/collections/posts/create", "create"}, {"/collections/posts/create/api", "create"},
		{"/collections/posts/CREATE", "create"}, {"/collections/posts/CREATE/API", "create"},
		{"/collections/posts/a%2Fb", "edit"}, {"/collections/posts/a%2Fb/api", "edit"},
		{"/COLLECTIONS/posts/a%2Fb/API", "edit"},
		{"/collections/posts/trash", ""}, {"/collections/posts/upload", ""}, {"/collections/posts/id/versions/2", ""},
		{"/collections/posts/TRASH", ""}, {"/collections/posts/UPLOAD", ""}, {"/collections/posts/id/VERSIONS/2", ""},
		{"/globals/settings", "global"}, {"/globals/settings/api", "global"}, {"/globals/settings/versions", ""},
		{"/GLOBALS/settings/API", "global"}, {"/globals/settings/VERSIONS", ""},
		{"/missing", "missing"}, {"/collections/inaccessible", ""}, {"/account", ""},
		{"/COLLECTIONS/inaccessible", ""}, {"/ACCOUNT/SECURITY", ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, navigation := range []bool{false, true} {
				calls = nil
				request := httptest.NewRequest(http.MethodGet, "/admin"+tc.path+"?q=Alpha&locale=fr", nil)
				if navigation {
					request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				var state protocol.AdminPreparedRouteStateV1
				if navigation {
					if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
						t.Fatal(err)
					}
				} else {
					state = decodeAdminInitialTemplate(t, response.Body.String())
				}
				if tc.key == "" {
					if len(calls) != 0 {
						t.Fatalf("unselected reads: %#v", calls)
					}
					continue
				}
				if len(calls) != 1 || calls[0].Key != tc.key || calls[0].Query.Get("q") != "Alpha" || state.Outcome != "prepared" || state.Route.Kind != "custom" || state.Route.Data != nil || state.Route.Document != nil {
					t.Fatalf("calls=%#v state=%#v", calls, state)
				}
				if !reflect.DeepEqual(state.ModuleGroups, []string{"entry"}) {
					t.Fatalf("speculative core chunks: %v", state.ModuleGroups)
				}
			}
		})
	}
	// Refresh and fallback use the exact same URL selectors without a sidecar dependency.
	delete(assets, "ridu-admin-bootstrap.json")
	calls = nil
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/loaders/specific?"+url.Values{"route": {"/collections/posts/a%2Fb/api?q=Alpha&locale=fr"}}.Encode(), nil))
	if response.Code != 200 || len(calls) != 1 || calls[0].Pathname != "/collections/posts/a%2Fb/api" || calls[0].RouteParams["document"] != "a/b" || calls[0].RouteParams["collection"] != "posts" || calls[0].Query.Get("q") != "Alpha" {
		t.Fatalf("refresh=%d body=%s calls=%#v", response.Code, response.Body, calls)
	}
	for _, route := range []string{"", "relative", "//evil.example/path", "https://evil.example/path", "/report#fragment", `/\evil`} {
		before := len(calls)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/loaders/report?"+url.Values{"route": {route}}.Encode(), nil))
		if response.Code != 400 || len(calls) != before {
			t.Fatalf("accepted %q", route)
		}
	}
}
