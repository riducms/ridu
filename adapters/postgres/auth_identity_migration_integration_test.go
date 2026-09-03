package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresAuthIdentityPlannerUpgradeCanonicalizesActiveAndTrash(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	manifest, directory := applyLegacyPostgresAuthArtifact(t, ctx, backend)
	if err := backend.Ready(ctx, manifest); err == nil || !strings.Contains(err.Error(), "canonicalization artifact") {
		t.Fatalf("legacy auth planner readiness = %v", err)
	}
	collection := manifest.Snapshot().Collections[0]
	table := quote(collectionTable(collection.ID))
	identityColumn := quote(fieldColumn(collection.Fields[0].ID))
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	rawIdentity := " \tUser@Example.COM\u00a0"
	if _, err := backend.pool.Exec(ctx, `INSERT INTO `+table+`
  (id, created_at, updated_at, deleted_at, `+identityColumn+`)
VALUES ($1, $2, $2, NULL, $3), ($4, $2, $2, $2, $5)`,
		"user-active", now, rawIdentity, "user-trash", " USER@example.com "); err != nil {
		t.Fatal(err)
	}

	createPostgresAuthIdentityUpgrade(t, ctx, directory, manifest)
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("canonical auth planner readiness: %v", err)
	}
	var active, trash string
	if err := backend.pool.QueryRow(ctx, `SELECT `+identityColumn+` FROM `+table+` WHERE id = 'user-active'`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT `+identityColumn+` FROM `+table+` WHERE id = 'user-trash'`).Scan(&trash); err != nil {
		t.Fatal(err)
	}
	if active != "user@example.com" || trash != "user@example.com" {
		t.Fatalf("canonical identities = active %q, trash %q", active, trash)
	}
	indexName := "z_u_" + identifierHash(string(collection.ID)+":"+string(collection.Fields[0].ID))
	var indexDefinition string
	if err := backend.pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes
WHERE schemaname = current_schema() AND indexname = $1`, indexName).Scan(&indexDefinition); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(indexDefinition), "lower(") || !strings.Contains(indexDefinition, fieldColumn(collection.Fields[0].ID)) {
		t.Fatalf("canonical auth index = %s", indexDefinition)
	}
}

func TestPostgresAuthIdentityPlannerUpgradeCollisionRollsBackValuesIndexAndLedger(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	manifest, directory := applyLegacyPostgresAuthArtifact(t, ctx, backend)
	collection := manifest.Snapshot().Collections[0]
	table := quote(collectionTable(collection.ID))
	identityColumn := quote(fieldColumn(collection.Fields[0].ID))
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	if _, err := backend.pool.Exec(ctx, `INSERT INTO `+table+`
  (id, created_at, updated_at, deleted_at, `+identityColumn+`)
VALUES ($1, $2, $2, NULL, $3), ($4, $2, $2, NULL, $5)`,
		"user-1", now, "user@example.com", "user-2", " user@example.com "); err != nil {
		t.Fatal(err)
	}
	createPostgresAuthIdentityUpgrade(t, ctx, directory, manifest)

	err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true})
	if err == nil || !strings.Contains(err.Error(), "active collision in collection users") {
		t.Fatalf("canonical collision error = %v", err)
	}
	if strings.Contains(err.Error(), "user@example.com") {
		t.Fatalf("canonical collision leaked identity: %v", err)
	}
	var first, second string
	if err := backend.pool.QueryRow(ctx, `SELECT `+identityColumn+` FROM `+table+` WHERE id = 'user-1'`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT `+identityColumn+` FROM `+table+` WHERE id = 'user-2'`).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if first != "user@example.com" || second != " user@example.com " {
		t.Fatalf("failed migration rewrote identities = %q, %q", first, second)
	}
	indexName := "z_u_" + identifierHash(string(collection.ID)+":"+string(collection.Fields[0].ID))
	var indexDefinition string
	if err := backend.pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes
WHERE schemaname = current_schema() AND indexname = $1`, indexName).Scan(&indexDefinition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(indexDefinition), "lower(") {
		t.Fatalf("failed migration changed legacy index: %s", indexDefinition)
	}
	var applied int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migrations`).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("failed migration ledger count = %d, %v", applied, err)
	}
}

func TestPostgresAuthIdentityPlannerUpgradeAddsNewAuthResourceAfterRetainedRewrite(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	before, directory := applyLegacyPostgresAuthArtifact(t, ctx, backend)
	users := before.Snapshot().Collections[0]
	usersTable := quote(collectionTable(users.ID))
	identityColumn := quote(fieldColumn(users.Fields[0].ID))
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	if _, err := backend.pool.Exec(ctx, `INSERT INTO `+usersTable+`
  (id, created_at, updated_at, deleted_at, `+identityColumn+`)
VALUES ($1, $2, $2, NULL, $3)`, "retained-user", now, " Retained@Example.Test "); err != nil {
		t.Fatal(err)
	}
	after := postgresAuthIdentityMigrationManifestWithAddedCollection()
	upgrade, err := BuildArtifactWithPreviousPlanner(ctx, "add-auth", &before, after, nil, true, atlasVersionV1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "add-auth", upgrade, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatalf("apply retained+new auth transition: %v", err)
	}
	var retained string
	if err := backend.pool.QueryRow(ctx, `SELECT `+identityColumn+` FROM `+usersTable+` WHERE id = 'retained-user'`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != "retained@example.test" {
		t.Fatalf("retained identity = %q", retained)
	}
	staff := after.Snapshot().Collections[1]
	var staffExists bool
	if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, collectionTable(staff.ID)).Scan(&staffExists); err != nil || !staffExists {
		t.Fatalf("new auth table exists = %t, %v", staffExists, err)
	}
	if err := backend.Ready(ctx, after); err != nil {
		t.Fatalf("mixed auth planner readiness: %v", err)
	}
}

func TestPostgresAuthIdentityCanonicalizationAtDirectStoreBoundary(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	manifest := postgresAuthIdentityMigrationManifest()
	directory := t.TempDir()
	initial, err := BuildArtifact(ctx, "canonical-auth", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "canonical-auth", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	createValues := store.Values{"email": store.String(" \tUser@Example.COM\u00a0")}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "canonical-user", Values: createValues})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := created.Values["email"].StringValue(); got != "user@example.com" {
		t.Fatalf("created identity = %q", got)
	}
	if got, _ := createValues["email"].StringValue(); got != " \tUser@Example.COM\u00a0" {
		t.Fatalf("Create mutated caller value to %q", got)
	}
	updateValues := store.Values{"email": store.String(" Next@Example.COM ")}
	updated, err := transaction.Update(ctx, store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: created.ID}, Values: updateValues,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := updated.Values["email"].StringValue(); got != "next@example.com" {
		t.Fatalf("updated identity = %q", got)
	}
	if got, _ := updateValues["email"].StringValue(); got != " Next@Example.COM " {
		t.Fatalf("Update mutated caller value to %q", got)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	assertPostgresAuthIdentityPair(t, backend, collection, "kelvin", "\u212a@example.test", "ascii-k", "K@example.test", true)
	assertPostgresAuthIdentityPair(t, backend, collection, "long-s", "\u017f@example.test", "ascii-s", "S@example.test", false)
	assertPostgresAuthIdentityPair(t, backend, collection, "sigma", "\u03a3@example.test", "final-sigma", "\u03c2@example.test", false)
	assertPostgresAuthIdentityPair(t, backend, collection, "dotted-i", "\u0130@example.test", "ascii-i", "I@example.test", true)
	assertPostgresAuthIdentityPair(t, backend, collection, "dotless-i", "\u0131@dotless.test", "ascii-i-distinct", "i@dotless.test", false)
}

func applyLegacyPostgresAuthArtifact(t *testing.T, ctx context.Context, backend *Store) (schema.Manifest, string) {
	t.Helper()
	manifest := postgresAuthIdentityMigrationManifest()
	directory := t.TempDir()
	legacy, _ := atlasPlannerContractFor(atlasVersionV1)
	initial, err := buildArtifactWithPlannerContracts(ctx, "legacy-auth", nil, manifest, nil, false, legacy, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "legacy-auth", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	return manifest, directory
}

func createPostgresAuthIdentityUpgrade(t *testing.T, ctx context.Context, directory string, manifest schema.Manifest) ridumigration.Artifact {
	t.Helper()
	upgrade, err := BuildArtifactWithPreviousPlanner(ctx, "canonical-auth", &manifest, manifest, nil, true, atlasVersionV1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "canonical-auth", upgrade, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	return upgrade
}

func assertPostgresAuthIdentityPair(t *testing.T, backend *Store, collection schema.Collection, firstID, firstIdentity, secondID, secondIdentity string, conflict bool) {
	t.Helper()
	ctx := context.Background()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: firstID, Values: store.Values{"email": store.String(firstIdentity)}}); err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatal(err)
	}
	_, err = transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: secondID, Values: store.Values{"email": store.String(secondIdentity)}})
	if conflict {
		if !errors.Is(err, store.ErrConflict) {
			_ = transaction.Rollback(ctx)
			t.Fatalf("second identity error = %v, want conflict", err)
		}
		if rollbackErr := transaction.Rollback(ctx); rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		return
	}
	if err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatalf("distinct second identity: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
