package graphql_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"golang.org/x/crypto/bcrypt"
)

func TestGraphQLWholeDataVariableNullDoesNotApplyDefault(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "GraphQL whole data null", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()},
			Access: ridu.CollectionAccess{ReadDrafts: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }}}},
		Globals: []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Text("siteName")}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	create := `mutation($data: PostCreateInput = {title: "Default create"}) { createPost(data: $data) { id title _revision } }`
	createdResult := graphQL(t, server.URL, create)
	if createdResult["errors"] != nil {
		t.Fatalf("omitted create variable did not use default: %#v", createdResult)
	}
	created := objectAt(t, createdResult, "data", "createPost")
	if created["title"] != "Default create" {
		t.Fatalf("create default = %#v", createdResult)
	}
	failedCreate := graphQL(t, server.URL, create, map[string]interface{}{"data": nil})
	assertErrorCode(t, failedCreate, "bad_query")
	count := graphQL(t, server.URL, `query { countPosts(draft: true) { totalDocs } }`)
	if count["errors"] != nil || objectAt(t, count, "data", "countPosts")["totalDocs"] != float64(1) {
		t.Fatalf("explicit-null create changed row count: %#v", count)
	}

	update := `mutation($id: ID!, $data: PostUpdateInput = {title: "Default update"}) { updatePost(id: $id, data: $data) { title _revision } }`
	updatedResult := graphQL(t, server.URL, update, map[string]interface{}{"id": created["id"]})
	if updatedResult["errors"] != nil {
		t.Fatalf("omitted update variable did not use default: %#v", updatedResult)
	}
	updated := objectAt(t, updatedResult, "data", "updatePost")
	if updated["title"] != "Default update" {
		t.Fatalf("update default = %#v", updatedResult)
	}
	failedUpdate := graphQL(t, server.URL, update, map[string]interface{}{"id": created["id"], "data": nil})
	assertErrorCode(t, failedUpdate, "bad_query")
	post, err := application.Local().Find(t.Context(), "posts", created["id"].(string), ridu.FindOptions{Draft: boolPointer(true)})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := post.Values["title"].StringValue(); title != "Default update" || post.Revision != int(updated["_revision"].(float64)) {
		t.Fatalf("explicit-null update changed working head: %#v", post)
	}

	global := `mutation($data: SettingsUpdateInput = {siteName: "Default site"}) { updateSettings(data: $data) { siteName _revision } }`
	globalResult := graphQL(t, server.URL, global)
	if globalResult["errors"] != nil {
		t.Fatalf("omitted global variable did not use default: %#v", globalResult)
	}
	settings := objectAt(t, globalResult, "data", "updateSettings")
	if settings["siteName"] != "Default site" {
		t.Fatalf("global default = %#v", globalResult)
	}
	beforeGlobal, err := application.Local().Global(t.Context(), "settings", ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	failedGlobal := graphQL(t, server.URL, global, map[string]interface{}{"data": nil})
	assertErrorCode(t, failedGlobal, "bad_query")
	afterGlobal, err := application.Local().Global(t.Context(), "settings", ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if afterGlobal.Revision != beforeGlobal.Revision {
		t.Fatalf("explicit-null global update changed revision: before=%d after=%d", beforeGlobal.Revision, afterGlobal.Revision)
	}
	globalRead := graphQL(t, server.URL, `query { Settings { siteName _revision } }`)
	read := objectAt(t, globalRead, "data", "Settings")
	if read["siteName"] != "Default site" || read["_revision"] != settings["_revision"] {
		t.Fatalf("explicit-null global update changed singleton: %#v", globalRead)
	}
}

func TestGraphQLDraftVariableNullDoesNotApplyDefault(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "GraphQL draft variable null", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()},
			Access: ridu.CollectionAccess{ReadDrafts: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	create := `mutation($draft: Boolean = false) { createPost(draft: $draft, data: {title: "Live"}) { id _status _revision } }`
	liveResult := graphQL(t, server.URL, create)
	live := objectAt(t, liveResult, "data", "createPost")
	if live["_status"] != "published" {
		t.Fatalf("omitted draft variable ignored declared default: %#v", liveResult)
	}
	stagedCreate := graphQL(t, server.URL, create, map[string]interface{}{"draft": nil})
	newDraft := objectAt(t, stagedCreate, "data", "createPost")
	if newDraft["_status"] != "draft" {
		t.Fatalf("explicit-null draft variable published a new document: %#v", stagedCreate)
	}
	stage := graphQL(t, server.URL, `mutation($id: ID!) { updatePost(id: $id, draft: true, data: {title: "Pending"}) { title _revision } }`, map[string]interface{}{"id": live["id"]})
	working := objectAt(t, stage, "data", "updatePost")
	read := `query($id: ID!, $draft: Boolean = false) { Post(id: $id, draft: $draft) { title _revision } }`
	liveRead := graphQL(t, server.URL, read, map[string]interface{}{"id": live["id"]})
	if objectAt(t, liveRead, "data", "Post")["title"] != "Live" {
		t.Fatalf("omitted draft read did not use false default: %#v", liveRead)
	}
	workingRead := graphQL(t, server.URL, read, map[string]interface{}{"id": live["id"], "draft": nil})
	if objectAt(t, workingRead, "data", "Post")["title"] != "Pending" {
		t.Fatalf("explicit-null draft read selected live head: %#v", workingRead)
	}
	update := `mutation($id: ID!, $draft: Boolean = true) { updatePost(id: $id, draft: $draft, data: {title: "Unintended"}) { title _revision } }`
	rejected := graphQL(t, server.URL, update, map[string]interface{}{"id": live["id"], "draft": nil})
	assertErrorCode(t, rejected, "publish_required")
	post, err := application.Local().Find(t.Context(), "posts", live["id"].(string), ridu.FindOptions{Draft: boolPointer(true)})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := post.Values["title"].StringValue(); title != "Pending" || post.Revision != int(working["_revision"].(float64)) {
		t.Fatalf("explicit-null draft update staged content: %#v", post)
	}
	defaultedUpdate := graphQL(t, server.URL, update, map[string]interface{}{"id": live["id"]})
	if defaultedUpdate["errors"] != nil || objectAt(t, defaultedUpdate, "data", "updatePost")["title"] != "Unintended" {
		t.Fatalf("omitted draft variable did not stage via its true default: %#v", defaultedUpdate)
	}
	restore := `mutation($id: ID!, $draft: Boolean = true) { restoreVersionPost(id: $id, revision: 1, draft: $draft) { title _revision _publishedRevision _hasDraftChanges } }`
	restoredResult := graphQL(t, server.URL, restore, map[string]interface{}{"id": live["id"], "draft": nil})
	if restoredResult["errors"] != nil {
		t.Fatalf("explicit-null restore failed: %#v", restoredResult)
	}
	restored := objectAt(t, restoredResult, "data", "restoreVersionPost")
	if restored["title"] != "Live" || restored["_publishedRevision"] != restored["_revision"] || restored["_hasDraftChanges"] != false {
		t.Fatalf("explicit-null restore routed to restore-as-draft: %#v", restoredResult)
	}
	publicRead := graphQL(t, server.URL, `query($id: ID!) { Post(id: $id, draft: false) { title _revision } }`, map[string]interface{}{"id": live["id"]})
	if public := objectAt(t, publicRead, "data", "Post"); public["title"] != "Live" || public["_revision"] != restored["_revision"] {
		t.Fatalf("restore did not promote selected version: %#v", publicRead)
	}
}

func TestGraphQLOptionalDuplicateDataDistinguishesDefaultFromNull(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "GraphQL duplicate input default", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title").Required()}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	created := graphQL(t, server.URL, `mutation { createPost(data: {title: "Original"}) { id title } }`)
	if created["errors"] != nil {
		t.Fatal(created)
	}
	source := objectAt(t, created, "data", "createPost")
	before, err := application.Local().Find(t.Context(), "posts", source["id"].(string), ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	duplicate := `mutation($id: ID!, $data: PostUpdateInput = {title: "Default copy"}) { duplicatePost(id: $id, data: $data) { id title } }`
	defaulted := graphQL(t, server.URL, duplicate, map[string]interface{}{"id": source["id"]})
	if defaulted["errors"] != nil {
		t.Fatal(defaulted)
	}
	defaultCopy := objectAt(t, defaulted, "data", "duplicatePost")
	if defaultCopy["title"] != "Default copy" || defaultCopy["id"] == source["id"] {
		t.Fatalf("omitted duplicate data did not use default: %#v", defaulted)
	}
	explicitNull := graphQL(t, server.URL, duplicate, map[string]interface{}{"id": source["id"], "data": nil})
	if explicitNull["errors"] != nil {
		t.Fatal(explicitNull)
	}
	plainCopy := objectAt(t, explicitNull, "data", "duplicatePost")
	if plainCopy["title"] != "Original" || plainCopy["id"] == source["id"] || plainCopy["id"] == defaultCopy["id"] {
		t.Fatalf("explicit-null duplicate data used default instead of original: %#v", explicitNull)
	}
	count := graphQL(t, server.URL, `query { countPosts { totalDocs } }`)
	if count["errors"] != nil || objectAt(t, count, "data", "countPosts")["totalDocs"] != float64(3) {
		t.Fatalf("duplicate count = %#v", count)
	}
	for _, id := range []string{defaultCopy["id"].(string), plainCopy["id"].(string)} {
		copy, err := application.Local().Find(t.Context(), "posts", id, ridu.FindOptions{})
		if err != nil || copy.Revision != before.Revision {
			t.Fatalf("duplicate %q revision = %#v, %v", id, copy, err)
		}
	}
	after, err := application.Local().Find(t.Context(), "posts", before.ID, ridu.FindOptions{})
	if err != nil || after.Revision != before.Revision {
		t.Fatalf("duplicate changed source revision: before=%#v after=%#v error=%v", before, after, err)
	}
}

func TestGraphQLDraftArrayRowNullKeysRemainInvalid(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "GraphQL null row identities", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields: field.Fields{
				field.Text("title").Required(),
				field.Array("rows", field.Fields{field.Text("label"), field.Array("children", field.Fields{field.Text("label")})}),
				field.Group("details", field.Fields{field.Array("rows", field.Fields{field.Text("label")})}),
			},
			Access: ridu.CollectionAccess{ReadDrafts: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	create := `mutation($data: PostCreateInput!) { createPost(draft: true, data: $data) { id _revision } }`
	invalidKey := func(response map[string]interface{}) {
		t.Helper()
		assertErrorCode(t, response, "validation")
		encoded, _ := json.Marshal(response)
		if !strings.Contains(string(encoded), `"invalid_row_key"`) {
			t.Fatalf("explicit-null row key was not rejected: %s", encoded)
		}
	}
	for _, data := range []map[string]interface{}{
		{"title": "Draft", "rows": map[string]interface{}{"_key": nil, "label": "top singleton"}},
		{"title": "Draft", "details": map[string]interface{}{"rows": []interface{}{map[string]interface{}{"_key": nil, "label": "nested group"}}}},
	} {
		invalidKey(graphQL(t, server.URL, create, map[string]interface{}{"data": data}))
	}
	count := graphQL(t, server.URL, `query { countPosts(draft: true) { totalDocs } }`)
	if count["errors"] != nil || objectAt(t, count, "data", "countPosts")["totalDocs"] != float64(0) {
		t.Fatalf("rejected draft creates persisted rows: %#v", count)
	}
	createdResult := graphQL(t, server.URL, create, map[string]interface{}{"data": map[string]interface{}{
		"title": "Draft", "rows": []interface{}{map[string]interface{}{"label": "retained"}},
	}})
	if createdResult["errors"] != nil {
		t.Fatal(createdResult)
	}
	created := objectAt(t, createdResult, "data", "createPost")
	before, err := application.Local().Find(t.Context(), "posts", created["id"].(string), ridu.FindOptions{Draft: boolPointer(true)})
	if err != nil {
		t.Fatal(err)
	}
	row, _ := before.Values["rows"].ListItem(0)
	key, valid := row.Get("_key").StringValue()
	if !valid || key == "" {
		t.Fatalf("valid omitted row key was not initialized: %#v", before.Values)
	}
	update := `mutation($id: ID!, $data: PostUpdateInput!) { updatePost(id: $id, draft: true, data: $data) { id _revision } }`
	for _, data := range []map[string]interface{}{
		{"rows": map[string]interface{}{"_key": nil, "label": "top singleton"}},
		{"rows": []interface{}{map[string]interface{}{"_key": key, "children": map[string]interface{}{"_key": nil, "label": "nested singleton"}}}},
		{"details": map[string]interface{}{"rows": []interface{}{map[string]interface{}{"_key": nil, "label": "nested group"}}}},
	} {
		invalidKey(graphQL(t, server.URL, update, map[string]interface{}{"id": created["id"], "data": data}))
		after, err := application.Local().Find(t.Context(), "posts", before.ID, ridu.FindOptions{Draft: boolPointer(true)})
		if err != nil || after.Revision != before.Revision {
			t.Fatalf("rejected null-key draft update changed revision: before=%#v after=%#v error=%v", before, after, err)
		}
	}
}

func TestGraphQLAuthPasswordVariableNullDoesNotUseDefault(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "GraphQL auth password null", Admin: ridu.AdminConfig{User: "users"}, Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{Slug: "users", Auth: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Fields:     field.Fields{field.Email("email").Required().Unique(), field.Text("displayName").Required()},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	create := `mutation($password: String = "correct horse battery staple", $email: String!) {
  createUser(draft: true, data: {email: $email, password: $password}) { id email _status }
}`
	failed := graphQL(t, server.URL, create, map[string]interface{}{"email": "null-password@example.test", "password": nil})
	assertErrorCode(t, failed, "bad_query")
	users, err := application.Local().List(t.Context(), "users", ridu.ListOptions{System: true, Draft: boolPointer(true)})
	if err != nil || users.Total != 0 {
		t.Fatalf("explicit-null password created an account: page=%#v error=%v", users, err)
	}
	missingLogin := graphQL(t, server.URL, `mutation { loginUser(email: "null-password@example.test", password: "correct horse battery staple") { token } }`)
	if missingLogin["errors"] == nil {
		t.Fatalf("explicit-null password created credentials: %#v", missingLogin)
	}
	created := graphQL(t, server.URL, create, map[string]interface{}{"email": "default-password@example.test"})
	if created["errors"] != nil {
		t.Fatalf("omitted password variable did not use default: %#v", created)
	}
	user := objectAt(t, created, "data", "createUser")
	if user["email"] != "default-password@example.test" || user["_status"] != "draft" {
		t.Fatalf("created draft auth user = %#v", created)
	}
	users, err = application.Local().List(t.Context(), "users", ridu.ListOptions{System: true, Draft: boolPointer(true)})
	if err != nil || users.Total != 1 {
		t.Fatalf("default password account count = %#v, %v", users, err)
	}
	stored, err := application.Local().Find(t.Context(), "users", user["id"].(string), ridu.FindOptions{System: true, Draft: boolPointer(true)})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := stored.Values["password"]; exists {
		t.Fatalf("credential leaked into document values: %#v", stored.Values)
	}
	login := graphQL(t, server.URL, `mutation { loginUser(email: "default-password@example.test", password: "correct horse battery staple") { token } }`)
	if login["errors"] != nil || objectAt(t, login, "data", "loginUser")["token"] == "" {
		t.Fatalf("defaulted credential cannot log in: %#v", login)
	}
}

func boolPointer(value bool) *bool { return &value }
