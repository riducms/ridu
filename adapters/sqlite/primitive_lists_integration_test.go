package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/tests/contracts/primitivelists"
)

func TestPrimitiveListsSQLiteLifecycle(t *testing.T) {
	config := primitivelists.Config()
	config.Collections[0].Access.Publish = config.Collections[0].Access.Update
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "lists.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	primitivelists.Exercise(t, app)
}

func TestPrimitiveListsSQLiteRepeatedQueries(t *testing.T) {
	config := primitivelists.RepeatedConfig()
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "list-queries.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	primitivelists.ExerciseRepeatedQueries(t, app)
}
