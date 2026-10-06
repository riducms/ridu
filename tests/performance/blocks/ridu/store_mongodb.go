//go:build blocksmongodb

// The blocksmongodb build links only the MongoDB adapter, like a generated MongoDB application.

package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const databaseAdapter = "mongodb"

func openMongo(ctx context.Context) (*mongodb.Store, error) {
	url := os.Getenv("RIDU_BLOCKS_DATABASE_URL")
	if url == "" {
		return nil, errors.New("RIDU_BLOCKS_DATABASE_URL is required")
	}
	return mongodb.OpenWithConfig(ctx, mongodb.Config{DatabaseURL: url, AllowInsecureTransport: true, ApplicationName: "ridu-blocks-benchmark"})
}

func openStore(ctx context.Context) (store.Store, func(), error) {
	backend, err := openMongo(ctx)
	if err != nil {
		return nil, nil, err
	}
	return backend, func() { _ = backend.Close() }, nil
}

func writeMigrations(ctx context.Context, config ridu.Config, directory string) (string, error) {
	manifest, err := ridu.Resolve(config)
	if err != nil {
		return "", err
	}
	if _, err := mongodb.CreateArtifact(ctx, directory, "blocks-initial", manifest, time.Unix(1, 0), mongodb.ArtifactOptions{}); err != nil {
		return "", err
	}
	return historyDigest(directory)
}

func applyMigrations(ctx context.Context, _ ridu.Config, directory string) error {
	backend, err := openMongo(ctx)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.ApplyArtifacts(ctx, directory)
}

func verifyMigrations(ctx context.Context, backend store.Store, manifest schema.Manifest, digest string) error {
	return backend.(*mongodb.Store).ReadyWithMigrationHistory(ctx, manifest, digest)
}
