package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
)

// A database `ridu dev` synchronized has the schema of the committed history
// but no ledger. Adoption records that history so ridu migrate can take over.
func TestSQLiteAdoptsADevelopmentSynchronizedDatabase(t *testing.T) {
	ctx := context.Background()
	initial := sqliteMigrationManifest(t, false)
	withSummary := sqliteMigrationManifest(t, true)
	directory := t.TempDir()
	artifact, err := planArtifact(ctx, "initial", nil, initial, false)
	if err != nil {
		t.Fatal(err)
	}
	first, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}

	backend := newSQLiteMigrationStore(t)
	if err := backend.Migrate(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "without migration history") {
		t.Fatalf("apply over a development database = %v", err)
	}
	adopted, err := backend.AdoptArtifacts(ctx, directory)
	if err != nil || len(adopted) != 1 || adopted[0] != first.Name {
		t.Fatalf("adopt = %v, %v", adopted, err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory, initial)
	if err != nil || len(statuses) != 1 || !statuses[0].Applied {
		t.Fatalf("status after adoption = %#v, %v", statuses, err)
	}
	history, err := ridumigration.DigestArtifactHistory([]ridumigration.ArtifactIdentity{{Name: first.Name, Digest: first.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, initial, history); err != nil {
		t.Fatalf("readiness after adoption: %v", err)
	}
	if again, err := backend.AdoptArtifacts(ctx, directory); err != nil || len(again) != 0 {
		t.Fatalf("adopting a current history = %v, %v", again, err)
	}

	// From here the runner applies later migrations as usual.
	next, err := planArtifact(ctx, "summary", &initial, withSummary, false)
	if err != nil {
		t.Fatal(err)
	}
	next.PreviousArtifactDigest = first.Digest
	if _, err := migrationartifact.Create(directory, "summary", next, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("apply after adoption: %v", err)
	}
	if err := backend.Ready(ctx, withSummary); err != nil {
		t.Fatalf("readiness after the next migration: %v", err)
	}
}

func TestSQLiteRefusesToAdoptASchemaNoMigrationDescribes(t *testing.T) {
	ctx := context.Background()
	initial := sqliteMigrationManifest(t, false)
	directory := t.TempDir()
	artifact, err := planArtifact(ctx, "initial", nil, initial, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	// ridu dev ran ahead of the committed history.
	if err := backend.Migrate(ctx, sqliteMigrationManifest(t, true)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.AdoptArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "matches no committed migration") {
		t.Fatalf("adopt a schema ahead of history = %v", err)
	}
	if exists, err := sqliteArtifactLedgerExists(ctx, backend.db); err != nil || exists {
		t.Fatalf("a refused adoption left a ledger: %t, %v", exists, err)
	}
}
