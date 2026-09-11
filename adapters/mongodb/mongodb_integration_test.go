package mongodb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMongoDBConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, manifest schema.Manifest) store.Store {
		backend := mongoIntegrationStore(t)
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatal(err)
		}
		return backend
	})
}

func TestMongoDBOpenRejectsStandaloneWithoutCreatingTheSelectedDatabase(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("RIDU_MONGODB_STANDALONE_URL"))
	if databaseURL == "" {
		t.Skip("set RIDU_MONGODB_STANDALONE_URL to run the negative topology test")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("RIDU_MONGODB_STANDALONE_URL is not a valid MongoDB URL")
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	databaseName := "ridu_test_" + hex.EncodeToString(suffix[:])
	parsed.Path = "/" + databaseName
	selectedURL := parsed.String()

	backend, err := OpenWithConfig(t.Context(), Config{
		DatabaseURL: selectedURL, AllowInsecureTransport: true,
		ConnectTimeout: 10 * time.Second, ServerSelectionTimeout: 10 * time.Second,
	})
	if backend != nil {
		_ = backend.Close()
		t.Fatal("standalone MongoDB returned a usable Ridu store")
	}
	if err == nil || !strings.Contains(err.Error(), "require a writable replica-set primary") {
		t.Fatalf("standalone topology error = %v", err)
	}

	client, err := mongo.Connect(options.Client().ApplyURI(selectedURL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Disconnect(cleanupContext); err != nil {
			t.Errorf("close standalone topology probe: %v", err)
		}
	})
	names, err := client.ListDatabaseNames(t.Context(), bson.D{{Key: "name", Value: databaseName}})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("rejected Open created database %q", databaseName)
	}
}

func TestMongoDBOpenRedactsLiveAuthenticationFailure(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("RIDU_MONGODB_STANDALONE_URL"))
	if databaseURL == "" {
		t.Skip("set RIDU_MONGODB_STANDALONE_URL to run the authentication redaction test")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("RIDU_MONGODB_STANDALONE_URL is not a valid MongoDB URL")
	}
	const secret = "mongo-live-secret"
	parsed.User = url.UserPassword("ridu-missing-user", secret)
	_, err = OpenWithConfig(t.Context(), Config{
		DatabaseURL: parsed.String(), AllowInsecureTransport: true,
		ConnectTimeout: 10 * time.Second, ServerSelectionTimeout: 10 * time.Second,
	})
	if err == nil {
		t.Fatal("invalid MongoDB credentials were accepted")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "ridu-missing-user") || strings.Contains(err.Error(), parsed.Host) {
		t.Fatalf("MongoDB authentication error exposed connection details: %v", err)
	}
}

func TestMongoDBOperationEngineCRUDUsesMutationFences(t *testing.T) {
	backend := mongoIntegrationStore(t)
	application, err := ridu.New(ridu.Config{
		Name: "MongoDB operation engine",
		Collections: []ridu.Collection{{
			Slug: "posts", Trash: true,
			Fields: field.Fields{field.Text("title").Required(), field.Number("rank")},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}

	created, err := application.Local().Create(t.Context(), "posts", store.Values{
		"title": store.String("source"), "rank": store.Number(1),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := application.Local().Update(t.Context(), "posts", created.ID, store.Values{"title": store.String("updated")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("operation-engine update: %v", err)
	}
	if title, _ := updated.Values["title"].StringValue(); title != "updated" {
		t.Fatalf("updated title = %q", title)
	}
	duplicate, err := application.Local().Duplicate(t.Context(), "posts", created.ID, store.Values{"title": store.String("copy")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("operation-engine duplicate: %v", err)
	}
	if duplicate.ID == created.ID {
		t.Fatal("duplicate reused its source ID")
	}
	titlePath := mongoMustPath(t, "title")
	distinct, err := application.Local().Distinct(t.Context(), "posts", ridu.DistinctOptions{Field: titlePath, Limit: 10})
	if err != nil {
		t.Fatalf("operation-engine distinct: %v", err)
	}
	if distinct.Total != 2 || len(distinct.Values) != 2 {
		t.Fatalf("operation-engine distinct = %#v, want two titles", distinct)
	}
	firstTitle, firstOK := distinct.Values[0].StringValue()
	secondTitle, secondOK := distinct.Values[1].StringValue()
	if !firstOK || !secondOK || firstTitle != "copy" || secondTitle != "updated" {
		t.Fatalf("operation-engine distinct titles = %#v", distinct.Values)
	}
	trashed, err := application.Local().Delete(t.Context(), "posts", created.ID, ridu.MutationOptions{})
	if err != nil || trashed.DeletedAt == nil {
		t.Fatalf("operation-engine trash = %#v, %v", trashed, err)
	}
	restored, err := application.Local().RestoreDeleted(t.Context(), "posts", created.ID, ridu.MutationOptions{})
	if err != nil || restored.DeletedAt != nil {
		t.Fatalf("operation-engine restore = %#v, %v", restored, err)
	}
	if _, err := application.Local().Delete(t.Context(), "posts", created.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().DeletePermanent(t.Context(), "posts", created.ID, ridu.MutationOptions{}); err != nil {
		t.Fatalf("operation-engine permanent delete: %v", err)
	}
}

func TestMongoDBOperationEngineNestedGroupsRejectArrayAncestorMatches(t *testing.T) {
	backend := mongoIntegrationStore(t)
	application, err := ridu.New(ridu.Config{
		Name: "MongoDB nested groups",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title"), field.Group("seo", field.Fields{field.Text("headline").Required(), field.Number("rank"), field.Group("details", field.Fields{field.Text("summary").Required()})}).Required()},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}

	created, err := application.Local().Create(t.Context(), "posts", store.Values{
		"title": store.String("nested"),
		"seo": store.Object(store.Values{
			"headline": store.String("original"),
			"rank":     store.Number(1),
			"details":  store.Object(store.Values{"summary": store.String("safe")}),
		}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("operation-engine nested create: %v", err)
	}
	updated, err := application.Local().Update(t.Context(), "posts", created.ID, store.Values{
		"seo": store.Object(store.Values{
			"headline": store.String("original"),
			"rank":     store.Number(2),
			"details":  store.Object(store.Values{"summary": store.String("safe")}),
		}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("operation-engine nested update: %v", err)
	}
	seo, ok := updated.Values["seo"].CopyObject()
	if !ok {
		t.Fatalf("updated group = %#v, want object", updated.Values["seo"])
	}
	if headline, _ := seo["headline"].StringValue(); headline != "original" {
		t.Fatalf("nested update headline = %#v, want original", seo)
	}
	if rank, _ := seo["rank"].NumberValue(); rank != 2 {
		t.Fatalf("nested update rank = %#v, want 2", seo)
	}
	details, ok := seo["details"].CopyObject()
	if !ok {
		t.Fatalf("nested update lost nested group: %#v", seo)
	}
	if summary, _ := details["summary"].StringValue(); summary != "safe" {
		t.Fatalf("nested update summary = %#v, want safe", details)
	}

	summaryPath := mongoMustPath(t, "seo.details.summary")
	where := query.Equal(summaryPath, query.String("safe"))
	page, err := application.Local().List(t.Context(), "posts", ridu.ListOptions{Where: where, Limit: 10})
	if err != nil {
		t.Fatalf("operation-engine nested list: %v", err)
	}
	if page.Total != 1 || len(page.Documents) != 1 || page.Documents[0].ID != created.ID {
		t.Fatalf("nested filter page = %#v, want created document", page)
	}

	collection := application.Manifest().Snapshot().Collections[0]
	physical := backend.database.Collection(physicalCollectionName(collection.ID))
	if _, err := physical.UpdateOne(t.Context(), bson.D{{Key: "_id", Value: created.ID}}, bson.D{{
		Key: "$set", Value: bson.D{{Key: "values.seo", Value: bson.A{
			bson.D{{Key: "details", Value: bson.D{{Key: "summary", Value: "safe"}}}},
		}}},
	}}); err != nil {
		t.Fatal(err)
	}
	page, err = application.Local().List(t.Context(), "posts", ridu.ListOptions{Where: where, Limit: 10})
	if err != nil {
		t.Fatalf("nested list after out-of-band array corruption: %v", err)
	}
	if page.Total != 0 || len(page.Documents) != 0 {
		t.Fatalf("array ancestor satisfied nested scalar predicate: %#v", page)
	}
}

func TestMongoDBRepeatedRootsWriteProjectAndFailClosedOnCorruptBSON(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoRepeatedCollection()
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}

	write := mongoBegin(t, backend, false)
	for _, fixture := range []struct {
		id    string
		title string
	}{
		{id: "whole-root", title: "whole-root"},
		{id: "corrupt-array-root", title: "corrupt-array-root"},
		{id: "corrupt-array-item", title: "corrupt-array-item"},
		{id: "corrupt-array-group", title: "corrupt-array-group"},
		{id: "corrupt-block", title: "corrupt-block"},
	} {
		values := mongoRepeatedValues()
		values["title"] = store.String(fixture.title)
		if _, err := write.Create(t.Context(), store.CreateRequest{Collection: collection, ID: fixture.id, Values: values}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	updated, err := write.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: "whole-root"},
		Values: store.Values{
			"tags": store.List(store.String("beta")),
			"rows": store.List(store.Object(store.Values{
				"_key": store.String("replacement-row"), "kind": store.String("replacement"), "label": store.String("Replaced"),
			})),
			"layout": store.List(store.Object(store.Values{
				"_key": store.String("replacement-block"), "blockType": store.String("hero"), "heading": store.String("Replacement"),
			})),
		},
	})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	rows, _ := updated.Values["rows"].CopyList()
	if len(rows) != 1 {
		mongoRollback(t, write)
		t.Fatalf("whole-root update retained old rows: %#v", updated.Values["rows"])
	}
	mongoCommit(t, write)

	projection := mongoBegin(t, backend, true)
	selected, err := projection.Find(t.Context(), store.Request{
		Collection: collection,
		ID:         "whole-root",
		Select: []query.Path{
			mongoMustPath(t, "tags"), mongoMustPath(t, "rows"), mongoMustPath(t, "layout"),
		},
	})
	if err != nil {
		mongoRollback(t, projection)
		t.Fatal(err)
	}
	if _, includesTitle := selected.Values["title"]; len(selected.Values) != 3 || includesTitle {
		mongoRollback(t, projection)
		t.Fatalf("whole-root repeated projection = %#v, want exactly three repeated roots", selected.Values)
	}
	mongoCommit(t, projection)

	physical := backend.database.Collection(physicalCollectionName(collection.ID))
	corruptions := []struct {
		id    string
		path  string
		value any
	}{
		{id: "corrupt-array-root", path: "values.rows", value: "not-an-array"},
		{id: "corrupt-array-item", path: "values.rows", value: bson.A{"not-an-object"}},
		{id: "corrupt-array-group", path: "values.rows", value: bson.A{bson.D{
			{Key: "kind", Value: "safe"}, {Key: "label", Value: "safe"}, {Key: "details", Value: "not-an-object"},
		}}},
		{id: "corrupt-block", path: "values.layout", value: bson.A{bson.D{{Key: "blockType", Value: int32(7)}, {Key: "heading", Value: "unsafe"}}}},
	}
	for _, corruption := range corruptions {
		if _, err := physical.UpdateOne(t.Context(), bson.D{{Key: "_id", Value: corruption.id}}, bson.D{{
			Key: "$set", Value: bson.D{{Key: corruption.path, Value: corruption.value}},
		}}); err != nil {
			t.Fatal(err)
		}
	}

	read := mongoBegin(t, backend, true)
	for _, fixture := range []struct {
		id       string
		repeated query.Expression
	}{
		{id: "corrupt-array-root", repeated: query.Equal(mongoMustPath(t, "rows.kind"), query.String("never"))},
		{id: "corrupt-array-item", repeated: query.Equal(mongoMustPath(t, "rows.kind"), query.String("never"))},
		{id: "corrupt-array-group", repeated: query.Equal(mongoMustPath(t, "rows.details.note"), query.String("never"))},
		{id: "corrupt-block", repeated: query.Equal(mongoMustPath(t, "layout.hero.heading"), query.String("never"))},
	} {
		predicate, err := query.Or(
			fixture.repeated,
			query.Equal(mongoMustPath(t, "title"), query.String(fixture.id)),
		)
		if err != nil {
			mongoRollback(t, read)
			t.Fatal(err)
		}
		filter := predicate.Node()
		page, err := read.List(t.Context(), store.Request{Collection: collection, Filter: &filter, Limit: 10})
		if err != nil {
			mongoRollback(t, read)
			t.Fatalf("shape-guarded query for %q: %v", fixture.id, err)
		}
		if page.Total != 0 || len(page.Documents) != 0 {
			mongoRollback(t, read)
			t.Fatalf("corrupt repeated value bypassed OR shape guard for %q: %#v", fixture.id, page)
		}
		if _, err := read.Find(t.Context(), store.Request{Collection: collection, ID: fixture.id}); err == nil {
			mongoRollback(t, read)
			t.Fatalf("direct corrupt repeated read %q was accepted", fixture.id)
		}
	}
	negated, err := query.Not(query.Equal(mongoMustPath(t, "rows.kind"), query.String("never")))
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	corruptOnly, err := query.And(
		query.Equal(mongoMustPath(t, "title"), query.String("corrupt-array-root")),
		negated,
	)
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	filter := corruptOnly.Node()
	page, err := read.List(t.Context(), store.Request{Collection: collection, Filter: &filter, Limit: 10})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if page.Total != 0 {
		mongoRollback(t, read)
		t.Fatalf("corrupt repeated value bypassed NOT shape guard: %#v", page)
	}
	mongoCommit(t, read)
}

func TestMongoDBRepeatedRootGuardsExcludeCorruptionFromAccessTotalsAndDistinct(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoRepeatedCollection()
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}

	corruptions := []struct {
		id    string
		path  string
		value any
	}{
		{id: "wrong-type-leaf", path: "values.rows", value: bson.A{bson.D{{Key: "kind", Value: int32(7)}, {Key: "label", Value: "visible"}}}},
		{id: "missing-required-leaf", path: "values.rows", value: bson.A{bson.D{{Key: "label", Value: "visible"}}}},
		{id: "incomplete-row", path: "values.rows", value: bson.A{bson.D{{Key: "kind", Value: "primary"}}}},
		{id: "unknown-row-key", path: "values.rows", value: bson.A{bson.D{{Key: "kind", Value: "primary"}, {Key: "label", Value: "visible"}, {Key: "unknown", Value: "leak"}}}},
		{id: "invalid-row-key", path: "values.rows", value: bson.A{bson.D{{Key: "_key", Value: int32(7)}, {Key: "kind", Value: "primary"}, {Key: "label", Value: "visible"}}}},
		{id: "duplicate-row-key", path: "values.rows", value: bson.A{
			bson.D{{Key: "_key", Value: "duplicate"}, {Key: "kind", Value: "primary"}, {Key: "label", Value: "visible"}},
			bson.D{{Key: "_key", Value: "duplicate"}, {Key: "kind", Value: "secondary"}, {Key: "label", Value: "visible"}},
		}},
		{id: "invalid-row-choice", path: "values.rows", value: bson.A{bson.D{{Key: "kind", Value: "primary"}, {Key: "label", Value: "visible"}, {Key: "state", Value: "unknown"}}}},
		{id: "invalid-select-choice", path: "values.tags", value: bson.A{"unknown"}},
		{id: "duplicate-select-choice", path: "values.tags", value: bson.A{"alpha", "alpha"}},
		{id: "nested-group-scalar", path: "values.rows", value: bson.A{bson.D{{Key: "kind", Value: "primary"}, {Key: "label", Value: "visible"}, {Key: "details", Value: int32(7)}}}},
		{id: "missing-block-required", path: "values.layout", value: bson.A{bson.D{{Key: "blockType", Value: "hero"}, {Key: "tone", Value: "bright"}}}},
		{id: "invalid-block-type", path: "values.layout", value: bson.A{bson.D{{Key: "blockType", Value: "unknown"}, {Key: "heading", Value: "unsafe"}}}},
		{id: "unknown-block-key", path: "values.layout", value: bson.A{bson.D{{Key: "blockType", Value: "quote"}, {Key: "heading", Value: "unsafe"}, {Key: "tone", Value: "leak"}}}},
	}

	write := mongoBegin(t, backend, false)
	for _, corruption := range corruptions {
		values := mongoRepeatedValues()
		values["title"] = store.String(corruption.id)
		if _, err := write.Create(t.Context(), store.CreateRequest{Collection: collection, ID: corruption.id, Values: values}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	mongoCommit(t, write)

	physical := backend.database.Collection(physicalCollectionName(collection.ID))
	for _, corruption := range corruptions {
		if _, err := physical.UpdateOne(t.Context(), bson.D{{Key: "_id", Value: corruption.id}}, bson.D{{
			Key: "$set", Value: bson.D{{Key: corruption.path, Value: corruption.value}},
		}}); err != nil {
			t.Fatal(err)
		}
	}

	rowKind := mongoMustPath(t, "rows.kind")
	rowNote := mongoMustPath(t, "rows.details.note")
	tags := mongoMustPath(t, "tags")
	heroHeading := mongoMustPath(t, "layout.hero.heading")
	quoteHeading := mongoMustPath(t, "layout.quote.heading")
	kindMatches := query.Equal(rowKind, query.String("primary"))
	noteMatches := query.Equal(rowNote, query.String("nested"))
	forward, err := query.And(kindMatches, noteMatches)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := query.And(noteMatches, kindMatches)
	if err != nil {
		t.Fatal(err)
	}
	forwardNot, err := query.Not(forward)
	if err != nil {
		t.Fatal(err)
	}
	reverseNot, err := query.Not(reverse)
	if err != nil {
		t.Fatal(err)
	}

	assertions := []struct {
		name   string
		id     string
		access query.Expression
	}{
		{name: "wrong-type queried leaf not-equal", id: "wrong-type-leaf", access: query.NotEqual(rowKind, query.String("blocked"))},
		{name: "missing required queried leaf not-equal", id: "missing-required-leaf", access: query.NotEqual(rowKind, query.String("blocked"))},
		{name: "incomplete row not-equal", id: "incomplete-row", access: query.NotEqual(rowKind, query.String("blocked"))},
		{name: "unknown row key not-equal", id: "unknown-row-key", access: query.NotEqual(rowKind, query.String("blocked"))},
		{name: "invalid row key not-equal", id: "invalid-row-key", access: query.NotEqual(rowKind, query.String("blocked"))},
		{name: "duplicate row key not-equal", id: "duplicate-row-key", access: query.NotEqual(rowKind, query.String("blocked"))},
		{name: "invalid row select choice not-equal", id: "invalid-row-choice", access: query.NotEqual(rowKind, query.String("blocked"))},
		{name: "invalid select choice not-equal", id: "invalid-select-choice", access: query.NotEqual(tags, query.String("blocked"))},
		{name: "duplicate select choice not-equal", id: "duplicate-select-choice", access: query.NotEqual(tags, query.String("blocked"))},
		{name: "nested group scalar not first", id: "nested-group-scalar", access: forwardNot},
		{name: "nested group scalar first", id: "nested-group-scalar", access: reverseNot},
		{name: "missing block required not-equal", id: "missing-block-required", access: query.NotEqual(heroHeading, query.String("blocked"))},
		{name: "invalid block type not-equal", id: "invalid-block-type", access: query.NotEqual(heroHeading, query.String("blocked"))},
		{name: "unknown block key not-equal", id: "unknown-block-key", access: query.NotEqual(quoteHeading, query.String("blocked"))},
	}

	read := mongoBegin(t, backend, true)
	distinct, ok := read.(store.DistinctTransaction)
	if !ok {
		mongoRollback(t, read)
		t.Fatal("MongoDB transaction does not implement DistinctTransaction")
	}
	title := mongoMustPath(t, "title")
	for _, assertion := range assertions {
		t.Run(assertion.name, func(t *testing.T) {
			filter := query.Equal(title, query.String(assertion.id)).Node()
			access := assertion.access.Node()
			page, err := read.List(t.Context(), store.Request{
				Collection: collection, Filter: &filter, Access: &access,
				Page: math.MaxInt, Limit: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != 0 || len(page.Documents) != 0 {
				t.Fatalf("corrupt document entered decoder-free list total: %#v", page)
			}
			values, err := distinct.Distinct(t.Context(), store.DistinctRequest{
				Collection: collection, Field: title, Filter: &filter, Access: &access,
				Page: 1, Limit: 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			if values.Total != 0 || len(values.Values) != 0 {
				t.Fatalf("corrupt document entered decoder-free distinct output: %#v", values)
			}
		})
	}
	mongoCommit(t, read)
}

func TestMongoDBDecoderFreeReadsExcludeCorruptUntouchedAuthoredFields(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoScalarCollection(false)
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}

	write := mongoBegin(t, backend, false)
	for _, fixture := range []struct {
		id    string
		title string
		rank  float64
	}{
		{id: "decoder-free-valid", title: "valid", rank: 1},
		{id: "decoder-free-corrupt", title: "will-be-corrupt", rank: 2},
	} {
		if _, err := write.Create(t.Context(), store.CreateRequest{
			Collection: collection, ID: fixture.id,
			Values: store.Values{"title": store.String(fixture.title), "rank": store.Number(fixture.rank)},
		}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	mongoCommit(t, write)

	// Preserve the complete metadata envelope and corrupt only an authored field
	// that none of the filter, access, selection, or distinct expressions reads.
	physical := backend.database.Collection(physicalCollectionName(collection.ID))
	if _, err := physical.UpdateOne(
		t.Context(),
		bson.D{{Key: "_id", Value: "decoder-free-corrupt"}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "values.title", Value: int32(7)}}}},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	duplicateKeys, err := encodeDocument(store.Document{
		ID: "decoder-free-duplicate-keys", CreatedAt: now, UpdatedAt: now,
		Values: store.Values{"title": store.String("placeholder"), "rank": store.Number(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range duplicateKeys {
		if duplicateKeys[index].Key == "values" {
			duplicateKeys[index].Value = bson.D{
				{Key: "title", Value: "first"},
				{Key: "title", Value: "second"},
				{Key: "rank", Value: float64(3)},
			}
		}
	}
	if _, err := physical.InsertOne(t.Context(), duplicateKeys); err != nil {
		t.Fatal(err)
	}

	rank := mongoMustPath(t, "rank")
	filter := query.GreaterThanEqual(rank, query.Number(0)).Node()
	access := query.LessThan(rank, query.Number(10)).Node()
	read := mongoBegin(t, backend, true)
	page, err := read.List(t.Context(), store.Request{
		Collection: collection, Filter: &filter, Access: &access,
		Page: math.MaxInt, Limit: 1,
	})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Documents) != 0 || page.Page != math.MaxInt {
		mongoRollback(t, read)
		t.Fatalf("decoder-free high page admitted untouched-field corruption: %#v", page)
	}

	selection, err := read.ResolveFilteredSelection(t.Context(), store.FilteredSelectionRequest{
		Collection: collection, Filter: &filter, Access: &access, Limit: 1,
	})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if selection.Overflow || len(selection.IDs) != 1 || selection.IDs[0] != "decoder-free-valid" {
		mongoRollback(t, read)
		t.Fatalf("filtered selection admitted untouched-field corruption: %#v", selection)
	}

	distinct, ok := read.(store.DistinctTransaction)
	if !ok {
		mongoRollback(t, read)
		t.Fatal("MongoDB transaction does not implement DistinctTransaction")
	}
	values, err := distinct.Distinct(t.Context(), store.DistinctRequest{
		Collection: collection, Field: rank, Filter: &filter, Access: &access,
		Page: math.MaxInt, Limit: 1,
	})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if values.Total != 1 || len(values.Values) != 0 || values.Page != math.MaxInt {
		mongoRollback(t, read)
		t.Fatalf("decoder-free distinct total admitted untouched-field corruption: %#v", values)
	}
	if _, err := read.Find(t.Context(), store.Request{
		Collection: collection, ID: "decoder-free-duplicate-keys",
	}); err == nil {
		mongoRollback(t, read)
		t.Fatal("strict decode accepted duplicate authored BSON keys")
	}
	mongoCommit(t, read)
}

func TestMongoDBMutationFenceExcludesConcurrentWritersAndRollsBack(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoScalarCollection(false)
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}
	seed := mongoBegin(t, backend, false)
	created, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "fenced",
		Values: store.Values{"title": store.String("stable"), "rank": store.Number(1)},
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	mongoCommit(t, seed)

	first := mongoBegin(t, backend, false)
	locked, err := first.Find(t.Context(), store.Request{Collection: collection, ID: created.ID, Lock: store.LockMutation})
	if err != nil {
		mongoRollback(t, first)
		t.Fatal(err)
	}
	if !locked.UpdatedAt.Equal(created.UpdatedAt) {
		mongoRollback(t, first)
		t.Fatalf("fence changed authored update time from %v to %v", created.UpdatedAt, locked.UpdatedAt)
	}

	contender := mongoBegin(t, backend, false)
	if _, err := contender.Find(t.Context(), store.Request{Collection: collection, ID: created.ID, Lock: store.LockMutation}); !errors.Is(err, store.ErrConflict) {
		mongoRollback(t, contender)
		mongoRollback(t, first)
		t.Fatalf("concurrent mutation fence = %v, want ErrConflict", err)
	}
	mongoRollback(t, contender)
	mongoCommit(t, first)

	fence, updatedAt := mongoStoredFence(t, backend, collection, created.ID)
	if fence != 1 || updatedAt != created.UpdatedAt.UnixNano() {
		t.Fatalf("committed fence state = fence %d updatedAt %d, want 1 and %d", fence, updatedAt, created.UpdatedAt.UnixNano())
	}

	rolledBack := mongoBegin(t, backend, false)
	if _, err := rolledBack.Find(t.Context(), store.Request{Collection: collection, ID: created.ID, Lock: store.LockMutation}); err != nil {
		mongoRollback(t, rolledBack)
		t.Fatal(err)
	}
	mongoRollback(t, rolledBack)
	fence, updatedAt = mongoStoredFence(t, backend, collection, created.ID)
	if fence != 1 || updatedAt != created.UpdatedAt.UnixNano() {
		t.Fatalf("rolled-back fence state = fence %d updatedAt %d, want 1 and %d", fence, updatedAt, created.UpdatedAt.UnixNano())
	}

	released := mongoBegin(t, backend, false)
	if _, err := released.Find(t.Context(), store.Request{Collection: collection, ID: created.ID, Lock: store.LockMutation}); err != nil {
		mongoRollback(t, released)
		t.Fatal(err)
	}
	mongoCommit(t, released)
	fence, _ = mongoStoredFence(t, backend, collection, created.ID)
	if fence != 2 {
		t.Fatalf("released fence = %d, want 2", fence)
	}
}

func TestMongoDBRESTScalarCRUD(t *testing.T) {
	backend := mongoIntegrationStore(t)
	application, err := ridu.New(ridu.Config{
		Name: "MongoDB REST",
		Collections: []ridu.Collection{{
			Slug: "posts", Trash: true,
			Fields: field.Fields{field.Text("title").Required(), field.Number("rank")},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	t.Cleanup(server.Close)

	var created struct {
		Doc map[string]any `json:"doc"`
	}
	mongoRESTRequest(t, server.Client(), http.MethodPost, server.URL+"/api/collections/posts", `{"title":"rest","rank":1}`, http.StatusCreated, &created)
	id, ok := created.Doc["id"].(string)
	if !ok || id == "" || created.Doc["title"] != "rest" {
		t.Fatalf("MongoDB REST create = %#v", created.Doc)
	}

	var updated struct {
		Doc map[string]any `json:"doc"`
	}
	mongoRESTRequest(t, server.Client(), http.MethodPatch, server.URL+"/api/collections/posts/"+url.PathEscape(id), `{"title":"updated"}`, http.StatusOK, &updated)
	if updated.Doc["title"] != "updated" {
		t.Fatalf("MongoDB REST update = %#v", updated.Doc)
	}

	where := url.QueryEscape(`{"title":{"equals":"updated"}}`)
	var listed struct {
		Docs       []map[string]any `json:"docs"`
		Pagination struct {
			TotalDocs int `json:"totalDocs"`
		} `json:"pagination"`
	}
	mongoRESTRequest(t, server.Client(), http.MethodGet, server.URL+"/api/collections/posts?where="+where+"&limit=10", "", http.StatusOK, &listed)
	if listed.Pagination.TotalDocs != 1 || len(listed.Docs) != 1 || listed.Docs[0]["id"] != id {
		t.Fatalf("MongoDB REST list = %#v", listed)
	}

	mongoRESTRequest(t, server.Client(), http.MethodDelete, server.URL+"/api/collections/posts/"+url.PathEscape(id), "", http.StatusOK, nil)
	mongoRESTRequest(t, server.Client(), http.MethodGet, server.URL+"/api/collections/posts/"+url.PathEscape(id), "", http.StatusNotFound, nil)
}

func TestMongoDBCanonicalImportIDsRoundTripThroughTheEngine(t *testing.T) {
	backend := mongoIntegrationStore(t)
	application, err := ridu.New(ridu.Config{
		Name: "MongoDB import IDs",
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title").Required()},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	ids := []string{"00042", "Case-Sensitive/界", strings.Repeat("x", store.MaxDocumentIDBytes)}
	for _, id := range ids {
		created, err := application.Local().Import(t.Context(), "posts", store.Values{"title": store.String(id)}, ridu.ImportOptions{ID: id})
		if err != nil {
			t.Fatalf("import ID %q: %v", id, err)
		}
		found, err := application.Local().Find(t.Context(), "posts", id, ridu.FindOptions{})
		if err != nil || found.ID != id || created.ID != id {
			t.Fatalf("ID %q round trip = created %q found %q, %v", id, created.ID, found.ID, err)
		}
	}
	if _, err := application.Local().Import(t.Context(), "posts", store.Values{"title": store.String("duplicate")}, ridu.ImportOptions{ID: ids[0]}); err == nil {
		t.Fatal("duplicate imported ID was accepted")
	}
}

func TestMongoDBReadsFailClosedOnOutOfBandSchemaDrift(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoScalarCollection(false)
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}
	seed := mongoBegin(t, backend, false)
	for _, id := range []string{"unknown-key", "wrong-type", "wrong-fence"} {
		if _, err := seed.Create(t.Context(), store.CreateRequest{
			Collection: collection, ID: id,
			Values: store.Values{"title": store.String("safe"), "rank": store.Number(1)},
		}); err != nil {
			mongoRollback(t, seed)
			t.Fatal(err)
		}
	}
	mongoCommit(t, seed)

	physical := backend.database.Collection(physicalCollectionName(collection.ID))
	if _, err := physical.UpdateOne(t.Context(), bson.D{{Key: "_id", Value: "unknown-key"}}, bson.D{{
		Key: "$set", Value: bson.D{{Key: "values.removed", Value: "must-not-leak"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := physical.UpdateOne(t.Context(), bson.D{{Key: "_id", Value: "wrong-type"}}, bson.D{{
		Key: "$set", Value: bson.D{{Key: "values.rank", Value: bson.A{1.0}}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := physical.UpdateOne(t.Context(), bson.D{{Key: "_id", Value: "wrong-fence"}}, bson.D{{
		Key: "$set", Value: bson.D{{Key: "meta.fence", Value: "not-a-long"}},
	}}); err != nil {
		t.Fatal(err)
	}

	snapshot := mongoBegin(t, backend, true)
	for _, fixture := range []struct {
		id   string
		want string
	}{
		{id: "unknown-key", want: "does not match collection"},
		{id: "wrong-type", want: "does not match collection"},
		{id: "wrong-fence", want: "invalid fence"},
	} {
		if _, err := snapshot.Find(t.Context(), store.Request{Collection: collection, ID: fixture.id}); err == nil || !strings.Contains(err.Error(), fixture.want) {
			mongoRollback(t, snapshot)
			t.Fatalf("drifted document %q read error = %v", fixture.id, err)
		}
	}
	mongoCommit(t, snapshot)

	mutation := mongoBegin(t, backend, false)
	if _, err := mutation.Find(t.Context(), store.Request{Collection: collection, ID: "wrong-fence", Lock: store.LockMutation}); err == nil || !strings.Contains(err.Error(), "invalid fence") {
		mongoRollback(t, mutation)
		t.Fatalf("mutation lock masked corrupt fence as absence: %v", err)
	}
	mongoRollback(t, mutation)
}

func TestMongoDBTransactionalScalarVerticalSlice(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoScalarCollection(true)
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}

	write := mongoBegin(t, backend, false)
	for _, fixture := range []struct {
		id    string
		title string
		rank  float64
	}{
		{id: "post-a", title: "public", rank: 2},
		{id: "post-b", title: "public", rank: 2},
		{id: "post-secret", title: "secret", rank: 3},
	} {
		created, err := write.Create(t.Context(), store.CreateRequest{
			Collection: collection, ID: fixture.id,
			Values: store.Values{"title": store.String(fixture.title), "rank": store.Number(fixture.rank)},
		})
		if err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
		if created.ID != fixture.id || created.CreatedAt.IsZero() || !created.CreatedAt.Equal(created.UpdatedAt) {
			t.Fatalf("created document = %#v", created)
		}
	}
	generated, err := write.Create(t.Context(), store.CreateRequest{
		Collection: collection,
		Values:     store.Values{"title": store.String("generated"), "rank": store.Number(0)},
	})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if !strings.HasPrefix(generated.ID, string(collection.ID)+"_") || len(generated.ID) != len(collection.ID)+1+24 {
		mongoRollback(t, write)
		t.Fatalf("generated MongoDB document ID = %q", generated.ID)
	}
	mongoCommit(t, write)

	rank := mongoMustPath(t, "rank")
	title := mongoMustPath(t, "title")
	filter := query.GreaterThan(rank, query.Number(1)).Node()
	access := query.NotEqual(title, query.String("secret")).Node()
	rankDescending, err := query.NewSort(rank, query.Descending)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := mongoBegin(t, backend, true)
	page, err := snapshot.List(t.Context(), store.Request{
		Collection: collection, Filter: &filter, Access: &access,
		Sort: []query.Sort{rankDescending}, Page: 1, Limit: 10,
	})
	if err != nil {
		mongoRollback(t, snapshot)
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Documents) != 2 || page.Documents[0].ID != "post-a" || page.Documents[1].ID != "post-b" {
		mongoRollback(t, snapshot)
		t.Fatalf("access-filtered stable page = %#v", page)
	}
	nonnegative := query.GreaterThanEqual(rank, query.Number(0)).Node()
	distinctTransaction, ok := snapshot.(store.DistinctTransaction)
	if !ok {
		mongoRollback(t, snapshot)
		t.Fatal("MongoDB transaction does not implement DistinctTransaction")
	}
	for pageNumber, expected := range []float64{0, 2} {
		distinct, err := distinctTransaction.Distinct(t.Context(), store.DistinctRequest{
			Collection: collection, Field: rank, Filter: &nonnegative, Access: &access,
			Page: pageNumber + 1, Limit: 1,
		})
		if err != nil {
			mongoRollback(t, snapshot)
			t.Fatal(err)
		}
		value, valid := distinct.Values[0].NumberValue()
		if distinct.Total != 2 || distinct.Page != pageNumber+1 || distinct.Limit != 1 || !valid || value != expected {
			mongoRollback(t, snapshot)
			t.Fatalf("distinct page %d = %#v, want %v", pageNumber+1, distinct, expected)
		}
	}
	overflowPage, err := snapshot.List(t.Context(), store.Request{
		Collection: collection, Filter: &filter, Access: &access, Page: math.MaxInt, Limit: 10,
	})
	if err != nil || overflowPage.Total != 2 || len(overflowPage.Documents) != 0 || overflowPage.Page != math.MaxInt {
		mongoRollback(t, snapshot)
		t.Fatalf("overflow-safe page = %#v, %v", overflowPage, err)
	}
	selection, err := snapshot.ResolveFilteredSelection(t.Context(), store.FilteredSelectionRequest{
		Collection: collection, Filter: &filter, Access: &access, Limit: 1,
	})
	if err != nil {
		mongoRollback(t, snapshot)
		t.Fatal(err)
	}
	if !selection.Overflow || len(selection.IDs) != 1 || selection.IDs[0] != "post-a" {
		mongoRollback(t, snapshot)
		t.Fatalf("filtered selection = %#v", selection)
	}
	if _, err := snapshot.Find(t.Context(), store.Request{
		Collection: collection, ID: "post-secret", Access: &access,
	}); !errors.Is(err, store.ErrNotFound) {
		mongoRollback(t, snapshot)
		t.Fatalf("access-denied find = %v, want ErrNotFound", err)
	}
	mongoCommit(t, snapshot)

	mutation := mongoBegin(t, backend, false)
	equalRank := query.Equal(rank, query.Number(2)).Node()
	updated, err := mutation.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{
			Collection: collection, ID: "post-b", Filter: &equalRank, Access: &access,
			Select: []query.Path{title},
		},
		Values: store.Values{"title": store.String("updated")},
	})
	if err != nil {
		mongoRollback(t, mutation)
		t.Fatal(err)
	}
	if value, ok := updated.Values["title"].StringValue(); !ok || value != "updated" || len(updated.Values) != 1 {
		mongoRollback(t, mutation)
		t.Fatalf("projected update = %#v", updated)
	}
	mongoCommit(t, mutation)

	duplicate := mongoBegin(t, backend, false)
	if _, err := duplicate.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "post-a",
		Values: store.Values{"title": store.String("duplicate"), "rank": store.Number(9)},
	}); !errors.Is(err, store.ErrConflict) {
		mongoRollback(t, duplicate)
		t.Fatalf("duplicate create = %v, want ErrConflict", err)
	}
	mongoRollback(t, duplicate)

	rolledBack := mongoBegin(t, backend, false)
	if _, err := rolledBack.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "post-rolled-back",
		Values: store.Values{"title": store.String("temporary"), "rank": store.Number(1)},
	}); err != nil {
		mongoRollback(t, rolledBack)
		t.Fatal(err)
	}
	mongoRollback(t, rolledBack)
	verification := mongoBegin(t, backend, true)
	if _, err := verification.Find(t.Context(), store.Request{Collection: collection, ID: "post-rolled-back"}); !errors.Is(err, store.ErrNotFound) {
		mongoRollback(t, verification)
		t.Fatalf("rolled-back document find = %v, want ErrNotFound", err)
	}
	mongoCommit(t, verification)

	trash := mongoBegin(t, backend, false)
	trashed, err := trash.Trash(t.Context(), store.Request{Collection: collection, ID: "post-a"})
	if err != nil {
		mongoRollback(t, trash)
		t.Fatal(err)
	}
	if trashed.DeletedAt == nil {
		mongoRollback(t, trash)
		t.Fatal("trash did not set deletedAt")
	}
	mongoCommit(t, trash)
	trashRead := mongoBegin(t, backend, true)
	if _, err := trashRead.Find(t.Context(), store.Request{Collection: collection, ID: "post-a"}); !errors.Is(err, store.ErrNotFound) {
		mongoRollback(t, trashRead)
		t.Fatalf("active find after trash = %v, want ErrNotFound", err)
	}
	if _, err := trashRead.Find(t.Context(), store.Request{Collection: collection, ID: "post-a", Deletion: store.DeletionTrash}); err != nil {
		mongoRollback(t, trashRead)
		t.Fatalf("trash find: %v", err)
	}
	mongoCommit(t, trashRead)
	restore := mongoBegin(t, backend, false)
	restored, err := restore.Restore(t.Context(), store.Request{Collection: collection, ID: "post-a"})
	if err != nil {
		mongoRollback(t, restore)
		t.Fatal(err)
	}
	if restored.DeletedAt != nil {
		mongoRollback(t, restore)
		t.Fatalf("restored deletedAt = %v, want nil", restored.DeletedAt)
	}
	mongoCommit(t, restore)

	hardDelete := mongoBegin(t, backend, false)
	if err := hardDelete.ApplyReferenceDelete(t.Context(), store.ReferenceDeleteRequest{
		Target:      store.DocumentReference{CollectionID: collection.ID, DocumentID: "post-b"},
		Collections: map[schema.StableID]schema.Collection{collection.ID: collection},
	}); err != nil {
		mongoRollback(t, hardDelete)
		t.Fatal(err)
	}
	deleted, err := hardDelete.Delete(t.Context(), store.Request{Collection: collection, ID: "post-b"})
	if err != nil || deleted.ID != "post-b" {
		mongoRollback(t, hardDelete)
		t.Fatalf("hard delete = %#v, %v", deleted, err)
	}
	if err := hardDelete.DeleteDocumentState(t.Context(), store.DocumentReference{CollectionID: collection.ID, DocumentID: "post-b"}); err != nil {
		mongoRollback(t, hardDelete)
		t.Fatal(err)
	}
	mongoCommit(t, hardDelete)
	deletedRead := mongoBegin(t, backend, true)
	if _, err := deletedRead.Find(t.Context(), store.Request{Collection: collection, ID: "post-b", Deletion: store.DeletionAll}); !errors.Is(err, store.ErrNotFound) {
		mongoRollback(t, deletedRead)
		t.Fatalf("hard-deleted find = %v, want ErrNotFound", err)
	}
	mongoCommit(t, deletedRead)
}

func TestMongoDBReadSnapshotDoesNotObserveLaterCommit(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoScalarCollection(false)
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}
	seed := mongoBegin(t, backend, false)
	if _, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "before",
		Values: store.Values{"title": store.String("before"), "rank": store.Number(1)},
	}); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	mongoCommit(t, seed)

	snapshot := mongoBegin(t, backend, true)
	if _, err := snapshot.Find(t.Context(), store.Request{Collection: collection, ID: "before", Lock: store.LockMutation}); err == nil || !strings.Contains(err.Error(), "read-only") {
		mongoRollback(t, snapshot)
		t.Fatalf("snapshot mutation-lock error = %v, want read-only rejection", err)
	}
	if _, err := snapshot.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "read-only-write",
		Values: store.Values{"title": store.String("blocked"), "rank": store.Number(0)},
	}); err == nil || !strings.Contains(err.Error(), "read-only") {
		mongoRollback(t, snapshot)
		t.Fatalf("snapshot mutation error = %v, want read-only rejection", err)
	}
	first, err := snapshot.List(t.Context(), store.Request{Collection: collection, Limit: 10})
	if err != nil || first.Total != 1 {
		mongoRollback(t, snapshot)
		t.Fatalf("initial snapshot page = %#v, %v", first, err)
	}
	concurrent := mongoBegin(t, backend, false)
	if _, err := concurrent.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "after",
		Values: store.Values{"title": store.String("after"), "rank": store.Number(2)},
	}); err != nil {
		mongoRollback(t, concurrent)
		mongoRollback(t, snapshot)
		t.Fatal(err)
	}
	mongoCommit(t, concurrent)
	second, err := snapshot.List(t.Context(), store.Request{Collection: collection, Limit: 10})
	if err != nil {
		mongoRollback(t, snapshot)
		t.Fatal(err)
	}
	if second.Total != 1 || len(second.Documents) != 1 || second.Documents[0].ID != "before" {
		mongoRollback(t, snapshot)
		t.Fatalf("later snapshot page = %#v, want original view", second)
	}
	mongoCommit(t, snapshot)
}

func TestMongoDBCanceledCommitAbortsBeforeSendingCommit(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoScalarCollection(false)
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}
	transaction := mongoBegin(t, backend, false)
	if _, err := transaction.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "canceled-commit",
		Values: store.Values{"title": store.String("temporary"), "rank": store.Number(1)},
	}); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := transaction.Commit(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled commit = %v, want context.Canceled", err)
	}
	verification := mongoBegin(t, backend, true)
	if _, err := verification.Find(t.Context(), store.Request{Collection: collection, ID: "canceled-commit"}); !errors.Is(err, store.ErrNotFound) {
		mongoRollback(t, verification)
		t.Fatalf("canceled commit became visible: %v", err)
	}
	mongoCommit(t, verification)
}

func mongoIntegrationStore(t *testing.T) *Store {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("RIDU_MONGODB_URL"))
	if databaseURL == "" {
		t.Skip("set RIDU_MONGODB_URL to run MongoDB integration tests")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("RIDU_MONGODB_URL is not a valid MongoDB URL")
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	databaseName := "ridu_test_" + hex.EncodeToString(suffix[:])
	parsed.Path = "/" + databaseName
	backend, err := OpenWithConfig(t.Context(), Config{
		DatabaseURL: parsed.String(), AllowInsecureTransport: true,
		ConnectTimeout: 10 * time.Second, ServerSelectionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("open isolated MongoDB database: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if !strings.HasPrefix(databaseName, "ridu_test_") || backend.database.Name() != databaseName {
			t.Errorf("refusing to drop unexpected MongoDB database %q", backend.database.Name())
		} else if err := backend.database.Drop(cleanupContext); err != nil {
			t.Errorf("drop isolated MongoDB database: %v", err)
		}
		if err := backend.Close(); err != nil {
			t.Errorf("close MongoDB integration store: %v", err)
		}
	})
	return backend
}

func mongoStoredFence(t *testing.T, backend *Store, collection schema.Collection, id string) (int64, int64) {
	t.Helper()
	raw, err := backend.database.Collection(physicalCollectionName(collection.ID)).FindOne(
		t.Context(), bson.D{{Key: "_id", Value: id}},
	).Raw()
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := raw.Lookup("meta").DocumentOK()
	if !ok {
		t.Fatal("stored MongoDB fence fixture has invalid metadata")
	}
	fence, fenceOK := metadata.Lookup("fence").Int64OK()
	updatedAt, updatedAtOK := metadata.Lookup("updatedAt").Int64OK()
	if !fenceOK || !updatedAtOK {
		t.Fatal("stored MongoDB fence fixture has invalid fence or updatedAt")
	}
	return fence, updatedAt
}

func mongoRESTRequest(t *testing.T, client *http.Client, method, target, body string, wantStatus int, output any) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		contents, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		t.Fatalf("MongoDB REST %s %s status = %d, want %d: %s", method, target, response.StatusCode, wantStatus, contents)
	}
	if output != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output); err != nil {
			t.Fatal(err)
		}
	}
}

func mongoBegin(t *testing.T, backend *Store, snapshot bool) store.Transaction {
	t.Helper()
	var (
		transaction store.Transaction
		err         error
	)
	if snapshot {
		transaction, err = backend.BeginSnapshot(t.Context())
	} else {
		transaction, err = backend.Begin(t.Context())
	}
	if err != nil {
		t.Fatal(err)
	}
	return transaction
}

func mongoCommit(t *testing.T, transaction store.Transaction) {
	t.Helper()
	if err := transaction.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func mongoRollback(t *testing.T, transaction store.Transaction) {
	t.Helper()
	rollbackContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := transaction.Rollback(rollbackContext); err != nil {
		t.Errorf("rollback MongoDB transaction: %v", err)
	}
}
