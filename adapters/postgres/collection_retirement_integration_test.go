package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresRemovedResourceStateCannotReattachAfterStableIDAndDocumentIDReuse(t *testing.T) {
	baseURL := os.Getenv("RIDU_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	backend := collectionRetirementBackend(t, ctx, baseURL)
	directory := t.TempDir()
	objectStorage, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	beforeConfig := collectionRetirementConfig(true)
	beforeConfig.Storage, beforeConfig.StorageNamespace = objectStorage, "collection-retirement"
	before, err := ridu.Resolve(beforeConfig)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := BuildArtifact(ctx, "initial-resource-state", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial-resource-state", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	beforeApp, err := ridu.New(beforeConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	ids := collectionRetirementResourceIDs(before.Snapshot())
	globalIDs := collectionRetirementGlobalIDs(before.Snapshot())

	keeperGlobal, err := beforeApp.Local().UpdateGlobal(ctx, "keeper-settings", store.Values{
		"title": store.String("Keeper settings"),
	}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	keeperGlobal, err = beforeApp.Local().PublishGlobal(ctx, "keeper-settings", keeperGlobal.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	retiredGlobal, err := beforeApp.Local().UpdateGlobal(ctx, "retired-settings", store.Values{
		"title": store.String("Old retired settings"),
	}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	retiredGlobal, err = beforeApp.Local().UpdateGlobal(ctx, "retired-settings", store.Values{
		"title": store.String("Old retired settings updated"),
	}, retiredGlobal.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := beforeApp.Local().PublishGlobal(ctx, "retired-settings", retiredGlobal.Revision, nil); err != nil {
		t.Fatal(err)
	}

	keeper, err := beforeApp.CreateAuthUser(ctx, "keepers", store.Values{"email": store.String("keeper@example.test")}, "keeper-password", nil)
	if err != nil {
		t.Fatal(err)
	}
	keeperSession, err := beforeApp.Login(ctx, "keepers", "keeper@example.test", "keeper-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.SetPreference(ctx, store.Preference{
		CollectionID: ids["keepers"], UserID: keeper.ID, Key: "retirement-control", Value: json.RawMessage(`{"kept":true}`),
	}); err != nil {
		t.Fatal(err)
	}

	mediaID := "surviving-media-document"
	oldObjectKey := "ridu/collection-retirement/objects/0123456789abcdef0123456789abcdef/old-object.txt"
	if err := objectStorage.Put(ctx, oldObjectKey, strings.NewReader("proof"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	media, err := beforeApp.Local().Import(ctx, "media", collectionRetirementUploadValues(oldObjectKey), ridu.ImportOptions{
		ID: mediaID, Status: store.StatusPublished,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	retiredUser, err := beforeApp.CreateAuthUser(ctx, "retired-users", store.Values{
		"email": store.String("old@example.test"), "avatar": store.String(mediaID),
	}, "old-password-value", nil)
	if err != nil {
		t.Fatal(err)
	}
	retiredUser, err = beforeApp.Local().Update(ctx, "retired-users", retiredUser.ID, store.Values{"email": store.String("old-updated@example.test")}, nil)
	if err != nil || retiredUser.Revision < 2 {
		t.Fatalf("seed retired user revision = %#v, %v", retiredUser, err)
	}
	retiredUser, err = beforeApp.Local().Publish(ctx, "retired-users", retiredUser.ID, retiredUser.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	retiredSession, err := beforeApp.Login(ctx, "retired-users", "old-updated@example.test", "old-password-value")
	if err != nil {
		t.Fatal(err)
	}
	retiredAPIKey, err := beforeApp.CreateAPIKey(ctx, retiredSession.Token, "retired deploy key", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.CreateAuthToken(ctx, store.AuthToken{
		TokenHash: "retired-reset-hash", CollectionID: ids["retired-users"], UserID: retiredUser.ID,
		Purpose: store.AuthTokenPasswordReset, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.SetPreference(ctx, store.Preference{
		CollectionID: ids["retired-users"], UserID: retiredUser.ID, Key: "old-preference", Value: json.RawMessage(`{"old":true}`),
	}); err != nil {
		t.Fatal(err)
	}

	entry, err := beforeApp.Local().Import(ctx, "entries", store.Values{
		"title": store.String("surviving owner"), "retiredUser": store.String(retiredUser.ID),
	}, ridu.ImportOptions{ID: "surviving-entry", Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatalf("seed surviving references: %#v", err)
	}
	hiddenSnapshot := before.Snapshot()
	for collectionIndex := range hiddenSnapshot.Collections {
		if hiddenSnapshot.Collections[collectionIndex].Slug != "entries" {
			continue
		}
		fields := hiddenSnapshot.Collections[collectionIndex].Fields
		hiddenSnapshot.Collections[collectionIndex].Fields = fields[:len(fields)-1]
	}
	hiddenReference := schema.NewManifest(hiddenSnapshot)
	_, hiddenReferenceError := BuildArtifact(ctx, "hide-reference-before-retirement", &before, hiddenReference, nil, true)
	var hiddenReferenceSafety *SafetyError
	if !errors.As(hiddenReferenceError, &hiddenReferenceSafety) ||
		!hasRiskCode(hiddenReferenceSafety.Risks, "RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE") {
		t.Fatalf("multi-artifact reference hiding was not rejected: %#v, %v", hiddenReferenceSafety, hiddenReferenceError)
	}
	entryVersions, err := beforeApp.Local().Versions(ctx, "entries", entry.ID, nil)
	if err != nil || len(entryVersions) == 0 || collectionRetirementString(entryVersions[0].Snapshot.Values["retiredUser"]) != retiredUser.ID {
		t.Fatalf("reference-bearing version before rejected transition = %#v, %v", entryVersions, err)
	}
	entry, err = beforeApp.Local().Restore(ctx, "entries", entry.ID, entryVersions[0].Revision, entry.Revision, nil)
	if err != nil || collectionRetirementString(entry.Values["retiredUser"]) != retiredUser.ID {
		t.Fatalf("reference-bearing version restore before retirement = %#v, %v", entry, err)
	}

	targetTask, err := backend.EnqueueTask(ctx, collectionRetirementTask(
		"retired-target-task", "application-retirement-proof",
		&store.DocumentReference{CollectionID: ids["retired-users"], DocumentID: retiredUser.ID},
		&store.DocumentReference{CollectionID: ids["keepers"], DocumentID: keeper.ID},
		json.RawMessage(`{"opaque":"application-owned"}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	requesterTask, err := backend.EnqueueTask(ctx, collectionRetirementTask(
		"retired-requester-task", "application-retirement-proof",
		&store.DocumentReference{CollectionID: ids["media"], DocumentID: media.ID},
		&store.DocumentReference{CollectionID: ids["retired-users"], DocumentID: retiredUser.ID},
		json.RawMessage(`{"opaque":"application-owned"}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	inputTargetTask, err := backend.EnqueueTask(ctx, collectionRetirementTask(
		"retired-input-target-task", "ridu-schedule-publish", nil, nil,
		json.RawMessage(fmt.Sprintf(`{"collectionID":%q,"documentID":%q,"requestedByCollectionID":%q,"requestedByUserID":%q,"expectedRevision":%d}`,
			ids["retired-users"], retiredUser.ID, ids["keepers"], keeper.ID, retiredUser.Revision)),
	))
	if err != nil {
		t.Fatal(err)
	}
	inputRequesterTask, err := backend.EnqueueTask(ctx, collectionRetirementTask(
		"retired-input-requester-task", "ridu-schedule-publish", nil, nil,
		json.RawMessage(fmt.Sprintf(`{"collectionID":%q,"documentID":%q,"requestedByCollectionID":%q,"requestedByUserID":%q,"expectedRevision":%d}`,
			ids["media"], media.ID, ids["retired-users"], retiredUser.ID, media.Revision)),
	))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, lock := range []store.DocumentLock{
		{CollectionID: ids["retired-users"], DocumentID: retiredUser.ID, OwnerCollectionID: ids["keepers"], OwnerID: keeper.ID},
		{CollectionID: ids["entries"], DocumentID: entry.ID, OwnerCollectionID: ids["retired-users"], OwnerID: retiredUser.ID},
	} {
		lock.OwnerLabel, lock.CreatedAt, lock.UpdatedAt, lock.ExpiresAt = "retirement proof", now, now, now.Add(time.Hour)
		if _, acquired, err := backend.AcquireDocumentLock(ctx, lock, now, false); err != nil || !acquired {
			t.Fatalf("seed document lock = %t, %v", acquired, err)
		}
	}
	var seededReferences int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_document_references
WHERE (owner_collection_id = $1 AND owner_document_id = $2 AND target_collection_id = $3)
   OR (owner_collection_id = $3 AND owner_document_id = $4 AND target_collection_id = $5)`,
		string(ids["entries"]), entry.ID, string(ids["retired-users"]), retiredUser.ID, string(ids["media"])).Scan(&seededReferences); err != nil || seededReferences != 2 {
		t.Fatalf("seeded relationship/upload references = %d, %v", seededReferences, err)
	}

	afterConfig := collectionRetirementConfig(false)
	afterConfig.Storage, afterConfig.StorageNamespace = objectStorage, "collection-retirement"
	after, err := ridu.Resolve(afterConfig)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := BuildArtifact(ctx, "remove-resources", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "remove-resources", removal, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); !errors.Is(err, ErrMaintenanceRequired) {
		t.Fatalf("resource removal without maintenance admission = %v", err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}
	assertRetiredFrameworkResourceState(t, ctx, backend, ids["retired-users"], globalIDs["retired-settings"])
	afterApp, err := ridu.New(afterConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	if current, err := afterApp.Session(ctx, keeperSession.Token); err != nil || current.User.ID != keeper.ID {
		t.Fatalf("unrelated auth session = %#v, %v", current, err)
	}
	preference, err := backend.GetPreference(ctx, ids["keepers"], keeper.ID, "retirement-control")
	var preferenceValue map[string]bool
	if err == nil {
		err = json.Unmarshal(preference.Value, &preferenceValue)
	}
	if err != nil || !preferenceValue["kept"] {
		t.Fatalf("unrelated preference = %#v, %v", preference, err)
	}
	if current, err := afterApp.Local().Global(ctx, "keeper-settings", nil); err != nil ||
		current.Revision != keeperGlobal.Revision || collectionRetirementString(current.Values["title"]) != "Keeper settings" {
		t.Fatalf("unrelated global = %#v, %v", current, err)
	}
	if versions, err := afterApp.Local().GlobalVersions(ctx, "keeper-settings", nil); err != nil || len(versions) != 2 {
		t.Fatalf("unrelated global versions = %#v, %v", versions, err)
	}

	readdedConfig := collectionRetirementConfig(true)
	readdedConfig.Storage, readdedConfig.StorageNamespace = objectStorage, "collection-retirement"
	readded, err := ridu.Resolve(readdedConfig)
	if err != nil {
		t.Fatal(err)
	}
	readdition, err := BuildArtifact(ctx, "readd-resources", &after, readded, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "readd-resources", readdition, time.Unix(3, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}
	readdedApp, err := ridu.New(readdedConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	var readdedGlobalRows int
	if err := backend.pool.QueryRow(ctx, "SELECT count(*) FROM "+quote(collectionTable(globalIDs["retired-settings"]))).Scan(&readdedGlobalRows); err != nil || readdedGlobalRows != 0 {
		t.Fatalf("readded retired global current rows before first update = %d, %v", readdedGlobalRows, err)
	}
	if versions, err := readdedApp.Local().GlobalVersions(ctx, "retired-settings", nil); err != nil || len(versions) != 0 {
		t.Fatalf("retired global versions attached after readdition = %#v, %v", versions, err)
	}
	newGlobal, err := readdedApp.Local().UpdateGlobal(ctx, "retired-settings", store.Values{
		"title": store.String("New retired settings"),
	}, 0, nil)
	if err != nil || newGlobal.Revision != 1 {
		t.Fatalf("reincarnated global = %#v, %v", newGlobal, err)
	}
	if versions, err := readdedApp.Local().GlobalVersions(ctx, "retired-settings", nil); err != nil || len(versions) != 1 ||
		collectionRetirementString(versions[0].Snapshot.Values["title"]) != "New retired settings" {
		t.Fatalf("reincarnated global versions = %#v, %v", versions, err)
	}
	if current, err := readdedApp.Local().Global(ctx, "keeper-settings", nil); err != nil ||
		current.Revision != keeperGlobal.Revision || collectionRetirementString(current.Values["title"]) != "Keeper settings" {
		t.Fatalf("unrelated global after readdition = %#v, %v", current, err)
	}
	if current, err := readdedApp.Local().Find(ctx, "entries", entry.ID, nil); err != nil {
		t.Fatalf("surviving reference owner after readdition: %v", err)
	} else if value, exists := current.Values["retiredUser"]; exists && value.Kind() != store.ValueNull {
		t.Fatalf("retired current relationship value reattached after field readdition: %#v", value)
	}
	if _, err := readdedApp.Local().Import(ctx, "retired-users", store.Values{"email": store.String("new@example.test")}, ridu.ImportOptions{
		ID: retiredUser.ID, Status: store.StatusPublished,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := readdedApp.Login(ctx, "retired-users", "new@example.test", "old-password-value"); err == nil {
		t.Fatal("retired password credential authenticated the reincarnated document")
	}
	if _, err := readdedApp.Session(ctx, retiredSession.Token); err == nil {
		t.Fatal("retired session authenticated the reincarnated document")
	}
	if _, err := readdedApp.AuthenticateAPIKey(ctx, retiredAPIKey.Key); err == nil {
		t.Fatal("retired API key authenticated the reincarnated document")
	}
	if _, err := backend.ResetPasswordWithToken(ctx, ids["retired-users"], "retired-reset-hash", []byte("replacement"), time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retired auth token attached to reincarnated document: %v", err)
	}
	if _, err := backend.GetPreference(ctx, ids["retired-users"], retiredUser.ID, "old-preference"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retired preference attached to reincarnated document: %v", err)
	}
	for _, taskID := range []string{targetTask.ID, requesterTask.ID, inputTargetTask.ID, inputRequesterTask.ID} {
		if _, err := backend.FindTask(ctx, taskID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("retired task %s attached to reincarnated document: %v", taskID, err)
		}
	}
	if _, err := backend.FindDocumentLock(ctx, ids["retired-users"], retiredUser.ID, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retired target lock attached after readdition: %v", err)
	}
	if _, err := backend.FindDocumentLock(ctx, ids["entries"], entry.ID, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retired owner lock attached after readdition: %v", err)
	}
	var userVersionCount, userMaxRevision, oldUserSnapshots int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*), COALESCE(max(revision), 0),
count(*) FILTER (WHERE snapshot::text LIKE '%old-updated@example.test%')
FROM ridu_versions WHERE collection_id = $1 AND document_id = $2`, string(ids["retired-users"]), retiredUser.ID).
		Scan(&userVersionCount, &userMaxRevision, &oldUserSnapshots); err != nil || userVersionCount != 1 || userMaxRevision != 1 || oldUserSnapshots != 0 {
		t.Fatalf("reincarnated user versions = count %d max %d old %d, %v", userVersionCount, userMaxRevision, oldUserSnapshots, err)
	}
	var mediaVersionCount, mediaSnapshots int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE snapshot::text LIKE '%' || $3 || '%')
FROM ridu_versions WHERE collection_id = $1 AND document_id = $2`, string(ids["media"]), mediaID, oldObjectKey).
		Scan(&mediaVersionCount, &mediaSnapshots); err != nil || mediaVersionCount != 1 || mediaSnapshots != 1 {
		t.Fatalf("unrelated upload versions = count %d matching %d, %v", mediaVersionCount, mediaSnapshots, err)
	}
	var survivingOwnerVersions int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_versions WHERE collection_id = $1 AND document_id = $2`, string(ids["entries"]), entry.ID).Scan(&survivingOwnerVersions); err != nil || survivingOwnerVersions != 0 {
		t.Fatalf("reference-bearing surviving version history = %d, %v", survivingOwnerVersions, err)
	}
	var reattachedReferences int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_document_references
	WHERE owner_collection_id = $1 OR target_collection_id = $1`, string(ids["retired-users"])).Scan(&reattachedReferences); err != nil || reattachedReferences != 0 {
		t.Fatalf("retired relationship/upload references after readdition = %d, %v", reattachedReferences, err)
	}
}

func collectionRetirementConfig(includeRetired bool) ridu.Config {
	authCollection := func(slug schema.CollectionSlug, extraFields ...field.Node) ridu.Collection {
		fields := append(field.Fields{field.Email("email").Required().Unique()}, extraFields...)
		return ridu.Collection{
			Slug: slug, Auth: true, Versions: true, LockDocuments: true,
			AuthConfig:         ridu.AuthConfig{APIKeys: true},
			VersionConfig:      ridu.VersionConfig{Drafts: true, MaxPerDocument: 10},
			DocumentLockConfig: ridu.DocumentLockConfig{Duration: time.Minute},
			Fields:             fields,
		}
	}
	collections := []ridu.Collection{
		authCollection("keepers"),
		{Slug: "media", Upload: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10}},
	}
	if includeRetired {
		collections = append(collections, authCollection("retired-users", field.Upload("avatar", "media")))
	}
	entryFields := field.Fields{field.Text("title").Required()}
	if includeRetired {
		entryFields = append(entryFields, field.Relationship("retiredUser", "retired-users"))
	}
	collections = append(collections, ridu.Collection{
		Slug: "entries", Fields: entryFields, Versions: true,
		VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10},
	})
	globals := []ridu.Global{{
		Slug: "keeper-settings", Fields: field.Fields{field.Text("title").Required()}, Versions: true,
		VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10},
	}}
	if includeRetired {
		globals = append(globals, ridu.Global{
			Slug: "retired-settings", Fields: field.Fields{field.Text("title").Required()}, Versions: true,
			VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10},
		})
	}
	return ridu.Config{
		Name: "Collection retirement", Admin: ridu.AdminConfig{User: "keepers"},
		Collections: collections, Globals: globals,
	}
}

func collectionRetirementUploadValues(objectKey string) store.Values {
	return store.Values{
		"filename": store.String("proof.txt"), "mimeType": store.String("text/plain"), "filesize": store.Number(5),
		"url": store.String("/media/proof.txt"), "objectKey": store.String(objectKey),
	}
}

func collectionRetirementString(value store.Value) string {
	result, _ := value.StringValue()
	return result
}

func collectionRetirementTask(id, slug string, target, requester *store.DocumentReference, input json.RawMessage) store.Task {
	return store.Task{
		ID: id, Slug: slug, Queue: "default", Input: input, RunAt: time.Now().Add(time.Hour), State: store.TaskStateQueued,
		MaxAttempts: 3, RetryDelay: time.Second, MaxRetryDelay: time.Minute, Backoff: store.TaskBackoffFixed,
		Timeout: time.Minute, Retention: time.Hour, Target: target, RequestedBy: requester,
	}
}

func collectionRetirementResourceIDs(snapshot schema.Snapshot) map[schema.CollectionSlug]schema.StableID {
	result := make(map[schema.CollectionSlug]schema.StableID, len(snapshot.Collections))
	for _, collection := range snapshot.Collections {
		result[collection.Slug] = collection.ID
	}
	return result
}

func collectionRetirementGlobalIDs(snapshot schema.Snapshot) map[schema.CollectionSlug]schema.StableID {
	result := make(map[schema.CollectionSlug]schema.StableID, len(snapshot.Globals))
	for _, global := range snapshot.Globals {
		result[global.Slug] = global.ID
	}
	return result
}

func assertRetiredFrameworkResourceState(t *testing.T, ctx context.Context, backend *Store, resourceIDs ...schema.StableID) {
	t.Helper()
	queries := []struct {
		name  string
		query string
	}{
		{"credentials", `SELECT count(*) FROM ridu_auth_credentials WHERE collection_id = ANY($1::text[])`},
		{"sessions", `SELECT count(*) FROM ridu_auth_sessions WHERE collection_id = ANY($1::text[])`},
		{"tokens", `SELECT count(*) FROM ridu_auth_tokens WHERE collection_id = ANY($1::text[])`},
		{"API keys", `SELECT count(*) FROM ridu_auth_api_keys WHERE collection_id = ANY($1::text[])`},
		{"preferences", `SELECT count(*) FROM ridu_preferences WHERE collection_id = ANY($1::text[])`},
		{"versions", `SELECT count(*) FROM ridu_versions WHERE collection_id = ANY($1::text[])`},
		{"tasks", `SELECT count(*) FROM ridu_tasks WHERE target_collection_id = ANY($1::text[]) OR requested_by_collection_id = ANY($1::text[])
OR (task_slug = 'ridu-schedule-publish' AND (input->>'collectionID' = ANY($1::text[]) OR input->>'requestedByCollectionID' = ANY($1::text[])))`},
		{"locks", `SELECT count(*) FROM ridu_document_locks WHERE collection_id = ANY($1::text[]) OR owner_collection_id = ANY($1::text[])`},
		{"relationship and upload references", `SELECT count(*) FROM ridu_document_references WHERE owner_collection_id = ANY($1::text[]) OR target_collection_id = ANY($1::text[])`},
	}
	values := make([]string, len(resourceIDs))
	for index, resourceID := range resourceIDs {
		values[index] = string(resourceID)
	}
	for _, candidate := range queries {
		var count int
		if err := backend.pool.QueryRow(ctx, candidate.query, values).Scan(&count); err != nil || count != 0 {
			t.Fatalf("retired %s rows = %d, %v", candidate.name, count, err)
		}
	}
	for _, resourceID := range resourceIDs {
		var exists bool
		if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, collectionTable(resourceID)).Scan(&exists); err != nil || exists {
			t.Fatalf("retired physical resource %s exists = %t, %v", resourceID, exists, err)
		}
	}
}

func collectionRetirementBackend(t *testing.T, ctx context.Context, baseURL string) *Store {
	t.Helper()
	schemaName := fmt.Sprintf("ridu_collection_retirement_%d", time.Now().UnixNano())
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schemaName}.Sanitize()+" CASCADE")
	})
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	parsed.RawQuery = parameters.Encode()
	backend, err := OpenWithConfig(ctx, PoolConfig{DatabaseURL: parsed.String(), AllowInsecureTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	return backend
}
