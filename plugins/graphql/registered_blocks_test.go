package graphql_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

// A registered block is one GraphQL type however many collections and blocks
// reference it, so the schema grows with block definitions, not placements.
func TestRegisteredBlocksShareOneTypeAcrossPlacements(t *testing.T) {
	card := field.Block{Slug: "card", Fields: field.Fields{field.Text("heading"), field.Select("tone", "quiet", "loud")}}
	section := field.Block{Slug: "section", Fields: field.Fields{field.Blocks("cards").References("card")}}
	config := ridu.Config{Name: "Shared blocks", Plugins: []ridu.Plugin{graphqlplugin.New()}, Blocks: []field.Block{card, section}}
	for _, slug := range []schema.CollectionSlug{"pages", "posts", "landings"} {
		config.Collections = append(config.Collections, ridu.Collection{Slug: slug, Fields: field.Fields{
			field.Text("title"), field.Blocks("layout").References("card", "section"),
		}})
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	sdl, err := graphqlplugin.GenerateSDL(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string]int{
		"type CardBlock {": 1, "tone: CardBlockToneOption\n": 1, "input CardBlockToneOptionWhere {": 1, "type SectionBlock {": 1,
		// Filters nest like the schema, with one input for each registered block.
		"input CardBlockWhere {": 1, "tone: CardBlockToneOptionWhere\n": 1, "input SectionBlockWhere {": 1, "card: CardBlockWhere\n": 4,
	} {
		if got := strings.Count(sdl, text); got != want {
			t.Fatalf("%q appears %d times, want %d:\n%s", text, got, want, sdl)
		}
	}
	inputBody := func(name string) string {
		body := sdl[strings.Index(sdl, "input "+name+" {"):]
		return body[:strings.Index(body, "}")]
	}
	if cards := inputBody("SectionBlockCardsWhere"); !strings.Contains(cards, "card: CardBlockWhere") {
		t.Fatalf("SectionBlockCardsWhere does not select the shared block:\n%s", cards)
	}
	for _, name := range []string{"Page", "Post", "Landing"} {
		if where := inputBody(name + "Where"); !strings.Contains(where, "layout: "+name+"LayoutWhere") {
			t.Fatalf("%sWhere does not filter its layout:\n%s", name, where)
		}
		if layout := inputBody(name + "LayoutWhere"); !strings.Contains(layout, "card: CardBlockWhere") || !strings.Contains(layout, "section: SectionBlockWhere") {
			t.Fatalf("%sLayoutWhere does not select shared blocks:\n%s", name, layout)
		}
	}

	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	cardValue := func(heading, tone string) store.Value {
		return store.Object(store.Values{"blockType": store.String("card"), "heading": store.String(heading), "tone": store.String(tone)})
	}
	for title, nestedTone := range map[string]string{"Nested loud": "loud", "Nested quiet": "quiet"} {
		layout := store.List(
			store.Object(store.Values{"blockType": store.String("section"), "cards": store.List(cardValue(title, nestedTone))}),
			cardValue("Top", "quiet"),
		)
		if _, err := app.Local().Create(context.Background(), "pages", store.Values{"title": store.String(title), "layout": layout}, ridu.MutationOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	result := graphQL(t, server.URL, `{
  Pages(where: {layout: {section: {cards: {card: {tone: {equals: LOUD}}}}}}) {
    docs {
      title
      layout {
        __typename
        ... on CardBlock { heading tone }
        ... on SectionBlock { cards { ... on CardBlock { heading tone } } }
      }
    }
  }
}`)
	docs, _ := objectAt(t, result, "data", "Pages")["docs"].([]interface{})
	if len(docs) != 1 {
		t.Fatalf("filtered pages = %#v", result)
	}
	layout, _ := docs[0].(map[string]interface{})["layout"].([]interface{})
	if len(layout) != 2 {
		t.Fatalf("layout = %#v", docs[0])
	}
	sectionBlock, top := layout[0].(map[string]interface{}), layout[1].(map[string]interface{})
	cards, _ := sectionBlock["cards"].([]interface{})
	if sectionBlock["__typename"] != "SectionBlock" || top["__typename"] != "CardBlock" || top["heading"] != "Top" || len(cards) != 1 {
		t.Fatalf("layout = %#v", layout)
	}
	if nested := cards[0].(map[string]interface{}); nested["heading"] != "Nested loud" || nested["tone"] != "LOUD" {
		t.Fatalf("nested card = %#v", nested)
	}
}

// The schema is built eagerly at startup, so its size must follow block
// definitions. This graph has 24 definitions and about 56,000 placements.
func TestGraphQLSchemaFollowsDefinitions(t *testing.T) {
	config := blockreferences.LayeredConfig(7, 3)
	config.Plugins = append(config.Plugins, graphqlplugin.New())
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	sdl, err := graphqlplugin.GenerateSDL(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if blocks := strings.Count(sdl, "Block {\n"); blocks != 24 {
		t.Fatalf("schema has %d block types", blocks)
	}
	if len(sdl) > 64<<10 {
		t.Fatalf("schema is %d bytes", len(sdl))
	}
}
