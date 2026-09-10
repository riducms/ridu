package mongodb

import (
	"bytes"
	"context"
	"errors"
	"math"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMongoDBContentMetadataIndexesAreExactAndFailClosed(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoVersionedCollection(true, 3)
	collection.ID, collection.Slug = "metadata-index-posts", "metadata-index-posts"
	collection.Capabilities.Trash = true
	manifest := mongoIndexTestManifest(collection)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	plans, err := mongoIndexPlans(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].definitions) != 2 {
		t.Fatalf("content metadata index plan = %#v", plans)
	}
	actual, err := backend.readCollectionIndexes(t.Context(), plans[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := compareMongoIndexSets(collection, plans[0].definitions, actual, false); err != nil {
		t.Fatalf("exact content metadata indexes: %v", err)
	}
	wantNativeID, err := bson.Marshal(bson.D{{Key: mongoIDPath, Value: mongoAscendingDirection}})
	if err != nil {
		t.Fatal(err)
	}
	nativeID, exists := actual["_id_"]
	if !exists || !bytes.Equal(nativeID.keys, wantNativeID) {
		t.Fatalf("native MongoDB ID index = %#v", nativeID)
	}

	physical := backend.database.Collection(plans[0].physicalName)
	if err := physical.Indexes().DropOne(t.Context(), mongoContentLifecycleIndexName); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err == nil || !strings.Contains(err.Error(), "required index") {
		t.Fatalf("missing lifecycle metadata index verification error = %v", err)
	}
	if err := backend.requireVerifiedIndexes(collection); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("failed metadata verification retained runtime authorization: %v", err)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("restore lifecycle metadata index: %v", err)
	}

	if err := physical.Indexes().DropOne(t.Context(), mongoContentPublicationIndexName); err != nil {
		t.Fatal(err)
	}
	wrongKeys := bson.D{{Key: mongoUpdatedAtPath, Value: mongoAscendingDirection}}
	if _, err := physical.Indexes().CreateOne(t.Context(), mongo.IndexModel{
		Keys: wrongKeys, Options: options.Index().SetName(mongoContentPublicationIndexName),
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong publication metadata index sync error = %v", err)
	}
	afterRejectedSync, err := backend.readCollectionIndexes(t.Context(), plans[0])
	if err != nil {
		t.Fatal(err)
	}
	wantWrongKeys, err := bson.Marshal(wrongKeys)
	if err != nil {
		t.Fatal(err)
	}
	if found := afterRejectedSync[mongoContentPublicationIndexName]; !bytes.Equal(found.keys, wantWrongKeys) {
		t.Fatalf("failed additive sync mutated wrong-shaped metadata index: %#v", found)
	}
	if err := physical.Indexes().DropOne(t.Context(), mongoContentPublicationIndexName); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("restore publication metadata index: %v", err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("verify restored content metadata indexes: %v", err)
	}
}

func TestMongoDBIndexContractRejectsPrepareUniqueConversionState(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoScalarCollection(false)
	collection.ID, collection.Slug = "prepare-unique-contract", "prepare-unique-contract"
	collection.Fields = append([]schema.Field(nil), collection.Fields...)
	collection.Fields[0].Index = true
	manifest := mongoIndexTestManifest(collection)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	physicalName := physicalCollectionName(collection.ID)
	physical := backend.database.Collection(physicalName)
	const indexName = "application_owner_lookup"
	if _, err := physical.Indexes().CreateOne(t.Context(), mongo.IndexModel{
		Keys: bson.D{{Key: "values.owner", Value: int32(1)}},
		Options: options.Index().
			SetName(indexName),
	}); err != nil {
		t.Fatal(err)
	}
	document := func(id string) bson.D {
		return bson.D{
			{Key: "_id", Value: id},
			{Key: "values", Value: bson.D{{Key: "owner", Value: "same-owner"}}},
		}
	}
	if _, err := physical.InsertMany(t.Context(), []any{document("existing-1"), document("existing-2")}); err != nil {
		t.Fatal(err)
	}
	if err := backend.database.RunCommand(t.Context(), bson.D{
		{Key: "collMod", Value: physicalName},
		{Key: "index", Value: bson.D{
			{Key: "name", Value: indexName},
			{Key: "prepareUnique", Value: true},
		}},
	}).Err(); err != nil {
		t.Fatalf("set prepareUnique: %v", err)
	}
	if _, err := physical.InsertOne(t.Context(), document("rejected")); !mongoErrorHasCode(err, 11000) {
		t.Fatalf("prepareUnique duplicate insert error = %v", err)
	}

	for _, verification := range []struct {
		name string
		run  func() error
	}{
		{name: "VerifyIndexes", run: func() error { return backend.VerifyIndexes(t.Context(), manifest) }},
		{name: "SyncIndexes", run: func() error { return backend.SyncIndexes(t.Context(), manifest) }},
	} {
		err := verification.run()
		if err == nil || !strings.Contains(err.Error(), indexName) ||
			!strings.Contains(err.Error(), "can change valid writes") {
			t.Fatalf("%s prepareUnique error = %v", verification.name, err)
		}
	}
}
func TestMongoDBConcurrentIndexOperationsCannotPublishStaleAuthorization(t *testing.T) {
	backend := mongoIntegrationStore(t)
	base := mongoScalarCollection(false)
	base.ID, base.Slug = "serialized-index-posts", "serialized-index-posts"
	baseManifest := mongoIndexTestManifest(base)
	trash := base
	trash.Capabilities.Trash = true
	trashManifest := mongoIndexTestManifest(trash)
	if err := backend.SyncIndexes(t.Context(), baseManifest); err != nil {
		t.Fatal(err)
	}
	if err := backend.requireVerifiedIndexes(base); err != nil {
		t.Fatal(err)
	}

	type operationResult struct {
		operation string
		err       error
	}
	results := make(chan operationResult, 2)
	started := make(chan string, 2)
	backend.indexLifecycleMu.Lock()
	go func() {
		started <- "sync"
		results <- operationResult{operation: "sync", err: backend.SyncIndexes(t.Context(), trashManifest)}
	}()
	go func() {
		started <- "verify"
		results <- operationResult{operation: "verify", err: backend.VerifyIndexes(t.Context(), baseManifest)}
	}()
	<-started
	<-started

	var premature *operationResult
	select {
	case result := <-results:
		premature = &result
	case <-time.After(75 * time.Millisecond):
	}
	if err := backend.requireVerifiedIndexes(base); err != nil {
		backend.indexLifecycleMu.Unlock()
		t.Fatalf("queued index operation cleared authorization before entering lifecycle guard: %v", err)
	}
	backend.indexLifecycleMu.Unlock()

	completed := make(map[string]error, 2)
	if premature != nil {
		completed[premature.operation] = premature.err
	}
	for len(completed) < 2 {
		result := <-results
		completed[result.operation] = result.err
	}
	if premature != nil {
		t.Fatalf("index operation %q bypassed lifecycle serialization: %v", premature.operation, premature.err)
	}
	if err := completed["sync"]; err != nil {
		t.Fatalf("serialized additive index sync: %v", err)
	}
	if err := completed["verify"]; err != nil && !strings.Contains(err.Error(), "unexpected index") {
		t.Fatalf("serialized old-plan verification error = %v", err)
	}
	if err := backend.requireVerifiedIndexes(base); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("old plan retained stale authorization after physical metadata change: %v", err)
	}
	if completed["verify"] == nil {
		if err := backend.requireVerifiedIndexes(trash); err != nil {
			t.Fatalf("last successful sync did not publish the current physical plan: %v", err)
		}
	} else if err := backend.requireVerifiedIndexes(trash); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("failed last verification did not clear the current-plan authorization: %v", err)
	}
}

func TestMongoDBInvalidIndexPlanRejectsAndClearsAuthorization(t *testing.T) {
	backend := mongoIntegrationStore(t)
	valid := mongoScalarCollection(false)
	valid.ID, valid.Slug = "valid-index-plan", "valid-index-plan"
	valid.Fields = append([]schema.Field(nil), valid.Fields...)
	valid.Fields[0].Index = true
	validManifest := mongoIndexTestManifest(valid)
	if err := backend.SyncIndexes(t.Context(), validManifest); err != nil {
		t.Fatal(err)
	}

	physical := backend.database.Collection(physicalCollectionName(valid.ID))
	indexNames := func() []string {
		t.Helper()
		specifications, err := physical.Indexes().ListSpecifications(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, len(specifications))
		for index := range specifications {
			names[index] = specifications[index].Name
		}
		sort.Strings(names)
		return names
	}
	wantIndexes := indexNames()

	first := mongoScalarCollection(false)
	first.ID, first.Slug = "duplicate-resource", "first-resource"
	first.Fields = append([]schema.Field(nil), first.Fields...)
	first.Fields[0].ID = "first-resource-title"
	first.Fields[0].Index = true
	second := first
	second.Slug = "second-resource"
	second.Fields = append([]schema.Field(nil), first.Fields...)
	second.Fields[0].ID = "second-resource-title"
	invalidManifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "MongoDB invalid-plan preflight"},
		Collections: []schema.Collection{first, second},
		Globals:     []schema.Global{},
		Plugins:     []schema.Plugin{},
	})

	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{name: "SyncIndexes", run: func() error { return backend.SyncIndexes(t.Context(), invalidManifest) }},
		{name: "VerifyIndexes", run: func() error { return backend.VerifyIndexes(t.Context(), invalidManifest) }},
	} {
		if operation.name == "VerifyIndexes" {
			if err := backend.SyncIndexes(t.Context(), validManifest); err != nil {
				t.Fatal(err)
			}
		}
		err := operation.run()
		if err == nil || !strings.Contains(err.Error(), "share stable resource ID") {
			t.Fatalf("%s physical collision error = %v", operation.name, err)
		}
		if got := indexNames(); !slices.Equal(got, wantIndexes) {
			t.Fatalf("%s invalid plan changed existing indexes: got %#v, want %#v", operation.name, got, wantIndexes)
		}
		if err := backend.requireVerifiedIndexes(valid); err == nil || !strings.Contains(err.Error(), "not verified") {
			t.Fatalf("%s invalid plan retained runtime authorization: %v", operation.name, err)
		}
	}
}
func TestMongoDBUnusedDerivedNamespacesAreStatusDiagnosticsNotServingFailures(t *testing.T) {
	t.Run("unversioned resource", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		global := mongoScalarCollection(false)
		global.ID, global.Slug = "unversioned-global", "unversioned-global"
		global.Capabilities.Global = true
		global.Fields = append([]schema.Field(nil), global.Fields...)
		global.Fields[0].ID = "unversioned-global-title"
		global.Fields[0].Index = true
		manifest := schema.NewManifest(schema.Snapshot{
			Version: schema.CurrentVersion, Application: schema.Application{Name: "MongoDB unversioned namespace diagnostic"},
			Globals: []schema.Global{global}, Plugins: []schema.Plugin{},
		})
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatal(err)
		}
		versionCollection := backend.database.Collection(physicalVersionCollectionName(global.ID))
		if _, err := versionCollection.InsertOne(t.Context(), bson.D{{Key: "_id", Value: "retired-version"}}); err != nil {
			t.Fatal(err)
		}
		if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("serving verification rejected unused version namespace: %v", err)
		}
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("additive sync rejected unused version namespace: %v", err)
		}
		plan, err := mongoPhysicalIndexPlans(manifest)
		if err != nil {
			t.Fatal(err)
		}
		err = backend.verifyIndexPlansWithoutAuthorization(t.Context(), plan.collections, plan.system)
		if err == nil || !strings.Contains(err.Error(), "version state exists while versions are disabled") {
			t.Fatalf("migration/status version diagnostic = %v", err)
		}
	})

	t.Run("reference-free resources", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "reference-free", "reference-free"
		collection.Fields = append([]schema.Field(nil), collection.Fields...)
		collection.Fields[0].ID = "reference-free-title"
		collection.Fields[1].ID = "reference-free-rank"
		manifest := mongoIndexTestManifest(collection)
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatal(err)
		}
		references := backend.database.Collection(mongoReferenceCollectionName)
		if _, err := references.InsertOne(t.Context(), bson.D{{Key: "_id", Value: "retired-reference"}}); err != nil {
			t.Fatal(err)
		}
		if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("serving verification rejected unused reference namespace: %v", err)
		}
		if _, err := mongoIndexedCreate(t.Context(), backend, collection, "safe-write", store.Values{
			"title": store.String("safe"), "rank": store.Number(1),
		}); err != nil {
			t.Fatalf("serving write with unused reference namespace: %v", err)
		}
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("additive sync rejected unused reference namespace: %v", err)
		}
		plan, err := mongoPhysicalIndexPlans(manifest)
		if err != nil {
			t.Fatal(err)
		}
		err = backend.verifyIndexPlansWithoutAuthorization(t.Context(), plan.collections, plan.system)
		if err == nil || !strings.Contains(err.Error(), "no relationship or upload field requires it") {
			t.Fatalf("migration/status reference diagnostic = %v", err)
		}
	})
}

func TestMongoDBAdmittedReferenceCleanupDoesNotRereadVerificationState(t *testing.T) {
	backend := mongoIntegrationStore(t)
	target, owner, _, manifest := mongoFrameworkIndexTestManifest(t)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	const ownerID = "admitted-reference-owner"
	if _, err := backend.database.Collection(mongoReferenceCollectionName).InsertOne(t.Context(), bson.D{
		{Key: "_id", Value: "admitted-reference-sentinel"},
		{Key: "ownerCollection", Value: string(owner.ID)},
		{Key: "ownerDocument", Value: ownerID},
		{Key: "targetCollection", Value: string(target.ID)},
		{Key: "targetDocument", Value: "admitted-reference-target"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.requireVerifiedReferenceIndexes(owner); err != nil {
		t.Fatalf("admit relationship cleanup: %v", err)
	}

	// Simulate a concurrent lifecycle verification clearing published state
	// after this relationship operation has already been admitted. Low-level
	// cleanup must complete against that captured plan instead of rereading a
	// mutable flag and silently leaving stale reference rows.
	backend.clearVerifiedIndexes()
	transaction := mongoBegin(t, backend, false)
	documentTransaction, ok := transaction.(*documentTransaction)
	if !ok {
		mongoRollback(t, transaction)
		t.Fatalf("MongoDB transaction type = %T", transaction)
	}
	if err := documentTransaction.replaceDocumentReferences(t.Context(), owner, store.Document{
		ID: ownerID, Values: store.Values{},
	}); err != nil {
		mongoRollback(t, transaction)
		t.Fatalf("complete admitted reference cleanup: %v", err)
	}
	mongoCommit(t, transaction)
	if count, err := backend.database.Collection(mongoReferenceCollectionName).CountDocuments(
		t.Context(), bson.D{{Key: "_id", Value: "admitted-reference-sentinel"}},
	); err != nil || count != 0 {
		t.Fatalf("admitted reference cleanup left stale row: count=%d error=%v", count, err)
	}
}

func TestMongoDBCollectionContractRequiresSimpleWritableCollections(t *testing.T) {
	createCollection := func(t *testing.T, backend *Store, name, locale string) {
		t.Helper()
		if err := backend.database.CreateCollection(
			t.Context(),
			name,
			options.CreateCollection().SetCollation(&options.Collation{Locale: locale, Strength: 2}),
		); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("simple collation preserves exact ID semantics", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "simple-collection-contract", "simple-collection-contract"
		physicalName := physicalCollectionName(collection.ID)
		createCollection(t, backend, physicalName, "simple")

		manifest := mongoIndexTestManifest(collection)
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("sync explicit simple collection: %v", err)
		}
		if _, err := backend.database.Collection(physicalName).InsertMany(t.Context(), []any{
			bson.D{{Key: "_id", Value: "Case"}},
			bson.D{{Key: "_id", Value: "case"}},
		}); err != nil {
			t.Fatalf("simple collection rejected exact-case IDs: %v", err)
		}
	})

	t.Run("non-simple collation changes ID equality", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "non-simple-collection-contract", "non-simple-collection-contract"
		collection.Fields = append([]schema.Field(nil), collection.Fields...)
		collection.Fields[0].Index = true
		physicalName := physicalCollectionName(collection.ID)
		createCollection(t, backend, physicalName, "en")
		physical := backend.database.Collection(physicalName)
		if _, err := physical.InsertOne(t.Context(), bson.D{{Key: "_id", Value: "Case"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := physical.InsertOne(t.Context(), bson.D{{Key: "_id", Value: "case"}}); !mongo.IsDuplicateKeyError(err) {
			t.Fatalf("non-simple collection ID equality error = %v", err)
		}

		err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection))
		if err == nil || !strings.Contains(err.Error(), `collection "non-simple-collection-contract"`) ||
			!strings.Contains(err.Error(), "collation") {
			t.Fatalf("non-simple collection sync error = %v", err)
		}
		indexes, listErr := physical.Indexes().ListSpecifications(t.Context())
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(indexes) != 1 || indexes[0].Name != "_id_" {
			t.Fatalf("failed collection preflight changed indexes: %#v", indexes)
		}
	})

	t.Run("views are read-only", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "view-collection-contract", "view-collection-contract"
		physicalName := physicalCollectionName(collection.ID)
		const sourceName = "view_contract_source"
		if err := backend.database.CreateCollection(t.Context(), sourceName); err != nil {
			t.Fatal(err)
		}
		if err := backend.database.CreateView(t.Context(), physicalName, sourceName, mongo.Pipeline{}); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.database.Collection(physicalName).InsertOne(
			t.Context(), bson.D{{Key: "_id", Value: "read-only"}},
		); err == nil {
			t.Fatal("write to MongoDB view unexpectedly succeeded")
		}

		err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection))
		if err == nil || !strings.Contains(err.Error(), `collection "view-collection-contract"`) ||
			!strings.Contains(err.Error(), "writable") {
			t.Fatalf("view collection sync error = %v", err)
		}
	})
}
func TestMongoDBNativeIDIndexContractCoversEveryPlannedNamespaceKind(t *testing.T) {
	backend := mongoIntegrationStore(t)
	createVersionOneCollection := func(t *testing.T, name string) {
		t.Helper()
		if err := backend.database.RunCommand(t.Context(), bson.D{
			{Key: "create", Value: name},
			{Key: "idIndex", Value: bson.D{
				{Key: "v", Value: int32(1)},
				{Key: "key", Value: bson.D{{Key: mongoIDPath, Value: mongoAscendingDirection}}},
				{Key: "name", Value: "requested_custom_native_id_name"},
			}},
		}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	assertNativeID := func(t *testing.T, name string, version int32) {
		t.Helper()
		specifications, err := backend.database.ListCollectionSpecifications(
			t.Context(), bson.D{{Key: "name", Value: name}},
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(specifications) != 1 || !mongoNativeIDSpecificationMatches(specifications[0].IDIndex) ||
			specifications[0].IDIndex.Version != version {
			t.Fatalf("MongoDB typed native ID specification for %q = %#v", name, specifications)
		}
		cursor, err := backend.database.Collection(name).Indexes().List(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := cursor.Close(t.Context()); err != nil {
				t.Errorf("close MongoDB index cursor for %q: %v", name, err)
			}
		}()
		matched := 0
		for cursor.Next(t.Context()) {
			raw := append(bson.Raw(nil), cursor.Current...)
			indexName, ok := raw.Lookup("name").StringValueOK()
			if !ok || indexName != "_id_" {
				continue
			}
			matched++
			actualVersion, ok := raw.Lookup("v").Int32OK()
			if !ok || actualVersion != version || !mongoRawNativeIDIndexMatches(raw) {
				t.Fatalf("MongoDB raw native ID specification for %q has unexpected semantic shape", name)
			}
		}
		if err := cursor.Err(); err != nil {
			t.Fatal(err)
		}
		if matched != 1 {
			t.Fatalf("MongoDB native ID index count for %q = %d, want 1", name, matched)
		}
	}

	content := mongoVersionedCollection(true, 3)
	content.ID, content.Slug = "native-id-content", "native-id-content"
	global := mongoScalarCollection(false)
	global.ID, global.Slug = "native-id-global", "native-id-global"
	global.Capabilities.Global = true
	lazy := mongoScalarCollection(false)
	lazy.ID, lazy.Slug = "native-id-lazy", "native-id-lazy"
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{
			Name: "MongoDB native ID index contract",
		},
		Collections: []schema.Collection{content, lazy},
		Globals:     []schema.Global{global},
		Plugins:     []schema.Plugin{},
	})
	versionOneNamespaces := []string{
		physicalCollectionName(content.ID),
		physicalCollectionName(global.ID),
		physicalVersionCollectionName(content.ID),
		mongoUploadLockCollectionName,
	}
	for _, name := range versionOneNamespaces {
		createVersionOneCollection(t, name)
		assertNativeID(t, name, 1)
	}

	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("sync with version-one native ID indexes: %v", err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("verify with version-one native ID indexes: %v", err)
	}
	for _, name := range versionOneNamespaces {
		assertNativeID(t, name, 1)
	}
	assertNativeID(t, mongoPreferenceCollectionName, 2)
	if names, err := backend.database.ListCollectionNames(
		t.Context(), bson.D{{Key: "name", Value: physicalCollectionName(lazy.ID)}},
	); err != nil || len(names) != 0 {
		t.Fatalf("zero-index lazy namespace after native ID verification = %#v, error %v", names, err)
	}
}

func TestMongoDBCollectionContractRejectsOnlyEnforcingValidators(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoScalarCollection(false)
	collection.ID, collection.Slug = "validator-contract", "validator-contract"
	manifest := mongoIndexTestManifest(collection)
	physicalName := physicalCollectionName(collection.ID)
	if err := backend.database.CreateCollection(t.Context(), physicalName); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	modify := func(elements ...bson.E) {
		t.Helper()
		command := bson.D{{Key: "collMod", Value: physicalName}}
		command = append(command, elements...)
		if err := backend.database.RunCommand(t.Context(), command).Err(); err != nil {
			t.Fatal(err)
		}
	}
	validator := bson.D{{Key: "values.requiredByDatabase", Value: bson.D{{Key: "$exists", Value: true}}}}
	modify(bson.E{Key: "validator", Value: validator})

	if _, err := mongoIndexedCreate(t.Context(), backend, collection, "rejected-by-validator", store.Values{
		"title": store.String("valid to Ridu"), "rank": store.Number(1),
	}); err == nil || !strings.Contains(err.Error(), "server code 121") {
		t.Fatalf("enforcing validator write error = %v", err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err == nil || !strings.Contains(err.Error(), "validation") {
		t.Fatalf("enforcing validator verification error = %v", err)
	}

	// MongoDB retains its default strict/error settings after the validator is
	// cleared. Those settings alone cannot reject a write and must not become
	// catalogue-presence failures.
	modify(bson.E{Key: "validator", Value: bson.D{}})
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("inert validation settings rejected: %v", err)
	}

	modify(
		bson.E{Key: "validator", Value: validator},
		bson.E{Key: "validationAction", Value: "warn"},
	)
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("warning-only validator rejected: %v", err)
	}
	if _, err := mongoIndexedCreate(t.Context(), backend, collection, "warning-validator", store.Values{
		"title": store.String("still writable"), "rank": store.Number(2),
	}); err != nil {
		t.Fatalf("warning-only validator rejected Ridu write: %v", err)
	}
}

func TestMongoDBCollectionContractRejectsCappedCollections(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoScalarCollection(false)
	collection.ID, collection.Slug = "capped-content-contract", "capped-content-contract"
	collection.Fields = append([]schema.Field(nil), collection.Fields...)
	collection.Fields[0].Index = true
	physicalName := physicalCollectionName(collection.ID)
	if err := backend.database.RunCommand(t.Context(), bson.D{
		{Key: "create", Value: physicalName},
		{Key: "capped", Value: true},
		{Key: "size", Value: int32(4096)},
		{Key: "max", Value: int32(2)},
	}).Err(); err != nil {
		t.Fatal(err)
	}
	physical := backend.database.Collection(physicalName)
	if _, err := physical.InsertMany(t.Context(), []any{
		bson.D{{Key: "_id", Value: "oldest"}},
		bson.D{{Key: "_id", Value: "middle"}},
		bson.D{{Key: "_id", Value: "newest"}},
	}); err != nil {
		t.Fatal(err)
	}
	if count, err := physical.CountDocuments(t.Context(), bson.D{}); err != nil || count != 2 {
		t.Fatalf("capped rollover document count = %d, error %v", count, err)
	}
	if err := physical.FindOne(t.Context(), bson.D{{Key: "_id", Value: "oldest"}}).Err(); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("capped rollover retained oldest document: %v", err)
	}

	session, err := backend.client.StartSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.EndSession(t.Context())
	transactionWriteErr := mongo.WithSession(t.Context(), session, func(sessionContext context.Context) error {
		if err := session.StartTransaction(); err != nil {
			return err
		}
		_, err := physical.InsertOne(sessionContext, bson.D{{Key: "_id", Value: "transactional-write"}})
		return err
	})
	if !mongoErrorHasCode(transactionWriteErr, 263) {
		t.Fatalf("capped transactional write error = %v", transactionWriteErr)
	}

	manifest := mongoIndexTestManifest(collection)
	err = backend.SyncIndexes(t.Context(), manifest)
	if err == nil || !strings.Contains(err.Error(), `collection "capped-content-contract"`) ||
		!strings.Contains(err.Error(), "retention") {
		t.Fatalf("capped collection sync error = %v", err)
	}
	indexes, listErr := physical.Indexes().ListSpecifications(t.Context())
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(indexes) != 1 || indexes[0].Name != "_id_" {
		t.Fatalf("failed capped preflight changed indexes: %#v", indexes)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err == nil ||
		!strings.Contains(err.Error(), "retention") {
		t.Fatalf("capped collection verification error = %v", err)
	}
}
func TestMongoDBCollectionContractAllowsClusteredStorageButRejectsExpiry(t *testing.T) {
	createClustered := func(t *testing.T, backend *Store, name string, expiration *int64) {
		t.Helper()
		command := bson.D{
			{Key: "create", Value: name},
			{Key: "clusteredIndex", Value: bson.D{
				{Key: "key", Value: bson.D{{Key: "_id", Value: int32(1)}}},
				{Key: "unique", Value: true},
			}},
		}
		if expiration != nil {
			command = append(command, bson.E{Key: "expireAfterSeconds", Value: *expiration})
		}
		if err := backend.database.RunCommand(t.Context(), command).Err(); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("plain clustered storage serves transactional Ridu writes", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "clustered-storage", "clustered-storage"
		physicalName := physicalCollectionName(collection.ID)
		createClustered(t, backend, physicalName, nil)

		manifest := mongoIndexTestManifest(collection)
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("sync clustered collection: %v", err)
		}
		if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("verify clustered collection: %v", err)
		}
		created, err := mongoIndexedCreate(t.Context(), backend, collection, "clustered-document", store.Values{
			"title": store.String("clustered"), "rank": store.Number(1),
		})
		if err != nil || created.ID != "clustered-document" {
			t.Fatalf("transactional clustered write = %#v, %v", created, err)
		}
	})

	t.Run("collection expiry remains a data-lifetime hazard", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "clustered-expiry", "clustered-expiry"
		expiration := int64(0)
		createClustered(t, backend, physicalCollectionName(collection.ID), &expiration)

		err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection))
		if err == nil || !strings.Contains(err.Error(), "retention") {
			t.Fatalf("clustered expiry sync error = %v", err)
		}
	})
}

func TestMongoDBCollectionContractRejectsOnlyEffectiveEncryptedFields(t *testing.T) {
	createEncryptedCollection := func(t *testing.T, backend *Store, name string, fields bson.A) {
		t.Helper()
		if err := backend.database.RunCommand(t.Context(), bson.D{
			{Key: "create", Value: name},
			{Key: "encryptedFields", Value: bson.D{{Key: "fields", Value: fields}}},
		}).Err(); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("empty field metadata is inert", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "inert-encrypted-fields", "inert-encrypted-fields"
		collection.Fields = append([]schema.Field(nil), collection.Fields...)
		collection.Fields[0].Index = true
		createEncryptedCollection(t, backend, physicalCollectionName(collection.ID), bson.A{})

		manifest := mongoIndexTestManifest(collection)
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("sync collection with inert encrypted-fields metadata: %v", err)
		}
		if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("verify collection with inert encrypted-fields metadata: %v", err)
		}
		created, err := mongoIndexedCreate(t.Context(), backend, collection, "inert-encryption-document", store.Values{
			"title": store.String("allowed"),
			"rank":  store.Number(1),
		})
		if err != nil || created.ID != "inert-encryption-document" {
			t.Fatalf("transactional write with inert encrypted-fields metadata = %#v, %v", created, err)
		}
	})

	t.Run("declared encrypted field changes valid writes", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "effective-encrypted-fields", "effective-encrypted-fields"
		collection.Fields = append([]schema.Field(nil), collection.Fields...)
		collection.Fields[0].Index = true
		const encryptedPath = "secret-encrypted-field"
		createEncryptedCollection(t, backend, physicalCollectionName(collection.ID), bson.A{bson.D{
			{Key: "keyId", Value: bson.Binary{Subtype: 4, Data: bytes.Repeat([]byte{0x5a}, 16)}},
			{Key: "path", Value: encryptedPath},
			{Key: "bsonType", Value: "string"},
		}})

		physical := backend.database.Collection(physicalCollectionName(collection.ID))
		if _, err := physical.InsertOne(t.Context(), bson.D{{Key: "ordinary", Value: "allowed"}}); err != nil {
			t.Fatalf("ordinary field insert into encrypted-fields collection: %v", err)
		}
		if _, err := physical.InsertOne(t.Context(), bson.D{{Key: encryptedPath, Value: "plaintext"}}); !mongoErrorHasCode(err, 121) {
			t.Fatalf("plaintext encrypted-field insert error = %v", err)
		}

		err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection))
		if err == nil || !strings.Contains(err.Error(), `collection "effective-encrypted-fields"`) ||
			!strings.Contains(err.Error(), "encryption") {
			t.Fatalf("effective encrypted-fields sync error = %v", err)
		}
	})
}
func TestMongoDBIndexCountBoundaryMatchesServerAndPreflightsOverflow(t *testing.T) {
	backend := mongoIntegrationStore(t)
	bounded := mongoLocalizedIndexLimitTestCollection(t, 31)
	bounded.Capabilities.Trash = true
	manifest := mongoLocalizedIndexTestManifest(bounded)
	plans, err := mongoPhysicalIndexPlans(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans.collections) != 1 || len(plans.collections[0].definitions) != mongoMaxIndexesPerCollection-1 {
		t.Fatalf("bounded MongoDB index-count plan = %#v", plans.collections)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("sync maximum MongoDB index count: %v", err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("verify maximum MongoDB index count: %v", err)
	}
	actual, err := backend.readCollectionIndexes(t.Context(), plans.collections[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != mongoMaxIndexesPerCollection {
		t.Fatalf("live MongoDB index count = %d, want %d including native _id_", len(actual), mongoMaxIndexesPerCollection)
	}
	if err := compareMongoIndexSets(bounded, plans.collections[0].definitions, actual, false); err != nil {
		t.Fatalf("verify maximum MongoDB index set: %v", err)
	}

	overflow := bounded
	overflow.Capabilities.Versions = true
	overflow.Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 3}
	err = backend.SyncIndexes(t.Context(), mongoLocalizedIndexTestManifest(overflow))
	if err == nil || !strings.Contains(err.Error(), "requires 65 indexes") {
		t.Fatalf("overflow MongoDB index-count preflight error = %v", err)
	}
	after, err := backend.readCollectionIndexes(t.Context(), plans.collections[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != mongoMaxIndexesPerCollection {
		t.Fatalf("failed index-count preflight changed live index count to %d", len(after))
	}
	if _, exists := after[mongoContentPublicationIndexName]; exists {
		t.Fatal("failed index-count preflight created the overflow publication index")
	}
	if err := backend.requireVerifiedIndexes(bounded); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("failed index-count preflight retained runtime authorization: %v", err)
	}
	versionCollections, err := backend.database.ListCollectionNames(t.Context(), bson.D{{
		Key: "name", Value: physicalVersionCollectionName(bounded.ID),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(versionCollections) != 0 {
		t.Fatalf("failed index-count preflight created version collection %#v", versionCollections)
	}
}

func TestMongoDBIndexKeyPatternBoundaryMatchesServerAndPreflightsOverflow(t *testing.T) {
	backend := mongoIntegrationStore(t)
	bounded := mongoIndexKeyPatternLimitTestCollection(t, "key-pattern-live-boundary", mongoMaxIndexKeyPatternBytes)
	manifest := mongoIndexTestManifest(bounded)
	plans, err := mongoPhysicalIndexPlans(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans.collections) != 1 || len(plans.collections[0].definitions) != 1 {
		t.Fatalf("bounded MongoDB key-pattern plan = %#v", plans.collections)
	}
	encoded, err := bson.Marshal(plans.collections[0].definitions[0].keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != mongoMaxIndexKeyPatternBytes {
		t.Fatalf("bounded MongoDB key pattern = %d bytes, want %d", len(encoded), mongoMaxIndexKeyPatternBytes)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("sync maximum MongoDB key pattern: %v", err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("verify maximum MongoDB key pattern: %v", err)
	}
	actual, err := backend.readCollectionIndexes(t.Context(), plans.collections[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := compareMongoIndexSets(bounded, plans.collections[0].definitions, actual, false); err != nil {
		t.Fatalf("verify maximum MongoDB key pattern: %v", err)
	}
	before, err := backend.database.ListCollectionNames(t.Context(), bson.D{})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(before)

	overflow := mongoIndexKeyPatternLimitTestCollection(t, "key-pattern-live-overflow", mongoMaxIndexKeyPatternBytes+1)
	err = backend.SyncIndexes(t.Context(), mongoIndexTestManifest(overflow))
	if err == nil || !strings.Contains(err.Error(), "2049-byte key pattern") {
		t.Fatalf("overflow MongoDB key-pattern preflight error = %v", err)
	}
	after, err := backend.database.ListCollectionNames(t.Context(), bson.D{})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(after)
	if !slices.Equal(after, before) {
		t.Fatalf("failed key-pattern preflight changed MongoDB collections: before=%#v after=%#v", before, after)
	}
	if err := backend.requireVerifiedIndexes(bounded); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("failed key-pattern preflight retained runtime authorization: %v", err)
	}
}

func TestMongoDBSyncIndexesEnforcesUniqueNullAndTrashSemantics(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoIndexTestCollection(t)
	manifest := mongoIndexTestManifest(collection)

	unverified := mongoBegin(t, backend, false)
	if _, err := unverified.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "before-index-sync", Values: mongoIndexedValues("before", "acme", "before"),
	}); err == nil || !strings.Contains(err.Error(), "not verified") {
		mongoRollback(t, unverified)
		t.Fatalf("unverified indexed write error = %v", err)
	}
	mongoRollback(t, unverified)

	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("sync MongoDB indexes: %v", err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("verify MongoDB indexes: %v", err)
	}

	first, err := mongoIndexedCreate(t.Context(), backend, collection, "first", mongoIndexedValues("alpha", "acme", "welcome"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = mongoIndexedCreate(t.Context(), backend, collection, "duplicate-field", mongoIndexedValues("alpha", "other", "other"))
	assertMongoRedactedConflict(t, err, physicalCollectionName(collection.ID), "alpha", "duplicate-field")
	_, err = mongoIndexedCreate(t.Context(), backend, collection, "duplicate-compound", mongoIndexedValues("beta", "acme", "welcome"))
	assertMongoRedactedConflict(t, err, physicalCollectionName(collection.ID), "acme", "welcome", "duplicate-compound")
	if _, err := mongoIndexedCreate(t.Context(), backend, collection, "update-source", mongoIndexedValues("gamma", "other", "other")); err != nil {
		t.Fatal(err)
	}
	_, err = mongoIndexedUpdate(t.Context(), backend, collection, "update-source", store.Values{"code": store.String("alpha")})
	assertMongoRedactedConflict(t, err, physicalCollectionName(collection.ID), "alpha", "update-source")

	for _, fixture := range []struct {
		id     string
		values store.Values
	}{
		{id: "missing-one", values: store.Values{"rank": store.Number(1)}},
		{id: "missing-two", values: store.Values{"rank": store.Number(2)}},
		{id: "null-one", values: store.Values{"code": store.Null(), "tenant": store.Null(), "rank": store.Number(3)}},
		{id: "null-two", values: store.Values{"code": store.Null(), "tenant": store.Null(), "rank": store.Number(4)}},
	} {
		if _, err := mongoIndexedCreate(t.Context(), backend, collection, fixture.id, fixture.values); err != nil {
			t.Fatalf("NULLS DISTINCT create %q: %v", fixture.id, err)
		}
	}

	if _, err := mongoIndexedTrash(t.Context(), backend, collection, first.ID, false); err != nil {
		t.Fatalf("trash unique owner: %v", err)
	}
	if _, err := mongoIndexedCreate(t.Context(), backend, collection, "replacement", mongoIndexedValues("alpha", "acme", "welcome")); err != nil {
		t.Fatalf("reuse trashed unique values: %v", err)
	}
	_, err = mongoIndexedTrash(t.Context(), backend, collection, first.ID, true)
	assertMongoRedactedConflict(t, err, physicalCollectionName(collection.ID), "alpha", "welcome", first.ID)
}

func TestMongoDBUniqueIndexAllowsExactlyOneConcurrentWinner(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoIndexTestCollection(t)
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		id := id
		go func() {
			<-start
			_, err := mongoIndexedCreate(ctx, backend, collection, id, mongoIndexedValues("same", "tenant", "slug"))
			results <- err
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for index := 0; index < 2; index++ {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrConflict):
			assertMongoRedactedConflict(t, err, physicalCollectionName(collection.ID), "same", "tenant", "slug")
			conflicts++
		default:
			t.Fatalf("concurrent unique create error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent unique results = %d success, %d conflicts; want 1 and 1", successes, conflicts)
	}
}

func TestMongoDBLocalizedIndexesAreExactPerLocaleAndRaceSafe(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoLocalizedIndexTestCollection(t)
	locales := []schema.LocaleCode{"en", "fr"}
	manifest := mongoLocalizedIndexTestManifest(collection)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("sync localized MongoDB indexes: %v", err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("verify localized MongoDB indexes: %v", err)
	}
	plans, err := mongoIndexPlans(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].definitions) != 11 {
		t.Fatalf("localized physical index plan = %#v", plans)
	}
	actual, err := backend.readCollectionIndexes(t.Context(), plans[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := compareMongoIndexSets(collection, plans[0].definitions, actual, false); err != nil {
		t.Fatalf("exact localized MongoDB indexes: %v", err)
	}
	for _, definition := range plans[0].definitions {
		for _, key := range definition.keys {
			if strings.Contains(key.Key, ".en") && strings.Contains(key.Key, ".fr") {
				t.Fatalf("localized physical index crosses locales: %#v", definition)
			}
		}
	}

	wrongLocales := mongoBegin(t, backend, false)
	if _, err := wrongLocales.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "wrong-locale-order",
		Locales: []schema.LocaleCode{"fr", "en"},
		Values:  mongoLocalizedIndexedValues("wrong-locale-order", "wrong", store.Values{"en": store.String("wrong")}, store.Values{"en": store.String("wrong")}),
	}); err == nil || !strings.Contains(err.Error(), "locale configuration") {
		mongoRollback(t, wrongLocales)
		t.Fatalf("locale-order drift write error = %v", err)
	}
	mongoRollback(t, wrongLocales)

	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "cross-locale-a",
		mongoLocalizedIndexedValues("cross-locale-a", "cross-a", store.Values{"en": store.String("shared"), "fr": store.String("fr-a")}, store.Values{"en": store.String("title-a")})); err != nil {
		t.Fatal(err)
	}
	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "cross-locale-b",
		mongoLocalizedIndexedValues("cross-locale-b", "cross-b", store.Values{"en": store.String("en-b"), "fr": store.String("shared")}, store.Values{"fr": store.String("title-b")})); err != nil {
		t.Fatalf("same unique value in different locales: %v", err)
	}

	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "compound-a",
		mongoLocalizedIndexedValues("compound-a", "acme", store.Values{"en": store.String("compound-code-a")}, store.Values{"en": store.String("same"), "fr": store.String("other")})); err != nil {
		t.Fatal(err)
	}
	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "compound-b",
		mongoLocalizedIndexedValues("compound-b", "acme", store.Values{"en": store.String("compound-code-b")}, store.Values{"en": store.String("other"), "fr": store.String("same")})); err != nil {
		t.Fatalf("same compound value in different locales: %v", err)
	}
	_, err = mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "compound-duplicate",
		mongoLocalizedIndexedValues("compound-duplicate", "acme", store.Values{"en": store.String("compound-code-duplicate")}, store.Values{"en": store.String("same")}))
	assertMongoRedactedConflict(t, err, physicalCollectionName(collection.ID), "acme", "same", "compound-duplicate")
	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "empty-string-owner",
		mongoLocalizedIndexedValues("empty-string-owner", "empty-owner", store.Values{"en": store.String("")}, store.Values{"en": store.String("empty-title-owner")})); err != nil {
		t.Fatal(err)
	}
	_, err = mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "empty-string-duplicate",
		mongoLocalizedIndexedValues("empty-string-duplicate", "empty-duplicate", store.Values{"en": store.String("")}, store.Values{"en": store.String("empty-title-duplicate")}))
	assertMongoRedactedConflict(t, err, physicalCollectionName(collection.ID), "empty-string-duplicate", "empty-title-duplicate")

	for _, fixture := range []struct {
		id    string
		code  store.Values
		title store.Values
	}{
		{id: "localized-missing-a"},
		{id: "localized-missing-b"},
		{id: "localized-null-a", code: store.Values{"en": store.Null()}, title: store.Values{"en": store.Null()}},
		{id: "localized-null-b", code: store.Values{"en": store.Null()}, title: store.Values{"en": store.Null()}},
	} {
		if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, fixture.id,
			mongoLocalizedIndexedValues(fixture.id, "nulls-distinct", fixture.code, fixture.title)); err != nil {
			t.Fatalf("localized NULLS DISTINCT create %q: %v", fixture.id, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{"localized-race-a", "localized-race-b"} {
		id := id
		go func() {
			<-start
			_, err := mongoLocalizedIndexedCreate(ctx, backend, collection, locales, id,
				mongoLocalizedIndexedValues(id, id, store.Values{"en": store.String("localized-race")}, store.Values{"en": store.String(id)}))
			results <- err
		}()
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrConflict):
			assertMongoRedactedConflict(t, err, physicalCollectionName(collection.ID), "localized-race")
			conflicts++
		default:
			t.Fatalf("localized concurrent unique create: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("localized concurrent unique results = %d success, %d conflicts; want 1 and 1", successes, conflicts)
	}

	owner, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "localized-trash-owner",
		mongoLocalizedIndexedValues("localized-trash-owner", "trash-owner", store.Values{"en": store.String("trash-reuse")}, store.Values{"en": store.String("trash-title")}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mongoLocalizedIndexedTrash(t.Context(), backend, collection, locales, owner.ID, false); err != nil {
		t.Fatalf("trash localized unique owner: %v", err)
	}
	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "localized-trash-replacement",
		mongoLocalizedIndexedValues("localized-trash-replacement", "trash-replacement", store.Values{"en": store.String("trash-reuse")}, store.Values{"en": store.String("replacement-title")})); err != nil {
		t.Fatalf("reuse trashed localized unique value: %v", err)
	}
	_, err = mongoLocalizedIndexedTrash(t.Context(), backend, collection, locales, owner.ID, true)
	assertMongoRedactedConflict(t, err, physicalCollectionName(collection.ID), "trash-reuse", owner.ID)

	var dropped string
	for _, definition := range plans[0].definitions {
		if len(definition.keys) != 0 && definition.keys[0].Key == "values.code.en" {
			dropped = definition.name
			break
		}
	}
	if dropped == "" {
		t.Fatal("localized unique index was not planned")
	}
	if err := backend.database.Collection(plans[0].physicalName).Indexes().DropOne(t.Context(), dropped); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err == nil || !strings.Contains(err.Error(), "required index") {
		t.Fatalf("missing localized index verification error = %v", err)
	}
	if err := backend.requireVerifiedIndexesForLocales(collection, locales); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("failed localized verification retained runtime authorization: %v", err)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("restore localized MongoDB index: %v", err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("final localized MongoDB verification: %v", err)
	}
}

func TestMongoDBNumericUniqueIndexesCanonicalizeSignedZeroAndRedactConflicts(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoSignedZeroIndexTestCollection(t)
	locales := []schema.LocaleCode{"en", "fr"}
	manifest := mongoLocalizedIndexTestManifest(collection)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	definitions, err := mongoDeclaredIndexesForLocales(collection, locales)
	if err != nil {
		t.Fatal(err)
	}
	commonSecrets := []string{physicalCollectionName(collection.ID)}
	for _, definition := range definitions {
		commonSecrets = append(commonSecrets, definition.name)
	}
	assertConflict := func(err error, secrets ...string) {
		t.Helper()
		assertMongoRedactedConflict(t, err, append(commonSecrets, secrets...)...)
	}

	negativeZero := math.Copysign(0, -1)
	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "direct-negative",
		store.Values{"single": store.Number(negativeZero)}); err != nil {
		t.Fatal(err)
	}
	_, err = mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "direct-positive",
		store.Values{"single": store.Number(0)})
	assertConflict(err, "direct-positive")
	mongoAssertStoredPositiveZero(t, backend, collection, "direct-negative", "values", "single")

	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "compound-negative",
		store.Values{"tenant": store.String("ordinary-zero"), "tuple": store.Number(negativeZero)}); err != nil {
		t.Fatal(err)
	}
	_, err = mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "compound-positive",
		store.Values{"tenant": store.String("ordinary-zero"), "tuple": store.Number(0)})
	assertConflict(err, "ordinary-zero", "compound-positive")
	mongoAssertStoredPositiveZero(t, backend, collection, "compound-negative", "values", "tuple")

	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "localized-negative",
		store.Values{"localizedSingle": store.Object(store.Values{"en": store.Number(negativeZero)})}); err != nil {
		t.Fatal(err)
	}
	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "localized-other-locale",
		store.Values{"localizedSingle": store.Object(store.Values{"fr": store.Number(0)})}); err != nil {
		t.Fatalf("same numeric identity in another locale: %v", err)
	}
	_, err = mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "localized-positive",
		store.Values{"localizedSingle": store.Object(store.Values{"en": store.Number(0)})})
	assertConflict(err, "localized-positive")
	mongoAssertStoredPositiveZero(t, backend, collection, "localized-negative", "values", "localizedSingle", "en")

	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "localized-update-source",
		store.Values{"localizedSingle": store.Object(store.Values{"en": store.Number(1)})}); err != nil {
		t.Fatal(err)
	}
	_, err = mongoLocalizedIndexedUpdate(t.Context(), backend, collection, locales, "localized-update-source",
		store.Values{"localizedSingle": store.Object(store.Values{"en": store.Number(0)})})
	assertConflict(err, "localized-update-source")

	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "localized-compound-negative",
		store.Values{
			"tenant":         store.String("localized-zero"),
			"localizedTuple": store.Object(store.Values{"en": store.Number(negativeZero)}),
		}); err != nil {
		t.Fatal(err)
	}
	_, err = mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "localized-compound-positive",
		store.Values{
			"tenant":         store.String("localized-zero"),
			"localizedTuple": store.Object(store.Values{"en": store.Number(0)}),
		})
	assertConflict(err, "localized-zero", "localized-compound-positive")
	if _, err := mongoLocalizedIndexedCreate(t.Context(), backend, collection, locales, "localized-compound-other-locale",
		store.Values{
			"tenant":         store.String("localized-zero"),
			"localizedTuple": store.Object(store.Values{"fr": store.Number(0)}),
		}); err != nil {
		t.Fatalf("same compound numeric identity in another locale: %v", err)
	}
	mongoAssertStoredPositiveZero(t, backend, collection, "localized-compound-negative", "values", "localizedTuple", "en")

	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("final signed-zero MongoDB index verification: %v", err)
	}
}

func TestMongoDBSyncIndexesRejectsExistingDuplicatesAndAllowsHarmlessIndexes(t *testing.T) {
	t.Run("existing duplicates", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		indexed := mongoIndexTestCollection(t)
		unindexed := indexed
		unindexed.Indexes = nil
		unindexed.Fields = cloneMongoIndexTestFields(indexed.Fields)
		unindexed.Fields[0].Unique = false
		unindexed.Fields[1].Index = false
		unindexed.Fields[3].Nested.ResolvedFields()[0].Index = false

		if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(unindexed)); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"duplicate-a", "duplicate-b"} {
			if _, err := mongoIndexedCreate(t.Context(), backend, unindexed, id, mongoIndexedValues("duplicate", "tenant", "slug")); err != nil {
				t.Fatal(err)
			}
		}
		err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(indexed))
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate-data index build error = %v, want ErrConflict", err)
		}
		var serverError mongo.ServerError
		if errors.As(err, &serverError) {
			t.Fatalf("duplicate-data index build exposes raw server error %T: %v", serverError, serverError)
		}
		definitions, planErr := mongoDeclaredIndexes(indexed)
		if planErr != nil {
			t.Fatal(planErr)
		}
		secrets := []string{physicalCollectionName(indexed.ID), "duplicate"}
		for _, definition := range definitions {
			secrets = append(secrets, definition.name)
		}
		for _, secret := range secrets {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("duplicate-data index build exposes %q: %v", secret, err)
			}
		}
		if err := backend.requireVerifiedIndexes(indexed); err == nil || !strings.Contains(err.Error(), "not verified") {
			t.Fatalf("failed unique build authorized indexed runtime: %v", err)
		}
	})

	t.Run("ordinary unmanaged non-unique index", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoIndexTestCollection(t)
		manifest := mongoIndexTestManifest(collection)
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatal(err)
		}
		_, err := backend.database.Collection(physicalCollectionName(collection.ID)).Indexes().CreateOne(
			t.Context(),
			mongo.IndexModel{
				Keys:    bson.D{{Key: "meta.updatedAt", Value: int64(1)}},
				Options: options.Index().SetName("out_of_band_index"),
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("verify with harmless unmanaged index: %v", err)
		}
		if err := backend.requireVerifiedIndexes(collection); err != nil {
			t.Fatalf("harmless unmanaged index did not authorize runtime: %v", err)
		}
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatalf("sync with harmless unmanaged index: %v", err)
		}
		if _, err := mongoIndexedCreate(
			t.Context(), backend, collection, "after-harmless-index", mongoIndexedValues("after", "tenant", "slug"),
		); err != nil {
			t.Fatalf("write with harmless unmanaged index: %v", err)
		}
		indexes, err := backend.database.Collection(physicalCollectionName(collection.ID)).Indexes().ListSpecifications(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(indexes, func(specification mongo.IndexSpecification) bool {
			return specification.Name == "out_of_band_index"
		}) {
			t.Fatal("additive sync removed harmless unmanaged index")
		}
	})
}

func TestMongoDBUnmanagedIndexHazardsRejectVerification(t *testing.T) {
	assertHazard := func(t *testing.T, backend *Store, manifest schema.Manifest, indexName string) {
		t.Helper()
		for _, verification := range []struct {
			name string
			run  func() error
		}{
			{name: "VerifyIndexes", run: func() error { return backend.VerifyIndexes(t.Context(), manifest) }},
			{name: "SyncIndexes", run: func() error { return backend.SyncIndexes(t.Context(), manifest) }},
		} {
			err := verification.run()
			if err == nil || !strings.Contains(err.Error(), indexName) ||
				!strings.Contains(err.Error(), "valid writes or data lifetime") {
				t.Fatalf("%s unmanaged-index hazard error = %v", verification.name, err)
			}
		}
	}

	t.Run("unique index changes valid writes", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "unmanaged-unique-hazard", "unmanaged-unique-hazard"
		collection.Fields = append([]schema.Field(nil), collection.Fields...)
		collection.Fields[0].Index = true
		manifest := mongoIndexTestManifest(collection)
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatal(err)
		}
		physical := backend.database.Collection(physicalCollectionName(collection.ID))
		const indexName = "application_unique_lookup"
		if _, err := physical.Indexes().CreateOne(t.Context(), mongo.IndexModel{
			Keys:    bson.D{{Key: "values.applicationUnique", Value: int32(1)}},
			Options: options.Index().SetName(indexName).SetUnique(true),
		}); err != nil {
			t.Fatal(err)
		}
		document := func(id string) bson.D {
			return bson.D{
				{Key: "_id", Value: id},
				{Key: "values", Value: bson.D{{Key: "applicationUnique", Value: "same-value"}}},
			}
		}
		if _, err := physical.InsertOne(t.Context(), document("first")); err != nil {
			t.Fatal(err)
		}
		if _, err := physical.InsertOne(t.Context(), document("second")); !mongo.IsDuplicateKeyError(err) {
			t.Fatalf("unmanaged unique index duplicate error = %v", err)
		}
		assertHazard(t, backend, manifest, indexName)
	})

	t.Run("TTL index changes data lifetime", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "unmanaged-ttl-hazard", "unmanaged-ttl-hazard"
		collection.Fields = append([]schema.Field(nil), collection.Fields...)
		collection.Fields[0].Index = true
		manifest := mongoIndexTestManifest(collection)
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatal(err)
		}
		const indexName = "application_expiry_lookup"
		if _, err := backend.database.Collection(physicalCollectionName(collection.ID)).Indexes().CreateOne(
			t.Context(),
			mongo.IndexModel{
				Keys:    bson.D{{Key: "meta.applicationExpiresAt", Value: int32(1)}},
				Options: options.Index().SetName(indexName).SetExpireAfterSeconds(0),
			},
		); err != nil {
			t.Fatal(err)
		}
		assertHazard(t, backend, manifest, indexName)
	})

	t.Run("2dsphere index rejects a valid text write", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		collection := mongoScalarCollection(false)
		collection.ID, collection.Slug = "unmanaged-2dsphere-hazard", "unmanaged-2dsphere-hazard"
		collection.Fields = append([]schema.Field(nil), collection.Fields...)
		collection.Fields[0].Index = true
		manifest := mongoIndexTestManifest(collection)
		if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
			t.Fatal(err)
		}
		const indexName = "application_location_lookup"
		if _, err := backend.database.Collection(physicalCollectionName(collection.ID)).Indexes().CreateOne(
			t.Context(),
			mongo.IndexModel{
				Keys:    bson.D{{Key: "values.title", Value: "2dsphere"}},
				Options: options.Index().SetName(indexName),
			},
		); err != nil {
			t.Fatal(err)
		}
		if _, err := mongoIndexedCreate(t.Context(), backend, collection, "ordinary-text", store.Values{
			"title": store.String("ordinary text"), "rank": store.Number(1),
		}); err == nil || !strings.Contains(err.Error(), "server code 16755") {
			t.Fatalf("Ridu-valid text write through unmanaged 2dsphere index = %v", err)
		}
		assertHazard(t, backend, manifest, indexName)
	})
}

func TestMongoDBSystemIndexesAreExplicitExactAndFailClosed(t *testing.T) {
	backend := mongoIntegrationStore(t)
	target, owner, versioned, manifest := mongoFrameworkIndexTestManifest(t)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	plans, err := mongoIndexPlans(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range mongoSystemIndexPlans(plans) {
		actual, err := backend.readNamedCollectionIndexes(t.Context(), plan.physicalName, plan.description)
		if err != nil {
			t.Fatal(err)
		}
		if err := compareMongoNamedIndexSets(plan.description, plan.definitions, actual, false); err != nil {
			t.Fatalf("verify exact framework index shape for %s: %v", plan.description, err)
		}
	}
	if err := backend.requireVerifiedReferenceIndexes(owner); err != nil {
		t.Fatal(err)
	}
	if err := backend.requireVerifiedVersionIndexes(versioned); err != nil {
		t.Fatal(err)
	}
	if err := backend.requireVerifiedPreferenceIndexes(); err != nil {
		t.Fatal(err)
	}
	if err := backend.requireVerifiedDocumentLockIndexes(); err != nil {
		t.Fatal(err)
	}
	if err := backend.requireVerifiedIndexes(target); err != nil {
		t.Fatal(err)
	}

	if err := backend.database.Collection(mongoReferenceCollectionName).Indexes().DropOne(t.Context(), mongoReferenceTargetIndexName); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err == nil || !strings.Contains(err.Error(), "required index") {
		t.Fatalf("missing reference index verification error = %v", err)
	}
	if err := backend.requireVerifiedReferenceIndexes(owner); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("failed reference verification retained authorization: %v", err)
	}
	if err := backend.requireVerifiedVersionIndexes(versioned); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("failed manifest verification retained version authorization: %v", err)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("restore missing reference index: %v", err)
	}

	if err := backend.database.Collection(mongoPreferenceCollectionName).Indexes().DropOne(t.Context(), mongoPreferenceOwnerIndexName); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err == nil || !strings.Contains(err.Error(), "required index") {
		t.Fatalf("missing preference index verification error = %v", err)
	}
	if err := backend.requireVerifiedPreferenceIndexes(); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("failed preference verification retained authorization: %v", err)
	}
	if err := backend.requireVerifiedDocumentLockIndexes(); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("failed manifest verification retained document-lock authorization: %v", err)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("restore missing preference index: %v", err)
	}
	if _, err := backend.database.Collection(mongoDocumentLockCollectionName).Indexes().CreateOne(
		t.Context(),
		mongo.IndexModel{
			Keys:    bson.D{{Key: "expiresAt", Value: int32(1)}},
			Options: options.Index().SetName("out_of_band_document_lock_index"),
		},
	); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("verify harmless unmanaged document-lock index: %v", err)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("sync harmless unmanaged document-lock index: %v", err)
	}

	if _, err := backend.database.Collection(physicalVersionCollectionName(versioned.ID)).Indexes().CreateOne(
		t.Context(),
		mongo.IndexModel{
			Keys:    bson.D{{Key: mongoVersionStatusPath, Value: int32(1)}},
			Options: options.Index().SetName("out_of_band_version_index"),
		},
	); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("verify harmless unmanaged version index: %v", err)
	}
}

func TestMongoDBPopulationRequiresTargetContentIndexVerification(t *testing.T) {
	backend := mongoIntegrationStore(t)
	target, owner, _, manifest := mongoFrameworkIndexTestManifest(t)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	write := mongoBegin(t, backend, false)
	targetDocument, err := write.Create(t.Context(), store.CreateRequest{
		Collection: target, ID: "target", Values: mongoIndexedValues("target", "tenant", "slug"),
	})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	ownerDocument, err := write.Create(t.Context(), store.CreateRequest{
		Collection: owner, ID: "owner", Values: store.Values{"target": store.String(targetDocument.ID)},
	})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	mongoCommit(t, write)

	backend.indexesMu.Lock()
	delete(backend.verifiedIndexes, target.ID)
	backend.indexesMu.Unlock()
	collections := map[schema.StableID]schema.Collection{
		target.ID: target, owner.ID: owner,
	}
	request := store.Request{
		Collection: owner, Collections: collections, ID: ownerDocument.ID,
		Populate: []query.Population{{Path: mongoIndexMustPath(t, "target"), Depth: 1}},
	}
	unverified := mongoBegin(t, backend, true)
	if _, err := unverified.Find(t.Context(), request); err == nil || !strings.Contains(err.Error(), "not verified") || !strings.Contains(err.Error(), string(target.ID)) {
		mongoRollback(t, unverified)
		t.Fatalf("unverified population target error = %v", err)
	}
	mongoRollback(t, unverified)

	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	verified := mongoBegin(t, backend, true)
	populated, err := verified.Find(t.Context(), request)
	if err != nil {
		mongoRollback(t, verified)
		t.Fatal(err)
	}
	resolved, ok := populated.Values["target"].CopyDocument()
	if !ok || resolved.ID != targetDocument.ID {
		mongoRollback(t, verified)
		t.Fatalf("verified population target = %#v", populated.Values["target"])
	}
	mongoCommit(t, verified)
}

func mongoIndexTestManifest(collection schema.Collection) schema.Manifest {
	return schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "MongoDB index tests"},
		Collections: []schema.Collection{collection},
		Plugins:     []schema.Plugin{},
	})
}

func mongoSignedZeroIndexTestCollection(t *testing.T) schema.Collection {
	t.Helper()
	single := mongoIndexMustPath(t, "single")
	tenant := mongoIndexMustPath(t, "tenant")
	tuple := mongoIndexMustPath(t, "tuple")
	localizedSingle := mongoIndexMustPath(t, "localizedSingle")
	localizedTuple := mongoIndexMustPath(t, "localizedTuple")
	return schema.Collection{
		ID: "signed-zero-indexes", Slug: "signed-zero-indexes",
		Labels: schema.CollectionLabels{Singular: "Signed zero index", Plural: "Signed zero indexes"},
		Fields: []schema.Field{
			{
				ID: "signed-zero-indexes-single", Name: "single", Path: single,
				Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Unique: true, Number: &schema.NumberField{},
			},
			{
				ID: "signed-zero-indexes-tenant", Name: "tenant", Path: tenant,
				Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
			},
			{
				ID: "signed-zero-indexes-tuple", Name: "tuple", Path: tuple,
				Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Number: &schema.NumberField{},
			},
			{
				ID: "signed-zero-indexes-localized-single", Name: "localizedSingle", Path: localizedSingle,
				Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Localized: true, Unique: true, Number: &schema.NumberField{},
			},
			{
				ID: "signed-zero-indexes-localized-tuple", Name: "localizedTuple", Path: localizedTuple,
				Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Localized: true, Number: &schema.NumberField{},
			},
		},
		Indexes: []schema.CollectionIndex{
			{Fields: []query.Path{tenant, tuple}, Unique: true},
			{Fields: []query.Path{tenant, localizedTuple}, Unique: true},
		},
	}
}

func mongoFrameworkIndexTestManifest(t *testing.T) (schema.Collection, schema.Collection, schema.Collection, schema.Manifest) {
	t.Helper()
	target := mongoIndexTestCollection(t)
	targetPath := mongoIndexMustPath(t, "target")
	owner := schema.Collection{
		ID: "framework-index-owners", Slug: "framework-index-owners",
		Labels: schema.CollectionLabels{Singular: "Owner", Plural: "Owners"},
		Fields: []schema.Field{{
			ID: "framework-index-owners-target", Name: "target", Path: targetPath,
			Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
			Relationship: &schema.RelationshipField{
				CollectionID: target.ID, CollectionSlug: target.Slug, OnDelete: schema.ReferenceDeleteNullify,
			},
		}},
	}
	versioned := mongoVersionedCollection(true, 3)
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "MongoDB framework index tests"},
		Collections: []schema.Collection{target, owner, versioned}, Plugins: []schema.Plugin{},
	})
	return target, owner, versioned, manifest
}

func mongoIndexedValues(code, tenant, slug string) store.Values {
	return store.Values{
		"code": store.String(code), "tenant": store.String(tenant), "rank": store.Number(1),
		"seo": store.Object(store.Values{"slug": store.String(slug)}),
	}
}

func mongoLocalizedIndexTestManifest(collection schema.Collection) schema.Manifest {
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{
			Name: "MongoDB localized index tests",
			Localization: &schema.LocalizationSettings{
				DefaultLocale: "en",
				Locales: []schema.Locale{
					{Code: "en", Label: "English"},
					{Code: "fr", Label: "French"},
				},
			},
		},
		Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
	})
}

func mongoLocalizedIndexedValues(id, tenant string, code, title store.Values) store.Values {
	values := store.Values{
		"tenant": store.String(tenant),
		"seo": store.Object(store.Values{
			"headline": store.Object(store.Values{"en": store.String("headline-" + id)}),
		}),
	}
	if code != nil {
		values["code"] = store.Object(code)
	}
	if title != nil {
		values["title"] = store.Object(title)
	}
	return values
}

func mongoLocalizedIndexedCreate(
	ctx context.Context,
	backend *Store,
	collection schema.Collection,
	locales []schema.LocaleCode,
	id string,
	values store.Values,
) (store.Document, error) {
	transaction, err := backend.Begin(ctx)
	if err != nil {
		return store.Document{}, err
	}
	document, err := transaction.Create(ctx, store.CreateRequest{
		Collection: collection, ID: id, Values: values, Locales: locales,
	})
	if err != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return store.Document{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func mongoLocalizedIndexedUpdate(
	ctx context.Context,
	backend *Store,
	collection schema.Collection,
	locales []schema.LocaleCode,
	id string,
	values store.Values,
) (store.Document, error) {
	transaction, err := backend.Begin(ctx)
	if err != nil {
		return store.Document{}, err
	}
	document, err := transaction.Update(ctx, store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: id, Locales: locales}, Values: values,
	})
	if err != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return store.Document{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func mongoAssertStoredPositiveZero(t *testing.T, backend *Store, collection schema.Collection, id string, path ...string) {
	t.Helper()
	raw, err := backend.database.Collection(physicalCollectionName(collection.ID)).FindOne(
		t.Context(), bson.D{{Key: "_id", Value: id}},
	).Raw()
	if err != nil {
		t.Fatal(err)
	}
	if len(path) == 0 {
		t.Fatal("stored MongoDB number path is required")
	}
	value := raw.Lookup(path[0])
	for _, segment := range path[1:] {
		document, ok := value.DocumentOK()
		if !ok {
			t.Fatalf("stored MongoDB path %q is not a document", strings.Join(path, "."))
		}
		value = document.Lookup(segment)
	}
	number, ok := value.DoubleOK()
	if !ok || number != 0 || math.Signbit(number) {
		t.Fatalf("stored MongoDB number %q = %v (double=%v, signbit=%v), want positive zero double", strings.Join(path, "."), number, ok, math.Signbit(number))
	}
}

func mongoLocalizedIndexedTrash(
	ctx context.Context,
	backend *Store,
	collection schema.Collection,
	locales []schema.LocaleCode,
	id string,
	restore bool,
) (store.Document, error) {
	transaction, err := backend.Begin(ctx)
	if err != nil {
		return store.Document{}, err
	}
	request := store.Request{Collection: collection, ID: id, Locales: locales}
	var document store.Document
	if restore {
		document, err = transaction.Restore(ctx, request)
	} else {
		document, err = transaction.Trash(ctx, request)
	}
	if err != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return store.Document{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func mongoIndexedCreate(ctx context.Context, backend *Store, collection schema.Collection, id string, values store.Values) (store.Document, error) {
	transaction, err := backend.Begin(ctx)
	if err != nil {
		return store.Document{}, err
	}
	document, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: id, Values: values})
	if err != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return store.Document{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func mongoIndexedTrash(ctx context.Context, backend *Store, collection schema.Collection, id string, restore bool) (store.Document, error) {
	transaction, err := backend.Begin(ctx)
	if err != nil {
		return store.Document{}, err
	}
	request := store.Request{Collection: collection, ID: id}
	var document store.Document
	if restore {
		document, err = transaction.Restore(ctx, request)
	} else {
		document, err = transaction.Trash(ctx, request)
	}
	if err != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return store.Document{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func mongoIndexedUpdate(ctx context.Context, backend *Store, collection schema.Collection, id string, values store.Values) (store.Document, error) {
	transaction, err := backend.Begin(ctx)
	if err != nil {
		return store.Document{}, err
	}
	document, err := transaction.Update(ctx, store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: id}, Values: values,
	})
	if err != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return store.Document{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func cloneMongoIndexTestFields(fields []schema.Field) []schema.Field {
	cloned := append([]schema.Field(nil), fields...)
	for index := range cloned {
		if cloned[index].Nested != nil {
			nested := *cloned[index].Nested
			nested.Fields = cloneMongoIndexTestFields(nested.ResolvedFields())
			cloned[index].Nested = &nested
		}
	}
	return cloned
}
