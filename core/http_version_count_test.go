package core_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/store"
)

func TestRESTVersionCountRoutesReturnAuthorizedCountsWithoutSnapshots(t *testing.T) {
	denyVersions := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Deny(), nil
	}
	application, err := ridu.New(ridu.Config{
		Name: "Version count transport",
		Collections: []ridu.Collection{
			{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}},
			{Slug: "private-posts", Versions: true, Fields: field.Fields{field.Text("title").Required()}, Access: ridu.CollectionAccess{ReadVersions: denyVersions}},
		},
		Globals: []ridu.Global{
			{Slug: "settings", Versions: true, Fields: field.Fields{field.Text("title").Required()}},
			{Slug: "private-settings", Versions: true, Fields: field.Fields{field.Text("title").Required()}, Access: ridu.GlobalAccess{ReadVersions: denyVersions}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Draft")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Publish(t.Context(), "posts", post.ID, ridu.MutationOptions{ExpectedRevision: post.Revision}); err != nil {
		t.Fatal(err)
	}
	privatePost, err := application.Local().Create(t.Context(), "private-posts", store.Values{"title": store.String("Private")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"settings", "private-settings"} {
		if _, err := application.Local().UpdateGlobal(t.Context(), slug, store.Values{"title": store.String("Configured")}, ridu.MutationOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	for _, test := range []struct {
		name   string
		path   string
		status int
		count  int
		code   protocol.ErrorCode
	}{
		{"collection", "/api/collections/posts/" + post.ID + "/versions/count", http.StatusOK, 2, ""},
		{"global", "/api/globals/settings/versions/count", http.StatusOK, 1, ""},
		{"collection denied", "/api/collections/private-posts/" + privatePost.ID + "/versions/count", http.StatusForbidden, 0, protocol.ErrorAccess},
		{"global denied", "/api/globals/private-settings/versions/count", http.StatusForbidden, 0, protocol.ErrorAccess},
		{"collection bad locale", "/api/collections/posts/" + post.ID + "/versions/count?locale=missing", http.StatusBadRequest, 0, protocol.ErrorBadRequest},
		{"global bad locale", "/api/globals/settings/versions/count?locale=missing", http.StatusBadRequest, 0, protocol.ErrorBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := requestJSON(t, client, http.MethodGet, "http://ridu.test"+test.path, nil, "")
			if response.StatusCode != test.status {
				t.Fatalf("status = %d, want %d: %s", response.StatusCode, test.status, readBody(t, response))
			}
			if test.status != http.StatusOK {
				var failure protocol.ErrorEnvelope
				decodeResponse(t, response, &failure)
				if failure.Error.Code != test.code {
					t.Fatalf("error code = %q, want %q", failure.Error.Code, test.code)
				}
				return
			}
			var body map[string]json.RawMessage
			decodeResponse(t, response, &body)
			if len(body) != 1 {
				t.Fatalf("count response includes more than totalDocs: %#v", body)
			}
			var count int
			if err := json.Unmarshal(body["totalDocs"], &count); err != nil || count != test.count {
				t.Fatalf("totalDocs = %d, %v; want %d", count, err, test.count)
			}
		})
	}
	for _, path := range []string{
		"/api/collections/posts/" + post.ID + "/versions/0",
		"/api/globals/settings/versions/0",
	} {
		response := requestJSON(t, client, http.MethodGet, "http://ridu.test"+path, nil, "")
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(readBody(t, response), "positive integer") {
			t.Fatalf("invalid revision %q did not preserve its error", path)
		}
	}
}
