package mongodb

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

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
