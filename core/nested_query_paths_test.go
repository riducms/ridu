package core_test

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
	nestedpaths "github.com/riducms/ridu/tests/contracts/nested_paths"
)

func TestLocalNestedQueryPaths(t *testing.T) {
	nestedpaths.Run(t, func(t *testing.T, config ridu.Config) (store.Store, *ridu.App) {
		t.Helper()
		backend := teststore.New()
		app, err := ridu.New(config, backend)
		if err != nil {
			t.Fatal(err)
		}
		return backend, app
	})
}
