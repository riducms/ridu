package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/migration"
)

func TestSQLiteReadyWithMigrationHistoryUsesAppliedLedger(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := sqliteMigrationManifest(t, false)
	file, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), false)
	if err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	expected, err := migration.DigestArtifactHistory([]migration.ArtifactIdentity{{Name: file.Name, Digest: file.Checksum}})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, manifest, expected); err != nil {
		t.Fatalf("exact applied history: %v", err)
	}
	missing, err := migration.DigestArtifactHistory([]migration.ArtifactIdentity{
		{Name: file.Name, Digest: file.Checksum},
		{Name: "99999999999999.999999999_pending.ridu.json", Digest: strings.Repeat("d", 64)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, manifest, missing); err == nil || !strings.Contains(err.Error(), "executable history") {
		t.Fatalf("missing applied artifact readiness error = %v", err)
	}
}
