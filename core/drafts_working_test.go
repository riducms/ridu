package core_test

import (
	"strings"
	"testing"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestEditorialDraftRetainsLiveHeadUntilValidPublication(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "working drafts", Collections: []ridu.Collection{{
		Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{
			field.Text("title").Required().MinLength(3),
			field.Group("meta", field.Fields{field.Text("summary").Required()}),
			field.Array("sections", field.Fields{field.Text("heading").Required()}).MinRows(1),
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	ctx := t.Context()
	published := false
	draft := true
	live, err := local.Create(ctx, "pages", store.Values{
		"title":    store.String("Live A"),
		"meta":     store.Object(store.Values{"summary": store.String("Complete")}),
		"sections": store.List(store.Object(store.Values{"heading": store.String("Intro")})),
	}, ridu.MutationOptions{Draft: &published})
	if err != nil {
		t.Fatal(err)
	}
	working, err := local.Update(ctx, "pages", live.ID, store.Values{"title": store.Null(), "sections": store.List()}, ridu.MutationOptions{Draft: &draft, ExpectedRevision: live.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if working.Status != store.StatusPublished || !working.HasDraftChanges || working.PublishedRevision != live.Revision {
		t.Fatalf("working metadata = %#v", working)
	}
	public, err := local.Find(ctx, "pages", live.ID, ridu.FindOptions{Draft: &published})
	if err != nil || stringValue(public.Values["title"]) != "Live A" || public.Revision != live.Revision {
		t.Fatalf("live head changed during draft save: %#v, %v", public, err)
	}
	if _, err := local.Publish(ctx, "pages", live.ID, ridu.MutationOptions{ExpectedRevision: working.Revision}); !operationCode(err, "validation") {
		t.Fatalf("incomplete publication = %v, want validation", err)
	}
	if _, err := local.Update(ctx, "pages", live.ID, store.Values{"title": store.String("Stale")}, ridu.MutationOptions{Draft: &draft, ExpectedRevision: live.Revision}); !operationCode(err, "conflict") {
		t.Fatalf("stale draft save = %v, want conflict", err)
	}
	working, err = local.Update(ctx, "pages", live.ID, store.Values{
		"title":    store.String("Live B"),
		"sections": store.List(store.Object(store.Values{"heading": store.String("Updated")})),
	}, ridu.MutationOptions{Draft: &draft, ExpectedRevision: working.Revision})
	if err != nil {
		t.Fatal(err)
	}
	liveB, err := local.Publish(ctx, "pages", live.ID, ridu.MutationOptions{ExpectedRevision: working.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if liveB.HasDraftChanges || liveB.PublishedRevision != liveB.Revision || stringValue(liveB.Values["title"]) != "Live B" {
		t.Fatalf("promoted working head = %#v", liveB)
	}
	working, err = local.RestoreAsDraft(ctx, "pages", live.ID, live.Revision, ridu.MutationOptions{ExpectedRevision: liveB.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if working.Status != store.StatusPublished || !working.HasDraftChanges || stringValue(working.Values["title"]) != "Live A" {
		t.Fatalf("restored working snapshot = %#v", working)
	}
	public, err = local.Find(ctx, "pages", live.ID, ridu.FindOptions{Draft: &published})
	if err != nil || stringValue(public.Values["title"]) != "Live B" {
		t.Fatalf("restore-as-draft changed live head: %#v, %v", public, err)
	}
	reset, err := local.DiscardDraft(ctx, "pages", live.ID, ridu.MutationOptions{ExpectedRevision: working.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if reset.HasDraftChanges || stringValue(reset.Values["title"]) != "Live B" || reset.Revision <= working.Revision {
		t.Fatalf("discard failed to reset working head: %#v", reset)
	}
	if _, err := local.DiscardDraft(ctx, "pages", live.ID, ridu.MutationOptions{ExpectedRevision: reset.Revision}); !operationCode(err, "conflict") {
		t.Fatalf("second discard = %v, want conflict", err)
	}
	unpublished, err := local.Unpublish(ctx, "pages", live.ID, ridu.MutationOptions{ExpectedRevision: reset.Revision})
	if err != nil || unpublished.Status != store.StatusDraft || unpublished.PublishedRevision != 0 {
		t.Fatalf("unpublish did not remove live head: %#v, %v", unpublished, err)
	}
	if _, err := local.Find(ctx, "pages", live.ID, ridu.FindOptions{Draft: &published}); !operationCode(err, "not_found") {
		t.Fatalf("public read after unpublish = %v, want not_found", err)
	}
	republished, err := local.Publish(ctx, "pages", live.ID, ridu.MutationOptions{ExpectedRevision: unpublished.Revision})
	if err != nil || republished.Status != store.StatusPublished || republished.HasDraftChanges {
		t.Fatalf("republish after unpublish = %#v, %v", republished, err)
	}
}

func TestUploadDraftKeepsFileStrictAndStagesEditorialMetadata(t *testing.T) {
	objects, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := ridu.New(ridu.Config{Name: "upload drafts", Storage: objects, StorageNamespace: "upload-drafts", Collections: []ridu.Collection{{
		Slug: "media", Upload: true, UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("alt").Required()},
		Access: ridu.CollectionAccess{ReadDrafts: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	created, err := app.Upload(t.Context(), "media", ridu.UploadInput{Filename: "note.txt", Reader: strings.NewReader("file bytes"), Draft: &draft})
	if err != nil || created.Status != store.StatusDraft {
		t.Fatalf("file-bearing incomplete draft = %#v, %v", created, err)
	}
	duplicated, err := app.Duplicate(t.Context(), "media", created.ID, nil, ridu.MutationOptions{ExpectedRevision: created.Revision})
	if err != nil || duplicated.Status != store.StatusDraft {
		t.Fatalf("incomplete upload draft duplicate = %#v, %v", duplicated, err)
	}
	if _, err := app.Local().Publish(t.Context(), "media", created.ID, ridu.MutationOptions{ExpectedRevision: created.Revision}); !operationCode(err, "validation") {
		t.Fatalf("publish without alt = %v, want validation", err)
	}
	updated, err := app.UpdateUpload(t.Context(), "media", created.ID, ridu.UpdateUploadInput{Draft: &draft, Data: store.Values{"alt": store.String("A note")}, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	published, err := app.Local().Publish(t.Context(), "media", created.ID, ridu.MutationOptions{ExpectedRevision: updated.Revision})
	if err != nil || published.Status != store.StatusPublished {
		t.Fatalf("publish complete upload = %#v, %v", published, err)
	}
}

func TestAuthDraftNeverDefersIdentity(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "auth draft identity", Admin: ridu.AdminConfig{User: "users"}, Collections: []ridu.Collection{{
		Slug: "users", Auth: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Email("email").Required().Unique(), field.Text("bio").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	if _, err := app.Local().Create(t.Context(), "users", store.Values{}, ridu.MutationOptions{Draft: &draft}); !operationCode(err, "validation") {
		t.Fatalf("missing auth identity in draft = %v, want validation", err)
	}
	if _, err := app.Local().Create(t.Context(), "users", store.Values{"email": store.String("editor@example.test")}, ridu.MutationOptions{Draft: &draft}); err != nil {
		t.Fatalf("optional editorial bio should be deferrable: %v", err)
	}
}

func TestDiscardDraftRequiresFieldUpdateAccessForRevertedValues(t *testing.T) {
	allowTitle := true
	app, err := ridu.New(ridu.Config{Name: "discard admission", Collections: []ridu.Collection{{
		Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required().Access(field.Access{Update: func(operation.Context) (bool, error) {
			return allowTitle, nil
		}})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft, published := true, false
	live, err := app.Local().Create(t.Context(), "pages", store.Values{"title": store.String("Live")}, ridu.MutationOptions{Draft: &published})
	if err != nil {
		t.Fatal(err)
	}
	working, err := app.Local().Update(t.Context(), "pages", live.ID, store.Values{"title": store.String("Working")}, ridu.MutationOptions{Draft: &draft, ExpectedRevision: live.Revision})
	if err != nil {
		t.Fatal(err)
	}
	allowTitle = false
	if _, err := app.Local().DiscardDraft(t.Context(), "pages", live.ID, ridu.MutationOptions{ExpectedRevision: working.Revision}); !operationCode(err, "field_access_denied") {
		t.Fatalf("protected discard = %v, want field_access_denied", err)
	}
	allowTitle = true
	reset, err := app.Local().DiscardDraft(t.Context(), "pages", live.ID, ridu.MutationOptions{ExpectedRevision: working.Revision})
	if err != nil || stringValue(reset.Values["title"]) != "Live" {
		t.Fatalf("authorized discard = %#v, %v", reset, err)
	}
}

func TestEditorialDraftDefersOnlyCompletionAndExposesWritePhase(t *testing.T) {
	var seen []operation.WritePhase
	app, err := ridu.New(ridu.Config{Name: "draft validation", Collections: []ridu.Collection{{
		Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{
			field.Text("title").Required().MinLength(3).Validate(func(ctx operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
				seen = append(seen, ctx.WritePhase)
				return nil, nil
			}),
			field.Email("contact"),
			field.Group("meta", field.Fields{field.Text("summary").Required()}),
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	_, err = app.Local().Create(t.Context(), "pages", store.Values{"title": store.String("x"), "meta": store.Object(store.Values{})}, ridu.MutationOptions{Draft: &draft})
	if err != nil {
		t.Fatalf("partial nested draft was rejected: %v", err)
	}
	if len(seen) == 0 || seen[0] != operation.WritePhaseDraft {
		t.Fatalf("custom validation phase = %#v", seen)
	}
	if _, err := app.Local().Create(t.Context(), "pages", store.Values{"contact": store.String("invalid")}, ridu.MutationOptions{Draft: &draft}); !operationCode(err, "validation") {
		t.Fatalf("invalid present email = %v, want validation", err)
	}
}
