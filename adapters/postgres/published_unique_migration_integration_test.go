package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestPostgresNewUniqueRejectsDuplicatePublishedHeads(t *testing.T) {
	for _, mode := range []string{"development", "artifact"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			backend := migrationArtifactTestBackend(t)
			field := atlasTextField("posts-slug", "slug")
			before := schema.NewManifest(schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Unique live heads"}, Plugins: []schema.Plugin{},
				Collections: []schema.Collection{{ID: "posts", Slug: "posts", Versions: &schema.VersionSettings{Drafts: true}, Fields: []schema.Field{field}}},
			})
			unique := field
			unique.Unique = true
			afterSnapshot := before.Snapshot()
			afterSnapshot.Collections[0].Fields[0] = unique
			after := schema.NewManifest(afterSnapshot)
			directory := t.TempDir()
			if mode == "artifact" {
				if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), nil, false); err != nil {
					t.Fatal(err)
				}
				if err := backend.ApplyArtifacts(ctx, directory); err != nil {
					t.Fatal(err)
				}
			} else if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
				t.Fatal(err)
			}
			collection := before.Snapshot().Collections[0]
			for _, candidate := range []struct{ id, pending string }{{"a", "new-a"}, {"b", "new-b"}} {
				write, err := backend.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				created, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: candidate.id, Status: store.StatusPublished, Values: store.Values{"slug": store.String("shared-live")}})
				if err == nil {
					_, err = conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: candidate.id, ExpectedRevision: created.Revision}, Intent: store.WriteIntentSaveDraft, Values: store.Values{"slug": store.String(candidate.pending)}})
				}
				if err != nil {
					_ = write.Rollback(ctx)
					t.Fatal(err)
				}
				if err := write.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "artifact" {
				artifact, err := BuildArtifact(ctx, "add-unique", &before, after, nil, false)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := migrationartifact.Create(directory, "add-unique", artifact, time.Unix(2, 0)); err != nil {
					t.Fatal(err)
				}
				if err := backend.ApplyArtifacts(ctx, directory); !errors.Is(err, store.ErrConflict) {
					t.Fatalf("artifact admitted duplicate live heads: %v", err)
				}
				statuses, err := backend.ArtifactStatus(ctx, directory)
				if err != nil || len(statuses) != 2 || statuses[1].Applied {
					t.Fatalf("failed artifact state = %#v, %v", statuses, err)
				}
			} else {
				if err := backend.SyncDevelopmentSchema(ctx, after); !errors.Is(err, store.ErrConflict) {
					t.Fatalf("development sync admitted duplicate live heads: %v", err)
				}
				recorded, exists, err := backend.DevelopmentManifest(ctx)
				if err != nil || !exists || !recorded.Equal(before) {
					t.Fatalf("failed sync changed manifest: %#v, %t, %v", recorded.Snapshot(), exists, err)
				}
			}
		})
	}
}
