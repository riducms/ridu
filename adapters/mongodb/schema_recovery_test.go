package mongodb

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDecodersPreserveSchemaRecoveryDiagnostics(t *testing.T) {
	blocks := []schema.BlockType{{Slug: "hero", TypeName: "Hero"}, mongoEmbeddedCard()}
	bind := func(field schema.Field) schema.Field { return schematest.Bind(t, "pages", blocks, field)[0] }
	blockAt := func(path string) schema.Field {
		return schema.Field{Name: "layout", Path: query.Field(strings.Split(path, ".")...), Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{BlockReferences: []string{"hero"}}}
	}
	block := bind(blockAt("layout"))
	unknown := store.List(store.Object(store.Values{"blockType": store.String("retired-private-type"), "private": store.String("private-payload")}))
	embeddedValue := func(kind, key string) store.Value {
		payload := store.Values{"variant": store.String(kind), "title": store.String("private-payload")}
		if key != "" {
			payload["uid"] = store.String(key)
		}
		return store.Object(store.Values{"document": store.Object(store.Values{"kind": store.String("widget"), "attributes": store.Object(payload)})})
	}
	for _, test := range []struct {
		name  string
		field schema.Field
		value store.Value
		path  string
		code  string
	}{
		{"root", block, unknown, "layout.0.blockType", "unknown_block_schema"},
		{"localized", func() schema.Field { f := blockAt("layout"); f.Localized = true; return bind(f) }(), store.Object(store.Values{"en": unknown}), "layout.en.0.blockType", "unknown_block_schema"},
		{"nested", bind(schema.Field{Name: "sections", Path: query.Field("sections"), Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{blockAt("sections.layout")}}}), store.List(store.Object(store.Values{"layout": unknown})), "sections.0.layout.0.blockType", "unknown_block_schema"},
		{"embedded-unknown", mongoEmbeddedFixture(t), embeddedValue("retired-private-type", "one"), "canvas.document.attributes.variant", "unknown_embedded_schema"},
		{"embedded-missing-identity", mongoEmbeddedFixture(t), embeddedValue("card", ""), "canvas.document.attributes.uid", "missing_embedded_identity"},
		{"embedded-localized", func() schema.Field { f := mongoEmbeddedCanvas("canvas"); f.Localized = true; return bind(f) }(), store.Object(store.Values{"en": embeddedValue("retired-private-type", "one")}), "canvas.en.document.attributes.variant", "unknown_embedded_schema"},
		{"embedded-nested", bind(schema.Field{Name: "settings", Path: query.Field("settings"), Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{mongoEmbeddedCanvas("settings.canvas")}}}), store.Object(store.Values{"canvas": embeddedValue("retired-private-type", "one")}), "settings.canvas.document.attributes.variant", "unknown_embedded_schema"},
	} {
		t.Run(test.name, func(t *testing.T) {
			collection := schema.Collection{ID: "pages", Slug: "pages", Versions: &schema.VersionSettings{Drafts: true}, Fields: []schema.Field{test.field}}
			now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
			document := store.Document{ID: "page", CreatedAt: now, UpdatedAt: now, Status: store.StatusDraft, Revision: 1, Values: store.Values{test.field.Name: test.value}}
			encoded, err := encodeDocument(document)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := bson.Marshal(encoded)
			if err != nil {
				t.Fatal(err)
			}
			versionRaw, err := bson.Marshal(bson.D{
				{Key: "_id", Value: versionID(document.ID, document.Revision)},
				{Key: mongoVersionOwnerPath, Value: document.ID},
				{Key: mongoVersionRevisionPath, Value: int64(document.Revision)},
				{Key: mongoVersionStatusPath, Value: string(document.Status)},
				{Key: mongoVersionCreatedAtPath, Value: now.UnixNano()},
				{Key: mongoVersionSnapshotPath, Value: encoded},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, decode := range []struct {
				name string
				run  func() error
			}{
				{"document", func() error {
					doc, err := decodeCollectionDocumentForLocales(raw, collection, []schema.LocaleCode{"en"})
					if len(doc.Values) > 0 {
						t.Fatal("failed decoder returned payload")
					}
					return err
				}},
				{"version", func() error {
					version, err := decodeMongoVersionForLocales(versionRaw, collection, []schema.LocaleCode{"en"})
					if len(version.Snapshot.Values) > 0 {
						t.Fatal("failed decoder returned historical payload")
					}
					return err
				}},
			} {
				t.Run(decode.name, func(t *testing.T) {
					err := decode.run()
					var recovery *store.SchemaRecoveryError
					if !errors.As(err, &recovery) || len(recovery.Issues) != 1 || recovery.Issues[0].Code != test.code || recovery.Issues[0].Path != test.path {
						t.Fatalf("recovery error = %#v (%v)", recovery, err)
					}
					if strings.Contains(err.Error(), "retired-private-type") || strings.Contains(err.Error(), "private-payload") {
						t.Fatal("error disclosed unknown content")
					}
				})
			}
		})
	}
}
