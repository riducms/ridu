package mongodb

import (
	"errors"
	"fmt"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBRelationshipsPopulateWithAccessAndReconcileHardDeletes(t *testing.T) {
	backend := mongoIntegrationStore(t)
	publicPath := mongoMustPath(t, "public")
	application, err := ridu.New(ridu.Config{
		Name: "MongoDB relationships",
		Collections: []ridu.Collection{
			{
				Slug: "people",
				Fields: field.Fields{field.Text("name").Required(), field.Checkbox("public").Required(), field.Text("secret").Access(field.Access{Read: func(operation.AccessContext,

				) (bool, error) {
					return false, nil
				}})},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					return ridu.Where(query.Equal(publicPath, query.Boolean(true))), nil
				}},
			},
			{Slug: "teams", Fields: field.Fields{field.Text("name").Required()}},
			{
				Slug: "posts", Trash: true,
				Fields: field.Fields{field.Text("title").Required(), field.Relationship("guard", "people").OnDelete(field.ReferenceDeleteRestrict), field.Relationship("owner", "people").OnDelete(field.ReferenceDeleteNullify), field.Relationships("related", "people").OnDelete(field.ReferenceDeleteNullify), field.PolymorphicRelationship("subject", "people", "teams").OnDelete(field.ReferenceDeleteNullify), field.Group("meta", field.Fields{field.Relationship("reviewer", "people").OnDelete(field.ReferenceDeleteNullify)})},
			},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}

	visible, err := application.Local().Create(t.Context(), "people", store.Values{
		"name": store.String("Visible"), "public": store.Boolean(true), "secret": store.String("must-redact"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	laterHidden, err := application.Local().Create(t.Context(), "people", store.Values{
		"name": store.String("Later hidden"), "public": store.Boolean(true), "secret": store.String("hidden"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	team, err := application.Local().Create(t.Context(), "teams", store.Values{"name": store.String("Core")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	teamReference := store.Object(store.Values{"relationTo": store.String("teams"), "id": store.String(team.ID)})
	post, err := application.Local().Create(t.Context(), "posts", store.Values{
		"title": store.String("References"), "guard": store.String(visible.ID), "owner": store.String(visible.ID),
		"related": store.List(store.String(visible.ID), store.String(visible.ID), store.String(laterHidden.ID)),
		"subject": teamReference,
		"meta":    store.Object(store.Values{"reviewer": store.String(visible.ID)}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	trashed, err := application.Local().Create(t.Context(), "posts", store.Values{
		"title": store.String("Trashed owner"), "owner": store.String(visible.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(t.Context(), "posts", trashed.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(t.Context(), "people", laterHidden.ID, store.Values{"public": store.Boolean(false)}, nil); err != nil {
		t.Fatal(err)
	}

	ownerPath := mongoMustPath(t, "owner")
	relatedPath := mongoMustPath(t, "related")
	subjectPath := mongoMustPath(t, "subject")
	reviewerPath := mongoMustPath(t, "meta.reviewer")
	populated, err := application.Local().FindWithOptions(t.Context(), "posts", post.ID, ridu.FindOptions{Populate: []query.Population{
		{Path: ownerPath}, {Path: relatedPath}, {Path: subjectPath}, {Path: reviewerPath},
	}})
	if err != nil {
		t.Fatal(err)
	}
	owner, ok := populated.Values["owner"].CopyDocument()
	if !ok || owner.ID != visible.ID {
		t.Fatalf("populated owner = %#v", populated.Values["owner"])
	}
	if _, leaked := owner.Values["secret"]; leaked {
		t.Fatal("population leaked a target field denied by read access")
	}
	related, ok := populated.Values["related"].CopyList()
	if !ok || len(related) != 3 {
		t.Fatalf("populated related = %#v", populated.Values["related"])
	}
	for index := range 2 {
		if document, populated := related[index].CopyDocument(); !populated || document.ID != visible.ID {
			t.Fatalf("populated duplicate relationship %d = %#v", index, related[index])
		}
	}
	if hiddenID, valid := related[2].StringValue(); !valid || hiddenID != laterHidden.ID {
		t.Fatalf("access-denied target was not retained as its canonical ID: %#v", related[2])
	}
	subject, ok := populated.Values["subject"].CopyObject()
	if !ok {
		t.Fatalf("polymorphic subject = %#v", populated.Values["subject"])
	}
	if relationTo, _ := subject["relationTo"].StringValue(); relationTo != "teams" {
		t.Fatalf("polymorphic discriminator = %#v", subject)
	}
	if document, populated := subject["id"].CopyDocument(); !populated || document.ID != team.ID {
		t.Fatalf("polymorphic populated target = %#v", subject["id"])
	}
	meta, _ := populated.Values["meta"].CopyObject()
	if document, populated := meta["reviewer"].CopyDocument(); !populated || document.ID != visible.ID {
		t.Fatalf("nested group population = %#v", meta["reviewer"])
	}

	if _, err := application.Local().Delete(t.Context(), "people", visible.ID, nil); !mongoOperationCode(err, "delete_restricted") {
		t.Fatalf("restricted target delete = %v", err)
	}
	unchanged, err := application.Local().Find(t.Context(), "posts", post.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id, valid := unchanged.Values["owner"].StringValue(); !valid || id != visible.ID {
		t.Fatalf("restrict planning partially nullified owner: %#v", unchanged.Values["owner"])
	}
	if _, err := application.Local().Update(t.Context(), "people", laterHidden.ID, store.Values{"public": store.Boolean(true)}, nil); err != nil {
		t.Fatal(err)
	}
	beforeNullify, err := application.Local().Update(t.Context(), "posts", post.ID, store.Values{"guard": store.Null()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(t.Context(), "people", visible.ID, nil); err != nil {
		t.Fatal(err)
	}

	reconciled, err := application.Local().Find(t.Context(), "posts", post.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Values["owner"].Kind() != store.ValueNull {
		t.Fatalf("singular reference was not nullified: %#v", reconciled.Values["owner"])
	}
	remaining, _ := reconciled.Values["related"].CopyList()
	if len(remaining) != 1 {
		t.Fatalf("has-many target occurrences were not removed: %#v", remaining)
	}
	if id, _ := remaining[0].StringValue(); id != laterHidden.ID {
		t.Fatalf("has-many reconciliation removed another target: %#v", remaining)
	}
	meta, _ = reconciled.Values["meta"].CopyObject()
	if meta["reviewer"].Kind() != store.ValueNull {
		t.Fatalf("nested relationship was not nullified: %#v", meta["reviewer"])
	}
	trashedAfter, err := application.Local().FindWithOptions(t.Context(), "posts", trashed.ID, ridu.FindOptions{TrashOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if trashedAfter.Values["owner"].Kind() != store.ValueNull {
		t.Fatalf("trashed current owner was not reconciled: %#v", trashedAfter.Values["owner"])
	}
	if !reconciled.UpdatedAt.Equal(beforeNullify.UpdatedAt) {
		t.Fatalf("automatic nullification changed owner UpdatedAt: %v -> %v", beforeNullify.UpdatedAt, reconciled.UpdatedAt)
	}

	if _, err := application.Local().Delete(t.Context(), "teams", team.ID, nil); err != nil {
		t.Fatal(err)
	}
	reconciled, err = application.Local().Find(t.Context(), "posts", post.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Values["subject"].Kind() != store.ValueNull {
		t.Fatalf("polymorphic singular reference was not nullified: %#v", reconciled.Values["subject"])
	}
}

func TestMongoDBReferenceFencePreventsConcurrentAdmissionAndDelete(t *testing.T) {
	backend := mongoIntegrationStore(t)
	application, err := ridu.New(ridu.Config{
		Name: "MongoDB reference fence",
		Collections: []ridu.Collection{
			{Slug: "targets", Fields: field.Fields{field.Text("name")}},
			{Slug: "owners", Fields: field.Fields{field.Relationship("target", "targets")}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(t.Context(), "targets", store.Values{"name": store.String("Target")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	collections := mongoCollectionsBySlug(application.Manifest().Snapshot().Collections)
	targetCollection := collections["targets"]
	ownerCollection := collections["owners"]
	allCollections := map[schema.StableID]schema.Collection{
		targetCollection.ID: targetCollection, ownerCollection.ID: ownerCollection,
	}

	admission := mongoBegin(t, backend, false)
	if _, err := admission.Find(t.Context(), store.Request{
		Collection: targetCollection, Collections: allCollections, ID: target.ID, Lock: store.LockReference,
	}); err != nil {
		mongoRollback(t, admission)
		t.Fatal(err)
	}
	owner, err := admission.Create(t.Context(), store.CreateRequest{
		Collection: ownerCollection, ID: "owner", Values: store.Values{"target": store.String(target.ID)},
	})
	if err != nil {
		mongoRollback(t, admission)
		t.Fatal(err)
	}

	deleteAttempt := mongoBegin(t, backend, false)
	if _, err := deleteAttempt.Find(t.Context(), store.Request{
		Collection: targetCollection, Collections: allCollections, ID: target.ID, Lock: store.LockMutation,
	}); !errors.Is(err, store.ErrConflict) {
		mongoRollback(t, deleteAttempt)
		mongoRollback(t, admission)
		t.Fatalf("concurrent target delete fence = %v, want ErrConflict", err)
	}
	mongoRollback(t, deleteAttempt)
	mongoCommit(t, admission)

	retry := mongoBegin(t, backend, false)
	if _, err := retry.Find(t.Context(), store.Request{
		Collection: targetCollection, Collections: allCollections, ID: target.ID, Lock: store.LockMutation,
	}); err != nil {
		mongoRollback(t, retry)
		t.Fatal(err)
	}
	if err := retry.ApplyReferenceDelete(t.Context(), store.ReferenceDeleteRequest{
		Target: store.DocumentReference{CollectionID: targetCollection.ID, DocumentID: target.ID}, Collections: allCollections,
	}); err != nil {
		mongoRollback(t, retry)
		t.Fatal(err)
	}
	if _, err := retry.Delete(t.Context(), store.Request{Collection: targetCollection, ID: target.ID}); err != nil {
		mongoRollback(t, retry)
		t.Fatal(err)
	}
	mongoCommit(t, retry)

	verification := mongoBegin(t, backend, true)
	reconciled, err := verification.Find(t.Context(), store.Request{Collection: ownerCollection, ID: owner.ID})
	if err != nil {
		mongoRollback(t, verification)
		t.Fatal(err)
	}
	if reconciled.Values["target"].Kind() != store.ValueNull {
		mongoRollback(t, verification)
		t.Fatalf("retry committed a dangling reference: %#v", reconciled.Values["target"])
	}
	mongoCommit(t, verification)
}

func TestMongoDBPopulationBudgetCountsDuplicateOutputNodes(t *testing.T) {
	backend := mongoIntegrationStore(t)
	application, err := ridu.New(ridu.Config{
		Name: "MongoDB population budget",
		Collections: []ridu.Collection{
			{Slug: "targets", Fields: field.Fields{field.Text("name")}},
			{Slug: "owners", Fields: field.Fields{field.Relationships("targets", "targets")}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(t.Context(), "targets", store.Values{"name": store.String("Target")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := application.Local().Create(t.Context(), "owners", store.Values{
		"targets": store.List(store.String(target.ID), store.String(target.ID)),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	collections := mongoCollectionsBySlug(application.Manifest().Snapshot().Collections)
	allCollections := map[schema.StableID]schema.Collection{
		collections["targets"].ID: collections["targets"], collections["owners"].ID: collections["owners"],
	}
	transaction := mongoBegin(t, backend, true)
	_, err = transaction.Find(t.Context(), store.Request{
		Collection: collections["owners"], Collections: allCollections, ID: owner.ID,
		Populate:         []query.Population{{Path: mongoMustPath(t, "targets"), Depth: 1}},
		PopulationBudget: store.NewPopulationBudget(1),
	})
	if !errors.Is(err, store.ErrPopulationLimit) {
		mongoRollback(t, transaction)
		t.Fatalf("population budget error = %v, want ErrPopulationLimit", err)
	}
	mongoRollback(t, transaction)
}

func TestMongoDBRecursivePopulationChargesEachOutputNodeOnce(t *testing.T) {
	backend := mongoIntegrationStore(t)
	application, err := ridu.New(ridu.Config{
		Name: "MongoDB recursive population budget",
		Collections: []ridu.Collection{{
			Slug:   "nodes",
			Fields: field.Fields{field.Text("name").Required(), field.Relationship("next", "nodes")},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	leaves, err := application.Local().Create(t.Context(), "nodes", store.Values{"name": store.String("leaf")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	middle, err := application.Local().Create(t.Context(), "nodes", store.Values{
		"name": store.String("middle"), "next": store.String(leaves.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := application.Local().Create(t.Context(), "nodes", store.Values{
		"name": store.String("root"), "next": store.String(middle.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	collection := application.Manifest().Snapshot().Collections[0]
	collections := map[schema.StableID]schema.Collection{collection.ID: collection}
	request := store.Request{
		Collection: collection, Collections: collections, ID: root.ID,
		Populate: []query.Population{{Path: mongoMustPath(t, "next"), Depth: 2}},
	}

	exact := mongoBegin(t, backend, true)
	request.PopulationBudget = store.NewPopulationBudget(2)
	populated, err := exact.Find(t.Context(), request)
	if err != nil {
		mongoRollback(t, exact)
		t.Fatalf("two-node recursive population with budget 2: %v", err)
	}
	populatedMiddle, ok := populated.Values["next"].CopyDocument()
	if !ok || populatedMiddle.ID != middle.ID {
		mongoRollback(t, exact)
		t.Fatalf("populated middle = %#v", populated.Values["next"])
	}
	populatedLeaf, ok := populatedMiddle.Values["next"].CopyDocument()
	if !ok || populatedLeaf.ID != leaves.ID {
		mongoRollback(t, exact)
		t.Fatalf("populated leaf = %#v", populatedMiddle.Values["next"])
	}
	mongoCommit(t, exact)

	overflow := mongoBegin(t, backend, true)
	request.PopulationBudget = store.NewPopulationBudget(1)
	if _, err := overflow.Find(t.Context(), request); !errors.Is(err, store.ErrPopulationLimit) {
		mongoRollback(t, overflow)
		t.Fatalf("two-node recursive population with budget 1 = %v, want ErrPopulationLimit", err)
	}
	mongoRollback(t, overflow)
}

func TestMongoDBRecursivePopulationAllowsWideGeneratedPlans(t *testing.T) {
	backend := mongoIntegrationStore(t)
	hubFields := field.Fields{field.Text("name")}
	for index := 0; index < 65; index++ {
		hubFields = append(hubFields, field.Relationship(fmt.Sprintf("reference%02d", index), "leaves"))
	}
	application, err := ridu.New(ridu.Config{
		Name: "MongoDB generated population paths",
		Collections: []ridu.Collection{
			{Slug: "leaves", Fields: field.Fields{field.Text("name")}},
			{Slug: "hubs", Fields: hubFields},
			{Slug: "sources", Fields: field.Fields{field.Relationship("hub", "hubs")}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	hub, err := application.Local().Create(t.Context(), "hubs", store.Values{"name": store.String("wide")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source, err := application.Local().Create(t.Context(), "sources", store.Values{"hub": store.String(hub.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	populated, err := application.Local().FindWithOptions(t.Context(), "sources", source.ID, ridu.FindOptions{
		Populate: []query.Population{{Path: mongoMustPath(t, "hub"), Depth: 2}},
	})
	if err != nil {
		t.Fatalf("wide generated population plan: %v", err)
	}
	populatedHub, ok := populated.Values["hub"].CopyDocument()
	if !ok || populatedHub.ID != hub.ID {
		t.Fatalf("wide generated plan result = %#v", populated.Values["hub"])
	}
}

func TestMongoDBDeleteDocumentStateRemovesOwnedAndTargetReferenceRows(t *testing.T) {
	backend := mongoIntegrationStore(t)
	application, err := ridu.New(ridu.Config{
		Name: "MongoDB reference state cleanup",
		Collections: []ridu.Collection{
			{Slug: "targets", Fields: field.Fields{field.Text("name")}},
			{Slug: "other-targets", Fields: field.Fields{field.Text("name")}},
			{Slug: "owners", Fields: field.Fields{field.Relationship("target", "targets").OnDelete(field.ReferenceDeleteRestrict)}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	collections := mongoCollectionsBySlug(application.Manifest().Snapshot().Collections)
	targetCollection := collections["targets"]
	otherTargetCollection := collections["other-targets"]
	ownerCollection := collections["owners"]
	referenceField := ownerCollection.Fields[0]
	reused := store.DocumentReference{CollectionID: targetCollection.ID, DocumentID: "reused-id"}
	fixtures := []referenceindex.Entry{
		{
			Owner:   store.DocumentReference{CollectionID: ownerCollection.ID, DocumentID: "missing-incoming-owner"},
			FieldID: referenceField.ID, Target: reused,
		},
		{
			Owner: reused, FieldID: referenceField.ID,
			Target: store.DocumentReference{CollectionID: otherTargetCollection.ID, DocumentID: "owned-target"},
		},
		{
			Owner:   store.DocumentReference{CollectionID: ownerCollection.ID, DocumentID: "collection-collision-owner"},
			FieldID: referenceField.ID,
			Target:  store.DocumentReference{CollectionID: otherTargetCollection.ID, DocumentID: reused.DocumentID},
		},
		{
			Owner:   store.DocumentReference{CollectionID: ownerCollection.ID, DocumentID: "unrelated-owner"},
			FieldID: referenceField.ID,
			Target:  store.DocumentReference{CollectionID: targetCollection.ID, DocumentID: "unrelated-target"},
		},
	}
	encoded := make([]any, len(fixtures))
	for index, fixture := range fixtures {
		encoded[index], err = encodeMongoReferenceEntry(fixture)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := backend.database.Collection(mongoReferenceCollectionName).InsertMany(t.Context(), encoded); err != nil {
		t.Fatal(err)
	}

	rolledBack := mongoBegin(t, backend, false)
	if err := rolledBack.DeleteDocumentState(t.Context(), reused); err != nil {
		mongoRollback(t, rolledBack)
		t.Fatal(err)
	}
	mongoRollback(t, rolledBack)
	if count, err := backend.database.Collection(mongoReferenceCollectionName).CountDocuments(t.Context(), bson.D{}); err != nil || count != int64(len(fixtures)) {
		t.Fatalf("reference rows after rolled-back cleanup = %d, %v", count, err)
	}

	cleanup := mongoBegin(t, backend, false)
	if err := cleanup.DeleteDocumentState(t.Context(), reused); err != nil {
		mongoRollback(t, cleanup)
		t.Fatal(err)
	}
	mongoCommit(t, cleanup)
	if count, err := backend.database.Collection(mongoReferenceCollectionName).CountDocuments(t.Context(), bson.D{}); err != nil || count != 2 {
		t.Fatalf("reference rows after committed cleanup = %d, %v", count, err)
	}
	for _, preserved := range []bson.D{
		{{Key: "targetCollection", Value: string(otherTargetCollection.ID)}, {Key: "targetDocument", Value: reused.DocumentID}},
		{{Key: "targetCollection", Value: string(targetCollection.ID)}, {Key: "targetDocument", Value: "unrelated-target"}},
	} {
		if count, err := backend.database.Collection(mongoReferenceCollectionName).CountDocuments(t.Context(), preserved); err != nil || count != 1 {
			t.Fatalf("preserved reference row count = %d, %v for %#v", count, err, preserved)
		}
	}

	if _, err := application.Local().Import(t.Context(), "targets", store.Values{"name": store.String("recreated")}, ridu.ImportOptions{ID: reused.DocumentID}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(t.Context(), "targets", reused.DocumentID, nil); err != nil {
		t.Fatalf("same-ID recreation inherited stale restrict metadata: %v", err)
	}
}

func mongoCollectionsBySlug(collections []schema.Collection) map[schema.CollectionSlug]schema.Collection {
	result := make(map[schema.CollectionSlug]schema.Collection, len(collections))
	for _, collection := range collections {
		result[collection.Slug] = collection
	}
	return result
}

func mongoOperationCode(err error, code string) bool {
	var operationError *ridu.OperationError
	return errors.As(err, &operationError) && operationError.Code == code
}
