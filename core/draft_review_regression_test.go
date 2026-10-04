package core_test

import (
	"errors"
	"slices"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPublishRunsCustomValidatorsForRetainedExactLocales(t *testing.T) {
	app, err := ridu.New(ridu.Config{
		Name:         "publication validates translations",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Collections: []ridu.Collection{{
			Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields: field.Fields{field.Text("title").Localized().Required().Validate(func(ctx operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
				text, _ := value.Get()
				if ctx.WritePhase == operation.WritePhasePublished && text == "bad" {
					return []operation.Issue{{Code: "publication_title", Message: "bad translation cannot be published"}}, nil
				}
				return nil, nil
			})},
		}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	draft, live := true, false
	document, err := local.Create(t.Context(), "posts", store.Values{"title": store.String("English")}, ridu.MutationOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	document, err = local.Update(t.Context(), "posts", document.ID, store.Values{"title": store.String("Bon")}, ridu.MutationOptions{Draft: &draft, Locale: "fr", ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	document, err = local.Publish(t.Context(), "posts", document.ID, ridu.MutationOptions{ExpectedRevision: document.Revision, Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	publishedRevision := document.Revision
	document, err = local.Update(t.Context(), "posts", document.ID, store.Values{"title": store.String("bad")}, ridu.MutationOptions{Draft: &draft, Locale: "fr", ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Publish(t.Context(), "posts", document.ID, ridu.MutationOptions{ExpectedRevision: document.Revision, Locale: "en"}); !operationCode(err, "validation") {
		t.Fatalf("publish skipped retained French custom validator: %v", err)
	} else {
		var failure *ridu.OperationError
		if !errors.As(err, &failure) || !slices.ContainsFunc(failure.Issues, func(issue schema.Issue) bool {
			return issue.Code == "publication_title" && issue.Locale == "fr"
		}) {
			t.Fatalf("publish did not report French custom validation: %#v", err)
		}
	}
	public, err := local.Find(t.Context(), "posts", document.ID, ridu.FindOptions{Locale: "fr", Draft: &live})
	if err != nil || stringValue(public.Values["title"]) != "Bon" || public.Revision != publishedRevision {
		t.Fatalf("failed publish changed French live head: %#v, %v", public, err)
	}
}

func TestAuthDraftUpdateRetainsIdentityAndRejectsExplicitClear(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "auth partial draft", Admin: ridu.AdminConfig{User: "users"}, Collections: []ridu.Collection{{
		Slug: "users", Auth: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Email("email").Required().Unique(), field.Text("bio").Required()},
		Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
			if stringValue(ctx.Data["bio"]) == "erase identity" {
				ctx.Data["email"] = store.Null()
			}
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	document, err := app.Local().Create(t.Context(), "users", store.Values{"email": store.String("editor@example.test")}, ridu.MutationOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := app.Local().Update(t.Context(), "users", document.ID, store.Values{"bio": store.String("Working")}, ridu.MutationOptions{Draft: &draft, ExpectedRevision: document.Revision})
	if err != nil || stringValue(updated.Values["email"]) != "editor@example.test" {
		t.Fatalf("partial auth draft update lost retained identity: %#v, %#v", updated, err)
	}
	if _, err := app.Local().Update(t.Context(), "users", document.ID, store.Values{"bio": store.String("erase identity")}, ridu.MutationOptions{Draft: &draft, ExpectedRevision: updated.Revision}); !operationCode(err, "validation") {
		t.Fatalf("hook-cleared auth identity = %v, want validation after hooks", err)
	}
	if _, err := app.Local().Update(t.Context(), "users", document.ID, store.Values{"email": store.Null()}, ridu.MutationOptions{Draft: &draft, ExpectedRevision: updated.Revision}); !operationCode(err, "validation") {
		t.Fatalf("explicit auth identity clear = %v, want validation", err)
	}
}

func TestTrashRestoreChecksBothWorkingAndLiveUniqueHeads(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "restore both unique heads", Collections: []ridu.Collection{{
		Slug: "posts", Trash: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("slug").Required().Unique()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	draft, live := true, false
	document, err := local.Create(t.Context(), "posts", store.Values{"slug": store.String("public")}, ridu.MutationOptions{Draft: &live})
	if err != nil {
		t.Fatal(err)
	}
	document, err = local.Update(t.Context(), "posts", document.ID, store.Values{"slug": store.String("working")}, ridu.MutationOptions{Draft: &draft, ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Delete(t.Context(), "posts", document.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	competitor, err := local.Create(t.Context(), "posts", store.Values{"slug": store.String("public")}, ridu.MutationOptions{Draft: &live})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.RestoreDeleted(t.Context(), "posts", document.ID, ridu.MutationOptions{}); !operationCode(err, "conflict") {
		t.Fatalf("restore resurrected conflicting live slug: %v", err)
	}
	if _, err := local.Find(t.Context(), "posts", document.ID, ridu.FindOptions{Draft: &live}); !operationCode(err, "not_found") {
		t.Fatalf("failed restore exposed trashed live head: %v", err)
	}
	if _, err := local.Delete(t.Context(), "posts", competitor.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	restored, err := local.RestoreDeleted(t.Context(), "posts", document.ID, ridu.MutationOptions{})
	if err != nil || stringValue(restored.Values["slug"]) != "working" {
		t.Fatalf("restore after freeing live slug = %#v, %v", restored, err)
	}
}

func TestDuplicateUsesLiveWhenDraftReadDeniedInAnyRetainedLocale(t *testing.T) {
	for _, deniedLocale := range []schema.LocaleCode{"en", "fr"} {
		t.Run(string(deniedLocale), func(t *testing.T) {
			app, err := ridu.New(ridu.Config{
				Name: "duplicate visible source", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
				Collections: []ridu.Collection{{
					Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
					Fields: field.Fields{field.Text("title").Localized().Required()},
					Access: ridu.CollectionAccess{ReadDrafts: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
						if ctx.Locale == deniedLocale {
							return ridu.Deny(), nil
						}
						return ridu.Allow(), nil
					}},
				}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			local := app.Local()
			draft := true
			source, err := local.Create(t.Context(), "posts", store.Values{"title": store.String("English live")}, ridu.MutationOptions{Draft: &draft, System: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := local.Duplicate(t.Context(), "posts", source.ID, nil, ridu.MutationOptions{}); !operationCode(err, "not_found") {
				t.Fatalf("duplicate exposed never-published source: %v", err)
			}
			source, err = local.Update(t.Context(), "posts", source.ID, store.Values{"title": store.String("French live")}, ridu.MutationOptions{Draft: &draft, System: true, Locale: "fr", ExpectedRevision: source.Revision})
			if err != nil {
				t.Fatal(err)
			}
			source, err = local.Publish(t.Context(), "posts", source.ID, ridu.MutationOptions{System: true, ExpectedRevision: source.Revision})
			if err != nil {
				t.Fatal(err)
			}
			source, err = local.Update(t.Context(), "posts", source.ID, store.Values{"title": store.String("French working")}, ridu.MutationOptions{Draft: &draft, System: true, Locale: "fr", ExpectedRevision: source.Revision})
			if err != nil {
				t.Fatal(err)
			}
			copy, err := local.Duplicate(t.Context(), "posts", source.ID, nil, ridu.MutationOptions{ExpectedRevision: source.PublishedRevision})
			if err != nil {
				t.Fatalf("duplicate readable public source: %v", err)
			}
			all, err := local.Find(t.Context(), "posts", copy.ID, ridu.FindOptions{AllLocales: true, System: true, Draft: &draft})
			if err != nil {
				t.Fatal(err)
			}
			localized, ok := all.Values["title"].CopyObject()
			if !ok || stringValue(localized["en"]) != "English live" || stringValue(localized["fr"]) != "French live" {
				t.Fatalf("duplicate copied unreadable working content: %#v", all.Values["title"])
			}
		})
	}
}
