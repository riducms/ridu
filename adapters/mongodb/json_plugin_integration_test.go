package mongodb

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestMongoDBJSONAndPluginFieldEnvelopeUsesStoreJSONVocabulary(t *testing.T) {
	manifest, err := ridu.Resolve(mongoJSONPluginConfig())
	if err != nil {
		t.Fatal(err)
	}
	collection := mongoCollectionsBySlug(manifest.Snapshot().Collections)["pages"]
	if err := validateCollectionEnvelope(collection); err != nil {
		t.Fatalf("JSON and plugin field envelope rejected: %v", err)
	}

	content := mongoRichTextDocument("portable", "post-1", "asset-1", "Portable asset")
	for _, fixture := range []struct {
		name  string
		value store.Value
	}{
		{name: "object", value: store.Object(store.Values{"nested": store.List(store.Null(), store.Boolean(true))})},
		{name: "list", value: store.List(store.String("value"), store.Number(2))},
		{name: "string", value: store.String("value")},
		{name: "number", value: store.Number(2)},
		{name: "boolean", value: store.Boolean(true)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if err := validateCompleteValues(collection, store.Values{"metadata": fixture.value, "content": content}); err != nil {
				t.Fatalf("JSON root value rejected: %v", err)
			}
		})
	}

	metadataPath, _ := query.NewPath("metadata")
	contentPath, _ := query.NewPath("content")
	if _, err := requestProjection(store.Request{Collection: collection, Select: []query.Path{metadataPath, contentPath}}); err != nil {
		t.Fatalf("whole-field JSON/plugin projection rejected: %v", err)
	}

	malformed := collection
	malformed.Fields = append([]schema.Field(nil), collection.Fields...)
	malformed.Fields[1].Plugin = nil
	if err := validateCollectionEnvelope(malformed); err == nil {
		t.Fatal("plugin field without its resolved contract was accepted")
	}
}

func TestMongoDBJSONAndOfficialRichTextLifecycle(t *testing.T) {
	backend := mongoIntegrationStore(t)
	files, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := mongoJSONPluginConfig()
	config.Storage = files
	config.StorageNamespace = "mongodb-richtext-test"
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	firstPostID, firstMediaID := createMongoRichTextTargets(t, backend, application.Manifest(), "first")
	secondPostID, secondMediaID := createMongoRichTextTargets(t, backend, application.Manifest(), "second")

	firstMetadata := store.Object(store.Values{
		"":            store.String("empty key"),
		"a.b":         store.String("dotted key"),
		"$literal":    store.String("dollar key"),
		"label":       store.String("first"),
		"rank":        store.Number(1),
		"enabled":     store.Boolean(true),
		"empty":       store.Null(),
		"emptyObject": store.Object(store.Values{}),
		"emptyList":   store.List(),
		"nested": store.Object(store.Values{
			"items": store.List(store.String("one"), store.Number(2), store.Boolean(false), store.Null()),
		}),
	})
	firstContent := mongoRichTextDocument("First", firstPostID, firstMediaID, "First asset")
	created, err := application.Local().Create(t.Context(), "pages", store.Values{
		"metadata": firstMetadata,
		"content":  firstContent,
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != store.StatusDraft || created.Revision != 1 {
		t.Fatalf("created version metadata = %s/%d, want draft/1", created.Status, created.Revision)
	}
	assertMongoJSONPluginValues(t, created, firstMetadata, firstContent, "First")

	draft := true
	metadataPath, _ := query.NewPath("metadata")
	contentPath, _ := query.NewPath("content")
	found, err := application.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{
		Draft: &draft, Select: []query.Path{metadataPath, contentPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertMongoJSONPluginValues(t, found, firstMetadata, firstContent, "First")

	secondMetadata := store.List(
		store.String("second"),
		store.Object(store.Values{
			"$literal": store.String("still data"),
			"a.b":      store.List(store.Number(3), store.Null()),
		}),
	)
	secondContent := mongoRichTextDocument("Second", secondPostID, secondMediaID, "Second asset")
	updated, err := application.Local().Update(t.Context(), "pages", created.ID, store.Values{
		"metadata": secondMetadata,
		"content":  secondContent,
	}, ridu.MutationOptions{ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.StatusDraft || updated.Revision != 2 {
		t.Fatalf("updated version metadata = %s/%d, want draft/2", updated.Status, updated.Revision)
	}
	assertMongoJSONPluginValues(t, updated, secondMetadata, secondContent, "Second")

	found, err = application.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	assertMongoJSONPluginValues(t, found, secondMetadata, secondContent, "Second")

	versions, err := application.Local().Versions(t.Context(), "pages", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Revision != 2 || versions[1].Revision != 1 {
		t.Fatalf("versions = %#v, want revisions [2, 1]", versions)
	}
	assertMongoJSONPluginValues(t, versions[0].Snapshot, secondMetadata, secondContent, "Second")
	assertMongoJSONPluginValues(t, versions[1].Snapshot, firstMetadata, firstContent, "First")

	restored, err := application.Local().Restore(t.Context(), "pages", created.ID, 1, ridu.MutationOptions{ExpectedRevision: updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != store.StatusDraft || restored.Revision != 3 {
		t.Fatalf("restored version metadata = %s/%d, want draft/3", restored.Status, restored.Revision)
	}
	assertMongoJSONPluginValues(t, restored, firstMetadata, firstContent, "First")

	found, err = application.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	assertMongoJSONPluginValues(t, found, firstMetadata, firstContent, "First")
	versions, err = application.Local().Versions(t.Context(), "pages", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 || versions[0].Revision != 3 || versions[1].Revision != 2 || versions[2].Revision != 1 {
		t.Fatalf("versions after restore = %#v, want revisions [3, 2, 1]", versions)
	}
}

func mongoJSONPluginConfig() ridu.Config {
	return ridu.Config{
		Name:    "MongoDB JSON and rich text",
		Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{
			{
				Slug: "pages", Versions: true,
				VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 5},
				Fields: field.Fields{field.JSON("metadata").Required(), richtext.Field("content", richtext.Config{
					Features:                []richtext.Feature{richtext.FeatureRelationships, richtext.FeatureUploads},
					RelationshipCollections: []string{"posts"},
					UploadCollections:       []string{"media"},
				}).Required(),
				},
			},
			{Slug: "posts", Fields: field.Fields{field.Text("title").Required()}},
			{Slug: "media", Upload: true},
		},
	}
}

func mongoRichTextDocument(text, relationshipID, uploadID, caption string) store.Value {
	return store.Object(store.Values{
		"version": store.Number(richtext.DocumentVersion),
		"root": store.Object(store.Values{
			"type": store.String("root"),
			"children": store.List(
				store.Object(store.Values{
					"type": store.String("paragraph"),
					"children": store.List(store.Object(store.Values{
						"type": store.String("text"), "text": store.String(text),
					})),
				}),
				store.Object(store.Values{
					"type": store.String("relationship"), "relationTo": store.String("posts"), "id": store.String(relationshipID),
				}),
				store.Object(store.Values{
					"type": store.String("upload"), "relationTo": store.String("media"), "id": store.String(uploadID), "caption": store.String(caption),
				}),
			),
		}),
	})
}

func createMongoRichTextTargets(t *testing.T, backend *Store, manifest schema.Manifest, suffix string) (string, string) {
	t.Helper()
	collections := mongoCollectionsBySlug(manifest.Snapshot().Collections)
	transaction := mongoBegin(t, backend, false)
	post, err := transaction.Create(t.Context(), store.CreateRequest{
		Collection: collections["posts"], ID: suffix + "-post",
		Values: store.Values{"title": store.String(suffix + " post")},
	})
	if err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	media, err := transaction.Create(t.Context(), store.CreateRequest{
		Collection: collections["media"], ID: suffix + "-media",
		Values: store.Values{
			"filename": store.String(suffix + ".txt"), "mimeType": store.String("text/plain"), "filesize": store.Number(5),
			"url": store.String("/" + suffix + ".txt"), "objectKey": store.String("richtext/" + suffix + ".txt"),
		},
	})
	if err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	mongoCommit(t, transaction)
	return post.ID, media.ID
}

func assertMongoJSONPluginValues(t testing.TB, document store.Document, metadata, content store.Value, text string) {
	t.Helper()
	actualMetadata, err := json.Marshal(document.Values["metadata"])
	if err != nil {
		t.Fatalf("encode stored metadata: %v", err)
	}
	wantedMetadata, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("encode expected metadata: %v", err)
	}
	if !bytes.Equal(actualMetadata, wantedMetadata) {
		t.Fatalf("metadata = %s, want %s", actualMetadata, wantedMetadata)
	}
	actualContent, err := json.Marshal(document.Values["content"])
	if err != nil {
		t.Fatalf("encode stored rich text: %v", err)
	}
	wantedContent, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("encode expected rich text: %v", err)
	}
	if !bytes.Equal(actualContent, wantedContent) {
		t.Fatalf("rich text = %s, want %s", actualContent, wantedContent)
	}
	rendered, err := richtext.RenderHTML(document.Values["content"], map[string]func(store.Values) (string, error){
		"relationship": func(store.Values) (string, error) { return "", nil },
		"upload":       func(store.Values) (string, error) { return "", nil },
	})
	if err != nil {
		t.Fatalf("render rich text: %v", err)
	}
	want := "<p>" + text + "</p>"
	if rendered != want {
		t.Fatalf("rendered rich text = %q, want %q", rendered, want)
	}
}
