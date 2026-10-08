package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/enableversions"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// seedPostgresUnversionedPosts applies an unversioned history and stores two
// posts that reference a category, one of them trashed. It returns their IDs.
func seedPostgresUnversionedPosts(t *testing.T, backend *Store, directory string) (string, string) {
	t.Helper()
	ctx := t.Context()
	if _, err := CreateArtifact(ctx, directory, "initial", postgresVersionsManifest(t, false, false), time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	return createPostgresUnversionedPosts(t, backend)
}

func createPostgresUnversionedPosts(t *testing.T, backend *Store) (string, string) {
	t.Helper()
	ctx := t.Context()
	application, err := ridu.New(postgresVersionsConfig(false, false), backend)
	if err != nil {
		t.Fatal(err)
	}
	system := ridu.MutationOptions{System: true}
	category, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("News")}, system)
	if err != nil {
		t.Fatal(err)
	}
	live, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Live"), "category": store.String(category.ID)}, system)
	if err != nil {
		t.Fatal(err)
	}
	trashed, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Trashed"), "category": store.String(category.ID)}, system)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(ctx, "posts", trashed.ID, system); err != nil {
		t.Fatal(err)
	}
	return live.ID, trashed.ID
}

func postgresCount(t *testing.T, backend *Store, statement string, arguments ...any) int {
	t.Helper()
	var count int
	if err := backend.pool.QueryRow(context.Background(), statement, arguments...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// postgresVersionsLayout reports whether the posts working table has its
// versioned status column and whether its live table exists.
func postgresVersionsLayout(t *testing.T, backend *Store, posts schema.Collection) (bool, bool) {
	t.Helper()
	var status, live bool
	if err := backend.pool.QueryRow(context.Background(), `SELECT
EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = '_status'),
to_regclass(current_schema() || '.' || $2) IS NOT NULL`, collectionTable(posts.ID), publishedCollectionTable(posts.ID)).Scan(&status, &live); err != nil {
		t.Fatal(err)
	}
	return status, live
}

// Enabling versions turns every stored document, trashed ones included, into
// a versioned document as the recorded choice says, keeps their values,
// references and unique values on both heads, and replays in verify.
func TestPostgresMigrationEnablesVersionsOnStoredDocuments(t *testing.T) {
	for _, test := range []struct {
		name     string
		existing ridumigration.ExistingDocuments
		drafts   bool
	}{
		{name: "published", existing: ridumigration.ExistingPublished, drafts: true},
		{name: "draft", existing: ridumigration.ExistingDraft, drafts: true},
		{name: "published without drafts", existing: ridumigration.ExistingPublished},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			existing := test.existing
			backend := migrationArtifactTestBackend(t)
			directory := t.TempDir()
			liveID, trashedID := seedPostgresUnversionedPosts(t, backend, directory)
			after := postgresVersionsManifest(t, true, test.drafts)
			posts := postgresVersionsCollection(t, after, "posts")

			if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{}); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_REQUIRED") {
				t.Fatalf("migration without a choice = %v", err)
			}
			if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{ExistingDocuments: postgresVersionsChoice(posts, existing)}); err != nil {
				t.Fatal(err)
			}
			files, err := migrationartifact.ReadAll(directory)
			if err != nil || len(files) != 2 {
				t.Fatalf("history = %d files, %v", len(files), err)
			}
			if recorded, err := enableversions.Recorded(files[1].Artifact); err != nil || len(recorded) != 1 || recorded[posts.ID] != existing {
				t.Fatalf("recorded choices = %#v, %v", recorded, err)
			}
			if err := VerifyArtifactsWithOptions(ctx, os.Getenv("RIDU_POSTGRES_URL"), directory, RunnerOptions{AllowInsecureDatabase: true}); err != nil {
				t.Fatalf("verify replays the migration: %v", err)
			}
			if err := backend.ApplyArtifacts(ctx, directory); !errors.Is(err, ErrMaintenanceRequired) {
				t.Fatalf("conversion without maintenance admission = %v", err)
			}
			if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
				t.Fatal(err)
			}
			if err := backend.Ready(ctx, after); err != nil {
				t.Fatal(err)
			}

			application, err := ridu.New(postgresVersionsConfig(true, test.drafts), backend)
			if err != nil {
				t.Fatal(err)
			}
			published, draft := false, true
			public, publicErr := application.Local().Find(ctx, "posts", liveID, ridu.FindOptions{Draft: &published})
			working, err := application.Local().Find(ctx, "posts", liveID, ridu.FindOptions{Draft: &draft, System: true})
			if err != nil {
				t.Fatal(err)
			}
			if title, _ := working.Values["title"].StringValue(); title != "Live" || working.Revision != 1 {
				t.Fatalf("working document = %#v", working)
			}
			versions, err := application.Local().Versions(ctx, "posts", liveID, ridu.FindOptions{System: true})
			if err != nil || len(versions) != 1 || versions[0].Revision != working.Revision || versions[0].Status != working.Status ||
				!versions[0].CreatedAt.Equal(working.UpdatedAt) {
				t.Fatalf("versions = %#v, %v; working = %#v", versions, err, working)
			}
			if title, _ := versions[0].Snapshot.Values["title"].StringValue(); title != "Live" || versions[0].Snapshot.Status != working.Status {
				t.Fatalf("version snapshot = %#v", versions[0].Snapshot)
			}
			workingReferences := postgresCount(t, backend, `SELECT count(*) FROM ridu_document_references WHERE owner_collection_id = $1 AND published_head = false`, string(posts.ID))
			liveReferences := postgresCount(t, backend, `SELECT count(*) FROM ridu_document_references WHERE owner_collection_id = $1 AND published_head = true`, string(posts.ID))
			liveRows := postgresCount(t, backend, `SELECT count(*) FROM `+quote(publishedCollectionTable(posts.ID)))
			switch existing {
			case ridumigration.ExistingPublished:
				if publicErr != nil {
					t.Fatalf("published read = %v", publicErr)
				}
				// Only a draft-enabled resource reports its live revision on
				// working reads.
				publishedRevision := 0
				if test.drafts {
					publishedRevision = working.Revision
				}
				if title, _ := public.Values["title"].StringValue(); title != "Live" || working.Status != store.StatusPublished ||
					working.PublishedRevision != publishedRevision || working.HasDraftChanges {
					t.Fatalf("published document = %#v, working = %#v", public, working)
				}
				if liveRows != 2 || workingReferences != 2 || liveReferences != workingReferences {
					t.Fatalf("live rows %d, working references %d, live references %d", liveRows, workingReferences, liveReferences)
				}
			case ridumigration.ExistingDraft:
				if !errors.Is(publicErr, store.ErrNotFound) {
					t.Fatalf("published read of a draft = %#v, %v", public, publicErr)
				}
				if working.Status != store.StatusDraft || working.PublishedRevision != 0 {
					t.Fatalf("draft document = %#v", working)
				}
				if liveRows != 0 || workingReferences != 2 || liveReferences != 0 {
					t.Fatalf("live rows %d, working references %d, live references %d", liveRows, workingReferences, liveReferences)
				}
			}
			trashed, err := application.Local().Find(ctx, "posts", trashedID, ridu.FindOptions{Draft: &draft, System: true, TrashOnly: true})
			if err != nil || trashed.DeletedAt == nil || trashed.Status != working.Status {
				t.Fatalf("trashed document = %#v, %v", trashed, err)
			}
			if versions, err := application.Local().Versions(ctx, "posts", trashedID, ridu.FindOptions{System: true}); err != nil || len(versions) != 1 {
				t.Fatalf("trashed document versions = %#v, %v", versions, err)
			}
			if _, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Live")}, ridu.MutationOptions{System: true}); err == nil {
				t.Fatal("the converted documents no longer hold their unique values")
			}

			// The converted history continues with the engine's next revision.
			edited, err := application.Local().PublishChanges(ctx, "posts", liveID, store.Values{"title": store.String("Edited")}, ridu.MutationOptions{System: true})
			if err != nil || edited.Revision != 2 {
				t.Fatalf("publish after conversion = %#v, %v", edited, err)
			}
			if versions, err := application.Local().Versions(ctx, "posts", liveID, ridu.FindOptions{System: true}); err != nil || len(versions) != 2 {
				t.Fatalf("versions after an edit = %#v, %v", versions, err)
			}
			public, err = application.Local().Find(ctx, "posts", liveID, ridu.FindOptions{Draft: &published})
			if title, _ := public.Values["title"].StringValue(); err != nil || title != "Edited" {
				t.Fatalf("published document after an edit = %#v, %v", public, err)
			}
		})
	}
}

// require-empty stops migrate up before any DDL commits while the resource
// stores a document, trashed ones included, and applies once it stores none.
// It needs maintenance admission: an old writer could otherwise store an
// unversioned document after the check.
func TestPostgresRequireEmptyStopsWhileDocumentsAreStored(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	directory := t.TempDir()
	seedPostgresUnversionedPosts(t, backend, directory)
	after := postgresVersionsManifest(t, true, false)
	posts := postgresVersionsCollection(t, after, "posts")
	if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{ExistingDocuments: postgresVersionsChoice(posts, ridumigration.ExistingDraft)}); err == nil || !strings.Contains(err.Error(), "does not enable drafts") {
		t.Fatalf("draft choice without drafts = %v", err)
	}
	if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{ExistingDocuments: postgresVersionsChoice(posts, ridumigration.ExistingRequireEmpty)}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArtifactsWithOptions(ctx, os.Getenv("RIDU_POSTGRES_URL"), directory, RunnerOptions{AllowInsecureDatabase: true}); err != nil {
		t.Fatalf("verify replays an empty history: %v", err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); !errors.Is(err, ErrMaintenanceRequired) {
		t.Fatalf("require-empty without maintenance admission = %v", err)
	}
	maintenance := RunnerOptions{AllowMaintenance: true, AllowInsecureDatabase: true}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, maintenance); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_NOT_EMPTY") || !strings.Contains(err.Error(), "2 documents") {
		t.Fatalf("require-empty with stored documents = %v", err)
	}
	if status, live := postgresVersionsLayout(t, backend, posts); status || live {
		t.Fatalf("a refused migration committed DDL: status column %t, live table %t", status, live)
	}
	if applied, steps := postgresCount(t, backend, `SELECT count(*) FROM ridu_migrations`), postgresCount(t, backend, `SELECT count(*) FROM ridu_migration_steps WHERE artifact_name LIKE '%enable-versions%'`); applied != 1 || steps != 0 {
		t.Fatalf("a refused migration changed the ledger: %d applied, %d steps", applied, steps)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("statuses after a refused migration = %#v, %v", statuses, err)
	}
	if _, err := backend.pool.Exec(ctx, `DELETE FROM ridu_document_references WHERE owner_collection_id = $1`, string(posts.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, `DELETE FROM `+quote(collectionTable(posts.ID))); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, maintenance); err != nil {
		t.Fatalf("require-empty on an empty collection = %v", err)
	}
	if err := backend.Ready(ctx, after); err != nil {
		t.Fatal(err)
	}
}

// Development synchronization enables versions silently only on a resource
// that stores no document. A database it synchronized that way can adopt a
// migration that records require-empty, which says the same, but not one that
// claims to have converted stored documents.
func TestPostgresDevelopmentSyncEnablesVersionsOnlyWhenEmpty(t *testing.T) {
	ctx := t.Context()
	before, after := postgresVersionsManifest(t, false, false), postgresVersionsManifest(t, true, true)
	posts := postgresVersionsCollection(t, after, "posts")

	backend := migrationArtifactTestBackend(t)
	reports, err := backend.ReviewDevelopmentVersions(ctx, before, after)
	if err != nil || len(reports) != 1 || reports[0].Resource.ID != posts.ID || reports[0].Documents != 0 {
		t.Fatalf("review before any table exists = %#v, %v", reports, err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, after); err != nil {
		t.Fatalf("enabling versions on an empty collection = %v", err)
	}
	if status, live := postgresVersionsLayout(t, backend, posts); !status || !live {
		t.Fatalf("versioned layout: status column %t, live table %t", status, live)
	}
	for _, existing := range []ridumigration.ExistingDocuments{ridumigration.ExistingPublished, ridumigration.ExistingRequireEmpty} {
		directory := t.TempDir()
		if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{ExistingDocuments: postgresVersionsChoice(posts, existing)}); err != nil {
			t.Fatal(err)
		}
		adopted, err := backend.AdoptArtifacts(ctx, directory)
		switch existing {
		case ridumigration.ExistingPublished:
			if err == nil || !strings.Contains(err.Error(), "enable_versions step") {
				t.Fatalf("adopting a conversion = %v, %v", adopted, err)
			}
		case ridumigration.ExistingRequireEmpty:
			if err != nil || len(adopted) != 2 {
				t.Fatalf("adopting require-empty = %v, %v", adopted, err)
			}
			if err := backend.Ready(ctx, after); err != nil {
				t.Fatal(err)
			}
		}
	}

	backend = migrationArtifactTestBackend(t)
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	createPostgresUnversionedPosts(t, backend)
	if err := backend.SyncDevelopmentSchema(ctx, after); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_DOCUMENTS") || !strings.Contains(err.Error(), "2 documents") {
		t.Fatalf("enabling versions over stored documents = %v", err)
	}
	if recorded, exists, err := backend.DevelopmentManifest(ctx); err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("refused sync changed the recorded schema: exists=%t, %v", exists, err)
	}
	if status, live := postgresVersionsLayout(t, backend, posts); status || live {
		t.Fatalf("refused sync changed the layout: status column %t, live table %t", status, live)
	}
	reports, err = backend.ReviewDevelopmentVersions(ctx, before, after)
	if err != nil || len(reports) != 1 || reports[0].Resource.Slug != "posts" || reports[0].Documents != 2 {
		t.Fatalf("review = %#v, %v", reports, err)
	}
}
