package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestSQLiteAuthIdentityPlannerUpgradeRequiresDestructiveApproval(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteAuthMigrationManifest(t)
	directory := t.TempDir()
	legacy, _ := sqlitePlannerContractFor(sqlitePlannerVersionV1)
	initial, err := buildSQLiteArtifact(ctx, "initial", nil, manifest, "", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}

	if _, err := CreateArtifact(ctx, directory, "canonical-auth", manifest, time.Unix(2, 0), false); err == nil {
		t.Fatal("SQLite auth identity planner upgrade did not require destructive approval")
	} else {
		var safety *SafetyError
		if !errors.As(err, &safety) || !sqliteAuthIdentityRiskHasLevel(safety.Risks, ridumigration.RiskDestructive) {
			t.Fatalf("unapproved SQLite auth identity upgrade = %#v, %v", safety, err)
		}
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("unapproved SQLite auth identity upgrade published history = %d files, %v", len(files), err)
	}
	if _, err := CreateArtifact(ctx, directory, "canonical-auth", manifest, time.Unix(2, 0), true); err != nil {
		t.Fatalf("create approved SQLite auth identity upgrade: %v", err)
	}
	files, err = migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 2 || !sqliteAuthIdentityRiskHasLevel(files[1].Artifact.Risks, ridumigration.RiskDestructive) {
		t.Fatalf("approved SQLite auth identity upgrade history = %#v, %v", files, err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("apply explicitly approved SQLite auth identity upgrade: %v", err)
	}
	if _, err := CreateArtifact(ctx, directory, "unchanged", manifest, time.Unix(3, 0), false); err == nil {
		t.Fatal("unchanged current-planner manifest unexpectedly created an artifact")
	} else {
		var safety *SafetyError
		if errors.As(err, &safety) || !strings.Contains(err.Error(), "schema is current") {
			t.Fatalf("unchanged current-planner manifest = %#v, %v", safety, err)
		}
	}

	nonUpgradeDirectory := t.TempDir()
	if _, err := CreateArtifact(ctx, nonUpgradeDirectory, "initial", manifest, time.Unix(1, 0), false); err != nil {
		t.Fatalf("create ordinary current-planner auth artifact without destructive approval: %v", err)
	}
	additiveSnapshot := manifest.Snapshot()
	displayNamePath, _ := query.NewPath("displayName")
	additiveSnapshot.Collections[0].Fields = append(additiveSnapshot.Collections[0].Fields, schema.Field{
		ID: "users-display-name", Name: "displayName", Path: displayNamePath,
		Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
	})
	additive := schema.NewManifest(additiveSnapshot)
	if _, err := CreateArtifact(ctx, nonUpgradeDirectory, "add-display-name", additive, time.Unix(2, 0), false); err != nil {
		t.Fatalf("create non-upgrade additive artifact without destructive approval: %v", err)
	}
	nonUpgradeBackend := newSQLiteMigrationStore(t)
	if err := nonUpgradeBackend.ApplyArtifacts(ctx, nonUpgradeDirectory); err != nil {
		t.Fatalf("apply non-upgrade history: %v", err)
	}
}

func TestSQLitePendingV1ArtifactPreservesLegacyAuthUniquenessAndReadinessFailsClosed(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteAuthMigrationManifest(t)
	directory := t.TempDir()
	legacy, _ := sqlitePlannerContractFor(sqlitePlannerVersionV1)
	initial, err := buildSQLiteArtifact(ctx, "initial", nil, manifest, "", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}

	identities := []string{
		"user@example.test",
		" user@example.test ",
		"\u0130@example.test",
		"i@example.test",
	}
	for index, identity := range identities {
		values, err := json.Marshal(map[string]string{"email": identity})
		if err != nil {
			t.Fatal(err)
		}
		id := "user-" + string(rune('1'+index))
		if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, deleted_at, status, revision, values_json)
VALUES ('users', ?, 1, 1, NULL, '', 0, ?)`, id, string(values)); err != nil {
			t.Fatal(err)
		}
		legacyKey, err := json.Marshal(legacyAuthIdentityKey(identity))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_unique_values
  (collection_id, index_key, value_key, document_id)
VALUES ('users', 'field:users-email', ?, ?)`, string(legacyKey), id); err != nil {
			t.Fatal(err)
		}
	}

	additiveSnapshot := manifest.Snapshot()
	displayNamePath, _ := query.NewPath("displayName")
	additiveSnapshot.Collections[0].Fields = append(additiveSnapshot.Collections[0].Fields, schema.Field{
		ID: "users-display-name", Name: "displayName", Path: displayNamePath,
		Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
	})
	additive := schema.NewManifest(additiveSnapshot)
	pending, err := buildSQLiteArtifact(ctx, "pending-v1", &manifest, additive, sqlitePlannerVersionV1, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "pending-v1", pending, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("apply pending frozen v1 artifact: %v", err)
	}

	var keys int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_unique_values
WHERE collection_id = 'users' AND index_key = 'field:users-email'`).Scan(&keys); err != nil || keys != len(identities) {
		t.Fatalf("legacy auth unique keys = %d, %v", keys, err)
	}
	for index, identity := range identities {
		id := "user-" + string(rune('1'+index))
		var storedIdentity, storedKey string
		if err := backend.db.QueryRowContext(ctx, `SELECT json_extract(values_json, '$.email')
FROM ridu_documents WHERE collection_id = 'users' AND id = ?`, id).Scan(&storedIdentity); err != nil {
			t.Fatal(err)
		}
		if err := backend.db.QueryRowContext(ctx, `SELECT value_key FROM ridu_unique_values
WHERE collection_id = 'users' AND document_id = ?`, id).Scan(&storedKey); err != nil {
			t.Fatal(err)
		}
		expectedKey, _ := json.Marshal(legacyAuthIdentityKey(identity))
		if storedIdentity != identity || storedKey != string(expectedKey) {
			t.Fatalf("legacy user %s = identity %q key %q", id, storedIdentity, storedKey)
		}
	}
	if err := backend.Ready(ctx, additive); err == nil || !strings.Contains(err.Error(), "canonicalization artifact") {
		t.Fatalf("v1 auth readiness = %v", err)
	}
}

func TestSQLiteAuthIdentityPlannerUpgradeCanonicalizesAuthoredValuesAndRegistry(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteAuthMigrationManifest(t)
	directory := t.TempDir()
	legacy, _ := sqlitePlannerContractFor(sqlitePlannerVersionV1)
	initial, err := buildSQLiteArtifact(ctx, "initial", nil, manifest, "", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}

	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, deleted_at, status, revision, values_json)
VALUES (?, ?, ?, ?, NULL, '', 0, ?)`, "users", "user-1", int64(1), int64(1), `{"email":" \tUser@Example.COM\u00a0"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, deleted_at, status, revision, values_json)
VALUES (?, ?, ?, ?, ?, '', 0, ?)`, "users", "user-trash", int64(1), int64(1), int64(2), `{"email":" USER@example.com "}`); err != nil {
		t.Fatal(err)
	}

	upgrade, err := buildSQLiteArtifact(ctx, "canonical-auth-identities", &manifest, manifest, sqlitePlannerVersionV1, currentSQLitePlannerContract())
	if err != nil {
		t.Fatal(err)
	}
	if upgrade.Planner.Version != sqlitePlannerVersion || !sqliteArtifactHasStep(upgrade, ridumigration.StepCanonicalizeAuthIdentities) {
		t.Fatalf("SQLite auth identity upgrade = planner %s phases %#v", upgrade.Planner.Version, upgrade.Phases)
	}
	if _, err := migrationartifact.Create(directory, "canonical-auth-identities", upgrade, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}

	var identity, valueKey string
	if err := backend.db.QueryRowContext(ctx, `SELECT json_extract(values_json, '$.email')
FROM ridu_documents WHERE collection_id = 'users' AND id = 'user-1'`).Scan(&identity); err != nil {
		t.Fatal(err)
	}
	if identity != "user@example.com" {
		t.Fatalf("canonical stored identity = %q", identity)
	}
	if err := backend.db.QueryRowContext(ctx, `SELECT value_key FROM ridu_unique_values
WHERE collection_id = 'users' AND document_id = 'user-1'`).Scan(&valueKey); err != nil {
		t.Fatal(err)
	}
	if valueKey != `"user@example.com"` {
		t.Fatalf("canonical unique key = %q", valueKey)
	}
	var trashedIdentity string
	if err := backend.db.QueryRowContext(ctx, `SELECT json_extract(values_json, '$.email')
FROM ridu_documents WHERE collection_id = 'users' AND id = 'user-trash'`).Scan(&trashedIdentity); err != nil {
		t.Fatal(err)
	}
	if trashedIdentity != "user@example.com" {
		t.Fatalf("canonical trashed identity = %q", trashedIdentity)
	}
	var trashUniqueKeys int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_unique_values
WHERE collection_id = 'users' AND document_id = 'user-trash'`).Scan(&trashUniqueKeys); err != nil || trashUniqueKeys != 0 {
		t.Fatalf("trashed unique keys = %d, %v", trashUniqueKeys, err)
	}
	var registryBefore string
	if err := backend.db.QueryRowContext(ctx, `SELECT group_concat(collection_id || ':' || index_key || ':' || value_key || ':' || document_id, '|')
FROM ridu_unique_values`).Scan(&registryBefore); err != nil {
		t.Fatal(err)
	}
	if err := backend.DownArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "irreversible auth-identity canonicalization") {
		t.Fatalf("lossy auth identity down = %v", err)
	}
	var activeAfterDown, trashAfterDown, registryAfter string
	if err := backend.db.QueryRowContext(ctx, `SELECT json_extract(values_json, '$.email') FROM ridu_documents WHERE id = 'user-1'`).Scan(&activeAfterDown); err != nil {
		t.Fatal(err)
	}
	if err := backend.db.QueryRowContext(ctx, `SELECT json_extract(values_json, '$.email') FROM ridu_documents WHERE id = 'user-trash'`).Scan(&trashAfterDown); err != nil {
		t.Fatal(err)
	}
	if err := backend.db.QueryRowContext(ctx, `SELECT group_concat(collection_id || ':' || index_key || ':' || value_key || ':' || document_id, '|')
FROM ridu_unique_values`).Scan(&registryAfter); err != nil {
		t.Fatal(err)
	}
	if activeAfterDown != identity || trashAfterDown != trashedIdentity || registryAfter != registryBefore {
		t.Fatalf("rejected down changed identity/index state: active=%q trash=%q registry=%q", activeAfterDown, trashAfterDown, registryAfter)
	}
	var applied int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_migrations`).Scan(&applied); err != nil || applied != 2 {
		t.Fatalf("rejected down changed ledger count = %d, %v", applied, err)
	}
}

func TestSQLiteAuthIdentityPlannerUpgradeRejectsCanonicalCollisionBeforeRewrite(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteAuthMigrationManifest(t)
	directory := t.TempDir()
	legacy, _ := sqlitePlannerContractFor(sqlitePlannerVersionV1)
	initial, err := buildSQLiteArtifact(ctx, "initial", nil, manifest, "", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	for index, encoded := range []string{`{"email":"user@example.com"}`, `{"email":" user@example.com "}`} {
		if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, deleted_at, status, revision, values_json)
VALUES (?, ?, ?, ?, NULL, '', 0, ?)`, "users", "user-"+string(rune('1'+index)), int64(1), int64(1), encoded); err != nil {
			t.Fatal(err)
		}
	}
	upgrade, err := buildSQLiteArtifact(ctx, "canonical-auth-identities", &manifest, manifest, sqlitePlannerVersionV1, currentSQLitePlannerContract())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "canonical-auth-identities", upgrade, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	err = backend.ApplyArtifacts(ctx, directory)
	if err == nil || !strings.Contains(err.Error(), "active collision in collection users") {
		t.Fatalf("canonical collision error = %v", err)
	}
	if strings.Contains(err.Error(), "user@example.com") {
		t.Fatalf("canonical collision error leaked identity: %v", err)
	}
	var identity string
	if scanErr := backend.db.QueryRowContext(ctx, `SELECT json_extract(values_json, '$.email')
FROM ridu_documents WHERE collection_id = 'users' AND id = 'user-2'`).Scan(&identity); scanErr != nil {
		t.Fatal(scanErr)
	}
	if identity != " user@example.com " {
		t.Fatalf("failed migration rewrote identity to %q", identity)
	}
	var applied int
	if scanErr := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_migrations`).Scan(&applied); scanErr != nil || applied != 1 {
		t.Fatalf("failed migration ledger count = %d, %v", applied, scanErr)
	}
}

func sqliteAuthMigrationManifest(t *testing.T) schema.Manifest {
	t.Helper()
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "auth identity migration", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			Fields: []field.Definition{field.Email("email", field.Required(), field.Unique())},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func sqliteArtifactHasStep(artifact ridumigration.Artifact, kind ridumigration.StepKind) bool {
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind == kind {
				return true
			}
		}
	}
	return false
}

func sqliteAuthIdentityRiskHasLevel(risks []ridumigration.Risk, level ridumigration.RiskLevel) bool {
	for _, risk := range risks {
		if risk.Code == "RIDU_AUTH_IDENTITY_CANONICALIZATION" && risk.Level == level {
			return true
		}
	}
	return false
}
