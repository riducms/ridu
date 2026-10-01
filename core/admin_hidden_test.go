package core_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

// Hidden is admin presentation. It reaches the manifest the admin reads, and
// nothing else about the collection or global changes: the Local API and the
// REST API serve it exactly as they serve a visible one.
func TestHiddenResourcesKeepTheirAPIs(t *testing.T) {
	config := ridu.Config{
		Name: "Hidden",
		Collections: []ridu.Collection{
			{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Relationship("stats", "post-stats")}},
			{Slug: "post-stats", Admin: ridu.CollectionAdmin{Hidden: true}, Fields: field.Fields{field.Number("views")}},
		},
		Globals: []ridu.Global{{Slug: "sync-state", Admin: ridu.GlobalAdmin{Hidden: true}, Fields: field.Fields{field.Number("cursor")}}},
	}
	application, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := application.Manifest().Snapshot()
	if snapshot.Collections[0].Admin.Hidden || !snapshot.Collections[1].Admin.Hidden || !snapshot.Globals[0].Admin.Hidden {
		t.Fatalf("hidden flags = %v %v %v", snapshot.Collections[0].Admin.Hidden, snapshot.Collections[1].Admin.Hidden, snapshot.Globals[0].Admin.Hidden)
	}

	ctx := context.Background()
	stats, err := application.Local().Create(ctx, "post-stats", store.Values{"views": store.Number(3)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("create in a hidden collection: %v", err)
	}
	if _, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello"), "stats": store.String(stats.ID)}, ridu.MutationOptions{}); err != nil {
		t.Fatalf("reference a hidden collection: %v", err)
	}
	if _, err := application.Local().UpdateGlobal(ctx, "sync-state", store.Values{"cursor": store.Number(7)}, ridu.MutationOptions{}); err != nil {
		t.Fatalf("update a hidden global: %v", err)
	}

	handler := application.Handler(ridu.HandlerOptions{})
	for _, path := range []string{"/api/collections/post-stats/" + stats.ID, "/api/collections/post-stats", "/api/globals/sync-state"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, response.Code, response.Body.String())
		}
	}
}
