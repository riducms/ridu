package postgres

import (
	"reflect"
	"testing"

	atlasschema "ariga.io/atlas/sql/schema"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func TestPostgresLiveTableTwinsWorkingLayout(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Live table layout",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{Slug: "categories", Fields: field.Fields{field.Text("name")}},
			{
				Slug: "posts", Trash: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Indexes: []ridu.CollectionIndex{{Fields: []string{"slug", "rank"}, Unique: true}, {Fields: []string{"seo.title", "rank"}}},
				Fields: field.Fields{
					field.Text("title").Localized().Unique(),
					field.Text("slug").Unique(),
					field.Number("rank").Index(),
					field.Relationship("category", "categories"),
					field.Relationship("translatedCategory", "categories").Localized(),
					field.Group("seo", field.Fields{field.Text("title")}),
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	physical := atlasSchema(manifest, atlasIdentityMap{})
	upload := atlasSchema(atlasUploadReferenceManifest(), atlasIdentityMap{})
	categories := manifest.Snapshot().Collections[0]
	if _, exists := physical.Table(publishedCollectionTable(categories.ID)); exists {
		t.Fatal("an unversioned collection has a live table")
	}
	for _, item := range []struct {
		schema *atlasschema.Schema
		id     schema.StableID
	}{{physical, manifest.Snapshot().Collections[1].ID}, {upload, "media"}} {
		working, workingExists := item.schema.Table(collectionTable(item.id))
		live, liveExists := item.schema.Table(publishedCollectionTable(item.id))
		if !workingExists || !liveExists {
			t.Fatalf("%s tables: working %t, live %t", item.id, workingExists, liveExists)
		}
		if len(live.Columns) != len(working.Columns)+1 || live.Columns[len(live.Columns)-1].Name != "has_draft_changes" {
			t.Fatalf("%s live columns differ from working columns plus has_draft_changes", item.id)
		}
		for index, column := range working.Columns {
			if !reflect.DeepEqual(column.Type, live.Columns[index].Type) || column.Name != live.Columns[index].Name || !reflect.DeepEqual(column.Default, live.Columns[index].Default) {
				t.Fatalf("%s live column %d = %#v, want %#v", item.id, index, live.Columns[index], column)
			}
		}
		if len(working.Indexes) == 0 || len(live.Indexes) != len(working.Indexes) {
			t.Fatalf("%s indexes: working %d, live %d", item.id, len(working.Indexes), len(live.Indexes))
		}
		for _, index := range working.Indexes {
			twin, exists := live.Index(livePhysicalName(index.Name))
			if !exists || twin.Unique != index.Unique || atlasIndexDefinitionSignature(twin, live) != atlasIndexDefinitionSignature(index, working) || !reflect.DeepEqual(twin.Attrs, index.Attrs) {
				t.Fatalf("%s index %s has no identical live twin", item.id, index.Name)
			}
		}
		if len(live.ForeignKeys) != len(working.ForeignKeys)+1 {
			t.Fatalf("%s foreign keys: working %d, live %d", item.id, len(working.ForeignKeys), len(live.ForeignKeys))
		}
		for _, foreignKey := range working.ForeignKeys {
			twin, exists := live.ForeignKey(livePhysicalName(foreignKey.Symbol))
			if !exists || twin.RefTable != foreignKey.RefTable || len(twin.Columns) != 1 || twin.Columns[0].Name != foreignKey.Columns[0].Name {
				t.Fatalf("%s foreign key %s has no identical live twin", item.id, foreignKey.Symbol)
			}
		}
		cascade, exists := live.ForeignKey(liveWorkingForeignKey)
		if !exists || cascade.RefTable != working || cascade.OnDelete != atlasschema.Cascade || cascade.Columns[0].Name != "id" {
			t.Fatalf("%s live table does not cascade from its working row: %#v", item.id, cascade)
		}
	}
	if _, exists := physical.Table("ridu_published_documents"); exists {
		t.Fatal("the shared JSON published table still exists")
	}
}
