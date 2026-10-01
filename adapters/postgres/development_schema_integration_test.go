package postgres

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresDevelopmentSchemaRecordPublishesOnlySuccessfulSync(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	before := atlasTestManifest(atlasTextField("posts-title", "title"))
	if _, exists, err := backend.DevelopmentManifest(ctx); err != nil || exists {
		t.Fatalf("fresh database baseline = %t, %v", exists, err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if _, err := write.Create(ctx, store.CreateRequest{Collection: before.Snapshot().Collections[0], ID: id, Values: store.Values{"title": store.String("duplicate")}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	unique := atlasTextField("posts-title", "title")
	unique.Unique = true
	candidate := atlasTestManifest(unique, atlasTextField("posts-summary", "summary"))
	if err := backend.SyncDevelopmentSchema(ctx, candidate); err == nil {
		t.Fatal("unique build succeeded over duplicate retained values")
	}
	recorded, exists, err := backend.DevelopmentManifest(ctx)
	if err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("failed sync certified candidate: %t, %v, %#v", exists, err, recorded.Snapshot())
	}
	if plan, err := backend.Plan(ctx, before); err != nil || len(plan) != 0 {
		t.Fatalf("failed sync changed physical schema: %#v, %v", plan, err)
	}
	// Schema-only changes also publish, even when Atlas has no SQL to run.
	cosmetic := before.Snapshot()
	cosmetic.Application.Name = "Accepted presentation change"
	after := schema.NewManifest(cosmetic)
	if err := backend.SyncDevelopmentSchema(ctx, after); err != nil {
		t.Fatal(err)
	}
	recorded, exists, err = backend.DevelopmentManifest(ctx)
	if err != nil || !exists || !recorded.Equal(after) {
		t.Fatalf("no-op physical sync did not publish manifest: %t, %v", exists, err)
	}
	detached := recorded.Snapshot()
	detached.Collections[0].Fields[0].Name = "mutated"
	again, _, err := backend.DevelopmentManifest(ctx)
	if err != nil || !again.Equal(after) {
		t.Fatalf("record accessor exposed mutable schema: %v", err)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE ridu_postgres_schema SET manifest_digest = $1`, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.DevelopmentManifest(ctx); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("corrupt schema record was trusted: %v", err)
	}
}

func TestPostgresDevelopmentSchemaUnknownCannotBeCertifiedByPhysicalEquality(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	resolve := func(body field.Node) schema.Manifest {
		t.Helper()
		manifest, err := core.Resolve(core.Config{Name: "Schema provenance", Collections: []core.Collection{{Slug: "posts", Fields: field.Fields{body}}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before := resolve(field.JSON("body"))
	after := resolve(field.Group("body", field.Fields{field.Text("note")}))
	plan, err := backend.Plan(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if plan, err := backend.Plan(ctx, after); err != nil || len(plan) != 0 {
		t.Fatalf("fixture does not demonstrate equal physical shapes: %#v, %v", plan, err)
	}
	if _, _, err := backend.DevelopmentManifest(ctx); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("unrecorded schema was inferred: %v", err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, after); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("unknown schema sync accepted candidate: %v", err)
	}
	directory := t.TempDir()
	artifact, err := BuildArtifact(ctx, "initial", nil, after, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.AdoptArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("physical-only baseline blessed unknown JSON field kind: %v", err)
	}
	var ledger bool
	if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.ridu_migrations') IS NOT NULL`).Scan(&ledger); err != nil || ledger {
		t.Fatalf("refused adoption created history: %t, %v", ledger, err)
	}
}

// Every applied migration and successful synchronization records the schema.
// Without that record neither ridu dev nor ridu migrate up can know which
// schema produced the stored content, so both refuse to continue.
func TestPostgresDevelopmentSchemaRecordFollowsCompleteMigrationBoundaries(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	directory, file := createInitialArtifact(t, "schema-record")
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{StopAfterPhase: file.Artifact.Name + "/" + file.Artifact.Phases[0].ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.DevelopmentManifest(ctx); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("partial migration certified target: %v", err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	before := schema.NewManifest(file.Artifact.After)
	recorded, exists, err := backend.DevelopmentManifest(ctx)
	if err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("completed artifact did not publish target: %t, %v", exists, err)
	}
	if _, err := backend.pool.Exec(ctx, `DROP TABLE ridu_postgres_schema`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.DevelopmentManifest(ctx); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("applied history alone fabricated a potentially stale development baseline: %v", err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, before); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("development sync accepted a database without a schema record: %v", err)
	}
	after := atlasTestManifest(atlasTextField("posts-title", "title"), atlasTextField("posts-summary", "summary"))
	artifact, err := BuildArtifact(ctx, "summary", &before, after, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "summary", artifact, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("migration of a database without a schema record = %v", err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("refused migration changed history: %#v, %v", statuses, err)
	}
	var recordedTable bool
	if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.ridu_postgres_schema') IS NOT NULL`).Scan(&recordedTable); err != nil || recordedTable {
		t.Fatalf("refused migration wrote a schema record: %t, %v", recordedTable, err)
	}
}

func TestPostgresDevelopmentSchemaRecordAllowsAdminOnlyChangesInHistory(t *testing.T) {
	ctx := t.Context()
	before := atlasTestManifest(atlasTextField("posts-title", "title"))
	presentation := before.Snapshot()
	presentation.Collections[0].Admin.Hidden = true
	presentation.Collections[0].Fields[0].Admin.Label = "Updated title label"
	relabelled := schema.NewManifest(presentation)
	for _, managed := range []bool{false, true} {
		name := "development baseline adoption"
		if managed {
			name = "applied history and additive migration"
		}
		t.Run(name, func(t *testing.T) {
			backend := migrationArtifactTestBackend(t)
			directory := t.TempDir()
			artifact, err := BuildArtifact(ctx, "initial", nil, before, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
				t.Fatal(err)
			}
			if managed {
				if err := backend.ApplyArtifacts(ctx, directory); err != nil {
					t.Fatal(err)
				}
			} else if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
				t.Fatal(err)
			}
			if err := backend.SyncDevelopmentSchema(ctx, relabelled); err != nil {
				t.Fatal(err)
			}
			if _, err := backend.AdoptArtifacts(ctx, directory); err != nil {
				t.Fatalf("admin-only sync blocked baseline adoption: %v", err)
			}
			if !managed {
				return
			}
			afterSnapshot := relabelled.Snapshot()
			afterSnapshot.Collections[0].Fields = append(afterSnapshot.Collections[0].Fields, atlasTextField("posts-summary", "summary"))
			after := schema.NewManifest(afterSnapshot)
			// The artifact's source is the original committed manifest, whose
			// presentation differs from the accepted development record.
			additive, err := BuildArtifact(ctx, "summary", &before, after, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := migrationartifact.Create(directory, "summary", additive, time.Unix(2, 0)); err != nil {
				t.Fatal(err)
			}
			if err := backend.ApplyArtifacts(ctx, directory); err != nil {
				t.Fatalf("admin-only sync blocked additive migration: %v", err)
			}
			recorded, exists, err := backend.DevelopmentManifest(ctx)
			if err != nil || !exists || !recorded.Equal(after) {
				t.Fatalf("completed migration did not publish candidate: %t, %v", exists, err)
			}
		})
	}
}

func TestPostgresDevelopmentSchemaPublicationRechecksCurrentAndSnapshotValues(t *testing.T) {
	for _, snapshotsOnly := range []bool{false, true} {
		name := "late current and snapshot values"
		if snapshotsOnly {
			name = "late snapshot-only value"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			backend := migrationArtifactTestBackend(t)
			resolve := func(body field.Node) schema.Manifest {
				t.Helper()
				manifest, err := core.Resolve(core.Config{Name: "Publication recount", Collections: []core.Collection{{Slug: "posts", Versions: true, Fields: field.Fields{body}}}})
				if err != nil {
					t.Fatal(err)
				}
				return manifest
			}
			before := resolve(field.JSON("body"))
			after := resolve(field.Group("body", field.Fields{field.Text("note")}))
			if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
				t.Fatal(err)
			}
			initial, err := backend.ReviewDevelopmentFieldKinds(ctx, before, after)
			if err != nil || initial[0].Documents != 0 || initial[0].Snapshots != 0 {
				t.Fatalf("initial empty review: %#v, %v", initial, err)
			}
			resource := before.Snapshot().Collections[0]
			write, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			document, err := write.Create(ctx, store.CreateRequest{Collection: resource, ID: "late", Values: store.Values{"body": store.Object(store.Values{"legacy": store.String("retain")})}})
			if err != nil {
				t.Fatal(err)
			}
			version, err := write.(store.VersionTransaction).SaveVersion(ctx, resource, document, 0)
			if err != nil {
				t.Fatal(err)
			}
			if snapshotsOnly {
				document, err = write.Update(ctx, store.UpdateRequest{Request: store.Request{Collection: resource, ID: document.ID, ExpectedRevision: document.Revision}, Values: store.Values{"body": store.Null()}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := write.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := backend.SyncDevelopmentSchema(ctx, after); err == nil || !strings.Contains(err.Error(), "RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM") {
				t.Fatalf("publication ignored values written after review: %v", err)
			}
			recorded, exists, err := backend.DevelopmentManifest(ctx)
			if err != nil || !exists || !recorded.Equal(before) {
				t.Fatalf("refused publication changed schema record: %t, %v", exists, err)
			}
			read, err := backend.BeginSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := read.Find(ctx, store.Request{Collection: resource, ID: document.ID})
			if err != nil {
				t.Fatal(err)
			}
			versions, err := read.(store.VersionTransaction).ListVersions(ctx, store.VersionRequest{Collection: resource, DocumentID: document.ID})
			if err != nil || len(versions) != 1 || !reflect.DeepEqual(document, actual) || !reflect.DeepEqual(version, versions[0]) {
				t.Fatalf("refused publication changed retained values: %#v, %#v, %v", actual, versions, err)
			}
			if err := read.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			counts, err := backend.ReviewDevelopmentFieldKinds(ctx, before, after)
			if err != nil {
				t.Fatal(err)
			}
			if err := backend.ClearDevelopmentFieldKinds(ctx, before, after, counts); err != nil {
				t.Fatal(err)
			}
			if err := backend.SyncDevelopmentSchema(ctx, after); err != nil {
				t.Fatalf("explicitly cleared values still blocked publication: %v", err)
			}
		})
	}
}
