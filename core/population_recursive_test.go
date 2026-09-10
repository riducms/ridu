package core_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestRecursivePopulationTraversesNestedShapesWithAccessDepthAndRedaction(t *testing.T) {
	ctx := context.Background()
	publicPath, _ := query.NewPath("public")
	application, err := ridu.New(ridu.Config{Name: "Recursive population", Collections: []ridu.Collection{
		{
			Slug: "people", Fields: field.Fields{field.Text("name").Required(), field.Checkbox("public").Required(), field.Text("secret").Access(field.Access{Read: func(operation.AccessContext,

			) (bool, error) {
				return false, nil
			}})},
			Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Where(query.Equal(publicPath, query.Boolean(true))), nil
			}},
		},
		{Slug: "teams", Fields: field.Fields{field.Text("name").Required(), field.Relationship("owner", "people")}},
		{Slug: "entries", Fields: field.Fields{field.Group("meta", field.Fields{field.Relationship("reviewer", "people")}), field.Array("sections", field.Fields{field.Relationship("reviewer", "people")}), field.Blocks("layout", field.Block{Slug: "quote", Fields: field.Fields{field.Relationship("source", "teams")}})}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	visible, err := application.Local().Create(ctx, "people", store.Values{
		"name": store.String("Visible"), "public": store.Boolean(true), "secret": store.String("nested-secret"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	laterHidden, err := application.Local().Create(ctx, "people", store.Values{
		"name": store.String("Later hidden"), "public": store.Boolean(true), "secret": store.String("hidden-secret"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	team, err := application.Local().Create(ctx, "teams", store.Values{
		"name": store.String("Core"), "owner": store.String(visible.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := application.Local().Create(ctx, "entries", store.Values{
		"meta": store.Object(store.Values{"reviewer": store.String(visible.ID)}),
		"sections": store.List(
			store.Object(store.Values{"_key": store.String("one"), "reviewer": store.String(visible.ID)}),
			store.Object(store.Values{"_key": store.String("two"), "reviewer": store.String(laterHidden.ID)}),
		),
		"layout": store.List(store.Object(store.Values{
			"_key": store.String("quote-one"), "blockType": store.String("quote"), "source": store.String(team.ID),
		})),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "people", laterHidden.ID, store.Values{"public": store.Boolean(false)}, nil); err != nil {
		t.Fatal(err)
	}

	metaReviewer, _ := query.NewPath("meta", "reviewer")
	sectionReviewer, _ := query.NewPath("sections", "reviewer")
	blockSource, _ := query.NewPath("layout", "quote", "source")
	result, err := application.Local().FindWithOptions(ctx, "entries", entry.ID, ridu.FindOptions{Populate: []query.Population{
		{Path: metaReviewer}, {Path: sectionReviewer}, {Path: blockSource, Depth: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := result.Values["meta"].CopyObject()
	reviewer, populated := meta["reviewer"].CopyDocument()
	if !populated || reviewer.ID != visible.ID {
		t.Fatalf("group population = %#v", meta["reviewer"])
	}
	if _, leaked := reviewer.Values["secret"]; leaked {
		t.Fatal("nested group population leaked a redacted field")
	}
	sections, _ := result.Values["sections"].CopyList()
	first, _ := sections[0].CopyObject()
	firstReviewer, populated := first["reviewer"].CopyDocument()
	if !populated || firstReviewer.ID != visible.ID {
		t.Fatalf("array population = %#v", first["reviewer"])
	}
	second, _ := sections[1].CopyObject()
	if _, populated := second["reviewer"].CopyDocument(); populated {
		t.Fatalf("access-filtered nested target was populated: %#v", second["reviewer"])
	} else if id, _ := second["reviewer"].StringValue(); id != laterHidden.ID {
		t.Fatalf("access-filtered nested target changed = %q", id)
	}
	layout, _ := result.Values["layout"].CopyList()
	quote, _ := layout[0].CopyObject()
	populatedTeam, populated := quote["source"].CopyDocument()
	if !populated || populatedTeam.ID != team.ID {
		t.Fatalf("block population = %#v", quote["source"])
	}
	owner, populated := populatedTeam.Values["owner"].CopyDocument()
	if !populated || owner.ID != visible.ID {
		t.Fatalf("depth-two nested target population = %#v", populatedTeam.Values["owner"])
	}
	if _, leaked := owner.Values["secret"]; leaked {
		t.Fatal("depth-two population leaked a redacted field")
	}

	parameters := url.Values{}
	parameters.Set("populate", `{"meta.reviewer":true,"sections.reviewer":true,"layout.quote.source":{"depth":2}}`)
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/entries/"+entry.ID+"?"+parameters.Encode(), nil, "")
	var envelope protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, response, &envelope)
	responseMeta := envelope.Doc["meta"].(map[string]any)
	responseReviewer := responseMeta["reviewer"].(map[string]any)
	if responseReviewer["id"] != visible.ID || responseReviewer["secret"] != nil {
		t.Fatalf("REST nested group population = %#v", responseReviewer)
	}
	responseLayout := envelope.Doc["layout"].([]any)
	responseQuote := responseLayout[0].(map[string]any)
	responseTeam := responseQuote["source"].(map[string]any)
	responseOwner := responseTeam["owner"].(map[string]any)
	if responseTeam["id"] != team.ID || responseOwner["id"] != visible.ID || responseOwner["secret"] != nil {
		t.Fatalf("REST nested depth population = team %#v owner %#v", responseTeam, responseOwner)
	}
}

func TestAnonymousPopulationDoesNotExposeDraftTargetsFromPublicSources(t *testing.T) {
	ctx := context.Background()
	application, err := ridu.New(ridu.Config{Name: "Published population", Collections: []ridu.Collection{
		{Slug: "lessons", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}},
		{Slug: "links", Fields: field.Fields{field.Relationship("lesson", "lessons").Required()}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	staff := store.Document{ID: "staff-1"}
	draftMode := true
	draft, err := application.Local().CreateWithOptions(ctx, "lessons", store.Values{"title": store.String("Draft lesson")}, ridu.MutationOptions{Actor: &staff, Draft: &draftMode})
	if err != nil {
		t.Fatal(err)
	}
	link, err := application.Local().Create(ctx, "links", store.Values{"lesson": store.String(draft.ID)}, &staff)
	if err != nil {
		t.Fatal(err)
	}
	lessonPath, _ := query.NewPath("lesson")

	anonymous, err := application.Local().FindWithOptions(ctx, "links", link.ID, ridu.FindOptions{Populate: []query.Population{{Path: lessonPath}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, populated := anonymous.Values["lesson"].CopyDocument(); populated {
		t.Fatalf("anonymous population exposed draft target: %#v", anonymous.Values["lesson"])
	}
	if lessonID, _ := anonymous.Values["lesson"].StringValue(); lessonID != draft.ID {
		t.Fatalf("anonymous population changed unresolved relationship = %q, want %q", lessonID, draft.ID)
	}
	anonymousDraftMode := true
	anonymousDraft, err := application.Local().FindWithOptions(ctx, "links", link.ID, ridu.FindOptions{
		Draft: &anonymousDraftMode, Populate: []query.Population{{Path: lessonPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, populated := anonymousDraft.Values["lesson"].CopyDocument(); populated {
		t.Fatalf("anonymous draft override exposed draft target: %#v", anonymousDraft.Values["lesson"])
	}

	staffView, err := application.Local().FindWithOptions(ctx, "links", link.ID, ridu.FindOptions{Actor: &staff, Populate: []query.Population{{Path: lessonPath}}})
	if err != nil {
		t.Fatal(err)
	}
	if populated, ok := staffView.Values["lesson"].CopyDocument(); !ok || populated.ID != draft.ID {
		t.Fatalf("authorized draft population = %#v", staffView.Values["lesson"])
	}
}

func TestTrashMutationsPopulateTheirReturnedDocuments(t *testing.T) {
	ctx := context.Background()
	application, err := ridu.New(ridu.Config{Name: "Trash mutation population", Collections: []ridu.Collection{
		{Slug: "people", Fields: field.Fields{field.Text("name").Required()}},
		{Slug: "entries", Trash: true, Fields: field.Fields{field.Relationship("author", "people").Required()}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	person, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("Ada")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := application.Local().Create(ctx, "entries", store.Values{"author": store.String(person.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	authorPath, _ := query.NewPath("author")
	options := ridu.MutationOptions{Populate: []query.Population{{Path: authorPath}}}

	deleted, err := application.Local().DeleteWithOptions(ctx, "entries", entry.ID, options)
	if err != nil {
		t.Fatal(err)
	}
	if author, populated := deleted.Values["author"].CopyDocument(); !populated || author.ID != person.ID {
		t.Fatalf("soft-delete population = %#v", deleted.Values["author"])
	}
	restored, err := application.Local().RestoreDeletedWithOptions(ctx, "entries", entry.ID, options)
	if err != nil {
		t.Fatal(err)
	}
	if author, populated := restored.Values["author"].CopyDocument(); !populated || author.ID != person.ID {
		t.Fatalf("trash-restore population = %#v", restored.Values["author"])
	}
}

func TestPopulationRejectsUnknownDuplicateAndUnboundedRequests(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Population bounds", Collections: []ridu.Collection{
		{Slug: "people", Fields: field.Fields{field.Text("name")}},
		{Slug: "posts", Fields: field.Fields{field.Relationship("author", "people")}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	person, err := application.Local().Create(context.Background(), "people", store.Values{"name": store.String("Ada")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(context.Background(), "posts", store.Values{"author": store.String(person.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	author, _ := query.NewPath("author")
	unknown, _ := query.NewPath("unknown")
	for name, populations := range map[string][]query.Population{
		"unknown":   {{Path: unknown}},
		"duplicate": {{Path: author}, {Path: author}},
		"depth":     {{Path: author, Depth: population.MaxDepth + 1}},
	} {
		if _, err := application.Local().FindWithOptions(context.Background(), "posts", post.ID, ridu.FindOptions{Populate: populations}); !operationCode(err, "bad_query") {
			t.Fatalf("%s population error = %v", name, err)
		}
	}
	wide := make([]query.Population, population.MaxExplicitPaths+1)
	for index := range wide {
		wide[index] = query.Population{Path: author}
	}
	if _, err := application.Local().FindWithOptions(context.Background(), "posts", post.ID, ridu.FindOptions{Populate: wide}); !operationCode(err, "bad_query") {
		t.Fatalf("wide population error = %v", err)
	}
}

func TestPopulationMaterializationBudgetRejectsDenseRecursiveGraphs(t *testing.T) {
	ctx := context.Background()
	application, err := ridu.New(ridu.Config{Name: "Population output budget", Collections: []ridu.Collection{
		{Slug: "nodes", Fields: field.Fields{field.Text("name"), field.Relationships("links", "nodes")}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	const nodeCount = 10
	nodes := make([]store.Document, nodeCount)
	for index := range nodes {
		nodes[index], err = application.Local().Create(ctx, "nodes", store.Values{"name": store.String("node")}, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	links := make([]store.Value, len(nodes))
	for index, node := range nodes {
		links[index] = store.String(node.ID)
	}
	for _, node := range nodes {
		if _, err := application.Local().Update(ctx, "nodes", node.ID, store.Values{"links": store.List(links...)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	linksPath, _ := query.NewPath("links")
	if _, err := application.Local().List(ctx, "nodes", ridu.ListOptions{
		Page: 1, Limit: nodeCount, Populate: []query.Population{{Path: linksPath, Depth: population.MaxDepth}},
	}); !operationCode(err, "bad_query") {
		t.Fatalf("dense local population error = %v, want bad_query", err)
	}

	parameters := url.Values{}
	parameters.Set("limit", "10")
	parameters.Set("populate", `{"links":{"depth":5}}`)
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/nodes?"+parameters.Encode(), nil, "")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("dense REST population status = %d: %s", response.StatusCode, readBody(t, response))
	}
	var envelope protocol.ErrorEnvelope
	decodeResponse(t, response, &envelope)
	if envelope.Error.Code != protocol.ErrorBadRequest || envelope.Error.Message != "population materializes more than 4096 related documents" {
		t.Fatalf("dense REST population error = %#v", envelope.Error)
	}
}
