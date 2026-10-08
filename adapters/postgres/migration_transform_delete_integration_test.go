package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/store"
)

// A transform delete removes the framework state the document owns, as an
// operation-engine delete does. PostgreSQL has no foreign key from that state
// to the per-resource document tables, so without the cleanup a user later
// created with the same ID would inherit the deleted user's preferences (and
// sessions, credentials, and API keys).
func TestPostgresCompiledDataTransformDeleteRemovesDocumentState(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	databaseURL := backend.pool.Config().ConnConfig.ConnString()
	directory := t.TempDir()
	manifest := postgresAuthIdentityManifest()
	initial, err := BuildArtifact(ctx, "initial", nil, manifest, ArtifactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	users := manifest.Snapshot().Collections[0]
	createUser := func(email string) {
		t.Helper()
		transaction, err := backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer transaction.Rollback(ctx)
		if _, err := transaction.Create(ctx, store.CreateRequest{Collection: users, ID: "removed", Values: store.Values{"email": store.String(email)}}); err != nil {
			t.Fatal(err)
		}
		if err := transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	createUser("removed@example.test")
	if _, err := backend.SetPreference(ctx, store.Preference{CollectionID: users.ID, UserID: "removed", Key: "theme", Value: json.RawMessage(`{"mode":"dark"}`)}); err != nil {
		t.Fatal(err)
	}
	descriptor := ridumigration.DataTransformDescriptor{Name: "remove-user", Checksum: ridumigration.DataTransformChecksum([]byte("remove-user-v1"))}
	artifact, err := buildPostgresTransformTestArtifact(ctx, descriptor.Name, &manifest, manifest, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, artifact.Name, artifact, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	callback := func(ctx context.Context, transaction ridumigration.DataTransaction) error {
		_, err := transaction.Delete(ctx, store.Request{Collection: users, ID: "removed"})
		return err
	}
	transform := ridumigration.DataTransform{DataTransformDescriptor: descriptor, Up: callback, Down: callback}
	request := ridumigration.ProjectRequest{
		Action: ridumigration.ProjectApply, DatabaseURL: databaseURL, Directory: directory,
		AllowInsecureDatabase: true, AllowMaintenance: true,
	}
	if err := ProjectMigrations(transform).RunProjectMigration(ctx, request, manifest); err != nil {
		t.Fatal(err)
	}
	createUser("recreated@example.test")
	if preference, err := backend.GetPreference(ctx, users.ID, "removed", "theme"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("recreated document inherited the transform-deleted document's preference %s, %v", preference.Value, err)
	}
}
