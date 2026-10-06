//go:build !blocksmongodb

// The default build links only the PostgreSQL adapter, like a generated PostgreSQL application.

package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const databaseAdapter = "postgres"

func openPostgres(ctx context.Context) (*postgres.Store, error) {
	url := os.Getenv("RIDU_BLOCKS_DATABASE_URL")
	if url == "" {
		return nil, errors.New("RIDU_BLOCKS_DATABASE_URL is required")
	}
	return postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: url, AllowInsecureTransport: true})
}

func openStore(ctx context.Context) (store.Store, func(), error) {
	backend, err := openPostgres(ctx)
	if err != nil {
		return nil, nil, err
	}
	return backend, backend.Close, nil
}

func writeMigrations(ctx context.Context, config ridu.Config, directory string) (string, error) {
	manifest, err := ridu.Resolve(config)
	if err != nil {
		return "", err
	}
	artifact, err := postgres.BuildArtifact(ctx, "blocks-initial", nil, manifest, nil, false)
	if err != nil {
		return "", err
	}
	if _, err := migrationartifact.Create(directory, "blocks-initial", artifact, time.Unix(1, 0)); err != nil {
		return "", err
	}
	return historyDigest(directory)
}

func applyMigrations(ctx context.Context, _ ridu.Config, directory string) error {
	backend, err := openPostgres(ctx)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.ApplyArtifacts(ctx, directory)
}

func verifyMigrations(ctx context.Context, backend store.Store, manifest schema.Manifest, digest string) error {
	return backend.(*postgres.Store).ReadyWithMigrationHistory(ctx, manifest, digest)
}
