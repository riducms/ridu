package mongodb

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBGlobalUsesResourceStorePathAndSnapshotPredicates(t *testing.T) {
	backend := mongoIntegrationStore(t)
	siteName, err := query.NewPath("siteName")
	if err != nil {
		t.Fatal(err)
	}
	filtered := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(siteName, query.String("Ridu"))), nil
	}
	allowMissingRead := true
	allowInitialization := false
	config := ridu.Config{
		Name: "MongoDB global resource path",
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title")},
		}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Versions: true,
			Fields: field.Fields{field.Text("siteName").Required().Index(), field.Text("announcement").Default("Welcome")},
			Access: ridu.GlobalAccess{
				Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if allowMissingRead {
						return ridu.Allow(), nil
					}
					return filtered(ctx)
				},
				ReadVersions: filtered,
				Update: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if allowInitialization {
						return ridu.Allow(), nil
					}
					return filtered(ctx)
				},
			},
		}},
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	manifest := application.Manifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	global := snapshot.Globals[0]
	if !global.Capabilities.Global || global.ID != "global-site-settings" {
		t.Fatalf("resolved MongoDB global = %#v", global)
	}
	if physicalCollectionName(global.ID) == physicalCollectionName(snapshot.Collections[0].ID) {
		t.Fatal("MongoDB global shared a physical collection with an ordinary collection")
	}
	initial, err := application.Local().Global(t.Context(), "site-settings", ridu.FindOptions{})
	initialAnnouncement, _ := initial.Values["announcement"].StringValue()
	if err != nil || initial.ID != "site-settings" || initial.Revision != 0 || initialAnnouncement != "Welcome" {
		t.Fatalf("missing MongoDB global defaults = %#v, %v", initial, err)
	}
	if count, err := backend.database.Collection(physicalCollectionName(global.ID)).CountDocuments(t.Context(), bson.D{}); err != nil || count != 0 {
		t.Fatalf("missing MongoDB global persisted by read = %d, %v", count, err)
	}
	allowMissingRead = false

	if _, err := application.Local().UpdateGlobal(
		t.Context(), "site-settings", store.Values{"siteName": store.String("Ridu")}, ridu.MutationOptions{},
	); !mongoOperationCode(err, "not_found") {
		t.Fatalf("filtered first MongoDB global update = %v, want not_found", err)
	}
	allowInitialization = true
	created, err := application.Local().UpdateGlobal(
		t.Context(), "site-settings", store.Values{"siteName": store.String("Ridu")}, ridu.MutationOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	allowInitialization = false
	current, err := application.Local().Global(t.Context(), "site-settings", ridu.FindOptions{})
	if currentName, _ := current.Values["siteName"].StringValue(); err != nil || currentName != "Ridu" || current.ID != "site-settings" {
		t.Fatalf("matching MongoDB global = %#v, %v", current, err)
	}
	changed, err := application.Local().PublishGlobalChanges(
		t.Context(), "site-settings", store.Values{"siteName": store.String("Hidden")}, ridu.MutationOptions{ExpectedRevision: created.Revision},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Global(t.Context(), "site-settings", ridu.FindOptions{}); !mongoOperationCode(err, "not_found") {
		t.Fatalf("non-matching MongoDB global read = %v, want not_found", err)
	}
	if _, err := application.Local().PublishGlobalChanges(
		t.Context(), "site-settings", store.Values{"siteName": store.String("Ridu")}, ridu.MutationOptions{ExpectedRevision: changed.Revision},
	); !mongoOperationCode(err, "not_found") {
		t.Fatalf("non-matching MongoDB global update = %v, want not_found", err)
	}
	versions, err := application.Local().GlobalVersions(t.Context(), "site-settings", ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("filtered MongoDB global versions = %#v, want one visible snapshot", versions)
	}
	versionName, _ := versions[0].Snapshot.Values["siteName"].StringValue()
	if versions[0].Revision != 1 || versionName != "Ridu" {
		t.Fatalf("filtered MongoDB global versions = %#v", versions)
	}
	if _, err := application.Local().GlobalVersion(t.Context(), "site-settings", 2, ridu.FindOptions{}); !mongoOperationCode(err, "not_found") {
		t.Fatalf("non-matching MongoDB global version = %v, want not_found", err)
	}

	raw, err := backend.database.Collection(physicalCollectionName(global.ID)).FindOne(
		t.Context(), bson.D{{Key: "_id", Value: "site-settings"}},
	).Raw()
	if err != nil {
		t.Fatal(err)
	}
	if err := requireExactKeys(raw, "MongoDB global resource", "_id", "meta", "values"); err != nil {
		t.Fatal(err)
	}
	if count, err := backend.database.Collection(physicalVersionCollectionName(global.ID)).CountDocuments(
		t.Context(), bson.D{{Key: mongoVersionOwnerPath, Value: "site-settings"}},
	); err != nil || count != 2 {
		t.Fatalf("stored MongoDB global version count = %d, %v", count, err)
	}
}
