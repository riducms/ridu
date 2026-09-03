package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/store"
)

func TestFrozenSQLiteV1HistoryReplaysWithCurrentRunner(t *testing.T) {
	ctx := context.Background()
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name, digest string
	}{
		{
			name:   "20260829120000.000000000_initial.ridu.json",
			digest: "260e00424b87e71ce2a2018f51472c7532ce7bf530e1b69a3f435dba7ddc32af",
		},
		{
			name:   "20260829120001.000000000_add-summary.ridu.json",
			digest: "3276448bf298e03cf8c7f8e5de952b7622810dcbda7f1fc82f49692f429a5706",
		},
	}
	if len(files) != len(want) {
		t.Fatalf("frozen SQLite history contains %d artifacts, want %d", len(files), len(want))
	}
	for index, expected := range want {
		file := files[index]
		if file.Name != expected.name || file.Digest != expected.digest {
			t.Fatalf("frozen SQLite artifact %d = %s/%s, want %s/%s", index, file.Name, file.Digest, expected.name, expected.digest)
		}
		if file.Artifact.Planner.Name != sqlitePlannerName || file.Artifact.Planner.Version != "1.0.0" {
			t.Fatalf("frozen SQLite artifact %s planner = %#v", file.Name, file.Artifact.Planner)
		}
	}
	if err := VerifyArtifacts(ctx, directory); err != nil {
		t.Fatalf("verify frozen SQLite history: %v", err)
	}

	latest, err := files[len(files)-1].Artifact.AfterManifest()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := Open(ctx, filepath.Join(t.TempDir(), "historical.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("apply frozen SQLite history: %v", err)
	}
	if err := backend.Ready(ctx, latest); err != nil {
		t.Fatalf("ready after frozen SQLite history: %v", err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory, latest)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != len(want) || !statuses[0].Applied || !statuses[1].Applied {
		t.Fatalf("frozen SQLite history status = %#v", statuses)
	}

	collection := latest.Snapshot().Collections[0]
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, err := transaction.Create(ctx, store.CreateRequest{
		Collection: collection,
		ID:         "historical-runner",
		Values: store.Values{
			"title":   store.String("Frozen history"),
			"summary": store.String("Current runner"),
		},
	})
	if err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := read.Find(ctx, store.Request{Collection: collection, ID: created.ID})
	if rollbackErr := read.Rollback(ctx); err == nil && rollbackErr != nil {
		err = rollbackErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if summary, _ := stored.Values["summary"].StringValue(); summary != "Current runner" {
		t.Fatalf("document through frozen SQLite history = %#v", stored)
	}
}
