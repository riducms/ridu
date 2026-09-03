package ridu_test

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

func TestRootFacadeResolvesCoreConfig(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Facade contract",
		Collections: []ridu.Collection{{
			Slug: "posts",
			Fields: []field.Definition{
				field.Text("title"),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Snapshot().Application.Name != "Facade contract" {
		t.Fatalf("application name = %q", manifest.Snapshot().Application.Name)
	}
}
