package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestSQLiteMigrationLifecycleDownResetRefreshAndFresh(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	initial := sqliteMigrationManifest(t, false)
	additive := sqliteMigrationManifest(t, true)
	first, err := planArtifact(ctx, "initial", nil, initial, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", first, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	second, err := planArtifact(ctx, "add-summary", &initial, additive, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "add-summary", second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}

	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collectionID := string(initial.Snapshot().Collections[0].ID)
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, status, revision, values_json)
VALUES (?, 'post-1', 1, 1, '', 0, '{"title":"Preserved","summary":"value"}')`, collectionID); err != nil {
		t.Fatal(err)
	}

	if err := backend.DownArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory, additive)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("status after down = %#v", statuses)
	}
	var values string
	if err := backend.db.QueryRowContext(ctx, `SELECT values_json FROM ridu_documents WHERE id = 'post-1'`).Scan(&values); err != nil {
		t.Fatal(err)
	}
	if values != `{"title":"Preserved"}` {
		t.Fatalf("down did not retire the rolled-back field value: %s", values)
	}

	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.RefreshArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	statuses, err = backend.ArtifactStatus(ctx, directory, additive)
	if err != nil {
		t.Fatal(err)
	}
	if !statuses[0].Applied || !statuses[1].Applied {
		t.Fatalf("status after refresh = %#v", statuses)
	}

	if err := backend.ResetArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	statuses, err = backend.ArtifactStatus(ctx, directory, additive)
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("status after reset = %#v", statuses)
	}
	var managed int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name LIKE 'ridu_%'`).Scan(&managed); err != nil {
		t.Fatal(err)
	}
	if managed != 0 {
		t.Fatalf("reset left %d managed SQLite objects", managed)
	}

	if _, err := backend.db.ExecContext(ctx, `CREATE TABLE application_scratch (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := backend.FreshArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	var scratch int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'application_scratch'`).Scan(&scratch); err != nil {
		t.Fatal(err)
	}
	if scratch != 0 {
		t.Fatal("fresh kept an application-owned table")
	}
	statuses, err = backend.ArtifactStatus(ctx, directory, additive)
	if err != nil {
		t.Fatal(err)
	}
	if !statuses[0].Applied || !statuses[1].Applied {
		t.Fatalf("status after fresh = %#v", statuses)
	}
}

func TestSQLiteProjectMigrationDriverRequiresItsResolvedManifestAtArtifactHead(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	current := sqliteMigrationManifest(t, false)
	artifact, err := planArtifact(ctx, "initial", nil, current, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	driver := ProjectMigrations()
	request := migration.ProjectRequest{Action: migration.ProjectVerify, Directory: directory}
	stale := sqliteMigrationManifest(t, true)
	if err := driver.RunProjectMigration(ctx, request, stale); err == nil || !strings.Contains(err.Error(), "does not match latest migration artifact") {
		t.Fatalf("stale project manifest error = %v", err)
	}
	if err := driver.RunProjectMigration(ctx, request, current); err != nil {
		t.Fatalf("current project manifest: %v", err)
	}
}

func TestSQLiteProjectMigrationDriverExecutesValidatedArtifactSnapshot(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := sqliteMigrationManifest(t, false)
	artifact, err := planArtifact(ctx, "initial", nil, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	created, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.RequireCurrentHistory(directory, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(created.Path); err != nil {
		t.Fatal(err)
	}

	driver := ProjectMigrations().(*sqliteProjectMigrationDriver)
	registry, err := newSQLiteDataTransformRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		action      migration.ProjectAction
		seedApplied bool
		wantApplied bool
	}{
		{action: migration.ProjectApply, wantApplied: true},
		{action: migration.ProjectDown, seedApplied: true},
		{action: migration.ProjectReset, seedApplied: true},
		{action: migration.ProjectRefresh, seedApplied: true, wantApplied: true},
		{action: migration.ProjectFresh, seedApplied: true, wantApplied: true},
		{action: migration.ProjectVerify},
	}
	for _, test := range tests {
		t.Run(string(test.action), func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "project.sqlite")
			if test.seedApplied {
				backend, err := Open(ctx, databasePath)
				if err != nil {
					t.Fatal(err)
				}
				if err := backend.applySQLiteArtifacts(ctx, files, sqlitePlannerContractFor, registry); err != nil {
					backend.Close()
					t.Fatal(err)
				}
				if err := backend.Close(); err != nil {
					t.Fatal(err)
				}
			}

			request := migration.ProjectRequest{Action: test.action, DatabasePath: databasePath, Directory: directory}
			if test.action == migration.ProjectVerify {
				request.DatabasePath = ""
			}
			if err := driver.runProjectMigrationFiles(ctx, request, files, registry); err != nil {
				t.Fatalf("execute retained %s snapshot: %v", test.action, err)
			}
			if test.action == migration.ProjectVerify {
				return
			}

			backend, err := Open(ctx, databasePath)
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			statuses, err := backend.artifactStatusWithResolver(ctx, files, sqlitePlannerContractFor)
			if err != nil {
				t.Fatal(err)
			}
			if len(statuses) != 1 || statuses[0].Applied != test.wantApplied {
				t.Fatalf("%s retained snapshot status = %#v, want applied %t", test.action, statuses, test.wantApplied)
			}
		})
	}
}

func TestSQLiteRollbackDoesNotResurrectAddedRelationshipValues(t *testing.T) {
	ctx := context.Background()
	resolve := func(withAuthor bool) (ridu.Config, schema.Manifest) {
		t.Helper()
		postFields := field.Fields{field.Text("title")}
		if withAuthor {
			postFields = append(postFields, field.Relationship("author", "authors"))
		}
		config := ridu.Config{Name: "SQLite rollback relationship", Collections: []ridu.Collection{
			{Slug: "authors", Fields: field.Fields{field.Text("name")}},
			{Slug: "posts", Fields: postFields},
		}}
		manifest, err := ridu.Resolve(config)
		if err != nil {
			t.Fatal(err)
		}
		return config, manifest
	}
	baseConfig, base := resolve(false)
	withAuthorConfig, withAuthor := resolve(true)
	directory := t.TempDir()
	initial, err := planArtifact(ctx, "initial", nil, base, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	added, err := planArtifact(ctx, "add-author", &base, withAuthor, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "add-author", added, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(withAuthorConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	author, err := application.Local().Create(ctx, "authors", store.Values{"name": store.String("Author")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Post"), "author": store.String(author.ID),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.DownArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	baseApplication, err := ridu.New(baseConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baseApplication.Local().Delete(ctx, "authors", author.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("reapply after deleting former relationship target: %v", err)
	}
	reapplied, err := application.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := reapplied.Values["author"]; exists {
		t.Fatalf("rolled-back relationship value resurrected: %#v", reapplied.Values)
	}
}

func TestSQLiteMigrationDownRetiresAddedResourcesAndDormantReferences(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	initial := sqliteRollbackResourceManifest(t, false)
	additive := sqliteRollbackResourceManifest(t, true)
	additiveSnapshot := additive.Snapshot()
	additiveSnapshot.Application = initial.Snapshot().Application
	additive = schema.NewManifest(additiveSnapshot)
	first, err := planArtifact(ctx, "initial", nil, initial, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", first, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	second, err := planArtifact(ctx, "add-resources", &initial, additive, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "add-resources", second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}

	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	keeper := sqliteResourceBySlug(t, additive, "keepers")
	retiredUser := sqliteResourceBySlug(t, additive, "retired-users")
	retiredGlobal := sqliteResourceBySlug(t, additive, "retired-settings")
	insertDocument := func(collection schema.Collection, id, values string) {
		t.Helper()
		if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, status, revision, values_json)
VALUES (?, ?, 1, 1, 'draft', 1, ?)`, string(collection.ID), id, values); err != nil {
			t.Fatal(err)
		}
	}
	insertDocument(keeper, "keeper-1", `{"title":"Keeper","retiredUser":"retired-1"}`)
	insertDocument(retiredUser, "retired-1", `{"email":"retired@example.com"}`)
	insertDocument(retiredGlobal, "global", `{"title":"Retired settings"}`)
	if err := rebuildDocumentReferences(ctx, backend.db, additive); err != nil {
		t.Fatal(err)
	}
	insertVersion := func(collection schema.Collection, document store.Document) {
		t.Helper()
		encoded, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_versions
  (id, collection_id, document_id, revision, status, snapshot_json, created_at)
VALUES (?, ?, ?, 1, 'draft', ?, 1)`, document.ID+":1", string(collection.ID), document.ID, string(encoded)); err != nil {
			t.Fatal(err)
		}
	}
	insertVersion(keeper, store.Document{
		ID: "keeper-1", Status: store.StatusDraft, Revision: 1,
		Values: store.Values{"title": store.String("Keeper"), "retiredUser": store.String("retired-1")},
	})
	insertVersion(retiredUser, store.Document{
		ID: "retired-1", Status: store.StatusDraft, Revision: 1,
		Values: store.Values{"email": store.String("retired@example.com")},
	})
	insertVersion(retiredGlobal, store.Document{
		ID: "global", Status: store.StatusDraft, Revision: 1,
		Values: store.Values{"title": store.String("Retired settings")},
	})
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_auth_credentials
  (collection_id, user_id, password_hash, verified) VALUES (?, 'retired-1', x'00', 1)`, string(retiredUser.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_auth_sessions
  (token_hash, id, collection_id, user_id, expires_at, created_at, last_seen_at)
VALUES ('session-hash', 'session-1', ?, 'retired-1', 2, 1, 1)`, string(retiredUser.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_auth_tokens
  (token_hash, purpose, collection_id, user_id, expires_at, created_at)
VALUES ('token-hash', 'verify', ?, 'retired-1', 2, 1)`, string(retiredUser.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_auth_api_keys
  (id, token_hash, collection_id, user_id, name, created_at)
VALUES ('key-1', 'key-hash', ?, 'retired-1', 'test', 1)`, string(retiredUser.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_preferences
  (collection_id, user_id, key, value_json, updated_at)
VALUES (?, 'retired-1', 'columns', '{}', 1)`, string(retiredUser.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_document_locks
  (collection_id, document_id, owner_collection_id, owner_id, owner_label, created_at, updated_at, expires_at)
VALUES (?, 'retired-1', ?, 'keeper-1', 'Keeper', 1, 1, 2)`, string(retiredUser.ID), string(keeper.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_tasks
  (id, slug, queue, input_json, state, run_at, attempts, max_attempts,
   retry_delay_ns, max_retry_delay_ns, backoff, timeout_ns, retention_ns,
   target_collection_id, target_document_id, requested_by_collection_id, requested_by_document_id,
   created_at, updated_at)
VALUES ('task-1', 'test', 'default', '{}', 'queued', 1, 0, 1,
        0, 0, 'fixed', 0, 0, ?, 'keeper-1', ?, 'retired-1', 1, 1)`, string(keeper.ID), string(retiredUser.ID)); err != nil {
		t.Fatal(err)
	}

	if err := backend.DownArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []schema.Collection{retiredUser, retiredGlobal} {
		var documents int
		if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_documents WHERE collection_id = ?`, string(resource.ID)).Scan(&documents); err != nil {
			t.Fatal(err)
		}
		if documents != 0 {
			t.Fatalf("retired resource %s kept %d documents", resource.ID, documents)
		}
	}
	assertSQLiteLifecycleCount(t, backend, `SELECT count(*) FROM ridu_document_references WHERE owner_collection_id = ? OR target_collection_id = ?`, string(retiredUser.ID), string(retiredUser.ID))
	assertSQLiteLifecycleCount(t, backend, `SELECT count(*) FROM ridu_unique_values WHERE collection_id = ?`, string(retiredUser.ID))
	assertSQLiteLifecycleCount(t, backend, `SELECT count(*) FROM ridu_versions WHERE collection_id IN (?, ?)`, string(retiredUser.ID), string(retiredGlobal.ID))
	assertSQLiteLifecycleCount(t, backend, `SELECT count(*) FROM ridu_tasks WHERE target_collection_id = ? OR requested_by_collection_id = ?`, string(retiredUser.ID), string(retiredUser.ID))
	assertSQLiteLifecycleCount(t, backend, `SELECT count(*) FROM ridu_document_locks WHERE collection_id = ? OR owner_collection_id = ?`, string(retiredUser.ID), string(retiredUser.ID))
	assertSQLiteLifecycleCount(t, backend, `SELECT count(*) FROM ridu_preferences WHERE collection_id = ?`, string(retiredUser.ID))
	for _, table := range []string{"ridu_auth_tokens", "ridu_auth_sessions", "ridu_auth_api_keys", "ridu_auth_credentials"} {
		assertSQLiteLifecycleCount(t, backend, `SELECT count(*) FROM `+table+` WHERE collection_id = ?`, string(retiredUser.ID))
	}
	var encoded string
	if err := backend.db.QueryRowContext(ctx, `SELECT values_json FROM ridu_documents WHERE collection_id = ? AND id = 'keeper-1'`, string(keeper.ID)).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var values store.Values
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		t.Fatal(err)
	}
	if _, exists := values["retiredUser"]; exists {
		t.Fatalf("surviving canonical reference was not scrubbed = %s", encoded)
	}
	var keeperVersionCount int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_versions WHERE collection_id = ?`, string(keeper.ID)).Scan(&keeperVersionCount); err != nil {
		t.Fatal(err)
	}
	if keeperVersionCount != 1 {
		t.Fatalf("surviving resource version count = %d, want 1", keeperVersionCount)
	}
	var snapshotJSON string
	if err := backend.db.QueryRowContext(ctx, `SELECT snapshot_json FROM ridu_versions WHERE collection_id = ? AND document_id = 'keeper-1'`, string(keeper.ID)).Scan(&snapshotJSON); err != nil {
		t.Fatal(err)
	}
	var snapshot store.Document
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		t.Fatal(err)
	}
	if _, exists := snapshot.Values["retiredUser"]; exists {
		t.Fatalf("surviving version retained rolled-back field = %s", snapshotJSON)
	}
	if title, ok := snapshot.Values["title"].StringValue(); !ok || title != "Keeper" {
		t.Fatalf("surviving version lost retained values = %s", snapshotJSON)
	}

	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("reapply additive resource artifact: %v", err)
	}
	for _, resource := range []schema.Collection{retiredUser, retiredGlobal} {
		var documents int
		if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_documents WHERE collection_id = ?`, string(resource.ID)).Scan(&documents); err != nil {
			t.Fatal(err)
		}
		if documents != 0 {
			t.Fatalf("reapply resurrected %d documents for %s", documents, resource.ID)
		}
	}
}

func TestSQLiteDataTransformFailureRollsBackSchemaDataAndLedger(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := sqliteMigrationManifest(t, false)
	collection := manifest.Snapshot().Collections[0]
	descriptor := migration.DataTransformDescriptor{
		Name: "seed-post", Checksum: migration.DataTransformChecksum([]byte("seed-post-v1")),
	}
	artifact, err := planArtifact(ctx, "initial", nil, manifest, false, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	transform := migration.DataTransform{
		DataTransformDescriptor: descriptor,
		Up: func(ctx context.Context, transaction migration.DataTransaction) error {
			if _, exposesCommit := transaction.(interface{ Commit(context.Context) error }); exposesCommit {
				return errors.New("migration transaction exposed commit")
			}
			if _, err := transaction.Create(ctx, store.CreateRequest{
				Collection: collection, ID: "seed", Values: store.Values{"title": store.String("created")},
				CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
			}); err != nil {
				return err
			}
			return errors.New("stop after data write")
		},
		Down: func(context.Context, migration.DataTransaction) error { return nil },
	}
	err = backend.ApplyArtifacts(ctx, directory, transform)
	if err == nil || !strings.Contains(err.Error(), "stop after data write") {
		t.Fatalf("apply error = %v, want callback failure", err)
	}
	var managed int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name LIKE 'ridu_%'`).Scan(&managed); err != nil {
		t.Fatal(err)
	}
	if managed != 0 {
		t.Fatalf("failed schema/data transform left %d managed objects", managed)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "unregistered data transform") {
		t.Fatalf("plain apply error = %v, want explicit callback registration", err)
	}
}

func TestSQLiteDataTransformsRunUpAndDownInExactReverseOrder(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := sqliteMigrationManifest(t, false)
	descriptors := []migration.DataTransformDescriptor{
		{Name: "first-transform", Checksum: migration.DataTransformChecksum([]byte("first-v1"))},
		{Name: "second-transform", Checksum: migration.DataTransformChecksum([]byte("second-v1"))},
	}
	artifact, err := planArtifact(ctx, "initial", nil, manifest, false, descriptors...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	var events []string
	transforms := make([]migration.DataTransform, len(descriptors))
	for index, descriptor := range descriptors {
		name := descriptor.Name
		transforms[index] = migration.DataTransform{
			DataTransformDescriptor: descriptor,
			Up: func(context.Context, migration.DataTransaction) error {
				events = append(events, "up:"+name)
				return nil
			},
			Down: func(context.Context, migration.DataTransaction) error {
				events = append(events, "down:"+name)
				return nil
			},
		}
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory, transforms...); err != nil {
		t.Fatal(err)
	}
	if err := backend.DownArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "unregistered data transform") {
		t.Fatalf("plain down error = %v, want explicit callback registration", err)
	}
	if err := backend.DownArtifacts(ctx, directory, transforms...); err != nil {
		t.Fatal(err)
	}
	want := []string{"up:first-transform", "up:second-transform", "down:second-transform", "down:first-transform"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("transform order = %v, want %v", events, want)
	}
}

func TestSQLiteApplyRequiresEveryHistoricalDataTransformRegistration(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := sqliteMigrationManifest(t, false)
	noop := func(context.Context, migration.DataTransaction) error { return nil }
	firstDescriptor := migration.DataTransformDescriptor{Name: "first", Checksum: migration.DataTransformChecksum([]byte("first"))}
	first, err := planArtifact(ctx, "initial", nil, manifest, false, firstDescriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", first, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	firstTransform := migration.DataTransform{DataTransformDescriptor: firstDescriptor, Up: noop, Down: noop}
	if err := backend.ApplyArtifacts(ctx, directory, firstTransform); err != nil {
		t.Fatal(err)
	}

	secondDescriptor := migration.DataTransformDescriptor{Name: "second", Checksum: migration.DataTransformChecksum([]byte("second"))}
	second, err := planArtifact(ctx, "second", &manifest, manifest, false, secondDescriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "second", second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	secondTransform := migration.DataTransform{DataTransformDescriptor: secondDescriptor, Up: noop, Down: noop}
	if err := backend.ApplyArtifacts(ctx, directory, secondTransform); err == nil || !strings.Contains(err.Error(), `unregistered data transform "first"`) {
		t.Fatalf("partial registry apply error = %v", err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("partial registry changed ledger = %#v", statuses)
	}
	if err := backend.ApplyArtifacts(ctx, directory, firstTransform, secondTransform); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), `unregistered data transform "first"`) {
		t.Fatalf("fully applied missing registry error = %v", err)
	}
}

func TestSQLiteSameDigestDataTransformsCannotBeReordered(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := sqliteMigrationManifest(t, false)
	if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	firstDescriptor := migration.DataTransformDescriptor{Name: "first-transform", Checksum: migration.DataTransformChecksum([]byte("first-transform-v1"))}
	if _, err := CreateArtifact(ctx, directory, "first-transform", manifest, time.Unix(2, 0), false, firstDescriptor); err != nil {
		t.Fatal(err)
	}
	secondDescriptor := migration.DataTransformDescriptor{Name: "second-transform", Checksum: migration.DataTransformChecksum([]byte("second-transform-v1"))}
	if _, err := CreateArtifact(ctx, directory, "second-transform", manifest, time.Unix(3, 0), false, secondDescriptor); err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 3 {
		t.Fatalf("artifact history = %#v, %v", files, err)
	}
	firstTransformPrefix := strings.SplitN(files[1].Name, "_", 2)[0]
	secondTransformPrefix := strings.SplitN(files[2].Name, "_", 2)[0]
	temporary := files[1].Path + ".swap"
	if err := os.Rename(files[1].Path, temporary); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(files[2].Path, filepath.Join(directory, firstTransformPrefix+"_second-transform"+migrationartifact.Extension)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, filepath.Join(directory, secondTransformPrefix+"_first-transform"+migrationartifact.Extension)); err != nil {
		t.Fatal(err)
	}

	var calls []string
	callback := func(name string) migration.DataTransform {
		descriptor := firstDescriptor
		if name == secondDescriptor.Name {
			descriptor = secondDescriptor
		}
		return migration.DataTransform{
			DataTransformDescriptor: descriptor,
			Up: func(context.Context, migration.DataTransaction) error {
				calls = append(calls, "up:"+name)
				return nil
			},
			Down: func(context.Context, migration.DataTransaction) error { return nil },
		}
	}
	err = VerifyArtifacts(ctx, directory, callback(firstDescriptor.Name), callback(secondDescriptor.Name))
	if err == nil || !strings.Contains(err.Error(), "predecessor differs") {
		t.Fatalf("reordered same-digest replay error = %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("reordered callbacks executed before lineage rejection: %v", calls)
	}
}

func TestSQLiteDataOnlyExpectedArtifactHeadControlsReadiness(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := sqliteMigrationManifest(t, false)
	if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}

	descriptor := migration.DataTransformDescriptor{Name: "data-only", Checksum: migration.DataTransformChecksum([]byte("data-only-head-v1"))}
	if _, err := CreateArtifact(ctx, directory, "data-only", manifest, time.Unix(2, 0), false, descriptor); err != nil {
		t.Fatal(err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("never-applied data-only status = %#v", statuses)
	}

	calls := 0
	transform := migration.DataTransform{
		DataTransformDescriptor: descriptor,
		Up: func(context.Context, migration.DataTransaction) error {
			calls++
			return nil
		},
		Down: func(context.Context, migration.DataTransaction) error {
			calls--
			return nil
		},
	}
	if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("ready at applied data-only head: %v", err)
	}

	if _, err := backend.db.ExecContext(ctx, `UPDATE ridu_sqlite_schema SET expected_artifact_digest = ? WHERE singleton = 1`, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, manifest); err == nil || !strings.Contains(err.Error(), "expected artifact digest") {
		t.Fatalf("tampered expected head readiness error = %v", err)
	}
	if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
		t.Fatalf("no-op apply did not restore expected head: %v", err)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("ready after no-op head restore: %v", err)
	}

	if err := backend.DownArtifacts(ctx, directory, transform); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("data-only callback balance after down = %d", calls)
	}
	if err := backend.Ready(ctx, manifest); err == nil || !strings.Contains(err.Error(), "expected artifact digest") {
		t.Fatalf("rolled-back data-only head readiness error = %v", err)
	}
	statuses, err = backend.ArtifactStatus(ctx, directory, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("rolled-back data-only status = %#v", statuses)
	}

	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	olderDirectory := t.TempDir()
	initialBytes, err := os.ReadFile(files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(olderDirectory, files[0].Name), initialBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, olderDirectory); err != nil {
		t.Fatalf("older checkout no-op apply: %v", err)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("older checkout readiness: %v", err)
	}

	if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
		t.Fatalf("reapply data-only head: %v", err)
	}
	if calls != 1 {
		t.Fatalf("data-only callback calls after reapply = %d", calls)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("ready after data-only reapply: %v", err)
	}
}

func TestSQLiteProjectMigrationDriverRejectsIncompleteCallbacks(t *testing.T) {
	descriptor := migration.DataTransformDescriptor{
		Name: "incomplete-transform", Checksum: migration.DataTransformChecksum([]byte("incomplete-v1")),
	}
	driver := ProjectMigrations(migration.DataTransform{DataTransformDescriptor: descriptor})
	if err := driver.Validate(); err == nil || !strings.Contains(err.Error(), "requires both up and down callbacks") {
		t.Fatalf("driver validation error = %v, want incomplete callback rejection", err)
	}
}

func TestSQLiteMigrationAllowsCallerIDPolicyChange(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	initial := sqliteMigrationManifest(t, false)
	updatedSnapshot := initial.Snapshot()
	updatedSnapshot.Application.AllowIDOnCreate = true
	updated := schema.NewManifest(updatedSnapshot)
	first, err := planArtifact(ctx, "initial", nil, initial, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", first, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	second, err := planArtifact(ctx, "allow-caller-ids", &initial, updated, false)
	if err != nil {
		t.Fatalf("plan caller-ID policy change: %v", err)
	}
	if _, err := migrationartifact.Create(directory, "allow-caller-ids", second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("apply caller-ID policy change: %v", err)
	}
	if err := backend.Ready(ctx, updated); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteDataOnlyArtifactAndReviewedFieldRename(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	initial := sqliteMigrationManifest(t, false)
	first, err := planArtifact(ctx, "initial", nil, initial, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", first, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}

	dataOnly := migration.DataTransformDescriptor{Name: "data-only", Checksum: migration.DataTransformChecksum([]byte("data-only-v1"))}
	second, err := planArtifact(ctx, "data-only", &initial, initial, false, dataOnly)
	if err != nil {
		t.Fatalf("build data-only artifact: %v", err)
	}
	if _, err := migrationartifact.Create(directory, "data-only", second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	called := 0
	noop := migration.DataTransform{DataTransformDescriptor: dataOnly,
		Up:   func(context.Context, migration.DataTransaction) error { called++; return nil },
		Down: func(context.Context, migration.DataTransaction) error { called--; return nil },
	}
	if err := backend.ApplyArtifacts(ctx, directory, noop); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("data-only transform calls = %d, want 1", called)
	}

	renamed, err := ridu.Resolve(ridu.Config{Name: "SQLite migrations", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("headline").Required()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	rename := migration.DataTransformDescriptor{Name: "rename-title", Checksum: migration.DataTransformChecksum([]byte("rename-title-v1"))}
	if _, err := planArtifact(ctx, "rename-title", &initial, renamed, false, rename); err == nil {
		t.Fatal("reviewed field rename did not require destructive approval")
	} else {
		var safety *SafetyError
		if !errors.As(err, &safety) {
			t.Fatalf("field rename error = %v, want SafetyError", err)
		}
	}
	if _, err := planArtifact(ctx, "rename-title", &initial, renamed, true, rename); err != nil {
		t.Fatalf("build reviewed field rename: %v", err)
	}

	removedSnapshot := initial.Snapshot()
	removedSnapshot.Collections = nil
	removed := schema.NewManifest(removedSnapshot)
	if _, err := planArtifact(ctx, "remove-posts", &initial, removed, true, rename); err == nil || !strings.Contains(err.Error(), "typed resource-retirement") {
		t.Fatalf("collection retirement error = %v, want typed retirement requirement", err)
	}
}

func TestSQLiteDataTransformDownPreservesRestoredFieldDuringRollbackScrub(t *testing.T) {
	ctx := context.Background()
	before, err := ridu.Resolve(ridu.Config{Name: "SQLite rollback transform", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("title"), field.Group("metadata", field.Fields{field.Text("stable")})},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := ridu.Resolve(ridu.Config{Name: "SQLite rollback transform", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("headline"), field.Group("metadata", field.Fields{field.Text("stable"), field.Text("temporary")})},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	initial, err := planArtifact(ctx, "initial", nil, before, false)
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
	beforeCollection := before.Snapshot().Collections[0]
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.Create(ctx, store.CreateRequest{
		Collection: beforeCollection,
		ID:         "post-1",
		Values: store.Values{
			"title":    store.String("Restored title"),
			"metadata": store.Object(store.Values{"stable": store.String("keep")}),
		},
	}); err != nil {
		_ = write.Rollback(ctx)
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	descriptor := migration.DataTransformDescriptor{
		Name: "rename-title", Checksum: migration.DataTransformChecksum([]byte("rename-title-rollback-v1")),
	}
	rename, err := planArtifact(ctx, "rename-title", &before, after, true, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "rename-title", rename, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	afterCollection := after.Snapshot().Collections[0]
	transform := migration.DataTransform{
		DataTransformDescriptor: descriptor,
		Up: func(ctx context.Context, transaction migration.DataTransaction) error {
			document, err := transaction.Find(ctx, store.Request{Collection: beforeCollection, ID: "post-1"})
			if err != nil {
				return err
			}
			title, ok := document.Values["title"].StringValue()
			if !ok {
				return errors.New("up transform could not read title")
			}
			_, err = transaction.Update(ctx, store.UpdateRequest{
				Request: store.Request{Collection: afterCollection, ID: document.ID},
				Values: store.Values{
					"headline": store.String(title),
					"metadata": store.Object(store.Values{"temporary": store.String("remove on down")}),
				},
			})
			return err
		},
		Down: func(ctx context.Context, transaction migration.DataTransaction) error {
			document, err := transaction.Find(ctx, store.Request{Collection: afterCollection, ID: "post-1"})
			if err != nil {
				return err
			}
			headline, ok := document.Values["headline"].StringValue()
			if !ok {
				return errors.New("down transform could not read headline")
			}
			_, err = transaction.Update(ctx, store.UpdateRequest{
				Request: store.Request{Collection: beforeCollection, ID: document.ID},
				Values:  store.Values{"title": store.String(headline)},
			})
			return err
		},
	}
	if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
		t.Fatal(err)
	}
	if err := backend.DownArtifacts(ctx, directory, transform); err != nil {
		t.Fatal(err)
	}

	var encoded string
	if err := backend.db.QueryRowContext(ctx, `SELECT values_json FROM ridu_documents WHERE collection_id = ? AND id = 'post-1'`, string(beforeCollection.ID)).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var values store.Values
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		t.Fatal(err)
	}
	if title, ok := values["title"].StringValue(); !ok || title != "Restored title" {
		t.Fatalf("rollback lost Down-restored title: %s", encoded)
	}
	if _, exists := values["headline"]; exists {
		t.Fatalf("rollback retained after-only headline: %s", encoded)
	}
	metadata, ok := values["metadata"].CopyObject()
	if !ok {
		t.Fatalf("rollback lost metadata: %s", encoded)
	}
	if stable, ok := metadata["stable"].StringValue(); !ok || stable != "keep" {
		t.Fatalf("rollback lost stable nested value: %s", encoded)
	}
	if _, exists := metadata["temporary"]; exists {
		t.Fatalf("rollback retained after-only nested value: %s", encoded)
	}
}

func TestSQLiteReviewedTransformsRemainFailClosedOutsideUnversionedFields(t *testing.T) {
	ctx := context.Background()
	descriptor := migration.DataTransformDescriptor{Name: "rewrite-posts", Checksum: migration.DataTransformChecksum([]byte("rewrite-posts-v1"))}
	initial := sqliteMigrationManifest(t, false)

	applicationChangedSnapshot := initial.Snapshot()
	applicationChangedSnapshot.Application.Localization = &schema.LocalizationSettings{
		DefaultLocale: "en", Locales: []schema.Locale{{Code: "en", Label: "English"}},
	}
	applicationChanged := schema.NewManifest(applicationChangedSnapshot)
	if _, err := planArtifact(ctx, "application-change", &initial, applicationChanged, true, descriptor); err == nil || !strings.Contains(err.Error(), "application settings") {
		t.Fatalf("application setting transform error = %v", err)
	}

	capabilityChanged, err := ridu.Resolve(ridu.Config{Name: "SQLite migrations", Collections: []ridu.Collection{{
		Slug: "posts", Trash: true, Fields: field.Fields{field.Text("title").Required()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planArtifact(ctx, "capability-change", &initial, capabilityChanged, true, descriptor); err == nil || !strings.Contains(err.Error(), "outside its fields") {
		t.Fatalf("capability transform error = %v", err)
	}

	versionedBefore, err := ridu.Resolve(ridu.Config{Name: "Versioned SQLite migration", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title").Required()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	versionedAfter, err := ridu.Resolve(ridu.Config{Name: "Versioned SQLite migration", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, Fields: field.Fields{field.Text("headline").Required()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planArtifact(ctx, "versioned-rename", &versionedBefore, versionedAfter, true, descriptor); err == nil || !strings.Contains(err.Error(), "retained snapshots") {
		t.Fatalf("versioned field transform error = %v", err)
	}
}

func TestSQLiteDataTransformCannotDivergeVersionHistory(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	config := ridu.Config{Name: "Versioned SQLite data transform", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title").Required()},
	}}}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	first, err := planArtifact(ctx, "initial", nil, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", first, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Original")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := migration.DataTransformDescriptor{Name: "rewrite-versioned", Checksum: migration.DataTransformChecksum([]byte("rewrite-versioned-v1"))}
	second, err := planArtifact(ctx, "rewrite-versioned", &manifest, manifest, false, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "rewrite-versioned", second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	transform := migration.DataTransform{
		DataTransformDescriptor: descriptor,
		Up: func(ctx context.Context, transaction migration.DataTransaction) error {
			_, err := transaction.Update(ctx, store.UpdateRequest{
				Request: store.Request{Collection: collection, ID: document.ID},
				Values:  store.Values{"title": store.String("Diverged")},
			})
			return err
		},
		Down: func(context.Context, migration.DataTransaction) error { return nil },
	}
	if err := backend.ApplyArtifacts(ctx, directory, transform); err == nil || !strings.Contains(err.Error(), "retained snapshots") {
		t.Fatalf("versioned transform apply error = %v", err)
	}
	stored, err := application.Local().Find(ctx, "posts", document.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := stored.Values["title"].StringValue(); title != "Original" || stored.Revision != document.Revision {
		t.Fatalf("rejected transform changed current document: %#v", stored)
	}
	versions, err := application.Local().Versions(ctx, "posts", document.ID, ridu.FindOptions{})
	if err != nil || len(versions) != 1 || versions[0].Revision != document.Revision {
		t.Fatalf("rejected transform changed history: %#v, %v", versions, err)
	}
}

func TestSQLiteDataTransformNameIsChecksumBoundAcrossHistory(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := sqliteMigrationManifest(t, false)
	initialDescriptor := migration.DataTransformDescriptor{Name: "normalize-posts", Checksum: migration.DataTransformChecksum([]byte("normalize-posts-initial"))}
	if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), false, initialDescriptor); err != nil {
		t.Fatal(err)
	}
	changedDescriptor := migration.DataTransformDescriptor{Name: initialDescriptor.Name, Checksum: migration.DataTransformChecksum([]byte("normalize-posts-changed"))}
	if _, err := CreateArtifact(ctx, directory, "changed-callback", manifest, time.Unix(2, 0), false, changedDescriptor); err == nil || !strings.Contains(err.Error(), "use a new transform name") {
		t.Fatalf("changed transform identity error = %v", err)
	}
	replacement := migration.DataTransformDescriptor{Name: "normalize-posts-replacement", Checksum: changedDescriptor.Checksum}
	if _, err := CreateArtifact(ctx, directory, "new-callback", manifest, time.Unix(2, 0), false, replacement); err != nil {
		t.Fatalf("new transform identity: %v", err)
	}
	noop := func(context.Context, migration.DataTransaction) error { return nil }
	if err := VerifyArtifacts(ctx, directory,
		migration.DataTransform{DataTransformDescriptor: initialDescriptor, Up: noop, Down: noop},
		migration.DataTransform{DataTransformDescriptor: replacement, Up: noop, Down: noop},
	); err != nil {
		t.Fatalf("shadow replay with immutable transform identities: %v", err)
	}

	unsafeDirectory := t.TempDir()
	first, err := planArtifact(ctx, "initial", nil, manifest, false, initialDescriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(unsafeDirectory, "initial", first, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	initialTransform := migration.DataTransform{DataTransformDescriptor: initialDescriptor, Up: noop, Down: noop}
	if err := backend.ApplyArtifacts(ctx, unsafeDirectory, initialTransform); err != nil {
		t.Fatal(err)
	}
	changed, err := planArtifact(ctx, "changed-callback", &manifest, manifest, false, changedDescriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(unsafeDirectory, "changed-callback", changed, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	changedTransform := migration.DataTransform{DataTransformDescriptor: changedDescriptor, Up: noop, Down: noop}
	if err := backend.ApplyArtifacts(ctx, unsafeDirectory, changedTransform); err == nil || !strings.Contains(err.Error(), "use a new transform name") {
		t.Fatalf("incremental apply accepted changed transform identity: %v", err)
	}
}

func sqliteRollbackResourceManifest(t *testing.T, additive bool) schema.Manifest {
	t.Helper()
	keepers := ridu.Collection{
		Slug: "keepers", Versions: true,
		Fields: field.Fields{field.Text("title")},
	}
	config := ridu.Config{Name: "SQLite rollback resources", Collections: []ridu.Collection{keepers}}
	if additive {
		keepers.Fields = append(keepers.Fields, field.Relationship("retiredUser", "retired-users").OnDelete(field.ReferenceDeleteRestrict))
		config.Admin = ridu.AdminConfig{User: "retired-users"}
		config.Collections = []ridu.Collection{
			keepers,
			{
				Slug: "retired-users", Auth: true, Versions: true, LockDocuments: true,
				Fields: field.Fields{field.Email("email").Required().Unique()},
			},
		}
		config.Globals = []ridu.Global{{
			Slug: "retired-settings", Versions: true,
			Fields: field.Fields{field.Text("title")},
		}}
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func sqliteResourceBySlug(t *testing.T, manifest schema.Manifest, slug schema.CollectionSlug) schema.Collection {
	t.Helper()
	snapshot := manifest.Snapshot()
	for _, resource := range append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...) {
		if resource.Slug == slug {
			return resource
		}
	}
	t.Fatalf("resource %s is absent", slug)
	return schema.Collection{}
}

func assertSQLiteLifecycleCount(t *testing.T, backend *Store, query string, arguments ...any) {
	t.Helper()
	var count int
	if err := backend.db.QueryRowContext(context.Background(), query, arguments...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("retired framework state count = %d for %s", count, query)
	}
}
