package core_test

import (
	"context"
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSlugLifecycleIsServerAuthoritative(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Slugs", Collections: []ridu.Collection{{
		Slug:   "posts",
		Fields: field.Fields{field.Group("seo", field.Fields{field.Text("title").Required()}), field.Slug("slug", "seo.title").Label("URL slug")},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	created, err := application.Local().Create(ctx, "posts", store.Values{
		"seo": store.Object(store.Values{"title": store.String("Hello, Ridu!")}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSlug(t, created, "hello-ridu")

	generated, err := application.Local().Update(ctx, "posts", created.ID, store.Values{
		"seo": store.Object(store.Values{"title": store.String("A New Title")}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSlug(t, generated, "a-new-title")

	manual, err := application.Local().Update(ctx, "posts", created.ID, store.Values{
		"slug": store.String("  Hand Authored / URL  "),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSlug(t, manual, "hand-authored-url")

	preserved, err := application.Local().Update(ctx, "posts", created.ID, store.Values{
		"seo": store.Object(store.Values{"title": store.String("Manual Slug Stays")}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSlug(t, preserved, "hand-authored-url")

	regenerated, err := application.Local().Update(ctx, "posts", created.ID, store.Values{
		"slug": store.String("Manual Slug Stays"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSlug(t, regenerated, "manual-slug-stays")

	followed, err := application.Local().Update(ctx, "posts", created.ID, store.Values{
		"seo": store.Object(store.Values{"title": store.String("Generated Again")}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSlug(t, followed, "generated-again")

	_, err = application.Local().Create(ctx, "posts", store.Values{
		"seo": store.Object(store.Values{"title": store.String("Generated Again")}),
	}, nil)
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "validation" {
		t.Fatalf("duplicate slug error = %v, want validation", err)
	}
	if len(operationError.Issues) != 1 || operationError.Issues[0].Code != "unique" || operationError.Issues[0].Path != "slug" {
		t.Fatalf("duplicate slug issues = %#v", operationError.Issues)
	}
}

func TestSlugCreateNormalizesManualValues(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Manual slugs", Collections: []ridu.Collection{{
		Slug:   "posts",
		Fields: field.Fields{field.Text("title").Required(), field.Slug("slug", "title")},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(context.Background(), "posts", store.Values{
		"title": store.String("Ignored Source"),
		"slug":  store.String("  A Custom / URL  "),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSlug(t, created, "a-custom-url")
}

func TestSlugNormalizesFinalHookMutations(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Hook slugs", Collections: []ridu.Collection{{
		Slug:   "posts",
		Fields: field.Fields{field.Text("title").Required(), field.Slug("slug", "title")},
		Hooks: ridu.CollectionHooks{BeforeOperation: []ridu.Hook{func(ctx ridu.HookContext) error {
			title, _ := ctx.Data["title"].StringValue()
			switch title {
			case "Rewrite source":
				ctx.Data["title"] = store.String("Hooked Source")
			case "Manual hook":
				ctx.Data["slug"] = store.String("Hook / Choice")
			}
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	rewritten, err := application.Local().Create(context.Background(), "posts", store.Values{
		"title": store.String("Rewrite source"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSlug(t, rewritten, "hooked-source")

	manual, err := application.Local().Create(context.Background(), "posts", store.Values{
		"title": store.String("Manual hook"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSlug(t, manual, "hook-choice")
}

func TestSlugSourceValidationReportsAuthoringPaths(t *testing.T) {
	tests := []struct {
		name   string
		fields field.Fields
		code   string
		path   string
	}{
		{
			name:   "missing source field",
			fields: field.Fields{field.Text("title"), field.Slug("slug", "missing")},
			code:   "invalid_slug_source", path: "collections[0].fields[1].sourcePath",
		},
		{
			name:   "localized source field",
			fields: field.Fields{field.Text("title").Localized(), field.Slug("slug", "title")},
			code:   "unsupported_localized_slug_source", path: "collections[0].fields[1].sourcePath",
		},
		{
			name:   "nested slug",
			fields: field.Fields{field.Text("title"), field.Group("seo", field.Fields{field.Slug("slug", "title")})},
			code:   "unsupported_nested_slug", path: "collections[0].fields[1].fields[0].sourcePath",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(ridu.Config{
				Name: "Invalid slug",
				Localization: ridu.LocalizationConfig{
					DefaultLocale: "en",
					Locales:       []ridu.Locale{{Code: "en", Label: "English"}},
				},
				Collections: []ridu.Collection{{Slug: "posts", Fields: test.fields}},
			})
			var validationError *schema.ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Resolve error = %T, want *schema.ValidationError", err)
			}
			for _, issue := range validationError.Issues {
				if issue.Code == test.code && issue.Path == test.path {
					return
				}
			}
			t.Fatalf("issues = %#v, want %s at %s", validationError.Issues, test.code, test.path)
		})
	}
}

func assertSlug(t *testing.T, document store.Document, expected string) {
	t.Helper()
	actual, valid := document.Values["slug"].StringValue()
	if !valid || actual != expected {
		t.Fatalf("slug = %q (%t), want %q", actual, valid, expected)
	}
}
