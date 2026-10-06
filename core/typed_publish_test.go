package core_test

import (
	"errors"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
)

// A published document in a versioned collection or global changes only by
// being published again. Typed handles publish changes, publish a saved draft
// and unpublish without falling back to dynamic values.
func TestTypedHandlesPublishVersionedContent(t *testing.T) {
	app, err := core.New(core.Config{
		Name: "Typed publishing",
		Collections: []core.Collection{
			{Slug: "pages", Versions: true, Fields: field.Fields{field.Text("title").Required()}},
			{Slug: "articles", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}},
		},
		Globals: []core.Global{{Slug: "site", Versions: true, Fields: field.Fields{field.Text("title")}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	type input struct {
		Title string `json:"title"`
	}
	type document struct {
		ID       string  `json:"id"`
		Title    *string `json:"title"`
		Status   string  `json:"_status"`
		Revision int     `json:"_revision"`
	}
	code := func(err error) string {
		var failure *core.OperationError
		if errors.As(err, &failure) {
			return failure.Code
		}
		return ""
	}

	pages := core.NewTypedCollection[document, input, input, input]("pages").With(app.Local())
	page, err := pages.Create(t.Context(), input{Title: "About"}, core.TypedMutationOptions{})
	if err != nil || page.Status != "published" {
		t.Fatalf("create = %#v, %v; want a published page", page, err)
	}
	if _, err := pages.Update(t.Context(), page.ID, input{Title: "About us"}, core.TypedMutationOptions{}); code(err) != "publish_required" {
		t.Fatalf("update of a published page = %v; want publish_required", err)
	}
	edited, err := pages.PublishChanges(t.Context(), page.ID, input{Title: "About us"}, core.TypedMutationOptions{ExpectedRevision: page.Revision})
	if err != nil || edited.Title == nil || *edited.Title != "About us" || edited.Revision <= page.Revision {
		t.Fatalf("publish changes = %#v, %v", edited, err)
	}
	if _, err := pages.PublishChanges(t.Context(), page.ID, input{Title: "About the team"}, core.TypedMutationOptions{ExpectedRevision: page.Revision}); code(err) != "conflict" {
		t.Fatalf("publish changes at a stale revision = %v; want conflict", err)
	}

	articles := core.NewTypedCollection[document, input, input, input]("articles").With(app.Local())
	draft, err := articles.Create(t.Context(), input{Title: "Working title"}, core.TypedMutationOptions{})
	if err != nil || draft.Status != "draft" {
		t.Fatalf("create = %#v, %v; want a draft", draft, err)
	}
	published, err := articles.Publish(t.Context(), draft.ID, core.TypedMutationOptions{ExpectedRevision: draft.Revision})
	if err != nil || published.Status != "published" {
		t.Fatalf("publish = %#v, %v", published, err)
	}
	unpublished, err := articles.Unpublish(t.Context(), draft.ID, core.TypedMutationOptions{ExpectedRevision: published.Revision})
	if err != nil || unpublished.Status != "draft" || unpublished.Title == nil || *unpublished.Title != "Working title" {
		t.Fatalf("unpublish = %#v, %v; want the draft with its content", unpublished, err)
	}

	site := core.NewTypedGlobal[document, input, input]("site").With(app.Local())
	first, err := site.Update(t.Context(), input{Title: "Acme"}, core.TypedMutationOptions{})
	if err != nil || first.Status != "published" {
		t.Fatalf("first global update = %#v, %v; want a published global", first, err)
	}
	if _, err := site.Update(t.Context(), input{Title: "Acme Ltd"}, core.TypedMutationOptions{}); code(err) != "publish_required" {
		t.Fatalf("update of a published global = %v; want publish_required", err)
	}
	renamed, err := site.PublishChanges(t.Context(), input{Title: "Acme Ltd"}, core.TypedMutationOptions{ExpectedRevision: first.Revision})
	if err != nil || renamed.Title == nil || *renamed.Title != "Acme Ltd" {
		t.Fatalf("global publish changes = %#v, %v", renamed, err)
	}
}
