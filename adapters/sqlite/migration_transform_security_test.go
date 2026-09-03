package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSQLiteDataTransformRequestsStayWithinArtifactResourceShapes(t *testing.T) {
	for _, test := range []struct {
		name      string
		wantError string
		operation func(schema.Collection) migration.DataTransformCallback
	}{
		{
			name:      "unknown-resource",
			wantError: "outside the immutable artifact manifests",
			operation: func(schema.Collection) migration.DataTransformCallback {
				return func(ctx context.Context, transaction migration.DataTransaction) error {
					_, err := transaction.Create(ctx, store.CreateRequest{
						Collection: schema.Collection{ID: "orphans", Slug: "orphans"},
						ID:         "orphan-1", Values: store.Values{},
					})
					return err
				}
			},
		},
		{
			name:      "forged-resource-shape",
			wantError: "must exactly match its immutable before or after manifest shape",
			operation: func(collection schema.Collection) migration.DataTransformCallback {
				forged := collection
				forged.Fields = append(append([]schema.Field(nil), collection.Fields...), schema.Field{ID: "posts-dormant", Name: "dormant"})
				return func(ctx context.Context, transaction migration.DataTransaction) error {
					_, err := transaction.Create(ctx, store.CreateRequest{
						Collection: forged, ID: "post-1", Values: store.Values{"dormant": store.String("hidden")},
					})
					return err
				}
			},
		},
		{
			name:      "unknown-value",
			wantError: "outside the immutable resource shape",
			operation: func(collection schema.Collection) migration.DataTransformCallback {
				return func(ctx context.Context, transaction migration.DataTransaction) error {
					_, err := transaction.Create(ctx, store.CreateRequest{
						Collection: collection, ID: "post-1", Values: store.Values{"dormant": store.String("hidden")},
					})
					return err
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			directory := t.TempDir()
			manifest := sqliteMigrationManifest(t, false)
			initial, err := planArtifact(ctx, "initial", nil, manifest, false)
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

			descriptor := migration.DataTransformDescriptor{
				Name:     "guard-" + test.name,
				Checksum: migration.DataTransformChecksum([]byte("guard-" + test.name + "-v1")),
			}
			artifact, err := planArtifact(ctx, "guard-"+test.name, &manifest, manifest, false, descriptor)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := migrationartifact.Create(directory, "guard-"+test.name, artifact, time.Unix(2, 0)); err != nil {
				t.Fatal(err)
			}
			transform := migration.DataTransform{
				DataTransformDescriptor: descriptor,
				Up:                      test.operation(manifest.Snapshot().Collections[0]),
				Down:                    func(context.Context, migration.DataTransaction) error { return nil },
			}
			if err := backend.ApplyArtifacts(ctx, directory, transform); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("unsafe transform error = %v, want %q", err, test.wantError)
			}
			var documents, applied int
			if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_documents`).Scan(&documents); err != nil {
				t.Fatal(err)
			}
			if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_migrations`).Scan(&applied); err != nil {
				t.Fatal(err)
			}
			if documents != 0 || applied != 1 {
				t.Fatalf("rejected transform committed documents=%d applied=%d", documents, applied)
			}
		})
	}
}
