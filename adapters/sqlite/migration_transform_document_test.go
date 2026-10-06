package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// A data transform updates the stored document, never a copy the callback
// read with a projection or edited, and its deletes take the document's
// framework state with them, so a document later created with the same ID
// inherits nothing.
func TestSQLiteDataTransformWritesStoredDocumentsAndDeletesTheirState(t *testing.T) {
	ctx := context.Background()
	manifest, err := ridu.Resolve(ridu.Config{Name: "Transform documents", Collections: []ridu.Collection{{
		Slug: "notes", Fields: field.Fields{field.Text("title"), field.Text("body")},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	notes := manifest.Snapshot().Collections[0]
	directory := t.TempDir()
	initial, err := planArtifact(ctx, "initial", nil, manifest, false)
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
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"kept", "removed"} {
		if _, err := write.Create(ctx, store.CreateRequest{Collection: notes, ID: id, Values: store.Values{"title": store.String("Old"), "body": store.String("Body")}}); err != nil {
			_ = write.Rollback(ctx)
			t.Fatal(err)
		}
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.SetPreference(ctx, store.Preference{CollectionID: notes.ID, UserID: "removed", Key: "theme", Value: json.RawMessage(`{"mode":"dark"}`)}); err != nil {
		t.Fatal(err)
	}

	descriptor := migration.DataTransformDescriptor{Name: "retitle", Checksum: migration.DataTransformChecksum([]byte("retitle-v1"))}
	retitle, err := planArtifact(ctx, "retitle", &manifest, manifest, false, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "retitle", retitle, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	title, _ := query.ParsePath("title")
	transform := migration.DataTransform{
		DataTransformDescriptor: descriptor,
		Up: func(ctx context.Context, transaction migration.DataTransaction) error {
			projected, err := transaction.Find(ctx, store.Request{Collection: notes, ID: "kept", Select: []query.Path{title}})
			if err != nil {
				return err
			}
			if _, present := projected.Values["body"]; present {
				return errors.New("projected read returned an unselected field")
			}
			projected.Values["title"] = store.String("Edited copy")
			if _, err := transaction.Update(ctx, migration.UpdateRequest{
				Request: store.Request{Collection: notes, ID: "kept", Select: []query.Path{title}}, Values: store.Values{"title": store.String("New")},
			}); err != nil {
				return err
			}
			_, err = transaction.Delete(ctx, store.Request{Collection: notes, ID: "removed"})
			return err
		},
		Down: func(context.Context, migration.DataTransaction) error { return nil },
	}
	if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
		t.Fatal(err)
	}

	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := read.Find(ctx, store.Request{Collection: notes, ID: "kept"})
	_ = read.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := kept.Values["title"].StringValue(); got != "New" {
		t.Fatalf("transformed title = %q, want New", got)
	}
	if got, _ := kept.Values["body"].StringValue(); got != "Body" {
		t.Fatalf("unselected body after a transform update = %#v, want the stored value", kept.Values["body"])
	}
	recreate, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recreate.Create(ctx, store.CreateRequest{Collection: notes, ID: "removed", Values: store.Values{"title": store.String("Recreated")}}); err != nil {
		_ = recreate.Rollback(ctx)
		t.Fatal(err)
	}
	if err := recreate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if preference, err := backend.GetPreference(ctx, notes.ID, "removed", "theme"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("recreated document inherited the transform-deleted document's preference %s, %v", preference.Value, err)
	}
}
