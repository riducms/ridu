//go:build postgresonly

// The postgresonly build links only the PostgreSQL adapter, like a generated
// PostgreSQL application. The performance comparison uses it so the measured
// binary does not carry the SQLite and MongoDB adapters the fixture's browser
// suites select at runtime.

package main

import (
	"context"
	"errors"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

var errPostgresOnlyFixture = errors.New("the postgresonly fixture build supports only RIDU_POSTGRES_URL")

func openMongoFixtureBackend(context.Context, ridu.Config, string) (store.Store, func(), error) {
	return nil, nil, errPostgresOnlyFixture
}

func openSQLiteFixtureBackend(context.Context, ridu.Config, string) (store.Store, func(), error) {
	return nil, nil, errPostgresOnlyFixture
}

func mongoFixtureURL(string) (string, error) { return "", errPostgresOnlyFixture }

func resetMongoFixture(context.Context, string) error { return errPostgresOnlyFixture }
