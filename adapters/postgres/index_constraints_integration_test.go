package postgres_test

import (
	"context"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

func TestPostgresCompoundIndexesUseExactLocaleNullAndTrashSemantics(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{
		Name: "PostgreSQL compound indexes",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug: "posts", Trash: true,
			Fields: field.Fields{field.Text("tenant").Index(), field.Group("seo", field.Fields{field.Text("slug").Index()}), field.Text("localizedCode").Localized().Index()},
			Indexes: []ridu.CollectionIndex{
				{Fields: []string{"tenant", "seo.slug"}, Unique: true},
				{Fields: []string{"tenant", "localizedCode"}, Unique: true},
			},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	if plan, err := backend.Plan(ctx, manifest); err != nil || len(plan) != 0 {
		t.Fatalf("post-index migration plan = %#v, %v", plan, err)
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	values := func(tenant, slug, code string) store.Values {
		result := store.Values{"tenant": store.String(tenant)}
		if slug != "" {
			result["seo"] = store.Object(store.Values{"slug": store.String(slug)})
		}
		if code != "" {
			result["localizedCode"] = store.String(code)
		}
		return result
	}

	first, err := application.Local().Create(ctx, "posts", values("acme", "welcome", "A"), ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "posts", values("acme", "welcome", "B"), ridu.MutationOptions{}); !hasOperationCode(err, "conflict") {
		t.Fatalf("nested tuple duplicate = %v, want conflict", err)
	}
	if _, err := application.Local().Create(ctx, "posts", values("acme", "different", "A"), ridu.MutationOptions{}); !hasOperationCode(err, "conflict") {
		t.Fatalf("localized tuple duplicate = %v, want conflict", err)
	}
	for index := 0; index < 2; index++ {
		if _, err := application.Local().Create(ctx, "posts", values("nullable", "", ""), ridu.MutationOptions{}); err != nil {
			t.Fatalf("NULLS DISTINCT create %d: %v", index, err)
		}
	}
	if _, err := application.Local().Create(ctx, "posts", values("locale", "bonjour", "same"), ridu.MutationOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "posts", values("locale", "hello", "same"), ridu.MutationOptions{}); err != nil {
		t.Fatalf("different exact locale conflicted: %v", err)
	}
	if _, err := application.Local().Delete(ctx, "posts", first.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "posts", values("acme", "welcome", "A"), ridu.MutationOptions{}); err != nil {
		t.Fatalf("trashed tuple remained active: %v", err)
	}
	if _, err := application.Local().RestoreDeleted(ctx, "posts", first.ID, ridu.MutationOptions{}); !hasOperationCode(err, "conflict") {
		t.Fatalf("restore occupied tuple = %v, want conflict", err)
	}
}
