package postgres_test

import (
	"os"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/primitivelists"
)

func TestPrimitiveListsPostgresLifecycle(t *testing.T) {
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run isolated PostgreSQL primitive-list lifecycle")
	}
	config := primitivelists.Config()
	config.Collections[0].Access.Publish = config.Collections[0].Access.Update
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend := openPostgresConformanceStore(t, databaseURL, manifest)
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	primitivelists.Exercise(t, app)
}

func TestPrimitiveListsPostgresRepeatedQueries(t *testing.T) {
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run isolated PostgreSQL primitive-list repeated queries")
	}
	config := primitivelists.RepeatedConfig()
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend := openPostgresConformanceStore(t, databaseURL, manifest)
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	primitivelists.ExerciseRepeatedQueries(t, app)
}

func TestPrimitiveListsPostgresDefaultColumns(t *testing.T) {
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run isolated PostgreSQL primitive-list default columns")
	}
	config := core.Config{Name: "List defaults", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{field.TextList("points").Default("oak", "oak"), field.NumberList("sizes").Default(0, 0)}}}}
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend := openPostgresConformanceStore(t, databaseURL, manifest)
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "products", store.Values{}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	points, _ := created.Values["points"].CopyList()
	sizes, _ := created.Values["sizes"].CopyList()
	if len(points) != 2 || len(sizes) != 2 {
		t.Fatalf("defaults %#v", created.Values)
	}
}
