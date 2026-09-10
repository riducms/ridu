package postgres_test

import (
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/store"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

func TestPostgresRichTextBlocksAcceptance(t *testing.T) {
	richtextblocks.Run(t, postgresRichTextBlocksFactory)
}

func TestPostgresRichTextBlockReferencesAcceptance(t *testing.T) {
	richtextblocks.RunReferences(t, postgresRichTextBlocksFactory)
}

func TestPostgresRichTextBlocksPerformance(t *testing.T) {
	richtextblocks.RunPerformance(t, "postgres", postgresRichTextBlocksFactory)
}

func postgresRichTextBlocksFactory(t *testing.T, config ridu.Config) (store.Store, *ridu.App) {
	t.Helper()
	backend, manifest := integrationBackend(t, t.Context(), config)
	artifact, err := postgres.BuildArtifact(t.Context(), "richtext-blocks", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if _, err := migrationartifact.Create(directory, "richtext-blocks", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(t.Context(), directory); err != nil {
		t.Fatal(err)
	}
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	return backend, app
}
