package embedded_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func mustPath(raw string) query.Path {
	path, err := query.ParsePath(raw)
	if err != nil {
		panic(err)
	}
	return path
}

// cardBlock is the embedded payload definition; its paths and IDs are
// definition-relative and every placement derives its own.
func cardBlock() schema.BlockType {
	return schema.BlockType{Slug: "card", TypeName: "Card", Fields: []schema.Field{
		{ID: "block-card-heading", Name: "heading", Path: mustPath("heading"), Type: schema.FieldTypeText, Localized: true},
		{ID: "block-card-author", Name: "author", Path: mustPath("author"), Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{CollectionID: "authors", CollectionSlug: "authors", OnDelete: schema.ReferenceDeleteNullify}},
		{ID: "block-card-opaque", Name: "opaque", Path: mustPath("opaque"), Type: schema.FieldTypeJSON},
	}}
}

func canvasField(path string) schema.Field {
	payload := schema.EmbeddedTreeCase{TagValue: "widget", Payload: "attributes", Identity: "uid", Discriminator: "variant", BlockReferences: []string{"card"}}
	tree := schema.EmbeddedTree{Version: 1, Key: "parts", Root: []string{"document"}, Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{payload}}
	return schema.Field{ID: schema.StableID("pages-" + path), Name: "canvas", Path: mustPath(path), Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{Key: "canvas", EmbeddedTrees: []schema.EmbeddedTree{tree}}}
}

// envelopeSchema returns the canvas field bound as a field of pages.
func envelopeSchema(t testing.TB) schema.Field {
	return schematest.Bind(t, "pages", []schema.BlockType{cardBlock()}, canvasField("canvas"))[0]
}

// authorPlacementID is the stable ID of the card author at its only placement.
const authorPlacementID schema.StableID = "pages-canvas-parts-widget-card-author"

func object(values store.Values) store.Value { return store.Object(values) }
func envelope(rows ...store.Value) store.Value {
	return object(store.Values{"document": object(store.Values{"kind": store.String("canvas"), "items": store.List(rows...)})})
}
func card(id, title, author string) store.Value {
	return object(store.Values{"kind": store.String("widget"), "attributes": object(store.Values{"uid": store.String(id), "variant": store.String("card"), "heading": store.String(title), "author": store.String(author)})})
}
func payloads(t *testing.T, field schema.Field, value store.Value) map[string]store.Values {
	t.Helper()
	rows, err := embedded.Occurrences(field, value, "canvas", nil)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]store.Values{}
	for _, row := range rows {
		result[row.Key] = row.Payload
	}
	return result
}

func TestEmbeddedLocalizationUsesIdentityAcrossReorderedEnvelope(t *testing.T) {
	field := envelopeSchema(t)
	fields := []schema.Field{field}
	en := localization.Selection{Locale: "en", Chain: []schema.LocaleCode{"en"}, Configured: []schema.LocaleCode{"en", "fr"}}
	fr := localization.Selection{Locale: "fr", Chain: []schema.LocaleCode{"fr"}, Configured: en.Configured}
	current, err := localization.StoragePatch(fields, store.Values{"canvas": envelope(card("a", "Alpha", "author-a"), card("b", "Beta", "author-b"))}, en)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := localization.StoragePatch(fields, store.Values{"canvas": envelope(card("b", "Bêta", "author-b"), card("a", "Alphabète", "author-a"))}, fr)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := localization.MergeStorageUpdateChecked(fields, current, patch)
	if err != nil {
		t.Fatal(err)
	}
	rows := payloads(t, field, merged["canvas"])
	for id, want := range map[string]map[string]string{"a": {"en": "Alpha", "fr": "Alphabète"}, "b": {"en": "Beta", "fr": "Bêta"}} {
		locales, _ := rows[id]["heading"].CopyObject()
		for locale, title := range want {
			got, _ := locales[locale].StringValue()
			if got != title {
				t.Fatalf("%s/%s=%q want %q", id, locale, got, title)
			}
		}
	}
	projected, err := localization.ProjectDocumentChecked(store.Document{Values: merged}, fields, en)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := payloads(t, field, projected.Values["canvas"])["b"]["heading"].StringValue(); got != "Beta" {
		t.Fatalf("projection=%q", got)
	}
	if got := projected.LocalizationSources["canvas.document.items.0.attributes.heading"]; got != "en" {
		t.Fatalf("source locale=%q", got)
	}
	copied, err := localization.LocalizedValuesChecked(fields, projected.Values)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := payloads(t, field, copied["canvas"])["a"]["author"]; exists {
		t.Fatal("copy included nonlocalized sibling")
	}
	if issues := localization.CopyLocaleIssues(fields, copied); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestEmbeddedReferenceLifecycleAndPopulationIgnoreOpaqueLookalikes(t *testing.T) {
	field := envelopeSchema(t)
	fields := []schema.Field{field}
	collection := schema.Collection{ID: "pages", Fields: fields}
	value := envelope(card("a", "Alpha", "author-a"))
	value, err := embedded.Transform(field, value, "canvas", nil, func(o embedded.Occurrence) (store.Values, error) {
		o.Payload["opaque"] = envelope(card("fake", "Hidden", "author-hidden"))
		return o.Payload, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	document := store.Document{ID: "page", Values: store.Values{"canvas": value}}
	entries, err := referenceindex.Collect(collection, document)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Target.DocumentID != "author-a" {
		t.Fatalf("references=%#v", entries)
	}
	if got := referenceindex.ReferenceFieldIDs(collection.ID, field); !reflect.DeepEqual(got, []schema.StableID{authorPlacementID}) {
		t.Fatal(got)
	}
	reference, root, found := referenceindex.FindReferenceField(collection, authorPlacementID)
	if !found || root.Name != "canvas" || reference.Name != "author" {
		t.Fatalf("schema discovery=%#v", reference)
	}
	if !referenceindex.TargetsAnyResource(collection, []schema.StableID{"authors"}) {
		t.Fatal("missing embedded target")
	}
	path, _ := query.ParsePath("canvas.parts.widget.card.author")
	// Population resolves configuration: the block's shared definition field.
	if actual, found := population.FieldAtPath(fields, path); !found || actual.ID != "block-card-author" {
		t.Fatal("population schema missing")
	}
	mapped, matched := population.MapAtPath(fields, document.Values, path, population.LocaleSelection{}, func(_ schema.Field, value store.Value) store.Value {
		return store.String("populated-" + stringValue(value))
	})
	if !matched || stringValue(payloads(t, field, mapped["canvas"])["a"]["author"]) != "populated-author-a" {
		t.Fatal("population missed occurrence")
	}
	updated, changed, err := referenceindex.NullifyTarget(collection, document.Values, store.DocumentReference{CollectionID: "authors", DocumentID: "author-a"})
	if err != nil || !changed {
		t.Fatalf("nullify %v %v", changed, err)
	}
	if payloads(t, field, updated["canvas"])["a"]["author"].Kind() != store.ValueNull {
		t.Fatal("reference retained")
	}
	if !reflect.DeepEqual(payloads(t, field, updated["canvas"])["a"]["opaque"], payloads(t, field, value)["a"]["opaque"]) {
		t.Fatal("opaque JSON changed")
	}
}
func stringValue(value store.Value) string { result, _ := value.StringValue(); return result }

func TestEmbeddedIndexRejectsMalformedEnvelopeWithoutPartialResults(t *testing.T) {
	field := envelopeSchema(t)
	valid := card("a", "Alpha", "author-a")
	invalid := object(store.Values{"kind": store.String("widget"), "attributes": object(store.Values{"uid": store.String("b"), "variant": store.String("removed")})})
	entries, err := referenceindex.Collect(schema.Collection{ID: "pages", Fields: []schema.Field{field}}, store.Document{Values: store.Values{"canvas": envelope(valid, invalid)}})
	if err == nil || !strings.Contains(err.Error(), "canvas.document.items.1.attributes.variant") || len(entries) != 0 {
		t.Fatalf("partial index/error: %#v %v", entries, err)
	}
}

func TestEmbeddedIndexBudgetRejectsWholeDocument(t *testing.T) {
	field := envelopeSchema(t)
	nodes := make([]store.Value, embedded.MaxNodes+1)
	for i := range nodes {
		nodes[i] = object(store.Values{"kind": store.String("text")})
	}
	nodes[0] = card("one", "Title", "author")
	entries, err := referenceindex.Collect(schema.Collection{ID: "pages", Fields: []schema.Field{field}}, store.Document{Values: store.Values{"canvas": envelope(nodes...)}})
	if err == nil || !strings.Contains(err.Error(), "budget") || len(entries) != 0 {
		t.Fatalf("budget failure produced index: %d %v", len(entries), err)
	}
}

func TestEmbeddedWholeFieldLocalesIndexDistinctOccurrences(t *testing.T) {
	field := canvasField("canvas")
	field.Localized = true
	field = schematest.Bind(t, "pages", []schema.BlockType{cardBlock()}, field)[0]
	value := object(store.Values{"en": envelope(card("shared", "English", "author-en")), "fr": envelope(card("shared", "French", "author-fr"))})
	entries, err := referenceindex.Collect(schema.Collection{ID: "pages", Fields: []schema.Field{field}}, store.Document{ID: "page", Values: store.Values{"canvas": value}})
	if err != nil || len(entries) != 2 {
		t.Fatalf("references: %#v %v", entries, err)
	}
	byLocale := map[schema.LocaleCode]string{}
	for _, entry := range entries {
		byLocale[entry.Locale] = entry.Target.DocumentID
	}
	if byLocale["en"] != "author-en" || byLocale["fr"] != "author-fr" {
		t.Fatalf("localized references: %v", byLocale)
	}
}

func TestEmbeddedIndexPreflightInsideBlocksWithRowLabelMetadata(t *testing.T) {
	plugin := canvasField("canvas")
	plugin.ID = "block-panel-canvas"
	panel := schema.BlockType{Slug: "panel", TypeName: "Panel", Fields: []schema.Field{plugin}}
	wrapper := schema.Field{ID: "pages-layout", Name: "layout", Path: mustPath("layout"), Type: schema.FieldTypeBlocks, Nested: &schema.NestedField{Fields: []schema.Field{}}, Blocks: &schema.BlocksField{BlockReferences: []string{"panel"}}}
	wrapper = schematest.Bind(t, "pages", []schema.BlockType{panel, cardBlock()}, wrapper)[0]
	invalid := object(store.Values{"kind": store.String("widget"), "attributes": object(store.Values{"uid": store.String("b"), "variant": store.String("removed")})})
	row := object(store.Values{"blockType": store.String("panel"), "_key": store.String("row"), "canvas": envelope(card("a", "First", "author-a"), invalid)})
	entries, err := referenceindex.Collect(schema.Collection{ID: "pages", Fields: []schema.Field{wrapper}}, store.Document{Values: store.Values{"layout": store.List(row)}})
	if err == nil || !strings.Contains(err.Error(), "layout.0.canvas.document.items.1.attributes.variant") || len(entries) != 0 {
		t.Fatalf("Blocks row-label metadata bypassed preflight: %#v %v", entries, err)
	}
}
