package core_test

import (
	"context"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestVersionLocaleAndJoinHelpersPreserveExactActorCollection(t *testing.T) {
	staffOnly := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor != nil && ctx.ActorCollection == "staff" {
			return ridu.Allow(), nil
		}
		return ridu.Deny(), nil
	}
	application, err := ridu.New(ridu.Config{
		Name: "specialized exact identity",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{
				Slug: "categories", Access: ridu.CollectionAccess{Read: staffOnly},
				Fields: field.Fields{field.Text("name"), field.Join("posts", "posts", "category")},
			},
			{
				Slug: "posts", Versions: true,
				Access: ridu.CollectionAccess{Read: staffOnly, ReadVersions: staffOnly, Update: staffOnly},
				Fields: field.Fields{field.Text("title").Localized(), field.Relationship("category", "categories")},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	category, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("News")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("English")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.Document{ID: "shared-actor"}
	readOptions := ridu.FindOptions{Actor: actor, ActorCollection: "staff", Locale: "en"}
	versions, err := application.Local().Versions(ctx, "posts", post.ID, readOptions)
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions = %#v, %v", versions, err)
	}
	if _, err := application.Local().Versions(ctx, "posts", post.ID, ridu.FindOptions{Actor: actor}); !operationCode(err, "access_denied") {
		t.Fatalf("actor-only versions = %v, want blank collection denied", err)
	}
	if _, err := application.Local().Version(ctx, "posts", post.ID, 1, readOptions); err != nil {
		t.Fatal(err)
	}
	copied, err := application.Local().CopyLocale(ctx, "posts", post.ID, "en", "fr", ridu.MutationOptions{
		Actor: actor, ActorCollection: "staff", ExpectedRevision: post.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := copied.Values["title"].StringValue(); title != "English" {
		t.Fatalf("copied title = %q", title)
	}
	restored, err := application.Local().Restore(ctx, "posts", post.ID, 1, ridu.MutationOptions{
		Actor: actor, ActorCollection: "staff", ExpectedRevision: copied.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined, err := application.Local().MutateJoin(ctx, "categories", category.ID, "posts", []string{post.ID}, nil, ridu.MutationOptions{
		Actor: actor, ActorCollection: "staff", Locale: schema.LocaleCode("en"),
	})
	if err != nil {
		t.Fatalf("mutate join: %+v", err)
	}
	if joined.Added != 1 || restored.ID != post.ID {
		t.Fatalf("join/restored = %#v / %#v", joined, restored)
	}
}
