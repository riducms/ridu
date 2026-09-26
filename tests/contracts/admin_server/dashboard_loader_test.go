package main

import (
	"os"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/typescript"
)

func TestAdminLoaderGeneratedFixture(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{Name: "Admin loader fixture", Admin: ridu.AdminConfig{Loaders: []ridu.AdminLoaderDefinition{editorialDashboard, editorialView}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Text("title")}}}})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := "../admin_app/loaders.generated.ts"
	if os.Getenv("RIDU_UPDATE_ADMIN_LOADERS") == "1" {
		if err := os.WriteFile(path, generated, 0644); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(generated) {
		t.Fatal("Admin loader fixture drift: run RIDU_UPDATE_ADMIN_LOADERS=1 go test ./tests/contracts/admin_server -run TestAdminLoaderGeneratedFixture")
	}
}
