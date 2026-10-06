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
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
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

// A transform takes its collection from its own migration, so a fresh database
// still replays it after a later migration changes that collection. The
// current config's shape is refused once it no longer matches the migration.
func TestSQLiteDataTransformReplaysAfterALaterSchemaChange(t *testing.T) {
	ctx := context.Background()
	resolve := func(fields field.Fields) schema.Manifest {
		manifest, err := ridu.Resolve(ridu.Config{Name: "Transform replay", Collections: []ridu.Collection{{
			Slug: "notes", Fields: fields,
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before := resolve(field.Fields{field.Text("title")})
	after := resolve(field.Fields{field.Text("title"), field.Text("slug")})
	descriptor := migration.DataTransformDescriptor{Name: "seed-notes", Checksum: migration.DataTransformChecksum([]byte("seed-notes-v1"))}
	directory := t.TempDir()
	for index, step := range []struct {
		name          string
		before, after *schema.Manifest
		transforms    []migration.DataTransformDescriptor
	}{
		{name: "initial", after: &before},
		{name: "seed-notes", before: &before, after: &before, transforms: []migration.DataTransformDescriptor{descriptor}},
		{name: "add-slug", before: &before, after: &after},
	} {
		artifact, err := planArtifact(ctx, step.name, step.before, *step.after, false, step.transforms...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrationartifact.Create(directory, step.name, artifact, time.Unix(int64(index+1), 0)); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(collection func(migration.DataTransaction) (schema.Collection, error)) migration.DataTransform {
		return migration.DataTransform{
			DataTransformDescriptor: descriptor,
			Up: func(ctx context.Context, transaction migration.DataTransaction) error {
				notes, err := collection(transaction)
				if err != nil {
					return err
				}
				_, err = transaction.Create(ctx, store.CreateRequest{Collection: notes, ID: "seeded", Values: store.Values{"title": store.String("Seeded")}})
				return err
			},
			Down: func(context.Context, migration.DataTransaction) error { return nil },
		}
	}

	current := after.Snapshot().Collections[0]
	stale := seed(func(migration.DataTransaction) (schema.Collection, error) { return current, nil })
	if err := newSQLiteMigrationStore(t).ApplyArtifacts(ctx, directory, stale); err == nil || !strings.Contains(err.Error(), "must exactly match its immutable before or after manifest shape") {
		t.Fatalf("replay with the current config's shape = %v, want the manifest-shape refusal", err)
	}

	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory, seed(func(transaction migration.DataTransaction) (schema.Collection, error) {
		return transaction.Collection("notes")
	})); err != nil {
		t.Fatal(err)
	}
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	seeded, err := read.Find(ctx, store.Request{Collection: current, ID: "seeded"})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := seeded.Values["title"].StringValue(); title != "Seeded" {
		t.Fatalf("seeded title = %q, want Seeded", title)
	}
}
