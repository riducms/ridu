package mongodb

import (
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBListWindowUsesVerifiedUniqueIndexAndOneOverflowSentinel(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoWindowCollection(t)
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "MongoDB window tests"},
		Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
	})

	unverified := mongoBegin(t, backend, true)
	windows := unverified.(store.WindowTransaction)
	_, err := windows.ListWindow(t.Context(), store.Request{
		Collection: collection,
		IndexWindow: &store.IndexWindow{
			Path: mongoWindowPath(t, "slug"), LowerBound: "a", UpperBound: "c",
		},
		Limit: 2,
	})
	if err == nil || !strings.Contains(err.Error(), "not verified") {
		mongoRollback(t, unverified)
		t.Fatalf("unverified MongoDB list window error = %v", err)
	}
	mongoRollback(t, unverified)

	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	write := mongoBegin(t, backend, false)
	corruptDeletedIDs := make([]string, 32)
	for index := range corruptDeletedIDs {
		corruptDeletedIDs[index] = fmt.Sprintf("window-corrupt-deleted-%02d", index)
		if _, err := write.Create(t.Context(), store.CreateRequest{
			Collection: collection, ID: corruptDeletedIDs[index],
			Values: store.Values{
				"slug":  store.String(fmt.Sprintf("a%03d", index)),
				"title": store.String("Out-of-band deleted shape"),
			},
		}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	for _, fixture := range []struct {
		id    string
		slug  string
		title string
	}{
		{id: "window-1", slug: "apple", title: "Apple"},
		{id: "window-2", slug: "apricot", title: "Apricot"},
		{id: "window-3", slug: "banana", title: "Banana"},
		{id: "window-4", slug: "berry", title: "Berry"},
		{id: "window-5", slug: "carrot", title: "Carrot"},
	} {
		if _, err := write.Create(t.Context(), store.CreateRequest{
			Collection: collection, ID: fixture.id,
			Values: store.Values{"slug": store.String(fixture.slug), "title": store.String(fixture.title)},
		}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	mongoCommit(t, write)

	physical := backend.database.Collection(physicalCollectionName(collection.ID))
	if _, err := physical.UpdateMany(
		t.Context(),
		bson.D{{Key: mongoIDPath, Value: bson.D{{Key: "$in", Value: corruptDeletedIDs}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: mongoDeletedAtPath, Value: int64(1)}}}},
	); err != nil {
		t.Fatal(err)
	}

	read := mongoBegin(t, backend, true)
	windowRequest := store.Request{
		Collection: collection,
		IndexWindow: &store.IndexWindow{
			Path: mongoWindowPath(t, "slug"), LowerBound: "a", UpperBound: "c",
		},
		Select: []query.Path{mongoWindowPath(t, "title")},
		Limit:  2,
	}
	window, err := read.(store.WindowTransaction).ListWindow(t.Context(), windowRequest)
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	mongoCommit(t, read)

	if !window.HasMore || len(window.Documents) != 2 {
		t.Fatalf("MongoDB list window = %#v, want two documents and overflow", window)
	}
	if window.Documents[0].ID != "window-1" || window.Documents[1].ID != "window-2" {
		t.Fatalf("MongoDB list window IDs = %q, %q", window.Documents[0].ID, window.Documents[1].ID)
	}
	for _, document := range window.Documents {
		if len(document.Values) != 1 {
			t.Fatalf("MongoDB list window projection = %#v, want title only", document.Values)
		}
		if _, exists := document.Values["title"]; !exists {
			t.Fatalf("MongoDB list window projection = %#v, want title", document.Values)
		}
	}

	path, indexName, predicate, err := mongoListWindowPredicate(windowRequest)
	if err != nil {
		t.Fatal(err)
	}
	var explained struct {
		ExecutionStats struct {
			TotalKeysExamined int64 `bson:"totalKeysExamined"`
			TotalDocsExamined int64 `bson:"totalDocsExamined"`
		} `bson:"executionStats"`
	}
	err = backend.database.RunCommand(t.Context(), bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "find", Value: physical.Name()},
			{Key: "filter", Value: predicate},
			{Key: "sort", Value: bson.D{{Key: path, Value: mongoAscendingDirection}}},
			{Key: "hint", Value: indexName},
			{Key: "limit", Value: int64(windowRequest.Limit + 1)},
		}},
		{Key: "verbosity", Value: "executionStats"},
	}).Decode(&explained)
	if err != nil {
		t.Fatal(err)
	}
	maximumExamined := int64(windowRequest.Limit + 1)
	if explained.ExecutionStats.TotalKeysExamined > maximumExamined || explained.ExecutionStats.TotalDocsExamined > maximumExamined {
		t.Fatalf(
			"MongoDB list window examined %d keys and %d documents, want at most %d despite corrupt out-of-window state",
			explained.ExecutionStats.TotalKeysExamined, explained.ExecutionStats.TotalDocsExamined, maximumExamined,
		)
	}

	if _, err := physical.UpdateOne(
		t.Context(),
		bson.D{{Key: mongoIDPath, Value: "window-1"}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "values.slug", Value: bson.A{"aardvark"}}}}},
	); err != nil {
		t.Fatal(err)
	}
	corrupt := mongoBegin(t, backend, true)
	_, err = corrupt.(store.WindowTransaction).ListWindow(t.Context(), windowRequest)
	if err == nil {
		mongoRollback(t, corrupt)
		t.Fatal("MongoDB list window accepted an out-of-band array in its scalar unique index")
	}
	mongoRollback(t, corrupt)
	if _, err := physical.UpdateOne(
		t.Context(),
		bson.D{{Key: mongoIDPath, Value: "window-1"}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "values.slug", Value: "apple"}}}},
	); err != nil {
		t.Fatal(err)
	}

	if err := physical.Indexes().DropOne(t.Context(), indexName); err != nil {
		t.Fatal(err)
	}
	missingIndex := mongoBegin(t, backend, true)
	_, err = missingIndex.(store.WindowTransaction).ListWindow(t.Context(), store.Request{
		Collection: collection,
		IndexWindow: &store.IndexWindow{
			Path: mongoWindowPath(t, "slug"), LowerBound: "a", UpperBound: "c",
		},
		Limit: 2,
	})
	if err == nil {
		mongoRollback(t, missingIndex)
		t.Fatal("MongoDB list window silently scanned after its verified unique index was removed")
	}
	mongoRollback(t, missingIndex)
}
