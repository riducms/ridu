package graphql_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/field"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestGraphQLPostgresCRUDLocalizationAndPopulation(t *testing.T) {
	databaseURL := os.Getenv("RIDU_GRAPHQL_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("RIDU_GRAPHQL_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schemaName := fmt.Sprintf("ridu_graphql_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop GraphQL test schema: %v", err)
		}
	}()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	parsed.RawQuery = parameters.Encode()
	databaseURL = parsed.String()
	backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: databaseURL, AllowInsecureTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	config := ridu.Config{
		Name: "GraphQL PostgreSQL", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}},
		Collections: []ridu.Collection{
			{Slug: "categories", Fields: field.Fields{
				field.Text("name").Required().Localized(),
				field.Join("posts", "posts", "category"),
			}},
			{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 5}, Fields: field.Fields{
				field.Text("title").Required().Localized(),
				field.Relationship("category", "categories"),
				field.Group("seo", field.Fields{
					field.Text("description"),
				}),
				field.Array("links", field.Fields{
					field.Text("label"),
				}),
			}},
		},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := backend.Plan(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	category := graphQL(t, server.URL, `mutation { createCategory(locale: EN, data: {name: "News"}) { id } }`)
	categoryID := objectAt(t, category, "data", "createCategory")["id"].(string)
	post := graphQL(t, server.URL, `mutation($category: ID!) { createPost(draft: false, locale: EN, data: {title: "Postgres", category: $category, seo: {description: "Indexed"}, links: [{label: "Docs"}]}) { id } }`, map[string]interface{}{"category": categoryID})
	postID := objectAt(t, post, "data", "createPost")["id"].(string)
	categoryPath, _ := query.ParsePath("category")
	published := false
	if _, err := application.Local().Find(ctx, "posts", postID, ridu.FindOptions{Locale: "fr", Draft: &published, Populate: []query.Population{{Path: categoryPath, Depth: 1}}}); err != nil {
		var operationError *ridu.OperationError
		errors.As(err, &operationError)
		t.Fatalf("direct PostgreSQL populated find: %v (cause: %v)", err, operationError.Cause)
	}
	result := graphQL(t, server.URL, `query($id: ID!, $category: ID!) {
  Post(id: $id, locale: FR) { title category { name } }
  Posts(where: {seo__description: {equals: "Indexed"}, links__label: {equals: "Docs"}}) { totalDocs }
  Category(id: $category) { posts(count: true) { docs { title } totalDocs } }
}`, map[string]interface{}{"id": postID, "category": categoryID})
	if objectAt(t, result, "data")["Post"] == nil {
		t.Fatalf("PostgreSQL singular result = %#v", result)
	}
	document := objectAt(t, result, "data", "Post")
	if document["title"] != "Postgres" || document["category"].(map[string]interface{})["name"] != "News" {
		t.Fatalf("PostgreSQL GraphQL result = %#v", document)
	}
	if objectAt(t, result, "data", "Posts")["totalDocs"] != float64(1) || objectAt(t, result, "data", "Category", "posts")["totalDocs"] != float64(1) {
		t.Fatalf("PostgreSQL nested filter/join result = %#v", result)
	}
	draft := graphQL(t, server.URL, `mutation { createPost(data: {title: "Draft"}) { id } }`)
	draftID := objectAt(t, draft, "data", "createPost")["id"].(string)
	assertErrorCode(t, graphQL(t, server.URL, `query($id: ID!) { Post(id: $id) { id } }`, map[string]interface{}{"id": draftID}), "not_found")
	if objectAt(t, graphQL(t, server.URL, `query($id: ID!) { Post(id: $id, draft: true) { id } }`, map[string]interface{}{"id": draftID}), "data", "Post")["id"] != draftID {
		t.Fatal("PostgreSQL explicit draft read did not return the draft")
	}
}
