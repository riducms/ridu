package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/store"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

func TestSQLiteRichTextBlocksAcceptance(t *testing.T) {
	richtextblocks.Run(t, sqliteRichTextBlocksFactory)
}

func TestSQLiteRichTextBlockReferencesAcceptance(t *testing.T) {
	richtextblocks.RunReferences(t, sqliteRichTextBlocksFactory)
}

func TestSQLiteRichTextBlockNamesAcceptance(t *testing.T) {
	richtextblocks.RunNames(t, sqliteRichTextBlocksFactory)
}

func TestSQLiteRichTextBlocksPerformance(t *testing.T) {
	richtextblocks.RunPerformance(t, "sqlite", sqliteRichTextBlocksFactory)
}

func sqliteRichTextBlocksFactory(t *testing.T, config ridu.Config) (store.Store, *ridu.App) {
	t.Helper()
	backend, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "blocks.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(t.Context(), app.Manifest()); err != nil {
		t.Fatal(err)
	}
	return backend, app
}
