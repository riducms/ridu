package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	modernsqlite "modernc.org/sqlite"
)

func TestSQLiteArtifactsApplyInitialAndAdditiveHistory(t *testing.T) {
	ctx := context.Background()
	initial := sqliteMigrationManifest(t, false)
	additive := sqliteMigrationManifest(t, true)
	summaryIndexName := documentFieldIndexName(additive.Snapshot().Collections[0].ID, "summary", nil)
	directory := t.TempDir()
	first, err := planArtifact(ctx, "initial", nil, initial, false)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := planArtifact(ctx, "initial", nil, initial, false)
	if err != nil {
		t.Fatal(err)
	}
	firstDigest, _ := first.Digest()
	rebuiltDigest, _ := rebuilt.Digest()
	if firstDigest != rebuiltDigest {
		t.Fatalf("offline planner is not deterministic: %s != %s", firstDigest, rebuiltDigest)
	}
	firstFile, err := migrationartifact.Create(directory, "initial", first, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}

	backend, err := Open(ctx, filepath.Join(t.TempDir(), "application.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	var managed int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name LIKE 'ridu_%'`).Scan(&managed); err != nil {
		t.Fatal(err)
	}
	if managed != 0 {
		t.Fatalf("Open mutated SQLite schema: %d managed objects", managed)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, status, revision, values_json)
VALUES (?, ?, ?, ?, '', 0, ?)`, string(initial.Snapshot().Collections[0].ID), "existing", int64(1), int64(1), `{"title":"Before","summary":"Already stored"}`); err != nil {
		t.Fatal(err)
	}

	second, err := planArtifact(ctx, "add-summary", &initial, additive, false)
	if err != nil {
		t.Fatal(err)
	}
	secondFile, err := migrationartifact.Create(directory, "add-summary", second, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	status, err := backend.ArtifactStatus(ctx, directory, additive)
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 2 || !status[0].Applied || status[1].Applied ||
		status[0].Name != firstFile.Name || status[1].Name != secondFile.Name {
		t.Fatalf("stale migration status = %#v", status)
	}
	if err := backend.Ready(ctx, additive); err == nil || !strings.Contains(err.Error(), "does not match executable digest") {
		t.Fatalf("stale readiness error = %v", err)
	}

	// Shadow verification must replay the full history without advancing the
	// application database that is deliberately still one artifact behind.
	if err := VerifyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	status, err = backend.ArtifactStatus(ctx, directory, additive)
	if err != nil {
		t.Fatal(err)
	}
	if status[1].Applied {
		t.Fatal("shadow verification mutated the application database")
	}

	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, additive); err != nil {
		t.Fatal(err)
	}
	if err := backend.verifyImmutableReadyState(ctx, additive); err != nil {
		t.Fatal(err)
	}
	var indexCount int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, summaryIndexName).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatalf("manifest-owned summary index count = %d", indexCount)
	}
	var uniqueDocument string
	if err := backend.db.QueryRowContext(ctx, `SELECT document_id FROM ridu_unique_values WHERE collection_id = ? AND document_id = ?`, string(additive.Snapshot().Collections[0].ID), "existing").Scan(&uniqueDocument); err != nil {
		t.Fatal(err)
	}
	if uniqueDocument != "existing" {
		t.Fatalf("rebuilt unique value belongs to %q", uniqueDocument)
	}
	status, err = backend.ArtifactStatus(ctx, directory, additive)
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 2 || !status[0].Applied || !status[1].Applied || status[1].Phases[0].State != "complete" {
		t.Fatalf("completed migration status = %#v", status)
	}
	if _, err := backend.db.ExecContext(ctx, "DROP INDEX "+quoteSQLiteIdentifier(summaryIndexName)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ArtifactStatus(ctx, directory, additive); err == nil || !strings.Contains(err.Error(), "physical schema drift") {
		t.Fatalf("manifest-owned index drift error = %v", err)
	}
	if err := backend.verifyImmutableReadyState(ctx, additive); err == nil || !strings.Contains(err.Error(), "physical schema drift") {
		t.Fatalf("manifest-owned index readiness error = %v", err)
	}
}

func TestSQLiteArtifactInspectionDoesNotCreateOrChangeDatabase(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteMigrationManifest(t, false)
	directory := t.TempDir()
	artifact, err := planArtifact(ctx, "initial", nil, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}

	missingPath := filepath.Join(t.TempDir(), "missing.sqlite")
	statuses, err := InspectArtifacts(ctx, missingPath, directory, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Applied {
		t.Fatalf("missing database status = %#v", statuses)
	}
	if _, err := os.Stat(missingPath); !os.IsNotExist(err) {
		t.Fatalf("inspection created missing database: %v", err)
	}

	databasePath := filepath.Join(t.TempDir(), "application.sqlite")
	backend, err := OpenWithConfig(ctx, Config{Path: databasePath, DisableWAL: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		backend.Close()
		t.Fatal(err)
	}
	var journalMode string
	if err := backend.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		backend.Close()
		t.Fatal(err)
	}
	if journalMode != "delete" {
		backend.Close()
		t.Fatalf("initial journal mode = %q", journalMode)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	statuses, err = InspectArtifacts(ctx, databasePath, directory, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || !statuses[0].Applied {
		t.Fatalf("applied database status = %#v", statuses)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(databasePath + suffix); !os.IsNotExist(err) {
			t.Fatalf("inspection created %s sidecar: %v", suffix, err)
		}
	}

	backend, err = OpenWithConfig(ctx, Config{Path: databasePath, DisableWAL: true})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := backend.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "delete" {
		t.Fatalf("journal mode after inspection = %q", journalMode)
	}
}

func TestSQLiteArtifactAllowsRecursiveOptionalFieldAdditions(t *testing.T) {
	ctx := context.Background()
	resolve := func(additive bool) schema.Manifest {
		t.Helper()
		metaFields := field.Fields{field.Text("title")}
		sectionFields := field.Fields{field.Text("heading")}
		heroFields := field.Fields{field.Text("heading")}
		blocks := []field.Block{field.Block{Slug: "hero", Fields: heroFields}}
		if additive {
			metaFields = append(metaFields, field.Text("description"))
			sectionFields = append(sectionFields, field.Text("caption"))
			heroFields = append(heroFields, field.Text("eyebrow"))
			blocks = []field.Block{field.Block{Slug: "hero", Fields: heroFields}, field.Block{Slug: "quote", Fields: field.Fields{field.Text("body").Required()}}}
		}
		manifest, err := ridu.Resolve(ridu.Config{
			Name: "SQLite recursive migrations",
			Collections: []ridu.Collection{{
				Slug:   "posts",
				Fields: field.Fields{field.Group("meta", metaFields), field.Array("sections", sectionFields), field.Blocks("layout", blocks...)},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	initial := resolve(false)
	additive := resolve(true)
	directory := t.TempDir()
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
	collectionID := string(initial.Snapshot().Collections[0].ID)
	stored := `{"meta":{"title":"Before"},"sections":[{"heading":"One"}],"layout":[{"blockType":"hero","heading":"Welcome"}]}`
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, values_json)
VALUES (?, 'post-1', 1, 1, ?)`, collectionID, stored); err != nil {
		t.Fatal(err)
	}
	second, err := planArtifact(ctx, "add-nested-fields", &initial, additive, false)
	if err != nil {
		t.Fatalf("plan recursive optional additions: %v", err)
	}
	if _, err := migrationartifact.Create(directory, "add-nested-fields", second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("apply recursive optional additions: %v", err)
	}
	if err := backend.Ready(ctx, additive); err != nil {
		t.Fatal(err)
	}
	var preserved string
	if err := backend.db.QueryRowContext(ctx, `SELECT values_json FROM ridu_documents WHERE collection_id = ? AND id = 'post-1'`, collectionID).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if preserved != stored {
		t.Fatalf("recursive additive artifact rewrote existing JSON:\n got %s\nwant %s", preserved, stored)
	}
}

func TestSQLiteArtifactAllowsAppendOnlyCollectionIndexes(t *testing.T) {
	ctx := context.Background()
	resolve := func(indexed bool) schema.Manifest {
		t.Helper()
		collection := ridu.Collection{Slug: "posts", Fields: field.Fields{field.Text("tenant"), field.Text("slug")}}
		if indexed {
			collection.Indexes = []ridu.CollectionIndex{{Fields: []string{"tenant", "slug"}}}
		}
		manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite additive compound index", Collections: []ridu.Collection{collection}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before := resolve(false)
	after := resolve(true)
	artifact, err := planArtifact(ctx, "add-compound", &before, after, false)
	if err != nil {
		t.Fatalf("plan append-only compound index: %v", err)
	}
	directory := t.TempDir()
	initial, err := planArtifact(ctx, "initial", nil, before, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "add-compound", artifact, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collection := after.Snapshot().Collections[0]
	name := documentCompoundIndexName(collection.ID, []string{"tenant", "slug"}, nil)
	var count int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("append-only compound index count = %d", count)
	}
}

func TestSQLiteArtifactAddsIndexAndUniquenessAtomically(t *testing.T) {
	ctx := context.Background()
	initial := sqliteMigrationManifest(t, false)
	indexed, err := ridu.Resolve(ridu.Config{
		Name: "SQLite migrations",
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title").Index().Unique()},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	titleIndexName := documentFieldIndexName(indexed.Snapshot().Collections[0].ID, "title", nil)
	directory := t.TempDir()
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
	collectionID := string(initial.Snapshot().Collections[0].ID)
	for index := 1; index <= 2; index++ {
		if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, status, revision, values_json)
VALUES (?, ?, ?, ?, '', 0, '{"title":"duplicate"}')`, collectionID, fmt.Sprintf("post-%d", index), index, index); err != nil {
			t.Fatal(err)
		}
	}
	second, err := planArtifact(ctx, "index-title", &initial, indexed, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "index-title", second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil {
		t.Fatal("unique-index migration accepted duplicate existing values")
	}
	var applied, titleIndex int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, titleIndexName).Scan(&titleIndex); err != nil {
		t.Fatal(err)
	}
	if applied != 1 || titleIndex != 0 {
		t.Fatalf("failed migration committed ledger/index state: applied=%d index=%d", applied, titleIndex)
	}
	if err := backend.Ready(ctx, initial); err != nil {
		t.Fatalf("failed migration changed readiness: %v", err)
	}
	if _, err := backend.db.ExecContext(ctx, `DELETE FROM ridu_documents WHERE collection_id = ? AND id = 'post-2'`, collectionID); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, indexed); err != nil {
		t.Fatal(err)
	}
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, titleIndexName).Scan(&titleIndex); err != nil {
		t.Fatal(err)
	}
	if titleIndex != 1 {
		t.Fatalf("title expression index count = %d", titleIndex)
	}
}

func TestSQLiteArtifactRebuildsAddedRelationshipReferencesAtomically(t *testing.T) {
	ctx := context.Background()
	withoutRelationship, err := ridu.Resolve(ridu.Config{
		Name: "SQLite artifact references",
		Collections: []ridu.Collection{
			{Slug: "tags", Fields: field.Fields{field.Text("name")}},
			{Slug: "posts"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	withRelationship, err := ridu.Resolve(ridu.Config{
		Name: "SQLite artifact references",
		Collections: []ridu.Collection{
			{Slug: "tags", Fields: field.Fields{field.Text("name")}},
			{Slug: "posts", Fields: field.Fields{field.Relationship("tag", "tags")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var tagsID, postsID schema.StableID
	for _, collection := range withoutRelationship.Snapshot().Collections {
		switch collection.Slug {
		case "tags":
			tagsID = collection.ID
		case "posts":
			postsID = collection.ID
		}
	}

	for _, test := range []struct {
		name      string
		insertTag bool
		wantApply bool
		wantRefs  int
	}{
		{name: "existing target", insertTag: true, wantApply: true, wantRefs: 1},
		{name: "missing target rolls back", wantApply: false, wantRefs: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			initial, err := planArtifact(ctx, "initial", nil, withoutRelationship, false)
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
			if test.insertTag {
				if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, values_json)
VALUES (?, 'tag-1', 1, 1, '{"name":"Tag"}')`, string(tagsID)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, values_json)
VALUES (?, 'post-1', 1, 1, '{"tag":"tag-1"}')`, string(postsID)); err != nil {
				t.Fatal(err)
			}
			addRelationship, err := planArtifact(ctx, "add-relationship", &withoutRelationship, withRelationship, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := migrationartifact.Create(directory, "add-relationship", addRelationship, time.Unix(2, 0)); err != nil {
				t.Fatal(err)
			}
			err = backend.ApplyArtifacts(ctx, directory)
			if test.wantApply && err != nil {
				t.Fatal(err)
			}
			if !test.wantApply && err == nil {
				t.Fatal("relationship migration accepted a missing target")
			}
			var references int
			if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_document_references`).Scan(&references); err != nil {
				t.Fatal(err)
			}
			if references != test.wantRefs {
				t.Fatalf("references = %d, want %d", references, test.wantRefs)
			}
			if !test.wantApply {
				var applied int
				if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_migrations`).Scan(&applied); err != nil {
					t.Fatal(err)
				}
				if applied != 1 {
					t.Fatalf("failed relationship migration committed %d artifacts", applied)
				}
				if err := backend.Ready(ctx, withoutRelationship); err != nil {
					t.Fatalf("failed relationship migration changed readiness: %v", err)
				}
			}
		})
	}
}

func TestSQLiteArtifactStatusRejectsEditedHistoryAndLedgerLineage(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteMigrationManifest(t, false)

	t.Run("edited artifact", func(t *testing.T) {
		directory := t.TempDir()
		artifact, err := planArtifact(ctx, "initial", nil, manifest, false)
		if err != nil {
			t.Fatal(err)
		}
		file, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		backend := newSQLiteMigrationStore(t)
		if err := backend.ApplyArtifacts(ctx, directory); err != nil {
			t.Fatal(err)
		}
		encoded, err := os.ReadFile(file.Path)
		if err != nil {
			t.Fatal(err)
		}
		edited, err := migration.DecodeArtifact(encoded)
		if err != nil {
			t.Fatal(err)
		}
		edited.Planner.Version = "1.0.1"
		encoded, err = json.MarshalIndent(edited, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file.Path, append(encoded, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.ArtifactStatus(ctx, directory, manifest); err == nil || !strings.Contains(err.Error(), "unsupported planner version") {
			t.Fatalf("edited artifact status error = %v", err)
		}
		if _, err := CreateArtifact(ctx, directory, "after-edited", manifest, time.Unix(2, 0), false); err == nil || !strings.Contains(err.Error(), "unsupported planner version") {
			t.Fatalf("creation over edited history error = %v", err)
		}
	})

	t.Run("ledger lineage", func(t *testing.T) {
		directory := t.TempDir()
		artifact, err := planArtifact(ctx, "initial", nil, manifest, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
			t.Fatal(err)
		}
		backend := newSQLiteMigrationStore(t)
		if err := backend.ApplyArtifacts(ctx, directory); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.db.ExecContext(ctx, `UPDATE ridu_migrations SET to_digest = ? WHERE position = 1`, strings.Repeat("0", 64)); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.ArtifactStatus(ctx, directory, manifest); err == nil || !strings.Contains(err.Error(), "manifest lineage differs") {
			t.Fatalf("ledger lineage status error = %v", err)
		}
		if err := backend.verifyImmutableReadyState(ctx, manifest); err == nil || !strings.Contains(err.Error(), "does not match executable digest") {
			t.Fatalf("ledger lineage readiness error = %v", err)
		}
	})

	t.Run("ledger planner provenance", func(t *testing.T) {
		directory := t.TempDir()
		artifact, err := planArtifact(ctx, "initial", nil, manifest, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
			t.Fatal(err)
		}
		backend := newSQLiteMigrationStore(t)
		if err := backend.ApplyArtifacts(ctx, directory); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.db.ExecContext(ctx, `UPDATE ridu_migrations SET planner_version = '9.0.0' WHERE position = 1`); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.ArtifactStatus(ctx, directory, manifest); err == nil || !strings.Contains(err.Error(), "planner provenance differs") {
			t.Fatalf("ledger planner status error = %v", err)
		}
		if err := backend.verifyImmutableReadyState(ctx, manifest); err == nil || !strings.Contains(err.Error(), "unsupported planner version") {
			t.Fatalf("ledger planner readiness error = %v", err)
		}
	})
}

func TestSQLiteArtifactStatusDetectsPhysicalDrift(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := sqliteMigrationManifest(t, false)
	artifact, err := planArtifact(ctx, "initial", nil, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `DROP INDEX ridu_documents_active`); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ArtifactStatus(ctx, directory, manifest); err == nil || !strings.Contains(err.Error(), "physical schema drift") {
		t.Fatalf("physical drift status error = %v", err)
	}
}

func TestSQLitePhysicalSchemaClassifiesApplicationObjectsOnManagedTables(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteMigrationManifest(t, false)
	for _, test := range []struct {
		name       string
		statement  string
		objectKey  string
		wantReject bool
		tempShadow bool
		writeError string
	}{
		{
			name:      "ordinary non-unique index",
			statement: `CREATE INDEX application_lookup ON ridu_documents (created_at)`,
			objectKey: "index:application_lookup",
		},
		{
			name:       "mixed-case reserved Ridu non-unique index",
			statement:  `CREATE INDEX RIDU_stale_lookup ON ridu_documents (created_at)`,
			objectKey:  "index:RIDU_stale_lookup",
			wantReject: true,
		},
		{
			name:       "non-unique expression index",
			statement:  `CREATE INDEX application_expression_hazard ON ridu_documents (json_extract('not json', '$.x'))`,
			objectKey:  "index:application_expression_hazard",
			wantReject: true,
			writeError: "malformed JSON",
		},
		{
			name:       "partial non-unique index",
			statement:  `CREATE INDEX application_partial_lookup ON ridu_documents (created_at) WHERE status <> ''`,
			objectKey:  "index:application_partial_lookup",
			wantReject: true,
		},
		{
			name:       "unique index",
			statement:  `CREATE UNIQUE INDEX application_unique_lookup ON ridu_documents (created_at)`,
			objectKey:  "index:application_unique_lookup",
			wantReject: true,
			tempShadow: true,
		},
		{
			name:       "trigger",
			statement:  `CREATE TRIGGER application_audit AFTER INSERT ON ridu_documents BEGIN SELECT 1; END`,
			objectKey:  "trigger:application_audit",
			wantReject: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := newSQLiteMigrationStore(t)
			if err := backend.Migrate(ctx, manifest); err != nil {
				t.Fatal(err)
			}
			if _, err := backend.db.ExecContext(ctx, test.statement); err != nil {
				t.Fatal(err)
			}
			var runner sqlRunner = backend.db
			if test.tempShadow {
				connection, err := backend.db.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer connection.Close()
				if _, err := connection.ExecContext(ctx, `CREATE TEMP TABLE ridu_documents (created_at INTEGER)`); err != nil {
					t.Fatal(err)
				}
				if _, err := connection.ExecContext(ctx, `CREATE INDEX temp.application_unique_lookup ON ridu_documents (created_at)`); err != nil {
					t.Fatal(err)
				}
				runner = connection
			}
			if test.writeError != "" {
				transaction, err := backend.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := transaction.Create(ctx, store.CreateRequest{
					Collection: manifest.Snapshot().Collections[0],
					ID:         "valid-document",
					Values:     store.Values{"title": store.String("Valid title")},
				})
				if rollbackErr := transaction.Rollback(ctx); rollbackErr != nil {
					t.Fatal(rollbackErr)
				}
				if writeErr == nil || !strings.Contains(writeErr.Error(), test.writeError) {
					t.Fatalf("valid write error = %v, want %q", writeErr, test.writeError)
				}
			}
			if !test.wantReject {
				if err := assertSQLitePhysicalSchema(ctx, runner, manifest, false, currentSQLitePlannerContract()); err != nil {
					t.Fatalf("physical schema assertion rejected harmless index: %v", err)
				}
				if err := backend.Ready(ctx, manifest); err != nil {
					t.Fatalf("Ready rejected harmless index: %v", err)
				}
				return
			}
			want := "unexpected behavior-changing object " + test.objectKey
			if err := assertSQLitePhysicalSchema(ctx, runner, manifest, false, currentSQLitePlannerContract()); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("physical schema assertion error = %v, want %q", err, want)
			}
			if err := backend.Ready(ctx, manifest); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Ready error = %v, want %q", err, want)
			}
		})
	}
}

func TestSQLiteDevelopmentMigrateReadinessDoesNotRequireArtifactLedger(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteMigrationManifest(t, false)
	backend := newSQLiteMigrationStore(t)
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if err := backend.verifyImmutableReadyState(ctx, manifest); err != nil {
		t.Fatalf("development migration readiness = %v", err)
	}
	expectedHistory, err := migration.DigestArtifactHistory([]migration.ArtifactIdentity{{
		Name: "20260901000000.000000000_initial.ridu.json", Digest: strings.Repeat("a", 64),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, manifest, expectedHistory); err == nil || !strings.Contains(err.Error(), "migration ledger is missing") {
		t.Fatalf("exact readiness without artifact ledger error = %v", err)
	}
	if _, err := backend.db.ExecContext(ctx, `DROP INDEX ridu_documents_active`); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `CREATE INDEX ridu_documents_active ON ridu_documents (id)`); err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(ctx, manifest); err == nil || !strings.Contains(err.Error(), "physical schema drift") {
		t.Fatalf("development migration physical drift error = %v", err)
	}
	if err := backend.Ready(ctx, manifest); err == nil || !strings.Contains(err.Error(), "physical schema drift") {
		t.Fatalf("development physical drift readiness error = %v", err)
	}
}

func TestSQLiteDevelopmentReadyRechecksDigestInsideVerificationSnapshot(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteMigrationManifest(t, false)
	backend := newSQLiteMigrationStore(t)
	if err := backend.Ready(ctx, manifest); err == nil || !strings.Contains(err.Error(), "SQLite schema ledger is missing") {
		t.Fatalf("unmigrated readiness error = %v", err)
	}
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}

	writer, err := backend.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	backend.db.SetMaxIdleConns(0)

	verificationOpening := make(chan struct{})
	continueVerification := make(chan struct{})
	defer close(continueVerification)
	targetDSN := backend.dsn
	var matchingConnections atomic.Int32
	modernsqlite.RegisterConnectionHook(func(_ modernsqlite.ExecQuerierContext, dsn string) error {
		if dsn == targetDSN && matchingConnections.Add(1) == 2 {
			close(verificationOpening)
			<-continueVerification
		}
		return nil
	})

	readyErrors := make(chan error, 1)
	go func() {
		readyErrors <- backend.Ready(ctx, manifest)
	}()
	select {
	case <-verificationOpening:
	case <-time.After(5 * time.Second):
		t.Fatal("Ready did not reach the gated verification connection")
	}
	if _, err := writer.ExecContext(ctx, `UPDATE ridu_sqlite_schema SET manifest_digest = ? WHERE singleton = 1`, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	continueVerification <- struct{}{}
	select {
	case err := <-readyErrors:
		if err == nil || !strings.Contains(err.Error(), "does not match executable digest") {
			t.Fatalf("concurrently changed development digest readiness error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ready did not finish after releasing the verification connection")
	}
}

func TestSQLiteDevelopmentMigrateCannotBypassImmutableHistory(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteMigrationManifest(t, false)
	directory := t.TempDir()
	artifact, err := planArtifact(ctx, "initial", nil, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(ctx, manifest); err == nil || !strings.Contains(err.Error(), "immutable migration history") {
		t.Fatalf("development migration over immutable history error = %v", err)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("rejected development migration changed readiness: %v", err)
	}
}

func TestSQLiteDevelopmentMigrateRebuildsRelationshipReferences(t *testing.T) {
	ctx := context.Background()
	withoutRelationship, err := ridu.Resolve(ridu.Config{
		Name: "SQLite development references",
		Collections: []ridu.Collection{
			{Slug: "tags", Fields: field.Fields{field.Text("name")}},
			{Slug: "posts"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	withRelationship, err := ridu.Resolve(ridu.Config{
		Name: "SQLite development references",
		Collections: []ridu.Collection{
			{Slug: "tags", Fields: field.Fields{field.Text("name")}},
			{Slug: "posts", Fields: field.Fields{field.Relationship("tag", "tags")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.Migrate(ctx, withoutRelationship); err != nil {
		t.Fatal(err)
	}
	resources := withoutRelationship.Snapshot().Collections
	var tagsID, postsID schema.StableID
	for _, collection := range resources {
		switch collection.Slug {
		case "tags":
			tagsID = collection.ID
		case "posts":
			postsID = collection.ID
		}
	}
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, values_json)
VALUES (?, 'tag-1', 1, 1, '{"name":"Tag"}'),
       (?, 'post-1', 1, 1, '{"tag":"tag-1"}')`, string(tagsID), string(postsID)); err != nil {
		t.Fatal(err)
	}

	if err := backend.Migrate(ctx, withRelationship); err != nil {
		t.Fatal(err)
	}
	var references int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_document_references`).Scan(&references); err != nil {
		t.Fatal(err)
	}
	if references != 1 {
		t.Fatalf("references after adding relationship = %d, want 1", references)
	}
	if err := backend.Migrate(ctx, withoutRelationship); err != nil {
		t.Fatal(err)
	}
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_document_references`).Scan(&references); err != nil {
		t.Fatal(err)
	}
	if references != 0 {
		t.Fatalf("references after removing relationship = %d, want 0", references)
	}
}

func TestSQLiteBuildArtifactRejectsNonAdditiveSchema(t *testing.T) {
	ctx := context.Background()
	initial := sqliteMigrationManifest(t, false)
	removed := ridu.Config{Name: "SQLite migrations", Collections: []ridu.Collection{{Slug: "posts"}}}
	removedManifest, err := ridu.Resolve(removed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planArtifact(ctx, "remove-title", &initial, removedManifest, false); err == nil || !strings.Contains(err.Error(), "was removed") {
		t.Fatalf("non-additive planner error = %v", err)
	}

}

func TestNormalizeSQLiteSchemaSQLPreservesQuotedWhitespace(t *testing.T) {
	formatted := normalizeSQLiteSchemaSQL("  CREATE  TABLE example (\n value TEXT\t)  ")
	if formatted != "CREATE TABLE example ( value TEXT )" {
		t.Fatalf("normalized external whitespace = %q", formatted)
	}
	quotedDoubleSpace := normalizeSQLiteSchemaSQL(`CREATE TABLE example ("two  spaces" TEXT DEFAULT 'also  two')`)
	quotedSingleSpace := normalizeSQLiteSchemaSQL(`CREATE TABLE example ("two spaces" TEXT DEFAULT 'also two')`)
	if quotedDoubleSpace == quotedSingleSpace {
		t.Fatalf("quoted schema whitespace was collapsed: %q", quotedDoubleSpace)
	}
}

func TestSQLiteApplyRejectsUnsupportedExecutionBeforeMutation(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteMigrationManifest(t, false)
	artifact, err := migration.NewArtifact("unsupported", migration.Planner{
		Name: sqlitePlannerName, Version: sqlitePlannerVersion,
	}, nil, manifest)
	if err != nil {
		t.Fatal(err)
	}
	backfillPayload, err := migration.MarshalStepPayload(migration.BackfillReferencesPayload{BatchSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	assertPayload, err := migration.MarshalStepPayload(migration.AssertSchemaPayload{})
	if err != nil {
		t.Fatal(err)
	}
	physical := migration.PhysicalDigestSeed(artifact.FromDigest)
	backfill := migration.Step{ID: "step-0001", Kind: migration.StepBackfillReferences, ExecutorVersion: 1, Name: "unsupported backfill", Payload: backfillPayload}
	backfillDigest, err := migration.PhasePhysicalDigest(physical, migration.PhaseBatch, []migration.Step{backfill})
	if err != nil {
		t.Fatal(err)
	}
	assertion := migration.Step{ID: "step-0002", Kind: migration.StepAssertSchema, ExecutorVersion: 1, Name: "verify schema", Payload: assertPayload}
	assertDigest, err := migration.PhasePhysicalDigest(backfillDigest, migration.PhaseTransaction, []migration.Step{assertion})
	if err != nil {
		t.Fatal(err)
	}
	artifact.Phases = []migration.Phase{
		{ID: "phase-001", Mode: migration.PhaseBatch, PhysicalContractVersion: migration.PhysicalContractVersion, BeforePhysicalDigest: physical, AfterPhysicalDigest: backfillDigest, Steps: []migration.Step{backfill}},
		{ID: "phase-002", Mode: migration.PhaseTransaction, PhysicalContractVersion: migration.PhysicalContractVersion, BeforePhysicalDigest: backfillDigest, AfterPhysicalDigest: assertDigest, Steps: []migration.Step{assertion}},
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if _, err := migrationartifact.Create(directory, "unsupported", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "unsupported phase mode") {
		t.Fatalf("unsupported execution error = %v", err)
	}
	var managed int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name LIKE 'ridu_%'`).Scan(&managed); err != nil {
		t.Fatal(err)
	}
	if managed != 0 {
		t.Fatalf("unsupported artifact mutated SQLite schema: %d managed objects", managed)
	}
}

func newSQLiteMigrationStore(t *testing.T) *Store {
	t.Helper()
	backend, err := Open(context.Background(), filepath.Join(t.TempDir(), "migration.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	return backend
}

func sqliteMigrationManifest(t *testing.T, summary bool) schema.Manifest {
	t.Helper()
	fields := field.Fields{field.Text("title")}
	if summary {
		fields = append(fields, field.Text("summary").Index().Unique())
	}
	manifest, err := ridu.Resolve(ridu.Config{
		Name:        "SQLite migrations",
		Collections: []ridu.Collection{{Slug: "posts", Fields: fields}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
