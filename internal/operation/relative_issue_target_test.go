package operation

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestRelativeIssueTargetChecksCandidateIdentityAndCase(t *testing.T) {
	links := func(slug string) schema.Field {
		url := schema.Field{ID: schema.StableID("block-" + slug + "-links-url"), Name: "url", Path: mustTargetPath(t, "links.url"), Type: schema.FieldTypeText}
		return schema.Field{ID: schema.StableID("block-" + slug + "-links"), Name: "links", Path: mustTargetPath(t, "links"), Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{url}}}
	}
	blocks := schematest.Bind(t, "pages", []schema.BlockType{{Slug: "card", TypeName: "Card", Fields: []schema.Field{links("card")}}, {Slug: "note", TypeName: "Note", Fields: []schema.Field{links("note")}}},
		schema.Field{ID: "pages-content", Name: "content", Path: mustTargetPath(t, "content"), Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{BlockReferences: []string{"card", "note"}}})[0]
	link := store.Object(store.Values{"_key": store.String("link.b[0]"), "url": store.String("invalid")})
	card := store.Object(store.Values{"_key": store.String("section-a"), "blockType": store.String("card"), "links": store.List(link)})
	ctx := Context{Collection: schema.Collection{ID: "pages", Slug: "pages"}, RuntimePath: "content", Value: store.List(card), Locale: "fr"}
	target := operation.At().Block("section-a", "card").Field("links").Row("link.b[0]").Field("url")
	// The resolved field is named by its placement beneath the block.
	f, path, locale, err := ResolveIssueTarget(boundTo(ctx, blocks), target)
	if err != nil || f.ID != "pages-content-card-links-url" || f.Path.String() != "content.card.links.url" || path != "content.0.links.0.url" || locale != "fr" {
		t.Fatalf("target = %s %s %s %v", f.ID, path, locale, err)
	}
	for name, test := range map[string]struct {
		value  store.Value
		target operation.IssueTarget
		cause  string
	}{
		"deleted row":                 {store.List(), target, "no row"},
		"replacement case":            {store.List(store.Object(store.Values{"_key": store.String("section-a"), "blockType": store.String("note"), "links": store.List(link)})), target, "expected \"card\""},
		"ambiguous identity":          {store.List(card, card), target, "ambiguous"},
		"wrong selector":              {store.List(card), operation.At().Row("section-a"), "Row requires an array"},
		"missing child":               {store.List(card), operation.At().Block("section-a", "card").Field("missing"), "no child field"},
		"missing nested row":          {store.List(card), operation.At().Block("section-a", "card").Field("links").Row("missing").Field("url"), "no row"},
		"implicit repeated traversal": {store.List(card), operation.At("links.url"), "use Row or Block"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx.Value = test.value
			_, _, _, err := ResolveIssueTarget(boundTo(ctx, blocks), test.target)
			if err == nil || !strings.Contains(err.Error(), test.cause) {
				t.Fatalf("target failure = %v; want %s", err, test.cause)
			}
		})
	}
}

// boundTo binds ctx to a resource's own field, as a field callback's context is.
func boundTo(ctx Context, field schema.Field) Context {
	ctx.bound = boundField{field: &field}
	return ctx
}

func mustTargetPath(t *testing.T, raw string) query.Path {
	t.Helper()
	path, err := query.ParsePath(raw)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRelativeIssueTargetLocaleBoundaries(t *testing.T) {
	title := schema.Field{ID: "title", Name: "title", Type: schema.FieldTypeText}
	rows := schema.Field{ID: "rows", Name: "rows", Type: schema.FieldTypeArray, Localized: true, Nested: &schema.NestedField{Fields: []schema.Field{title}}}
	group := schema.Field{ID: "group", Name: "group", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{rows}}}
	value := store.List(store.Object(store.Values{"_key": store.String("A"), "title": store.String("French")}))
	ctx := Context{RuntimePath: "group", AllLocales: true, Value: store.Object(store.Values{"rows": store.Object(store.Values{"fr": value})})}
	target := operation.At("rows").Locale("fr").Row("A").Field("title")
	f, path, locale, err := ResolveIssueTarget(boundTo(ctx, group), target)
	if err != nil || f.ID != "title" || path != "group.rows.fr.0.title" || locale != "fr" {
		t.Fatalf("localized target = %s %s %s %v", f.ID, path, locale, err)
	}
	for _, target := range []operation.IssueTarget{operation.At("rows").Row("A"), operation.At("rows").Locale("en").Row("A")} {
		if _, _, _, err := ResolveIssueTarget(boundTo(ctx, group), target); err == nil {
			t.Fatal("ambiguous/missing translation accepted")
		}
	}
	ctx = Context{RuntimePath: "rows", Locale: "fr", Value: value}
	if _, _, _, err := ResolveIssueTarget(boundTo(ctx, rows), operation.At().Locale("en").Row("A")); err == nil {
		t.Fatal("exact-locale callback escaped its translation")
	}
	_, path, locale, err = ResolveIssueTarget(boundTo(ctx, rows), operation.At().Row("A").Field("title"))
	if err != nil || path != "rows.0.title" || locale != "fr" {
		t.Fatalf("inherited locale = %s %s %v", path, locale, err)
	}
}

func TestIssueTargetsRetainPhysicalIndicesAcrossNonRows(t *testing.T) {
	title := schema.Field{ID: "title", Name: "title", Type: schema.FieldTypeText}
	rows := schema.Field{ID: "rows", Name: "rows", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{title}}}
	value := store.List(
		store.Null(),
		store.Object(store.Values{"title": store.String("no identity")}),
		store.Object(store.Values{"_key": store.String("target"), "title": store.String("invalid")}),
	)
	field, path, _, err := ResolveIssueTarget(boundTo(Context{RuntimePath: "rows", Value: value}, rows), operation.At().Row("target").Field("title"))
	if err != nil || field.ID != title.ID || path != "rows.2.title" {
		t.Fatalf("target after skipped rows = %s %s %v", field.ID, path, err)
	}
	locations := fieldIssueLocations(Collection{Schema: schema.Collection{Fields: []schema.Field{rows}}}, store.Values{"rows": value}, false, "")
	if location, exists := locations[path]; !exists || location.fieldID != title.ID {
		t.Fatalf("field issue location missing at physical row index: %+v", locations)
	}
	if _, exists := locations["rows.0.title"]; exists {
		t.Fatal("malformed row acquired a child issue target")
	}
	if _, exists := locations["rows.1.title"]; exists {
		t.Fatal("row without stable identity acquired a child issue target")
	}
}
