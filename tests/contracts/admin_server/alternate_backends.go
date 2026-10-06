//go:build !postgresonly

// The default fixture build also links the SQLite and MongoDB adapters so the
// browser suites can select any official store at runtime.

package main

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/mongodb"
	sqliteadapter "github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/store"
)

func openMongoFixtureBackend(ctx context.Context, config ridu.Config, mongoDatabaseURL string) (store.Store, func(), error) {
	backend, err := mongodb.OpenWithConfig(ctx, mongodb.Config{DatabaseURL: mongoDatabaseURL, AllowInsecureTransport: true})
	if err != nil {
		return nil, nil, err
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		_ = backend.Close()
		return nil, nil, err
	}
	if err := backend.SyncDevelopmentSchema(ctx, manifest); err != nil {
		_ = backend.Close()
		return nil, nil, err
	}
	return backend, func() { _ = backend.Close() }, nil
}

func openSQLiteFixtureBackend(ctx context.Context, config ridu.Config, sqlitePath string) (store.Store, func(), error) {
	backend, err := sqliteadapter.Open(ctx, sqlitePath)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		_ = backend.Close()
		return nil, nil, err
	}
	if err := backend.Migrate(ctx, manifest); err != nil {
		_ = backend.Close()
		return nil, nil, err
	}
	return backend, func() { _ = backend.Close() }, nil
}
