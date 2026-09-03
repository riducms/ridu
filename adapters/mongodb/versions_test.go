package mongodb

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func mongoVersionedCollection(drafts bool, maximum int) schema.Collection {
	collection := mongoScalarCollection(false)
	owner, _ := query.NewPath("owner")
	collection.Capabilities.Versions = true
	collection.Versions = &schema.VersionSettings{Drafts: drafts, MaxPerDocument: maximum}
	collection.Fields = append(collection.Fields, schema.Field{
		ID: "posts-owner", Name: "owner", Path: owner, Required: true,
		Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
	})
	return collection
}

func TestMongoVersionCodecRoundTripsAndRejectsEnvelopeMismatch(t *testing.T) {
	collection := mongoVersionedCollection(true, 10)
	now := time.Date(2026, time.August, 30, 12, 34, 56, 789, time.UTC)
	document := store.Document{
		ID: "post:custom", CreatedAt: now, UpdatedAt: now.Add(time.Second),
		Status: store.StatusPublished, Revision: 7,
		Values: store.Values{"title": store.String("stable"), "rank": store.Number(2), "owner": store.String("editor")},
	}
	snapshot, err := encodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	encoded := bson.D{
		{Key: "_id", Value: versionID(document.ID, document.Revision)},
		{Key: mongoVersionOwnerPath, Value: document.ID},
		{Key: mongoVersionRevisionPath, Value: int64(document.Revision)},
		{Key: mongoVersionStatusPath, Value: string(document.Status)},
		{Key: mongoVersionCreatedAtPath, Value: now.UnixNano()},
		{Key: mongoVersionSnapshotPath, Value: snapshot},
	}
	rawBytes, err := bson.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	version, err := decodeMongoVersion(bson.Raw(rawBytes), collection)
	if err != nil {
		t.Fatal(err)
	}
	if version.ID != "post:custom:7" || version.DocumentID != document.ID || version.Revision != 7 ||
		version.Status != store.StatusPublished || !version.CreatedAt.Equal(now) || !reflect.DeepEqual(version.Snapshot, document) {
		t.Fatalf("decoded version = %#v", version)
	}

	for name, mutate := range map[string]func(bson.D){
		"revision": func(candidate bson.D) { candidate[2].Value = int64(8) },
		"status":   func(candidate bson.D) { candidate[3].Value = string(store.StatusDraft) },
		"owner":    func(candidate bson.D) { candidate[1].Value = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := append(bson.D(nil), encoded...)
			mutate(candidate)
			bytes, err := bson.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeMongoVersion(bson.Raw(bytes), collection); err == nil {
				t.Fatal("mismatched version envelope was accepted")
			}
		})
	}
}

func TestMongoVersionAccessPredicateTargetsStoredSnapshot(t *testing.T) {
	collection := mongoVersionedCollection(true, 10)
	owner, _ := query.NewPath("owner")
	status, _ := query.NewPath("_status")
	expression, err := query.And(
		query.Equal(owner, query.String("editor")),
		query.Equal(status, query.String(string(store.StatusDraft))),
	)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileMongoNode(collection, expression.Node(), "version access", mongoPredicateScope{storagePrefix: mongoVersionSnapshotPath + "."})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bson.MarshalExtJSON(compiled, false, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"snapshot.values.owner"`) || !strings.Contains(text, `"snapshot.meta.status"`) {
		t.Fatalf("prefixed version access predicate = %s", text)
	}
	if strings.Contains(text, `"values.owner"`) || strings.Contains(text, `"meta.status"`) {
		t.Fatalf("version access predicate retained an unscoped document path: %s", text)
	}
}

func TestMongoVersionMetadataMatchesCollectionDraftContract(t *testing.T) {
	publishedOnly := mongoVersionedCollection(false, 10)
	if err := validateMongoVersionMetadata(publishedOnly, store.StatusPublished, 1); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		status   store.Status
		revision int
	}{
		{status: store.StatusDraft, revision: 1},
		{status: store.StatusPublished, revision: 0},
		{status: "", revision: 1},
	} {
		if err := validateMongoVersionMetadata(publishedOnly, fixture.status, fixture.revision); err == nil {
			t.Fatalf("metadata %q/%d was accepted", fixture.status, fixture.revision)
		}
	}
	if physicalVersionCollectionName("posts") == physicalCollectionName("posts") ||
		!strings.HasPrefix(physicalVersionCollectionName("posts"), "z_v_") {
		t.Fatalf("version collection name = %q", physicalVersionCollectionName("posts"))
	}
}
