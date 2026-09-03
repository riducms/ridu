package typescript_test

import (
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/typescript"
)

func TestGeneratedCreateContractAllowsServerGeneratedSlug(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{Name: "Slug contracts", Collections: []ridu.Collection{{
		Slug: "posts",
		Fields: []field.Definition{
			field.Text("title", field.Required()),
			field.Slug("slug", "title"),
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	if !strings.Contains(text, "export interface PostsCreate {\n\t\"title\": string;\n\t\"slug\"?: string;\n}") {
		t.Fatalf("generated create contract does not allow a server-generated slug:\n%s", text)
	}
	if !strings.Contains(text, "export interface PostsUpdate {\n\t\"title\"?: string;\n\t\"slug\"?: string;\n}") {
		t.Fatalf("generated update contract does not allow slug regeneration:\n%s", text)
	}
}
