package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestDraftPublishingConflictsAndRestore(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{Name: "versions", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10},
		Fields:        field.Fields{field.Text("title").Required()},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	draft, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("First")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != store.StatusDraft || draft.Revision != 1 {
		t.Fatalf("created metadata = %s/%d", draft.Status, draft.Revision)
	}
	if _, err := application.Local().Find(ctx, "posts", draft.ID, ridu.FindOptions{}); err == nil {
		t.Fatal("public read exposed a draft")
	}
	actor := &store.Document{ID: "editor"}
	published, err := application.Local().Publish(ctx, "posts", draft.ID, ridu.MutationOptions{Actor: actor, ExpectedRevision: draft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != store.StatusPublished || published.Revision != 2 {
		t.Fatalf("published metadata = %s/%d", published.Status, published.Revision)
	}
	if _, err := application.Local().Update(ctx, "posts", draft.ID, store.Values{"title": store.String("bypass")}, ridu.MutationOptions{Actor: actor, ExpectedRevision: published.Revision}); !operationCode(err, "publish_required") || !strings.Contains(err.Error(), "PublishChanges") || !strings.Contains(err.Error(), "/api/collections/posts/{id}/publish") {
		t.Fatalf("ordinary published update error = %v, want publish_required naming PublishChanges and its route", err)
	}
	unchanged, err := application.Local().Find(ctx, "posts", draft.ID, ridu.FindOptions{Actor: actor})
	if err != nil || stringValue(unchanged.Values["title"]) != "First" || unchanged.Revision != published.Revision {
		t.Fatalf("rejected ordinary update changed live content: %#v, %v", unchanged, err)
	}
	updated, err := application.Local().PublishChanges(ctx, "posts", draft.ID, store.Values{"title": store.String("Second")}, ridu.MutationOptions{Actor: actor, ExpectedRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := application.Local().Restore(ctx, "posts", draft.ID, 2, ridu.MutationOptions{Actor: actor, ExpectedRevision: updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	title, _ := restored.Values["title"].StringValue()
	if title != "First" || restored.Revision != 4 || restored.Status != store.StatusPublished {
		t.Fatalf("restored = %q revision %d", title, restored.Revision)
	}
	restoredDraft, err := application.Local().RestoreAsDraft(ctx, "posts", draft.ID, 2, ridu.MutationOptions{Actor: actor, ExpectedRevision: restored.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if restoredDraft.Status != store.StatusPublished || !restoredDraft.HasDraftChanges || restoredDraft.Revision != 5 {
		t.Fatalf("restored as draft = %#v", restoredDraft)
	}
	restoredHistoricalDraft, err := application.Local().Restore(ctx, "posts", draft.ID, 1, ridu.MutationOptions{Actor: actor, ExpectedRevision: restoredDraft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := restoredHistoricalDraft.Values["title"].StringValue(); title != "First" || restoredHistoricalDraft.Status != store.StatusDraft || restoredHistoricalDraft.PublishedRevision != 0 || restoredHistoricalDraft.Revision != 6 {
		t.Fatalf("restored historical draft = %#v", restoredHistoricalDraft)
	}
	live := false
	if _, err := application.Local().Find(ctx, "posts", draft.ID, ridu.FindOptions{Draft: &live}); !operationCode(err, "not_found") {
		t.Fatalf("restoring a historical draft left public content: %v", err)
	}
	versions, err := application.Local().Versions(ctx, "posts", draft.ID, ridu.FindOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 6 {
		t.Fatalf("versions = %d", len(versions))
	}
}

func TestRestoreIncompleteHistoricalDraftRespectsPublicationIntent(t *testing.T) {
	actor := &store.Document{ID: "editor"}
	application, err := ridu.New(ridu.Config{Name: "incomplete historical restore", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
		Access: ridu.CollectionAccess{ReadDrafts: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
			if ctx.Actor != nil && ctx.Actor.ID == actor.ID {
				return ridu.Allow(), nil
			}
			return ridu.Deny(), nil
		}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	local := application.Local()
	incomplete, err := local.Create(ctx, "posts", store.Values{}, ridu.MutationOptions{Actor: actor})
	if err != nil || incomplete.Status != store.StatusDraft {
		t.Fatalf("incomplete historical draft = %#v, %v", incomplete, err)
	}
	complete, err := local.Update(ctx, "posts", incomplete.ID, store.Values{"title": store.String("Public title")}, ridu.MutationOptions{Actor: actor, ExpectedRevision: incomplete.Revision})
	if err != nil {
		t.Fatal(err)
	}
	published, err := local.Publish(ctx, "posts", incomplete.ID, ridu.MutationOptions{Actor: actor, ExpectedRevision: complete.Revision})
	if err != nil {
		t.Fatal(err)
	}
	draft, live := true, false
	staged, err := local.RestoreAsDraft(ctx, "posts", incomplete.ID, incomplete.Revision, ridu.MutationOptions{Actor: actor, ExpectedRevision: published.Revision})
	if err != nil || staged.Status != store.StatusPublished || !staged.HasDraftChanges || staged.PublishedRevision != published.Revision {
		t.Fatalf("incomplete snapshot was not staged beside live: %#v, %v", staged, err)
	}
	working, err := local.Find(ctx, "posts", incomplete.ID, ridu.FindOptions{Actor: actor, Draft: &draft})
	if err != nil || working.Revision != staged.Revision {
		t.Fatalf("authorized working read = %#v, %v", working, err)
	}
	if _, exists := working.Values["title"]; exists {
		t.Fatalf("working snapshot unexpectedly acquired a title: %#v", working.Values)
	}
	if _, err := local.Find(ctx, "posts", incomplete.ID, ridu.FindOptions{Draft: &draft}); !operationCode(err, "access_denied") {
		t.Fatalf("anonymous explicit draft read = %v, want access_denied", err)
	}
	public, err := local.Find(ctx, "posts", incomplete.ID, ridu.FindOptions{Draft: &live})
	if err != nil || stringValue(public.Values["title"]) != "Public title" || public.Revision != published.Revision {
		t.Fatalf("restore-as-draft changed live snapshot: %#v, %v", public, err)
	}
	if _, err := local.Restore(ctx, "posts", incomplete.ID, incomplete.Revision, ridu.MutationOptions{Actor: actor, ExpectedRevision: published.Revision}); !operationCode(err, "conflict") {
		t.Fatalf("stale restore = %v, want conflict", err)
	}
	restored, err := local.Restore(ctx, "posts", incomplete.ID, incomplete.Revision, ridu.MutationOptions{Actor: actor, ExpectedRevision: staged.Revision})
	if err != nil || restored.Status != store.StatusDraft || restored.PublishedRevision != 0 || restored.Revision <= staged.Revision {
		t.Fatalf("normal restore of incomplete draft = %#v, %v", restored, err)
	}
	if _, err := local.Find(ctx, "posts", incomplete.ID, ridu.FindOptions{Draft: &live}); !operationCode(err, "not_found") {
		t.Fatalf("historical draft restore retained public snapshot: %v", err)
	}
	working, err = local.Find(ctx, "posts", incomplete.ID, ridu.FindOptions{Actor: actor, Draft: &draft})
	if err != nil || working.Revision != restored.Revision || working.Status != store.StatusDraft {
		t.Fatalf("working draft after unpublishing restore = %#v, %v", working, err)
	}
}

func TestRestoreHistoricalDraftIntoAlreadyUnpublishedDocument(t *testing.T) {
	allowUpdate, allowUnpublish := true, false
	application, err := ridu.New(ridu.Config{Name: "restore unpublished working draft", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
		Access: ridu.CollectionAccess{
			ReadDrafts: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
			Update: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				if allowUpdate {
					return ridu.Allow(), nil
				}
				return ridu.Deny(), nil
			},
			Unpublish: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				if allowUnpublish {
					return ridu.Allow(), nil
				}
				return ridu.Deny(), nil
			},
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local, ctx := application.Local(), t.Context()
	draft, err := local.Create(ctx, "posts", store.Values{"title": store.String("First draft")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := local.Update(ctx, "posts", draft.ID, store.Values{"title": store.String("Second draft")}, ridu.MutationOptions{ExpectedRevision: draft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Restore(ctx, "posts", draft.ID, draft.Revision, ridu.MutationOptions{ExpectedRevision: draft.Revision}); !operationCode(err, "conflict") {
		t.Fatalf("stale never-published restore = %v, want conflict", err)
	}
	restored, err := local.Restore(ctx, "posts", draft.ID, draft.Revision, ridu.MutationOptions{ExpectedRevision: updated.Revision})
	if err != nil || restored.Status != store.StatusDraft || restored.PublishedRevision != 0 || restored.Revision != updated.Revision+1 || stringValue(restored.Values["title"]) != "First draft" {
		t.Fatalf("never-published draft-to-draft restore = %#v, %v", restored, err)
	}
	live := false
	if _, err := local.Find(ctx, "posts", draft.ID, ridu.FindOptions{Draft: &live}); !operationCode(err, "not_found") {
		t.Fatalf("never-published restore created a live head: %v", err)
	}
	allowUpdate = false
	if _, err := local.Restore(ctx, "posts", draft.ID, updated.Revision, ridu.MutationOptions{ExpectedRevision: restored.Revision}); !operationCode(err, "access_denied") {
		t.Fatalf("draft-to-draft restore without Update access = %v, want access_denied", err)
	}
	allowUpdate = true
	published, err := local.Publish(ctx, "posts", draft.ID, ridu.MutationOptions{ExpectedRevision: restored.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Restore(ctx, "posts", draft.ID, updated.Revision, ridu.MutationOptions{ExpectedRevision: published.Revision}); !operationCode(err, "access_denied") {
		t.Fatalf("live historical-draft restore bypassed Unpublish access: %v", err)
	}
	allowUnpublish = true
	unpublished, err := local.Unpublish(ctx, "posts", draft.ID, ridu.MutationOptions{ExpectedRevision: published.Revision})
	if err != nil {
		t.Fatal(err)
	}
	allowUnpublish = false
	if _, err := local.Restore(ctx, "posts", draft.ID, updated.Revision, ridu.MutationOptions{ExpectedRevision: published.Revision}); !operationCode(err, "conflict") {
		t.Fatalf("stale post-unpublish restore = %v, want conflict", err)
	}
	restored, err = local.Restore(ctx, "posts", draft.ID, updated.Revision, ridu.MutationOptions{ExpectedRevision: unpublished.Revision})
	if err != nil || restored.Status != store.StatusDraft || restored.PublishedRevision != 0 || restored.Revision != unpublished.Revision+1 || stringValue(restored.Values["title"]) != "Second draft" {
		t.Fatalf("post-unpublish draft-to-draft restore = %#v, %v", restored, err)
	}
	if _, err := local.Find(ctx, "posts", draft.ID, ridu.FindOptions{Draft: &live}); !operationCode(err, "not_found") {
		t.Fatalf("post-unpublish restore recreated a live head: %v", err)
	}
	versions, err := local.Versions(ctx, "posts", draft.ID, ridu.FindOptions{})
	if err != nil || len(versions) != 6 || versions[len(versions)-1].Status != store.StatusDraft {
		t.Fatalf("failed restores wrote versions or final draft was not retained: %#v, %v", versions, err)
	}
}

func TestRestoreExactlyReplacesValuesAfterAdditiveSchemaChange(t *testing.T) {
	backend := teststore.New()
	restoring := false
	var restoreHooks, restoreFieldAccess, restoreReferenceReads int
	var hookSawSnapshot, accessSawSnapshot bool
	var restoreHookData store.Values

	config := func(additive bool) ridu.Config {
		details := field.Fields{field.Text("headline")}
		postFields := field.Fields{field.Text("title").Required(), field.Relationship("author", "authors").Required()}
		if additive {
			details = append(details, field.Text("summary"))
			postFields = append(postFields, field.Text("summary").Access(field.Access{Update: func(operation.Context) (bool, error) {
				if restoring {
					restoreFieldAccess++
				}
				return true, nil
			}}))
		}
		postFields = append(postFields, field.Group("details", details).Localized())
		post := ridu.Collection{
			Slug: "posts", Versions: true, Fields: postFields,
			Access: ridu.CollectionAccess{Publish: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				if restoring {
					_, hasSummary := ctx.Data["summary"]
					accessSawSnapshot = accessSawSnapshot || !hasSummary
				}
				return ridu.Allow(), nil
			}},
			Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
				if !restoring {
					return nil
				}
				restoreHooks++
				restoreHookData = store.CloneValues(ctx.Data)
				_, hasSummary := ctx.Data["summary"]
				details, _ := ctx.Data["details"].CopyObject()
				_, hasNestedSummary := details["summary"]
				hookSawSnapshot = !hasSummary && !hasNestedSummary
				return nil
			}}},
		}

		return ridu.Config{
			Name: "Exact version replacement",
			Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
				{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
			}},
			Collections: []ridu.Collection{
				{
					Slug: "authors", Fields: field.Fields{field.Text("name").Required()},
					Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
						if restoring {
							restoreReferenceReads++
						}
						return ridu.Allow(), nil
					}},
				},
				post,
			},
		}
	}

	initial, err := ridu.New(config(false), backend)
	if err != nil {
		t.Fatal(err)
	}
	author, err := initial.Local().Create(t.Context(), "authors", store.Values{"name": store.String("Author")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	document, err := initial.Local().Create(t.Context(), "posts", store.Values{
		"title": store.String("Snapshot title"), "author": store.String(author.ID),
		"details": store.Object(store.Values{"headline": store.String("English snapshot")}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	document, err = initial.Local().PublishChanges(t.Context(), "posts", document.ID, store.Values{
		"details": store.Object(store.Values{"headline": store.String("French snapshot")}),
	}, ridu.MutationOptions{ExpectedRevision: document.Revision, Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	snapshotRevision := document.Revision

	expanded, err := ridu.New(config(true), backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err = expanded.Local().PublishChanges(t.Context(), "posts", document.ID, store.Values{
		"summary": store.String("Current summary"),
		"details": store.Object(store.Values{
			"headline": store.String("English current"), "summary": store.String("English current summary"),
		}),
	}, ridu.MutationOptions{ExpectedRevision: document.Revision, Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	document, err = expanded.Local().PublishChanges(t.Context(), "posts", document.ID, store.Values{
		"details": store.Object(store.Values{
			"headline": store.String("French current"), "summary": store.String("French current summary"),
		}),
	}, ridu.MutationOptions{ExpectedRevision: document.Revision, Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	document, err = expanded.Local().PublishChanges(t.Context(), "posts", document.ID, store.Values{
		"title": store.String("Ordinary patch"),
	}, ridu.MutationOptions{ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	beforeRestore, err := expanded.Local().Find(t.Context(), "posts", document.ID, ridu.FindOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(beforeRestore.Values["summary"]) != "Current summary" {
		t.Fatalf("ordinary patch removed omitted top-level value: %#v", beforeRestore.Values)
	}
	currentDetails, _ := beforeRestore.Values["details"].CopyObject()
	englishCurrent, _ := currentDetails["en"].CopyObject()
	frenchCurrent, _ := currentDetails["fr"].CopyObject()
	if stringValue(englishCurrent["summary"]) != "English current summary" || stringValue(frenchCurrent["summary"]) != "French current summary" {
		t.Fatalf("ordinary patch removed omitted localized nested values: %#v", currentDetails)
	}

	restoring = true
	restored, err := expanded.Local().Restore(t.Context(), "posts", document.ID, snapshotRevision, ridu.MutationOptions{ExpectedRevision: document.Revision})
	restoring = false
	if err != nil {
		t.Fatal(err)
	}
	if restoreHooks != 1 || !hookSawSnapshot {
		t.Fatalf("restore hook observations = calls %d snapshot %v data %#v", restoreHooks, hookSawSnapshot, restoreHookData)
	}
	if restoreFieldAccess == 0 || !accessSawSnapshot {
		t.Fatalf("restore access observations = field calls %d snapshot %v", restoreFieldAccess, accessSawSnapshot)
	}
	if restoreReferenceReads == 0 {
		t.Fatal("restore did not revalidate snapshot relationships")
	}

	allLocales, err := expanded.Local().Find(t.Context(), "posts", restored.ID, ridu.FindOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := allLocales.Values["summary"]; exists {
		t.Fatalf("restore retained top-level field absent from snapshot: %#v", allLocales.Values)
	}
	localizedDetails, valid := allLocales.Values["details"].CopyObject()
	if !valid {
		t.Fatalf("restored localized details = %#v", allLocales.Values["details"])
	}
	for locale, wantHeadline := range map[string]string{"en": "English snapshot", "fr": "French snapshot"} {
		details, valid := localizedDetails[locale].CopyObject()
		if !valid || stringValue(details["headline"]) != wantHeadline {
			t.Fatalf("restored %s details = %#v", locale, localizedDetails[locale])
		}
		if _, exists := details["summary"]; exists {
			t.Fatalf("restore retained %s nested field absent from snapshot: %#v", locale, details)
		}
	}
	versions, err := expanded.Local().Versions(t.Context(), "posts", document.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 6 || versions[0].Revision != restored.Revision {
		t.Fatalf("restore version history = %#v", versions)
	}
	if _, exists := versions[0].Snapshot.Values["summary"]; exists {
		t.Fatalf("restored version retained omitted field: %#v", versions[0].Snapshot.Values)
	}
	if stringValue(versions[1].Snapshot.Values["summary"]) != "Current summary" {
		t.Fatalf("restore lost prior current version: %#v", versions[1].Snapshot.Values)
	}
}

func TestPublishChangesRequiresUpdateAndPublishAccess(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "publish edit access", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
		Access: ridu.CollectionAccess{
			Update:  func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil },
			Publish: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Draft")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().PublishChanges(context.Background(), "posts", draft.ID, store.Values{"title": store.String("Unauthorized edit")}, ridu.MutationOptions{ExpectedRevision: draft.Revision}); !operationCode(err, "access_denied") {
		t.Fatalf("publish body without update access = %v, want access_denied", err)
	}
	published, err := application.Local().Publish(context.Background(), "posts", draft.ID, ridu.MutationOptions{ExpectedRevision: draft.Revision})
	if err != nil {
		t.Fatalf("status-only publish should require only publish access: %v", err)
	}
	if published.Status != store.StatusPublished || stringValue(published.Values["title"]) != "Draft" {
		t.Fatalf("status-only publish = %#v", published)
	}
}

func TestDraftMutationIntentCannotBypassVersionOrPublicationContracts(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Draft mutation boundaries", Collections: []ridu.Collection{
		{Slug: "pages", Fields: field.Fields{field.Text("title").Required()}},
		{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}, Access: ridu.CollectionAccess{
			Create: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
			Update: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil },
		}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	if _, err := application.Local().Create(context.Background(), "pages", store.Values{"title": store.String("Unsafe")}, ridu.MutationOptions{Draft: &draft}); !operationCode(err, "bad_operation") {
		t.Fatalf("unversioned draft create error = %v, want bad_operation", err)
	}
	post, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Draft")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	published := false
	if _, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Unauthorized publish")}, ridu.MutationOptions{Draft: &published}); !operationCode(err, "access_denied") {
		t.Fatalf("published create without update access error = %v, want access_denied", err)
	}
	if _, err := application.Local().Update(context.Background(), "posts", post.ID, store.Values{"title": store.String("Bypass")}, ridu.MutationOptions{Draft: &published}); !operationCode(err, "bad_operation") {
		t.Fatalf("status-changing update error = %v, want bad_operation", err)
	}
}

func TestPublishChangesUsesPublishAccessAndLifecycle(t *testing.T) {
	actor := &store.Document{ID: "publisher"}
	var publishHooks int
	var publishHookTitle string
	application, err := ridu.New(ridu.Config{Name: "publish changes", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
		Access: ridu.CollectionAccess{
			Update: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
			Publish: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				title, _ := ctx.Data["title"].StringValue()
				if ctx.Actor != nil && ctx.Actor.ID == actor.ID && title == "Approved" {
					return ridu.Allow(), nil
				}
				return ridu.Deny(), nil
			},
		},
		Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
			if ctx.Operation == operation.Publish {
				publishHooks++
				publishHookTitle, _ = ctx.Data["title"].StringValue()
				ctx.Data["title"] = store.String("Approved by publish hook")
			}
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Draft")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := application.Local().Update(context.Background(), "posts", draft.ID, store.Values{"title": store.String("Ordinary edit")}, ridu.MutationOptions{Actor: actor})
	if err != nil {
		t.Fatalf("ordinary update should use update access: %v", err)
	}
	if _, err := application.Local().PublishChanges(context.Background(), "posts", draft.ID, store.Values{"title": store.String("Denied")}, ridu.MutationOptions{Actor: actor, ExpectedRevision: updated.Revision}); !operationCode(err, "access_denied") {
		t.Fatalf("publish changes without publish access error = %v, want access_denied", err)
	}
	published, err := application.Local().PublishChanges(context.Background(), "posts", draft.ID, store.Values{"title": store.String("Approved")}, ridu.MutationOptions{Actor: actor, ExpectedRevision: updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := published.Values["title"].StringValue(); published.Status != store.StatusPublished || title != "Approved by publish hook" || publishHooks != 1 || publishHookTitle != "Approved" {
		t.Fatalf("published changes = status %q title %q hook input %q hooks %d", published.Status, title, publishHookTitle, publishHooks)
	}
}

func TestPublishAndUnpublishRunConfiguredFieldLifecycle(t *testing.T) {
	seen := make(map[string][]operation.Kind)
	record := func(phase string) field.Observer[string] {
		return func(ctx operation.Context, _ operation.Value[string]) error {
			seen[phase] = append(seen[phase], ctx.Operation)
			return nil
		}
	}
	recordWrite := func(phase string) field.Transform[string] {
		return func(ctx operation.Context, _ operation.Value[string]) (operation.Change[string], error) {
			seen[phase] = append(seen[phase], ctx.Operation)
			return operation.Keep[string](), nil
		}
	}
	title := field.Text("title").Required().Hooks(field.Hooks[string]{BeforeValidate: []field.RawTransform{func(ctx operation.Context, _ operation.Value[store.Value]) (operation.Change[store.Value], error) {
		seen["beforeValidate"] = append(seen["beforeValidate"], ctx.Operation)
		return operation.Keep[store.Value](), nil
	}}, BeforeChange: []field.Transform[string]{func(ctx operation.Context, _ operation.Value[string]) (operation.Change[string], error) {
		seen["beforeChange"] = append(seen["beforeChange"], ctx.Operation)
		value := "Unpublished by hook"
		if ctx.Operation == operation.Publish {
			value = "Published by hook"
		}
		return operation.Set(value), nil
	}}, BeforeOperation: []field.Transform[string]{recordWrite("beforeOperation")}, AfterChange: []field.Observer[string]{record("afterChange")}, AfterOperation: []field.Observer[string]{record("afterOperation")}, AfterCommit: []field.Observer[string]{record("afterCommit")}})
	application, err := ridu.New(ridu.Config{Name: "Status field hooks", Collections: []ridu.Collection{{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{title}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Stable")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	clear(seen)
	document, err = application.Local().Publish(context.Background(), "posts", document.ID, ridu.MutationOptions{ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := document.Values["title"].StringValue(); title != "Published by hook" {
		t.Fatalf("published title = %q", title)
	}
	document, err = application.Local().Unpublish(context.Background(), "posts", document.ID, ridu.MutationOptions{ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := document.Values["title"].StringValue(); title != "Unpublished by hook" {
		t.Fatalf("unpublished title = %q", title)
	}
	versions, err := application.Local().Versions(context.Background(), "posts", document.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 || stringValue(versions[0].Snapshot.Values["title"]) != "Unpublished by hook" || stringValue(versions[1].Snapshot.Values["title"]) != "Published by hook" {
		t.Fatalf("status hook versions = %#v", versions)
	}
	statusWant := []operation.Kind{operation.Publish, operation.Unpublish}
	readWant := []operation.Kind{operation.Publish, operation.Unpublish, operation.ReadVersions}
	for _, phase := range []string{"beforeOperation", "afterOperation"} {
		if !slices.Equal(seen[phase], readWant) {
			t.Errorf("%s operations = %v, want %v", phase, seen[phase], readWant)
		}
	}
	// Validation and commit hooks describe changes, so reading versions skips them.
	for _, phase := range []string{"beforeValidate", "beforeChange", "afterChange", "afterCommit"} {
		if !slices.Equal(seen[phase], statusWant) {
			t.Errorf("%s operations = %v, want %v", phase, seen[phase], statusWant)
		}
	}
}

func TestMutationPopulationNeverEntersVersionSnapshots(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Canonical version snapshots", Collections: []ridu.Collection{
		{Slug: "categories", Fields: field.Fields{field.Text("name").Required()}},
		{Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title").Required(), field.Text("body"), field.Relationship("category", "categories")}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	category, err := application.Local().Create(context.Background(), "categories", store.Values{"name": store.String("News")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	categoryPath, _ := query.NewPath("category")
	created, err := application.Local().Create(context.Background(), "posts", store.Values{
		"title": store.String("Story"), "body": store.String("Complete body"), "category": store.String(category.ID),
	}, ridu.MutationOptions{Populate: []query.Population{{Path: categoryPath}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, populated := created.Values["category"].CopyDocument(); !populated {
		t.Fatalf("mutation response was not populated: %#v", created.Values["category"])
	}
	versions, err := application.Local().Versions(context.Background(), "posts", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %#v", versions)
	}
	categoryID, scalar := versions[0].Snapshot.Values["category"].StringValue()
	if !scalar || categoryID != category.ID || stringValue(versions[0].Snapshot.Values["body"]) != "Complete body" {
		t.Fatalf("version snapshot contains response projection: %#v", versions[0].Snapshot.Values)
	}
	updated, err := application.Local().PublishChanges(context.Background(), "posts", created.ID, store.Values{"title": store.String("Changed")}, ridu.MutationOptions{ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := application.Local().Restore(context.Background(), "posts", created.ID, 1, ridu.MutationOptions{ExpectedRevision: updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(restored.Values["title"]) != "Story" || stringValue(restored.Values["body"]) != "Complete body" || stringValue(restored.Values["category"]) != category.ID {
		t.Fatalf("restored canonical snapshot = %#v", restored.Values)
	}
}

func TestVersionReadsRunComputedAndAfterReadLifecycleBeforeRedaction(t *testing.T) {
	var collectionReads, fieldReads int
	application, err := ridu.New(ridu.Config{Name: "Version read lifecycle", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		Fields: field.Fields{field.Text("title").Required().ReplaceAfterRead(func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
			fieldReads++
			return operation.Keep[string](), nil
		}), field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) {
			return false, nil
		}}), field.Virtual("summary", func(operation.Context) (operation.Value[string],

			error) {
			return operation.Present("computed"), nil
		})},

		Hooks: ridu.CollectionHooks{AfterRead: []ridu.Hook{func(ctx ridu.HookContext) error {
			collectionReads++
			ctx.Document.Values["marker"] = store.String("after-read")
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{
		"title": store.String("First"), "secret": store.String("hidden"),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	document, err = application.Local().PublishChanges(context.Background(), "posts", document.ID, store.Values{"title": store.String("Second")}, ridu.MutationOptions{ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	collectionReads, fieldReads = 0, 0
	versions, err := application.Local().Versions(context.Background(), "posts", document.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || collectionReads != 2 || fieldReads != 2 {
		t.Fatalf("version read hook counts: versions=%d collection=%d field=%d", len(versions), collectionReads, fieldReads)
	}
	for _, version := range versions {
		if stringValue(version.Snapshot.Values["summary"]) != "computed" || stringValue(version.Snapshot.Values["marker"]) != "after-read" {
			t.Fatalf("version response lifecycle = %#v", version.Snapshot.Values)
		}
		if _, exposed := version.Snapshot.Values["secret"]; exposed {
			t.Fatalf("version field redaction failed: %#v", version.Snapshot.Values)
		}
	}
	collectionReads, fieldReads = 0, 0
	version, err := application.Local().Version(context.Background(), "posts", document.ID, 1, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if collectionReads != 1 || fieldReads != 1 {
		t.Fatalf("single version read hook counts: collection=%d field=%d, want one lifecycle", collectionReads, fieldReads)
	}
	if stringValue(version.Snapshot.Values["summary"]) != "computed" || stringValue(version.Snapshot.Values["marker"]) != "after-read" {
		t.Fatalf("single version response lifecycle = %#v", version.Snapshot.Values)
	}
}

func TestVersionHistoryChangesOnlyAfterMutations(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "read-only versions", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10},
		Fields:        field.Fields{field.Text("title").Required()},
		Access: ridu.CollectionAccess{ReadDrafts: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
			if ctx.Actor == nil {
				return ridu.Deny(), nil
			}
			return ridu.Allow(), nil
		}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	actor := &store.Document{ID: "editor"}
	document, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Stable")}, ridu.MutationOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := application.Local().Find(ctx, "posts", document.ID, ridu.FindOptions{Actor: actor}); err != nil {
			t.Fatal(err)
		}
		if _, err := application.Local().List(ctx, "posts", ridu.ListOptions{Actor: actor}); err != nil {
			t.Fatal(err)
		}
	}
	versions, err := application.Local().Versions(ctx, "posts", document.ID, ridu.FindOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].ID != document.ID+":1" {
		t.Fatalf("versions after reads = %#v", versions)
	}
}

func TestRestoreAsDraftRejectsVersionedCollectionWithoutDrafts(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "versions without drafts", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Published")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().RestoreAsDraft(context.Background(), "posts", document.ID, 1, ridu.MutationOptions{ExpectedRevision: document.Revision}); !operationCode(err, "bad_operation") {
		t.Fatalf("restore as draft without drafts = %v", err)
	}
}

func TestVersionHistoryRedactsUnreadableFields(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "redacted versions", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		Fields: field.Fields{field.Text("title").Required(), field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) {
			return false, nil
		}})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Visible"), "secret": store.String("classified")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	versions, err := application.Local().Versions(context.Background(), "posts", document.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %d", len(versions))
	}
	if _, exists := versions[0].Snapshot.Values["secret"]; exists {
		t.Fatal("version history exposed a field denied by read access")
	}
}

func TestVersionHistoryUsesDistinctAccessRule(t *testing.T) {
	var operationKind operation.Kind
	application, err := ridu.New(ridu.Config{Name: "version access", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		Fields: field.Fields{field.Text("title").Required()},
		Access: ridu.CollectionAccess{
			Read: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
			ReadVersions: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				operationKind = ctx.Operation
				return ridu.Deny(), nil
			},
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Visible")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Find(context.Background(), "posts", document.ID, ridu.FindOptions{}); err != nil {
		t.Fatalf("ordinary read = %v", err)
	}
	if _, err := application.Local().Versions(context.Background(), "posts", document.ID, ridu.FindOptions{}); !operationCode(err, "access_denied") {
		t.Fatalf("version read = %v", err)
	}
	if _, err := application.Local().Restore(context.Background(), "posts", document.ID, 1, ridu.MutationOptions{ExpectedRevision: document.Revision}); !operationCode(err, "access_denied") {
		t.Fatalf("version restore without readVersions access = %v", err)
	}
	if operationKind != operation.ReadVersions {
		t.Fatalf("version access operation = %q", operationKind)
	}
}

func TestVersionHistoryAppliesFilteredAccessToEverySnapshot(t *testing.T) {
	ownerPath, err := query.NewPath("owner")
	if err != nil {
		t.Fatal(err)
	}
	owned := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil {
			return ridu.Deny(), nil
		}
		return ridu.Where(query.Equal(ownerPath, ctx.Actor.ID)), nil
	}
	application, err := ridu.New(ridu.Config{Name: "version snapshot access", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		VersionConfig: ridu.VersionConfig{MaxPerDocument: 10},
		Fields:        field.Fields{field.Text("title"), field.Text("owner").Required()},
		Access: ridu.CollectionAccess{
			Create:       func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
			Read:         owned,
			ReadVersions: owned,
			Update:       func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	firstOwner := &store.Document{ID: "owner-a"}
	secondOwner := &store.Document{ID: "owner-b"}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{
		"title": store.String("Transferred"), "owner": store.String(firstOwner.ID),
	}, ridu.MutationOptions{Actor: firstOwner})
	if err != nil {
		t.Fatal(err)
	}
	document, err = application.Local().PublishChanges(context.Background(), "posts", document.ID, store.Values{
		"owner": store.String(secondOwner.ID),
	}, ridu.MutationOptions{Actor: firstOwner, ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	versions, err := application.Local().Versions(context.Background(), "posts", document.ID, ridu.FindOptions{Actor: secondOwner})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Revision != 2 {
		t.Fatalf("new owner versions = %#v, want only the transferred snapshot", versions)
	}
	owner, _ := versions[0].Snapshot.Values["owner"].StringValue()
	if owner != secondOwner.ID {
		t.Fatalf("visible snapshot owner = %q, want %q", owner, secondOwner.ID)
	}
	versions, err = application.Local().Versions(context.Background(), "posts", document.ID, ridu.FindOptions{Actor: firstOwner})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Revision != 1 {
		t.Fatalf("original owner versions = %#v, want only the original snapshot", versions)
	}
	thirdOwner := &store.Document{ID: "owner-c"}
	for _, test := range []struct {
		name  string
		actor *store.Document
		want  bool
	}{
		{name: "original owner", actor: firstOwner, want: true},
		{name: "current owner", actor: secondOwner, want: true},
		{name: "unrelated owner", actor: thirdOwner, want: false},
	} {
		t.Run("capability "+test.name, func(t *testing.T) {
			capabilities, err := application.Local().Capabilities(context.Background(), "posts", document.ID, ridu.CapabilityOptions{Actor: test.actor})
			if err != nil {
				t.Fatal(err)
			}
			if capabilities.Operations.ReadVersions != test.want {
				t.Fatalf("readVersions capability = %t, want %t", capabilities.Operations.ReadVersions, test.want)
			}
		})
	}
}

func TestScheduledPublicationUsesDurableJobBoundary(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{Name: "scheduled", Admin: ridu.AdminConfig{User: "users"}, Collections: []ridu.Collection{
		{Slug: "users", Auth: true, Fields: field.Fields{field.Text("email").Required().Unique()}},
		{
			Slug: "books", Versions: true,
			VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields:        field.Fields{field.Text("title").Required()},
		},
	}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("publisher@example.test")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actor := &publisher
	identity := &ridu.AuthIdentity{Collection: "users", Actor: publisher}
	document, err := application.Local().Create(context.Background(), "books", store.Values{"title": store.String("Scheduled")}, ridu.MutationOptions{Actor: actor, ActorCollection: "users"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SchedulePublish(context.Background(), "books", document.ID, time.Now().Add(-time.Second), ridu.PublicationScheduleOptions{ExpectedRevision: document.Revision}, identity); err != nil {
		t.Fatal(err)
	}
	completed, err := application.RunScheduledPublications(context.Background(), 10, actor)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("completed = %d", completed)
	}
	if published, err := application.Local().Find(context.Background(), "books", document.ID, ridu.FindOptions{}); err != nil || published.Status != store.StatusPublished {
		t.Fatalf("public read = %#v, %v", published, err)
	}
	published, err := application.Local().Find(context.Background(), "books", document.ID, ridu.FindOptions{Actor: actor, ActorCollection: "users"})
	if err != nil {
		t.Fatal(err)
	}
	unpublishJob, err := application.ScheduleUnpublish(context.Background(), "books", document.ID, time.Now().Add(-time.Second), ridu.PublicationScheduleOptions{ExpectedRevision: published.Revision}, identity)
	if err != nil {
		t.Fatal(err)
	}
	if unpublishJob.Action != store.PublicationActionUnpublish {
		t.Fatalf("scheduled action = %q", unpublishJob.Action)
	}
	completed, err = application.RunScheduledPublications(context.Background(), 10, actor)
	if err != nil || completed != 1 {
		t.Fatalf("scheduled unpublish = %d, %v", completed, err)
	}
	includeDraft := true
	unpublished, err := application.Local().Find(context.Background(), "books", document.ID, ridu.FindOptions{Draft: &includeDraft, Actor: actor, ActorCollection: "users"})
	if err != nil || unpublished.Status != store.StatusDraft {
		t.Fatalf("unpublished document = %#v, %v", unpublished, err)
	}
}

func TestScheduleUnpublishRequiresDraftSupport(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "scheduled history", Collections: []ridu.Collection{{
		Slug: "events", Versions: true,
		Fields: field.Fields{field.Text("name").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(t.Context(), "events", store.Values{"name": store.String("Released")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.ScheduleUnpublish(t.Context(), "events", document.ID, time.Now().Add(time.Hour), ridu.PublicationScheduleOptions{ExpectedRevision: document.Revision}, nil); !operationCode(err, "bad_operation") {
		t.Fatalf("schedule unpublish without drafts error = %v", err)
	}
}

func TestScheduledUnpublishFailureUsesPublicationErrorCode(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{Name: "scheduled unpublish failure", Collections: []ridu.Collection{{
		Slug: "books", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(t.Context(), "books", store.Values{"title": store.String("Scheduled")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	published, err := application.Local().Publish(t.Context(), "books", document.ID, ridu.MutationOptions{ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.ScheduleUnpublish(t.Context(), "books", published.ID, time.Now().Add(-time.Second), ridu.PublicationScheduleOptions{ExpectedRevision: published.Revision + 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completed, err := application.RunScheduledPublications(t.Context(), 1, nil); err != nil || completed != 0 {
		t.Fatalf("run stale scheduled unpublish = %d, %v", completed, err)
	}
	failed, err := backend.FindTask(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != store.TaskStateFailed || failed.LastErrorCode != "scheduled_publication_rejected" {
		t.Fatalf("failed scheduled unpublish = %#v", failed)
	}
}

func TestScheduledPublicationDoesNotRequireReadAccess(t *testing.T) {
	denyRead := func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil }
	titlePath, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	allowScheduledTitle := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(titlePath, "Scheduled")), nil
	}
	application, err := ridu.New(ridu.Config{Name: "scheduled without read", Collections: []ridu.Collection{{
		Slug: "books", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Access: ridu.CollectionAccess{Read: denyRead, Publish: allowScheduledTitle, Unpublish: allowScheduledTitle},
		Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(t.Context(), "books", store.Values{"title": store.String("Scheduled")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	publishJob, err := application.SchedulePublish(t.Context(), "books", document.ID, time.Now().Add(time.Hour), ridu.PublicationScheduleOptions{ExpectedRevision: document.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if publishJob.Action != store.PublicationActionPublish || publishJob.ExpectedRevision != document.Revision {
		t.Fatalf("scheduled publish = %#v", publishJob)
	}
	blocked, err := application.Local().Create(t.Context(), "books", store.Values{"title": store.String("Blocked")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SchedulePublish(t.Context(), "books", blocked.ID, time.Now().Add(time.Hour), ridu.PublicationScheduleOptions{ExpectedRevision: blocked.Revision}, nil); !operationCode(err, "not_found") {
		t.Fatalf("filtered scheduled publish error = %v", err)
	}
	published, err := application.Local().Publish(t.Context(), "books", document.ID, ridu.MutationOptions{ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	unpublishJob, err := application.ScheduleUnpublish(t.Context(), "books", document.ID, time.Now().Add(time.Hour), ridu.PublicationScheduleOptions{ExpectedRevision: 0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unpublishJob.Action != store.PublicationActionUnpublish || unpublishJob.ExpectedRevision != published.Revision {
		t.Fatalf("scheduled unpublish = %#v", unpublishJob)
	}
}

func TestScheduledPublicationTerminalFailureRemainsActionableAndDismissible(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{Name: "scheduled failure", Admin: ridu.AdminConfig{User: "users"}, Collections: []ridu.Collection{
		{Slug: "users", Auth: true, Fields: field.Fields{field.Text("email").Required().Unique()}},
		{
			Slug: "books", Versions: true,
			VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields:        field.Fields{field.Text("title").Required()},
		},
	}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	publisher, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("publisher@example.test")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actor := &publisher
	identity := &ridu.AuthIdentity{Collection: "users", Actor: publisher}
	document, err := application.Local().Create(ctx, "books", store.Values{"title": store.String("Scheduled")}, ridu.MutationOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.SchedulePublish(ctx, "books", document.ID, time.Now().Add(-time.Second), ridu.PublicationScheduleOptions{ExpectedRevision: document.Revision}, identity)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claimed scheduled publish = %#v, %v", claimed, err)
	}
	if err := backend.FailTask(ctx, store.TaskFailure{
		ID: job.ID, LeaseToken: claimed[0].LeaseToken, Code: "scheduled_publication_rejected", Message: "terminal",
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := application.ScheduledPublications(ctx, "books", document.ID, identity)
	if err != nil || len(listed) != 1 || listed[0].ID != job.ID || listed[0].LastError != "terminal" {
		t.Fatalf("listed terminal schedule = %#v, %v", listed, err)
	}
	if err := application.CancelScheduledPublication(ctx, "books", document.ID, job.ID, identity); err != nil {
		t.Fatal(err)
	}
	listed, err = application.ScheduledPublications(ctx, "books", document.ID, identity)
	if err != nil || len(listed) != 0 {
		t.Fatalf("listed after terminal dismiss = %#v, %v", listed, err)
	}
	if _, err := backend.FindTask(ctx, job.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("dismissed task lookup = %v", err)
	}
}

func TestScheduledPublicationRehydratesRequestingActor(t *testing.T) {
	backend := teststore.New()
	publishersOnly := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor != nil {
			if role, ok := ctx.Actor.Values["role"].StringValue(); ok && role == "publisher" {
				return ridu.Allow(), nil
			}
		}
		return ridu.Deny(), nil
	}
	application, err := ridu.New(ridu.Config{
		Name:  "scheduled actor",
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: field.Fields{field.Text("email").Required().Unique(), field.Text("role").Required()}},
			{Slug: "books", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Access: ridu.CollectionAccess{Update: publishersOnly}, Fields: field.Fields{field.Text("title").Required()}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	publisher, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("publisher@example.test"), "role": store.String("publisher")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(ctx, "books", store.Values{"title": store.String("Authorized")}, ridu.MutationOptions{Actor: &publisher})
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.SchedulePublish(ctx, "books", document.ID, time.Now().Add(-time.Second), ridu.PublicationScheduleOptions{ExpectedRevision: document.Revision}, &ridu.AuthIdentity{Collection: "users", Actor: publisher})
	if err != nil {
		t.Fatal(err)
	}
	if job.RequestedByCollectionID == "" || job.RequestedByUserID != publisher.ID {
		t.Fatalf("requesting identity = %q/%q", job.RequestedByCollectionID, job.RequestedByUserID)
	}
	completed, err := application.RunScheduledPublications(ctx, 10, nil)
	if err != nil || completed != 1 {
		t.Fatalf("run scheduled = %d, %v", completed, err)
	}
	if published, err := application.Local().Find(ctx, "books", document.ID, ridu.FindOptions{}); err != nil || published.Status != store.StatusPublished {
		t.Fatalf("published document = %#v, %v", published, err)
	}
}

func TestScheduledPublicationRechecksExactAuthCollectionAtExecution(t *testing.T) {
	backend := teststore.New()
	publishersOnly := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		role := ""
		if ctx.Actor != nil {
			role, _ = ctx.Actor.Values["role"].StringValue()
		}
		if ctx.ActorCollection == "staff" && role == "publisher" {
			return ridu.Allow(), nil
		}
		return ridu.Deny(), nil
	}
	application, err := ridu.New(ridu.Config{
		Name: "scheduled exact identity", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: field.Fields{field.Text("email").Required().Unique(), field.Text("role").Required()}},
			{Slug: "staff", Auth: true, Fields: field.Fields{field.Text("email").Required().Unique(), field.Text("role").Required()}},
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
	const requesterID = "shared-requester"
	for _, fixture := range []struct {
		collection schema.CollectionSlug
		email      string
		role       string
	}{
		{collection: "users", email: "user@example.test", role: "publisher"},
		{collection: "staff", email: "staff@example.test", role: "publisher"},
	} {
		if _, err := transaction.Create(ctx, store.CreateRequest{
			Collection: collections[fixture.collection], ID: requesterID,
			Values: store.Values{"email": store.String(fixture.email), "role": store.String(fixture.role)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(ctx, "books", store.Values{"title": store.String("Exact requester")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.SchedulePublish(ctx, "books", document.ID, time.Now().Add(-time.Second), ridu.PublicationScheduleOptions{ExpectedRevision: document.Revision}, &ridu.AuthIdentity{
		Collection: "staff", Actor: store.Document{ID: requesterID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.RequestedByCollectionID != collections["staff"].ID || job.RequestedByUserID != requesterID {
		t.Fatalf("scheduled requester = %q/%q", job.RequestedByCollectionID, job.RequestedByUserID)
	}
	if _, err := application.Local().Update(ctx, "staff", requesterID, store.Values{"role": store.String("viewer")}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	completed, err := application.RunScheduledPublications(ctx, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 0 {
		t.Fatalf("completed = %d, want revoked staff request to fail", completed)
	}
	if _, err := application.Local().Find(ctx, "books", document.ID, ridu.FindOptions{}); err == nil {
		t.Fatal("revoked staff request published by substituting same-ID users actor")
	}
	failed, err := backend.FindTask(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != store.TaskStateFailed || failed.LastErrorCode != "scheduled_publication_rejected" {
		t.Fatalf("task after revoked requester = %#v", failed)
	}
}
