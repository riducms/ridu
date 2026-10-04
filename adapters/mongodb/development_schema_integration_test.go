package mongodb

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func mongoDevelopmentSchemaTestManifest(t *testing.T, nodes ...field.Node) schema.Manifest {
	t.Helper()
	manifest, err := core.Resolve(core.Config{Name: "Schema provenance", Collections: []core.Collection{{Slug: "posts", Fields: nodes}}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestDecodeMongoDevelopmentManifestPreservesHistoricalBlockNameAbsence(t *testing.T) {
	current := mongoDevelopmentSchemaTestManifest(t, field.Blocks("layout", field.Block{
		Slug: "card", Fields: field.Fields{field.Text("title")},
	}))
	snapshot := current.Snapshot()
	block := &snapshot.Collections[0].Fields[0].Blocks.Types[0]
	if len(block.Fields) != 2 || block.Fields[1].Name != "blockName" {
		t.Fatalf("current block fields = %#v", block.Fields)
	}
	block.Fields = block.Fields[:1]
	historical := schema.NewManifest(snapshot)
	encoded, err := historical.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), "blockName") {
		t.Fatalf("historical fixture unexpectedly passes current validation: %v", err)
	}
	digest, err := ridumigration.DigestManifest(historical)
	if err != nil {
		t.Fatal(err)
	}
	stored := func(recordedDigest string) bson.Raw {
		t.Helper()
		raw, err := bson.Marshal(bson.D{
			{Key: "_id", Value: mongoDevelopmentSchemaID},
			{Key: "manifestJSON", Value: string(encoded)},
			{Key: "manifestDigest", Value: recordedDigest},
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	decoded, err := decodeMongoDevelopmentManifest(stored(digest))
	if err != nil || !decoded.Equal(historical) {
		t.Fatalf("decode historical canonical manifest: %v, equal=%t", err, decoded.Equal(historical))
	}
	fields := decoded.Snapshot().Collections[0].Fields[0].Blocks.ResolvedTypes()[0].ResolvedFields()
	if len(fields) != 1 || fields[0].Name != "title" {
		t.Fatalf("historical block children were changed: %#v", fields)
	}
	if _, err := decodeMongoDevelopmentManifest(stored(strings.Repeat("f", 64))); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("corrupt historical digest was accepted: %v", err)
	}
}

func TestMongoDBDevelopmentSchemaRecordPublishesOnlySuccessfulSync(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	before := mongoDevelopmentSchemaTestManifest(t, field.Text("title"))
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
	candidate := mongoDevelopmentSchemaTestManifest(t, field.Text("title").Unique(), field.Text("summary"))
	if err := backend.SyncDevelopmentSchema(ctx, candidate); err == nil {
		t.Fatal("unique build succeeded over duplicate retained values")
	}
	recorded, exists, err := backend.DevelopmentManifest(ctx)
	if err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("failed sync certified candidate: %t, %v", exists, err)
	}
	// Successfully created indexes are additive; a safe retry can still publish.
	after := mongoDevelopmentSchemaTestManifest(t, field.Text("title"), field.Text("summary"))
	if err := backend.SyncDevelopmentSchema(ctx, after); err != nil {
		t.Fatal(err)
	}
	recorded, exists, err = backend.DevelopmentManifest(ctx)
	if err != nil || !exists || !recorded.Equal(after) {
		t.Fatalf("index-equivalent sync did not publish manifest: %t, %v", exists, err)
	}
	detached := recorded.Snapshot()
	detached.Collections[0].Fields[0].Name = "mutated"
	again, _, err := backend.DevelopmentManifest(ctx)
	if err != nil || !again.Equal(after) {
		t.Fatalf("record accessor exposed mutable schema: %v", err)
	}
	if _, err := backend.mongoMigrationCollection(mongoDevelopmentSchemaCollection).UpdateOne(ctx,
		bson.D{{Key: "_id", Value: mongoDevelopmentSchemaID}}, bson.D{{Key: "$set", Value: bson.D{{Key: "manifestDigest", Value: strings.Repeat("f", 64)}}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.DevelopmentManifest(ctx); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("corrupt schema record was trusted: %v", err)
	}
}

func TestMongoDBDevelopmentUniqueAdmissionChecksPublishedOnlyDuplicates(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	resolve := func(unique bool) schema.Manifest {
		t.Helper()
		slug := field.Text("slug")
		if unique {
			slug = slug.Unique()
		}
		manifest, err := core.Resolve(core.Config{Name: "Unique active heads", Collections: []core.Collection{{
			Slug: "posts", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{slug},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before, candidate := resolve(false), resolve(true)
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	collection := before.Snapshot().Collections[0]
	write := mongoBegin(t, backend, false)
	for _, id := range []string{"one", "two"} {
		created, err := write.Create(ctx, store.CreateRequest{
			Collection: collection, ID: id, Status: store.StatusPublished,
			Values: store.Values{"slug": store.String("shared-live")},
		})
		if err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
		if _, err := write.Update(ctx, store.UpdateRequest{
			Request: store.Request{Collection: collection, ID: id, ExpectedRevision: created.Revision},
			Intent:  store.WriteIntentSaveDraft, Values: store.Values{"slug": store.String("draft-" + id)},
		}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	mongoCommit(t, write)
	if err := backend.SyncDevelopmentSchema(ctx, candidate); err == nil {
		t.Fatal("unique declaration was admitted despite duplicate published-only values")
	}
	recorded, exists, err := backend.DevelopmentManifest(ctx)
	if err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("failed admission certified candidate: exists=%t, error=%v", exists, err)
	}
}

func TestMongoDBDevelopmentSchemaUnknownCannotBeCertifiedByPhysicalEquality(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	before := mongoDevelopmentSchemaTestManifest(t, field.JSON("body"))
	after := mongoDevelopmentSchemaTestManifest(t, field.Group("body", field.Fields{field.Text("note")}))
	if err := backend.SyncIndexes(ctx, before); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(ctx, after); err != nil {
		t.Fatalf("fixture does not demonstrate equal physical shapes: %v", err)
	}
	if _, _, err := backend.DevelopmentManifest(ctx); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("unrecorded schema was inferred: %v", err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, after); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("unknown schema sync accepted candidate: %v", err)
	}
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", after, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.AdoptArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("physical-only baseline blessed unknown JSON field kind: %v", err)
	}
	state, err := backend.readMongoMigrationLedgerState(ctx)
	if err != nil || len(state.artifacts) != 0 || len(state.steps) != 0 {
		t.Fatalf("refused adoption created history: %#v, %v", state, err)
	}
}

func TestMongoDBDevelopmentSchemaRecordRequiresMigrationFence(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	before := mongoDevelopmentSchemaTestManifest(t, field.Text("title"))
	after := mongoDevelopmentSchemaTestManifest(t, field.Text("title"), field.Text("summary"))
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	lease, err := backend.acquireMongoMigrationLease(ctx, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	otherFence, err := newMongoMigrationToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName).UpdateOne(ctx,
		bson.D{{Key: "_id", Value: mongoMigrationLeaseID}}, bson.D{{Key: "$set", Value: bson.D{{Key: "fence", Value: otherFence}}}}); err != nil {
		t.Fatal(err)
	}
	if err := lease.transaction(ctx, func(sessionContext context.Context) error {
		return backend.writeMongoDevelopmentManifest(sessionContext, after)
	}); err == nil {
		t.Fatal("lost lease published candidate manifest")
	}
	recorded, exists, err := backend.DevelopmentManifest(ctx)
	if err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("lost owner changed authoritative record: %t, %v", exists, err)
	}
}

func TestMongoDBDevelopmentSchemaRecordAllowsAdminOnlyChangesInHistory(t *testing.T) {
	ctx := t.Context()
	before := mongoDevelopmentSchemaTestManifest(t, field.Text("title"))
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
			backend := mongoIntegrationStore(t)
			directory := t.TempDir()
			if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
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
			after := mongoDevelopmentSchemaTestManifest(t, field.Text("title"), field.Text("summary").Index())
			if _, err := CreateArtifact(ctx, directory, "summary", after, time.Unix(2, 0), ArtifactOptions{}); err != nil {
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

func TestMongoDBDevelopmentSchemaPublicationRechecksCurrentAndSnapshotValues(t *testing.T) {
	for _, snapshotsOnly := range []bool{false, true} {
		name := "late current and snapshot values"
		if snapshotsOnly {
			name = "late snapshot-only value"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			backend := mongoIntegrationStore(t)
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
			if err := backend.VerifyIndexes(ctx, before); err != nil {
				t.Fatal(err)
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

// Every applied migration and successful synchronization records the schema.
// Without that record neither ridu dev nor ridu migrate up can know which
// schema produced the stored content, so both refuse to continue.
func TestMongoDBDevelopmentSchemaRecordRequiredAfterMigration(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	before := mongoDevelopmentSchemaTestManifest(t, field.Text("title"))
	indexed := mongoDevelopmentSchemaTestManifest(t, field.Text("title"), field.Text("summary").Index())
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if recorded, exists, err := backend.DevelopmentManifest(ctx); err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("completed artifact did not publish target: %t, %v", exists, err)
	}
	if err := backend.mongoMigrationCollection(mongoDevelopmentSchemaCollection).Drop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.DevelopmentManifest(ctx); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("applied history alone fabricated a potentially stale development baseline: %v", err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, before); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("development sync accepted a database without a schema record: %v", err)
	}
	if _, err := CreateArtifact(ctx, directory, "summary", indexed, time.Unix(2, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("migration of a database without a schema record = %v", err)
	}
	state, err := backend.readMongoMigrationLedgerState(ctx)
	if err != nil || len(state.artifacts) != 1 {
		t.Fatalf("refused migration changed history: %#v, %v", state, err)
	}
	if record, err := backend.readMongoDevelopmentManifest(ctx); err != nil || record != nil {
		t.Fatalf("refused migration wrote a schema record: %v", err)
	}
}
