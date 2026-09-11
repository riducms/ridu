package graphql_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/unifiedfields"
)

func TestUnifiedGraphQLContractsAndOccurrenceRuntime(t *testing.T) {
	for _, mode := range []string{"inline", "reference"} {
		t.Run(mode, func(t *testing.T) { testUnifiedGraphQLContractsAndOccurrenceRuntime(t, mode == "reference") })
	}
}

func testUnifiedGraphQLContractsAndOccurrenceRuntime(t *testing.T, references bool) {
	deny := func(operation.Context) (bool, error) { return false, nil }
	sku := field.Text("sku").Required().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{
		func(_ operation.Context, value operation.Value[string]) (operation.Change[string], error) {
			text, _ := value.Get()
			return operation.Replace(operation.Present(strings.ToUpper(text))), nil
		},
	}}).Validate(func(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
		if text, _ := value.Get(); text == "INVALID" {
			return []operation.Issue{{Code: "invalid_sku", Message: "SKU is unavailable"}}, nil
		}
		return nil, nil
	})
	quote := field.Block{Slug: "quote", Fields: field.Fields{
		field.Text("text"), field.Relationship("source", "users"),
	}}
	content := field.Blocks("content", quote)
	if references {
		content = field.Blocks("content").References("quote")
	}
	config := ridu.Config{
		Name: "Unified GraphQL", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Collections: []ridu.Collection{
			{Slug: "users", Fields: field.Fields{
				field.Text("name").Required(),
				field.Text("privateNote").Access(field.Access{Read: deny}),
			}},
			{Slug: "posts", Fields: field.Fields{
				field.Text("title").Required(),
				field.Text("defaulted").Required().Default("Default"),
				field.Text("secret").Access(field.Access{Read: deny}),
				field.Text("translation").Localized(),
				field.Group("meta", field.Fields{
					field.Number("score"),
					field.Text("caption").Localized(),
				}),
				field.Array("sections", field.Fields{
					sku,
					field.Text("note"),
				}),
				content,
				field.Relationship("author", "users"),
				field.Slug("slug", "title"),
			}},
		},
	}
	if references {
		config.Blocks = []field.Block{quote}
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	sdl, err := graphqlplugin.GenerateSDL(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"title: String!", "defaulted: String\n", "slug: String\n", "_key: String!", "union PostContentBlock", "author: User", "sections: [PostSections]", "translation: String"} {
		if !strings.Contains(sdl, expected) {
			t.Fatalf("missing %q in graph SDL:\n%s", expected, sdl)
		}
	}
	if strings.Contains(sdl, "secret: PostSecretWhere") {
		t.Fatal("attached protected field was exposed as a query capability")
	}
	application, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	user, err := application.Local().Create(context.Background(), "users", store.Values{"name": store.String("Ada"), "privateNote": store.String("private")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	created := graphQL(t, server.URL, `mutation($author: ID!) {
		createPost(data: {title: "Story", secret: "private", author: $author, translation: "Hello", meta: {score: 2, caption: "English"}, sections: [{_key: "A", sku: "one", note: "retained A"}, {_key: "B", sku: "two", note: "retained B"}], content: [{_key: "C", blockType: "quote", text: "Quotation", source: $author}]}) {
			id title defaulted slug secret author { id name privateNote } sections { _key sku note } content { ... on PostContentBlockQuote { _key blockType text source { name } } }
		}
	}`, map[string]interface{}{"author": user.ID})
	post := objectAt(t, created, "data", "createPost")
	if post["defaulted"] != "Default" || post["slug"] != "story" || post["secret"] != nil || objectAt(t, post, "author")["privateNote"] != nil {
		t.Fatalf("graph create projection = %#v", post)
	}
	id := post["id"].(string)
	updated := graphQL(t, server.URL, `mutation($id: ID!) { updatePost(id: $id, data: {sections: [{_key: "B", sku: "second"}, {_key: "A", sku: "first"}]}) { sections { _key sku note } } }`, map[string]interface{}{"id": id})
	rows := objectAt(t, updated, "data", "updatePost")["sections"].([]interface{})
	if row := rows[0].(map[string]interface{}); row["_key"] != "B" || row["sku"] != "SECOND" || row["note"] != "retained B" {
		t.Fatalf("GraphQL reorder lost identity or attached hook: %#v", rows)
	}
	local, err := application.Local().Find(context.Background(), "posts", id, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := local.Values["sections"].ListItem(0)
	key, _ := first.Get("_key").StringValue()
	value, _ := first.Get("sku").StringValue()
	if key != "B" || value != "SECOND" {
		t.Fatalf("Local API disagrees with GraphQL: %#v", first)
	}
	invalid := graphQL(t, server.URL, `mutation($id: ID!) { updatePost(id: $id, data: {sections: [{_key: "B", sku: "invalid"}, {_key: "A", sku: "first"}]}) { id } }`, map[string]interface{}{"id": id})
	assertErrorCode(t, invalid, "validation")
	encoded, _ := json.Marshal(invalid)
	if !strings.Contains(string(encoded), "sections.0.sku") || !strings.Contains(string(encoded), "invalid_sku") {
		t.Fatalf("structured issue lost occurrence location: %s", encoded)
	}
	firstIssue := invalid["errors"].([]interface{})[0].(map[string]interface{})["extensions"].(map[string]interface{})["issues"].([]interface{})[0].(map[string]interface{})
	reordered := graphQL(t, server.URL, `mutation($id: ID!) { updatePost(id: $id, data: {sections: [{_key: "A", sku: "first"}, {_key: "B", sku: "invalid"}]}) { id } }`, map[string]interface{}{"id": id})
	assertErrorCode(t, reordered, "validation")
	reorderedIssue := reordered["errors"].([]interface{})[0].(map[string]interface{})["extensions"].(map[string]interface{})["issues"].([]interface{})[0].(map[string]interface{})
	if firstIssue["target"] == "" || firstIssue["target"] != reorderedIssue["target"] || reorderedIssue["path"] != "sections.1.sku" {
		t.Fatalf("GraphQL issue identity followed an index: %#v -> %#v", firstIssue, reorderedIssue)
	}
	assertErrorCode(t, graphQL(t, server.URL, `{ Posts(sort: "secret") { docs { title } } }`), "field_access_denied")
	graphQL(t, server.URL, `mutation($id: ID!) { updatePost(id: $id, locale: FR, data: {translation: "Bonjour", meta: {caption: "French"}}) { id } }`, map[string]interface{}{"id": id})
	localized := graphQL(t, server.URL, `query($id: ID!) { english: Post(id: $id, locale: EN) { translation meta { caption } } french: Post(id: $id, locale: FR) { translation meta { caption } } }`, map[string]interface{}{"id": id})
	if objectAt(t, localized, "data", "english")["translation"] != "Hello" || objectAt(t, localized, "data", "french", "meta")["caption"] != "French" {
		t.Fatalf("localized graph output = %#v", localized)
	}
}

func TestSharedUnifiedFixtureGraphQLComputedAndEmbeddedContract(t *testing.T) {
	config := unifiedfields.Config()
	config.Plugins = append(config.Plugins, graphqlplugin.New())
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	created := graphQL(t, server.URL, `mutation {
		createUnifiedArticle(data: {title: "GraphQL fixture", sku: " sku-root ", presentationHidden: "logical value", body: {version: 1, root: {type: "root", children: [{type: "block", version: 1, fields: {_key: "embedded-A", blockType: "card", label: "Card", sku: " sku-embedded ", accent: "blue"}}]}}}) { id sku summary body privateNote presentationHidden }
	}`)
	article := objectAt(t, created, "data", "createUnifiedArticle")
	if article["summary"] != "Article: GraphQL fixture" || article["sku"] != "SKU-ROOT" || article["presentationHidden"] != "logical value" {
		t.Fatalf("shared graph runtime/computed exposure = %#v", created)
	}
	body, _ := json.Marshal(article["body"])
	if !strings.Contains(string(body), "SKU-EMBEDDED") || !strings.Contains(string(body), "embedded-A") {
		t.Fatalf("embedded graph did not execute through GraphQL JSON transport: %s", body)
	}
	for _, text := range []string{
		`mutation { createUnifiedArticle(data: {title: "Output is read only", summary: "caller"}) { id } }`,
		`{ UnifiedArticles(allLocales: true) { docs { title } } }`,
		`{ UnifiedArticles(where: {privateNote: {equals: "private"}}) { docs { title } } }`,
	} {
		response := graphQL(t, server.URL, text)
		if errors, _ := response["errors"].([]interface{}); len(errors) == 0 {
			t.Fatalf("unsupported or restricted graph request unexpectedly accepted: %#v", response)
		}
	}
}

func TestUnifiedGraphQLSchemaContractAndDeterminism(t *testing.T) {
	config := ridu.Config{Name: "GraphQL contract", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
		field.Text("title").Required(),
		field.Text("defaulted").Required().Default("Default"),
		field.Group("meta", field.Fields{
			field.Number("score"),
		}),
		field.Array("rows", field.Fields{
			field.Text("value"),
		}),
		field.Blocks("content", field.Block{Slug: "quote", Fields: field.Fields{
			field.Text("text"),
		}}),
	}}}}
	var previous string
	for range 2 {
		manifest, err := ridu.Resolve(config)
		if err != nil {
			t.Fatal(err)
		}
		sdl, err := graphqlplugin.GenerateSDL(manifest)
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"title: String!", "defaulted: String\n", "meta: PostMeta", "score: Float", "rows: [PostRows]", "_key: String!", "union PostContentBlock", "text: String"} {
			if !strings.Contains(sdl, expected) {
				t.Fatalf("missing %q in graph SDL:\n%s", expected, sdl)
			}
		}
		if previous != "" && sdl != previous {
			t.Fatal("unified graph SDL is not deterministic")
		}
		previous = sdl
	}
}
