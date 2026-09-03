package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresCompiledDataTransformExecutesValidatedInMemoryHistorySnapshot(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	directory := t.TempDir()
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	descriptor := ridumigration.DataTransformDescriptor{
		Name: "snapshot-transform", Checksum: ridumigration.DataTransformChecksum([]byte("snapshot-transform-v1")),
	}
	artifact, err := buildPostgresTransformTestArtifact(ctx, descriptor.Name, nil, manifest, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := migrationartifact.Create(directory, artifact.Name, artifact, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.RequireCurrentHistoryForPlanner(directory, manifest, "atlas")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(created.Path, created.Path+".changed-after-snapshot"); err != nil {
		t.Fatal(err)
	}
	upCalls := 0
	callback := func(context.Context, ridumigration.DataTransaction) error {
		upCalls++
		return nil
	}
	transform := ridumigration.DataTransform{DataTransformDescriptor: descriptor, Up: callback, Down: callback}
	registry, err := newPostgresDataTransformRegistry([]ridumigration.DataTransform{transform})
	if err != nil {
		t.Fatal(err)
	}
	request := ridumigration.ProjectRequest{
		Action: ridumigration.ProjectApply, DatabaseURL: backend.pool.Config().ConnConfig.ConnString(), Directory: directory,
		AllowInsecureDatabase: true, AllowMaintenance: true,
	}
	driver := &postgresProjectMigrationDriver{transforms: []ridumigration.DataTransform{transform}}
	if err := driver.runProjectMigrationFiles(ctx, request, files, registry); err != nil {
		t.Fatal(err)
	}
	if upCalls != 1 {
		t.Fatalf("in-memory history callback calls = %d", upCalls)
	}
}

func TestPostgresCompiledDataTransformApplyResumeInspectAndVerify(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	databaseURL := backend.pool.Config().ConnConfig.ConnString()
	directory := t.TempDir()
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))

	initial, err := BuildArtifact(ctx, "initial-transform-base", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	postgresTransformCreateDocument(t, ctx, backend, collection, "post-1", "original")

	descriptor := ridumigration.DataTransformDescriptor{
		Name: "normalize-titles", Checksum: ridumigration.DataTransformChecksum([]byte("normalize-titles-v1")),
	}
	transformArtifact, err := buildPostgresTransformTestArtifact(ctx, descriptor.Name, &manifest, manifest, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, transformArtifact.Name, transformArtifact, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}

	upCalls := 0
	downCalls := 0
	transform := ridumigration.DataTransform{
		DataTransformDescriptor: descriptor,
		Up: func(ctx context.Context, transaction ridumigration.DataTransaction) error {
			upCalls++
			request := store.Request{
				Collection: collection, Collections: map[schema.StableID]schema.Collection{collection.ID: collection},
				ID: "post-1", Page: 1, Limit: 10,
			}
			_, err := transaction.Find(ctx, request)
			if err != nil {
				if err == store.ErrNotFound {
					return nil
				}
				return err
			}
			return updatePostgresTransformTitle(ctx, transaction, request, "migrated")
		},
		Down: func(context.Context, ridumigration.DataTransaction) error {
			downCalls++
			return nil
		},
	}
	driver := ProjectMigrations(transform)
	request := ridumigration.ProjectRequest{
		Action: ridumigration.ProjectApply, DatabaseURL: databaseURL, Directory: directory,
		AllowInsecureDatabase: true, AllowMaintenance: true,
		StopAfterPhase: transformArtifact.Phases[len(transformArtifact.Phases)-1].ID,
	}

	if _, err := backend.ArtifactStatus(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := driver.RunProjectMigration(ctx, request, manifest); err != nil {
		t.Fatal(err)
	}
	if upCalls != 1 || downCalls != 0 {
		t.Fatalf("stopped callback calls up=%d down=%d", upCalls, downCalls)
	}
	if got := postgresTransformDocumentTitle(t, ctx, backend, collection, "post-1"); got != "migrated" {
		t.Fatalf("title after stopped phase = %q", got)
	}
	var applied int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migrations`).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("stopped migration ledger rows = %d, %v", applied, err)
	}

	request.StopAfterPhase = ""
	if err := driver.RunProjectMigration(ctx, request, manifest); err != nil {
		t.Fatal(err)
	}
	if upCalls != 1 || downCalls != 0 {
		t.Fatalf("resume reran callback: up=%d down=%d", upCalls, downCalls)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migrations`).Scan(&applied); err != nil || applied != 2 {
		t.Fatalf("completed migration ledger rows = %d, %v", applied, err)
	}

	verifyRequest := request
	verifyRequest.Action = ridumigration.ProjectVerify
	for name, candidate := range map[string]ridumigration.ProjectDriver{
		"missing": ProjectMigrations(),
		"checksum changed": ProjectMigrations(ridumigration.DataTransform{
			DataTransformDescriptor: ridumigration.DataTransformDescriptor{Name: descriptor.Name, Checksum: ridumigration.DataTransformChecksum([]byte("normalize-titles-v2"))},
			Up:                      transform.Up, Down: transform.Down,
		}),
	} {
		t.Run("verify rejects "+name, func(t *testing.T) {
			if err := candidate.RunProjectMigration(ctx, verifyRequest, manifest); err == nil {
				t.Fatalf("verification accepted %s registration", name)
			}
		})
	}
	beforeVerify := postgresTransformDocumentTitle(t, ctx, backend, collection, "post-1")
	if err := driver.RunProjectMigration(ctx, verifyRequest, manifest); err != nil {
		t.Fatal(err)
	}
	if upCalls != 2 || downCalls != 0 {
		t.Fatalf("verification callback calls up=%d down=%d", upCalls, downCalls)
	}
	if got := postgresTransformDocumentTitle(t, ctx, backend, collection, "post-1"); got != beforeVerify {
		t.Fatalf("verification modified application title: before=%q after=%q", beforeVerify, got)
	}
}

func TestPostgresCompiledDataTransformVerifyIsolatesSchemaDataAndLedger(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	databaseURL := backend.pool.Config().ConnConfig.ConnString()
	directory := t.TempDir()
	before := atlasTestManifest(atlasTextField("posts-title", "title"))
	initial, err := BuildArtifact(ctx, "initial-transform-verify-isolation", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	beforeCollection := before.Snapshot().Collections[0]
	postgresTransformCreateDocument(t, ctx, backend, beforeCollection, "post-1", "live")

	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Fields = append(afterSnapshot.Collections[0].Fields, atlasTextField("posts-summary", "summary"))
	after := schema.NewManifest(afterSnapshot)
	afterCollection := after.Snapshot().Collections[0]
	descriptor := ridumigration.DataTransformDescriptor{
		Name: "verify-isolation", Checksum: ridumigration.DataTransformChecksum([]byte("verify-isolation-v1")),
	}
	artifact, err := buildPostgresTransformTestArtifact(ctx, descriptor.Name, &before, after, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := migrationartifact.Create(directory, artifact.Name, artifact, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}

	callbackCalls := 0
	callback := func(ctx context.Context, transaction ridumigration.DataTransaction) error {
		callbackCalls++
		_, err := transaction.Find(ctx, store.Request{
			Collection: beforeCollection,
			Collections: map[schema.StableID]schema.Collection{
				beforeCollection.ID: beforeCollection,
			},
			ID: "post-1",
		})
		switch {
		case err == nil:
			return fmt.Errorf("verification callback observed live application document")
		case !errors.Is(err, store.ErrNotFound):
			return fmt.Errorf("probe replayed shadow data: %w", err)
		}
		_, err = transaction.Create(ctx, store.CreateRequest{
			Collection: afterCollection, ID: "verify-only",
			Values: store.Values{"title": store.String("shadow"), "summary": store.String("shadow-only")},
		})
		return err
	}
	transform := ridumigration.DataTransform{DataTransformDescriptor: descriptor, Up: callback, Down: callback}
	request := ridumigration.ProjectRequest{
		Action: ridumigration.ProjectVerify, DatabaseURL: databaseURL, Directory: directory,
		AllowInsecureDatabase: true, AllowMaintenance: true,
	}
	if err := ProjectMigrations(transform).RunProjectMigration(ctx, request, after); err != nil {
		t.Fatal(err)
	}
	if callbackCalls != 1 {
		t.Fatalf("verification callback calls = %d", callbackCalls)
	}

	if got := postgresTransformDocumentTitle(t, ctx, backend, beforeCollection, "post-1"); got != "live" {
		t.Fatalf("verification modified live title %q", got)
	}
	var summaryColumn, verifyOnlyDocument bool
	if err := backend.pool.QueryRow(ctx, `SELECT
EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
), EXISTS (
  SELECT 1 FROM `+quote(collectionTable(beforeCollection.ID))+` WHERE id = $3
)`, collectionTable(beforeCollection.ID), fieldColumn("posts-summary"), "verify-only").Scan(&summaryColumn, &verifyOnlyDocument); err != nil {
		t.Fatal(err)
	}
	if summaryColumn || verifyOnlyDocument {
		t.Fatalf("verification changed live schema/data: summary column=%t verify-only document=%t", summaryColumn, verifyOnlyDocument)
	}
	var migrations, steps int
	if err := backend.pool.QueryRow(ctx, `SELECT
(SELECT count(*) FROM ridu_migrations),
(SELECT count(*) FROM ridu_migration_steps WHERE artifact_name = $1)`, created.Name).Scan(&migrations, &steps); err != nil {
		t.Fatal(err)
	}
	if migrations != 1 || steps != 0 {
		t.Fatalf("verification changed live ledgers: migrations=%d pending artifact steps=%d", migrations, steps)
	}
}

func TestPostgresCompiledDataTransformRuntimeRejectsVersionedResourceMutation(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	databaseURL := backend.pool.Config().ConnConfig.ConnString()
	directory := t.TempDir()
	manifestSnapshot := atlasTestManifest(atlasTextField("posts-title", "title")).Snapshot()
	manifestSnapshot.Collections[0].Capabilities.Versions = true
	manifestSnapshot.Collections[0].Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	manifest := schema.NewManifest(manifestSnapshot)
	initial, err := BuildArtifact(ctx, "initial-versioned-transform-runtime", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	document, err := transaction.Create(ctx, store.CreateRequest{
		Collection: collection, ID: "post-1", Status: store.StatusPublished,
		Values: store.Values{"title": store.String("original")},
	})
	if err != nil {
		_ = transaction.Rollback(context.Background())
		t.Fatal(err)
	}
	if _, err := transaction.(store.VersionTransaction).SaveVersion(ctx, collection, document, 10); err != nil {
		_ = transaction.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	descriptor := ridumigration.DataTransformDescriptor{
		Name: "reject-versioned-runtime", Checksum: ridumigration.DataTransformChecksum([]byte("reject-versioned-runtime-v1")),
	}
	artifact, err := buildPostgresTransformTestArtifact(ctx, descriptor.Name, &manifest, manifest, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := migrationartifact.Create(directory, artifact.Name, artifact, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	callback := func(ctx context.Context, transaction ridumigration.DataTransaction) error {
		request := store.Request{
			Collection: collection, Collections: map[schema.StableID]schema.Collection{collection.ID: collection}, ID: document.ID,
		}
		_, err := transaction.Find(ctx, request)
		if err != nil {
			return err
		}
		return updatePostgresTransformTitle(ctx, transaction, request, "changed")
	}
	transform := ridumigration.DataTransform{DataTransformDescriptor: descriptor, Up: callback, Down: callback}
	request := ridumigration.ProjectRequest{
		Action: ridumigration.ProjectApply, DatabaseURL: databaseURL, Directory: directory,
		AllowInsecureDatabase: true, AllowMaintenance: true,
	}
	if err := ProjectMigrations(transform).RunProjectMigration(ctx, request, manifest); err == nil || !strings.Contains(err.Error(), "cannot mutate versioned resource") {
		t.Fatalf("versioned runtime mutation error = %v", err)
	}
	if got := postgresTransformDocumentTitle(t, ctx, backend, collection, document.ID); got != "original" {
		t.Fatalf("rejected versioned mutation retained title %q", got)
	}

	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	versions, err := transaction.(store.VersionTransaction).ListVersions(ctx, store.VersionRequest{
		Collection: collection, DocumentID: document.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Revision != document.Revision {
		t.Fatalf("versions after rejected transform = %#v", versions)
	}
	if title, _ := versions[0].Snapshot.Values["title"].StringValue(); title != "original" {
		t.Fatalf("rejected transform changed retained version title %q", title)
	}
	var ledgers, steps int
	if err := backend.pool.QueryRow(ctx, `SELECT
(SELECT count(*) FROM ridu_migrations WHERE name = $1),
(SELECT count(*) FROM ridu_migration_steps WHERE artifact_name = $1)`, created.Name).Scan(&ledgers, &steps); err != nil {
		t.Fatal(err)
	}
	if ledgers != 0 || steps != 0 {
		t.Fatalf("rejected versioned transform retained ledger=%d step=%d rows", ledgers, steps)
	}
}

func TestPostgresCompiledDataTransformFailureInvalidatesEscapedTransactionAndRollsBackSchemaDataStepsAndLedger(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	databaseURL := backend.pool.Config().ConnConfig.ConnString()
	directory := t.TempDir()
	before := atlasTestManifest(atlasTextField("posts-title", "title"))
	initial, err := BuildArtifact(ctx, "initial-transform-rollback", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	beforeCollection := before.Snapshot().Collections[0]
	postgresTransformCreateDocument(t, ctx, backend, beforeCollection, "post-1", "original")

	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Fields = append(afterSnapshot.Collections[0].Fields, atlasTextField("posts-summary", "summary"))
	after := schema.NewManifest(afterSnapshot)
	afterCollection := after.Snapshot().Collections[0]
	descriptor := ridumigration.DataTransformDescriptor{
		Name: "backfill-summary", Checksum: ridumigration.DataTransformChecksum([]byte("backfill-summary-v1")),
	}
	artifact, err := buildPostgresTransformTestArtifact(ctx, descriptor.Name, &before, after, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := migrationartifact.Create(directory, artifact.Name, artifact, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}

	var escaped ridumigration.DataTransaction
	callback := func(ctx context.Context, transaction ridumigration.DataTransaction) error {
		escaped = transaction
		request := store.Request{
			Collection: afterCollection, Collections: map[schema.StableID]schema.Collection{afterCollection.ID: afterCollection},
			ID: "post-1", Page: 1, Limit: 10,
		}
		document, err := transaction.Find(ctx, request)
		if err != nil {
			return err
		}
		if _, err := transaction.Update(ctx, store.UpdateRequest{
			Request: request, Values: store.Values{"title": store.String("changed"), "summary": store.String("filled")},
		}); err != nil {
			return err
		}
		if title, _ := document.Values["title"].StringValue(); title != "original" {
			return fmt.Errorf("unexpected source title %q", title)
		}
		forged := afterCollection
		forged.Fields = append([]schema.Field(nil), forged.Fields...)
		forged.Fields[0].ID = "forged-title"
		_, err = transaction.List(ctx, store.Request{
			Collection: forged, Collections: map[schema.StableID]schema.Collection{forged.ID: forged}, Page: 1, Limit: 10,
		})
		return errors.Join(err, driver.ErrBadConn)
	}
	transform := ridumigration.DataTransform{DataTransformDescriptor: descriptor, Up: callback, Down: callback}
	request := ridumigration.ProjectRequest{
		Action: ridumigration.ProjectApply, DatabaseURL: databaseURL, Directory: directory,
		AllowInsecureDatabase: true, AllowMaintenance: true,
	}
	err = ProjectMigrations(transform).RunProjectMigration(ctx, request, after)
	if err == nil || !strings.Contains(err.Error(), "exactly match") || !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("forged resource error = %v", err)
	}
	escapedUse := make(chan error, 1)
	go func() {
		_, escapedErr := escaped.Update(context.Background(), store.UpdateRequest{
			Request: store.Request{
				Collection: afterCollection, Collections: map[schema.StableID]schema.Collection{afterCollection.ID: afterCollection},
				ID: "post-1", Page: 1, Limit: 10,
			},
			Values: store.Values{"title": store.String("escaped"), "summary": store.String("escaped")},
		})
		escapedUse <- escapedErr
	}()
	select {
	case escapedErr := <-escapedUse:
		if !errors.Is(escapedErr, errPostgresMigrationDataTransactionClosed) {
			t.Fatalf("escaped data transaction error = %v", escapedErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("escaped data transaction remained usable after its callback")
	}
	if got := postgresTransformDocumentTitle(t, ctx, backend, beforeCollection, "post-1"); got != "original" {
		t.Fatalf("failed callback retained title %q", got)
	}
	var columnExists bool
	if err := backend.pool.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
)`, collectionTable(beforeCollection.ID), fieldColumn("posts-summary")).Scan(&columnExists); err != nil || columnExists {
		t.Fatalf("failed callback retained summary column=%t, %v", columnExists, err)
	}
	var steps, ledgers int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migration_steps WHERE artifact_name = $1`, created.Name).Scan(&steps); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migrations WHERE name = $1`, created.Name).Scan(&ledgers); err != nil {
		t.Fatal(err)
	}
	if steps != 0 || ledgers != 0 {
		t.Fatalf("failed callback retained step=%d ledger=%d rows", steps, ledgers)
	}
}

func TestPostgresCompiledDataTransformCancellationRollsBackSchemaDataStepsAndLedger(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	directory := t.TempDir()
	before := atlasTestManifest(atlasTextField("posts-title", "title"))
	initial, err := BuildArtifact(ctx, "initial-transform-cancellation", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	beforeCollection := before.Snapshot().Collections[0]
	postgresTransformCreateDocument(t, ctx, backend, beforeCollection, "post-1", "original")

	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Fields = append(afterSnapshot.Collections[0].Fields, atlasTextField("posts-summary", "summary"))
	after := schema.NewManifest(afterSnapshot)
	afterCollection := after.Snapshot().Collections[0]
	descriptor := ridumigration.DataTransformDescriptor{
		Name: "cancel-backfill-summary", Checksum: ridumigration.DataTransformChecksum([]byte("cancel-backfill-summary-v1")),
	}
	artifact, err := buildPostgresTransformTestArtifact(ctx, descriptor.Name, &before, after, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := migrationartifact.Create(directory, artifact.Name, artifact, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}

	written := make(chan struct{})
	transform := ridumigration.DataTransform{
		DataTransformDescriptor: descriptor,
		Up: func(callbackContext context.Context, transaction ridumigration.DataTransaction) error {
			request := store.Request{
				Collection: afterCollection, Collections: map[schema.StableID]schema.Collection{afterCollection.ID: afterCollection},
				ID: "post-1", Page: 1, Limit: 10,
			}
			if _, err := transaction.Update(callbackContext, store.UpdateRequest{
				Request: request, Values: store.Values{"title": store.String("cancelled"), "summary": store.String("cancelled")},
			}); err != nil {
				return err
			}
			close(written)
			<-callbackContext.Done()
			return callbackContext.Err()
		},
		Down: func(context.Context, ridumigration.DataTransaction) error { return nil },
	}
	request := ridumigration.ProjectRequest{
		Action: ridumigration.ProjectApply, DatabaseURL: backend.pool.Config().ConnConfig.ConnString(), Directory: directory,
		AllowInsecureDatabase: true, AllowMaintenance: true,
	}
	migrationContext, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		result <- ProjectMigrations(transform).RunProjectMigration(migrationContext, request, after)
	}()
	select {
	case <-written:
	case runErr := <-result:
		cancel()
		t.Fatalf("migration returned before cancellation: %v", runErr)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("compiled transform did not reach its cancellation boundary")
	}
	cancel()
	select {
	case runErr := <-result:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("cancelled migration error = %v", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled compiled transform did not roll back")
	}

	if got := postgresTransformDocumentTitle(t, ctx, backend, beforeCollection, "post-1"); got != "original" {
		t.Fatalf("cancelled callback retained title %q", got)
	}
	var columnExists bool
	if err := backend.pool.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
)`, collectionTable(beforeCollection.ID), fieldColumn("posts-summary")).Scan(&columnExists); err != nil || columnExists {
		t.Fatalf("cancelled callback retained summary column=%t, %v", columnExists, err)
	}
	var steps, ledgers int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migration_steps WHERE artifact_name = $1`, created.Name).Scan(&steps); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migrations WHERE name = $1`, created.Name).Scan(&ledgers); err != nil {
		t.Fatal(err)
	}
	if steps != 0 || ledgers != 0 {
		t.Fatalf("cancelled callback retained step=%d ledger=%d rows", steps, ledgers)
	}
}

func TestPostgresCompiledDataTransformCancellationCannotRollbackDuringCallback(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	directory := t.TempDir()
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	artifact, err := BuildArtifact(ctx, "initial-transform-cancellation-exclusion", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, artifact.Name, artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	postgresTransformCreateDocument(t, ctx, backend, collection, "post-1", "original")

	database := stdlib.OpenDB(*backend.pool.Config().ConnConfig)
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	transactionContext, cancelTransaction := context.WithCancel(ctx)
	callbackContext, cancelCallback := context.WithCancel(ctx)
	sqlTransaction, err := connection.BeginTx(transactionContext, nil)
	if err != nil {
		cancelCallback()
		cancelTransaction()
		t.Fatal(err)
	}
	defer func() {
		cancelCallback()
		cancelTransaction()
		_ = sqlTransaction.Rollback()
	}()

	callbackEntered := make(chan struct{})
	descriptor := ridumigration.DataTransformDescriptor{
		Name: "cancellation-exclusion", Checksum: ridumigration.DataTransformChecksum([]byte("cancellation-exclusion-v1")),
	}
	transform := ridumigration.DataTransform{
		DataTransformDescriptor: descriptor,
		Up: func(callbackContext context.Context, transaction ridumigration.DataTransaction) error {
			close(callbackEntered)
			<-callbackContext.Done()

			operationContext, cancelOperation := context.WithTimeout(context.WithoutCancel(callbackContext), 5*time.Second)
			defer cancelOperation()
			document, err := transaction.Find(operationContext, store.Request{
				Collection: collection, Collections: map[schema.StableID]schema.Collection{collection.ID: collection},
				ID: "post-1", Page: 1, Limit: 10,
			})
			if err != nil {
				return fmt.Errorf("read through protected transaction after cancellation: %w", err)
			}
			if title, _ := document.Values["title"].StringValue(); title != "original" {
				return fmt.Errorf("protected transaction title = %q", title)
			}
			return callbackContext.Err()
		},
		Down: func(context.Context, ridumigration.DataTransaction) error { return nil },
	}
	transformResult := make(chan error, 1)
	go func() {
		transformResult <- executePostgresDataTransform(callbackContext, connection, artifact, transform)
	}()
	select {
	case <-callbackEntered:
	case err := <-transformResult:
		t.Fatalf("compiled transform returned before entering callback: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("compiled transform did not enter callback")
	}

	// BeginTx cancellation claims the SQL transaction before database/sql
	// waits for Raw's driver-connection lock. Commit reports the transaction
	// context error until that claim occurs, then reports ErrTxDone without
	// waiting for the still-blocked physical rollback.
	cancelTransaction()
	rollbackDeadline := time.Now().Add(5 * time.Second)
	for {
		commitErr := sqlTransaction.Commit()
		if errors.Is(commitErr, sql.ErrTxDone) {
			break
		}
		if !errors.Is(commitErr, context.Canceled) {
			t.Fatalf("observe cancellation rollback claim: %v", commitErr)
		}
		if time.Now().After(rollbackDeadline) {
			t.Fatal("database/sql did not claim the cancellation rollback")
		}
		runtime.Gosched()
	}
	cancelCallback()
	select {
	case err := <-transformResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled protected transform error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled protected transform did not return")
	}
}

func TestPostgresCompiledDataTransformRefusesCallbackAfterCancellationRollback(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	directory := t.TempDir()
	initial, err := BuildArtifact(ctx, "initial-transform-pre-raw-cancellation", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	postgresTransformCreateDocument(t, ctx, backend, collection, "post-1", "original")

	database := stdlib.OpenDB(*backend.pool.Config().ConnConfig)
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	migrationContext, cancel := context.WithCancel(ctx)
	transaction, err := connection.BeginTx(migrationContext, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	pendingName := "cancelled-before-transform-raw"
	if _, err := transaction.ExecContext(ctx, "ALTER TABLE "+quote(collectionTable(collection.ID))+" ADD COLUMN "+quote(fieldColumn("posts-summary"))+" text"); err != nil {
		cancel()
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE "+quote(collectionTable(collection.ID))+" SET "+quote(fieldColumn("posts-title"))+" = $1 WHERE id = $2", "staged", "post-1"); err != nil {
		cancel()
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_migration_steps
(artifact_name, artifact_digest, phase_id, step_id, phase_mode, state, checkpoint, attempts, completed_at)
VALUES ($1, $2, 'phase-001', 'step-0001', 'transaction', 'complete', '{}'::jsonb, 1, now())`, pendingName, strings.Repeat("c", 64)); err != nil {
		cancel()
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_migrations
(name, artifact_digest, from_digest, to_digest, planner_name, planner_version)
VALUES ($1, $2, $3, $4, 'atlas', '1.0.0')`, pendingName, strings.Repeat("c", 64), strings.Repeat("a", 64), strings.Repeat("b", 64)); err != nil {
		cancel()
		_ = transaction.Rollback()
		t.Fatal(err)
	}

	cancel()
	_ = transaction.Rollback()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var status byte
		var closed bool
		if err := connection.Raw(func(driverConnection any) error {
			postgresConnection, ok := driverConnection.(*stdlib.Conn)
			if !ok {
				return fmt.Errorf("unexpected PostgreSQL driver %T", driverConnection)
			}
			native := postgresConnection.Conn()
			closed = native.IsClosed()
			status = native.PgConn().TxStatus()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if closed || status == 'I' {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancelled SQL transaction remained open in status %q", status)
		}
		time.Sleep(time.Millisecond)
	}

	descriptor := ridumigration.DataTransformDescriptor{
		Name: "must-not-escape-cancelled-transaction", Checksum: ridumigration.DataTransformChecksum([]byte("must-not-escape-cancelled-transaction-v1")),
	}
	artifact, err := buildPostgresTransformTestArtifact(ctx, descriptor.Name, &manifest, manifest, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	callbackCalls := 0
	callback := func(context.Context, ridumigration.DataTransaction) error {
		callbackCalls++
		return nil
	}
	transform := ridumigration.DataTransform{DataTransformDescriptor: descriptor, Up: callback, Down: callback}
	if err := executePostgresDataTransform(context.Background(), connection, artifact, transform); err == nil || !strings.Contains(err.Error(), "no longer active") {
		t.Fatalf("post-cancellation callback error = %v", err)
	}
	if callbackCalls != 0 {
		t.Fatalf("post-cancellation callback calls = %d", callbackCalls)
	}
	if got := postgresTransformDocumentTitle(t, ctx, backend, collection, "post-1"); got != "original" {
		t.Fatalf("post-cancellation callback retained title %q", got)
	}
	var summaryColumn bool
	if err := backend.pool.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
)`, collectionTable(collection.ID), fieldColumn("posts-summary")).Scan(&summaryColumn); err != nil || summaryColumn {
		t.Fatalf("post-cancellation callback retained summary column=%t, %v", summaryColumn, err)
	}
	var steps, ledgers int
	if err := backend.pool.QueryRow(ctx, `SELECT
(SELECT count(*) FROM ridu_migration_steps WHERE artifact_name = $1),
(SELECT count(*) FROM ridu_migrations WHERE name = $1)`, pendingName).Scan(&steps, &ledgers); err != nil {
		t.Fatal(err)
	}
	if steps != 0 || ledgers != 0 {
		t.Fatalf("post-cancellation callback retained step=%d ledger=%d rows", steps, ledgers)
	}
}

func postgresTransformCreateDocument(t *testing.T, ctx context.Context, backend *Store, collection schema.Collection, id, title string) {
	t.Helper()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	now := time.Now().UTC()
	if _, err := transaction.Create(ctx, store.CreateRequest{
		Collection: collection, ID: id, Values: store.Values{"title": store.String(title)},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func postgresTransformDocumentTitle(t *testing.T, ctx context.Context, backend *Store, collection schema.Collection, id string) string {
	t.Helper()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	document, err := transaction.Find(ctx, store.Request{
		Collection: collection, Collections: map[schema.StableID]schema.Collection{collection.ID: collection}, ID: id,
	})
	if err != nil {
		t.Fatal(err)
	}
	title, _ := document.Values["title"].StringValue()
	return title
}

func updatePostgresTransformTitle(ctx context.Context, transaction ridumigration.DataTransaction, request store.Request, title string) error {
	_, err := transaction.Update(ctx, store.UpdateRequest{
		Request: request, Values: store.Values{"title": store.String(title)},
	})
	return err
}
