package postgres

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresDevelopmentFieldClearPreservesReplacementRequiredness(t *testing.T) {
	for _, required := range []bool{false, true} {
		name := "optional replacement"
		if required {
			name = "required replacement rolls back"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			backend := migrationArtifactTestBackend(t)
			resolve := func(body field.Node) schema.Manifest {
				t.Helper()
				manifest, err := core.Resolve(core.Config{Name: "Required field recovery", Collections: []core.Collection{{Slug: "posts", Versions: true, Fields: field.Fields{body, field.Text("summary")}}}})
				if err != nil {
					t.Fatal(err)
				}
				return manifest
			}
			before, after := resolve(field.Text("body").Required()), resolve(field.Number("body").Required(required))
			if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
				t.Fatal(err)
			}
			collection := before.Snapshot().Collections[0]
			write, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer write.Rollback(ctx)
			document, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "retained", Values: store.Values{"body": store.String("old value"), "summary": store.String("untouched")}})
			if err != nil {
				t.Fatal(err)
			}
			version, err := write.(store.VersionTransaction).SaveVersion(ctx, collection, document, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := write.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			reports, err := backend.ReviewDevelopmentFieldKinds(ctx, before, after)
			// The working and live rows are one logical document; the version
			// is the only retained snapshot.
			if err != nil || reports[0].Documents != 1 || reports[0].Snapshots != 1 {
				t.Fatalf("stored recovery counts: %#v, %v", reports, err)
			}
			err = backend.ClearDevelopmentFieldKinds(ctx, before, after, reports)
			if required {
				if err == nil || !strings.Contains(err.Error(), "stays required") {
					t.Fatalf("clearing a replacement that stays required = %v", err)
				}
			} else if err != nil {
				t.Fatalf("optional replacement could not clear an old required field: %v", err)
			}
			readManifest := after
			if required {
				readManifest = before
			}
			if err := backend.VerifySchema(ctx, readManifest); err != nil {
				t.Fatalf("clear changed the wrong physical schema: %v", err)
			}
			read, err := backend.BeginSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer read.Rollback(ctx)
			resource := readManifest.Snapshot().Collections[0]
			actual, err := read.Find(ctx, store.Request{Collection: resource, ID: document.ID})
			if err != nil {
				t.Fatal(err)
			}
			versions, err := read.(store.VersionTransaction).ListVersions(ctx, store.VersionRequest{Collection: resource, DocumentID: document.ID})
			if err != nil || len(versions) != 1 {
				t.Fatalf("retained version history: %#v, %v", versions, err)
			}
			if !required {
				document.Values["body"] = store.Null()
				delete(version.Snapshot.Values, "body")
			}
			if !reflect.DeepEqual(document, actual) || !reflect.DeepEqual(version, versions[0]) {
				t.Fatalf("clear changed unrelated values or metadata: document %#v -> %#v; version %#v -> %#v", document, actual, version, versions[0])
			}
		})
	}
}
