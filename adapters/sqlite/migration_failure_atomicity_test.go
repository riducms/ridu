package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/store"
)

func TestSQLiteResetRefreshAndFreshFailuresAreAtomic(t *testing.T) {
	for _, action := range []string{"reset", "refresh", "fresh"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			directory := t.TempDir()
			manifest := sqliteMigrationManifest(t, false)
			collection := manifest.Snapshot().Collections[0]
			descriptor := migration.DataTransformDescriptor{
				Name:     action + "-failure",
				Checksum: migration.DataTransformChecksum([]byte(action + "-failure-v1")),
			}
			failReplay := false
			transform := migration.DataTransform{
				DataTransformDescriptor: descriptor,
				Up: func(ctx context.Context, transaction migration.DataTransaction) error {
					if !failReplay {
						return nil
					}
					if _, err := transaction.Create(ctx, store.CreateRequest{
						Collection: collection,
						ID:         "transient-replay-write",
						Values:     store.Values{"title": store.String("must roll back")},
					}); err != nil {
						return err
					}
					return errors.New("injected replay failure")
				},
				Down: func(ctx context.Context, transaction migration.DataTransaction) error {
					if action != "reset" {
						return nil
					}
					if _, err := transaction.Update(ctx, store.UpdateRequest{
						Request: store.Request{Collection: collection, ID: "kept"},
						Values:  store.Values{"title": store.String("must roll back")},
					}); err != nil {
						return err
					}
					return errors.New("injected reset failure")
				},
			}
			artifact, err := planArtifact(ctx, "initial", nil, manifest, false, descriptor)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
				t.Fatal(err)
			}
			backend, err := Open(ctx, filepath.Join(t.TempDir(), "failure.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = backend.Close() })
			if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
				t.Fatal(err)
			}
			write, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := write.Create(ctx, store.CreateRequest{
				Collection: collection,
				ID:         "kept",
				Values:     store.Values{"title": store.String("before failure")},
			}); err != nil {
				_ = write.Rollback(ctx)
				t.Fatal(err)
			}
			if err := write.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if action == "fresh" {
				if _, err := backend.db.ExecContext(ctx, `CREATE TABLE application_scratch (value TEXT NOT NULL)`); err != nil {
					t.Fatal(err)
				}
				if _, err := backend.db.ExecContext(ctx, `INSERT INTO application_scratch (value) VALUES ('preserved')`); err != nil {
					t.Fatal(err)
				}
			}

			failReplay = action != "reset"
			switch action {
			case "reset":
				err = backend.ResetArtifacts(ctx, directory, transform)
			case "refresh":
				err = backend.RefreshArtifacts(ctx, directory, transform)
			case "fresh":
				err = backend.FreshArtifacts(ctx, directory, transform)
			}
			if err == nil || !strings.Contains(err.Error(), "injected "+action+" failure") && !strings.Contains(err.Error(), "injected replay failure") {
				t.Fatalf("%s error = %v, want injected failure", action, err)
			}
			if err := backend.Ready(ctx, manifest); err != nil {
				t.Fatalf("failed %s changed readiness: %v", action, err)
			}
			statuses, err := backend.ArtifactStatus(ctx, directory, manifest)
			if err != nil {
				t.Fatal(err)
			}
			if len(statuses) != 1 || !statuses[0].Applied {
				t.Fatalf("failed %s changed migration ledger: %#v", action, statuses)
			}
			read, err := backend.BeginSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			kept, findErr := read.Find(ctx, store.Request{Collection: collection, ID: "kept"})
			if findErr != nil {
				_ = read.Rollback(ctx)
				t.Fatalf("failed %s removed the existing document: %v", action, findErr)
			}
			_, transientErr := read.Find(ctx, store.Request{Collection: collection, ID: "transient-replay-write"})
			if !errors.Is(transientErr, store.ErrNotFound) {
				_ = read.Rollback(ctx)
				t.Fatalf("failed %s retained replay write: %v", action, transientErr)
			}
			if rollbackErr := read.Rollback(ctx); rollbackErr != nil {
				t.Fatal(rollbackErr)
			}
			if title, _ := kept.Values["title"].StringValue(); title != "before failure" {
				t.Fatalf("failed %s changed document = %#v", action, kept)
			}
			if action == "fresh" {
				var scratch string
				if err := backend.db.QueryRowContext(ctx, `SELECT value FROM application_scratch`).Scan(&scratch); err != nil {
					t.Fatal(err)
				}
				if scratch != "preserved" {
					t.Fatalf("failed fresh changed application-owned table value %q", scratch)
				}
			}
		})
	}
}
