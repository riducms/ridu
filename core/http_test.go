package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

type baseOnlyStore struct {
	inner store.Store
}

func (backend baseOnlyStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return struct{ store.Transaction }{Transaction: transaction}, nil
}

func TestRESTMatchesLocalCRUDAndDecodesQueries(t *testing.T) {
	application := httpFixture(t)
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	local, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("local")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/"+local.ID, nil, "")
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Request-ID") == "" {
		t.Fatalf("find status = %d, request ID = %q", response.StatusCode, response.Header.Get("X-Request-ID"))
	}
	var found struct {
		Doc map[string]any `json:"doc"`
	}
	decodeResponse(t, response, &found)
	if found.Doc["id"] != local.ID || found.Doc["title"] != "local" {
		t.Fatalf("REST document = %#v, local = %#v", found.Doc, local)
	}

	created := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts", strings.NewReader(`{"title":"rest"}`), "")
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d: %s", created.StatusCode, readBody(t, created))
	}
	var createdDocument protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, created, &createdDocument)

	invalidCreate := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts", strings.NewReader(`{"title":""}`), "")
	assertRequiredRESTIssue(t, invalidCreate, "title")
	invalidUpdate := requestJSON(
		t,
		client,
		http.MethodPatch,
		"http://ridu.test/api/collections/posts/"+createdDocument.Doc["id"].(string),
		strings.NewReader(`{"title":""}`),
		"",
	)
	assertRequiredRESTIssue(t, invalidUpdate, "title")

	where := url.QueryEscape(`{"title":{"equals":"rest"}}`)
	listed := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts?where="+where+"&page=1&limit=5", nil, "")
	var page struct {
		Docs       []map[string]any `json:"docs"`
		Pagination struct {
			TotalDocs int `json:"totalDocs"`
		} `json:"pagination"`
	}
	decodeResponse(t, listed, &page)
	if page.Pagination.TotalDocs != 1 || page.Docs[0]["title"] != "rest" {
		t.Fatalf("filtered REST page = %#v", page)
	}
}

func TestRESTSelectionIncludesTrashMetadata(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Trash metadata projection",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Trash:  true,
			Fields: field.Fields{field.Text("title").Required()},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(t.Context(), "posts", store.Values{"title": store.String("not-selected")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(t.Context(), "posts", document.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}

	selected := url.QueryEscape(`{"deletedAt":true}`)
	response := requestJSON(t, handlerClient(application.Handler(ridu.HandlerOptions{})), http.MethodGet,
		"http://ridu.test/api/collections/posts?trash=true&select="+selected, nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("selected trash list status = %d: %s", response.StatusCode, readBody(t, response))
	}
	var envelope protocol.PageEnvelope[map[string]any]
	decodeResponse(t, response, &envelope)
	if len(envelope.Docs) != 1 || envelope.Docs[0]["id"] != document.ID {
		t.Fatalf("selected trash documents = %#v", envelope.Docs)
	}
	if deletedAt, ok := envelope.Docs[0]["deletedAt"].(string); !ok || deletedAt == "" {
		t.Fatalf("selected deletedAt metadata = %#v, want timestamp", envelope.Docs[0]["deletedAt"])
	}
	if _, included := envelope.Docs[0]["title"]; included {
		t.Fatalf("unselected title leaked into projection: %#v", envelope.Docs[0])
	}
}

func TestRESTSelectionSeparatesStoredAndComputedOutputs(t *testing.T) {
	var computedCalls atomic.Int32
	var joinReadCalls atomic.Int32
	var globalComputedCalls atomic.Int32
	application, err := ridu.New(ridu.Config{
		Name: "Output selection",
		Collections: []ridu.Collection{
			{
				Slug: "categories",
				Fields: field.Fields{field.Text("title").Required(), field.Text("privateNote").Required(), field.Virtual("summary", field.ValueString, func(ctx operation.Context) (operation.Value[store.
					Value],

					error) {
					computedCalls.Add(1)
					title, _ := ctx.Root.Get("title").
						StringValue()
					privateNote, _ := ctx.Root.Get("privateNote").
						StringValue()
					return operation.Present(store.String(title + ": " + privateNote)), nil
				}),

					field.Join("posts", "posts", "category")},
			},
			{
				Slug:   "posts",
				Fields: field.Fields{field.Text("title").Required(), field.Relationship("category", "categories").Required()},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					joinReadCalls.Add(1)
					return ridu.Allow(), nil
				}},
			},
		},
		Globals: []ridu.Global{{
			Slug: "settings",
			Fields: field.Fields{field.Text("privateNote").Default("global dependency"), field.Text("otherDefault").Default("must not leak"), field.Virtual("summary", field.ValueString, func(ctx operation.Context) (operation.Value[store.
				Value],

				error) {
				globalComputedCalls.Add(1)
				privateNote, _ := ctx.Root.Get("privateNote").
					StringValue()
				return operation.Present(store.String("Global: " + privateNote)), nil
			})},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	category, err := application.Local().Create(t.Context(), "categories", store.Values{
		"title": store.String("News"), "privateNote": store.String("resolver dependency"),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(t.Context(), "posts", store.Values{
		"title": store.String("Launch"), "category": store.String(category.ID),
	}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	computedCalls.Store(0)
	joinReadCalls.Store(0)
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))

	selectedOutput := url.QueryEscape(`{"summary":true}`)
	response := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/categories/"+category.ID+"?select="+selectedOutput, nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("selected output status = %d: %s", response.StatusCode, readBody(t, response))
	}
	var found protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, response, &found)
	if found.Doc["summary"] != "News: resolver dependency" {
		t.Fatalf("selected computed output = %#v", found.Doc)
	}
	for _, unselected := range []string{"title", "privateNote", "posts"} {
		if _, leaked := found.Doc[unselected]; leaked {
			t.Fatalf("unselected field %q leaked into computed-only response: %#v", unselected, found.Doc)
		}
	}
	if computedCalls.Load() != 1 || joinReadCalls.Load() != 0 {
		t.Fatalf("selected output executions: computed=%d join=%d", computedCalls.Load(), joinReadCalls.Load())
	}

	selectedStored := url.QueryEscape(`{"title":true}`)
	response = requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/categories?select="+selectedStored, nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("selected stored field status = %d: %s", response.StatusCode, readBody(t, response))
	}
	var page protocol.PageEnvelope[map[string]any]
	decodeResponse(t, response, &page)
	if len(page.Docs) != 1 || page.Docs[0]["title"] != "News" {
		t.Fatalf("selected stored response = %#v", page.Docs)
	}
	for _, unselected := range []string{"privateNote", "summary", "posts"} {
		if _, leaked := page.Docs[0][unselected]; leaked {
			t.Fatalf("unselected field %q leaked into stored-only response: %#v", unselected, page.Docs[0])
		}
	}
	if computedCalls.Load() != 1 || joinReadCalls.Load() != 0 {
		t.Fatalf("unselected outputs executed: computed=%d join=%d", computedCalls.Load(), joinReadCalls.Load())
	}

	response = requestJSON(t, client, http.MethodGet, "http://ridu.test/api/globals/settings?select="+selectedOutput, nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("selected missing-global output status = %d: %s", response.StatusCode, readBody(t, response))
	}
	var global protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, response, &global)
	if global.Doc["summary"] != "Global: global dependency" || globalComputedCalls.Load() != 1 {
		t.Fatalf("selected missing-global output = %#v, executions=%d", global.Doc, globalComputedCalls.Load())
	}
	for _, unselected := range []string{"privateNote", "otherDefault"} {
		if _, leaked := global.Doc[unselected]; leaked {
			t.Fatalf("unselected synthesized global %q leaked: %#v", unselected, global.Doc)
		}
	}
}

func TestListWindowRequiresOptionalStoreCapability(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Missing list-window capability",
		Collections: []ridu.Collection{{
			Slug:   "jobs",
			Fields: field.Fields{field.Text("queueKey").Unique().Index()},
		}},
	}, baseOnlyStore{inner: teststore.New()})
	if err != nil {
		t.Fatal(err)
	}
	queueKey, err := query.NewPath("queueKey")
	if err != nil {
		t.Fatal(err)
	}

	_, err = application.Local().ListWindow(t.Context(), "jobs", ridu.ListWindowOptions{
		Index: queueKey, LowerBound: "0:", UpperBound: "1:", Limit: 10,
	})
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "store_failed" || operationError.Status != http.StatusInternalServerError || operationError.Message != "list windows require store.WindowTransaction" {
		t.Fatalf("missing list-window capability error = %#v, %v", operationError, err)
	}
}

func TestRESTEvaluatesSafeDocumentAndFieldCapabilities(t *testing.T) {
	var adminOperation operation.Kind
	titlePath, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	editable := query.Equal(titlePath, query.String("editable"))
	application, err := ridu.New(ridu.Config{
		Name: "Access fixture", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "posts",
			Fields: field.Fields{field.Text("title").Required(), field.Textarea("secret").Access(field.Access{
				Create: func(operation.Context) (bool, error) {
					return false, nil
				},
				Read: func(operation.Context) (bool, error) {
					return false, nil
				},
				Update: func(operation.Context) (bool, error) {
					return false, nil
				},
			})},
			Access: ridu.CollectionAccess{
				Create: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
				Read:   func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
				Update: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Where(editable), nil },
				Delete: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil },
			},
		}, {
			Slug: "users", Auth: true,
			Fields: field.Fields{field.Text("email").Required().Unique()},
			Access: ridu.CollectionAccess{
				Admin: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					adminOperation = ctx.Operation
					return ridu.Deny(), nil
				},
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("editable")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	denied, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("locked")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))

	response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/access/collections/posts", strings.NewReader(`{"id":"`+allowed.ID+`"}`), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("allowed capabilities status = %d: %s", response.StatusCode, readBody(t, response))
	}
	var capabilities protocol.AccessCapabilitiesEnvelope
	decodeResponse(t, response, &capabilities)
	if capabilities.Operations.Admin || capabilities.Operations.ReadVersions || !capabilities.Operations.Create || !capabilities.Operations.Read || !capabilities.Operations.Update || capabilities.Operations.Delete {
		t.Fatalf("allowed operations = %#v", capabilities.Operations)
	}
	if secret := capabilities.Fields["secret"]; secret.Read || secret.Create || secret.Update {
		t.Fatalf("secret capabilities = %#v", secret)
	}
	response = requestJSON(t, client, http.MethodPost, "http://ridu.test/api/access/collections/users", strings.NewReader(`{}`), "")
	decodeResponse(t, response, &capabilities)
	if capabilities.Operations.Admin {
		t.Fatalf("auth collection admin capability = true")
	}
	if adminOperation != operation.Admin {
		t.Fatalf("admin access operation = %q", adminOperation)
	}

	response = requestJSON(t, client, http.MethodPost, "http://ridu.test/api/access/collections/posts", strings.NewReader(`{"id":"`+denied.ID+`"}`), "")
	decodeResponse(t, response, &capabilities)
	if capabilities.Operations.Update {
		t.Fatalf("filtered update capability = true for locked document")
	}
}

func TestRESTResolvesOneBoundedFilteredSelectionWithExactCapabilities(t *testing.T) {
	visibilityPath, _ := query.NewPath("visibility")
	titlePath, _ := query.NewPath("title")
	application, err := ridu.New(ridu.Config{Name: "Selection", Collections: []ridu.Collection{{
		Slug: "posts", Trash: true,
		Fields: field.Fields{field.Text("title").Required(), field.Text("visibility").Required(), field.Textarea("secret").Access(field.Access{Read: func(operation.Context) (bool, error) {
			return false, nil
		}})},
		Access: ridu.CollectionAccess{
			Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Where(query.Equal(visibilityPath, query.String("public"))), nil
			},
			Update: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Where(query.Equal(titlePath, query.String("editable"))), nil
			},
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range []store.Values{
		{"title": store.String("locked"), "visibility": store.String("public"), "secret": store.String("hidden")},
		{"title": store.String("editable"), "visibility": store.String("public"), "secret": store.String("hidden")},
		{"title": store.String("editable"), "visibility": store.String("private"), "secret": store.String("hidden")},
	} {
		if _, err := application.Local().Create(context.Background(), "posts", values, ridu.MutationOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/access/collections/posts/selection", strings.NewReader(`{}`), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("selection status = %d: %s", response.StatusCode, readBody(t, response))
	}
	var selection protocol.CollectionSelectionEnvelope
	decodeResponse(t, response, &selection)
	if selection.TotalDocs != 2 || len(selection.Items) != 2 {
		t.Fatalf("selection = %#v", selection)
	}
	ids := []string{selection.Items[0].ID, selection.Items[1].ID}
	if !slices.IsSorted(ids) {
		t.Fatalf("selection IDs are not canonical: %v", ids)
	}
	updateCapabilities := []bool{selection.Items[0].Access.Operations.Update, selection.Items[1].Access.Operations.Update}
	if updateCapabilities[0] == updateCapabilities[1] {
		t.Fatalf("mixed update capabilities = %v", updateCapabilities)
	}
	for _, item := range selection.Items {
		if item.Access.Fields["secret"].Read {
			t.Fatalf("secret field readable for %s", item.ID)
		}
	}
	response = requestJSON(t, client, http.MethodPost, "http://ridu.test/api/access/collections/posts/selection", strings.NewReader(`{"where":{"title":{"equals":"editable"}}}`), "")
	decodeResponse(t, response, &selection)
	if selection.TotalDocs != 1 || !selection.Items[0].Access.Operations.Update {
		t.Fatalf("filtered selection = %#v", selection)
	}
	response = requestJSON(t, client, http.MethodPost, "http://ridu.test/api/access/collections/posts/selection", strings.NewReader(`{"trash":true}`), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("empty trash selection status = %d: %s", response.StatusCode, readBody(t, response))
	}
	decodeResponse(t, response, &selection)
	if selection.TotalDocs != 0 || len(selection.Items) != 0 {
		t.Fatalf("empty trash selection = %#v", selection)
	}
}

func TestRESTRejectsSelectionOverflowBeforeCapabilityEvaluation(t *testing.T) {
	updateChecks := 0
	application, err := ridu.New(ridu.Config{Name: "Selection bound", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("title").Required()},
		Access: ridu.CollectionAccess{Update: func(ridu.AccessContext) (ridu.AccessDecision, error) {
			updateChecks++
			return ridu.Allow(), nil
		}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 101)
	for index := range ids {
		ids[index] = fmt.Sprintf("post-%03d", index)
		if _, err := application.Local().Import(context.Background(), "posts", store.Values{"title": store.String(ids[index])}, ridu.ImportOptions{ID: ids[index]}); err != nil {
			t.Fatal(err)
		}
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	endpoint := "http://ridu.test/api/access/collections/posts/selection"
	response := requestJSON(t, client, http.MethodPost, endpoint, strings.NewReader(`{}`), "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("overflow status = %d: %s", response.StatusCode, readBody(t, response))
	}
	var failure protocol.ErrorEnvelope
	decodeResponse(t, response, &failure)
	if failure.Error.Code != protocol.ErrorSelectionTooLarge {
		t.Fatalf("overflow error code = %q", failure.Error.Code)
	}
	if updateChecks != 0 {
		t.Fatalf("overflow evaluated %d document capabilities", updateChecks)
	}
	if _, err := application.Local().Delete(context.Background(), "posts", ids[len(ids)-1], ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	response = requestJSON(t, client, http.MethodPost, endpoint, strings.NewReader(`{}`), "")
	var selection protocol.CollectionSelectionEnvelope
	decodeResponse(t, response, &selection)
	if selection.TotalDocs != 100 || len(selection.Items) != 100 || updateChecks != 100 {
		t.Fatalf("exact-limit selection = %d items, %d update checks", selection.TotalDocs, updateChecks)
	}
}

func TestRESTBulkTrashRestorePermanentDeleteAndEmpty(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Trash REST", Collections: []ridu.Collection{{
		Slug: "posts", Trash: true, Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("First")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Second")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().BulkDelete(context.Background(), "posts", []string{first.ID, second.ID}, ridu.BulkOptions{}); err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	base := "http://ridu.test/api/collections/posts"
	body := `{"action":"restoreDeleted","ids":["` + first.ID + `","` + second.ID + `"]}`
	restored := requestJSON(t, client, http.MethodPost, base+"/bulk", strings.NewReader(body), "")
	if restored.StatusCode != http.StatusOK {
		t.Fatalf("bulk restore = %d: %s", restored.StatusCode, readBody(t, restored))
	}
	restored.Body.Close()
	if _, err := application.Local().BulkDelete(context.Background(), "posts", []string{first.ID, second.ID}, ridu.BulkOptions{}); err != nil {
		t.Fatal(err)
	}
	emptied := requestJSON(t, client, http.MethodDelete, base+"?trash=true", nil, "")
	if emptied.StatusCode != http.StatusOK {
		t.Fatalf("empty trash = %d: %s", emptied.StatusCode, readBody(t, emptied))
	}
	var envelope protocol.BulkEnvelope[map[string]any]
	decodeResponse(t, emptied, &envelope)
	if len(envelope.Docs) != 2 {
		t.Fatalf("empty trash docs = %#v", envelope.Docs)
	}
}

func TestRESTDocumentIDTrashCannotEmptyCollectionTrash(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Trash ID REST", AllowIDOnCreate: true, Collections: []ridu.Collection{{
		Slug: "posts", Trash: true, Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Target")}, ridu.MutationOptions{ID: "trash"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Other")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(context.Background(), "posts", other.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodDelete, "http://ridu.test/api/collections/posts/trash", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("delete trash ID = %d: %s", response.StatusCode, readBody(t, response))
	}
	var deleted protocol.DeleteEnvelope
	decodeResponse(t, response, &deleted)
	if deleted.ID != target.ID {
		t.Fatalf("deleted ID = %q, want %q", deleted.ID, target.ID)
	}
	trash, err := application.Local().List(context.Background(), "posts", ridu.ListOptions{TrashOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if trash.Total != 2 {
		t.Fatalf("trash count = %d, want target plus preexisting document", trash.Total)
	}
}

func TestPanickingAuditCallbackCannotChangeCommittedBulkResult(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Audit isolation", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("First")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Second")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics []ridu.RequestErrorEvent
	client := handlerClient(application.Handler(ridu.HandlerOptions{
		Audit: func(ridu.AuditEvent) { panic("audit sink secret") },
		RequestError: func(event ridu.RequestErrorEvent) {
			diagnostics = append(diagnostics, event)
		},
	}))
	body := fmt.Sprintf(`{"action":"update","ids":[%q,%q],"data":{"title":"Updated"}}`, first.ID, second.ID)
	response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts/bulk", strings.NewReader(body), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("bulk response = %d: %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	if len(diagnostics) != 2 || !diagnostics[0].Panic || diagnostics[0].Error == nil || strings.Contains(diagnostics[0].Error.Error(), "secret") {
		t.Fatalf("audit diagnostics = %#v", diagnostics)
	}
	for _, id := range []string{first.ID, second.ID} {
		document, err := application.Local().Find(context.Background(), "posts", id, ridu.FindOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if title, _ := document.Values["title"].StringValue(); title != "Updated" {
			t.Fatalf("committed title = %q", title)
		}
	}
}

func TestReadinessChecksAreTimeBoundedAndPanicIsRedacted(t *testing.T) {
	application := httpFixture(t)
	release := make(chan struct{})
	finished := make(chan struct{})
	var starts atomic.Int32
	var diagnostic ridu.RequestErrorEvent
	handler := application.Handler(ridu.HandlerOptions{
		ReadinessTimeout: 10 * time.Millisecond,
		ReadinessChecks: []ridu.ReadinessCheck{func(context.Context) error {
			starts.Add(1)
			<-release
			close(finished)
			return nil
		}},
		RequestError: func(event ridu.RequestErrorEvent) { diagnostic = event },
	})
	started := time.Now()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://ridu.test/readyz", nil))
	if response.Code != http.StatusServiceUnavailable || time.Since(started) > time.Second {
		t.Fatalf("bounded readiness = %d in %s: %s", response.Code, time.Since(started), response.Body.String())
	}
	if diagnostic.Error == nil || !strings.Contains(diagnostic.Error.Error(), "deadline exceeded") {
		t.Fatalf("readiness diagnostic = %#v", diagnostic)
	}
	for index := 0; index < 100; index++ {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://ridu.test/readyz", nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("concurrent readiness %d = %d: %s", index, response.Code, response.Body.String())
		}
	}
	if starts.Load() != 1 {
		t.Fatalf("hung readiness spawned %d dependency probes", starts.Load())
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("released readiness probe did not finish")
	}

	diagnostic = ridu.RequestErrorEvent{}
	application = httpFixture(t)
	handler = application.Handler(ridu.HandlerOptions{
		ReadinessChecks: []ridu.ReadinessCheck{func(context.Context) error { panic("readiness secret") }},
		RequestError:    func(event ridu.RequestErrorEvent) { diagnostic = event },
	})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://ridu.test/readyz", nil))
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "secret") || diagnostic.Error == nil || strings.Contains(diagnostic.Error.Error(), "secret") {
		t.Fatalf("panicking readiness = %d body=%s diagnostic=%#v", response.Code, response.Body.String(), diagnostic)
	}
}

func TestRESTMutatesInverseJoinInOneRequest(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Join REST", Collections: []ridu.Collection{
		{Slug: "categories", Fields: field.Fields{field.Text("name").Required(), field.Join("posts", "posts", "category")}},
		{Slug: "posts", Fields: field.Fields{field.Text("title").Required(), field.Relationship("category", "categories")}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	source, err := application.Local().Create(context.Background(), "categories", store.Values{"name": store.String("News")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	addition, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Add")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	removal, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Remove"), "category": store.String(source.ID)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	body := `{"additions":["` + addition.ID + `"],"removals":["` + removal.ID + `"]}`
	response := requestJSON(t, client, http.MethodPatch, "http://ridu.test/api/collections/categories/"+source.ID+"/joins/posts", strings.NewReader(body), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("join mutation = %d: %s", response.StatusCode, readBody(t, response))
	}
	var result protocol.JoinMutationEnvelope[map[string]any]
	decodeResponse(t, response, &result)
	if result.Added != 1 || result.Removed != 1 || result.Doc["id"] != source.ID {
		t.Fatalf("join mutation result = %#v", result)
	}
	assertRelationship(t, application, addition.ID, source.ID)
	assertRelationship(t, application, removal.ID, "")
}

func TestRESTInverseJoinRedactsTargetFields(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Redacted join REST", Collections: []ridu.Collection{
		{Slug: "categories", Fields: field.Fields{field.Text("name").Required(), field.Join("posts", "posts", "category")}},
		{Slug: "posts", Fields: field.Fields{field.Text("title").Required(), field.Text("privateNote").Access(field.Access{Read: func(operation.Context) (bool, error) {
			return false, nil
		}}), field.Relationship("category", "categories").Required()}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	category, err := application.Local().Create(context.Background(), "categories", store.Values{"name": store.String("News")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(context.Background(), "posts", store.Values{
		"title": store.String("Public title"), "privateNote": store.String("must not cross the join"),
		"category": store.String(category.ID),
	}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}

	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/categories/"+category.ID, nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("join read = %d: %s", response.StatusCode, readBody(t, response))
	}
	var result protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, response, &result)
	joined, valid := result.Doc["posts"].([]any)
	if !valid || len(joined) != 1 {
		t.Fatalf("joined posts = %#v", result.Doc["posts"])
	}
	post, valid := joined[0].(map[string]any)
	if !valid || post["title"] != "Public title" {
		t.Fatalf("joined post = %#v", joined[0])
	}
	if _, leaked := post["privateNote"]; leaked {
		t.Fatalf("REST inverse join leaked field-read denied value: %#v", post)
	}
}

func TestRESTVersionDetailAndScheduledPublishing(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Scheduled REST", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields:        field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Queued")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	document, err = application.Local().Publish(context.Background(), "posts", document.ID, ridu.MutationOptions{ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	base := "http://ridu.test/api/collections/posts/" + document.ID

	versionResponse := requestJSON(t, client, http.MethodGet, base+"/versions/1", nil, "")
	if versionResponse.StatusCode != http.StatusOK {
		t.Fatalf("version detail = %d: %s", versionResponse.StatusCode, readBody(t, versionResponse))
	}
	var version struct {
		Version struct {
			Revision int            `json:"Revision"`
			Snapshot map[string]any `json:"Snapshot"`
		} `json:"version"`
	}
	decodeResponse(t, versionResponse, &version)
	if version.Version.Revision != 1 || version.Version.Snapshot["title"] != "Queued" {
		t.Fatalf("version = %#v", version.Version)
	}

	scheduleResponse := requestJSON(t, client, http.MethodPost, base+"/schedule", strings.NewReader(`{"runAt":"2030-01-02T03:04:05Z"}`), "")
	if scheduleResponse.StatusCode != http.StatusCreated {
		t.Fatalf("schedule = %d: %s", scheduleResponse.StatusCode, readBody(t, scheduleResponse))
	}
	var scheduled protocol.ScheduledPublishEnvelope
	decodeResponse(t, scheduleResponse, &scheduled)
	if scheduled.ScheduledPublish.DocumentID != document.ID || scheduled.ScheduledPublish.ExpectedRevision != document.Revision {
		t.Fatalf("scheduled = %#v", scheduled.ScheduledPublish)
	}

	listResponse := requestJSON(t, client, http.MethodGet, base+"/schedule", nil, "")
	var listed protocol.ScheduledPublishesEnvelope
	decodeResponse(t, listResponse, &listed)
	if len(listed.ScheduledPublishes) != 1 || listed.ScheduledPublishes[0].ID != scheduled.ScheduledPublish.ID {
		t.Fatalf("scheduled list = %#v", listed.ScheduledPublishes)
	}

	cancelResponse := requestJSON(t, client, http.MethodDelete, base+"/schedule/"+scheduled.ScheduledPublish.ID, nil, "")
	if cancelResponse.StatusCode != http.StatusOK {
		t.Fatalf("cancel = %d: %s", cancelResponse.StatusCode, readBody(t, cancelResponse))
	}
	listResponse = requestJSON(t, client, http.MethodGet, base+"/schedule", nil, "")
	decodeResponse(t, listResponse, &listed)
	if len(listed.ScheduledPublishes) != 0 {
		t.Fatalf("scheduled list after cancel = %#v", listed.ScheduledPublishes)
	}

	restoreResponse := requestJSON(t, client, http.MethodPost, base+"/restore/2?draft=true", nil, "")
	if restoreResponse.StatusCode != http.StatusOK {
		t.Fatalf("restore as draft = %d: %s", restoreResponse.StatusCode, readBody(t, restoreResponse))
	}
	var restored protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, restoreResponse, &restored)
	if restored.Doc["_status"] != "draft" || restored.Doc["title"] != "Queued" {
		t.Fatalf("restored as draft = %#v", restored.Doc)
	}
	invalidDraft := requestJSON(t, client, http.MethodPost, base+"/restore/2?draft=perhaps", nil, "")
	if invalidDraft.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid restore draft = %d: %s", invalidDraft.StatusCode, readBody(t, invalidDraft))
	}
}

func TestRESTScheduledPublishBindsExactAuthCollectionAndRechecksRequester(t *testing.T) {
	const requesterID = "shared-schedule-requester"
	publishersOnly := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor != nil {
			role, _ := ctx.Actor.Values["role"].StringValue()
			if role == "publisher" {
				return ridu.Allow(), nil
			}
		}
		return ridu.Deny(), nil
	}
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{
		Name: "Scheduled REST identity", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: field.Fields{field.Text("email").Required().Unique(), field.Text("role").Required()}},
			{
				Slug: "staff", Auth: true,
				AuthConfig: ridu.AuthConfig{Strategies: []ridu.AuthStrategy{{Name: "schedule-header", Authenticate: func(ctx ridu.AuthStrategyContext) (ridu.AuthStrategyResult, error) {
					values := ctx.Headers["X-Schedule-Auth"]
					return ridu.AuthStrategyResult{Authenticated: len(values) == 1 && values[0] == "accepted", UserID: requesterID}, nil
				}}}},
				Fields: field.Fields{field.Text("email").Required().Unique(), field.Text("role").Required()},
			},
			{Slug: "books", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Access: ridu.CollectionAccess{Update: publishersOnly}, Fields: field.Fields{field.Text("title").Required()}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	collections := make(map[schema.CollectionSlug]schema.Collection)
	for _, collection := range application.Manifest().Snapshot().Collections {
		collections[collection.Slug] = collection
	}
	ctx := context.Background()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	usersActor, err := transaction.Create(ctx, store.CreateRequest{
		Collection: collections["users"], ID: requesterID,
		Values: store.Values{"email": store.String("schedule-user@example.test"), "role": store.String("publisher")},
	})
	if err != nil {
		t.Fatal(err)
	}
	staffActor, err := transaction.Create(ctx, store.CreateRequest{
		Collection: collections["staff"], ID: requesterID,
		Values: store.Values{"email": store.String("schedule-staff@example.test"), "role": store.String("publisher")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(ctx, "books", store.Values{"title": store.String("Exact requester")}, ridu.MutationOptions{Actor: &staffActor})
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	target := "http://ridu.test/api/collections/books/" + document.ID + "/schedule"
	body := strings.NewReader(`{"runAt":"` + time.Now().Add(-time.Second).UTC().Format(time.RFC3339) + `"}`)
	request, err := http.NewRequest(http.MethodPost, target, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Schedule-Auth", "accepted")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("schedule = %d: %s", response.StatusCode, readBody(t, response))
	}
	var scheduled protocol.ScheduledPublishEnvelope
	decodeResponse(t, response, &scheduled)
	queued, err := backend.FindTask(ctx, scheduled.ScheduledPublish.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.RequestedBy == nil || queued.RequestedBy.CollectionID != collections["staff"].ID || queued.RequestedBy.DocumentID != requesterID {
		t.Fatalf("queued requester = %#v, want staff/%s", queued.RequestedBy, requesterID)
	}

	staffActor, err = application.Local().Update(ctx, "staff", staffActor.ID, store.Values{"role": store.String("revoked")}, ridu.MutationOptions{Actor: &staffActor})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := application.RunScheduledPublishes(ctx, 10, nil)
	if err != nil || completed != 0 {
		t.Fatalf("run scheduled = %d, %v", completed, err)
	}
	failed, err := backend.FindTask(ctx, scheduled.ScheduledPublish.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != store.TaskStateFailed || failed.LastErrorCode != "scheduled_publish_rejected" {
		t.Fatalf("scheduled task after staff revocation = %#v", failed)
	}
	draft := true
	stored, err := application.Local().Find(ctx, "books", document.ID, ridu.FindOptions{Actor: &usersActor, Draft: &draft})
	if err != nil || stored.Status != store.StatusDraft {
		t.Fatalf("document after denied scheduled publish = %#v, %v", stored, err)
	}
}

func TestRESTPreviewTokensAreShortLivedReadOnlyAndTargetScoped(t *testing.T) {
	externalActorID := "shared-external-actor"
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{
		Name: "Preview REST", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{
				Slug: "users", Auth: true,
				AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}, APIKeys: true},
				Fields:     field.Fields{field.Text("email").Required().Unique(), field.Text("role")},
			},
			{
				Slug: "staff", Auth: true,
				AuthConfig: ridu.AuthConfig{Strategies: []ridu.AuthStrategy{{Name: "preview-header", Authenticate: func(ctx ridu.AuthStrategyContext) (ridu.AuthStrategyResult, error) {
					values := ctx.Headers["X-Preview-Auth"]
					return ridu.AuthStrategyResult{Authenticated: len(values) == 1 && values[0] == "accepted", UserID: externalActorID}, nil
				}}}},
				Fields: field.Fields{field.Text("email").Required().Unique(), field.Text("role")},
			},
			{
				Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Admin:  ridu.CollectionAdmin{LivePreview: ridu.LivePreviewConfig{URL: "https://preview.example.test/posts/{id}"}},
				Fields: field.Fields{field.Text("title").Required()},
				Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if ctx.Actor != nil {
						role, _ := ctx.Actor.Values["role"].StringValue()
						if role == "staff" {
							return ridu.Allow(), nil
						}
					}
					return ridu.Deny(), nil
				}},
			},
			{
				Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Admin:  ridu.CollectionAdmin{LivePreview: ridu.LivePreviewConfig{URL: "https://preview.example.test/pages/{id}"}},
				Fields: field.Fields{field.Text("title").Required()},
			},
		},
		Globals: []ridu.Global{{
			Slug: "site-settings", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Admin:  ridu.GlobalAdmin{LivePreview: ridu.LivePreviewConfig{URL: "https://preview.example.test/settings"}},
			Fields: field.Fields{field.Text("siteName").Required()},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	collections := make(map[schema.CollectionSlug]schema.Collection)
	for _, collection := range application.Manifest().Snapshot().Collections {
		collections[collection.Slug] = collection
	}
	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		collection schema.CollectionSlug
		email      string
		role       string
	}{
		{collection: "users", email: "collision-user@example.test", role: "reader"},
		{collection: "staff", email: "collision-staff@example.test", role: "staff"},
	} {
		if _, err := transaction.Create(context.Background(), store.CreateRequest{
			Collection: collections[fixture.collection], ID: externalActorID,
			Values: store.Values{"email": store.String(fixture.email), "role": store.String(fixture.role)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	actor, err := application.CreateAuthUser(context.Background(), "users", store.Values{
		"email": store.String("preview@example.test"), "role": store.String("staff"),
	}, "correct-horse-battery", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Draft preview")}, ridu.MutationOptions{Actor: &actor})
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Other draft")}, ridu.MutationOptions{Actor: &actor})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Import(context.Background(), "pages", store.Values{"title": store.String("Same ID, different collection")}, ridu.ImportOptions{
		ID: first.ID, Status: store.StatusDraft, Actor: &actor}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().UpdateGlobal(context.Background(), "site-settings", store.Values{"siteName": store.String("Ridu")}, ridu.MutationOptions{Actor: &actor}); err != nil {
		t.Fatal(err)
	}

	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	login := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"preview@example.test","password":"correct-horse-battery"}`), "")
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login = %d: %s", login.StatusCode, readBody(t, login))
	}
	cookie := login.Cookies()[0].String()
	login.Body.Close()

	unauthenticated := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/preview/collections/posts/"+first.ID+"/token", nil, "")
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated mint = %d: %s", unauthenticated.StatusCode, readBody(t, unauthenticated))
	}
	unauthenticated.Body.Close()

	minted := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/preview/collections/posts/"+first.ID+"/token", nil, cookie)
	if minted.StatusCode != http.StatusCreated || minted.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("mint = %d cache=%q: %s", minted.StatusCode, minted.Header.Get("Cache-Control"), readBody(t, minted))
	}
	var capability protocol.PreviewTokenEnvelope
	decodeResponse(t, minted, &capability)
	if capability.PreviewToken.Resource != "collection" || capability.PreviewToken.Slug != "posts" || capability.PreviewToken.DocumentID != first.ID || capability.PreviewToken.Token == "" {
		t.Fatalf("preview token = %#v", capability.PreviewToken)
	}
	collectionToken := capability.PreviewToken.Token
	expiresAt, err := time.Parse(time.RFC3339Nano, capability.PreviewToken.ExpiresAt)
	if err != nil || time.Until(expiresAt) < 4*time.Minute || time.Until(expiresAt) > 6*time.Minute {
		t.Fatalf("preview expiry = %q (%v)", capability.PreviewToken.ExpiresAt, err)
	}

	preview := previewRequest(t, client, "http://ridu.test/api/preview/collections/posts/"+first.ID, capability.PreviewToken.Token)
	if preview.StatusCode != http.StatusOK || preview.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("preview read = %d cache=%q: %s", preview.StatusCode, preview.Header.Get("Cache-Control"), readBody(t, preview))
	}
	var document protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, preview, &document)
	if document.Doc["title"] != "Draft preview" || document.Doc["_status"] != "draft" {
		t.Fatalf("preview document = %#v", document.Doc)
	}

	wrongTarget := previewRequest(t, client, "http://ridu.test/api/preview/collections/posts/"+second.ID, capability.PreviewToken.Token)
	var wrongTargetError protocol.ErrorEnvelope
	decodeResponse(t, wrongTarget, &wrongTargetError)
	if wrongTarget.StatusCode != http.StatusUnauthorized || wrongTargetError.Error.Code != protocol.ErrorInvalidPreviewToken {
		t.Fatalf("wrong-target preview = %d %#v", wrongTarget.StatusCode, wrongTargetError.Error)
	}
	wrongCollection := previewRequest(t, client, "http://ridu.test/api/preview/collections/pages/"+first.ID, capability.PreviewToken.Token)
	if wrongCollection.StatusCode != http.StatusUnauthorized {
		t.Fatalf("same-ID wrong-collection preview = %d: %s", wrongCollection.StatusCode, readBody(t, wrongCollection))
	}
	wrongCollection.Body.Close()
	normalREST := previewRequest(t, client, "http://ridu.test/api/collections/posts/"+first.ID, capability.PreviewToken.Token)
	if normalREST.StatusCode == http.StatusOK {
		t.Fatal("preview token authenticated an ordinary REST request")
	}
	normalREST.Body.Close()

	globalMint := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/preview/globals/site-settings/token", nil, cookie)
	if globalMint.StatusCode != http.StatusCreated {
		t.Fatalf("global mint = %d: %s", globalMint.StatusCode, readBody(t, globalMint))
	}
	decodeResponse(t, globalMint, &capability)
	globalPreview := previewRequest(t, client, "http://ridu.test/api/preview/globals/site-settings", capability.PreviewToken.Token)
	decodeResponse(t, globalPreview, &document)
	if globalPreview.StatusCode != http.StatusOK || document.Doc["siteName"] != "Ridu" {
		t.Fatalf("global preview = %d %#v", globalPreview.StatusCode, document.Doc)
	}
	collectionAsGlobal := previewRequest(t, client, "http://ridu.test/api/preview/globals/site-settings", collectionToken)
	if collectionAsGlobal.StatusCode != http.StatusUnauthorized {
		t.Fatalf("collection token on global = %d: %s", collectionAsGlobal.StatusCode, readBody(t, collectionAsGlobal))
	}
	collectionAsGlobal.Body.Close()
	globalAsCollection := previewRequest(t, client, "http://ridu.test/api/preview/collections/posts/"+first.ID, capability.PreviewToken.Token)
	if globalAsCollection.StatusCode != http.StatusUnauthorized {
		t.Fatalf("global token on collection = %d: %s", globalAsCollection.StatusCode, readBody(t, globalAsCollection))
	}
	globalAsCollection.Body.Close()

	directSession, err := application.Login(context.Background(), "users", "preview@example.test", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	key, err := application.CreateAPIKey(context.Background(), directSession.Token, "Preview", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	apiKeyMint := previewMintRequest(t, client, "http://ridu.test/api/preview/collections/posts/"+first.ID+"/token", map[string]string{
		"Authorization": "Bearer " + key.Key,
	})
	if apiKeyMint.StatusCode != http.StatusCreated {
		t.Fatalf("API-key preview mint = %d: %s", apiKeyMint.StatusCode, readBody(t, apiKeyMint))
	}
	apiKeyMint.Body.Close()

	externalMint := previewMintRequest(t, client, "http://ridu.test/api/preview/collections/posts/"+first.ID+"/token", map[string]string{
		"X-Preview-Auth": "accepted",
	})
	if externalMint.StatusCode != http.StatusCreated {
		t.Fatalf("external-strategy preview mint = %d: %s", externalMint.StatusCode, readBody(t, externalMint))
	}
	externalMint.Body.Close()

	revocable := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/preview/collections/posts/"+first.ID+"/token", nil, cookie)
	if revocable.StatusCode != http.StatusCreated {
		t.Fatalf("revocable mint = %d: %s", revocable.StatusCode, readBody(t, revocable))
	}
	decodeResponse(t, revocable, &capability)
	revoke := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/preview/token/revoke", strings.NewReader(`{"token":"`+capability.PreviewToken.Token+`"}`), cookie)
	if revoke.StatusCode != http.StatusOK || revoke.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("preview revoke = %d cache=%q: %s", revoke.StatusCode, revoke.Header.Get("Cache-Control"), readBody(t, revoke))
	}
	revoke.Body.Close()
	revokedRead := previewRequest(t, client, "http://ridu.test/api/preview/collections/posts/"+first.ID, capability.PreviewToken.Token)
	if revokedRead.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked preview read = %d: %s", revokedRead.StatusCode, readBody(t, revokedRead))
	}
	revokedRead.Body.Close()
}

func previewRequest(t *testing.T, client *http.Client, target, token string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func previewMintRequest(t *testing.T, client *http.Client, target string, headers map[string]string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertRequiredRESTIssue(t *testing.T, response *http.Response, path string) {
	t.Helper()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("validation status = %d, want 422: %s", response.StatusCode, readBody(t, response))
	}
	var envelope protocol.ErrorEnvelope
	decodeResponse(t, response, &envelope)
	if envelope.Error.Code != protocol.ErrorValidation || len(envelope.Error.Issues) != 1 ||
		envelope.Error.Issues[0].Code != "required" || envelope.Error.Issues[0].Path != path {
		t.Fatalf("validation error = %#v", envelope.Error)
	}
}

func TestRESTRejectsMalformedOversizedCanceledAndUnknownRequests(t *testing.T) {
	application := httpFixture(t)
	handler := application.Handler(ridu.HandlerOptions{MaxBodyBytes: 32})
	client := handlerClient(handler)
	tests := []struct {
		name, method, path, body string
		status                   int
	}{
		{"unknown collection", http.MethodGet, "/api/collections/missing", "", 404},
		{"malformed path", http.MethodGet, "/api/collections/posts/", "", 400},
		{"unknown query", http.MethodGet, "/api/collections/posts?surprise=yes", "", 400},
		{"bad pagination", http.MethodGet, "/api/collections/posts?page=0", "", 400},
		{"bad json", http.MethodPost, "/api/collections/posts", `{`, 400},
		{"trailing json", http.MethodPost, "/api/collections/posts", `{"title":"x"}{}`, 400},
		{"oversized", http.MethodPost, "/api/collections/posts", `{"title":"this payload is deliberately much too long"}`, 413},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := requestJSON(t, client, test.method, "http://ridu.test"+test.path, strings.NewReader(test.body), "")
			if response.StatusCode != test.status {
				t.Fatalf("status = %d, want %d: %s", response.StatusCode, test.status, readBody(t, response))
			}
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/collections/posts", nil).WithContext(canceled)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestTimeout {
		t.Fatalf("canceled status = %d, want 408", recorder.Code)
	}
}

func TestRESTRejectsAmbiguousAndStructurallyPathologicalJSON(t *testing.T) {
	application := httpFixture(t)
	client := handlerClient(application.Handler(ridu.HandlerOptions{MaxBodyBytes: 16 << 10}))

	duplicate := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts", strings.NewReader(`{"title":"first","title":"second"}`), "")
	if duplicate.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate key status = %d: %s", duplicate.StatusCode, readBody(t, duplicate))
	}
	var duplicateEnvelope protocol.ErrorEnvelope
	decodeResponse(t, duplicate, &duplicateEnvelope)
	if duplicateEnvelope.Error.Message != "JSON body contains a duplicate object key" {
		t.Fatalf("duplicate key error = %#v", duplicateEnvelope.Error)
	}

	deepValue := strings.Repeat("[", 130) + "null" + strings.Repeat("]", 130)
	deep := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts", strings.NewReader(`{"title":`+deepValue+`}`), "")
	if deep.StatusCode != http.StatusBadRequest {
		t.Fatalf("deep JSON status = %d: %s", deep.StatusCode, readBody(t, deep))
	}
	var deepEnvelope protocol.ErrorEnvelope
	decodeResponse(t, deep, &deepEnvelope)
	if deepEnvelope.Error.Message != "JSON body exceeds the structural complexity limit" {
		t.Fatalf("deep JSON error = %#v", deepEnvelope.Error)
	}
}

func TestRESTLoginCurrentSessionAndLogout(t *testing.T) {
	application := httpFixture(t)
	user, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("ada@example.test")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))

	wrong := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"ada@example.test","password":"wrong-pass"}`), "")
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d", wrong.StatusCode)
	}
	login := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"ada@example.test","password":"correct-horse"}`), "")
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d: %s", login.StatusCode, readBody(t, login))
	}
	cookies := login.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookies = %#v", cookies)
	}
	current := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/auth/me", nil, cookies[0].String())
	if current.StatusCode != http.StatusOK {
		t.Fatalf("current session status = %d: %s", current.StatusCode, readBody(t, current))
	}
	var currentSession protocol.SessionEnvelope[map[string]any]
	decodeResponse(t, current, &currentSession)
	if currentSession.Session.Collection != "users" {
		t.Fatalf("session collection = %q, want users", currentSession.Session.Collection)
	}
	logout := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/logout", bytes.NewReader([]byte(`{}`)), cookies[0].String())
	if logout.StatusCode != http.StatusOK || logout.Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout status/cookie = %d %#v", logout.StatusCode, logout.Cookies())
	}
	missing := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/auth/me", nil, cookies[0].String())
	if missing.StatusCode != http.StatusUnauthorized {
		t.Fatalf("logged-out session status = %d", missing.StatusCode)
	}
}

func TestRESTCreatesAuthUserWithoutExposingPassword(t *testing.T) {
	application := httpFixture(t)
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	created := requestJSON(
		t,
		client,
		http.MethodPost,
		"http://ridu.test/api/auth/users/create-user",
		strings.NewReader(`{"data":{"email":"new-user@example.test"},"password":"correct-horse"}`),
		"",
	)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create auth user = %d: %s", created.StatusCode, readBody(t, created))
	}
	var document protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, created, &document)
	if document.Doc["email"] != "new-user@example.test" || document.Doc["password"] != nil {
		t.Fatalf("created auth user = %#v", document.Doc)
	}
	login := requestJSON(
		t,
		client,
		http.MethodPost,
		"http://ridu.test/api/auth/users/login",
		strings.NewReader(`{"email":"new-user@example.test","password":"correct-horse"}`),
		"",
	)
	if login.StatusCode != http.StatusOK {
		t.Fatalf("created auth user login = %d: %s", login.StatusCode, readBody(t, login))
	}
	cookies := login.Cookies()
	login.Body.Close()
	if len(cookies) != 1 {
		t.Fatalf("created auth user cookies = %#v", cookies)
	}
	initialized := requestJSON(
		t,
		client,
		http.MethodPost,
		"http://ridu.test/api/auth/users/create-user",
		strings.NewReader(`{"data":{"email":"weak@example.test"},"password":"short"}`),
		"",
	)
	if initialized.StatusCode != http.StatusForbidden {
		t.Fatalf("initialized anonymous auth create = %d: %s", initialized.StatusCode, readBody(t, initialized))
	}
	initialized.Body.Close()
	authenticated := requestJSON(
		t,
		client,
		http.MethodPost,
		"http://ridu.test/api/auth/users/create-user",
		strings.NewReader(`{"data":{"email":"authenticated@example.test"},"password":"correct-horse"}`),
		cookies[0].String(),
	)
	if authenticated.StatusCode != http.StatusCreated {
		t.Fatalf("authenticated auth create = %d: %s", authenticated.StatusCode, readBody(t, authenticated))
	}
	authenticated.Body.Close()
}

func TestRESTReportsOneTimeAdminBootstrapAvailability(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Bootstrap status", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Fields:     field.Fields{field.Email("email").Required().Unique()},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))

	status := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/auth/users/bootstrap", nil, "")
	if status.StatusCode != http.StatusOK || status.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("empty bootstrap status = %d, cache %q: %s", status.StatusCode, status.Header.Get("Cache-Control"), readBody(t, status))
	}
	var bootstrap protocol.AuthBootstrapEnvelope
	decodeResponse(t, status, &bootstrap)
	if !bootstrap.Available {
		t.Fatal("empty admin collection did not advertise setup")
	}

	created := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/create-user", strings.NewReader(`{"data":{"email":"admin@example.test"},"password":"correct-horse"}`), "")
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("first admin create = %d: %s", created.StatusCode, readBody(t, created))
	}
	created.Body.Close()

	status = requestJSON(t, client, http.MethodGet, "http://ridu.test/api/auth/users/bootstrap", nil, "")
	if status.StatusCode != http.StatusOK {
		t.Fatalf("initialized bootstrap status = %d: %s", status.StatusCode, readBody(t, status))
	}
	decodeResponse(t, status, &bootstrap)
	if bootstrap.Available {
		t.Fatal("initialized admin collection still advertised setup")
	}

	wrongMethod := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/bootstrap", bytes.NewReader([]byte(`{}`)), "")
	if wrongMethod.StatusCode != http.StatusMethodNotAllowed || wrongMethod.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("bootstrap wrong method = %d, allow %q", wrongMethod.StatusCode, wrongMethod.Header.Get("Allow"))
	}
	wrongMethod.Body.Close()
}

func TestRESTExplicitCreatePolicyKeepsIntentionalPublicRegistration(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Public registration", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Fields:     field.Fields{field.Email("email").Required().Unique()},
			Access: ridu.CollectionAccess{Create: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Allow(), nil
			}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	for _, email := range []string{"first@example.test", "second@example.test"} {
		response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/create-user", strings.NewReader(`{"data":{"email":"`+email+`"},"password":"correct-horse"}`), "")
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("public registration for %s = %d: %s", email, response.StatusCode, readBody(t, response))
		}
		response.Body.Close()
	}
}

func TestRESTAnonymousBootstrapIsLimitedToConfiguredAdminCollection(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Multi-auth bootstrap", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}}, Fields: field.Fields{field.Email("email").Required().Unique()}},
			{Slug: "staff", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}}, Fields: field.Fields{field.Email("email").Required().Unique()}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	staff := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/staff/create-user", strings.NewReader(`{"data":{"email":"staff@example.test"},"password":"correct-horse"}`), "")
	if staff.StatusCode != http.StatusForbidden {
		t.Fatalf("secondary auth bootstrap = %d: %s", staff.StatusCode, readBody(t, staff))
	}
	staff.Body.Close()
	admin := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/create-user", strings.NewReader(`{"data":{"email":"admin@example.test"},"password":"correct-horse"}`), "")
	if admin.StatusCode != http.StatusCreated {
		t.Fatalf("admin auth bootstrap = %d: %s", admin.StatusCode, readBody(t, admin))
	}
	admin.Body.Close()
	staff = requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/staff/create-user", strings.NewReader(`{"data":{"email":"staff@example.test"},"password":"correct-horse"}`), "")
	if staff.StatusCode != http.StatusForbidden {
		t.Fatalf("secondary auth bootstrap after admin initialization = %d: %s", staff.StatusCode, readBody(t, staff))
	}
	staff.Body.Close()
}

func TestGenericCollectionCreateRejectsAuthCollections(t *testing.T) {
	application := httpFixture(t)
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	missingPassword := requestJSON(
		t,
		client,
		http.MethodPost,
		"http://ridu.test/api/collections/users",
		strings.NewReader(`{"email":"credentialless@example.test"}`),
		"",
	)
	if missingPassword.StatusCode != http.StatusBadRequest {
		t.Fatalf("credentialless auth create = %d: %s", missingPassword.StatusCode, readBody(t, missingPassword))
	}
	missingPassword.Body.Close()

	withPassword := requestJSON(
		t,
		client,
		http.MethodPost,
		"http://ridu.test/api/collections/users",
		strings.NewReader(`{"email":"generic-user@example.test","password":"correct-horse"}`),
		"",
	)
	if withPassword.StatusCode != http.StatusBadRequest {
		t.Fatalf("generic auth create with password = %d: %s", withPassword.StatusCode, readBody(t, withPassword))
	}
}

func TestRESTSortSelectCountPopulateAndEmbeddedAdminFallback(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{
		Name: "Query fixture",
		Collections: []ridu.Collection{
			{Slug: "authors", Versions: true, Fields: field.Fields{field.Text("email").Required()}},
			{Slug: "articles", Fields: field.Fields{field.Text("title").Required(), field.Relationship("author", "authors").Required()}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	author, err := application.Local().Create(context.Background(), "authors", store.Values{"email": store.String("ada@example.test")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Zulu", "Alpha"} {
		if _, err := application.Local().Create(context.Background(), "articles", store.Values{
			"title": store.String(title), "author": store.String(author.ID),
		}, ridu.MutationOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	query := url.Values{}
	query.Add("sort", "title")
	query.Set("select", `{"title":true,"author":true}`)
	query.Set("populate", `{"author":{"depth":2,"select":{"email":true,"_status":true,"_revision":true}}}`)
	response := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/articles?"+query.Encode(), nil, "")
	var page struct {
		Docs []struct {
			Title  string `json:"title"`
			Author struct {
				ID       string `json:"id"`
				Email    string `json:"email"`
				Status   string `json:"_status"`
				Revision int    `json:"_revision"`
			} `json:"author"`
		} `json:"docs"`
	}
	decodeResponse(t, response, &page)
	if len(page.Docs) != 2 || page.Docs[0].Title != "Alpha" || page.Docs[0].Author.ID != author.ID || page.Docs[0].Author.Email != "ada@example.test" || page.Docs[0].Author.Status != "published" || page.Docs[0].Author.Revision < 1 {
		t.Fatalf("query page = %#v", page)
	}
	count := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/articles/count", nil, "")
	var total protocol.CountEnvelope
	decodeResponse(t, count, &total)
	if total.TotalDocs != 2 {
		t.Fatalf("count = %d, want 2", total.TotalDocs)
	}
	admin := requestJSON(t, client, http.MethodGet, "http://ridu.test/admin/collections/articles/create", nil, "")
	if admin.StatusCode != http.StatusOK || !strings.Contains(admin.Header.Get("Content-Type"), "text/html") || !strings.Contains(readBody(t, admin), `<div id="app"></div>`) {
		t.Fatal("direct admin route did not return the embedded SPA entry")
	}
}

func TestHTTPSecurityRateLimitSessionRotationTrustedProxyAndAudit(t *testing.T) {
	application := httpFixture(t)
	user, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("security@example.test")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	var audits []ridu.AuditEvent
	handler := application.Handler(ridu.HandlerOptions{
		AllowedOrigins: []string{"https://admin.example.test"}, TrustedProxyCIDRs: []string{"192.0.2.0/24"},
		AuthRateLimit: 1, AuthRateWindow: time.Hour, Audit: func(event ridu.AuditEvent) { audits = append(audits, event) },
	})
	client := handlerClient(handler)
	deniedRequest, _ := http.NewRequest(http.MethodPost, "http://ridu.test/api/collections/posts", strings.NewReader(`{"title":"blocked"}`))
	deniedRequest.Header.Set("Origin", "https://evil.example.test")
	denied, err := client.Do(deniedRequest)
	if err != nil || denied.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin mutation = %v, %v", denied, err)
	}
	login := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"security@example.test","password":"correct-horse"}`), "")
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login = %d: %s", login.StatusCode, readBody(t, login))
	}
	original := login.Cookies()[0]
	resolved := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/auth/me", nil, original.String())
	if resolved.StatusCode != http.StatusOK || len(resolved.Cookies()) != 0 {
		t.Fatalf("current session must not rotate implicitly: status=%d cookies=%#v", resolved.StatusCode, resolved.Cookies())
	}
	current := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/refresh", nil, original.String())
	if current.StatusCode != http.StatusOK || len(current.Cookies()) != 1 || current.Cookies()[0].Value == original.Value {
		t.Fatalf("session was not rotated: status=%d cookies=%#v", current.StatusCode, current.Cookies())
	}
	stale := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/refresh", nil, original.String())
	if stale.StatusCode != http.StatusUnauthorized {
		t.Fatalf("rotated session remained valid: %d", stale.StatusCode)
	}
	limited := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"security@example.test","password":"correct-horse"}`), "")
	if limited.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("rate-limited login = %d", limited.StatusCode)
	}
	auditRequest, _ := http.NewRequest(http.MethodPost, "http://ridu.test/api/collections/posts", strings.NewReader(`{"title":"audited"}`))
	auditRequest.Header.Set("Origin", "https://admin.example.test")
	auditRequest.Header.Set("X-Forwarded-For", "203.0.113.44")
	auditRequest.RemoteAddr = "192.0.2.4:4242"
	audited, err := client.Do(auditRequest)
	if err != nil || audited.StatusCode != http.StatusCreated {
		t.Fatalf("audited create = %v, %v", audited, err)
	}
	if len(audits) < 2 || audits[len(audits)-1].ClientIP != "203.0.113.44" || audits[len(audits)-1].Action != "create" {
		t.Fatalf("audit events = %#v", audits)
	}
	var created protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, audited, &created)
	read := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/"+created.Doc["id"].(string), nil, "")
	read.Body.Close()
	if read.StatusCode != http.StatusOK || audits[len(audits)-1].Action != "read" {
		t.Fatalf("audited read = %d, events = %#v", read.StatusCode, audits)
	}
}

func TestAuthRateLimitCannotBeBypassedByRotatingIdentities(t *testing.T) {
	application := httpFixture(t)
	client := handlerClient(application.Handler(ridu.HandlerOptions{AuthRateLimit: 2, AuthRateWindow: time.Hour}))
	for index := 0; index < 3; index++ {
		body := fmt.Sprintf(`{"email":"rotated-%d@example.test","password":"deliberately-wrong"}`, index)
		response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(body), "")
		if index < 2 && response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("rotated login %d = %d, want 401: %s", index, response.StatusCode, readBody(t, response))
		}
		if index == 2 && response.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("rotated login %d = %d, want 429: %s", index, response.StatusCode, readBody(t, response))
		}
		response.Body.Close()
	}
}

func httpFixture(t *testing.T) *ridu.App {
	t.Helper()
	application, err := ridu.New(ridu.Config{
		Name:  "HTTP fixture",
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}}, Fields: field.Fields{field.Text("email").Required().Unique()}},
			{Slug: "posts", Fields: field.Fields{field.Text("title").Required()}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	return application
}

func requestJSON(t *testing.T, client *http.Client, method, target string, body io.Reader, cookie string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	encoded, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func handlerClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
}
