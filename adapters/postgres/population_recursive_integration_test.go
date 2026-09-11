package postgres_test

import (
	"context"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestPostgresRecursivePopulationTraversesNestedShapesWithAccessAndRedaction(t *testing.T) {
	ctx := context.Background()
	publicPath, _ := query.NewPath("public")
	config := ridu.Config{Name: "PostgreSQL recursive population", Collections: []ridu.Collection{
		{
			Slug: "people", Fields: field.Fields{field.Text("name").Required(), field.Checkbox("public").Required(), field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) {
				return false, nil
			}})},
			Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Where(query.Equal(publicPath, query.Boolean(true))), nil
			}},
		},
		{Slug: "teams", Fields: field.Fields{field.Text("name").Required(), field.Relationship("owner", "people")}},
		{Slug: "entries", Fields: field.Fields{field.Group("meta", field.Fields{field.Relationship("reviewer", "people")}), field.Array("sections", field.Fields{field.Relationship("reviewer", "people")}), field.Blocks("layout", field.Block{Slug: "quote", Fields: field.Fields{field.Relationship("source", "teams")}})}},
	}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := application.Local().Create(ctx, "people", store.Values{
		"name": store.String("Visible"), "public": store.Boolean(true), "secret": store.String("nested-secret"),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	laterHidden, err := application.Local().Create(ctx, "people", store.Values{
		"name": store.String("Later hidden"), "public": store.Boolean(true), "secret": store.String("hidden-secret"),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	team, err := application.Local().Create(ctx, "teams", store.Values{"name": store.String("Core"), "owner": store.String(visible.ID)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := application.Local().Create(ctx, "entries", store.Values{
		"meta": store.Object(store.Values{"reviewer": store.String(visible.ID)}),
		"sections": store.List(
			store.Object(store.Values{"_key": store.String("one"), "reviewer": store.String(visible.ID)}),
			store.Object(store.Values{"_key": store.String("two"), "reviewer": store.String(laterHidden.ID)}),
		),
		"layout": store.List(store.Object(store.Values{
			"_key": store.String("quote-one"), "blockType": store.String("quote"), "source": store.String(team.ID),
		})),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "people", laterHidden.ID, store.Values{"public": store.Boolean(false)}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}

	metaReviewer, _ := query.NewPath("meta", "reviewer")
	sectionReviewer, _ := query.NewPath("sections", "reviewer")
	blockSource, _ := query.NewPath("layout", "quote", "source")
	result, err := application.Local().Find(ctx, "entries", entry.ID, ridu.FindOptions{Populate: []query.Population{
		{Path: metaReviewer}, {Path: sectionReviewer}, {Path: blockSource, Depth: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := result.Values["meta"].CopyObject()
	reviewer, populated := meta["reviewer"].CopyDocument()
	if !populated || reviewer.ID != visible.ID {
		t.Fatalf("PostgreSQL group population = %#v", meta["reviewer"])
	}
	if _, leaked := reviewer.Values["secret"]; leaked {
		t.Fatal("PostgreSQL nested group population leaked a redacted field")
	}
	sections, _ := result.Values["sections"].CopyList()
	first, _ := sections[0].CopyObject()
	if nested, populated := first["reviewer"].CopyDocument(); !populated || nested.ID != visible.ID {
		t.Fatalf("PostgreSQL array population = %#v", first["reviewer"])
	}
	second, _ := sections[1].CopyObject()
	if _, populated := second["reviewer"].CopyDocument(); populated {
		t.Fatalf("PostgreSQL access-filtered nested target was populated: %#v", second["reviewer"])
	}
	layout, _ := result.Values["layout"].CopyList()
	quote, _ := layout[0].CopyObject()
	populatedTeam, populated := quote["source"].CopyDocument()
	if !populated || populatedTeam.ID != team.ID {
		t.Fatalf("PostgreSQL block population = %#v", quote["source"])
	}
	owner, populated := populatedTeam.Values["owner"].CopyDocument()
	if !populated || owner.ID != visible.ID {
		t.Fatalf("PostgreSQL depth-two nested target = %#v", populatedTeam.Values["owner"])
	}
	if _, leaked := owner.Values["secret"]; leaked {
		t.Fatal("PostgreSQL depth-two population leaked a redacted field")
	}
}
