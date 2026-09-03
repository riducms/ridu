package postgres_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestPostgresStoreConformance(t *testing.T) {
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL store conformance")
	}
	conformance.Run(t, func(t *testing.T, manifest schema.Manifest) store.Store {
		return openPostgresConformanceStore(t, databaseURL, manifest)
	})
}

func openPostgresConformanceStore(t *testing.T, databaseURL string, manifest schema.Manifest) *postgres.Store {
	t.Helper()
	ctx := t.Context()
	schemaName := fmt.Sprintf("ridu_conformance_%d", time.Now().UnixNano())
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA "`+schemaName+`" CASCADE`)
		admin.Close()
	})

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	parsed.RawQuery = parameters.Encode()
	backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{
		DatabaseURL: parsed.String(), AllowInsecureTransport: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	plan, err := backend.Plan(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	return backend
}
