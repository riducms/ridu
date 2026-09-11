package core_test

import (
	"context"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestCompoundUniqueIndexesMatchNullLocaleAndTrashSemantics(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Compound uniqueness",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug: "posts", Trash: true,
			Fields: field.Fields{field.Text("tenant"), field.Group("seo", field.Fields{field.Text("slug")}), field.Text("localizedCode").Localized()},
			Indexes: []ridu.CollectionIndex{
				{Fields: []string{"tenant", "seo.slug"}, Unique: true},
				{Fields: []string{"tenant", "localizedCode"}, Unique: true},
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
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
	if _, err := application.Local().Create(ctx, "posts", values("acme", "welcome", "B"), ridu.MutationOptions{}); !operationCode(err, "conflict") {
		t.Fatalf("nested compound duplicate error = %v, want conflict", err)
	}
	if _, err := application.Local().Create(ctx, "posts", values("acme", "different", "A"), ridu.MutationOptions{}); !operationCode(err, "conflict") {
		t.Fatalf("localized compound duplicate error = %v, want conflict", err)
	}

	// PostgreSQL's default NULLS DISTINCT contract allows multiple tuples with
	// a missing/null component.
	for index := 0; index < 2; index++ {
		if _, err := application.Local().Create(ctx, "posts", values("nullable", "", ""), ridu.MutationOptions{}); err != nil {
			t.Fatalf("nullable tuple %d: %v", index, err)
		}
	}

	// The same text in a different exact locale does not conflict. Fallback is
	// irrelevant to uniqueness.
	french, err := application.Local().Create(ctx, "posts", values("fr-only", "bonjour", "LOCAL"), ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "posts", values("fr-only", "hello", "LOCAL"), ridu.MutationOptions{}); err != nil {
		t.Fatalf("different exact locale conflicted: %v", err)
	}
	if _, err := application.Local().Update(ctx, "posts", french.ID, store.Values{"localizedCode": store.String("LOCAL")}, ridu.MutationOptions{Locale: "en"}); !operationCode(err, "conflict") {
		t.Fatalf("same exact locale update error = %v, want conflict", err)
	}

	if _, err := application.Local().Delete(ctx, "posts", first.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	replacement, err := application.Local().Create(ctx, "posts", values("acme", "welcome", "A"), ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("trashed tuple remained unique-active: %v", err)
	}
	if _, err := application.Local().RestoreDeleted(ctx, "posts", first.ID, ridu.MutationOptions{}); !operationCode(err, "conflict") {
		t.Fatalf("restore into occupied tuple error = %v, want conflict", err)
	}
	if _, err := application.Local().Delete(ctx, "posts", replacement.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().RestoreDeleted(ctx, "posts", first.ID, ridu.MutationOptions{}); err != nil {
		t.Fatalf("restore after freeing tuple: %v", err)
	}
}
