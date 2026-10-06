package postgres_test

import (
	"context"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

// Localized JSON is read exactly as stored, so an explicit null inside a
// localized group is returned like one inside a nonlocalized group. Read
// validation must still accept every document the workflow can store: a
// draft may hold a null required child, and publication rejects it in every
// locale, so live content never does.
func TestPostgresLocalizedNestedNullsReadThroughDraftAndPublication(t *testing.T) {
	ctx := context.Background()
	draft, published := true, false
	config := ridu.Config{
		Name: "Localized nested nulls",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields: field.Fields{
				field.Text("name").Required(),
				field.Group("seo", field.Fields{field.Text("title").Required(), field.Text("summary")}).Localized(),
			},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	local := application.Local()
	created, err := local.Create(ctx, "pages", store.Values{
		"name": store.String("Home"),
		"seo":  store.Object(store.Values{"title": store.Null(), "summary": store.String("Brouillon")}),
	}, ridu.MutationOptions{System: true, Draft: &draft, Locale: "fr"})
	if err != nil {
		t.Fatalf("save incomplete French draft: %v", err)
	}
	seoTitle := func(document store.Document) store.Value {
		return document.Values["seo"].Get("title")
	}
	working, err := local.Find(ctx, "pages", created.ID, ridu.FindOptions{System: true, Draft: &draft, Locale: "fr"})
	if err != nil {
		t.Fatalf("working French read of an incomplete draft: %v", err)
	}
	if seoTitle(working).Kind() != store.ValueNull {
		t.Fatalf("working French seo.title = %#v, want the stored null", seoTitle(working))
	}
	if _, err := local.Find(ctx, "pages", created.ID, ridu.FindOptions{System: true, Draft: &draft, AllLocales: true}); err != nil {
		t.Fatalf("all-locale working read of an incomplete draft: %v", err)
	}
	english, err := local.Update(ctx, "pages", created.ID, store.Values{
		"seo": store.Object(store.Values{"title": store.String("Home")}),
	}, ridu.MutationOptions{System: true, Draft: &draft, Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Publish(ctx, "pages", created.ID, ridu.MutationOptions{System: true, ExpectedRevision: english.Revision, Locale: "en"}); !hasOperationCode(err, "validation") {
		t.Fatalf("publication with a null required French child = %v, want a validation failure", err)
	}
	completed, err := local.Update(ctx, "pages", created.ID, store.Values{
		"seo": store.Object(store.Values{"title": store.String("Accueil"), "summary": store.Null()}),
	}, ridu.MutationOptions{System: true, Draft: &draft, Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Publish(ctx, "pages", created.ID, ridu.MutationOptions{System: true, ExpectedRevision: completed.Revision, Locale: "fr"}); err != nil {
		t.Fatalf("publish complete locales: %v", err)
	}
	live, err := local.Find(ctx, "pages", created.ID, ridu.FindOptions{System: true, Draft: &published, Locale: "fr"})
	if err != nil {
		t.Fatalf("published French read: %v", err)
	}
	if title, _ := seoTitle(live).StringValue(); title != "Accueil" || live.Values["seo"].Get("summary").Kind() != store.ValueNull {
		t.Fatalf("published French seo = %#v", live.Values["seo"])
	}
	if _, err := local.Find(ctx, "pages", created.ID, ridu.FindOptions{System: true, Draft: &published, AllLocales: true}); err != nil {
		t.Fatalf("published all-locale read: %v", err)
	}
	if _, err := local.Update(ctx, "pages", created.ID, store.Values{
		"seo": store.Object(store.Values{"title": store.Null()}),
	}, ridu.MutationOptions{System: true, Draft: &draft, Locale: "fr"}); err != nil {
		t.Fatalf("stage a French draft that clears the required child: %v", err)
	}
	if _, err := local.Find(ctx, "pages", created.ID, ridu.FindOptions{System: true, Draft: &published, Locale: "fr"}); err != nil {
		t.Fatalf("published French read beside an incomplete pending draft: %v", err)
	}
	if pending, err := local.Find(ctx, "pages", created.ID, ridu.FindOptions{System: true, Draft: &draft, Locale: "fr"}); err != nil || seoTitle(pending).Kind() != store.ValueNull {
		t.Fatalf("working French read of an incomplete pending draft = %#v, %v", pending.Values["seo"], err)
	}
}
