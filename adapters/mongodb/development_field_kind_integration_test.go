package mongodb

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestMongoDBFieldKindClearCountsLogicalHeadsAndCoversPublishedOnlyValues(t *testing.T) {
	backend := mongoIntegrationStore(t)
	resolve := func(body field.Node) schema.Manifest {
		t.Helper()
		manifest, err := core.Resolve(core.Config{Name: "Mongo active-head field recovery", Collections: []core.Collection{{
			Slug: "posts", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{body},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before, after := resolve(field.Text("body")), resolve(field.Number("body"))
	ctx := t.Context()
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	collection := before.Snapshot().Collections[0]
	write := mongoBegin(t, backend, false)
	for _, id := range []string{"both-heads", "live-only"} {
		created, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: id, Status: store.StatusPublished, Values: store.Values{"body": store.String("old")}})
		if err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
		if id == "both-heads" {
			if _, err := write.(store.VersionTransaction).SaveVersion(ctx, collection, created, 10); err != nil {
				mongoRollback(t, write)
				t.Fatal(err)
			}
			continue
		}
		if _, err := write.Update(ctx, store.UpdateRequest{
			Request: store.Request{Collection: collection, ID: id, ExpectedRevision: created.Revision},
			Intent:  store.WriteIntentSaveDraft, Values: store.Values{"body": store.Null()},
		}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	mongoCommit(t, write)
	reports, err := backend.ReviewDevelopmentFieldKinds(ctx, before, after)
	if err != nil || len(reports) != 1 || reports[0].Documents != 2 || reports[0].Snapshots != 1 {
		t.Fatalf("logical active-head and history counts = %#v, %v", reports, err)
	}
	if err := backend.ClearDevelopmentFieldKinds(ctx, before, after, reports); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, after); err != nil {
		t.Fatal(err)
	}
	read := mongoBegin(t, backend, true)
	defer mongoRollback(t, read)
	for _, id := range []string{"both-heads", "live-only"} {
		for _, publishedOnly := range []bool{false, true} {
			document, err := read.Find(ctx, store.Request{Collection: after.Snapshot().Collections[0], ID: id, PublishedOnly: publishedOnly})
			if err != nil {
				t.Fatal(err)
			}
			if value, exists := document.Values["body"]; exists && value.Kind() != store.ValueNull {
				t.Fatalf("%s head publishedOnly=%t retained incompatible body: %#v", id, publishedOnly, document.Values)
			}
		}
	}
	versions, err := read.(store.VersionTransaction).ListVersions(ctx, store.VersionRequest{Collection: after.Snapshot().Collections[0], DocumentID: "both-heads"})
	if err != nil || len(versions) != 1 {
		t.Fatalf("retained history after clear = %#v, %v", versions, err)
	}
	if value, exists := versions[0].Snapshot.Values["body"]; exists && value.Kind() != store.ValueNull {
		t.Fatalf("retained snapshot kept incompatible body: %#v", versions[0].Snapshot.Values)
	}
}

func TestMongoDBDevelopmentFieldClearWaitsForMigrationLease(t *testing.T) {
	backend := mongoIntegrationStore(t)
	before, err := core.Resolve(core.Config{Name: "Field recovery", Collections: []core.Collection{{Slug: "posts", Fields: field.Fields{field.Text("body")}}}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := core.Resolve(core.Config{Name: "Field recovery", Collections: []core.Collection{{Slug: "posts", Fields: field.Fields{field.Number("body")}}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := backend.SyncIndexes(ctx, before); err != nil {
		t.Fatal(err)
	}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: before.Snapshot().Collections[0], ID: "owner", Values: store.Values{"body": store.String("old")}}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	reports, err := backend.ReviewDevelopmentFieldKinds(ctx, before, after)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := backend.acquireMongoMigrationLease(ctx, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	blockedContext, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := backend.ClearDevelopmentFieldKinds(blockedContext, before, after, reports); err == nil || !strings.Contains(err.Error(), "lease") {
		t.Fatalf("clear entered an active migrator's lease: %v", err)
	}
	unchanged, err := backend.ReviewDevelopmentFieldKinds(ctx, before, after)
	if err != nil || unchanged[0].Documents != 1 {
		t.Fatalf("blocked clear changed content: %#v, %v", unchanged, err)
	}
	lease.release()
	if err := backend.ClearDevelopmentFieldKinds(ctx, before, after, reports); err != nil {
		t.Fatal(err)
	}
	cleared, err := backend.ReviewDevelopmentFieldKinds(ctx, before, after)
	if err != nil || cleared[0].Documents != 0 {
		t.Fatalf("clear after lease release = %#v, %v", cleared, err)
	}
}
