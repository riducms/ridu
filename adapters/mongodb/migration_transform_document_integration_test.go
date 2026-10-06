package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// A data transform updates the stored document, never a copy the callback
// read with a projection or edited, and its deletes take the document's
// framework state with them, so a user later created with the same ID
// inherits none of it.
func TestMongoDBDataTransformWritesStoredDocumentsAndDeletesTheirState(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	manifest, err := ridu.Resolve(ridu.Config{Name: "Transform documents", Admin: ridu.AdminConfig{User: "users"}, Collections: []ridu.Collection{
		{Slug: "notes", Fields: field.Fields{field.Text("title"), field.Text("body")}},
		{Slug: "users", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	notes, users := snapshot.Collections[0], snapshot.Collections[1]
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	createUser := func(email string) {
		t.Helper()
		transaction := mongoBegin(t, backend, false)
		if _, err := transaction.Create(ctx, store.CreateRequest{Collection: users, ID: "removed", Values: store.Values{"email": store.String(email)}}); err != nil {
			mongoRollback(t, transaction)
			t.Fatal(err)
		}
		mongoCommit(t, transaction)
	}
	transaction := mongoBegin(t, backend, false)
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: notes, ID: "kept", Values: store.Values{"title": store.String("Old"), "body": store.String("Body")}}); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	mongoCommit(t, transaction)
	createUser("removed@example.test")
	if _, err := backend.SetPreference(ctx, store.Preference{CollectionID: users.ID, UserID: "removed", Key: "theme", Value: json.RawMessage(`{"mode":"dark"}`)}); err != nil {
		t.Fatal(err)
	}

	descriptor := migration.DataTransformDescriptor{Name: "retitle", Checksum: migration.DataTransformChecksum([]byte("retitle-v1"))}
	if _, err := CreateArtifact(ctx, directory, "retitle", manifest, time.Unix(2, 0), ArtifactOptions{DataTransforms: []migration.DataTransformDescriptor{descriptor}}); err != nil {
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
			projected.Values["title"] = store.String("Edited copy")
			if _, err := transaction.Update(ctx, migration.UpdateRequest{
				Request: store.Request{Collection: notes, ID: "kept"}, Values: store.Values{"title": store.String("New")},
			}); err != nil {
				return err
			}
			_, err = transaction.Delete(ctx, store.Request{Collection: users, ID: "removed"})
			return err
		},
		Down: func(context.Context, migration.DataTransaction) error { return nil },
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}, transform); err != nil {
		t.Fatal(err)
	}
	read := mongoBegin(t, backend, true)
	kept, err := read.Find(ctx, store.Request{Collection: notes, ID: "kept"})
	mongoRollback(t, read)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := kept.Values["title"].StringValue(); got != "New" {
		t.Fatalf("transformed title = %q, want New", got)
	}
	if got, _ := kept.Values["body"].StringValue(); got != "Body" {
		t.Fatalf("unselected body after a transform update = %#v, want the stored value", kept.Values["body"])
	}
	createUser("recreated@example.test")
	if preference, err := backend.GetPreference(ctx, users.ID, "removed", "theme"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("recreated user inherited the transform-deleted user's preference %s, %v", preference.Value, err)
	}
}
