package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestSQLiteStoreConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, manifest schema.Manifest) store.Store {
		backend, err := Open(t.Context(), filepath.Join(t.TempDir(), "conformance.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = backend.Close() })
		if err := backend.Migrate(t.Context(), manifest); err != nil {
			t.Fatal(err)
		}
		return backend
	})
}
