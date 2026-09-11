package richtextblocks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

func parentLifecycle(t *testing.T, factory Factory) {
	seen := &observations{}
	_, app := factory(t, configuration(t, seen))
	ctx := t.Context()
	user, err := app.Local().Create(ctx, "users", store.Values{"email": store.String("publisher@example.test")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := app.Local().Create(ctx, "assets", store.Values{"title": store.String("Mountains"), "url": store.String("/mountains.svg")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := app.Local().Create(ctx, "pages", store.Values{"title": store.String("Release page"), "layout": store.List(
		store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("Hello")}),
		store.Object(store.Values{"blockType": store.String("content"), "title": store.String("Story"), "body": Document(paragraph("Portable content"))}),
		store.Object(store.Values{"blockType": store.String("media"), "asset": store.String(asset.ID), "caption": store.String("Mountains")}),
		store.Object(store.Values{"blockType": store.String("cta"), "label": store.String("Read more")}),
	)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := page.Values["layout"].CopyList()
	if len(rows) != 4 {
		t.Fatal("release layout truncated")
	}
	for i, row := range rows {
		value, _ := row.CopyObject()
		if stringValue(value["_key"]) == "" {
			t.Fatalf("layout %d missing identity", i)
		}
	}
	created, err := app.Local().Create(ctx, "articles", store.Values{"title": store.String("Release article"), "body": Document(
		paragraph("Opening paragraph"), Block("callout", "callout", store.Values{"title": store.String("First title"), "translation": store.String("Hello"), "detail": Document(Block("cta", "inner", store.Values{"label": store.String("Nested action"), "destination": store.String(page.ID)}))}),
		Block("media", "image", store.Values{"asset": store.String(asset.ID), "caption": store.String("Mountains")}),
		Block("cta", "action", store.Values{"label": store.String("Read page"), "destination": store.String(page.ID)}), paragraph("Closing paragraph"),
	)}, ridu.MutationOptions{Actor: &user})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != store.StatusDraft {
		t.Fatal("create was not draft")
	}
	if _, err := app.Local().Find(ctx, "articles", created.ID, ridu.FindOptions{}); err == nil {
		t.Fatal("draft publicly visible")
	}
	identity := &ridu.AuthIdentity{Collection: "users", Actor: user}
	token, err := app.CreateCollectionPreviewToken(ctx, "articles", created.ID, identity)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := app.FindCollectionPreview(ctx, token.Token, "articles", created.ID)
	if err != nil || stringValue(payload(t, preview.Values["body"], 1)["title"]) != "First title" {
		t.Fatalf("draft preview: %v", err)
	}
	published, err := app.Local().Publish(ctx, "articles", created.ID, ridu.MutationOptions{Actor: &user, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != store.StatusPublished {
		t.Fatal("publish lost status")
	}
	changed, err := app.Local().PublishChanges(ctx, "articles", created.ID, store.Values{"body": Document(Block("callout", "callout", store.Values{"title": store.String("Changed title")}))}, ridu.MutationOptions{Actor: &user, ExpectedRevision: published.Revision})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := app.Local().RestoreAsDraft(ctx, "articles", created.ID, published.Revision, ridu.MutationOptions{Actor: &user, ExpectedRevision: changed.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != store.StatusDraft || stringValue(payload(t, restored.Values["body"], 1)["title"]) != "First title" || stringValue(payload(t, restored.Values["body"], 1)["_key"]) != "callout" {
		t.Fatal("restore lost payload, identity or status")
	}
	duplicate, err := app.Local().Duplicate(ctx, "articles", created.ID, store.Values{"title": store.String("Copy")}, ridu.MutationOptions{Actor: &user})
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(payload(t, duplicate.Values["body"], 1)["_key"]) == "callout" || stringValue(payload(t, payload(t, duplicate.Values["body"], 1)["detail"], 0)["_key"]) == "inner" {
		t.Fatal("duplicate reused nested occurrence identity")
	}
	if stringValue(payload(t, payload(t, duplicate.Values["body"], 1)["detail"], 0)["label"]) != "Nested action" {
		t.Fatal("duplicate lost nested editor")
	}
	if _, err := app.SchedulePublish(ctx, "articles", created.ID, time.Now().Add(-time.Second), restored.Revision, identity); err != nil {
		t.Fatal(err)
	}
	if completed, err := app.RunScheduledPublishes(ctx, 10, &user); err != nil || completed != 1 {
		t.Fatalf("scheduled publish: %d %v", completed, err)
	}
	public, err := app.Local().Find(ctx, "articles", created.ID, ridu.FindOptions{})
	if err != nil || public.Status != store.StatusPublished {
		t.Fatalf("scheduled public read: %v", err)
	}
	if stringValue(payload(t, public.Values["body"], 1)["_key"]) != "callout" {
		t.Fatal("schedule changed identity")
	}
	// Ordinary HTTP serialization is the SDK transport. This checks the exact
	// same populated aggregate returned locally, without an editor dependency.
	server := httptest.NewServer(app.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/collections/articles/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("REST read status %d", response.StatusCode)
	}
	var wire struct {
		Doc map[string]json.RawMessage `json:"doc"`
	}
	if err := json.NewDecoder(response.Body).Decode(&wire); err != nil {
		t.Fatal(err)
	}
	var body store.Value
	if err := json.Unmarshal(wire.Doc["body"], &body); err != nil {
		t.Fatalf("REST body: %v", err)
	}
	if stringValue(payload(t, body, 1)["_key"]) != "callout" {
		t.Fatal("REST changed identity")
	}
	versions, err := app.Local().Versions(ctx, "articles", created.ID, ridu.FindOptions{Actor: &user})
	if err != nil || len(versions) != 5 {
		t.Fatalf("create/publish/edit/restore/schedule version count: %d %v", len(versions), err)
	}
	if len(seen.occurrences) != 6 {
		t.Fatalf("parent create/publish/edit/restore/duplicate/schedule hook dispatches: %v", seen.occurrences)
	}
}
