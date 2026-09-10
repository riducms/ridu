package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestCallerSuppliedDocumentIDsAreOptInAndUseTheCreatePipeline(t *testing.T) {
	disabled, err := ridu.New(callerIDConfig(false, nil), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabled.Local().Create(context.Background(), "posts", store.Values{
		"id": store.String("payload-42"), "title": store.String("Disabled"),
	}, nil); !operationIssue(err, "invalid_document_id", "id") {
		t.Fatalf("disabled caller ID error = %v", err)
	}
	imported, err := disabled.Local().Import(context.Background(), "posts", store.Values{
		"title": store.String("Imported numeric ID"),
	}, ridu.ImportOptions{ID: "9007199254740993", Status: store.StatusPublished}, nil)
	if err != nil || imported.ID != "9007199254740993" {
		t.Fatalf("migration import = %#v, %v", imported, err)
	}

	var hookSawMetadata bool
	enabled, err := ridu.New(callerIDConfig(true, func(ctx ridu.HookContext) error {
		_, hookSawMetadata = ctx.Data["id"]
		return nil
	}), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := enabled.Local().Create(context.Background(), "posts", store.Values{
		"id": store.String("payload-custom/key"), "title": store.String("Caller owned"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "payload-custom/key" || hookSawMetadata {
		t.Fatalf("created = %#v, hook saw id field = %t", created, hookSawMetadata)
	}

	createdWithOptions, err := enabled.Local().CreateWithOptions(context.Background(), "posts", store.Values{
		"title": store.String("Options ID"),
	}, ridu.MutationOptions{ID: "payload-options-id"})
	if err != nil || createdWithOptions.ID != "payload-options-id" {
		t.Fatalf("options create = %#v, %v", createdWithOptions, err)
	}
	if _, err := enabled.Local().Create(context.Background(), "posts", store.Values{
		"id": store.String(created.ID), "title": store.String("Collision"),
	}, nil); !operationCode(err, "conflict") {
		t.Fatalf("duplicate caller ID error = %v", err)
	}
	if _, err := enabled.Local().CreateWithOptions(context.Background(), "posts", store.Values{
		"title": store.String("Unsafe"),
	}, ridu.MutationOptions{ID: "bad\x00id"}); !operationIssue(err, "invalid_document_id", "id") {
		t.Fatalf("unsafe caller ID error = %v", err)
	}
	if _, err := enabled.Local().Create(context.Background(), "posts", store.Values{
		"id": store.String(""), "title": store.String("Empty"),
	}, nil); !operationIssue(err, "invalid_document_id", "id") {
		t.Fatalf("empty caller ID error = %v", err)
	}
	maximumID := strings.Repeat("x", store.MaxDocumentIDBytes)
	if document, err := enabled.Local().Create(context.Background(), "posts", store.Values{
		"id": store.String(maximumID), "title": store.String("Maximum"),
	}, nil); err != nil || document.ID != maximumID {
		t.Fatalf("maximum caller ID = %#v, %v", document, err)
	}
	tooLongID := maximumID + "x"
	if _, err := enabled.Local().Create(context.Background(), "posts", store.Values{
		"id": store.String(tooLongID), "title": store.String("Too long"),
	}, nil); !operationIssue(err, "invalid_document_id", "id") {
		t.Fatalf("oversized caller ID error = %v", err)
	}
	if _, err := disabled.Local().Import(context.Background(), "posts", store.Values{
		"title": store.String("Oversized import"),
	}, ridu.ImportOptions{ID: tooLongID, Status: store.StatusPublished}, nil); !operationIssue(err, "invalid_document_id", "id") {
		t.Fatalf("oversized import ID error = %v", err)
	}
}

func TestRESTCreateAcceptsOnlyConfiguredStringDocumentIDs(t *testing.T) {
	application, err := ridu.New(callerIDConfig(true, nil), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	source, err := application.Local().Create(context.Background(), "posts", store.Values{
		"id": store.String("payload-rest-source"), "title": store.String("Source"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	created := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts", strings.NewReader(`{"id":"payload-rest-id","title":"REST"}`), "")
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d: %s", created.StatusCode, readBody(t, created))
	}
	var envelope struct {
		Doc struct {
			ID string `json:"id"`
		} `json:"doc"`
	}
	if err := json.NewDecoder(created.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Doc.ID != "payload-rest-id" {
		t.Fatalf("REST ID = %q", envelope.Doc.ID)
	}

	numeric := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts", strings.NewReader(`{"id":42,"title":"Numeric wire ID"}`), "")
	defer numeric.Body.Close()
	if numeric.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("numeric ID status = %d: %s", numeric.StatusCode, readBody(t, numeric))
	}

	empty := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts", strings.NewReader(`{"id":"","title":"Empty wire ID"}`), "")
	defer empty.Body.Close()
	if empty.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("empty ID status = %d: %s", empty.StatusCode, readBody(t, empty))
	}

	duplicate := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts/"+source.ID+"/duplicate", strings.NewReader(`{"id":"copy-id","title":"Copy"}`), "")
	defer duplicate.Body.Close()
	if duplicate.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate destination ID status = %d: %s", duplicate.StatusCode, readBody(t, duplicate))
	}

	slashID := "payload-rest/path"
	slashCreated := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts", strings.NewReader(`{"id":"`+slashID+`","title":"Slash"}`), "")
	if slashCreated.StatusCode != http.StatusCreated {
		defer slashCreated.Body.Close()
		t.Fatalf("slash ID create status = %d: %s", slashCreated.StatusCode, readBody(t, slashCreated))
	}
	slashCreated.Body.Close()
	encodedPath := "http://ridu.test/api/collections/posts/payload-rest%2Fpath"
	for _, request := range []struct {
		method string
		body   string
	}{
		{method: http.MethodGet},
		{method: http.MethodPatch, body: `{"title":"Updated"}`},
		{method: http.MethodDelete},
	} {
		response := requestJSON(t, client, request.method, encodedPath, strings.NewReader(request.body), "")
		if response.StatusCode != http.StatusOK {
			defer response.Body.Close()
			t.Fatalf("%s slash ID status = %d: %s", request.method, response.StatusCode, readBody(t, response))
		}
		response.Body.Close()
	}

}

func TestMaximumCallerIDRemainsReachableByScheduledPublishing(t *testing.T) {
	ctx := context.Background()
	application, err := ridu.New(ridu.Config{
		Name: "Caller ID scheduled publishing", AllowIDOnCreate: true, Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
			{
				Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields: field.Fields{field.Text("title").Required()},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("p", store.MaxDocumentIDBytes)
	publisher, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("publisher@example.test")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := &ridu.AuthIdentity{Collection: "users", Actor: publisher}
	document, err := application.Local().Create(ctx, "posts", store.Values{
		"id": store.String(id), "title": store.String("Scheduled"),
	}, &publisher)
	if err != nil || document.ID != id {
		t.Fatalf("maximum-ID draft = %#v, %v", document, err)
	}
	job, err := application.SchedulePublish(ctx, "posts", id, time.Now().Add(time.Hour), document.Revision, identity)
	if err != nil || job.DocumentID != id {
		t.Fatalf("maximum-ID schedule = %#v, %v", job, err)
	}
	jobs, err := application.ScheduledPublishes(ctx, "posts", id, identity)
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("maximum-ID scheduled list = %#v, %v", jobs, err)
	}
	if err := application.CancelScheduledPublish(ctx, "posts", id, job.ID, identity); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationImportRequiresAnExplicitDocumentID(t *testing.T) {
	application, err := ridu.New(callerIDConfig(false, nil), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Import(context.Background(), "posts", store.Values{
		"title": store.String("Missing identity"),
	}, ridu.ImportOptions{}, nil); !operationIssue(err, "invalid_document_id", "id") {
		t.Fatalf("empty import ID error = %v", err)
	}
	page, err := application.Local().List(context.Background(), "posts", ridu.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("empty import ID wrote %d documents", page.Total)
	}
}

func callerIDConfig(allow bool, beforeValidate ridu.Hook) ridu.Config {
	var hooks ridu.CollectionHooks
	if beforeValidate != nil {
		hooks.BeforeValidate = []ridu.Hook{beforeValidate}
	}
	return ridu.Config{
		Name: "Caller IDs", AllowIDOnCreate: allow,
		Collections: []ridu.Collection{{
			Slug: "posts", Hooks: hooks,
			Fields: field.Fields{field.Text("title").Required()},
		}},
	}
}

func operationIssue(err error, code, path string) bool {
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) {
		return false
	}
	for _, issue := range operationError.Issues {
		if issue.Code == code && issue.Path == path {
			return true
		}
	}
	return false
}
