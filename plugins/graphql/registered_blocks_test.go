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
	for text, want := range map[string]int{"type CardBlock {": 1, "tone: CardBlockToneOption\n": 1, "input CardBlockToneOptionWhere {": 1, "type SectionBlock {": 1} {
		if got := strings.Count(sdl, text); got != want {
			t.Fatalf("%q appears %d times, want %d:\n%s", text, got, want, sdl)
		}
	}
	for _, name := range []string{"Page", "Post", "Landing"} {
		where := sdl[strings.Index(sdl, "input "+name+"Where {"):]
		where = where[:strings.Index(where, "}")]
		if !strings.Contains(where, "layout__card__tone: CardBlockToneOptionWhere") || !strings.Contains(where, "layout__section__cards__card__tone: CardBlockToneOptionWhere") {
			t.Fatalf("%sWhere does not filter shared block fields:\n%s", name, where)
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
  Pages(where: {layout__section__cards__card__tone: {equals: LOUD}}) {
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
