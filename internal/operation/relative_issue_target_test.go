package operation

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestRelativeIssueTargetChecksCandidateIdentityAndCase(t *testing.T) {
	url := schema.Field{ID: "url", Name: "url", Type: schema.FieldTypeText}
	links := schema.Field{ID: "links", Name: "links", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{url}}}
	blocks := schema.Field{ID: "content", Name: "content", Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{Types: []schema.BlockType{{Slug: "card", Fields: []schema.Field{links}}, {Slug: "note", Fields: []schema.Field{links}}}}}
	link := store.Object(store.Values{"_key": store.String("link.b[0]"), "url": store.String("invalid")})
	card := store.Object(store.Values{"_key": store.String("section-a"), "blockType": store.String("card"), "links": store.List(link)})
	ctx := Context{RuntimePath: "content", Value: store.List(card), Locale: "fr"}
	target := operation.At().Block("section-a", "card").Field("links").Row("link.b[0]").Field("url")
	f, path, locale, err := ResolveIssueTarget(blocks, ctx, target)
	if err != nil || f.ID != "url" || path != "content.0.links.0.url" || locale != "fr" {
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
			_, _, _, err := ResolveIssueTarget(blocks, ctx, test.target)
			if err == nil || !strings.Contains(err.Error(), test.cause) {
				t.Fatalf("target failure = %v; want %s", err, test.cause)
			}
		})
	}
}

func TestRelativeIssueTargetLocaleBoundaries(t *testing.T) {
	title := schema.Field{ID: "title", Name: "title", Type: schema.FieldTypeText}
	rows := schema.Field{ID: "rows", Name: "rows", Type: schema.FieldTypeArray, Localized: true, Nested: &schema.NestedField{Fields: []schema.Field{title}}}
	group := schema.Field{ID: "group", Name: "group", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{rows}}}
	value := store.List(store.Object(store.Values{"_key": store.String("A"), "title": store.String("French")}))
	ctx := Context{RuntimePath: "group", AllLocales: true, Value: store.Object(store.Values{"rows": store.Object(store.Values{"fr": value})})}
	target := operation.At("rows").Locale("fr").Row("A").Field("title")
	f, path, locale, err := ResolveIssueTarget(group, ctx, target)
	if err != nil || f.ID != "title" || path != "group.rows.fr.0.title" || locale != "fr" {
		t.Fatalf("localized target = %s %s %s %v", f.ID, path, locale, err)
	}
	for _, target := range []operation.IssueTarget{operation.At("rows").Row("A"), operation.At("rows").Locale("en").Row("A")} {
		if _, _, _, err := ResolveIssueTarget(group, ctx, target); err == nil {
			t.Fatal("ambiguous/missing translation accepted")
		}
	}
	ctx = Context{RuntimePath: "rows", Locale: "fr", Value: value}
	if _, _, _, err := ResolveIssueTarget(rows, ctx, operation.At().Locale("en").Row("A")); err == nil {
		t.Fatal("exact-locale callback escaped its translation")
	}
	_, path, locale, err = ResolveIssueTarget(rows, ctx, operation.At().Row("A").Field("title"))
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
	field, path, _, err := ResolveIssueTarget(rows, Context{RuntimePath: "rows", Value: value}, operation.At().Row("target").Field("title"))
	if err != nil || field.ID != title.ID || path != "rows.2.title" {
		t.Fatalf("target after skipped rows = %s %s %v", field.ID, path, err)
	}
	locations := fieldIssueLocations([]schema.Field{rows}, store.Values{"rows": value}, false, "")
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
