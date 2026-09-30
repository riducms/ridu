package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestAdminInitialListReadPartsPreserveAccessAndIndependentLifecycles(t *testing.T) {
	field := func(name string, kind schema.FieldType) schema.Field {
		path, _ := query.NewPath(name)
		return schema.Field{ID: schema.StableID(name), Name: name, Path: path, Type: kind, Category: schema.FieldCategoryScalar}
	}
	title, secret, status := field("title", schema.FieldTypeText), field("secret", schema.FieldTypeText), field("status", schema.FieldTypeSelect)
	status.Select = &schema.SelectField{Options: []schema.SelectOption{{Value: "draft"}, {Value: "published"}}}
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{title, secret, status}, Admin: schema.CollectionAdmin{UseAsTitle: "title"}}
	reads, sessions, preferences, audits := 0, 0, 0, 0
	fail := false
	predicate := query.Equal(title.Path, "visible").Node()
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Collections: []operationengine.Collection{{
		Schema: collection,
		Access: map[operation.Kind]operationengine.Access{operation.Read: func(operationengine.Context) (operationengine.Decision, error) {
			return operationengine.Decision{Kind: operationengine.Where, Access: &predicate}, nil
		}},
		Bindings: []operationengine.FieldBinding{{ID: string(secret.ID), Field: secret, Access: operationengine.FieldRules{Read: func(operationengine.Context) (bool, error) { return false, nil }}}},
		Hooks: operationengine.Hooks{BeforeOperation: []operationengine.Hook{func(ctx operationengine.Context) error {
			if ctx.Operation == operation.Read {
				reads++
			}
			if fail {
				return errors.New("private database detail")
			}
			return nil
		}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, titleValue := range []string{"visible", "hidden"} {
		if _, err := engine.Execute(t.Context(), operationengine.Request{Operation: operation.Create, Collection: "posts", ImportID: titleValue, Data: store.Values{"title": store.String(titleValue), "secret": store.String("private"), "status": store.String("draft")}}); err != nil {
			t.Fatal(err)
		}
	}
	handler := New(Config{Engine: engine, Manifest: schema.NewManifest(schema.Snapshot{Collections: []schema.Collection{collection}}),
		Session: func(context.Context, string) (AuthSession, error) {
			sessions++
			return AuthSession{Collection: "users", User: store.Document{ID: "actor"}}, nil
		},
		GetPreference: func(context.Context, *AuthIdentity, string) (json.RawMessage, error) { preferences++; return nil, nil },
		Audit:         func(AuditEvent) { audits++ },
	})
	for _, tc := range []struct {
		part         string
		reads        int
		page, counts bool
	}{{"page", 1, true, false}, {"counts", 3, false, true}} {
		t.Run(tc.part, func(t *testing.T) {
			reads, sessions = 0, 0
			request := httptest.NewRequest(http.MethodGet, "/api/admin/collection-list/posts?part="+tc.part+"&q=visible&status=draft&limit=10", nil)
			request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "credential-never-serialized"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatal(response.Body.String())
			}
			var data protocol.AdminCollectionListDataV1
			if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if reads != tc.reads || sessions != 1 || preferences != 0 || audits != 0 {
				t.Fatalf("reads/sessions/preferences/audits: %d/%d/%d/%d", reads, sessions, preferences, audits)
			}
			if (data.Page != nil) != tc.page || (len(data.Counts) != 0) != tc.counts || data.Preferences != nil {
				t.Fatalf("wrong parts: %#v", data)
			}
			if tc.page && (data.Page.Value == nil || len(data.Page.Value.Docs) != 1 || decodedDocument(t, data.Page.Value.Docs[0])["id"] != "visible" || decodedDocument(t, data.Page.Value.Docs[0])["secret"] != nil || data.Page.Value.Pagination.Limit != 10) {
				t.Fatalf("page: %#v", data.Page)
			}
			if tc.counts && (*data.Counts[""].Value != 1 || *data.Counts["draft"].Value != 1 || *data.Counts["published"].Value != 0) {
				t.Fatalf("counts: %#v", data.Counts)
			}
			if string(data.Query.Where) != `{"and":[{"title":{"like":"visible"}},{"status":{"equals":"draft"}}]}` {
				t.Fatalf("selection where: %s", data.Query.Where)
			}
			if string(data.Query.CountWhere) != `{"title":{"like":"visible"}}` {
				t.Fatalf("count filter must not include active status: %s", data.Query.CountWhere)
			}
			if response.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(strings.Join(response.Header().Values("Vary"), ","), "Cookie") {
				t.Fatal(response.Header())
			}
			if strings.Contains(response.Body.String(), "credential-never-serialized") {
				t.Fatal("leaked credential")
			}
		})
	}
	reads, sessions = 0, 0
	request := httptest.NewRequest(http.MethodHead, "/api/admin/collection-list/posts?part=page", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Body.Len() != 0 || reads != 0 || sessions != 0 {
		t.Fatal("HEAD performed work")
	}
	fail = true
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/collection-list/posts?part=page", nil))
	var failed protocol.AdminCollectionListDataV1
	if err := json.Unmarshal(response.Body.Bytes(), &failed); err != nil {
		t.Fatal(err)
	}
	if failed.Page.Error == nil || failed.Page.Error.Code != protocol.ErrorInternal || strings.Contains(response.Body.String(), "private database detail") {
		t.Fatalf("error: %s", response.Body.String())
	}
	if failed.Page.Error.RequestID != response.Header().Get("X-Request-ID") {
		t.Fatal("part error lost HTTP request identity")
	}
}

func TestAdminInitialListReadsUseBootstrapCredentialPrecedence(t *testing.T) {
	var tokens, actors []string
	collection := schema.Collection{ID: "posts", Slug: "posts"}
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Collections: []operationengine.Collection{{Schema: collection, Hooks: operationengine.Hooks{BeforeOperation: []operationengine.Hook{func(ctx operationengine.Context) error {
		actors = append(actors, ctx.Actor.ID)
		return nil
	}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Config{AdminAssets: adminInitialAssets(t), Engine: engine, Manifest: schema.NewManifest(schema.Snapshot{Collections: []schema.Collection{collection}}), Session: func(_ context.Context, token string) (AuthSession, error) {
		tokens = append(tokens, token)
		return AuthSession{Collection: "users", User: store.Document{ID: token}}, nil
	}})
	for _, path := range []string{"/admin/collections/posts", "/api/admin/collection-list/posts?part=page"} {
		tokens, actors = nil, nil
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
		request.Header.Set("Authorization", "Session explicit")
		request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "cookie"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 200 || len(tokens) != 1 || tokens[0] != "explicit" || len(actors) != 1 || actors[0] != "explicit" {
			t.Fatalf("%s: status=%d, sessions=%v, actors=%v", path, response.Code, tokens, actors)
		}
	}
}

func TestAdminInitialListFallbackReadIsIndependentOfMetadataAndSnapshotLimit(t *testing.T) {
	path, _ := query.NewPath("title")
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{{ID: "title", Name: "title", Path: path, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar}}}
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Collections: []operationengine.Collection{{Schema: collection}}})
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("x", maxAdminPreparedRouteBytes)
	if _, err := engine.Execute(t.Context(), operationengine.Request{Operation: operation.Create, Collection: "posts", ImportID: "large", Data: store.Values{"title": store.String(large)}}); err != nil {
		t.Fatal(err)
	}
	handler := New(Config{Engine: engine, Manifest: schema.NewManifest(schema.Snapshot{Collections: []schema.Collection{collection}})})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/collection-list/posts?part=page", nil))
	var data protocol.AdminCollectionListDataV1
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || data.Page.Value == nil || decodedDocument(t, data.Page.Value.Docs[0])["title"] != large {
		t.Fatal("ordinary fallback read was capped")
	}
	state := protocol.AdminPreparedRouteStateV1{Outcome: protocol.AdminPreparedRoutePrepared, Route: &protocol.AdminPreparedRouteDataV1{Kind: protocol.AdminPreparedRouteCollectionList, Data: &data}}
	var bounded protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(marshalBoundedAdminState(state), &bounded); err != nil {
		t.Fatal(err)
	}
	if bounded.Outcome != protocol.AdminPreparedRouteFallback || bounded.Route != nil || bounded.Diagnostic.Code != "snapshot_too_large" {
		t.Fatalf("snapshot did not fall back: %#v", bounded)
	}
}

func TestAdminInitialListFilterPlanningHandlesPrimitiveMembership(t *testing.T) {
	for _, kind := range []schema.FieldType{schema.FieldTypeTextList, schema.FieldTypeNumberList} {
		path, _ := query.NewPath("values")
		snapshot := schema.Snapshot{Collections: []schema.Collection{{Slug: "posts", Fields: []schema.Field{{Name: "values", Path: path, Type: kind}}}}}
		api := &API{}
		for _, operator := range []string{"in", "equals"} {
			where := api.adminListWhere(snapshot, "posts", url.Values{"filters": {`[[{"field":"values","operator":"` + operator + `","value":"1"}]]`}}, nil)
			if operator == "equals" {
				if where != nil {
					t.Fatal("unsupported scalar operator")
				}
				continue
			}
			encoded, _ := json.Marshal(where)
			if _, err := decodeWhere(encoded, snapshot.Collections[0]); err != nil {
				t.Fatal(err)
			}
			want := `{"values":{"in":[1]}}`
			if kind == schema.FieldTypeTextList {
				want = `{"values":{"in":["1"]}}`
			}
			if string(encoded) != want {
				t.Fatalf("%s: %s", kind, encoded)
			}
		}
	}
}

func TestAdminListGroupsDecodeAndPreserveConjunctions(t *testing.T) {
	path, _ := query.NewPath("title")
	collection := schema.Collection{Slug: "posts", Fields: []schema.Field{{Name: "title", Path: path, Type: schema.FieldTypeText}}}
	snapshot := schema.Snapshot{Collections: []schema.Collection{collection}}
	for _, filters := range []string{
		`[[{"field":"title","operator":"equals","value":"A"}]]`,
		`[[{"field":"title","operator":"like","value":"A"},{"field":"title","operator":"notEquals","value":"B"}],[{"field":"title","operator":"equals","value":"C"}]]`,
	} {
		api := &API{}
		where := api.adminListWhere(snapshot, "posts", url.Values{"q": {"search"}, "filters": {filters}}, nil)
		encoded, _ := json.Marshal(where)
		if _, err := decodeWhere(encoded, collection); err != nil {
			t.Fatalf("%s: %v", encoded, err)
		}
		if !strings.Contains(string(encoded), `"like":"search"`) {
			t.Fatalf("search predicate lost: %s", encoded)
		}
	}
}

func TestAdminListTimestampFilterUsesQueryableSystemMetadata(t *testing.T) {
	collection := schema.Collection{Slug: "posts"}
	api := &API{}
	where := api.adminListWhere(schema.Snapshot{Collections: []schema.Collection{collection}}, "posts", url.Values{
		"filters": {`[[{"field":"createdAt","operator":"greaterThan","value":"2026-01-01T00:00:00+01:00"}]]`},
	}, nil)
	encoded, _ := json.Marshal(where)
	expression, err := decodeWhere(encoded, collection)
	if err != nil {
		t.Fatalf("metadata filter %s: %v", encoded, err)
	}
	value, _ := expression.Node().Comparison.Value.StringValue()
	if value != "2025-12-31T23:00:00Z" {
		t.Fatalf("metadata timestamp = %q", value)
	}
}
