package core_test

import (
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestUniqueConflictsNameTheConflictingField(t *testing.T) {
	products := ridu.Collection{Slug: "products", Fields: field.Fields{
		field.Text("title"),
		field.Text("sku").Unique(),
	}}
	app, err := ridu.New(ridu.Config{Name: "Unique", Collections: []ridu.Collection{products}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	first, err := local.Create(t.Context(), "products", store.Values{"sku": store.String("A-1")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := local.Create(t.Context(), "products", store.Values{"sku": store.String("B-2")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	requireUniqueIssue := func(name string, err error) {
		t.Helper()
		var failure *ridu.OperationError
		if !errors.As(err, &failure) || failure.Code != "validation" || failure.Status != 422 ||
			len(failure.Issues) != 1 || failure.Issues[0].Path != "sku" || failure.Issues[0].Code != "unique" || failure.Issues[0].FieldID == "" {
			t.Fatalf("%s: error = %#v (%v), want a unique issue on sku", name, failure, err)
		}
	}
	_, err = local.Create(t.Context(), "products", store.Values{"sku": store.String("A-1")}, ridu.MutationOptions{})
	requireUniqueIssue("create", err)
	_, err = local.Update(t.Context(), "products", second.ID, store.Values{"sku": store.String("A-1")}, ridu.MutationOptions{})
	requireUniqueIssue("update", err)
	_, err = local.Duplicate(t.Context(), "products", first.ID, nil, ridu.MutationOptions{})
	requireUniqueIssue("duplicate", err)

	// Saving a document with its own value is not a conflict.
	if _, err := local.Update(t.Context(), "products", first.ID, store.Values{"title": store.String("Renamed")}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicatingASlugFindsTheNextFreeCopy(t *testing.T) {
	pages := ridu.Collection{Slug: "pages", Fields: field.Fields{
		field.Text("title").Required(),
		field.Slug("slug", "title"),
	}}
	app, err := ridu.New(ridu.Config{Name: "Slug copies", Collections: []ridu.Collection{pages}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	original, err := local.Create(t.Context(), "pages", store.Values{"title": store.String("About us")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	slugOf := func(document store.Document) string {
		slug, _ := document.Values["slug"].StringValue()
		return slug
	}
	if slugOf(original) != "about-us" {
		t.Fatalf("original slug = %q", slugOf(original))
	}
	first, err := local.Duplicate(t.Context(), "pages", original.ID, nil, ridu.MutationOptions{})
	if err != nil || slugOf(first) != "about-us-copy" {
		t.Fatalf("first copy slug = %q (%v)", slugOf(first), err)
	}
	second, err := local.Duplicate(t.Context(), "pages", original.ID, nil, ridu.MutationOptions{})
	if err != nil || slugOf(second) != "about-us-copy-2" {
		t.Fatalf("second copy slug = %q (%v)", slugOf(second), err)
	}
	third, err := local.Duplicate(t.Context(), "pages", first.ID, nil, ridu.MutationOptions{})
	if err != nil || slugOf(third) != "about-us-copy-3" {
		t.Fatalf("copy of a copy slug = %q (%v)", slugOf(third), err)
	}
	chosen, err := local.Duplicate(t.Context(), "pages", original.ID, store.Values{"slug": store.String("team")}, ridu.MutationOptions{})
	if err != nil || slugOf(chosen) != "team" {
		t.Fatalf("a slug chosen for the copy was replaced: %q (%v)", slugOf(chosen), err)
	}
}
