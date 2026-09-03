package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestMongoDBV2SemanticMigrationLifecycleOnAuthenticatedReplicaSet is one
// opt-in production-shaped semantic proof. It intentionally shares the
// authenticated TLS replica-set fixture instead of growing another service.
func TestMongoDBV2SemanticMigrationLifecycleOnAuthenticatedReplicaSet(t *testing.T) {
	if os.Getenv(mongoDBProductionFixtureEnvironment) != "true" {
		t.Skip("set RIDU_MONGODB_PRODUCTION_FIXTURE_TEST=true to run the authenticated MongoDB semantic migration proof")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker is required for the MongoDB semantic migration fixture: %v", err)
	}
	fixture := newMongoDBProductionFixture(t)
	fixture.start(t)
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	fixture.bootstrap(ctx, t)

	applicationURL := fixture.applicationURL(fixture.caPath, fixture.applicationPassword, true)
	backend, err := OpenWithConfig(ctx, productionMongoDBStoreConfig(applicationURL, "ridu-semantic-migration-proof"))
	if err != nil {
		t.Fatalf("open authenticated MongoDB semantic migration Store: %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Errorf("close MongoDB semantic migration Store: %v", err)
		}
	})

	directory := filepath.Join(t.TempDir(), "migrations")
	before := mongoDBSemanticLiveBeforeManifest(t)
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("apply initial MongoDB artifact: %v", err)
	}
	seed := mongoDBSeedSemanticLiveState(t, ctx, backend, before)

	after := mongoDBSemanticLiveAfterManifest(t, before)
	renames := []ridumigration.Rename{
		{CollectionBefore: "authors", CollectionAfter: "members", Fields: []ridumigration.FieldRename{{Before: "name", After: "displayName"}}},
		{CollectionBefore: "empty-drafts", CollectionAfter: "empty-archive"},
	}
	if _, err := CreateArtifactWithOptions(ctx, directory, "rename-and-retire", after, time.Unix(2, 0), ArtifactOptions{
		AllowDestructive: true,
		Renames:          renames,
	}); err != nil {
		t.Fatal(err)
	}
	mongoDBAssertSemanticRetirementTopology(t, directory)
	ordinary := RunnerOptions{LeaseDuration: 500 * time.Millisecond}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, ordinary); err == nil || !strings.Contains(err.Error(), "pending MongoDB semantic migrations require explicit maintenance admission") {
		t.Fatalf("pending MongoDB semantic migration without maintenance admission = %v", err)
	}
	if count, err := backend.database.Collection(physicalCollectionName("authors")).CountDocuments(ctx, bson.D{}); err != nil || count == 0 {
		t.Fatalf("rejected MongoDB semantic migration changed its source collection: count=%d error=%v", count, err)
	}
	if names, err := backend.database.ListCollectionNames(ctx, bson.D{{Key: "name", Value: physicalCollectionName("members")}}); err != nil || len(names) != 0 {
		t.Fatalf("rejected MongoDB semantic migration created its target collection: names=%v error=%v", names, err)
	}
	maintenance := RunnerOptions{AllowMaintenance: true, LeaseDuration: 500 * time.Millisecond}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, maintenance); err != nil {
		t.Fatalf("apply MongoDB rename and retirement artifact: %v", err)
	}
	mongoDBAssertSemanticLiveRenameAndRetirement(t, ctx, backend, before, after, seed)

	settings := mongoDBSemanticLiveCollection(t, after, "settings")
	descriptor := ridumigration.DataTransformDescriptor{
		Name:     "normalize-settings-note",
		Checksum: ridumigration.DataTransformChecksum([]byte("normalize settings.note from before to after; v1")),
	}
	injected := errors.New("injected semantic transform failure")
	failNext := true
	transform := ridumigration.DataTransform{
		DataTransformDescriptor: descriptor,
		Up: func(callbackContext context.Context, transaction ridumigration.DataTransaction) error {
			page, err := transaction.List(callbackContext, store.Request{Collection: settings, Page: 1, Limit: 10})
			if err != nil {
				return err
			}
			for _, document := range page.Documents {
				if document.ID != seed.settingsID {
					continue
				}
				if _, err := transaction.Update(callbackContext, store.UpdateRequest{
					Request: store.Request{Collection: settings, ID: document.ID, ExpectedRevision: document.Revision},
					Values:  store.Values{"note": store.String("after-transform")},
				}); err != nil {
					return err
				}
				if failNext {
					failNext = false
					return injected
				}
			}
			return nil
		},
		Down: func(context.Context, ridumigration.DataTransaction) error { return nil },
	}
	if _, err := CreateArtifactWithOptions(ctx, directory, "normalize-settings", after, time.Unix(3, 0), ArtifactOptions{
		DataTransforms: []ridumigration.DataTransformDescriptor{descriptor},
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, maintenance, transform); !errors.Is(err, injected) {
		t.Fatalf("failed MongoDB transform = %v, want injected callback error", err)
	}
	if note := mongoDBSemanticLiveDocumentText(t, ctx, backend, settings, seed.settingsID, "note"); note != "before-transform" {
		t.Fatalf("rolled-back transform note = %q", note)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	if statuses[len(statuses)-1].Applied {
		t.Fatalf("failed transform was checkpointed as applied: %#v", statuses[len(statuses)-1])
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, ordinary, transform); err == nil || !strings.Contains(err.Error(), "pending MongoDB semantic migrations require explicit maintenance admission") {
		t.Fatalf("interrupted MongoDB semantic migration without maintenance admission = %v", err)
	}
	if note := mongoDBSemanticLiveDocumentText(t, ctx, backend, settings, seed.settingsID, "note"); note != "before-transform" {
		t.Fatalf("maintenance-rejected interrupted transform note = %q", note)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, maintenance, transform); err != nil {
		t.Fatalf("resume checksum-bound MongoDB transform: %v", err)
	}
	if note := mongoDBSemanticLiveDocumentText(t, ctx, backend, settings, seed.settingsID, "note"); note != "after-transform" {
		t.Fatalf("resumed transform note = %q", note)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, ordinary, transform); err != nil {
		t.Fatalf("idempotent applied MongoDB semantic history without maintenance admission: %v", err)
	}

	additiveSnapshot := after.Snapshot()
	for index := range additiveSnapshot.Collections {
		if additiveSnapshot.Collections[index].Slug == "settings" {
			additiveSnapshot.Collections[index].Fields = append(additiveSnapshot.Collections[index].Fields,
				mongoDBMigrationTextField(t, "settings-search-key", "searchKey", false, true, false),
			)
		}
	}
	additive := schema.NewManifest(additiveSnapshot)
	if _, err := CreateArtifactWithOptions(ctx, directory, "add-settings-index", additive, time.Unix(4, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, ordinary, transform); err != nil {
		t.Fatalf("apply additive MongoDB migration after historical semantic work without maintenance admission: %v", err)
	}
	if err := backend.Ready(ctx, additive); err != nil {
		t.Fatalf("MongoDB semantic migration readiness: %v", err)
	}

	verificationConfig := productionMongoDBStoreConfig(fixture.seedURL(fixture.caPath, true), "ridu-semantic-shadow-proof")
	if err := VerifyArtifactsWithOptions(ctx, verificationConfig, directory, ordinary, transform); err == nil || !strings.Contains(err.Error(), "semantic migrations require explicit maintenance admission before verification") {
		t.Fatalf("clean-shadow MongoDB semantic verification without maintenance admission = %v", err)
	}
	if err := VerifyArtifactsWithOptions(ctx, verificationConfig, directory, maintenance, transform); err != nil {
		t.Fatalf("verify MongoDB v2 history in authenticated shadow database: %v", err)
	}
}

type mongoDBSemanticLiveSeed struct {
	authorID             string
	queuedAuthorID       string
	versionOnlyAuthorID  string
	authorToken          store.AuthToken
	authorTaskID         string
	scheduledRunningTask string
	scheduledQueuedTask  string
	retiredID            string
	retiredToken         store.AuthToken
	retiredTaskID        string
	settingsID           string
	stateTime            time.Time
}

func mongoDBSemanticLiveBeforeManifest(t *testing.T) schema.Manifest {
	t.Helper()
	manifest, err := ridu.Resolve(ridu.Config{
		Name:  "MongoDB semantic migration proof",
		Admin: ridu.AdminConfig{User: "authors"},
		Collections: []ridu.Collection{
			{Slug: "empty-drafts", Fields: []field.Definition{field.Text("note")}},
			{
				Slug: "authors", Auth: true, Versions: true, LockDocuments: true,
				Fields: []field.Definition{field.Email("email", field.Required(), field.Unique()), field.Text("name", field.Required())},
			},
			{
				Slug: "retired-users", Auth: true, Versions: true, LockDocuments: true,
				Fields: []field.Definition{
					field.Email("email", field.Required(), field.Unique()),
					field.Relationship("mentor", field.To("authors"), field.OnDelete(field.ReferenceDeleteRestrict)),
				},
			},
			{
				Slug: "posts",
				Fields: []field.Definition{
					field.Text("title", field.Required()),
					field.Relationship("author", field.To("authors"), field.OnDelete(field.ReferenceDeleteRestrict)),
					field.Group("metadata", field.Fields(
						field.Text("relationTo"),
						field.Relationship("subject", field.ToAny("authors", "settings"), field.OnDelete(field.ReferenceDeleteRestrict)),
					)),
				},
			},
			{Slug: "settings", Fields: []field.Definition{field.Text("note")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func mongoDBSemanticLiveAfterManifest(t *testing.T, before schema.Manifest) schema.Manifest {
	t.Helper()
	snapshot := before.Snapshot()
	collections := make([]schema.Collection, 0, len(snapshot.Collections)-1)
	for _, collection := range snapshot.Collections {
		switch collection.Slug {
		case "empty-drafts":
			collection.ID, collection.Slug = "empty-archive", "empty-archive"
		case "authors":
			collection.ID, collection.Slug = "members", "members"
			collection.Labels = schema.CollectionLabels{Singular: "Member", Plural: "Members"}
			for fieldIndex := range collection.Fields {
				if collection.Fields[fieldIndex].Name != "name" {
					continue
				}
				collection.Fields[fieldIndex].ID = "members-display-name"
				collection.Fields[fieldIndex].Name = "displayName"
				collection.Fields[fieldIndex].Path = mongoDBMigrationPath(t, "displayName")
				collection.Fields[fieldIndex].Index = true
			}
		case "retired-users":
			continue
		case "posts":
			mongoDBSemanticLiveRenameAuthorRelationshipTargets(collection.Fields)
		}
		collections = append(collections, collection)
	}
	snapshot.Collections = collections
	if snapshot.Application.Admin != nil {
		admin := *snapshot.Application.Admin
		admin.UserCollectionID, admin.UserCollectionSlug = "members", "members"
		snapshot.Application.Admin = &admin
	}
	return schema.NewManifest(snapshot)
}

func mongoDBSemanticLiveRenameAuthorRelationshipTargets(fields []schema.Field) {
	for fieldIndex := range fields {
		if relationship := fields[fieldIndex].Relationship; relationship != nil {
			copy := *relationship
			if copy.Polymorphic {
				copy.Targets = append([]schema.RelationshipTarget(nil), relationship.Targets...)
				for targetIndex := range copy.Targets {
					if copy.Targets[targetIndex].CollectionID == "authors" {
						copy.Targets[targetIndex].CollectionID, copy.Targets[targetIndex].CollectionSlug = "members", "members"
					}
				}
			} else if copy.CollectionID == "authors" {
				copy.CollectionID, copy.CollectionSlug = "members", "members"
			}
			fields[fieldIndex].Relationship = &copy
		}
		if fields[fieldIndex].Nested != nil {
			nested := *fields[fieldIndex].Nested
			nested.Fields = append([]schema.Field(nil), nested.Fields...)
			mongoDBSemanticLiveRenameAuthorRelationshipTargets(nested.Fields)
			fields[fieldIndex].Nested = &nested
		}
		if fields[fieldIndex].Blocks != nil {
			blocks := *fields[fieldIndex].Blocks
			blocks.Types = append([]schema.BlockType(nil), blocks.Types...)
			for blockIndex := range blocks.Types {
				blocks.Types[blockIndex].Fields = append([]schema.Field(nil), blocks.Types[blockIndex].Fields...)
				mongoDBSemanticLiveRenameAuthorRelationshipTargets(blocks.Types[blockIndex].Fields)
			}
			fields[fieldIndex].Blocks = &blocks
		}
	}
}

func mongoDBSeedSemanticLiveState(t *testing.T, ctx context.Context, backend *Store, manifest schema.Manifest) mongoDBSemanticLiveSeed {
	t.Helper()
	authors := mongoDBSemanticLiveCollection(t, manifest, "authors")
	retired := mongoDBSemanticLiveCollection(t, manifest, "retired-users")
	posts := mongoDBSemanticLiveCollection(t, manifest, "posts")
	settings := mongoDBSemanticLiveCollection(t, manifest, "settings")
	seed := mongoDBSemanticLiveSeed{
		authorID: "author-1", queuedAuthorID: "author-queued", versionOnlyAuthorID: "author-version-only",
		retiredID: "retired-1", settingsID: "settings-1",
		stateTime: time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC),
	}
	transaction := mongoBegin(t, backend, false)
	author, err := transaction.Create(ctx, store.CreateRequest{
		Collection: authors, ID: seed.authorID, Status: store.StatusPublished,
		Values: store.Values{"email": store.String("author@example.test"), "name": store.String("Ada")},
	})
	if err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	if _, err := transaction.(store.VersionTransaction).SaveVersion(ctx, authors, author, 10); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{
		Collection: authors, ID: seed.queuedAuthorID, Status: store.StatusPublished,
		Values: store.Values{"email": store.String("queued@example.test"), "name": store.String("Queued")},
	}); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	versionOnly, err := transaction.Create(ctx, store.CreateRequest{
		Collection: authors, ID: seed.versionOnlyAuthorID, Status: store.StatusPublished,
		Values: store.Values{"email": store.String("version@example.test"), "name": store.String("Version only")},
	})
	if err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	if _, err := transaction.(store.VersionTransaction).SaveVersion(ctx, authors, versionOnly, 10); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	retiredDocument, err := transaction.Create(ctx, store.CreateRequest{
		Collection: retired, ID: seed.retiredID, Status: store.StatusPublished,
		Values: store.Values{"email": store.String("retired@example.test"), "mentor": store.String(seed.authorID)},
	})
	if err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	if _, err := transaction.(store.VersionTransaction).SaveVersion(ctx, retired, retiredDocument, 10); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{
		Collection: settings, ID: seed.settingsID, Values: store.Values{"note": store.String("before-transform")},
	}); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{
		Collection: posts, ID: "post-1",
		Values: store.Values{
			"title": store.String("Preserved post"), "author": store.String(seed.authorID),
			"metadata": store.Object(store.Values{
				"relationTo": store.String("authors"),
				"subject":    store.Object(store.Values{"relationTo": store.String("authors"), "id": store.String(seed.authorID)}),
			}),
		},
	}); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	mongoCommit(t, transaction)
	deleted, err := backend.database.Collection(physicalCollectionName(authors.ID)).DeleteOne(ctx, bson.D{{Key: "_id", Value: seed.versionOnlyAuthorID}})
	if err != nil {
		t.Fatalf("delete version-only current author: %v", err)
	}
	if deleted.DeletedCount != 1 {
		t.Fatalf("delete version-only current author: deleted %d documents, want 1", deleted.DeletedCount)
	}

	seed.authorToken = store.AuthToken{
		TokenHash: "author-reset-digest", Purpose: store.AuthTokenPasswordReset, CollectionID: authors.ID,
		UserID: seed.authorID, CreatedAt: seed.stateTime, ExpiresAt: seed.stateTime.Add(time.Hour),
	}
	if err := backend.CreateAuthToken(ctx, seed.authorToken); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.SetPreference(ctx, store.Preference{CollectionID: authors.ID, UserID: seed.authorID, Key: "theme", Value: []byte(`"preserved"`)}); err != nil {
		t.Fatal(err)
	}
	authorTask := mongoTaskFixture(seed.stateTime, "semantic-author", "authors:opaque")
	authorTask.Input = json.RawMessage(`{"collectionID":"authors","opaque":true}`)
	authorReference := store.DocumentReference{CollectionID: authors.ID, DocumentID: seed.authorID}
	authorTask.Target, authorTask.RequestedBy = &authorReference, &authorReference
	storedAuthorTask, err := backend.EnqueueTask(ctx, authorTask)
	if err != nil {
		t.Fatal(err)
	}
	seed.authorTaskID = storedAuthorTask.ID

	scheduledTask := func(targetID string, runAt time.Time) store.Task {
		input, err := json.Marshal(map[string]any{
			"collectionID": "authors", "documentID": targetID, "expectedRevision": 1,
			"requestedByCollectionID": "authors", "requestedByUserID": seed.authorID,
			"future": map[string]any{"keep": true},
		})
		if err != nil {
			t.Fatal(err)
		}
		task := mongoTaskFixture(runAt, mongoScheduledPublishTaskSlug, mongoScheduledPublishConcurrencyKey(authors.ID, targetID))
		task.Queue, task.Input = "scheduled-publish", input
		task.Target = &store.DocumentReference{CollectionID: authors.ID, DocumentID: targetID}
		task.RequestedBy = &store.DocumentReference{CollectionID: authors.ID, DocumentID: seed.authorID}
		return task
	}
	running, err := backend.EnqueueTask(ctx, scheduledTask(seed.authorID, time.Unix(1, 0)))
	if err != nil {
		t.Fatal(err)
	}
	queued, err := backend.EnqueueTask(ctx, scheduledTask(seed.queuedAuthorID, time.Unix(2, 0)))
	if err != nil {
		t.Fatal(err)
	}
	seed.scheduledRunningTask, seed.scheduledQueuedTask = running.ID, queued.ID
	claimed, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, Slugs: []string{mongoScheduledPublishTaskSlug}, LeaseDuration: 5 * time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].ID != running.ID {
		t.Fatalf("claim pre-migration scheduled publish = %#v, %v", claimed, err)
	}

	seed.retiredToken = store.AuthToken{
		TokenHash: "retired-reset-digest", Purpose: store.AuthTokenPasswordReset, CollectionID: retired.ID,
		UserID: seed.retiredID, CreatedAt: seed.stateTime, ExpiresAt: seed.stateTime.Add(time.Hour),
	}
	if err := backend.CreateAuthToken(ctx, seed.retiredToken); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.SetPreference(ctx, store.Preference{CollectionID: retired.ID, UserID: seed.retiredID, Key: "theme", Value: []byte(`"retired"`)}); err != nil {
		t.Fatal(err)
	}
	retiredTask := mongoTaskFixture(seed.stateTime, "semantic-retired", "")
	retiredReference := store.DocumentReference{CollectionID: retired.ID, DocumentID: seed.retiredID}
	retiredTask.Target, retiredTask.RequestedBy = &retiredReference, &retiredReference
	storedRetiredTask, err := backend.EnqueueTask(ctx, retiredTask)
	if err != nil {
		t.Fatal(err)
	}
	seed.retiredTaskID = storedRetiredTask.ID
	return seed
}

func mongoDBAssertSemanticRetirementTopology(t *testing.T, directory string) {
	t.Helper()
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	kinds := mongoDBMigrationStepKinds(files[len(files)-1].Artifact)
	retire, drop, create, assert := -1, -1, -1, -1
	for index, kind := range kinds {
		switch kind {
		case ridumigration.StepRetireResources:
			retire = index
		case ridumigration.StepMongoDBDropResources:
			drop = index
		case ridumigration.StepMongoDBCreateIndex:
			if create < 0 {
				create = index
			}
		case ridumigration.StepMongoDBAssertSchema:
			assert = index
		}
	}
	if retire < 0 || drop <= retire || create <= drop || assert <= create {
		t.Fatalf("MongoDB semantic retirement topology = %v", kinds)
	}
}

func mongoDBAssertSemanticLiveRenameAndRetirement(
	t *testing.T,
	ctx context.Context,
	backend *Store,
	before schema.Manifest,
	after schema.Manifest,
	seed mongoDBSemanticLiveSeed,
) {
	t.Helper()
	members := mongoDBSemanticLiveCollection(t, after, "members")
	posts := mongoDBSemanticLiveCollection(t, after, "posts")
	authors := mongoDBSemanticLiveCollection(t, before, "authors")
	retired := mongoDBSemanticLiveCollection(t, before, "retired-users")

	names, err := backend.database.ListCollectionNames(ctx, bson.D{{Key: "name", Value: bson.D{{Key: "$in", Value: bson.A{
		physicalCollectionName("empty-drafts"), physicalCollectionName("empty-archive"),
		physicalCollectionName(authors.ID), physicalVersionCollectionName(authors.ID),
		physicalCollectionName(retired.ID), physicalVersionCollectionName(retired.ID),
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("retired/renamed source or empty namespace remains: %v", names)
	}

	read := mongoBegin(t, backend, true)
	member, err := read.Find(ctx, store.Request{Collection: members, ID: seed.authorID})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if name, _ := member.Values["displayName"].StringValue(); name != "Ada" {
		mongoRollback(t, read)
		t.Fatalf("renamed current field = %q", name)
	}
	if _, exists := member.Values["name"]; exists {
		mongoRollback(t, read)
		t.Fatalf("renamed current document retained source field: %#v", member.Values)
	}
	versions, err := read.(store.VersionTransaction).ListVersions(ctx, store.VersionRequest{Collection: members, DocumentID: seed.authorID})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if len(versions) != 1 {
		mongoRollback(t, read)
		t.Fatalf("renamed version count = %d", len(versions))
	}
	if name, _ := versions[0].Snapshot.Values["displayName"].StringValue(); name != "Ada" {
		mongoRollback(t, read)
		t.Fatalf("renamed version field = %q", name)
	}
	if _, exists := versions[0].Snapshot.Values["name"]; exists {
		mongoRollback(t, read)
		t.Fatalf("renamed version retained source field: %#v", versions[0].Snapshot.Values)
	}
	versionOnly, err := read.(store.VersionTransaction).ListVersions(ctx, store.VersionRequest{Collection: members, DocumentID: seed.versionOnlyAuthorID})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if len(versionOnly) != 1 {
		mongoRollback(t, read)
		t.Fatalf("version-only renamed snapshot count = %d", len(versionOnly))
	}
	if name, _ := versionOnly[0].Snapshot.Values["displayName"].StringValue(); name != "Version only" {
		mongoRollback(t, read)
		t.Fatalf("version-only renamed field = %q", name)
	}
	if _, stale := versionOnly[0].Snapshot.Values["name"]; stale {
		mongoRollback(t, read)
		t.Fatalf("version-only snapshot retained source key: %#v", versionOnly[0].Snapshot.Values)
	}
	post, err := read.Find(ctx, store.Request{Collection: posts, ID: "post-1"})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	metadata, valid := post.Values["metadata"].ObjectValue()
	if !valid {
		mongoRollback(t, read)
		t.Fatalf("renamed post metadata = %#v", post.Values["metadata"])
	}
	if relationTo, _ := metadata["relationTo"].StringValue(); relationTo != "authors" {
		mongoRollback(t, read)
		t.Fatalf("authored relationTo field was rewritten = %q", relationTo)
	}
	subject, valid := metadata["subject"].ObjectValue()
	if !valid {
		mongoRollback(t, read)
		t.Fatalf("nested polymorphic subject = %#v", metadata["subject"])
	}
	if relationTo, _ := subject["relationTo"].StringValue(); relationTo != "members" {
		mongoRollback(t, read)
		t.Fatalf("nested polymorphic relationTo = %#v", subject)
	}
	mongoCommit(t, read)

	if count, err := backend.database.Collection(mongoReferenceCollectionName).CountDocuments(ctx, bson.D{
		{Key: "ownerCollection", Value: string(posts.ID)}, {Key: "ownerDocument", Value: "post-1"},
		{Key: "targetCollection", Value: string(members.ID)}, {Key: "targetDocument", Value: seed.authorID},
	}); err != nil || count != 2 {
		t.Fatalf("renamed reference rows = %d, %v", count, err)
	}
	if count, err := backend.database.Collection(mongoReferenceCollectionName).CountDocuments(ctx, bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "ownerCollection", Value: string(retired.ID)}}, bson.D{{Key: "targetCollection", Value: string(retired.ID)}},
	}}}); err != nil || count != 0 {
		t.Fatalf("retired reference rows = %d, %v", count, err)
	}

	token, found, err := findMongoAuthTokenByHash(ctx, backend.authTokenCollection(), seed.authorToken.TokenHash)
	if err != nil || !found || token.Token.CollectionID != members.ID {
		t.Fatalf("renamed auth token = %#v, %t, %v", token, found, err)
	}
	if count, err := backend.authTokenCollection().CountDocuments(ctx, bson.D{{Key: "tokenHash", Value: seed.authorToken.TokenHash}}); err != nil || count != 1 {
		t.Fatalf("renamed globally unique auth token rows = %d, %v", count, err)
	}
	preference, err := backend.GetPreference(ctx, members.ID, seed.authorID, "theme")
	if err != nil || string(preference.Value) != `"preserved"` {
		t.Fatalf("renamed preference = %#v, %v", preference, err)
	}
	task, err := backend.FindTask(ctx, seed.authorTaskID)
	if err != nil || task.Target == nil || task.RequestedBy == nil || task.Target.CollectionID != members.ID || task.RequestedBy.CollectionID != members.ID {
		t.Fatalf("renamed task = %#v, %v", task, err)
	}
	if task.ConcurrencyKey != "authors:opaque" || string(task.Input) != `{"collectionID":"authors","opaque":true}` {
		t.Fatalf("generic task opaque state was rewritten = %#v", task)
	}
	mongoDBAssertSemanticScheduledPublishState(t, ctx, backend, members, seed)

	if _, found, err := findMongoAuthTokenByHash(ctx, backend.authTokenCollection(), seed.retiredToken.TokenHash); err != nil || found {
		t.Fatalf("retired auth token found = %t, %v", found, err)
	}
	if _, err := backend.GetPreference(ctx, retired.ID, seed.retiredID, "theme"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retired preference = %v", err)
	}
	if _, err := backend.FindTask(ctx, seed.retiredTaskID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retired task = %v", err)
	}
}

func mongoDBAssertSemanticScheduledPublishState(t *testing.T, ctx context.Context, backend *Store, members schema.Collection, seed mongoDBSemanticLiveSeed) {
	t.Helper()
	targets := map[string]string{
		seed.scheduledRunningTask: seed.authorID,
		seed.scheduledQueuedTask:  seed.queuedAuthorID,
	}
	for taskID, targetID := range targets {
		task, err := backend.FindTask(ctx, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Target == nil || task.RequestedBy == nil || task.Target.CollectionID != members.ID || task.RequestedBy.CollectionID != members.ID {
			t.Fatalf("rewritten scheduled-publish references = %#v", task)
		}
		wantKey := mongoScheduledPublishConcurrencyKey(members.ID, targetID)
		if task.ConcurrencyKey != wantKey {
			t.Fatalf("rewritten scheduled-publish concurrency key = %q, want %q", task.ConcurrencyKey, wantKey)
		}
		var input map[string]any
		if err := json.Unmarshal(task.Input, &input); err != nil {
			t.Fatal(err)
		}
		future, _ := input["future"].(map[string]any)
		if input["collectionID"] != string(members.ID) || input["requestedByCollectionID"] != string(members.ID) || future["keep"] != true {
			t.Fatalf("rewritten scheduled-publish input = %#v", input)
		}
		raw, err := backend.taskCollection().FindOne(ctx, bson.D{{Key: "_id", Value: taskID}}).Raw()
		if err != nil {
			t.Fatal(err)
		}
		concurrencyID, _ := raw.Lookup("concurrencyID").StringValueOK()
		if want := mongoTaskConcurrencyID(task.Queue, wantKey); concurrencyID != want {
			t.Fatalf("rewritten scheduled-publish concurrency ID = %q, want %q", concurrencyID, want)
		}
	}

	runningKey := mongoScheduledPublishConcurrencyKey(members.ID, seed.authorID)
	runningGuardID := mongoTaskConcurrencyID("scheduled-publish", runningKey)
	rawGuard, err := backend.taskConcurrencyCollection().FindOne(ctx, bson.D{{Key: "_id", Value: runningGuardID}}).Raw()
	if err != nil {
		t.Fatal(err)
	}
	runningGuard, err := decodeMongoTaskConcurrencyGuard(rawGuard)
	if err != nil {
		t.Fatal(err)
	}
	if runningGuard.TaskID != seed.scheduledRunningTask || runningGuard.Key != runningKey || runningGuard.Target == nil ||
		runningGuard.RequestedBy == nil || runningGuard.Target.CollectionID != members.ID || runningGuard.RequestedBy.CollectionID != members.ID {
		t.Fatalf("rewritten scheduled-publish guard = %#v", runningGuard)
	}

	claimed, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, Slugs: []string{mongoScheduledPublishTaskSlug}, LeaseDuration: 5 * time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].ID != seed.scheduledQueuedTask {
		t.Fatalf("claim migrated queued scheduled publish = %#v, %v", claimed, err)
	}
	for _, targetID := range []string{seed.authorID, seed.queuedAuthorID} {
		oldKey := mongoScheduledPublishConcurrencyKey("authors", targetID)
		newKey := mongoScheduledPublishConcurrencyKey(members.ID, targetID)
		oldID := mongoTaskConcurrencyID("scheduled-publish", oldKey)
		newID := mongoTaskConcurrencyID("scheduled-publish", newKey)
		if count, err := backend.taskConcurrencyCollection().CountDocuments(ctx, bson.D{{Key: "_id", Value: oldID}}); err != nil || count != 0 {
			t.Fatalf("old scheduled-publish guard %s count = %d, %v", oldID, count, err)
		}
		if count, err := backend.taskConcurrencyCollection().CountDocuments(ctx, bson.D{{Key: "_id", Value: newID}}); err != nil || count != 1 {
			t.Fatalf("new scheduled-publish guard %s count = %d, %v", newID, count, err)
		}
	}
}

func mongoDBSemanticLiveCollection(t *testing.T, manifest schema.Manifest, slug schema.CollectionSlug) schema.Collection {
	t.Helper()
	for _, collection := range manifest.Snapshot().Collections {
		if collection.Slug == slug {
			return collection
		}
	}
	t.Fatalf("MongoDB semantic fixture collection %q is missing", slug)
	return schema.Collection{}
}

func mongoDBSemanticLiveDocumentText(t *testing.T, ctx context.Context, backend *Store, collection schema.Collection, id, fieldName string) string {
	t.Helper()
	raw, err := backend.database.Collection(physicalCollectionName(collection.ID)).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Raw()
	if err != nil {
		t.Fatal(err)
	}
	document, err := decodeCollectionDocument(raw, collection)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := document.Values[fieldName].StringValue()
	return value
}
