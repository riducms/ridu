package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/riducms/ridu/adapters/storage/local"
)

func TestFixtureMigrationCommandsPrepareProductionStartup(t *testing.T) {
	baseDatabaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if baseDatabaseURL == "" {
		t.Skip("RIDU_POSTGRES_URL is required for the PostgreSQL production-startup contract")
	}
	const schemaName = "ridu_admin_fixture_migration_commands"
	t.Setenv("RIDU_POSTGRES_FIXTURE_SCHEMA", schemaName)
	t.Cleanup(func() {
		if err := dropPostgresFixture(context.Background(), baseDatabaseURL, schemaName); err != nil {
			t.Errorf("drop fixture schema: %v", err)
		}
	})
	directory := t.TempDir()
	var output bytes.Buffer
	if err := runFixtureCommand(t.Context(), []string{"migrations", directory}, &output); err != nil {
		t.Fatal(err)
	}
	historyDigest := strings.TrimSpace(output.String())
	if len(historyDigest) != 64 {
		t.Fatalf("history digest = %q", historyDigest)
	}
	if err := runFixtureCommand(t.Context(), []string{"migrate", directory}, &output); err != nil {
		t.Fatal(err)
	}
	databaseURL, err := postgresFixtureURL(baseDatabaseURL, schemaName)
	if err != nil {
		t.Fatal(err)
	}
	uploadStorage, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := fixtureConfig(uploadStorage)
	if _, closeBackend, err := fixtureApplicationHandler(t.Context(), config, false, nil, databaseURL, "", "", strings.Repeat("0", 64)); err == nil {
		closeBackend()
		t.Fatal("production startup accepted a foreign migration history")
	} else if !strings.Contains(err.Error(), "production readiness preflight") {
		t.Fatalf("foreign history error = %v", err)
	}
	handler, closeBackend, err := fixtureApplicationHandler(t.Context(), config, false, nil, databaseURL, "", "", historyDigest)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend()
	if handler == nil {
		t.Fatal("production startup returned no handler")
	}
}
