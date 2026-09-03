package postgres_test

import (
	"context"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestPostgresRecursivePopulationTraversesNestedShapesWithAccessAndRedaction(t *testing.T) {
	ctx := context.Background()
	publicPath, _ := query.NewPath("public")
	config := ridu.Config{Name: "PostgreSQL recursive population", Collections: []ridu.Collection{
		{
			Slug: "people", Fields: []field.Definition{
				field.Text("name", field.Required()), field.Checkbox("public", field.Required()), field.Text("secret"),
			},
			Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Where(query.Equal(publicPath, query.Boolean(true))), nil
			}},
			FieldAccess: map[string]ridu.FieldAccess{"secret": {Read: func(ridu.FieldAccessContext) (bool, error) { return false, nil }}},
		},
		{Slug: "teams", Fields: []field.Definition{
			field.Text("name", field.Required()), field.Relationship("owner", field.To("people")),
		}},
		{Slug: "entries", Fields: []field.Definition{
			field.Group("meta", field.Fields(field.Relationship("reviewer", field.To("people")))),
			field.Array("sections", field.Fields(field.Relationship("reviewer", field.To("people")))),
			field.Blocks("layout", field.BlockTypes(field.BlockType("quote", "Quote", field.Relationship("source", field.To("teams"))))),
		}},
	}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := application.Local().Create(ctx, "people", store.Values{
		"name": store.String("Visible"), "public": store.Boolean(true), "secret": store.String("nested-secret"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	laterHidden, err := application.Local().Create(ctx, "people", store.Values{
		"name": store.String("Later hidden"), "public": store.Boolean(true), "secret": store.String("hidden-secret"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	team, err := application.Local().Create(ctx, "teams", store.Values{"name": store.String("Core"), "owner": store.String(visible.ID)}, nil)
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
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "people", laterHidden.ID, store.Values{"public": store.Boolean(false)}, nil); err != nil {
		t.Fatal(err)
	}

	metaReviewer, _ := query.NewPath("meta", "reviewer")
	sectionReviewer, _ := query.NewPath("sections", "reviewer")
	blockSource, _ := query.NewPath("layout", "quote", "source")
	result, err := application.Local().FindWithOptions(ctx, "entries", entry.ID, ridu.FindOptions{Populate: []query.Population{
		{Path: metaReviewer}, {Path: sectionReviewer}, {Path: blockSource, Depth: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := result.Values["meta"].ObjectValue()
	reviewer, populated := meta["reviewer"].DocumentValue()
	if !populated || reviewer.ID != visible.ID {
		t.Fatalf("PostgreSQL group population = %#v", meta["reviewer"])
	}
	if _, leaked := reviewer.Values["secret"]; leaked {
		t.Fatal("PostgreSQL nested group population leaked a redacted field")
	}
	sections, _ := result.Values["sections"].Values()
	first, _ := sections[0].ObjectValue()
	if nested, populated := first["reviewer"].DocumentValue(); !populated || nested.ID != visible.ID {
		t.Fatalf("PostgreSQL array population = %#v", first["reviewer"])
	}
	second, _ := sections[1].ObjectValue()
	if _, populated := second["reviewer"].DocumentValue(); populated {
		t.Fatalf("PostgreSQL access-filtered nested target was populated: %#v", second["reviewer"])
	}
	layout, _ := result.Values["layout"].Values()
	quote, _ := layout[0].ObjectValue()
	populatedTeam, populated := quote["source"].DocumentValue()
	if !populated || populatedTeam.ID != team.ID {
		t.Fatalf("PostgreSQL block population = %#v", quote["source"])
	}
	owner, populated := populatedTeam.Values["owner"].DocumentValue()
	if !populated || owner.ID != visible.ID {
		t.Fatalf("PostgreSQL depth-two nested target = %#v", populatedTeam.Values["owner"])
	}
	if _, leaked := owner.Values["secret"]; leaked {
		t.Fatal("PostgreSQL depth-two population leaked a redacted field")
	}
}
