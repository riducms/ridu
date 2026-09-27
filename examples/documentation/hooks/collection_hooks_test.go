package content

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestArticleHooksRunAtTheirDocumentedStages(t *testing.T) {
	var purged []string
	purgeCache = func(_ context.Context, path string) error {
		purged = append(purged, path)
		return nil
	}
	t.Cleanup(func() { purgeCache = func(context.Context, string) error { return nil } })
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(nil) })

	backend := teststore.New()
	config := ridu.Config{Name: "Article hooks", Collections: []ridu.Collection{Articles, AuditLog}}
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	editor := store.Document{ID: "editor-1"}
	body := strings.Repeat("word ", 40)

	created, err := local.Create(t.Context(), "articles", store.Values{
		"title": store.String("Launch"),
		"slug":  store.String("launch"),
		"body":  store.String(body),
	}, ridu.MutationOptions{Actor: &editor})
	if err != nil {
		t.Fatal(err)
	}
	if excerpt, _ := created.Values["excerpt"].StringValue(); len(strings.Fields(excerpt)) != 30 {
		t.Fatalf("excerpt = %q, want the first 30 words", excerpt)
	}
	if count, _ := created.Values["wordCount"].NumberValue(); count != 40 {
		t.Fatalf("wordCount = %v, want 40", count)
	}
	if editorID, _ := created.Values["lastEditedBy"].StringValue(); editorID != "editor-1" {
		t.Fatalf("lastEditedBy = %q", editorID)
	}
	if metaTitle, _ := created.Values["metaTitle"].StringValue(); metaTitle != "Launch" {
		t.Fatalf("response metaTitle = %q, want the title", metaTitle)
	}
	if len(purged) != 1 || purged[0] != "/articles/launch" {
		t.Fatalf("purged after create = %v", purged)
	}

	// An update that leaves the body alone keeps the stored word count.
	updated, err := local.Update(t.Context(), "articles", created.ID, store.Values{
		"title": store.String("Launch day"),
	}, ridu.MutationOptions{Actor: &editor})
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := updated.Values["wordCount"].NumberValue(); count != 40 {
		t.Fatalf("wordCount after title update = %v", count)
	}
	if metaTitle, _ := updated.Values["metaTitle"].StringValue(); metaTitle != "Launch day" {
		t.Fatalf("metaTitle did not follow the title: %q", metaTitle)
	}
	if _, err := local.Find(t.Context(), "articles", created.ID, ridu.FindOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(purged) != 2 {
		t.Fatalf("a read purged the cache: %v", purged)
	}

	// The stored metaTitle remains empty; only responses are filled.
	plain := Articles
	plain.Hooks.AfterRead = nil
	config.Collections = []ridu.Collection{plain, AuditLog}
	storage, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := storage.Local().Find(t.Context(), "articles", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if metaTitle, _ := stored.Values["metaTitle"].StringValue(); metaTitle != "" {
		t.Fatalf("stored metaTitle = %q, want empty", metaTitle)
	}
	audit, err := local.List(t.Context(), "audit-log", ridu.ListOptions{})
	if err != nil || len(audit.Documents) != 2 {
		t.Fatalf("audit entries = %d, error = %v", len(audit.Documents), err)
	}

	featured, err := local.Update(t.Context(), "articles", created.ID, store.Values{
		"featured": store.Boolean(true),
	}, ridu.MutationOptions{Actor: &editor})
	if err != nil {
		t.Fatal(err)
	}
	copied, err := local.Duplicate(t.Context(), "articles", featured.ID, nil, ridu.MutationOptions{Actor: &editor})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := copied.Values["title"].StringValue(); title != "Launch day (copy)" {
		t.Fatalf("copied title = %q", title)
	}
	if isFeatured, _ := copied.Values["featured"].BooleanValue(); isFeatured {
		t.Fatal("the copy stayed featured")
	}

	logs.Reset()
	_, err = local.Delete(t.Context(), "articles", featured.ID, ridu.MutationOptions{})
	var failure *ridu.OperationError
	if !errors.As(err, &failure) || failure.Code != "hook_failed" ||
		!strings.Contains(errors.Unwrap(err).Error(), "remove the article from the homepage first") {
		t.Fatalf("deleting a featured article: %v", err)
	}
	if _, err := local.Find(t.Context(), "articles", featured.ID, ridu.FindOptions{}); err != nil {
		t.Fatalf("the blocked delete removed the article: %v", err)
	}
	if !strings.Contains(logs.String(), "delete failed") {
		t.Fatalf("AfterError did not log the failure: %q", logs.String())
	}
	if _, err := local.Delete(t.Context(), "articles", copied.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestFieldDuplicateAndCommitExamples(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(nil) })
	pages := ridu.Collection{Slug: "pages", Fields: field.Fields{field.Text("title").Required(), PageSlug, Status}}
	app, err := ridu.New(ridu.Config{Name: "Field examples", Collections: []ridu.Collection{pages}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	editor := store.Document{ID: "editor-1"}
	page, err := local.Create(t.Context(), "pages", store.Values{
		"title": store.String("About us"), "status": store.String("draft"),
	}, ridu.MutationOptions{Actor: &editor})
	if err != nil {
		t.Fatal(err)
	}
	copied, err := local.Duplicate(t.Context(), "pages", page.ID, nil, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if slug, _ := copied.Values["slug"].StringValue(); slug != "about-us-copy" {
		t.Fatalf("copied slug = %q", slug)
	}

	logs.Reset()
	if _, err := local.Update(t.Context(), "pages", page.ID, store.Values{"status": store.String("review")}, ridu.MutationOptions{Actor: &editor}); err != nil {
		t.Fatal(err)
	}
	if _, err := local.Update(t.Context(), "pages", page.ID, store.Values{"title": store.String("About")}, ridu.MutationOptions{Actor: &editor}); err != nil {
		t.Fatal(err)
	}
	if _, err := local.Find(t.Context(), "pages", page.ID, ridu.FindOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(logs.String()); strings.Count(got, "\n") != 0 ||
		!strings.Contains(got, `status "draft" -> "review" by user "editor-1"`) {
		t.Fatalf("status log = %q, want one draft -> review entry", got)
	}
}
