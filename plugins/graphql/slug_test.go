package graphql_test

import (
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
)

func TestGraphQLCreateInputAllowsServerGeneratedSlug(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{Name: "Slug GraphQL", Collections: []ridu.Collection{{
		Slug: "posts",
		Fields: []field.Definition{
			field.Text("title", field.Required()),
			field.Slug("slug", "title"),
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	sdl, err := graphqlplugin.GenerateSDL(manifest)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(sdl, "input PostCreateInput {")
	if start == -1 {
		t.Fatalf("GraphQL SDL has no post create input:\n%s", sdl)
	}
	end := strings.Index(sdl[start:], "}\n")
	if end == -1 {
		t.Fatalf("GraphQL post create input is not terminated:\n%s", sdl[start:])
	}
	input := sdl[start : start+end]
	if !strings.Contains(input, "title: String!") || !strings.Contains(input, "slug: String") || strings.Contains(input, "slug: String!") {
		t.Fatalf("GraphQL slug create contract = %q", input)
	}
}
