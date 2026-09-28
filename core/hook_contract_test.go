package core_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/store"
)

func TestRejectReturnsTheHookMessageAndFieldIssues(t *testing.T) {
	keepFeatured := func(ctx ridu.HookContext) error {
		if featured, _ := ctx.Original.Values["featured"].BooleanValue(); featured {
			return ridu.Reject("Remove the article from the homepage first",
				operation.Issue{Code: "featured", Message: "Unfeature it", Target: operation.At("featured")})
		}
		return nil
	}
	noShouting := func(_ operation.Context, value operation.Value[store.Value]) (operation.Change[store.Value], error) {
		raw, _ := value.Get()
		if title, _ := raw.Get("title").StringValue(); title != "" && title == strings.ToUpper(title) {
			return operation.Keep[store.Value](), ridu.Reject("Titles cannot be all capitals",
				operation.Issue{Code: "shouting", Message: "Use sentence case", Target: operation.At("title")})
		}
		return operation.Keep[store.Value](), nil
	}
	articles := ridu.Collection{
		Slug: "articles",
		Fields: field.Fields{
			field.Group("seo", field.Fields{field.Text("title")}).Hooks(field.Hooks[store.Value]{
				BeforeChange: []field.Transform[store.Value]{noShouting},
			}),
			field.Checkbox("featured"),
		},
		Hooks: ridu.CollectionHooks{BeforeDelete: []ridu.Hook{keepFeatured}},
	}
	app, err := ridu.New(ridu.Config{Name: "Reject", Collections: []ridu.Collection{articles}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	collection := app.Manifest().Snapshot().Collections[0]
	featuredFieldID := collection.Fields[1].ID
	seoTitleFieldID := collection.Fields[0].Nested.ResolvedFields()[0].ID
	local := app.Local()

	featured, err := local.Create(t.Context(), "articles", store.Values{"featured": store.Boolean(true)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = local.Delete(t.Context(), "articles", featured.ID, ridu.MutationOptions{})
	var failure *ridu.OperationError
	if !errors.As(err, &failure) || failure.Code != "rejected" || failure.Status != 422 ||
		failure.Message != "Remove the article from the homepage first" {
		t.Fatalf("collection rejection = %#v (%v)", failure, err)
	}
	if len(failure.Issues) != 1 || failure.Issues[0].Path != "featured" || failure.Issues[0].Code != "featured" ||
		failure.Issues[0].Message != "Unfeature it" || failure.Issues[0].FieldID != featuredFieldID ||
		failure.Issues[0].CollectionID != collection.ID || failure.Issues[0].GlobalID != "" {
		t.Fatalf("collection rejection issues = %#v", failure.Issues)
	}
	if _, err := local.Find(t.Context(), "articles", featured.ID, ridu.FindOptions{}); err != nil {
		t.Fatalf("the rejected delete removed the article: %v", err)
	}

	_, err = local.Create(t.Context(), "articles", store.Values{"seo": store.Object(store.Values{"title": store.String("BUY NOW")})}, ridu.MutationOptions{})
	if !errors.As(err, &failure) || failure.Code != "rejected" || failure.Status != 422 || failure.Message != "Titles cannot be all capitals" {
		t.Fatalf("field rejection = %#v (%v)", failure, err)
	}
	if len(failure.Issues) != 1 || failure.Issues[0].Path != "seo.title" || failure.Issues[0].Code != "shouting" ||
		failure.Issues[0].Message != "Use sentence case" || failure.Issues[0].FieldID != seoTitleFieldID ||
		failure.Issues[0].CollectionID != collection.ID || failure.Issues[0].GlobalID != "" {
		t.Fatalf("field rejection issues = %#v", failure.Issues)
	}

	server := httptest.NewServer(app.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	request, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/collections/articles/"+featured.ID, nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope protocol.ErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 422 || envelope.Error.Status != 422 || envelope.Error.Code != protocol.ErrorRejected ||
		envelope.Error.Message != "Remove the article from the homepage first" || len(envelope.Error.Issues) != 1 {
		t.Fatalf("HTTP rejection = %d %#v", response.StatusCode, envelope.Error)
	}
	if issue := envelope.Error.Issues[0]; issue.Path != "featured" || issue.Code != "featured" || issue.Message != "Unfeature it" ||
		issue.FieldID != featuredFieldID || issue.CollectionID != collection.ID || issue.GlobalID != "" {
		t.Fatalf("HTTP rejection issue = %#v", issue)
	}
}

func TestOrdinaryHookErrorsStayInternal(t *testing.T) {
	failing := ridu.Collection{Slug: "notes", Fields: field.Fields{field.Text("title")}, Hooks: ridu.CollectionHooks{
		BeforeChange: []ridu.Hook{func(ridu.HookContext) error { return errors.New("database password is hunter2") }},
	}}
	app, err := ridu.New(ridu.Config{Name: "Internal", Collections: []ridu.Collection{failing}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	response, err := server.Client().Post(server.URL+"/api/collections/notes", "application/json", strings.NewReader(`{"title":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope protocol.ErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 500 || envelope.Error.Code != protocol.ErrorInternal || strings.Contains(envelope.Error.Message, "hunter2") {
		t.Fatalf("ordinary hook error leaked or changed: %d %#v", response.StatusCode, envelope.Error)
	}
}

func TestHookPhasesMatchTheOperationKind(t *testing.T) {
	var events []string
	record := func(name string) ridu.Hook {
		return func(ctx ridu.HookContext) error {
			events = append(events, name+":"+string(ctx.Operation))
			return nil
		}
	}
	var fieldIDs []string
	title := field.Text("title").Hooks(field.Hooks[string]{
		BeforeValidate: []field.RawTransform{func(ctx operation.Context, _ operation.Value[store.Value]) (operation.Change[store.Value], error) {
			events = append(events, "field BeforeValidate:"+string(ctx.Operation))
			return operation.Keep[store.Value](), nil
		}},
		AfterChange: []field.Observer[string]{func(ctx operation.Context, _ operation.Value[string]) error {
			fieldIDs = append(fieldIDs, "AfterChange="+string(ctx.ID))
			return nil
		}},
		AfterCommit: []field.Observer[string]{func(ctx operation.Context, _ operation.Value[string]) error {
			events = append(events, "field AfterCommit:"+string(ctx.Operation))
			fieldIDs = append(fieldIDs, "AfterCommit="+string(ctx.ID))
			return nil
		}},
	})
	notes := ridu.Collection{Slug: "notes", Trash: true, Fields: field.Fields{title}, Hooks: ridu.CollectionHooks{
		BeforeValidate: []ridu.Hook{record("BeforeValidate")},
		AfterCommit:    []ridu.Hook{record("AfterCommit")},
	}}
	app, err := ridu.New(ridu.Config{Name: "Phases", Collections: []ridu.Collection{notes}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	created, err := local.Create(t.Context(), "notes", store.Values{"title": store.String("One")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(fieldIDs, ",") != "AfterChange="+created.ID+",AfterCommit="+created.ID {
		t.Fatalf("field hooks on create saw IDs %v, want %s", fieldIDs, created.ID)
	}
	fieldIDs = nil
	copied, err := local.Duplicate(t.Context(), "notes", created.ID, nil, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(fieldIDs, ",") != "AfterChange="+copied.ID+",AfterCommit="+copied.ID {
		t.Fatalf("field hooks on duplicate saw IDs %v, want the copy %s", fieldIDs, copied.ID)
	}
	events = nil
	if _, err := local.Find(t.Context(), "notes", created.ID, ridu.FindOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := local.List(t.Context(), "notes", ridu.ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("reads ran %v; BeforeValidate and AfterCommit are for changes", events)
	}
	if _, err := local.Delete(t.Context(), "notes", created.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(events, ","); got != "field AfterCommit:delete,AfterCommit:delete" {
		t.Fatalf("delete ran %s; want AfterCommit without BeforeValidate", got)
	}
}
