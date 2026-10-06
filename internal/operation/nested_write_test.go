package operation

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// nestedWriteEngine serves a draft-enabled collection whose before-change hook
// runs nested once, inside the outer operation's transaction.
func nestedWriteEngine(t *testing.T, nested *func(Context) error) *Engine {
	t.Helper()
	field := func(name string) schema.Field {
		return schema.Field{ID: schema.StableID("field-" + name), Name: name, Path: mustPopulationPath(t, name), Type: schema.FieldTypeText}
	}
	running := false
	hook := func(ctx Context) error {
		if *nested == nil || running {
			return nil
		}
		running = true
		defer func() { running = false }()
		return (*nested)(ctx)
	}
	engine, err := New(Config{Store: teststore.New(), Collections: []Collection{{
		Schema: schema.Collection{
			ID: "collection-posts", Slug: "posts", Capabilities: schema.Capabilities{Versions: true},
			Versions: &schema.VersionSettings{Drafts: true}, Fields: []schema.Field{field("title"), field("summary")},
		},
		Hooks: Hooks{BeforeChange: []Hook{hook}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func nestedWriteRead(t *testing.T, engine *Engine, id string, draft bool) *store.Document {
	t.Helper()
	result, err := engine.Execute(context.Background(), Request{Operation: operation.Read, Collection: "posts", ID: id, Draft: &draft, System: true})
	if err != nil {
		var failure *Error
		if !draft && errors.As(err, &failure) && failure.Code == "not_found" {
			return nil
		}
		t.Fatal(err)
	}
	return result.Document
}

func nestedWriteText(document *store.Document, name string) string {
	text, _ := document.Values[name].StringValue()
	return text
}

// Hooks receive the operation's transaction, so a nested operation can write
// the document the outer operation locked. The outer write must apply on top
// of that nested write: both persist, revisions advance once per write, each
// write records its own version, and the caller's expected revision still
// names the document as the outer operation locked it.
func TestNestedSameDocumentWritesPersistUnderTheOuterWrite(t *testing.T) {
	published, draft := false, true
	for name, scenario := range map[string]struct {
		seedPublished bool
		outer         Request
		nested        Request
		status        store.Status
		live          map[string]string // nil when no live head remains
		working       map[string]string
		pending       bool
	}{
		"update inside update": {
			outer:   Request{Operation: operation.Update, Data: store.Values{"title": store.String("outer")}},
			nested:  Request{Operation: operation.Update, Data: store.Values{"summary": store.String("nested")}},
			status:  store.StatusDraft,
			working: map[string]string{"title": "outer", "summary": "nested"},
		},
		// The rebased row is published, so the outer edit must be an explicit
		// draft save, exactly as for an update that started on a published row.
		"publish inside draft update": {
			outer:   Request{Operation: operation.Update, Draft: &draft, Data: store.Values{"title": store.String("outer")}},
			nested:  Request{Operation: operation.Publish, Data: store.Values{"summary": store.String("nested")}},
			status:  store.StatusPublished,
			live:    map[string]string{"title": "seed", "summary": "nested"},
			working: map[string]string{"title": "outer", "summary": "nested"},
			pending: true,
		},
		"unpublish inside draft update": {
			seedPublished: true,
			outer:         Request{Operation: operation.Update, Draft: &draft, Data: store.Values{"title": store.String("outer")}},
			nested:        Request{Operation: operation.Unpublish},
			status:        store.StatusDraft,
			working:       map[string]string{"title": "outer", "summary": "seed"},
		},
		"update inside publish": {
			outer:   Request{Operation: operation.Publish, Data: store.Values{"title": store.String("outer")}},
			nested:  Request{Operation: operation.Update, Data: store.Values{"summary": store.String("nested")}},
			status:  store.StatusPublished,
			live:    map[string]string{"title": "outer", "summary": "nested"},
			working: map[string]string{"title": "outer", "summary": "nested"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var nested func(Context) error
			engine := nestedWriteEngine(t, &nested)
			create := Request{Operation: operation.Create, Collection: "posts", System: true, Data: store.Values{"title": store.String("seed"), "summary": store.String("seed")}}
			if scenario.seedPublished {
				create.Draft = &published
			}
			seeded, err := engine.Execute(context.Background(), create)
			if err != nil {
				t.Fatal(err)
			}
			id, seedRevision := seeded.Document.ID, seeded.Document.Revision
			nestedRuns := 0
			nested = func(ctx Context) error {
				nestedRuns++
				request := scenario.nested
				request.Collection, request.ID, request.System = "posts", id, true
				_, err := engine.Execute(ctx.Context, request)
				return err
			}
			outer := scenario.outer
			outer.Collection, outer.ID, outer.System, outer.ExpectedRevision = "posts", id, true, seedRevision
			result, err := engine.Execute(context.Background(), outer)
			if err != nil {
				t.Fatalf("outer %s: %v", outer.Operation, err)
			}
			if nestedRuns != 1 {
				t.Fatalf("nested write ran %d times", nestedRuns)
			}
			nested = nil
			want := seedRevision + 2
			if result.Document.Revision != want {
				t.Fatalf("outer result revision = %d, want %d", result.Document.Revision, want)
			}
			working := nestedWriteRead(t, engine, id, true)
			if working.Revision != want || working.Status != scenario.status || working.HasDraftChanges != scenario.pending {
				t.Fatalf("working revision/status/pending = %d/%s/%t, want %d/%s/%t", working.Revision, working.Status, working.HasDraftChanges, want, scenario.status, scenario.pending)
			}
			for field, value := range scenario.working {
				if got := nestedWriteText(working, field); got != value {
					t.Fatalf("working %s = %q, want %q", field, got, value)
				}
			}
			live := nestedWriteRead(t, engine, id, false)
			if (live == nil) != (scenario.live == nil) {
				t.Fatalf("live head = %#v, want %v", live, scenario.live)
			}
			for field, value := range scenario.live {
				if got := nestedWriteText(live, field); got != value {
					t.Fatalf("live %s = %q, want %q", field, got, value)
				}
			}
			versions, err := engine.Versions(context.Background(), "posts", id, nil, LocalizationOptions{System: true})
			if err != nil {
				t.Fatal(err)
			}
			revisions := make([]int, 0, len(versions))
			for _, version := range versions {
				revisions = append(revisions, version.Revision)
			}
			slices.Sort(revisions)
			if !slices.Equal(revisions, []int{seedRevision, seedRevision + 1, seedRevision + 2}) {
				t.Fatalf("version revisions = %v", revisions)
			}
			for _, version := range versions {
				if version.Revision == want {
					for field, value := range scenario.working {
						if got := nestedWriteText(&version.Snapshot, field); got != value {
							t.Fatalf("outer version %s = %q, want %q", field, got, value)
						}
					}
				}
			}
		})
	}
}

// A stale expected revision fails against the locked row before any hook can
// write through the transaction.
func TestExpectedRevisionIsCheckedBeforeHooksRun(t *testing.T) {
	var nested func(Context) error
	engine := nestedWriteEngine(t, &nested)
	seeded, err := engine.Execute(context.Background(), Request{Operation: operation.Create, Collection: "posts", System: true, Data: store.Values{"title": store.String("seed")}})
	if err != nil {
		t.Fatal(err)
	}
	ran := false
	nested = func(Context) error { ran = true; return nil }
	_, err = engine.Execute(context.Background(), Request{
		Operation: operation.Update, Collection: "posts", ID: seeded.Document.ID, System: true,
		ExpectedRevision: seeded.Document.Revision + 1, Data: store.Values{"title": store.String("stale")},
	})
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != "conflict" {
		t.Fatalf("stale expected revision = %v", err)
	}
	if ran {
		t.Fatal("hooks ran for a stale expected revision")
	}
}

// Writing the document being saved from a validator or access rule would skip
// the rebase that hook writes receive, so the outer write fails instead.
func TestLateSameDocumentWriteFailsTheOuterWrite(t *testing.T) {
	var engine *Engine
	var id string
	nestedWrite := false
	field := func(name string) schema.Field {
		return schema.Field{ID: schema.StableID("field-" + name), Name: name, Path: mustPopulationPath(t, name), Type: schema.FieldTypeText}
	}
	collection := Collection{Schema: schema.Collection{ID: "collection-notes", Slug: "notes", Fields: []schema.Field{field("title"), field("summary")}}}
	collection.Bindings = []FieldBinding{{Field: collection.Schema.Fields[0], Validators: []FieldValidator{func(ctx Context) ([]schema.Issue, error) {
		if nestedWrite || ctx.Operation != operation.Update {
			return nil, nil
		}
		nestedWrite = true
		_, err := engine.Execute(ctx.Context, Request{Operation: operation.Update, Collection: "notes", ID: id, System: true, Data: store.Values{"summary": store.String("late")}})
		return nil, err
	}}}}
	var err error
	engine, err = New(Config{Store: teststore.New(), Collections: []Collection{collection}})
	if err != nil {
		t.Fatal(err)
	}
	seeded, err := engine.Execute(context.Background(), Request{Operation: operation.Create, Collection: "notes", System: true, Data: store.Values{"title": store.String("seed")}})
	if err != nil {
		t.Fatal(err)
	}
	id = seeded.Document.ID
	_, err = engine.Execute(context.Background(), Request{Operation: operation.Update, Collection: "notes", ID: id, System: true, Data: store.Values{"title": store.String("outer")}})
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != "conflict" {
		t.Fatalf("late same-document write = %v", err)
	}
	stored := nestedWriteReadNotes(t, engine, id)
	if nestedWriteText(stored, "title") != "seed" || nestedWriteText(stored, "summary") != "" {
		t.Fatalf("failed write left %v", stored.Values)
	}
}

func nestedWriteReadNotes(t *testing.T, engine *Engine, id string) *store.Document {
	t.Helper()
	result, err := engine.Execute(context.Background(), Request{Operation: operation.Read, Collection: "notes", ID: id, System: true})
	if err != nil {
		t.Fatal(err)
	}
	return result.Document
}
