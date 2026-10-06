package mongodb

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// mongoBlockGraphManifest resolves a layout builder whose section blocks may
// hold every lower block. Definitions grow linearly with depth while field
// placements grow exponentially, as in real block registries.
func mongoBlockGraphManifest(t testing.TB, depth int) schema.Manifest {
	t.Helper()
	blocks := []field.Block{
		{Slug: "text", Fields: field.Fields{field.Text("heading"), field.Textarea("body"), field.Select("tone", "plain", "loud")}},
		{Slug: "image", Fields: field.Fields{field.Relationship("image", "media"), field.Text("caption")}},
	}
	lower := []string{"text", "image"}
	for level := 1; level <= depth; level++ {
		slug := fmt.Sprintf("section-%d", level)
		blocks = append(blocks, field.Block{Slug: slug, Fields: field.Fields{
			field.Text("title"),
			field.Group("style", field.Fields{field.Checkbox("wide")}),
			field.Array("links", field.Fields{field.Text("label"), field.Text("url")}),
			field.Blocks("content").References(lower...),
		}})
		lower = append(lower, slug)
	}
	manifest, err := ridu.Resolve(ridu.Config{
		Name:   "MongoDB block graph",
		Blocks: blocks,
		Collections: []ridu.Collection{
			{Slug: "media", Fields: field.Fields{field.Text("title")}},
			{Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{
				field.Text("title"), field.Blocks("layout").References(lower...),
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// mongoBlockLibraryManifest resolves a large block library: definitions
// distinct feature blocks declared inline, each with editorial guidance and
// nesting registered text and image blocks. A manifest records every
// definition once.
func mongoBlockLibraryManifest(t testing.TB, definitions int) schema.Manifest {
	t.Helper()
	var blocks []field.Block
	for index := 1; index <= definitions; index++ {
		guidance := strings.Repeat(fmt.Sprintf("Summarize feature %d in one or two sentences for listings and previews. ", index), 48)
		blocks = append(blocks, field.Block{Slug: fmt.Sprintf("feature-%d", index), Fields: field.Fields{
			field.Text("title").Required(),
			field.Textarea("summary").EditAdmin(func(admin *field.Admin) { admin.Description = guidance }),
			field.Select("tone", "plain", "loud", "muted"),
			field.Number("priority"),
			field.Relationship("image", "media"),
			field.Group("style", field.Fields{field.Checkbox("wide"), field.Text("accent")}),
			field.Array("links", field.Fields{field.Text("label"), field.Text("url")}),
			field.Blocks("content").References("text", "image"),
		}})
	}
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "MongoDB block library",
		Blocks: []field.Block{
			{Slug: "text", Fields: field.Fields{field.Text("heading"), field.Textarea("body"), field.Select("tone", "plain", "loud")}},
			{Slug: "image", Fields: field.Fields{field.Relationship("image", "media"), field.Text("caption")}},
		},
		Collections: []ridu.Collection{
			{Slug: "media", Fields: field.Fields{field.Text("title")}},
			{Slug: "pages", Fields: field.Fields{field.Text("title"), field.Blocks("layout", blocks...)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// MongoDB stores one document per schema record. A large block library's
// manifest is larger than that limit as JSON but compresses well within it,
// and the record must round-trip exactly.
func TestMongoSchemaRecordStoresManifestsLargerThanADocument(t *testing.T) {
	if testing.Short() {
		t.Skip("resolves a large block library")
	}
	manifest := mongoBlockLibraryManifest(t, 3000)
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) <= mongoMaxDocumentBytes {
		t.Fatalf("fixture manifest is only %d bytes of JSON", len(encoded))
	}
	compressed, err := encodeMongoDevelopmentManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) > mongoMaxSchemaRecordManifestBytes/4 {
		t.Fatalf("compressed manifest is %d bytes", len(compressed))
	}
	digest, err := ridumigration.DigestManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	record, err := bson.Marshal(bson.D{
		{Key: "_id", Value: mongoDevelopmentSchemaID},
		{Key: mongoSchemaRecordManifestField, Value: bson.Binary{Subtype: bson.TypeBinaryGeneric, Data: compressed}},
		{Key: "manifestDigest", Value: digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMongoDevelopmentManifest(record)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(manifest) {
		t.Fatal("decoded schema record differs from the recorded manifest")
	}
}

func mongoManifestCollection(t testing.TB, manifest schema.Manifest, slug schema.CollectionSlug) schema.Collection {
	t.Helper()
	for _, collection := range manifest.Snapshot().Collections {
		if collection.Slug == slug {
			return collection
		}
	}
	t.Fatalf("manifest has no collection %q", slug)
	return schema.Collection{}
}

// Shape guards follow block definitions, never the placements those
// definitions reach: the decoder-free envelope guards rows by identity, and a
// filter on a block path guards the compared rows to their own fields. Deeper
// rows are validated by the strict decoder.
func TestMongoShapeGuardsStayBoundedOnDeepBlockGraphs(t *testing.T) {
	sizes := map[int]int{}
	for _, depth := range []int{4, 8} {
		pages := mongoManifestCollection(t, mongoBlockGraphManifest(t, depth), "pages")
		envelope, err := decoderFreeRequestPredicate(store.Request{Collection: pages, PublishedOnly: true}, false)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := bson.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		sizes[depth] = len(encoded)
		text, err := bson.MarshalExtJSON(envelope, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(text), `"heading"`) || strings.Contains(string(text), `"content"`) {
			t.Fatalf("depth %d envelope describes block row content: %s", depth, text)
		}
		if !strings.Contains(string(text), `"layout":{"bsonType":["array","null"],"items":{"bsonType":"object","properties":{"_key"`) {
			t.Fatalf("depth %d envelope does not guard layout rows by identity: %s", depth, text)
		}

		path, err := query.ParsePath(fmt.Sprintf("layout.section-%d.title", depth))
		if err != nil {
			t.Fatal(err)
		}
		filter := query.NotEqual(path, "hidden").Node()
		compiled, err := requestPredicate(store.Request{Collection: pages, Filter: &filter}, false)
		if err != nil {
			t.Fatal(err)
		}
		encodedFilter, err := bson.Marshal(compiled)
		if err != nil {
			t.Fatal(err)
		}
		if len(encodedFilter) > 32*1024 {
			t.Fatalf("depth %d block-path filter is %d bytes", depth, len(encodedFilter))
		}
		filterText, err := bson.MarshalExtJSON(compiled, false, false)
		if err != nil {
			t.Fatal(err)
		}
		// The compared rows' block definitions appear once each; a repeated
		// field inside those rows is guarded by row identity only.
		if count := strings.Count(string(filterText), `"heading"`); count != 1 {
			t.Fatalf("depth %d filter describes the text block %d times", depth, count)
		}
		if !strings.Contains(string(filterText), `"content":{"bsonType":["array","null"],"items":{"bsonType":"object","properties":{"_key"`) {
			t.Fatalf("depth %d filter does not guard nested rows by identity: %s", depth, filterText)
		}
	}
	if sizes[8] > 8*1024 {
		t.Fatalf("depth 8 decoder-free envelope is %d bytes", sizes[8])
	}
	// Four more definitions add only their slugs to the layout's row identity.
	if sizes[8]-sizes[4] > 256 {
		t.Fatalf("envelope grew from %d to %d bytes for four more definitions", sizes[4], sizes[8])
	}
}
