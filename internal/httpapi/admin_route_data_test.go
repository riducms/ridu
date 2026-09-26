package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestAdminInitialCreateAndUploadUseEngineAccessWithoutDocumentReads(t *testing.T) {
	path, _ := query.NewPath("title")
	defaultTitle := "Default title"
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{{
		ID: "title", Name: "title", Path: path, Type: schema.FieldTypeText,
		Category: schema.FieldCategoryScalar, Text: &schema.TextField{}, Default: &defaultTitle,
	}}}
	var inputs []store.Values
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Collections: []operationengine.Collection{{
		Schema: collection,
		Hooks:  operationengine.Hooks{BeforeRead: []operationengine.Hook{func(operationengine.Context) error { t.Fatal("access-only route ran a document read"); return nil }}},
		Access: map[operation.Kind]operationengine.Access{operation.Create: func(ctx operationengine.Context) (operationengine.Decision, error) {
			inputs = append(inputs, ctx.Data)
			return operationengine.Decision{Kind: operationengine.Allow}, nil
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	api := &API{config: Config{Engine: engine, Audit: func(AuditEvent) { t.Fatal("access-only route emitted read audit") }}}
	runtime := &protocol.AdminPreparedRuntimeV1{Manifest: schema.Snapshot{Collections: []schema.Collection{collection}}}
	request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts/create", nil)
	created := api.prepareAdminRouteData(request, runtime, adminPreparedIdentity{}, adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionCreate, segments: []string{"collections", "posts", "create"}})
	if created.Create == nil || created.Create.Values["title"] != defaultTitle || created.Create.Access.Value == nil {
		t.Fatalf("create data = %#v", created)
	}
	if len(inputs) != 1 {
		t.Fatalf("create evaluations = %d", len(inputs))
	}
	if got, _ := inputs[0]["title"].StringValue(); got != defaultTitle {
		t.Fatalf("access data = %#v", inputs[0])
	}
	inputs = nil
	upload := api.prepareAdminRouteData(request, runtime, adminPreparedIdentity{}, adminRouteClassification{kind: protocol.AdminPreparedRouteUpload, segments: []string{"collections", "posts", "upload"}})
	if upload.Access == nil || upload.Access.Value == nil || len(inputs) != 1 || len(inputs[0]) != 0 {
		t.Fatalf("upload = %#v access inputs = %#v", upload, inputs)
	}
}

func TestAdminInitialAccountWithoutSessionFallsBackWithoutReportingPanic(t *testing.T) {
	reported := 0
	api := &API{config: Config{RequestError: func(RequestErrorEvent) { reported++ }}}
	state := protocol.AdminPreparedRouteStateV1{Outcome: protocol.AdminPreparedRoutePrepared}
	api.completeAdminPreparedRoute(&state, httptest.NewRequest(http.MethodGet, "/admin/account", nil), &protocol.AdminPreparedRuntimeV1{}, adminPreparedIdentity{}, adminRouteClassification{kind: protocol.AdminPreparedRouteAccount})
	if state.Outcome != protocol.AdminPreparedRouteFallback || state.Route != nil || reported != 0 {
		t.Fatalf("state=%#v reported=%d", state, reported)
	}
}
