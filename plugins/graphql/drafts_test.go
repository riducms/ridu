package graphql_test

import (
	"net/http/httptest"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"golang.org/x/crypto/bcrypt"
)

func TestGraphQLAuthDraftCreateRequiresIdentityNotEditorialCompleteness(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL auth drafts", Admin: ridu.AdminConfig{User: "users"}, Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Access: ridu.CollectionAccess{Create: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Allow(), nil
			}},
			Fields: field.Fields{
				field.Email("email").Required().Unique(),
				field.Text("displayName").Required(),
				field.Group("profile", field.Fields{field.Text("email").Required()}),
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	created := graphQL(t, server.URL, `mutation {
  createUser(draft: true, data: {email: "draft@example.test", password: "correct horse battery staple", profile: {}}) {
    id email displayName _status
  }
}`)
	user := objectAt(t, created, "data", "createUser")
	if user["email"] != "draft@example.test" || user["displayName"] != nil || user["_status"] != "draft" {
		t.Fatalf("incomplete auth draft = %#v", created)
	}
	for _, mutation := range []string{
		`mutation { createUser(draft: true, data: {password: "correct horse battery staple"}) { id } }`,
		`mutation { createUser(draft: true, data: {email: "missing-password@example.test"}) { id } }`,
	} {
		result := graphQL(t, server.URL, mutation)
		if errors, ok := result["errors"].([]any); !ok || len(errors) == 0 {
			t.Fatalf("auth draft accepted missing credentials: %#v", result)
		}
	}
	strict := graphQL(t, server.URL, `mutation {
  createUser(draft: false, data: {email: "live@example.test", password: "correct horse battery staple"}) { id }
}`)
	assertErrorCode(t, strict, "validation")
}

func TestGraphQLWorkingDraftsPreserveLiveQueriesAndDiscard(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL working drafts", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{
			Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields: field.Fields{field.Text("title").Required().MinLength(3)},
			Access: ridu.CollectionAccess{ReadDrafts: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Allow(), nil
			}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	unfinished := graphQL(t, server.URL, `mutation { createPost(data: {}) { id title _status } }`)
	unfinishedPost := objectAt(t, unfinished, "data", "createPost")
	if unfinishedPost["title"] != nil || unfinishedPost["_status"] != "draft" {
		t.Fatalf("incomplete draft = %#v", unfinished)
	}
	failedPublish := graphQL(t, server.URL, `mutation($id: ID!) { publishPost(id: $id) { id } }`, map[string]interface{}{"id": unfinishedPost["id"]})
	assertErrorCode(t, failedPublish, "validation")

	created := graphQL(t, server.URL, `mutation { createPost(draft: false, data: {title: "Live"}) { id _revision } }`)
	live := objectAt(t, created, "data", "createPost")
	variables := map[string]interface{}{"id": live["id"]}
	staged := graphQL(t, server.URL, `mutation($id: ID!, $data: PostUpdateInput!) { updatePost(id: $id, draft: true, data: $data) { title _revision _publishedRevision _hasDraftChanges } }`, map[string]interface{}{"id": live["id"], "data": map[string]interface{}{"title": nil}})
	working := objectAt(t, staged, "data", "updatePost")
	if working["title"] != nil || working["_hasDraftChanges"] != true || working["_publishedRevision"] != live["_revision"] {
		t.Fatalf("working draft = %#v", staged)
	}
	liveRead := graphQL(t, server.URL, `query($id: ID!) {
  Post(id: $id, draft: false) { title _revision _publishedRevision _hasDraftChanges }
  Posts(draft: false, where: {title: {equals: "Live"}}) { totalDocs docs { title } }
  countPosts(draft: false, where: {title: {equals: "Live"}}) { totalDocs }
}`, variables)
	public := objectAt(t, liveRead, "data", "Post")
	if public["title"] != "Live" || public["_revision"] != live["_revision"] || public["_publishedRevision"] != nil || public["_hasDraftChanges"] != nil {
		t.Fatalf("live read = %#v", liveRead)
	}
	if objectAt(t, liveRead, "data", "Posts")["totalDocs"] != float64(1) || objectAt(t, liveRead, "data", "countPosts")["totalDocs"] != float64(1) {
		t.Fatalf("live filter/count used pending values: %#v", liveRead)
	}
	variables["revision"] = live["_revision"]
	stale := graphQL(t, server.URL, `mutation($id: ID!, $revision: Int!) { discardDraftPost(id: $id, expectedRevision: $revision) { title } }`, variables)
	assertErrorCode(t, stale, "conflict")
	variables["revision"] = working["_revision"]
	discarded := graphQL(t, server.URL, `mutation($id: ID!, $revision: Int!) { discardDraftPost(id: $id, expectedRevision: $revision) { title _hasDraftChanges } }`, variables)
	reset := objectAt(t, discarded, "data", "discardDraftPost")
	if reset["title"] != "Live" || reset["_hasDraftChanges"] != false {
		t.Fatalf("discard = %#v", discarded)
	}
}
