package core_test

import (
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

// A rule spanning two fields must see both on a partial update, where Data
// holds only what the caller sent.
func TestCandidateCombinesSubmittedAndUnchangedFields(t *testing.T) {
	type seen struct {
		data      []string
		candidate store.Values
	}
	var observed []seen
	record := func(ctx ridu.HookContext) error {
		if ctx.Operation != operation.Create && ctx.Operation != operation.Update {
			return nil
		}
		names := make([]string, 0, len(ctx.Data))
		for name := range ctx.Data {
			names = append(names, name)
		}
		observed = append(observed, seen{data: names, candidate: ctx.Candidate()})
		ctx.Candidate()["status"] = store.String("tampered")
		return nil
	}
	app, err := ridu.New(ridu.Config{Name: "Candidate", Collections: []ridu.Collection{{
		Slug:   "memberships",
		Fields: field.Fields{field.Text("league"), field.Text("status"), field.Group("seo", field.Fields{field.Text("title"), field.Text("summary")})},
		Hooks:  ridu.CollectionHooks{BeforeChange: []ridu.Hook{record}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	created, err := local.Create(t.Context(), "memberships", store.Values{
		"league": store.String("acts"), "status": store.String("invited"),
		"seo": store.Object(store.Values{"title": store.String("Acts"), "summary": store.String("Weekly")}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := local.Update(t.Context(), "memberships", created.ID, store.Values{
		"status": store.String("active"),
		"seo":    store.Object(store.Values{"title": store.String("Acts Club")}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(observed) != 2 {
		t.Fatalf("hook ran %d times, want 2", len(observed))
	}
	update := observed[1]
	if _, sent := update.candidate["league"]; !sent || len(update.data) != 2 {
		t.Fatalf("update Data = %v, Candidate = %v; want Data to hold only the sent fields and Candidate all of them", update.data, update.candidate)
	}
	if league, _ := update.candidate["league"].StringValue(); league != "acts" {
		t.Fatalf("Candidate league = %q, want the unchanged value", league)
	}
	if status, _ := update.candidate["status"].StringValue(); status != "active" {
		t.Fatalf("Candidate status = %q, want the submitted value", status)
	}
	// Candidate sees the group exactly as the engine will save it.
	seo, _ := update.candidate["seo"].CopyObject()
	if title, _ := seo["title"].StringValue(); title != "Acts Club" {
		t.Fatalf("Candidate seo.title = %q", title)
	}
	stored, _ := updated.Values["seo"].CopyObject()
	if summary, _ := stored["summary"].StringValue(); summary != "Weekly" {
		t.Fatalf("stored seo.summary = %q; the partial group update dropped a retained child", summary)
	}
	if summary, _ := seo["summary"].StringValue(); summary != "Weekly" {
		t.Fatalf("Candidate seo.summary = %q, want the retained child the engine saves", summary)
	}
	if status, _ := updated.Values["status"].StringValue(); status != "active" {
		t.Fatalf("changing the Candidate map changed the saved status to %q", status)
	}
}
