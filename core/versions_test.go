package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestDraftPublishingConflictsAndRestore(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{Name: "versions", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10},
		Fields:        []field.Definition{field.Text("title", field.Required())},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	draft, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("First")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != store.StatusDraft || draft.Revision != 1 {
		t.Fatalf("created metadata = %s/%d", draft.Status, draft.Revision)
	}
	if _, err := application.Local().Find(ctx, "posts", draft.ID, nil); err == nil {
		t.Fatal("public read exposed a draft")
	}
	actor := &store.Document{ID: "editor"}
	published, err := application.Local().Publish(ctx, "posts", draft.ID, draft.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != store.StatusPublished || published.Revision != 2 {
		t.Fatalf("published metadata = %s/%d", published.Status, published.Revision)
	}
	if _, err := application.Local().UpdateRevision(ctx, "posts", draft.ID, store.Values{"title": store.String("bypass")}, published.Revision, actor); !operationCode(err, "publish_required") {
		t.Fatalf("ordinary published update error = %v, want publish_required", err)
	}
	unchanged, err := application.Local().Find(ctx, "posts", draft.ID, actor)
	if err != nil || stringValue(unchanged.Values["title"]) != "First" || unchanged.Revision != published.Revision {
		t.Fatalf("rejected ordinary update changed live content: %#v, %v", unchanged, err)
	}
	updated, err := application.Local().PublishChanges(ctx, "posts", draft.ID, store.Values{"title": store.String("Second")}, 2, actor)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := application.Local().Restore(ctx, "posts", draft.ID, 2, updated.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	title, _ := restored.Values["title"].StringValue()
	if title != "First" || restored.Revision != 4 || restored.Status != store.StatusPublished {
		t.Fatalf("restored = %q revision %d", title, restored.Revision)
	}
	restoredDraft, err := application.Local().RestoreAsDraft(ctx, "posts", draft.ID, 2, restored.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	if restoredDraft.Status != store.StatusDraft || restoredDraft.Revision != 5 {
		t.Fatalf("restored as draft = %#v", restoredDraft)
	}
	versions, err := application.Local().Versions(ctx, "posts", draft.ID, actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 5 {
		t.Fatalf("versions = %d", len(versions))
	}
}

func TestRestoreExactlyReplacesValuesAfterAdditiveSchemaChange(t *testing.T) {
	backend := teststore.New()
	restoring := false
	var restoreHooks, restoreFieldAccess, restoreReferenceReads int
	var hookSawSnapshot, accessSawSnapshot bool
	var restoreHookData store.Values

	config := func(additive bool) ridu.Config {
		details := []field.Definition{field.Text("headline")}
		postFields := []field.Definition{
			field.Text("title", field.Required()),
			field.Relationship("author", field.To("authors"), field.Required()),
		}
		if additive {
			details = append(details, field.Text("summary"))
			postFields = append(postFields, field.Text("summary"))
		}
		postFields = append(postFields, field.Group("details", field.Localized(), field.Fields(details...)))
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
				details, _ := ctx.Data["details"].ObjectValue()
				_, hasNestedSummary := details["summary"]
				hookSawSnapshot = !hasSummary && !hasNestedSummary
				return nil
			}}},
		}
		if additive {
			post.FieldAccess = map[string]ridu.FieldAccess{"summary": {Update: func(ridu.FieldAccessContext) (bool, error) {
				if restoring {
					restoreFieldAccess++
				}
				return true, nil
			}}}
		}
		return ridu.Config{
			Name: "Exact version replacement",
			Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
				{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
			}},
			Collections: []ridu.Collection{
				{
					Slug: "authors", Fields: []field.Definition{field.Text("name", field.Required())},
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
	author, err := initial.Local().Create(t.Context(), "authors", store.Values{"name": store.String("Author")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	document, err := initial.Local().Create(t.Context(), "posts", store.Values{
		"title": store.String("Snapshot title"), "author": store.String(author.ID),
		"details": store.Object(store.Values{"headline": store.String("English snapshot")}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	document, err = initial.Local().PublishChanges(t.Context(), "posts", document.ID, store.Values{
		"details": store.Object(store.Values{"headline": store.String("French snapshot")}),
	}, document.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
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
	}, document.Revision, nil, ridu.LocaleOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	document, err = expanded.Local().PublishChanges(t.Context(), "posts", document.ID, store.Values{
		"details": store.Object(store.Values{
			"headline": store.String("French current"), "summary": store.String("French current summary"),
		}),
	}, document.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	document, err = expanded.Local().PublishChanges(t.Context(), "posts", document.ID, store.Values{
		"title": store.String("Ordinary patch"),
	}, document.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	beforeRestore, err := expanded.Local().Find(t.Context(), "posts", document.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(beforeRestore.Values["summary"]) != "Current summary" {
		t.Fatalf("ordinary patch removed omitted top-level value: %#v", beforeRestore.Values)
	}
	currentDetails, _ := beforeRestore.Values["details"].ObjectValue()
	englishCurrent, _ := currentDetails["en"].ObjectValue()
	frenchCurrent, _ := currentDetails["fr"].ObjectValue()
	if stringValue(englishCurrent["summary"]) != "English current summary" || stringValue(frenchCurrent["summary"]) != "French current summary" {
		t.Fatalf("ordinary patch removed omitted localized nested values: %#v", currentDetails)
	}

	restoring = true
	restored, err := expanded.Local().Restore(t.Context(), "posts", document.ID, snapshotRevision, document.Revision, nil)
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

	allLocales, err := expanded.Local().Find(t.Context(), "posts", restored.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := allLocales.Values["summary"]; exists {
		t.Fatalf("restore retained top-level field absent from snapshot: %#v", allLocales.Values)
	}
	localizedDetails, valid := allLocales.Values["details"].ObjectValue()
	if !valid {
		t.Fatalf("restored localized details = %#v", allLocales.Values["details"])
	}
	for locale, wantHeadline := range map[string]string{"en": "English snapshot", "fr": "French snapshot"} {
		details, valid := localizedDetails[locale].ObjectValue()
		if !valid || stringValue(details["headline"]) != wantHeadline {
			t.Fatalf("restored %s details = %#v", locale, localizedDetails[locale])
		}
		if _, exists := details["summary"]; exists {
			t.Fatalf("restore retained %s nested field absent from snapshot: %#v", locale, details)
		}
	}
	versions, err := expanded.Local().Versions(t.Context(), "posts", document.ID, nil)
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
		Fields: []field.Definition{field.Text("title", field.Required())},
		Access: ridu.CollectionAccess{
			Update:  func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil },
			Publish: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Draft")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().PublishChanges(context.Background(), "posts", draft.ID, store.Values{"title": store.String("Unauthorized edit")}, draft.Revision, nil); !operationCode(err, "access_denied") {
		t.Fatalf("publish body without update access = %v, want access_denied", err)
	}
	published, err := application.Local().Publish(context.Background(), "posts", draft.ID, draft.Revision, nil)
	if err != nil {
		t.Fatalf("status-only publish should require only publish access: %v", err)
	}
	if published.Status != store.StatusPublished || stringValue(published.Values["title"]) != "Draft" {
		t.Fatalf("status-only publish = %#v", published)
	}
}

func TestDraftMutationIntentCannotBypassVersionOrPublicationContracts(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Draft mutation boundaries", Collections: []ridu.Collection{
		{Slug: "pages", Fields: []field.Definition{field.Text("title", field.Required())}},
		{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: []field.Definition{field.Text("title", field.Required())}, Access: ridu.CollectionAccess{
			Create: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
			Update: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil },
		}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	if _, err := application.Local().CreateWithOptions(context.Background(), "pages", store.Values{"title": store.String("Unsafe")}, ridu.MutationOptions{Draft: &draft}); !operationCode(err, "bad_operation") {
		t.Fatalf("unversioned draft create error = %v, want bad_operation", err)
	}
	post, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Draft")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	published := false
	if _, err := application.Local().CreateWithOptions(context.Background(), "posts", store.Values{"title": store.String("Unauthorized publish")}, ridu.MutationOptions{Draft: &published}); !operationCode(err, "access_denied") {
		t.Fatalf("published create without update access error = %v, want access_denied", err)
	}
	if _, err := application.Local().UpdateWithOptions(context.Background(), "posts", post.ID, store.Values{"title": store.String("Bypass")}, ridu.MutationOptions{Draft: &published}); !operationCode(err, "bad_operation") {
		t.Fatalf("status-changing update error = %v, want bad_operation", err)
	}
}

func TestPublishChangesUsesPublishAccessAndLifecycle(t *testing.T) {
	actor := &store.Document{ID: "publisher"}
	var publishHooks int
	var publishHookTitle string
	application, err := ridu.New(ridu.Config{Name: "publish changes", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: []field.Definition{field.Text("title", field.Required())},
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
			if ctx.Operation == ridu.OperationPublish {
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
	draft, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Draft")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := application.Local().Update(context.Background(), "posts", draft.ID, store.Values{"title": store.String("Ordinary edit")}, actor)
	if err != nil {
		t.Fatalf("ordinary update should use update access: %v", err)
	}
	if _, err := application.Local().PublishChanges(context.Background(), "posts", draft.ID, store.Values{"title": store.String("Denied")}, updated.Revision, actor); !operationCode(err, "access_denied") {
		t.Fatalf("publish changes without publish access error = %v, want access_denied", err)
	}
	published, err := application.Local().PublishChanges(context.Background(), "posts", draft.ID, store.Values{"title": store.String("Approved")}, updated.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := published.Values["title"].StringValue(); published.Status != store.StatusPublished || title != "Approved by publish hook" || publishHooks != 1 || publishHookTitle != "Approved" {
		t.Fatalf("published changes = status %q title %q hook input %q hooks %d", published.Status, title, publishHookTitle, publishHooks)
	}
}

func TestPublishAndUnpublishRunConfiguredFieldLifecycle(t *testing.T) {
	seen := make(map[string][]ridu.Operation)
	record := func(phase string) ridu.Hook {
		return func(ctx ridu.HookContext) error {
			seen[phase] = append(seen[phase], ctx.Operation)
			return nil
		}
	}
	mutateBeforeChange := func(ctx ridu.HookContext) error {
		seen["beforeChange"] = append(seen["beforeChange"], ctx.Operation)
		if ctx.Operation == ridu.OperationPublish {
			ctx.Data["title"] = store.String("Published by hook")
		} else {
			ctx.Data["title"] = store.String("Unpublished by hook")
		}
		return nil
	}
	application, err := ridu.New(ridu.Config{Name: "status field hooks", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: []field.Definition{field.Text("title", field.Required())},
		FieldHooks: map[string]ridu.CollectionHooks{"title": {
			BeforeValidate:  []ridu.Hook{record("beforeValidate")},
			BeforeChange:    []ridu.Hook{mutateBeforeChange},
			BeforeOperation: []ridu.Hook{record("beforeOperation")},
			AfterChange:     []ridu.Hook{record("afterChange")},
			AfterOperation:  []ridu.Hook{record("afterOperation")},
			AfterCommit:     []ridu.Hook{record("afterCommit")},
		}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Stable")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	clear(seen)
	document, err = application.Local().Publish(context.Background(), "posts", document.ID, document.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := document.Values["title"].StringValue(); title != "Published by hook" {
		t.Fatalf("published title = %q", title)
	}
	document, err = application.Local().Unpublish(context.Background(), "posts", document.ID, document.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := document.Values["title"].StringValue(); title != "Unpublished by hook" {
		t.Fatalf("unpublished title = %q", title)
	}
	versions, err := application.Local().Versions(context.Background(), "posts", document.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 || stringValue(versions[0].Snapshot.Values["title"]) != "Unpublished by hook" || stringValue(versions[1].Snapshot.Values["title"]) != "Published by hook" {
		t.Fatalf("status hook versions = %#v", versions)
	}
	statusWant := []ridu.Operation{ridu.OperationPublish, ridu.OperationUnpublish}
	readWant := []ridu.Operation{ridu.OperationPublish, ridu.OperationUnpublish, ridu.OperationReadVersions}
	for _, phase := range []string{"beforeValidate", "beforeOperation", "afterOperation", "afterCommit"} {
		if !slices.Equal(seen[phase], readWant) {
			t.Errorf("%s operations = %v, want %v", phase, seen[phase], readWant)
		}
	}
	for _, phase := range []string{"beforeChange", "afterChange"} {
		if !slices.Equal(seen[phase], statusWant) {
			t.Errorf("%s operations = %v, want %v", phase, seen[phase], statusWant)
		}
	}
}

func TestMutationPopulationNeverEntersVersionSnapshots(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Canonical version snapshots", Collections: []ridu.Collection{
		{Slug: "categories", Fields: []field.Definition{field.Text("name", field.Required())}},
		{Slug: "posts", Versions: true, Fields: []field.Definition{
			field.Text("title", field.Required()), field.Text("body"), field.Relationship("category", field.To("categories")),
		}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	category, err := application.Local().Create(context.Background(), "categories", store.Values{"name": store.String("News")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	categoryPath, _ := query.NewPath("category")
	created, err := application.Local().CreateWithOptions(context.Background(), "posts", store.Values{
		"title": store.String("Story"), "body": store.String("Complete body"), "category": store.String(category.ID),
	}, ridu.MutationOptions{Populate: []query.Population{{Path: categoryPath}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, populated := created.Values["category"].DocumentValue(); !populated {
		t.Fatalf("mutation response was not populated: %#v", created.Values["category"])
	}
	versions, err := application.Local().Versions(context.Background(), "posts", created.ID, nil)
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
	updated, err := application.Local().PublishChanges(context.Background(), "posts", created.ID, store.Values{"title": store.String("Changed")}, created.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := application.Local().Restore(context.Background(), "posts", created.ID, 1, updated.Revision, nil)
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
		Fields: []field.Definition{
			field.Text("title", field.Required()),
			field.Text("secret"),
			field.Virtual("summary", field.ValueString),
		},
		Computed: map[string]ridu.Computed{"summary": func(ridu.ComputedContext) (store.Value, error) {
			return store.String("computed"), nil
		}},
		FieldAccess: map[string]ridu.FieldAccess{"secret": {Read: func(ridu.FieldAccessContext) (bool, error) { return false, nil }}},
		Hooks: ridu.CollectionHooks{AfterRead: []ridu.Hook{func(ctx ridu.HookContext) error {
			collectionReads++
			ctx.Document.Values["marker"] = store.String("after-read")
			return nil
		}}},
		FieldHooks: map[string]ridu.CollectionHooks{"title": {AfterRead: []ridu.Hook{func(ridu.HookContext) error {
			fieldReads++
			return nil
		}}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{
		"title": store.String("First"), "secret": store.String("hidden"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	document, err = application.Local().PublishChanges(context.Background(), "posts", document.ID, store.Values{"title": store.String("Second")}, document.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	collectionReads, fieldReads = 0, 0
	versions, err := application.Local().Versions(context.Background(), "posts", document.ID, nil)
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
	version, err := application.Local().Version(context.Background(), "posts", document.ID, 1, nil)
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
		Fields:        []field.Definition{field.Text("title", field.Required())},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	actor := &store.Document{ID: "editor"}
	document, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Stable")}, actor)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := application.Local().Find(ctx, "posts", document.ID, actor); err != nil {
			t.Fatal(err)
		}
		if _, err := application.Local().List(ctx, "posts", ridu.ListOptions{Actor: actor}); err != nil {
			t.Fatal(err)
		}
	}
	versions, err := application.Local().Versions(ctx, "posts", document.ID, actor)
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
		Fields: []field.Definition{field.Text("title", field.Required())},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Published")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().RestoreAsDraft(context.Background(), "posts", document.ID, 1, document.Revision, nil); !operationCode(err, "bad_operation") {
		t.Fatalf("restore as draft without drafts = %v", err)
	}
}

func TestVersionHistoryRedactsUnreadableFields(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "redacted versions", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		Fields: []field.Definition{field.Text("title", field.Required()), field.Text("secret")},
		FieldAccess: map[string]ridu.FieldAccess{
			"secret": {Read: func(ridu.FieldAccessContext) (bool, error) { return false, nil }},
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Visible"), "secret": store.String("classified")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := application.Local().Versions(context.Background(), "posts", document.ID, nil)
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
	var operation ridu.Operation
	application, err := ridu.New(ridu.Config{Name: "version access", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		Fields: []field.Definition{field.Text("title", field.Required())},
		Access: ridu.CollectionAccess{
			Read: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
			ReadVersions: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				operation = ctx.Operation
				return ridu.Deny(), nil
			},
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Visible")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Find(context.Background(), "posts", document.ID, nil); err != nil {
		t.Fatalf("ordinary read = %v", err)
	}
	if _, err := application.Local().Versions(context.Background(), "posts", document.ID, nil); !operationCode(err, "access_denied") {
		t.Fatalf("version read = %v", err)
	}
	if _, err := application.Local().Restore(context.Background(), "posts", document.ID, 1, document.Revision, nil); !operationCode(err, "access_denied") {
		t.Fatalf("version restore without readVersions access = %v", err)
	}
	if operation != ridu.OperationReadVersions {
		t.Fatalf("version access operation = %q", operation)
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
		return ridu.Where(query.Equal(ownerPath, query.String(ctx.Actor.ID))), nil
	}
	application, err := ridu.New(ridu.Config{Name: "version snapshot access", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		VersionConfig: ridu.VersionConfig{MaxPerDocument: 10},
		Fields:        []field.Definition{field.Text("title"), field.Text("owner", field.Required())},
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
	}, firstOwner)
	if err != nil {
		t.Fatal(err)
	}
	document, err = application.Local().PublishChanges(context.Background(), "posts", document.ID, store.Values{
		"owner": store.String(secondOwner.ID),
	}, document.Revision, firstOwner)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := application.Local().Versions(context.Background(), "posts", document.ID, secondOwner)
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
	versions, err = application.Local().Versions(context.Background(), "posts", document.ID, firstOwner)
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

func TestScheduledPublishUsesDurableJobBoundary(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{Name: "scheduled", Admin: ridu.AdminConfig{User: "users"}, Collections: []ridu.Collection{
		{Slug: "users", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())}},
		{
			Slug: "books", Versions: true,
			VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields:        []field.Definition{field.Text("title", field.Required())},
		},
	}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("publisher@example.test")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	actor := &publisher
	identity := &ridu.AuthIdentity{Collection: "users", Actor: publisher}
	document, err := application.Local().Create(context.Background(), "books", store.Values{"title": store.String("Scheduled")}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SchedulePublish(context.Background(), "books", document.ID, time.Now().Add(-time.Second), document.Revision, identity); err != nil {
		t.Fatal(err)
	}
	completed, err := application.RunScheduledPublishes(context.Background(), 10, actor)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("completed = %d", completed)
	}
	if published, err := application.Local().Find(context.Background(), "books", document.ID, nil); err != nil || published.Status != store.StatusPublished {
		t.Fatalf("public read = %#v, %v", published, err)
	}
}

func TestScheduledPublishTerminalFailureRemainsActionableAndDismissible(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{Name: "scheduled failure", Admin: ridu.AdminConfig{User: "users"}, Collections: []ridu.Collection{
		{Slug: "users", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())}},
		{
			Slug: "books", Versions: true,
			VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields:        []field.Definition{field.Text("title", field.Required())},
		},
	}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	publisher, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("publisher@example.test")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	actor := &publisher
	identity := &ridu.AuthIdentity{Collection: "users", Actor: publisher}
	document, err := application.Local().Create(ctx, "books", store.Values{"title": store.String("Scheduled")}, actor)
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.SchedulePublish(ctx, "books", document.ID, time.Now().Add(-time.Second), document.Revision, identity)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claimed scheduled publish = %#v, %v", claimed, err)
	}
	if err := backend.FailTask(ctx, store.TaskFailure{
		ID: job.ID, LeaseToken: claimed[0].LeaseToken, Code: "scheduled_publish_rejected", Message: "terminal",
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := application.ScheduledPublishes(ctx, "books", document.ID, identity)
	if err != nil || len(listed) != 1 || listed[0].ID != job.ID || listed[0].LastError != "terminal" {
		t.Fatalf("listed terminal schedule = %#v, %v", listed, err)
	}
	if err := application.CancelScheduledPublish(ctx, "books", document.ID, job.ID, identity); err != nil {
		t.Fatal(err)
	}
	listed, err = application.ScheduledPublishes(ctx, "books", document.ID, identity)
	if err != nil || len(listed) != 0 {
		t.Fatalf("listed after terminal dismiss = %#v, %v", listed, err)
	}
	if _, err := backend.FindTask(ctx, job.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("dismissed task lookup = %v", err)
	}
}

func TestScheduledPublishRehydratesRequestingActor(t *testing.T) {
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
			{Slug: "users", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique()), field.Text("role", field.Required())}},
			{Slug: "books", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Access: ridu.CollectionAccess{Update: publishersOnly}, Fields: []field.Definition{field.Text("title", field.Required())}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	publisher, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("publisher@example.test"), "role": store.String("publisher")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(ctx, "books", store.Values{"title": store.String("Authorized")}, &publisher)
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.SchedulePublish(ctx, "books", document.ID, time.Now().Add(-time.Second), document.Revision, &ridu.AuthIdentity{Collection: "users", Actor: publisher})
	if err != nil {
		t.Fatal(err)
	}
	if job.RequestedByCollectionID == "" || job.RequestedByUserID != publisher.ID {
		t.Fatalf("requesting identity = %q/%q", job.RequestedByCollectionID, job.RequestedByUserID)
	}
	completed, err := application.RunScheduledPublishes(ctx, 10, nil)
	if err != nil || completed != 1 {
		t.Fatalf("run scheduled = %d, %v", completed, err)
	}
	if published, err := application.Local().Find(ctx, "books", document.ID, nil); err != nil || published.Status != store.StatusPublished {
		t.Fatalf("published document = %#v, %v", published, err)
	}
}

func TestScheduledPublishRechecksExactAuthCollectionAtExecution(t *testing.T) {
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
			{Slug: "users", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique()), field.Text("role", field.Required())}},
			{Slug: "staff", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique()), field.Text("role", field.Required())}},
			{Slug: "books", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Access: ridu.CollectionAccess{Update: publishersOnly}, Fields: []field.Definition{field.Text("title", field.Required())}},
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
	document, err := application.Local().Create(ctx, "books", store.Values{"title": store.String("Exact requester")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.SchedulePublish(ctx, "books", document.ID, time.Now().Add(-time.Second), document.Revision, &ridu.AuthIdentity{
		Collection: "staff", Actor: store.Document{ID: requesterID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.RequestedByCollectionID != collections["staff"].ID || job.RequestedByUserID != requesterID {
		t.Fatalf("scheduled requester = %q/%q", job.RequestedByCollectionID, job.RequestedByUserID)
	}
	if _, err := application.Local().Update(ctx, "staff", requesterID, store.Values{"role": store.String("viewer")}, nil); err != nil {
		t.Fatal(err)
	}
	completed, err := application.RunScheduledPublishes(ctx, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 0 {
		t.Fatalf("completed = %d, want revoked staff request to fail", completed)
	}
	if _, err := application.Local().Find(ctx, "books", document.ID, nil); err == nil {
		t.Fatal("revoked staff request published by substituting same-ID users actor")
	}
	failed, err := backend.FindTask(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != store.TaskStateFailed || failed.LastErrorCode != "scheduled_publish_rejected" {
		t.Fatalf("task after revoked requester = %#v", failed)
	}
}
