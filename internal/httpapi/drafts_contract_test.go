package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestDraftHTTPReadsSelectTheHeadBeforeQuerying(t *testing.T) {
	app, err := core.New(core.Config{Name: "HTTP editorial contract", Collections: []core.Collection{{
		Slug: "posts", Versions: true, VersionConfig: core.VersionConfig{Drafts: true},
		Access: core.CollectionAccess{ReadDrafts: func(core.AccessContext) (core.AccessDecision, error) { return core.Allow(), nil }},
		Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	published, draft := false, true
	live, err := app.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Live title")}, core.MutationOptions{Draft: &published})
	if err != nil {
		t.Fatal(err)
	}
	working, err := app.Local().Update(t.Context(), "posts", live.ID, store.Values{"title": store.String("Pending title")}, core.MutationOptions{Draft: &draft, ExpectedRevision: live.Revision})
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Handler(core.HandlerOptions{})
	request := func(method, target, body string, revision int) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if revision > 0 {
			req.Header.Set("If-Match", fmt.Sprint(revision))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		var envelope map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode HTTP response: %v (%s)", err, response.Body.String())
		}
		return response.Code, envelope
	}
	base := "/api/collections/posts/" + live.ID
	for _, test := range []struct {
		selection, title string
		revision         int
		pending          bool
	}{
		{"false", "Live title", live.Revision, false},
		{"true", "Pending title", working.Revision, true},
	} {
		status, envelope := request(http.MethodGet, base+"?draft="+test.selection, "", 0)
		if status != http.StatusOK {
			t.Fatalf("read %s = %d: %#v", test.selection, status, envelope)
		}
		document := envelope["doc"].(map[string]any)
		if document["title"] != test.title || document["_revision"] != float64(test.revision) {
			t.Fatalf("read %s selected wrong snapshot: %#v", test.selection, document)
		}
		if _, present := document["_hasDraftChanges"]; present != test.pending {
			t.Fatalf("public draft internals: %#v", document)
		}
	}
	for _, test := range []struct {
		selection, title string
		count            int
	}{
		{"false", "Live title", 1}, {"false", "Pending title", 0},
		{"true", "Live title", 0}, {"true", "Pending title", 1},
	} {
		query := url.Values{"draft": {test.selection}, "where": {fmt.Sprintf(`{"title":{"equals":%q}}`, test.title)}}
		status, envelope := request(http.MethodGet, "/api/collections/posts?"+query.Encode(), "", 0)
		if status != http.StatusOK || len(envelope["docs"].([]any)) != test.count {
			t.Fatalf("list %v = %d: %#v", test, status, envelope)
		}
		status, envelope = request(http.MethodGet, "/api/collections/posts/count?"+query.Encode(), "", 0)
		if status != http.StatusOK || envelope["totalDocs"] != float64(test.count) {
			t.Fatalf("count %v = %d: %#v", test, status, envelope)
		}
	}
	if status, envelope := request(http.MethodPost, base+"/discard-draft", "", live.Revision); status != http.StatusConflict {
		t.Fatalf("stale discard = %d: %#v", status, envelope)
	}
	status, envelope := request(http.MethodPost, base+"/discard-draft", "", working.Revision)
	if status != http.StatusOK || envelope["doc"].(map[string]any)["title"] != "Live title" {
		t.Fatalf("discard = %d: %#v", status, envelope)
	}
}

func TestDraftHTTPSelectionNeverGrantsAnonymousSystemAccess(t *testing.T) {
	app, err := core.New(core.Config{Name: "Private drafts", Collections: []core.Collection{{Slug: "posts", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title")}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := app.Local().Create(t.Context(), "posts", store.Values{}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"/api/collections/posts?draft=true", "/api/collections/posts/count?draft=true",
		"/api/collections/posts/" + document.ID + "?draft=true",
	} {
		response := httptest.NewRecorder()
		app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusForbidden {
			t.Fatalf("anonymous draft read %s = %d: %s", target, response.Code, response.Body.String())
		}
	}
}

func TestVersionHistoryWithoutDraftsDoesNotExposeWorkingHeadMetadata(t *testing.T) {
	app, err := core.New(core.Config{Name: "Revision history only", Collections: []core.Collection{{
		Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := app.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Published")}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, selection := range []string{"", "?draft=false"} {
		response := httptest.NewRecorder()
		app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/collections/posts/"+document.ID+selection, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("read history-only document = %d: %s", response.Code, response.Body.String())
		}
		var envelope struct {
			Doc map[string]any `json:"doc"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Doc["_revision"] != float64(document.Revision) {
			t.Fatalf("history revision missing: %#v", envelope.Doc)
		}
		for _, name := range []string{"_publishedRevision", "_hasDraftChanges"} {
			if _, exists := envelope.Doc[name]; exists {
				t.Fatalf("history-only resource exposed %s: %#v", name, envelope.Doc)
			}
		}
	}
}
