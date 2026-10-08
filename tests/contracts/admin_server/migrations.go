package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/internal/migrationartifact"
)

const fixtureMigrationName = "browser-fixture-initial"

// postgresFixtureHistoryDigestVariable selects production startup. Like a
// server built by `ridu build`, the process then never resets or migrates its
// schema: it verifies the database against this executable migration history
// before serving, exactly as ridu.Execute does.
const postgresFixtureHistoryDigestVariable = "RIDU_POSTGRES_FIXTURE_HISTORY_DIGEST"

// runFixtureCommand mirrors a generated application's deployment steps.
// `migrations` writes the committed artifact history and prints the digest
// `ridu build` would link; `migrate` applies that history like `ridu migrate up`.
func runFixtureCommand(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 2 || args[1] == "" {
		return fmt.Errorf("usage: admin_server migrations <directory> | admin_server migrate <directory>")
	}
	switch args[0] {
	case "migrations":
		if err := writeFixtureMigration(ctx, fixtureConfig(nil), args[1]); err != nil {
			return err
		}
		files, err := migrationartifact.ReadAll(args[1])
		if err != nil {
			return err
		}
		digest, err := migrationartifact.HistoryDigest(files)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, digest)
		return err
	case "migrate":
		return migratePostgresFixture(ctx, args[1])
	default:
		return fmt.Errorf("unknown fixture command %q; use migrations or migrate", args[0])
	}
}

// writeFixtureMigration writes the deterministic initial PostgreSQL artifact
// for the fixture configuration into an empty migration directory.
func writeFixtureMigration(ctx context.Context, config ridu.Config, directory string) error {
	manifest, err := ridu.Resolve(config)
	if err != nil {
		return err
	}
	artifact, err := postgres.BuildArtifact(ctx, fixtureMigrationName, nil, manifest, postgres.ArtifactOptions{})
	if err != nil {
		return err
	}
	_, err = migrationartifact.Create(directory, fixtureMigrationName, artifact, time.Unix(1, 0))
	return err
}

// migratePostgresFixture recreates the named fixture schema and applies the
// artifact history. The schema is retained for a production-startup server.
func migratePostgresFixture(ctx context.Context, directory string) error {
	baseDatabaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if baseDatabaseURL == "" {
		return errors.New("migrate requires RIDU_POSTGRES_URL")
	}
	if os.Getenv("RIDU_POSTGRES_FIXTURE_SCHEMA") == "" {
		return errors.New("migrate requires RIDU_POSTGRES_FIXTURE_SCHEMA so the server can open the migrated schema")
	}
	schemaName, err := postgresFixtureSchema()
	if err != nil {
		return err
	}
	releaseLock, err := acquirePostgresFixtureLock(ctx, baseDatabaseURL, schemaName)
	if err != nil {
		return err
	}
	defer releaseLock()
	if err := resetPostgresFixture(ctx, baseDatabaseURL, schemaName); err != nil {
		return err
	}
	databaseURL, err := postgresFixtureURL(baseDatabaseURL, schemaName)
	if err != nil {
		return err
	}
	backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: databaseURL, AllowInsecureTransport: true})
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.ApplyArtifacts(ctx, directory)
}
