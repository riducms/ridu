package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu/store"
)

func TestRotateSessionSerializesWithOwnerDeletion(t *testing.T) {
	ctx := context.Background()
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	schemaName := fmt.Sprintf("ridu_auth_barrier_%d", time.Now().UnixNano())
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+quote(schemaName)); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), `DROP SCHEMA `+quote(schemaName)+` CASCADE`); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		admin.Close()
	})

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	applicationName := "ridu-auth-barrier-" + schemaName
	parameters.Set("application_name", applicationName)
	parsed.RawQuery = parameters.Encode()
	pool, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	backend := &Store{pool: pool}
	collectionID := store.DocumentReference{CollectionID: "users", DocumentID: "user-1"}
	table := quote(collectionTable(collectionID.CollectionID))
	if _, err := pool.Exec(ctx, `CREATE TABLE `+table+` (id text PRIMARY KEY, deleted_at timestamptz)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE ridu_auth_sessions (
id text PRIMARY KEY, token_hash text UNIQUE NOT NULL, collection_id text NOT NULL,
user_id text NOT NULL, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL,
last_seen_at timestamptz NOT NULL, ip_address text NOT NULL, user_agent text NOT NULL
)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO `+table+` (id) VALUES ($1)`, collectionID.DocumentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ridu_auth_sessions
(id, token_hash, collection_id, user_id, expires_at, created_at, last_seen_at, ip_address, user_agent)
VALUES ('session-1', 'current', $1, $2, $3, $4, $4, '', '')`, collectionID.CollectionID, collectionID.DocumentID, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}

	deletion, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deletion.Rollback(ctx) }()
	if _, err := deletion.Exec(ctx, `SELECT 1 FROM `+table+` WHERE id = $1 FOR UPDATE`, collectionID.DocumentID); err != nil {
		t.Fatal(err)
	}

	rotationResult := make(chan error, 1)
	go func() {
		rotationResult <- backend.RotateSession(ctx, "current", store.AuthSession{
			TokenHash: "replacement", CollectionID: collectionID.CollectionID, UserID: collectionID.DocumentID,
		}, now)
	}()
	waitForPostgresLock(t, ctx, admin, applicationName)
	if _, err := deletion.Exec(ctx, `DELETE FROM ridu_auth_sessions WHERE collection_id = $1 AND user_id = $2`, collectionID.CollectionID, collectionID.DocumentID); err != nil {
		t.Fatal(err)
	}
	if _, err := deletion.Exec(ctx, `DELETE FROM `+table+` WHERE id = $1`, collectionID.DocumentID); err != nil {
		t.Fatal(err)
	}
	if err := deletion.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-rotationResult:
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("rotation after concurrent owner deletion = %v, want store.ErrNotFound", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session rotation did not finish after owner deletion committed")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ridu_auth_sessions WHERE token_hash = 'replacement'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("replacement session survived owner deletion")
	}
}

func TestCreateAPIKeySerializesWithAuthorizingSessionRevocation(t *testing.T) {
	ctx := context.Background()
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	schemaName := fmt.Sprintf("ridu_api_key_barrier_%d", time.Now().UnixNano())
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+quote(schemaName)); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), `DROP SCHEMA `+quote(schemaName)+` CASCADE`); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		admin.Close()
	})

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	applicationName := "ridu-api-key-barrier-" + schemaName
	parameters.Set("application_name", applicationName)
	parsed.RawQuery = parameters.Encode()
	pool, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	backend := &Store{pool: pool}
	owner := store.DocumentReference{CollectionID: "users", DocumentID: "user-1"}
	table := quote(collectionTable(owner.CollectionID))
	for _, statement := range []string{
		`CREATE TABLE ` + table + ` (id text PRIMARY KEY, deleted_at timestamptz)`,
		`CREATE TABLE ridu_auth_credentials (
collection_id text NOT NULL, user_id text NOT NULL, password_hash bytea NOT NULL,
failed_login_attempts integer NOT NULL DEFAULT 0, locked_until timestamptz,
verified boolean NOT NULL DEFAULT true, PRIMARY KEY (collection_id, user_id)
)`,
		`CREATE TABLE ridu_auth_sessions (
id text PRIMARY KEY, token_hash text UNIQUE NOT NULL, collection_id text NOT NULL,
user_id text NOT NULL, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL,
last_seen_at timestamptz NOT NULL, ip_address text NOT NULL, user_agent text NOT NULL
)`,
		`CREATE TABLE ridu_auth_api_keys (
id text PRIMARY KEY, token_hash text UNIQUE NOT NULL, collection_id text NOT NULL,
user_id text NOT NULL, name text NOT NULL, created_at timestamptz NOT NULL,
last_used_at timestamptz, expires_at timestamptz
)`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO `+table+` (id) VALUES ($1)`, owner.DocumentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ridu_auth_credentials
(collection_id, user_id, password_hash) VALUES ($1, $2, $3)`, owner.CollectionID, owner.DocumentID, []byte("hash")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ridu_auth_sessions
(id, token_hash, collection_id, user_id, expires_at, created_at, last_seen_at, ip_address, user_agent)
VALUES ('session-1', 'current', $1, $2, $3, $4, $4, '', '')`, owner.CollectionID, owner.DocumentID, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}

	revocation, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = revocation.Rollback(ctx) }()
	if _, err := revocation.Exec(ctx, `DELETE FROM ridu_auth_sessions WHERE token_hash = 'current'`); err != nil {
		t.Fatal(err)
	}
	creationResult := make(chan error, 1)
	go func() {
		creationResult <- backend.CreateAPIKey(ctx, store.AuthAPIKey{
			ID: "late-key", TokenHash: "late-key-hash", CollectionID: owner.CollectionID,
			UserID: owner.DocumentID, Name: "Late key", CreatedAt: now,
		}, "current", now)
	}()
	waitForPostgresLock(t, ctx, admin, applicationName)
	if err := revocation.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-creationResult:
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("API-key creation after concurrent session revocation = %v, want store.ErrNotFound", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("API-key creation did not finish after session revocation committed")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ridu_auth_api_keys`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("API key survived authorizing-session revocation")
	}
}

func waitForPostgresLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, applicationName string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM pg_stat_activity
WHERE datname = current_database() AND application_name = $1 AND wait_event_type = 'Lock'
  AND cardinality(pg_blocking_pids(pid)) > 0 AND query LIKE '%FOR KEY SHARE%'
)`, applicationName).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("auth operation did not reach its PostgreSQL row-lock barrier")
}
