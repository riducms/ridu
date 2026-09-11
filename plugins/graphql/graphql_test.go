package graphql_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginegraphql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

func TestGraphQLCollectionCRUDFilteringPaginationAndGlobals(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL contracts", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{
				field.Text("title").Required(),
				field.Number("score"),
				field.Checkbox("featured"),
				field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
			},
		}},
		Globals: []ridu.Global{{Slug: "settings", Fields: field.Fields{
			field.Text("siteName"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	created := graphQL(t, server.URL, `mutation {
  createPost(data: {title: "First", score: 9, featured: true, secret: "hidden"}) {
    id title score featured secret
  }
}`)
	post := objectAt(t, created, "data", "createPost")
	if post["title"] != "First" || post["secret"] != nil {
		t.Fatalf("created post = %#v", post)
	}
	id, _ := post["id"].(string)
	if id == "" {
		t.Fatalf("created id = %#v", post["id"])
	}
	access := graphQL(t, server.URL, `query($id: ID!) { docAccessPost(id: $id) { read { permission } update { permission } fields { title { read { permission } } secret { read { permission } } } } docAccessSettings { read { permission } update { permission } } }`, map[string]interface{}{"id": id})
	postAccess := objectAt(t, access, "data", "docAccessPost")
	if !objectAt(t, postAccess, "read")["permission"].(bool) || objectAt(t, postAccess, "fields", "secret", "read")["permission"].(bool) {
		t.Fatalf("post access = %#v", postAccess)
	}
	settingsAccess := objectAt(t, access, "data", "docAccessSettings")
	if !objectAt(t, settingsAccess, "update")["permission"].(bool) {
		t.Fatalf("settings access = %#v", settingsAccess)
	}

	graphQL(t, server.URL, `mutation { createPost(data: {title: "Second", score: 3}) { id } }`)
	listed := graphQL(t, server.URL, `query {
  Posts(where: {AND: [{score: {greater_than: 5}}]}, sort: "-score", page: 1, limit: 10) {
    docs { id title score }
    totalDocs totalPages hasNextPage hasPrevPage pagingCounter
  }
  countPosts(where: {featured: {equals: true}}) { totalDocs }
}`)
	page := objectAt(t, listed, "data", "Posts")
	if page["totalDocs"] != float64(1) {
		t.Fatalf("page = %#v", page)
	}
	docs, _ := page["docs"].([]interface{})
	if len(docs) != 1 || docs[0].(map[string]interface{})["title"] != "First" {
		t.Fatalf("docs = %#v", docs)
	}
	count := objectAt(t, listed, "data", "countPosts")
	if count["totalDocs"] != float64(1) {
		t.Fatalf("count = %#v", count)
	}

	updated := graphQL(t, server.URL, `mutation($id: ID!) {
  updatePost(id: $id, data: {title: "Updated"}) { id title }
}`, map[string]interface{}{"id": id})
	if objectAt(t, updated, "data", "updatePost")["title"] != "Updated" {
		t.Fatalf("updated = %#v", updated)
	}
	settings := graphQL(t, server.URL, `mutation { updateSettings(data: {siteName: "Ridu"}) { siteName } }`)
	if objectAt(t, settings, "data", "updateSettings")["siteName"] != "Ridu" {
		t.Fatalf("settings = %#v", settings)
	}
	deleted := graphQL(t, server.URL, `mutation($id: ID!) { deletePost(id: $id) { id } }`, map[string]interface{}{"id": id})
	if objectAt(t, deleted, "data", "deletePost")["id"] != id {
		t.Fatalf("deleted = %#v", deleted)
	}
}

func TestGraphQLCollectionAccessRemainsAnAtomicStorePredicate(t *testing.T) {
	publishedPath, err := query.ParsePath("published")
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL access", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{
				field.Text("title").Required(),
				field.Checkbox("published"),
			},
			Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Where(query.Equal(publishedPath, query.Boolean(true))), nil
			}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	graphQL(t, server.URL, `mutation { a: createPost(data: {title: "Visible", published: true}) { id } b: createPost(data: {title: "Hidden", published: false}) { id } }`)
	result := graphQL(t, server.URL, `{ Posts(limit: 10) { docs { title } totalDocs } }`)
	page := objectAt(t, result, "data", "Posts")
	if page["totalDocs"] != float64(1) {
		t.Fatalf("access-filtered page = %#v", page)
	}
}

func TestGraphQLDeleteRestrictionUsesStableNonOracleError(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL reference delete restriction", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{
			{Slug: "users", Fields: field.Fields{
				field.Text("name"),
			}},
			{Slug: "posts", Fields: field.Fields{
				field.Text("title"),
				field.Relationship("protectedOwner", "users").OnDelete(field.ReferenceDeleteRestrict),
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(context.Background(), "users", store.Values{"name": store.String("Ada")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := application.Local().Create(context.Background(), "posts", store.Values{
		"title": store.String("Restricted"), "protectedOwner": store.String(target.ID),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	result := graphQL(t, server.URL, `mutation($id: ID!) { deleteUser(id: $id) { id } }`, map[string]interface{}{"id": target.ID})
	assertErrorCode(t, result, "delete_restricted")
	errorsValue, _ := result["errors"].([]interface{})
	errorValue, _ := errorsValue[0].(map[string]interface{})
	if errorValue["message"] != "document deletion is restricted by current references" {
		t.Fatalf("delete restriction message = %#v", errorValue)
	}
	extensions, _ := errorValue["extensions"].(map[string]interface{})
	if extensions["status"] != float64(http.StatusConflict) || extensions["issues"] != nil {
		t.Fatalf("delete restriction extensions = %#v", extensions)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{target.ID, owner.ID, "protectedOwner"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("GraphQL restriction disclosed %q: %s", secret, encoded)
		}
	}
}

func TestGraphQLGlobalAccessFiltersCurrentAndVersionSnapshots(t *testing.T) {
	siteName, err := query.NewPath("siteName")
	if err != nil {
		t.Fatal(err)
	}
	filtered := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(siteName, query.String("Ridu"))), nil
	}
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL filtered globals", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
		Globals: []ridu.Global{{
			Slug: "settings", Versions: true, Fields: field.Fields{
				field.Text("siteName").Required(),
			},
			Access: ridu.GlobalAccess{Read: filtered, ReadVersions: filtered},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Local().UpdateGlobal(context.Background(), "settings", store.Values{"siteName": store.String("Ridu")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().PublishGlobalChanges(context.Background(), "settings", store.Values{"siteName": store.String("Hidden")}, ridu.MutationOptions{ExpectedRevision: first.Revision}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	current := graphQL(t, server.URL, `{ Settings { siteName } }`)
	assertErrorCode(t, current, "not_found")
	versions := graphQL(t, server.URL, `{ versionsSettings(limit: 10) { docs { revision snapshot { siteName } } totalDocs } versionSettings(revision: 1) { revision snapshot { siteName } } }`)
	page := objectAt(t, versions, "data", "versionsSettings")
	if page["totalDocs"] != float64(1) {
		t.Fatalf("filtered global version page = %#v", page)
	}
	docs, _ := page["docs"].([]interface{})
	if len(docs) != 1 || objectAt(t, docs[0].(map[string]interface{}), "snapshot")["siteName"] != "Ridu" {
		t.Fatalf("filtered global version documents = %#v", docs)
	}
	if objectAt(t, versions, "data", "versionSettings")["revision"] != float64(1) {
		t.Fatalf("matching global version = %#v", versions)
	}
	nonMatching := graphQL(t, server.URL, `{ versionSettings(revision: 2) { revision } }`)
	assertErrorCode(t, nonMatching, "not_found")
}

func TestGraphQLSelectionPopulatesRelationshipsThroughTheEngine(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL relationships", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{
			{Slug: "categories", Fields: field.Fields{
				field.Text("name").Required(),
				field.Text("privateNote").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
				field.Relationship("parent", "categories"),
			}},
			{Slug: "posts", Fields: field.Fields{
				field.Text("title").Required(),
				field.Relationship("category", "categories"),
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	parentResult := graphQL(t, server.URL, `mutation { createCategory(data: {name: "Root", privateNote: "redact"}) { id } }`)
	parentID := objectAt(t, parentResult, "data", "createCategory")["id"].(string)
	categoryResult := graphQL(t, server.URL, `mutation($parent: ID!) { createCategory(data: {name: "News", privateNote: "redact", parent: $parent}) { id } }`, map[string]interface{}{"parent": parentID})
	categoryID := objectAt(t, categoryResult, "data", "createCategory")["id"].(string)
	postResult := graphQL(t, server.URL, `mutation($category: ID!) { createPost(data: {title: "Story", category: $category}) { id category { id name privateNote parent { id name privateNote } } } }`, map[string]interface{}{"category": categoryID})
	createdPost := objectAt(t, postResult, "data", "createPost")
	postID := createdPost["id"].(string)
	createdCategory := createdPost["category"].(map[string]interface{})
	if createdCategory["id"] != categoryID || createdCategory["name"] != "News" || createdCategory["privateNote"] != nil {
		t.Fatalf("mutation-populated category = %#v", createdCategory)
	}
	createdParent := createdCategory["parent"].(map[string]interface{})
	if createdParent["id"] != parentID || createdParent["name"] != "Root" || createdParent["privateNote"] != nil {
		t.Fatalf("nested mutation-populated parent = %#v", createdParent)
	}
	result := graphQL(t, server.URL, `query($id: ID!) { Post(id: $id) { title category { ...CategoryDetails } } } fragment CategoryDetails on Category { id name privateNote parent { id name privateNote } }`, map[string]interface{}{"id": postID})
	category := objectAt(t, result, "data", "Post", "category")
	if category["id"] != categoryID || category["name"] != "News" || category["privateNote"] != nil {
		t.Fatalf("populated category = %#v", category)
	}
	parent := category["parent"].(map[string]interface{})
	if parent["id"] != parentID || parent["name"] != "Root" || parent["privateNote"] != nil {
		t.Fatalf("nested populated parent = %#v", parent)
	}
}

func TestGraphQLBlocksExposeTypedUnions(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL blocks", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title").Required(),
			field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{
				field.Text("heading").Required(),
			}}, field.Block{Slug: "quote", Fields: field.Fields{
				field.Text("quote").Required(),
			}}),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	created := graphQL(t, server.URL, `mutation {
  createPost(data: {title: "Blocks", layout: [{blockType: "hero", heading: "Welcome"}, {blockType: "quote", quote: "Hello"}]}) { id }
}`)
	id := objectAt(t, created, "data", "createPost")["id"].(string)
	result := graphQL(t, server.URL, `query($id: ID!) {
  Post(id: $id) {
    layout {
      __typename
      ... on PostLayoutBlockHero { blockType heading }
      ... on PostLayoutBlockQuote { blockType quote }
    }
  }
}`, map[string]interface{}{"id": id})
	post := objectAt(t, result, "data", "Post")
	blocks, _ := post["layout"].([]interface{})
	if len(blocks) != 2 || blocks[0].(map[string]interface{})["__typename"] != "PostLayoutBlockHero" || blocks[1].(map[string]interface{})["__typename"] != "PostLayoutBlockQuote" {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestGraphQLNestedFieldsAndSelectEnumsRemainTyped(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL nested fields", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
			field.Text("title").Required(),
			field.Group("seo", field.Fields{
				field.Text("description"),
				field.Select("tone", "warm", "cool"),
			}),
			field.MultiSelect("roles").Options(field.Option{Value: "admin", Label: "admin"}, field.Option{Value: "editor", Label: "editor"}),
			field.Array("links", field.Fields{
				field.Text("label").Required(),
				field.Text("url").Required(),
			}),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	created := graphQL(t, server.URL, `mutation {
  createPage(data: {title: "Home", seo: {description: "Welcome", tone: WARM}, roles: [EDITOR, ADMIN], links: [{label: "Docs", url: "/docs"}]}) {
    id seo { description tone } roles links { label url }
  }
}`)
	page := objectAt(t, created, "data", "createPage")
	seo := page["seo"].(map[string]interface{})
	links := page["links"].([]interface{})
	roles := page["roles"].([]interface{})
	if seo["description"] != "Welcome" || seo["tone"] != "WARM" || len(roles) != 2 || roles[0] != "EDITOR" || roles[1] != "ADMIN" || links[0].(map[string]interface{})["url"] != "/docs" {
		t.Fatalf("nested page = %#v", page)
	}
	filtered := graphQL(t, server.URL, `query {
  Pages(where: {seo__tone: {equals: WARM}, roles: {contains: ADMIN}, links__label: {equals: "Docs"}}) { docs { title } totalDocs }
}`)
	filteredPage := objectAt(t, filtered, "data", "Pages")
	if filteredPage["totalDocs"] != float64(1) || filteredPage["docs"].([]interface{})[0].(map[string]interface{})["title"] != "Home" {
		t.Fatalf("nested filtered page = %#v", filteredPage)
	}
}

func TestGraphQLSelectionPopulatesRelationshipsInsideGroupsArraysAndBlocks(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL nested relationships", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{
			{Slug: "people", Fields: field.Fields{
				field.Text("name").Required(),
				field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
			}},
			{Slug: "teams", Fields: field.Fields{
				field.Text("name").Required(),
				field.Relationship("owner", "people"),
			}},
			{Slug: "pages", Fields: field.Fields{
				field.Text("title").Required(),
				field.Group("meta", field.Fields{
					field.Relationship("reviewer", "people"),
				}),
				field.Array("sections", field.Fields{
					field.Relationship("reviewer", "people"),
				}),
				field.Blocks("layout", field.Block{Slug: "quote", Fields: field.Fields{
					field.Relationship("source", "teams"),
				}}),
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	person, err := application.Local().Create(context.Background(), "people", store.Values{"name": store.String("Ada"), "secret": store.String("redact")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	team, err := application.Local().Create(context.Background(), "teams", store.Values{"name": store.String("Core"), "owner": store.String(person.ID)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := application.Local().Create(context.Background(), "pages", store.Values{
		"title": store.String("Nested"),
		"meta":  store.Object(store.Values{"reviewer": store.String(person.ID)}),
		"sections": store.List(store.Object(store.Values{
			"_key": store.String("section-one"), "reviewer": store.String(person.ID),
		})),
		"layout": store.List(store.Object(store.Values{
			"_key": store.String("quote-one"), "blockType": store.String("quote"), "source": store.String(team.ID),
		})),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	result := graphQL(t, server.URL, `query($id: ID!) {
  Page(id: $id) {
    meta { reviewer { id name secret } }
    sections { reviewer { id name secret } }
    layout {
      ... on PageLayoutBlockQuote {
        source { id name owner { id name secret } }
      }
    }
  }
}`, map[string]interface{}{"id": page.ID})
	resolved := objectAt(t, result, "data", "Page")
	meta := resolved["meta"].(map[string]interface{})
	reviewer := meta["reviewer"].(map[string]interface{})
	if reviewer["id"] != person.ID || reviewer["secret"] != nil {
		t.Fatalf("GraphQL group relationship = %#v", reviewer)
	}
	sections := resolved["sections"].([]interface{})
	sectionReviewer := objectAt(t, sections[0].(map[string]interface{}), "reviewer")
	if sectionReviewer["id"] != person.ID || sectionReviewer["secret"] != nil {
		t.Fatalf("GraphQL array relationship = %#v", sectionReviewer)
	}
	blocks := resolved["layout"].([]interface{})
	source := objectAt(t, blocks[0].(map[string]interface{}), "source")
	if source["id"] != team.ID {
		t.Fatalf("GraphQL block relationship = %#v", source)
	}
	owner := objectAt(t, source, "owner")
	if owner["id"] != person.ID || owner["secret"] != nil {
		t.Fatalf("GraphQL nested depth relationship = %#v", owner)
	}
}

func TestGraphQLPolymorphicRelationshipsUseTypedWrappers(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL polymorphic relationships", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{
			{Slug: "categories", Fields: field.Fields{
				field.Text("name").Required(),
			}},
			{Slug: "authors", Fields: field.Fields{
				field.Text("name").Required(),
			}},
			{Slug: "posts", Fields: field.Fields{
				field.Text("title").Required(),
				field.PolymorphicRelationship("owner", "categories", "authors"),
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	category := graphQL(t, server.URL, `mutation { createCategory(data: {name: "News"}) { id } }`)
	categoryID := objectAt(t, category, "data", "createCategory")["id"].(string)
	post := graphQL(t, server.URL, `mutation($owner: ID!) { createPost(data: {title: "Story", owner: {relationTo: CATEGORIES, value: $owner}}) { id } }`, map[string]interface{}{"owner": categoryID})
	postID := objectAt(t, post, "data", "createPost")["id"].(string)
	result := graphQL(t, server.URL, `query($id: ID!) {
  Post(id: $id) {
    owner {
      relationTo
      value { __typename ... on Category { id name } ... on Author { id name } }
    }
  }
}`, map[string]interface{}{"id": postID})
	owner := objectAt(t, result, "data", "Post", "owner")
	value := owner["value"].(map[string]interface{})
	if owner["relationTo"] != "CATEGORIES" || value["__typename"] != "Category" || value["id"] != categoryID || value["name"] != "News" {
		t.Fatalf("polymorphic owner = %#v", owner)
	}
}

func TestGraphQLJoinsExposeRedactedTargetDocuments(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL joins", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{
			{Slug: "categories", Fields: field.Fields{
				field.Text("name").Required(),
				field.Join("posts", "posts", "category").Limit(10),
			}},
			{Slug: "posts", Fields: field.Fields{
				field.Text("title").Required(),
				field.Text("privateNote").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
				field.Relationship("category", "categories").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	category := graphQL(t, server.URL, `mutation { createCategory(data: {name: "News"}) { id } }`)
	categoryID := objectAt(t, category, "data", "createCategory")["id"].(string)
	graphQL(t, server.URL, `mutation($category: ID!) { createPost(data: {title: "Story", privateNote: "redact", category: $category}) { id } }`, map[string]interface{}{"category": categoryID})
	result := graphQL(t, server.URL, `query($id: ID!) { Category(id: $id) { posts(limit: 5, count: true) { docs { title privateNote } totalDocs hasNextPage } } }`, map[string]interface{}{"id": categoryID})
	categoryResult := objectAt(t, result, "data", "Category")
	join := categoryResult["posts"].(map[string]interface{})
	posts := join["docs"].([]interface{})
	if len(posts) != 1 || posts[0].(map[string]interface{})["title"] != "Story" || posts[0].(map[string]interface{})["privateNote"] != nil {
		t.Fatalf("joined posts = %#v", join)
	}
	if join["totalDocs"] != float64(1) || join["hasNextPage"] != false {
		t.Fatalf("join pagination = %#v", join)
	}
	for _, predicate := range []string{`where: {privateNote: {contains: "redact"}}`, `sort: "privateNote"`, `where: {category: {equals: "` + categoryID + `"}}`} {
		denied := graphQL(t, server.URL, `query { Category(id: "`+categoryID+`") { posts(count: true, `+predicate+`) { docs { title } totalDocs } } }`)
		if strings.HasPrefix(predicate, "where:") {
			assertPrivateWhereRejected(t, denied)
		} else {
			assertErrorCode(t, denied, "field_access_denied")
		}
	}
	filtered := graphQL(t, server.URL, `query { Category(id: "`+categoryID+`") { posts(count: true, where: {title: {equals: "Story"}}, sort: "title") { docs { title category { id } } totalDocs } } }`)
	if objectAt(t, filtered, "data", "Category", "posts")["totalDocs"] != float64(1) {
		t.Fatalf("authorized configured join with caller filter = %#v", filtered)
	}
}

func TestGraphQLRejectsPrivateFieldQueryOracles(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Private GraphQL queries", Plugins: []ridu.Plugin{graphqlplugin.New()}, Collections: []ridu.Collection{{
		Slug: "employees", Fields: field.Fields{
			field.Text("name"),
			field.Number("salary").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
			field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	graphQL(t, server.URL, `mutation { createEmployee(data: {name: "Alice", salary: 73500, secret: "sapphire"}) { id } }`)
	for _, text := range []string{
		`{ Employees(where: {salary: {greater_than: 70000}}) { docs { name } totalDocs } }`,
		`{ countEmployees(where: {salary: {less_than: 75000}}) { totalDocs } }`,
		`{ countEmployees(where: {secret: {contains: "app"}}) { totalDocs } }`,
		`{ Employees(sort: "salary") { docs { name } } }`,
		`{ Employees(sort: "-salary") { docs { name } } }`,
	} {
		result := graphQL(t, server.URL, text)
		if strings.Contains(text, "where:") {
			assertPrivateWhereRejected(t, result)
		} else {
			assertErrorCode(t, result, "field_access_denied")
		}
	}
}

func assertPrivateWhereRejected(t *testing.T, result map[string]interface{}) {
	t.Helper()
	errors, _ := result["errors"].([]interface{})
	if len(errors) == 0 || !strings.Contains(fmt.Sprint(errors), "Unknown field") {
		t.Fatalf("protected field was exposed in GraphQL filtering input: %#v", result)
	}
}

func TestGraphQLSelectionSkipsUnrequestedComputedOutput(t *testing.T) {
	computedCalls := 0
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL computed selection", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{
				field.Text("title"),
				field.Virtual("label", field.ValueString, func(ctx operation.Context) (operation.Value[store.Value], error) {
					computedCalls++
					titleValue := ctx.Root.Get("title")
					title, _ := titleValue.StringValue()
					return operation.Present(store.String("Label: " + title)), nil
				}),
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	created := graphQL(t, server.URL, `mutation { createPost(data: {title: "Story"}) { id } }`)
	id := objectAt(t, created, "data", "createPost")["id"].(string)
	graphQL(t, server.URL, `query($id: ID!) { Post(id: $id) { title } }`, map[string]interface{}{"id": id})
	if computedCalls != 0 {
		t.Fatalf("unselected computed resolver calls = %d", computedCalls)
	}

	result := graphQL(t, server.URL, `query($id: ID!) { Post(id: $id) { ...PostLabel } } fragment PostLabel on Post { label }`, map[string]interface{}{"id": id})
	if objectAt(t, result, "data", "Post")["label"] != "Label: Story" || computedCalls != 1 {
		t.Fatalf("selected computed result = %#v, calls = %d", result, computedCalls)
	}
}

func TestGraphQLLocalizationAliasesAndOrderedFallback(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL localization", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "es", Label: "Spanish"}, {Code: "pt", Label: "Portuguese"},
		}},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title").Required().Localized(),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	created := graphQL(t, server.URL, `mutation { createPost(locale: EN, data: {title: "Hello"}) { id } }`)
	id := objectAt(t, created, "data", "createPost")["id"].(string)
	graphQL(t, server.URL, `mutation($id: ID!) { updatePost(id: $id, locale: ES, data: {title: "Hola"}) { title } }`, map[string]interface{}{"id": id})
	result := graphQL(t, server.URL, `query($id: ID!) {
  english: Post(id: $id, locale: EN) { title }
  spanish: Post(id: $id, locale: ES) { title }
  fallback: Post(id: $id, locale: PT, fallbackLocale: [ES, EN]) { title }
}`, map[string]interface{}{"id": id})
	data := objectAt(t, result, "data")
	if data["english"].(map[string]interface{})["title"] != "Hello" || data["spanish"].(map[string]interface{})["title"] != "Hola" || data["fallback"].(map[string]interface{})["title"] != "Hola" {
		t.Fatalf("localized aliases = %#v", data)
	}
}

func TestGraphQLAuthenticationUsesRiduSessionsAndActorCollection(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL auth", Plugins: []ridu.Plugin{graphqlplugin.New()}, Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}, MaxLoginAttempts: 2}, Access: ridu.CollectionAccess{Update: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }}, Fields: field.Fields{
				field.Email("email").Required().Unique(),
				field.Text("name"),
			}},
			{Slug: "staff", Auth: true, Fields: field.Fields{
				field.Email("email").Required().Unique(),
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	initialization := graphQL(t, server.URL, `{ initializedUser initializedStaff }`)
	if objectAt(t, initialization, "data")["initializedUser"] != false || objectAt(t, initialization, "data")["initializedStaff"] != false {
		t.Fatalf("initial auth state = %#v", initialization)
	}
	secondaryBootstrap := graphQL(t, server.URL, `mutation { createStaff(data: {email: "staff@example.com", password: "correct horse battery staple"}) { id } }`)
	assertErrorCode(t, secondaryBootstrap, "access_denied")
	graphQL(t, server.URL, `mutation { createUser(data: {email: "user@example.com", name: "Ada", password: "correct horse battery staple"}) { id email } }`)
	initialization = graphQL(t, server.URL, `{ initializedUser initializedStaff }`)
	if objectAt(t, initialization, "data")["initializedUser"] != true || objectAt(t, initialization, "data")["initializedStaff"] != false {
		t.Fatalf("initialized auth state = %#v", initialization)
	}
	login := graphQL(t, server.URL, `mutation { loginUser(email: "user@example.com", password: "correct horse battery staple") { token collection expiresAt user { id email name } } }`)
	session := objectAt(t, login, "data", "loginUser")
	token, _ := session["token"].(string)
	if token == "" || session["collection"] != "users" {
		t.Fatalf("login session = %#v", session)
	}
	authenticatedCreate := graphQLWithToken(t, server.URL, token, `mutation { createUser(data: {email: "second@example.com", name: "Grace", password: "correct horse battery staple"}) { id email } }`)
	if objectAt(t, authenticatedCreate, "data", "createUser")["email"] != "second@example.com" {
		t.Fatalf("authenticated auth create = %#v", authenticatedCreate)
	}
	deniedCreate := graphQL(t, server.URL, `mutation { createUser(data: {email: "third@example.com", name: "Lin", password: "correct horse battery staple"}) { id } }`)
	assertErrorCode(t, deniedCreate, "access_denied")
	preference := graphQLWithToken(t, server.URL, token, `mutation { setPreference(key: "theme", value: {mode: "dark"}) }`)
	if objectAt(t, preference, "data")["setPreference"].(map[string]interface{})["mode"] != "dark" {
		t.Fatalf("set preference = %#v", preference)
	}
	preference = graphQLWithToken(t, server.URL, token, `{ preference(key: "theme") }`)
	if objectAt(t, preference, "data")["preference"].(map[string]interface{})["mode"] != "dark" {
		t.Fatalf("preference = %#v", preference)
	}
	anonymousPreference := graphQL(t, server.URL, `{ preference(key: "theme") }`)
	assertErrorCode(t, anonymousPreference, "access_denied")
	me := graphQLWithToken(t, server.URL, token, `{ meUser { collection user { email name } } meStaff { collection user { id } } }`)
	user := objectAt(t, me, "data", "meUser", "user")
	if user["email"] != "user@example.com" || user["name"] != "Ada" {
		t.Fatalf("me user = %#v", user)
	}
	staff := objectAt(t, me, "data", "meStaff")
	if staff["user"] != nil {
		t.Fatalf("user token crossed auth collection: %#v", staff)
	}
	refreshed := graphQLWithToken(t, server.URL, token, `mutation { refreshUser { token user { email } } }`)
	newToken, _ := objectAt(t, refreshed, "data", "refreshUser")["token"].(string)
	if newToken == "" || newToken == token {
		t.Fatalf("refreshed token = %q", newToken)
	}
	logout := graphQLWithToken(t, server.URL, newToken, `mutation { logoutUser }`)
	if objectAt(t, logout, "data")["logoutUser"] != true {
		t.Fatalf("logout = %#v", logout)
	}
	graphQL(t, server.URL, `mutation { loginUser(email: "user@example.com", password: "wrong password value") { token } }`)
	graphQL(t, server.URL, `mutation { loginUser(email: "user@example.com", password: "wrong password value") { token } }`)
	locked := graphQL(t, server.URL, `mutation { loginUser(email: "user@example.com", password: "correct horse battery staple") { token } }`)
	assertErrorCode(t, locked, "access_denied")
	unlocked := graphQL(t, server.URL, `mutation { unlockUser(email: "user@example.com") }`)
	if objectAt(t, unlocked, "data")["unlockUser"] != true {
		t.Fatalf("unlock = %#v", unlocked)
	}
	login = graphQL(t, server.URL, `mutation { loginUser(email: "user@example.com", password: "correct horse battery staple") { token } }`)
	if objectAt(t, login, "data", "loginUser")["token"] == "" {
		t.Fatalf("login after unlock = %#v", login)
	}
}

func TestGraphQLPreferencesPreserveExactAdminIdentityWithCollidingAuthIDs(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL exact preference identity", Plugins: []ridu.Plugin{graphqlplugin.New()}, Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: field.Fields{
				field.Email("email").Required().Unique(),
			}},
			{Slug: "staff", Auth: true, Fields: field.Fields{
				field.Email("email").Required().Unique(),
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	user, err := application.Local().Import(ctx, "users", store.Values{"email": store.String("user@example.test")}, ridu.ImportOptions{ID: "shared-auth-id", Status: store.StatusPublished})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Import(ctx, "staff", store.Values{"email": store.String("staff@example.test")}, ridu.ImportOptions{ID: user.ID, Status: store.StatusPublished}); err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(ctx, "users", user.ID, "users-password-value"); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(ctx, "users", "user@example.test", "users-password-value")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	result := graphQLWithToken(t, server.URL, session.Token, `mutation { setPreference(key: "theme", value: "dark") }`)
	if objectAt(t, result, "data")["setPreference"] != "dark" {
		t.Fatalf("set colliding-ID preference = %#v", result)
	}
	result = graphQLWithToken(t, server.URL, session.Token, `{ preference(key: "theme") }`)
	if objectAt(t, result, "data")["preference"] != "dark" {
		t.Fatalf("read colliding-ID preference = %#v", result)
	}
	result = graphQLWithToken(t, server.URL, session.Token, `mutation { deletePreference(key: "theme") }`)
	if objectAt(t, result, "data")["deletePreference"] != true {
		t.Fatalf("delete colliding-ID preference = %#v", result)
	}
	result = graphQLWithToken(t, server.URL, session.Token, `{ preference(key: "theme") }`)
	if objectAt(t, result, "data")["preference"] != nil {
		t.Fatalf("deleted colliding-ID preference = %#v", result)
	}
	result = graphQLWithToken(t, server.URL, session.Token, `mutation { first: setPreference(key: "locale", value: "en") second: setPreference(key: "density", value: "compact") }`)
	if data := objectAt(t, result, "data"); data["first"] != "en" || data["second"] != "compact" {
		t.Fatalf("set preferences before exact reset = %#v", result)
	}
	result = graphQLWithToken(t, server.URL, session.Token, `mutation { resetPreferences }`)
	if objectAt(t, result, "data")["resetPreferences"] != true {
		t.Fatalf("reset colliding-ID preferences = %#v", result)
	}
	result = graphQLWithToken(t, server.URL, session.Token, `{ locale: preference(key: "locale") density: preference(key: "density") }`)
	if data := objectAt(t, result, "data"); data["locale"] != nil || data["density"] != nil {
		t.Fatalf("preferences after exact reset = %#v", result)
	}
}

func TestGraphQLAndRESTLoginRecordTrustedTransportMetadata(t *testing.T) {
	var hooked []ridu.AuthContext
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL login transport metadata", Plugins: []ridu.Plugin{graphqlplugin.New()}, Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{
				Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
				Hooks: ridu.AuthHooks{BeforeLogin: []ridu.AuthHook{func(ctx ridu.AuthContext) error {
					hooked = append(hooked, ctx)
					return nil
				}}},
			},
			Fields: field.Fields{
				field.Email("email").Required().Unique(),
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	user, err := application.Local().Import(ctx, "users", store.Values{"email": store.String("metadata@example.test")}, ridu.ImportOptions{ID: "metadata-user", Status: store.StatusPublished})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(ctx, "users", user.ID, "metadata-password-value"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{TrustedProxyCIDRs: []string{"127.0.0.0/8"}}))
	defer server.Close()

	restRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/users/login", strings.NewReader(`{"email":"metadata@example.test","password":"metadata-password-value"}`))
	if err != nil {
		t.Fatal(err)
	}
	restRequest.Header.Set("Content-Type", "application/json")
	restRequest.Header.Set("X-Forwarded-For", "198.51.100.10")
	restRequest.Header.Set("User-Agent", "Ridu REST metadata test")
	restResponse, err := http.DefaultClient.Do(restRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = restResponse.Body.Close()
	if restResponse.StatusCode != http.StatusOK {
		t.Fatalf("REST login status = %d", restResponse.StatusCode)
	}

	result := graphQLWithHeaders(t, server.URL, `mutation { loginUser(email: "metadata@example.test", password: "metadata-password-value") { token } }`, http.Header{
		"X-Forwarded-For": []string{"198.51.100.11"},
		"User-Agent":      []string{"Ridu GraphQL metadata test"},
	})
	graphQLToken, _ := objectAt(t, result, "data", "loginUser")["token"].(string)
	if graphQLToken == "" {
		t.Fatalf("GraphQL login = %#v", result)
	}
	if len(hooked) != 2 || hooked[0].IPAddress != "198.51.100.10" || hooked[0].UserAgent != "Ridu REST metadata test" || hooked[1].IPAddress != "198.51.100.11" || hooked[1].UserAgent != "Ridu GraphQL metadata test" {
		t.Fatalf("REST/GraphQL login hook metadata = %#v", hooked)
	}
	sessions, err := application.Sessions(ctx, graphQLToken)
	if err != nil {
		t.Fatal(err)
	}
	var graphQLSessionFound bool
	for _, session := range sessions {
		if session.IPAddress == "198.51.100.11" && session.UserAgent == "Ridu GraphQL metadata test" {
			graphQLSessionFound = true
		}
	}
	if !graphQLSessionFound {
		t.Fatalf("GraphQL session metadata = %#v", sessions)
	}

	untrustedServer := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer untrustedServer.Close()
	untrusted := graphQLWithHeaders(t, untrustedServer.URL, `mutation { loginUser(email: "metadata@example.test", password: "metadata-password-value") { token } }`, http.Header{
		"X-Forwarded-For": []string{"203.0.113.99"},
		"User-Agent":      []string{"Ridu untrusted proxy test"},
	})
	if token, _ := objectAt(t, untrusted, "data", "loginUser")["token"].(string); token == "" {
		t.Fatalf("untrusted-proxy GraphQL login = %#v", untrusted)
	}
	if len(hooked) != 3 || hooked[2].IPAddress == "203.0.113.99" || hooked[2].IPAddress == "" || hooked[2].UserAgent != "Ridu untrusted proxy test" {
		t.Fatalf("untrusted forwarding metadata = %#v", hooked)
	}
}

func TestGraphQLVersionsDraftsAndTrashUseRiduLifecycleOperations(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL lifecycle", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{
			Slug: "posts", Versions: true, Trash: true,
			VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10},
			Fields: field.Fields{
				field.Text("title").Required(),
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	created := graphQL(t, server.URL, `mutation { createPost(data: {title: "First"}) { id _revision _status } }`)
	post := objectAt(t, created, "data", "createPost")
	id := post["id"].(string)
	hiddenDraft := graphQL(t, server.URL, `query($id: ID!) { Post(id: $id) { id } }`, map[string]interface{}{"id": id})
	assertErrorCode(t, hiddenDraft, "not_found")
	visibleDraft := graphQL(t, server.URL, `query($id: ID!) { Post(id: $id, draft: true) { id _status } }`, map[string]interface{}{"id": id})
	if objectAt(t, visibleDraft, "data", "Post")["_status"] != "draft" {
		t.Fatalf("explicit draft read = %#v", visibleDraft)
	}
	publishedCreate := graphQL(t, server.URL, `mutation { createPost(draft: false, data: {title: "Published immediately"}) { id _status } }`)
	publishedID := objectAt(t, publishedCreate, "data", "createPost")["id"].(string)
	publishedRead := graphQL(t, server.URL, `query($id: ID!) { Post(id: $id) { id _status } }`, map[string]interface{}{"id": publishedID})
	if objectAt(t, publishedRead, "data", "Post")["_status"] != "published" {
		t.Fatalf("explicit published create = %#v", publishedRead)
	}
	graphQL(t, server.URL, `mutation($id: ID!) { updatePost(id: $id, data: {title: "Second"}) { _revision } }`, map[string]interface{}{"id": id})
	versions := graphQL(t, server.URL, `query($id: ID!) { versionsPosts(id: $id, limit: 10) { docs { revision snapshot { title } } totalDocs } versionPost(id: $id, revision: 1) { revision snapshot { title } } }`, map[string]interface{}{"id": id})
	page := objectAt(t, versions, "data", "versionsPosts")
	if page["totalDocs"].(float64) < 1 || objectAt(t, versions, "data", "versionPost")["revision"] != float64(1) {
		t.Fatalf("versions = %#v", versions)
	}
	restored := graphQL(t, server.URL, `mutation($id: ID!) { restoreVersionPost(id: $id, revision: 1, draft: true) { title _status } }`, map[string]interface{}{"id": id})
	if objectAt(t, restored, "data", "restoreVersionPost")["title"] != "First" {
		t.Fatalf("restored = %#v", restored)
	}
	published := graphQL(t, server.URL, `mutation($id: ID!) { publishPost(id: $id) { _status } }`, map[string]interface{}{"id": id})
	if objectAt(t, published, "data", "publishPost")["_status"] != "published" {
		t.Fatalf("published = %#v", published)
	}
	graphQL(t, server.URL, `mutation($id: ID!) { deletePost(id: $id) { id } }`, map[string]interface{}{"id": id})
	trashed := graphQL(t, server.URL, `query($id: ID!) { Post(id: $id, trash: true) { id title } }`, map[string]interface{}{"id": id})
	if objectAt(t, trashed, "data", "Post")["id"] != id {
		t.Fatalf("trashed = %#v", trashed)
	}
	restoredTrash := graphQL(t, server.URL, `mutation($id: ID!) { restoreDeletedPost(id: $id) { id } }`, map[string]interface{}{"id": id})
	if objectAt(t, restoredTrash, "data", "restoreDeletedPost")["id"] != id {
		t.Fatalf("restored trash = %#v", restoredTrash)
	}
}

func TestGraphQLSafeguardsRejectIntrospectionAliasesAndOversizedVariables(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL safeguards", Plugins: []ridu.Plugin{graphqlplugin.New(graphqlplugin.Options{MaxAliases: 1, MaxDepth: 4, MaxVariableBytes: 8})},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
			field.Relationship("parent", "posts"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	introspection := graphQL(t, server.URL, `{ __schema { queryType { name } } }`)
	assertErrorCode(t, introspection, "graphql_introspection_disabled")
	aliases := graphQL(t, server.URL, `{ a: Posts { totalDocs } b: Posts { totalDocs } }`)
	assertErrorCode(t, aliases, "graphql_aliases_exceeded")
	variables := graphQL(t, server.URL, `query($unused: String) { Posts { totalDocs } }`, map[string]interface{}{"unused": "too-large"})
	assertErrorCode(t, variables, "variables_too_large")
	depth := graphQL(t, server.URL, `{ Posts { docs { parent { parent { id } } } } }`)
	assertErrorCode(t, depth, "graphql_depth_exceeded")
	cycle := graphQL(t, server.URL, `{ Posts { ...A } } fragment A on PostsPage { ...B } fragment B on PostsPage { ...A }`)
	assertErrorCode(t, cycle, "graphql_fragment_cycle")

	for _, test := range []struct {
		name, body, code string
	}{
		{name: "duplicate JSON key", body: `{"query":"{ Posts { totalDocs } }","query":"{ Posts { docs { id } } }"}`, code: "bad_request"},
		{name: "deep JSON variables", body: `{"query":"query($value: String) { Posts { totalDocs } }","variables":{"value":` + strings.Repeat("[", 130) + `null` + strings.Repeat("]", 130) + `}}`, code: "graphql_json_complexity_exceeded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, requestError := http.NewRequest(http.MethodPost, server.URL+"/api/graphql", strings.NewReader(test.body))
			if requestError != nil {
				t.Fatal(requestError)
			}
			request.Header.Set("Content-Type", "application/json")
			response, responseError := http.DefaultClient.Do(request)
			if responseError != nil {
				t.Fatal(responseError)
			}
			defer response.Body.Close()
			var payload map[string]interface{}
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			assertErrorCode(t, payload, test.code)
		})
	}

	response, err := http.Get(server.URL + "/api/graphql")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d", response.StatusCode)
	}
}

func TestGraphQLAuthAliasesShareDistributedRESTAdmission(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL auth admission", Plugins: []ridu.Plugin{graphqlplugin.New(graphqlplugin.Options{MaxAliases: 4})}, Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{
				Password:      ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
				PasswordReset: ridu.PasswordResetConfig{Send: func(context.Context, ridu.PasswordResetNotification) error { return nil }},
				Verify:        &ridu.VerifyEmailConfig{Send: func(context.Context, ridu.VerifyEmailNotification) error { return nil }},
			},
			Fields: field.Fields{
				field.Email("email").Required().Unique(),
			},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	options := ridu.HandlerOptions{AuthRateLimit: 2, AuthRateWindow: time.Hour}
	restServer := httptest.NewServer(application.Handler(options))
	defer restServer.Close()
	graphQLServer := httptest.NewServer(application.Handler(options))
	defer graphQLServer.Close()

	request, err := http.NewRequest(http.MethodPost, restServer.URL+"/api/auth/users/login", strings.NewReader(`{"email":"rotated-one@example.test","password":"wrong password value"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("REST attempt status = %d, want 401", response.StatusCode)
	}

	result := graphQL(t, graphQLServer.URL, `mutation {
  first: loginUser(email: "rotated-two@example.test", password: "wrong password value") { token }
  second: loginUser(email: "rotated-three@example.test", password: "wrong password value") { token }
}`)
	failures, _ := result["errors"].([]interface{})
	if len(failures) != 2 {
		t.Fatalf("alias failures = %#v", result)
	}
	seenRateLimit := false
	for _, failure := range failures {
		encoded, _ := failure.(map[string]interface{})
		extensions, _ := encoded["extensions"].(map[string]interface{})
		if extensions["code"] == "rate_limited" && extensions["status"] == float64(http.StatusTooManyRequests) {
			seenRateLimit = true
		}
	}
	if !seenRateLimit {
		t.Fatalf("GraphQL aliases bypassed shared IP admission: %#v", result)
	}
	for name, mutation := range map[string]string{
		"forgot": `mutation {
  first: forgotPasswordUser(email: "one@example.test")
  second: forgotPasswordUser(email: "two@example.test")
  third: forgotPasswordUser(email: "three@example.test")
}`,
		"resend": `mutation {
  first: resendVerificationUser(email: "one@example.test")
  second: resendVerificationUser(email: "two@example.test")
  third: resendVerificationUser(email: "three@example.test")
}`,
	} {
		result := graphQL(t, graphQLServer.URL, mutation)
		failures, _ := result["errors"].([]interface{})
		if len(failures) != 1 {
			t.Fatalf("%s alias admission failures = %#v", name, result)
		}
		encoded, _ := failures[0].(map[string]interface{})
		extensions, _ := encoded["extensions"].(map[string]interface{})
		if extensions["code"] != "rate_limited" || extensions["status"] != float64(http.StatusTooManyRequests) {
			t.Fatalf("%s alias admission error = %#v", name, result)
		}
	}
}

func TestGraphQLParserAndFragmentAnalysisRejectHostileDocumentsBeforeValidation(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL parser safeguards", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	deep := "query " + strings.Repeat("{ x ", 512) + strings.Repeat("}", 512)
	assertErrorCode(t, graphQL(t, server.URL, deep), "graphql_document_depth_exceeded")

	var chain strings.Builder
	chain.WriteString(`{ Posts { ...F0 } }`)
	const fragmentCount = 96
	for index := 0; index < fragmentCount; index++ {
		fmt.Fprintf(&chain, " fragment F%d on PostsPage { ", index)
		if index+1 < fragmentCount {
			fmt.Fprintf(&chain, "...F%d", index+1)
		} else {
			chain.WriteString("totalDocs")
		}
		chain.WriteString(" }")
	}
	assertErrorCode(t, graphQL(t, server.URL, chain.String()), "graphql_fragment_depth_exceeded")

	// Delimiter-like bytes in strings and comments are data, not parser depth.
	stringAndComment := "query {\n# " + strings.Repeat("}]}", 256) + "\nPosts(where: {title: {equals: \"" + strings.Repeat("{[", 256) + "\"}}) { totalDocs }\n}"
	result := graphQL(t, server.URL, stringAndComment)
	if objectAt(t, result, "data", "Posts")["totalDocs"] != float64(0) {
		t.Fatalf("string/comment-aware query = %#v", result)
	}
	blockString := `query { Posts(where: {title: {equals: """` + strings.Repeat("{[", 256) + `"""}}) { totalDocs } }`
	result = graphQL(t, server.URL, blockString)
	if objectAt(t, result, "data", "Posts")["totalDocs"] != float64(0) {
		t.Fatalf("block-string-aware query = %#v", result)
	}
}

func TestGraphQLFragmentExpansionAndTokenBudgetsAreIndependentOfFieldComplexity(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL document budgets", Plugins: []ridu.Plugin{graphqlplugin.New(graphqlplugin.Options{MaxComplexity: 100_000})},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	var expansion strings.Builder
	expansion.WriteString(`{ Posts { ...F0 } }`)
	const expansionLevels = 16
	for index := 0; index < expansionLevels; index++ {
		fmt.Fprintf(&expansion, " fragment F%d on PostsPage { ", index)
		if index+1 < expansionLevels {
			fmt.Fprintf(&expansion, "...F%d ...F%d", index+1, index+1)
		} else {
			expansion.WriteString("totalDocs")
		}
		expansion.WriteString(" }")
	}
	assertErrorCode(t, graphQL(t, server.URL, expansion.String()), "graphql_fragment_expansions_exceeded")

	limitedApplication, err := ridu.New(ridu.Config{
		Name: "GraphQL token budget", Plugins: []ridu.Plugin{graphqlplugin.New(graphqlplugin.Options{MaxComplexity: 1})},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	limitedServer := httptest.NewServer(limitedApplication.Handler(ridu.HandlerOptions{}))
	defer limitedServer.Close()
	tokenHeavy := "query { " + strings.Repeat("x ", 7_000) + "}"
	assertErrorCode(t, graphQL(t, limitedServer.URL, tokenHeavy), "graphql_document_complexity_exceeded")
}

func TestGraphQLImmutableSchemaServesConcurrentRequests(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL concurrency", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	const workers = 32
	const requestsPerWorker = 10
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for requestIndex := 0; requestIndex < requestsPerWorker; requestIndex++ {
				request, requestError := http.NewRequest(http.MethodPost, server.URL+"/api/graphql", strings.NewReader(`{"query":"{ Posts(limit: 1) { totalDocs } }"}`))
				if requestError != nil {
					errorsFound <- requestError
					return
				}
				request.Header.Set("Content-Type", "application/json")
				response, responseError := http.DefaultClient.Do(request)
				if responseError != nil {
					errorsFound <- responseError
					return
				}
				body, readError := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if readError != nil || response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"totalDocs":0`)) {
					errorsFound <- fmt.Errorf("concurrent response status=%d body=%s read=%v", response.StatusCode, body, readError)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for requestError := range errorsFound {
		t.Fatal(requestError)
	}
}

func TestGraphQLHTTPAndDefaultListComplexityAreBounded(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL HTTP safeguards", Plugins: []ridu.Plugin{graphqlplugin.New(graphqlplugin.Options{MaxBodyBytes: 128, MaxComplexity: 5})},
		Collections: []ridu.Collection{
			{Slug: "categories", Fields: field.Fields{
				field.Text("name"),
				field.Join("posts", "posts", "category"),
			}},
			{Slug: "posts", Fields: field.Fields{
				field.Text("title"),
				field.Relationship("category", "categories"),
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	complexity := graphQL(t, server.URL, `{ Posts { docs { id title } totalDocs } }`)
	assertErrorCode(t, complexity, "graphql_complexity_exceeded")
	nestedComplexity := graphQL(t, server.URL, `{ Category(id: "missing") { posts { docs { title } } } }`)
	assertErrorCode(t, nestedComplexity, "graphql_complexity_exceeded")

	oversized := append([]byte(`{"query":"`), bytes.Repeat([]byte("x"), 256)...)
	oversized = append(oversized, []byte(`"}`)...)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/graphql", bytes.NewReader(oversized))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d", response.StatusCode)
	}

	request, err = http.NewRequest(http.MethodPost, server.URL+"/api/graphql", strings.NewReader(`{"query":"{ Posts { totalDocs } }"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "text/plain")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("unsupported media status = %d", response.StatusCode)
	}

	request, err = http.NewRequest(http.MethodPost, server.URL+"/api/graphql", strings.NewReader(`{"query":"{ Posts { totalDocs } }"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/graphql-response+json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/graphql-response+json") {
		t.Fatalf("response content type = %q", got)
	}
}

func TestGraphQLNonPositiveListLimitCannotBypassMaximum(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL list cap", Plugins: []ridu.Plugin{graphqlplugin.New(graphqlplugin.Options{MaxListLimit: 3})},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	result := graphQL(t, server.URL, `{ Posts(limit: 0) { limit } }`)
	if objectAt(t, result, "data", "Posts")["limit"] != float64(3) {
		t.Fatalf("bounded zero limit = %#v", result)
	}
}

func TestGraphQLRunsConfiguredValidationRules(t *testing.T) {
	customRule := func(context *enginegraphql.ValidationContext) *enginegraphql.ValidationRuleInstance {
		context.ReportError(errors.New("query rejected by application policy"))
		return enginegraphql.FieldsOnCorrectTypeRule(context)
	}
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL validation rules", Plugins: []ridu.Plugin{graphqlplugin.New(graphqlplugin.Options{ValidationRules: []enginegraphql.ValidationRuleFn{customRule}})},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	result := graphQL(t, server.URL, `{ Posts { totalDocs } }`)
	failures, _ := result["errors"].([]interface{})
	if len(failures) == 0 || !strings.Contains(failures[0].(map[string]interface{})["message"].(string), "application policy") {
		t.Fatalf("custom validation result = %#v", result)
	}
}

func TestGraphQLSDLIsDeterministicWithoutStartingATransport(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "GraphQL SDL",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title").Required(),
			field.Number("score"),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := graphqlplugin.GenerateSDL(manifest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := graphqlplugin.GenerateSDL(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("generated SDL changed between identical manifests")
	}
	if !strings.HasSuffix(first, "}\n") || strings.HasSuffix(first, "}\n\n") {
		t.Fatalf("generated SDL must end with exactly one newline: %q", first[len(first)-3:])
	}
	for _, expected := range []string{"type Query {", "Posts(", "createPost(", "input PostCreateInput", "title: String!"} {
		if !strings.Contains(first, expected) {
			t.Fatalf("SDL missing %q:\n%s", expected, first)
		}
	}
	for _, line := range strings.Split(first, "\n") {
		if strings.Contains(line, "createPost(") && strings.Contains(line, "draft:") {
			t.Fatalf("unversioned create mutation advertises draft intent: %s", line)
		}
	}
}

func TestGraphQLGenerationProviderUsesExactCompiledOptions(t *testing.T) {
	options := graphqlplugin.Options{
		Resources: map[string]graphqlplugin.ResourceOptions{
			"posts": {SingularName: "Article", PluralName: "Articles", DisableMutations: true},
		},
		Queries: []graphqlplugin.ExtensionField{{
			Name: "health", Type: enginegraphql.NewNonNull(enginegraphql.Boolean), Cost: 1,
			Resolve: func(graphqlplugin.ExtensionContext) (interface{}, error) { return true, nil },
		}},
	}
	compiled := graphqlplugin.New(options)
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Exact GraphQL generation", Plugins: []ridu.Plugin{compiled},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := compiled.(ridu.GenerationProvider)
	if !ok {
		t.Fatal("compiled GraphQL plugin does not provide generated artifacts")
	}
	artifacts, err := provider.GeneratedArtifacts(ridu.PluginGenerationContext{Manifest: manifest})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].Name != "schema" {
		t.Fatalf("GraphQL generated artifacts = %#v", artifacts)
	}
	expected, err := graphqlplugin.GenerateSDL(manifest, options)
	if err != nil {
		t.Fatal(err)
	}
	actual := string(artifacts[0].Content)
	if actual != expected || !strings.Contains(actual, "Articles(") || !strings.Contains(actual, "health: Boolean!") || strings.Contains(actual, "createArticle") || strings.Contains(actual, "createPost") {
		t.Fatalf("compiled GraphQL artifact did not preserve exact options:\n%s", actual)
	}
	_, err = graphqlplugin.GenerateSDL(manifest, graphqlplugin.Options{Resources: map[string]graphqlplugin.ResourceOptions{"typo": {DisableQueries: true}}})
	if err == nil || !strings.Contains(err.Error(), "unknown slug") {
		t.Fatalf("unknown resource options error = %v", err)
	}
}

func TestGraphQLSDLIncludesInterfacesAndArgumentDefaults(t *testing.T) {
	var article *enginegraphql.Object
	node := enginegraphql.NewInterface(enginegraphql.InterfaceConfig{
		Name: "Node",
		Fields: enginegraphql.Fields{
			"id": {Type: enginegraphql.NewNonNull(enginegraphql.ID)},
		},
		ResolveType: func(enginegraphql.ResolveTypeParams) *enginegraphql.Object { return article },
	})
	article = enginegraphql.NewObject(enginegraphql.ObjectConfig{
		Name:        "Article",
		Description: "A published article.",
		Interfaces:  []*enginegraphql.Interface{node},
		Fields: enginegraphql.Fields{
			"id":    {Type: enginegraphql.NewNonNull(enginegraphql.ID)},
			"title": {Type: enginegraphql.String},
		},
	})
	preferences := enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{
		Name: "GreetingPreferences", Description: "Greeting presentation options.",
		Fields: enginegraphql.InputObjectConfigFieldMap{
			"tone": {Type: enginegraphql.String, DefaultValue: "warm", Description: "Desired greeting tone."},
		},
	})
	state := enginegraphql.NewEnum(enginegraphql.EnumConfig{
		Name: "ArticleState", Description: "Editorial state.",
		Values: enginegraphql.EnumValueConfigMap{
			"CURRENT": {Value: "current", Description: "Currently visible."},
			"LEGACY":  {Value: "legacy", DeprecationReason: "Use CURRENT."},
		},
	})
	options := graphqlplugin.Options{Queries: []graphqlplugin.ExtensionField{
		{
			Name: "node", Type: node, Cost: 1,
			Resolve: func(graphqlplugin.ExtensionContext) (interface{}, error) { return nil, nil },
		},
		{
			Name: "article", Type: article, Cost: 1,
			Resolve: func(graphqlplugin.ExtensionContext) (interface{}, error) { return nil, nil },
		},
		{
			Name: "greeting", Description: "Build a greeting.", Type: enginegraphql.String, Cost: 1,
			Args: enginegraphql.FieldConfigArgument{
				"enabled":     {Type: enginegraphql.Boolean, DefaultValue: false, Description: "Whether greeting is enabled."},
				"preferences": {Type: preferences, DefaultValue: map[string]string{"tone": "warm"}},
				"punctuation": {Type: enginegraphql.String, DefaultValue: "!"},
			},
			Resolve: func(graphqlplugin.ExtensionContext) (interface{}, error) { return "hello", nil },
		},
		{
			Name: "articleState", Type: state, Cost: 1,
			Resolve: func(graphqlplugin.ExtensionContext) (interface{}, error) { return "current", nil },
		},
	}}
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "SDL fidelity", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sdl, err := graphqlplugin.GenerateSDL(manifest, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"interface Node {", `"A published article."`, "type Article implements Node {",
		`"Build a greeting."`, `"Whether greeting is enabled."`, `enabled: Boolean = false`,
		`preferences: GreetingPreferences = {tone: "warm"}`, `punctuation: String = "!"`,
		`"Desired greeting tone."`, `tone: String = "warm"`, `LEGACY @deprecated(reason: "Use CURRENT.")`,
	} {
		if !strings.Contains(sdl, expected) {
			t.Fatalf("SDL missing %q:\n%s", expected, sdl)
		}
	}
	if _, err := parser.Parse(parser.ParseParams{Source: sdl}); err != nil {
		t.Fatalf("generated SDL does not parse: %v\n%s", err, sdl)
	}
}

func TestGraphQLSDLRejectsUnrepresentableScalarObjectDefault(t *testing.T) {
	metadata := enginegraphql.NewScalar(enginegraphql.ScalarConfig{
		Name:         "Metadata",
		Serialize:    func(value interface{}) interface{} { return value },
		ParseValue:   func(value interface{}) interface{} { return value },
		ParseLiteral: func(ast.Value) interface{} { return nil },
	})
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Invalid SDL default", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = graphqlplugin.GenerateSDL(manifest, graphqlplugin.Options{Queries: []graphqlplugin.ExtensionField{{
		Name: "metadata", Type: enginegraphql.String, Cost: 1,
		Args:    enginegraphql.FieldConfigArgument{"value": {Type: metadata, DefaultValue: map[string]string{"content-type": "json"}}},
		Resolve: func(graphqlplugin.ExtensionContext) (interface{}, error) { return nil, nil },
	}}})
	if err == nil || !strings.Contains(err.Error(), "not a GraphQL name") {
		t.Fatalf("invalid scalar object default error = %v", err)
	}
}

func TestGraphQLUploadMetadataInputsExcludeStorageOwnedFields(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "GraphQL uploads",
		Collections: []ridu.Collection{{
			Slug: "media", Upload: true,
			UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
			Fields: field.Fields{
				field.Text("caption"),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sdl, err := graphqlplugin.GenerateSDL(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"filename: String", "duplicateMedia(", "caption: String"} {
		if !strings.Contains(sdl, expected) {
			t.Fatalf("upload SDL missing %q:\n%s", expected, sdl)
		}
	}
	if strings.Contains(sdl, "createMedia(") || strings.Contains(sdl, "input MediaCreateInput") {
		t.Fatalf("upload SDL advertises JSON create without file bytes:\n%s", sdl)
	}
	for _, inputName := range []string{"MediaUpdateInput"} {
		start := strings.Index(sdl, "input "+inputName+" {")
		if start < 0 {
			t.Fatalf("upload SDL missing input %q:\n%s", inputName, sdl)
		}
		end := strings.Index(sdl[start:], "\n}\n")
		if end < 0 {
			t.Fatalf("upload SDL missing input %q:\n%s", inputName, sdl)
		}
		definition := sdl[start : start+end]
		for _, forbidden := range []string{"filename:", "mimeType:", "filesize:", "url:", "objectKey:", "sizes:"} {
			if strings.Contains(definition, forbidden) {
				t.Fatalf("upload input %q exposes server-owned metadata %q:\n%s", inputName, forbidden, definition)
			}
		}
	}
	emptyManifest, err := ridu.Resolve(ridu.Config{
		Name: "GraphQL metadata-only upload",
		Collections: []ridu.Collection{{
			Slug: "assets", Upload: true,
			UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	emptySDL, err := graphqlplugin.GenerateSDL(emptyManifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(emptySDL, "input AssetCreateInput") || strings.Contains(emptySDL, "input AssetUpdateInput") || strings.Contains(emptySDL, "createAsset(") || !strings.Contains(emptySDL, "duplicateAsset(id: ID!)") {
		t.Fatalf("metadata-only upload SDL contains an empty mutation input:\n%s", emptySDL)
	}
}

func TestGraphQLRejectsGeneratedNameCollisionsAtStartup(t *testing.T) {
	_, err := ridu.New(ridu.Config{
		Name: "GraphQL collisions", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Collections: []ridu.Collection{
			{Slug: "news-posts", Labels: ridu.CollectionLabels{Singular: "Post", Plural: "News Posts"}, Fields: field.Fields{
				field.Text("title"),
			}},
			{Slug: "blog-posts", Labels: ridu.CollectionLabels{Singular: "Post", Plural: "Blog Posts"}, Fields: field.Fields{
				field.Text("title"),
			}},
		},
	}, teststore.New())
	if err == nil || !strings.Contains(err.Error(), "duplicate GraphQL type name") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestGraphQLCompiledExtensionsReceiveOnlySafeRuntimeFacades(t *testing.T) {
	plugin := graphqlplugin.New(graphqlplugin.Options{Queries: []graphqlplugin.ExtensionField{{
		Name: "postTotal", Type: enginegraphql.NewNonNull(enginegraphql.Int), Cost: 5,
		Resolve: func(ctx graphqlplugin.ExtensionContext) (interface{}, error) {
			if ctx.Local == nil || ctx.App == nil || ctx.Actor == nil || ctx.ActorCollection != "users" {
				return nil, fmt.Errorf("extension did not receive the exact authenticated Ridu context")
			}
			page, err := ctx.Local.List(ctx.Context, "posts", ridu.ListOptions{
				Page: 1, Limit: 1, Actor: ctx.Actor, ActorCollection: ctx.ActorCollection,
			})
			return page.Total, err
		},
	}}})
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL extensions", Plugins: []ridu.Plugin{plugin}, Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}}, Fields: field.Fields{
				field.Email("email").Required().Unique(),
			}},
			{Slug: "posts", Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				if ctx.ActorCollection != "users" {
					return ridu.Deny(), nil
				}
				return ridu.Allow(), nil
			}}, Fields: field.Fields{
				field.Text("title").Required(),
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.CreateAuthUser(context.Background(), "users", store.Values{"email": store.String("extension@example.test")}, "extension-password", ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(context.Background(), "users", "extension@example.test", "extension-password")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	graphQL(t, server.URL, `mutation { createPost(data: {title: "One"}) { id } }`)
	result := graphQLWithToken(t, server.URL, session.Token, `{ postTotal }`)
	if objectAt(t, result, "data")["postTotal"] != float64(1) {
		t.Fatalf("extension result = %#v", result)
	}
}

func TestGraphQLInternalErrorsAreRedactedReportedAndObserved(t *testing.T) {
	secret := errors.New("database password must never reach GraphQL")
	plugin := graphqlplugin.New(graphqlplugin.Options{Queries: []graphqlplugin.ExtensionField{{
		Name: "explode", Type: enginegraphql.String,
		Resolve: func(graphqlplugin.ExtensionContext) (interface{}, error) { return nil, secret },
	}}})
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL diagnostics", Plugins: []ridu.Plugin{plugin},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic ridu.RequestErrorEvent
	var observation ridu.RequestObservation
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{
		RequestError: func(event ridu.RequestErrorEvent) { diagnostic = event },
		Observe:      func(event ridu.RequestObservation) { observation = event },
	}))
	defer server.Close()

	result := graphQL(t, server.URL, `{ explode }`)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret.Error()) || !strings.Contains(string(encoded), "internal server error") {
		t.Fatalf("GraphQL error body = %s", encoded)
	}
	if !errors.Is(diagnostic.Error, secret) || diagnostic.Path != "/api/graphql" {
		t.Fatalf("trusted diagnostic = %#v", diagnostic)
	}
	if observation.Status != http.StatusOK || observation.ErrorCode != "internal_error" {
		t.Fatalf("GraphQL observation = %#v", observation)
	}
}

func TestGraphQLPanicsAndSerializationFailuresNeverReachClients(t *testing.T) {
	panicSecret := "resolver panic contains a database password"
	scalarSecret := "scalar panic contains a signing key"
	panickingScalar := enginegraphql.NewScalar(enginegraphql.ScalarConfig{
		Name: "PanickingScalar",
		Serialize: func(interface{}) interface{} {
			panic(scalarSecret)
		},
	})
	unmarshalableScalar := enginegraphql.NewScalar(enginegraphql.ScalarConfig{
		Name:      "UnmarshalableScalar",
		Serialize: func(interface{}) interface{} { return make(chan int) },
	})
	plugin := graphqlplugin.New(graphqlplugin.Options{Queries: []graphqlplugin.ExtensionField{
		{Name: "resolverPanic", Type: enginegraphql.String, Resolve: func(graphqlplugin.ExtensionContext) (interface{}, error) {
			panic(panicSecret)
		}},
		{Name: "scalarPanic", Type: panickingScalar, Resolve: func(graphqlplugin.ExtensionContext) (interface{}, error) { return "value", nil }},
		{Name: "marshalFailure", Type: unmarshalableScalar, Resolve: func(graphqlplugin.ExtensionContext) (interface{}, error) { return "value", nil }},
	}})
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL panic redaction", Plugins: []ridu.Plugin{plugin},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics []ridu.RequestErrorEvent
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{RequestError: func(event ridu.RequestErrorEvent) {
		diagnostics = append(diagnostics, event)
	}}))
	defer server.Close()

	for _, test := range []struct {
		name, query, secret string
	}{{"resolver", `{ resolverPanic }`, panicSecret}, {"scalar", `{ scalarPanic }`, scalarSecret}} {
		t.Run(test.name, func(t *testing.T) {
			result := graphQL(t, server.URL, test.query)
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), test.secret) || !strings.Contains(string(encoded), `"code":"internal_error"`) {
				t.Fatalf("panic response = %s", encoded)
			}
		})
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/graphql", strings.NewReader(`{"query":"{ marshalFailure }"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusInternalServerError || !bytes.Contains(body, []byte(`"code":"internal_error"`)) || bytes.Contains(body, []byte("unsupported type")) {
		t.Fatalf("marshal failure response = %d: %s", response.StatusCode, body)
	}
	if len(diagnostics) != 3 {
		t.Fatalf("trusted diagnostics = %#v", diagnostics)
	}
}

func TestGraphQLTransportPropagatesItsConfiguredBodyLimit(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "GraphQL transport body limit", Plugins: []ridu.Plugin{graphqlplugin.New(graphqlplugin.Options{MaxBodyBytes: 512})},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{MaxBodyBytes: 32}))
	defer server.Close()
	result := graphQL(t, server.URL, `{ Posts(limit: 1) { totalDocs } }`)
	if objectAt(t, result, "data", "Posts")["totalDocs"] != float64(0) {
		t.Fatalf("GraphQL response = %#v", result)
	}
}

func graphQL(t *testing.T, serverURL, queryText string, variables ...map[string]interface{}) map[string]interface{} {
	t.Helper()
	body := map[string]interface{}{"query": queryText}
	if len(variables) != 0 {
		body["variables"] = variables[len(variables)-1]
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(serverURL+"/api/graphql", "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode GraphQL response %q: %v", raw, err)
	}
	return result
}

func graphQLWithToken(t *testing.T, serverURL, token, queryText string) map[string]interface{} {
	t.Helper()
	encoded, err := json.Marshal(map[string]interface{}{"query": queryText})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, serverURL+"/api/graphql", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Session "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func graphQLWithHeaders(t *testing.T, serverURL, queryText string, headers http.Header) map[string]interface{} {
	t.Helper()
	encoded, err := json.Marshal(map[string]interface{}{"query": queryText})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, serverURL+"/api/graphql", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func objectAt(t *testing.T, value map[string]interface{}, path ...string) map[string]interface{} {
	t.Helper()
	current := value
	for _, segment := range path {
		next, ok := current[segment].(map[string]interface{})
		if !ok {
			t.Fatalf("%s in %#v is not an object", segment, current)
		}
		current = next
	}
	return current
}

func assertErrorCode(t *testing.T, result map[string]interface{}, expected string) {
	t.Helper()
	errors, _ := result["errors"].([]interface{})
	if len(errors) == 0 {
		t.Fatalf("missing GraphQL error in %#v", result)
	}
	errorValue, _ := errors[0].(map[string]interface{})
	extensions, _ := errorValue["extensions"].(map[string]interface{})
	if extensions["code"] != expected {
		t.Fatalf("error code = %#v, want %q; result = %#v", extensions["code"], expected, result)
	}
}
