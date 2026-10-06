package sqlite

import (
	"bytes"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestSQLiteBlockNameTransitionPreservesContentAndHistory(t *testing.T) {
	for _, immutable := range []bool{false, true} {
		name := "development"
		if immutable {
			name = "immutable migrations"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			current, err := ridu.Resolve(ridu.Config{Name: "Block schema transition", Collections: []ridu.Collection{{
				Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields: field.Fields{field.Text("title"), field.Blocks("layout", field.Block{
					Slug: "card", Fields: field.Fields{field.Text("title")},
					Admin: field.BlockAdmin{RowLabelPath: "title"},
				})},
			}}})
			if err != nil {
				t.Fatal(err)
			}
			previousSnapshot := current.Snapshot()
			block := &previousSnapshot.Blocks[0]
			block.Fields = block.Fields[:1]
			previous := schema.NewManifest(previousSnapshot)
			encoded, err := previous.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			encoded = bytes.ReplaceAll(encoded, []byte(`"rowLabel": "title"`), []byte(`"nameField": "title", "rowLabel": "title"`))
			previous, err = schema.ParseHistorical(encoded)
			if err != nil {
				t.Fatal(err)
			}
			backend := newSQLiteMigrationStore(t)
			directory := t.TempDir()
			var initialFile CreatedArtifact
			var initialBytes []byte
			if immutable {
				initialFile, err = CreateArtifact(ctx, directory, "initial", previous, time.Unix(1, 0), false)
				if err != nil {
					t.Fatal(err)
				}
				initialBytes, err = os.ReadFile(initialFile.Path)
				if err != nil {
					t.Fatal(err)
				}
				if err := backend.ApplyArtifacts(ctx, directory); err != nil {
					t.Fatal(err)
				}
			} else if err := backend.Migrate(ctx, previous); err != nil {
				t.Fatal(err)
			}
			if recorded, exists, err := backend.DevelopmentManifest(ctx); err != nil || !exists || !recorded.Equal(previous) {
				t.Fatalf("historical baseline changed: exists=%t, error=%v", exists, err)
			}
			collection := previousSnapshot.Collections[0]
			values := func(title string) store.Values {
				return store.Values{
					"title": store.String(title),
					"layout": store.List(store.Object(store.Values{
						"_key": store.String("stable-card"), "blockType": store.String("card"), "title": store.String(title),
					})),
				}
			}
			write, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer write.Rollback(ctx)
			published, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "kept", Status: store.StatusPublished, Values: values("Published content")})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := write.(store.VersionTransaction).SaveVersion(ctx, collection, published, 10); err != nil {
				t.Fatal(err)
			}
			working, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{
				Request: store.Request{Collection: collection, ID: published.ID, ExpectedRevision: published.Revision},
				Intent:  store.WriteIntentSaveDraft, Values: values("Working content"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := write.(store.VersionTransaction).SaveVersion(ctx, collection, working, 10); err != nil {
				t.Fatal(err)
			}
			if err := write.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			readContent := func(collection schema.Collection) ([]store.Document, []store.Version) {
				t.Helper()
				read, err := backend.BeginSnapshot(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer read.Rollback(ctx)
				var heads []store.Document
				for _, publishedOnly := range []bool{false, true} {
					document, err := read.Find(ctx, store.Request{Collection: collection, ID: "kept", PublishedOnly: publishedOnly})
					if err != nil {
						t.Fatal(err)
					}
					heads = append(heads, document)
				}
				versions, err := read.(store.VersionTransaction).ListVersions(ctx, store.VersionRequest{Collection: collection, DocumentID: "kept"})
				if err != nil || len(versions) != 2 {
					t.Fatalf("retained versions: count=%d, error=%v", len(versions), err)
				}
				return heads, versions
			}
			headsBefore, versionsBefore := readContent(collection)
			if immutable {
				if _, err := CreateArtifact(ctx, directory, "add-block-name", current, time.Unix(2, 0), false); err != nil {
					t.Fatal(err)
				}
				if err := VerifyArtifacts(ctx, directory); err != nil {
					t.Fatal(err)
				}
				if err := backend.ApplyArtifacts(ctx, directory); err != nil {
					t.Fatal(err)
				}
				files, err := migrationartifact.ReadAll(directory)
				if err != nil || len(files) != 2 || files[1].Artifact.PreviousArtifactDigest != initialFile.Checksum {
					t.Fatalf("historical artifact lineage changed: files=%#v, error=%v", files, err)
				}
				actual, err := os.ReadFile(initialFile.Path)
				if err != nil || !bytes.Equal(actual, initialBytes) || files[0].Digest != initialFile.Checksum {
					t.Fatalf("historical artifact bytes or digest changed: %v", err)
				}
			} else if err := backend.Migrate(ctx, current); err != nil {
				t.Fatal(err)
			}
			if recorded, exists, err := backend.DevelopmentManifest(ctx); err != nil || !exists || !recorded.Equal(current) {
				t.Fatalf("current baseline not recorded: exists=%t, error=%v", exists, err)
			}
			if err := backend.Ready(ctx, current); err != nil {
				t.Fatal(err)
			}
			headsAfter, versionsAfter := readContent(current.Snapshot().Collections[0])
			if !reflect.DeepEqual(headsBefore, headsAfter) || !reflect.DeepEqual(versionsBefore, versionsAfter) {
				t.Fatal("adding blockName changed current/published content, block identity, or retained versions")
			}
		})
	}
}
