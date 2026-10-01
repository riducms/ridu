package mongodb

import (
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// An executable built with ridu build is admitted by a database whose ledger
// matches its embedded history even when its admin settings differ from the
// latest migration: hiding or regrouping a collection needs no migration.
// Without the history, readiness still compares the exact manifest.
func TestMongoDBReadinessIgnoresAdminPresentationWithHistory(t *testing.T) {
	backend := mongoIntegrationStore(t)
	ctx := t.Context()
	config := func(admin ridu.CollectionAdmin) ridu.Config {
		return ridu.Config{Name: "Presentation", Collections: []ridu.Collection{{
			Slug: "posts", Admin: admin, Fields: field.Fields{field.Text("title").Index()},
		}}}
	}
	committed, err := ridu.Resolve(config(ridu.CollectionAdmin{}))
	if err != nil {
		t.Fatal(err)
	}
	relabelled, err := ridu.Resolve(config(ridu.CollectionAdmin{Hidden: true, Group: "Plumbing"}))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	created, err := CreateArtifact(ctx, directory, "initial", committed, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 1 || files[0].Digest != created.Checksum {
		t.Fatalf("history = %d files, %v", len(files), err)
	}
	history, err := ridumigration.DigestArtifactHistory([]ridumigration.ArtifactIdentity{{Name: files[0].Name, Digest: files[0].Digest}}, files[0].Artifact.ToDigest, committed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.RequireCurrentHistory(directory, relabelled); err != nil {
		t.Fatalf("ridu build would refuse an admin-only change: %v", err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, relabelled, history); err != nil {
		t.Fatalf("readiness with an admin-only change: %v", err)
	}
	if err := backend.Ready(ctx, relabelled); err == nil || !strings.Contains(err.Error(), "does not match the executable manifest") {
		t.Fatalf("readiness without the history = %v", err)
	}
	snapshot := relabelled.Snapshot()
	snapshot.Collections[0].Fields[0].Required = true
	if err := backend.ReadyWithMigrationHistory(ctx, schema.NewManifest(snapshot), history); err == nil {
		t.Fatal("readiness admitted a required-field change with the same physical indexes")
	}
	if _, err := backend.mongoMigrationCollection(mongoMigrationArtifactCollectionName).UpdateOne(ctx,
		bson.D{{Key: "_id", Value: files[0].Name}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "toDigest", Value: strings.Repeat("f", 64)}}}},
	); err != nil {
		t.Fatal(err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, relabelled, history); err == nil {
		t.Fatal("readiness admitted a corrupted ledger head")
	}
}
