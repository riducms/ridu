package blocktypes_test

import (
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/blocktypes"
	"github.com/riducms/ridu/schema"
)

func TestDraftReadsCoverReusedNestedFamiliesButNotAuthIdentity(t *testing.T) {
	inner := field.Block{Slug: "inner", TypeName: "AInner", Fields: field.Fields{field.Text("body").Required()}}
	outer := field.Block{Slug: "outer", TypeName: "ZOuter", Fields: field.Fields{field.Blocks("children", inner)}}
	manifest, err := core.Resolve(core.Config{Name: "Shared draft output", Admin: core.AdminConfig{User: "users"}, Collections: []core.Collection{
		{Slug: "static", Fields: field.Fields{field.Text("title").Required(), field.Blocks("layout", outer)}},
		{Slug: "editorial", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Blocks("layout", outer)}},
		{Slug: "users", Auth: true, Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Email("email").Required().Unique()}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	catalog, err := blocktypes.Build(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	incomplete := catalog.DraftReadFields(snapshot)
	for _, collection := range snapshot.Collections {
		for _, candidate := range collection.Fields {
			if candidate.Name == "title" || candidate.Name == "email" {
				if incomplete[candidate.ID] {
					t.Fatalf("strict field %s marked incomplete", candidate.ID)
				}
			}
		}
	}
	var assertChildren func([]schema.Field)
	assertChildren = func(fields []schema.Field) {
		for _, candidate := range fields {
			if !incomplete[candidate.ID] {
				t.Errorf("shared nested family field %s is not nullable", candidate.ID)
			}
			assertChildren(schema.ChildFields(candidate))
		}
	}
	for _, variant := range catalog.Variants {
		assertChildren(variant.Block.ResolvedFields())
	}
}
