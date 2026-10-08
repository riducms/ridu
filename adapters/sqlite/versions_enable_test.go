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
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func sqliteVersionsConfig(versions, drafts bool) ridu.Config {
	return ridu.Config{
		Name: "SQLite enable versions",
		Collections: []ridu.Collection{{
			Slug: "posts", Trash: true, Versions: versions, VersionConfig: ridu.VersionConfig{Drafts: drafts},
			Fields: field.Fields{field.Text("title").Index().Unique()},
		}},
	}
}

func sqliteVersionsManifest(t *testing.T, versions, drafts bool) schema.Manifest {
	t.Helper()
	manifest, err := ridu.Resolve(sqliteVersionsConfig(versions, drafts))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// seedSQLiteUnversionedPosts creates an unversioned history with two posts,
// one of them trashed, and returns their IDs.
func seedSQLiteUnversionedPosts(t *testing.T, backend *Store, directory string) (string, string) {
	t.Helper()
	ctx := t.Context()
	if _, err := CreateArtifact(ctx, directory, "initial", sqliteVersionsManifest(t, false, false), time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(sqliteVersionsConfig(false, false), backend)
	if err != nil {
		t.Fatal(err)
	}
	live, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Live")}, ridu.MutationOptions{System: true})
	if err != nil {
		t.Fatal(err)
	}
	trashed, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Trashed")}, ridu.MutationOptions{System: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(ctx, "posts", trashed.ID, ridu.MutationOptions{System: true}); err != nil {
		t.Fatal(err)
	}
	return live.ID, trashed.ID
}

func sqliteCount(t *testing.T, backend *Store, query string, arguments ...any) int {
	t.Helper()
	var count int
	if err := backend.db.QueryRowContext(context.Background(), query, arguments...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// Enabling versions turns every stored document, trashed ones included, into
// a versioned document as the recorded choice says, replays in verify, and
// rolls back to the unversioned documents.
func TestSQLiteMigrationEnablesVersionsOnStoredDocuments(t *testing.T) {
	for _, existing := range []migration.ExistingDocuments{migration.ExistingPublished, migration.ExistingDraft} {
		t.Run(string(existing), func(t *testing.T) {
			ctx := t.Context()
			directory := t.TempDir()
			backend := newSQLiteMigrationStore(t)
			liveID, trashedID := seedSQLiteUnversionedPosts(t, backend, directory)
			after := sqliteVersionsManifest(t, true, true)
			posts := sqliteCollectionBySlug(t, after, "posts")

			if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{}); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_REQUIRED") {
				t.Fatalf("migration without a choice = %v", err)
			}
			if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{
				ExistingDocuments: map[schema.StableID]migration.ExistingDocuments{posts.ID: existing},
			}); err != nil {
				t.Fatal(err)
			}
			files, err := migrationartifact.ReadAll(directory)
			if err != nil {
				t.Fatal(err)
			}
			if steps := enableVersionsSteps(files[1].Artifact); len(steps) != 1 || steps[0].ResourceID != posts.ID || steps[0].Existing != existing {
				t.Fatalf("recorded enable-versions steps = %#v", steps)
			}
			if err := VerifyArtifacts(ctx, directory); err != nil {
				t.Fatalf("verify replays the migration: %v", err)
			}
			if err := backend.ApplyArtifacts(ctx, directory); err != nil {
				t.Fatal(err)
			}
			if err := backend.Ready(ctx, after); err != nil {
				t.Fatal(err)
			}

			application, err := ridu.New(sqliteVersionsConfig(true, true), backend)
			if err != nil {
				t.Fatal(err)
			}
			published, draft := false, true
			public, publicErr := application.Local().Find(ctx, "posts", liveID, ridu.FindOptions{Draft: &published})
			working, err := application.Local().Find(ctx, "posts", liveID, ridu.FindOptions{Draft: &draft, System: true})
			if err != nil {
				t.Fatal(err)
			}
			if title, _ := working.Values["title"].StringValue(); title != "Live" {
				t.Fatalf("working document = %#v", working)
			}
			versions, err := application.Local().Versions(ctx, "posts", liveID, ridu.FindOptions{System: true})
			if err != nil || len(versions) != 1 || versions[0].Revision != working.Revision || versions[0].Status != working.Status {
				t.Fatalf("versions = %#v, %v; working = %#v", versions, err, working)
			}
			switch existing {
			case migration.ExistingPublished:
				if publicErr != nil {
					t.Fatalf("published read = %v", publicErr)
				}
				if title, _ := public.Values["title"].StringValue(); title != "Live" || working.Status != store.StatusPublished ||
					working.PublishedRevision != working.Revision || working.HasDraftChanges {
					t.Fatalf("published document = %#v, working = %#v", public, working)
				}
			case migration.ExistingDraft:
				if !errors.Is(publicErr, store.ErrNotFound) {
					t.Fatalf("published read of a draft = %#v, %v", public, publicErr)
				}
				if working.Status != store.StatusDraft || working.PublishedRevision != 0 {
					t.Fatalf("draft document = %#v", working)
				}
			}
			trashed, err := application.Local().Find(ctx, "posts", trashedID, ridu.FindOptions{Draft: &draft, System: true, TrashOnly: true})
			if err != nil || trashed.DeletedAt == nil || trashed.Status != working.Status {
				t.Fatalf("trashed document = %#v, %v", trashed, err)
			}
			if _, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Live")}, ridu.MutationOptions{System: true}); err == nil {
				t.Fatal("the converted documents no longer hold their unique values")
			}

			if err := backend.DownArtifacts(ctx, directory); err != nil {
				t.Fatal(err)
			}
			collectionID := string(posts.ID)
			if count := sqliteCount(t, backend, `SELECT count(*) FROM ridu_published_documents WHERE collection_id = ?`, collectionID) +
				sqliteCount(t, backend, `SELECT count(*) FROM ridu_versions WHERE collection_id = ?`, collectionID) +
				sqliteCount(t, backend, `SELECT count(*) FROM ridu_documents WHERE collection_id = ? AND (status <> '' OR revision <> 0)`, collectionID); count != 0 {
				t.Fatalf("rollback left %d versioned rows", count)
			}
			original, err := ridu.New(sqliteVersionsConfig(false, false), backend)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := original.Local().Find(ctx, "posts", liveID, ridu.FindOptions{System: true})
			if title, _ := restored.Values["title"].StringValue(); err != nil || title != "Live" {
				t.Fatalf("rolled-back document = %#v, %v", restored, err)
			}
			if err := backend.ApplyArtifacts(ctx, directory); err != nil {
				t.Fatalf("reapply after rollback: %v", err)
			}
		})
	}
}

func TestSQLiteRequireEmptyStopsWhileDocumentsAreStored(t *testing.T) {
	ctx := t.Context()
	directory := t.TempDir()
	backend := newSQLiteMigrationStore(t)
	seedSQLiteUnversionedPosts(t, backend, directory)
	after := sqliteVersionsManifest(t, true, false)
	posts := sqliteCollectionBySlug(t, after, "posts")
	if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{
		ExistingDocuments: map[schema.StableID]migration.ExistingDocuments{posts.ID: migration.ExistingDraft},
	}); err == nil || !strings.Contains(err.Error(), "does not enable drafts") {
		t.Fatalf("draft choice without drafts = %v", err)
	}
	if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{
		ExistingDocuments: map[schema.StableID]migration.ExistingDocuments{posts.ID: migration.ExistingRequireEmpty},
	}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArtifacts(ctx, directory); err != nil {
		t.Fatalf("verify replays an empty history: %v", err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_NOT_EMPTY") || !strings.Contains(err.Error(), "2 documents") {
		t.Fatalf("require-empty with stored documents = %v", err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory, after)
	if err != nil || len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("statuses after a refused migration = %#v, %v", statuses, err)
	}
	if _, err := backend.db.ExecContext(ctx, `DELETE FROM ridu_documents WHERE collection_id = ?`, string(posts.ID)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("require-empty on an empty collection = %v", err)
	}
}

func TestSQLiteDevelopmentSyncEnablesVersionsOnlyWhenEmptyOrChosen(t *testing.T) {
	ctx := t.Context()
	before, after := sqliteVersionsManifest(t, false, false), sqliteVersionsManifest(t, true, true)
	backend := newSQLiteMigrationStore(t)
	if err := backend.Migrate(ctx, before); err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(ctx, after); err != nil {
		t.Fatalf("enabling versions on an empty collection = %v", err)
	}

	backend = newSQLiteMigrationStore(t)
	if err := backend.Migrate(ctx, before); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(sqliteVersionsConfig(false, false), backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Live")}, ridu.MutationOptions{System: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(ctx, after); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_DOCUMENTS") {
		t.Fatalf("enabling versions over a stored document = %v", err)
	}
	if recorded, exists, err := backend.DevelopmentManifest(ctx); err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("refused sync changed the recorded schema: exists=%t, %v", exists, err)
	}
	reports, err := backend.ReviewDevelopmentVersions(ctx, before, after)
	if err != nil || len(reports) != 1 || reports[0].Resource.Slug != "posts" || reports[0].Documents != 1 {
		t.Fatalf("review = %#v, %v", reports, err)
	}
	posts := sqliteCollectionBySlug(t, after, "posts")
	if err := backend.EnableDevelopmentVersions(ctx, before, after, map[schema.StableID]migration.ExistingDocuments{posts.ID: migration.ExistingPublished}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, after); err != nil {
		t.Fatal(err)
	}
	versioned, err := ridu.New(sqliteVersionsConfig(true, true), backend)
	if err != nil {
		t.Fatal(err)
	}
	published := false
	found, err := versioned.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{Draft: &published})
	if title, _ := found.Values["title"].StringValue(); err != nil || title != "Live" {
		t.Fatalf("published development document = %#v, %v", found, err)
	}
	if err := backend.EnableDevelopmentVersions(ctx, before, after, map[schema.StableID]migration.ExistingDocuments{posts.ID: migration.ExistingPublished}); err == nil {
		t.Fatal("enabling versions again accepted choices made for an older schema")
	}

	// Like a rename, the conversion ridu dev ran directly cannot be adopted:
	// only a database migrations manage runs the step.
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{
		ExistingDocuments: map[schema.StableID]migration.ExistingDocuments{posts.ID: migration.ExistingPublished},
	}); err != nil {
		t.Fatal(err)
	}
	if adopted, err := backend.AdoptArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "enable_versions step") {
		t.Fatalf("adopting the conversion = %v, %v", adopted, err)
	}

	// Enabling versions while nothing is stored is all schema sync does, so a
	// database it synced adopts a migration that requires the resource empty.
	empty := newSQLiteMigrationStore(t)
	if err := empty.Migrate(ctx, before); err != nil {
		t.Fatal(err)
	}
	if err := empty.Migrate(ctx, after); err != nil {
		t.Fatal(err)
	}
	directory = t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{
		ExistingDocuments: map[schema.StableID]migration.ExistingDocuments{posts.ID: migration.ExistingRequireEmpty},
	}); err != nil {
		t.Fatal(err)
	}
	if adopted, err := empty.AdoptArtifacts(ctx, directory); err != nil || len(adopted) != 2 {
		t.Fatalf("adopting require-empty = %v, %v", adopted, err)
	}
}

func enableVersionsSteps(artifact migration.Artifact) []migration.EnableVersionsPayload {
	var payloads []migration.EnableVersionsPayload
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != migration.StepEnableVersions {
				continue
			}
			var payload migration.EnableVersionsPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				panic(err)
			}
			payloads = append(payloads, payload)
		}
	}
	return payloads
}
