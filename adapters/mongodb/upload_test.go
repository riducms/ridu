package mongodb

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoUploadMetadataAllowsFieldPoliciesWithinFixedStorageShape(t *testing.T) {
	config := mongoUploadTestConfig()
	config.Collections[0].Fields = append(config.Collections[0].Fields,
		field.Text("objectKey").Label("Storage key").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
		field.JSON("sizes").Admin(field.Admin{Description: "Generated image variants"}).Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
	)
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	media := mongoCollectionsBySlug(manifest.Snapshot().Collections)["media"]
	if err := validateCollectionEnvelope(media); err != nil {
		t.Fatalf("field presentation/access altered upload qualification: %v", err)
	}
	sizes, found := mongoFieldNamed(media.Fields, "sizes")
	if !found || !sizes.QueryRestricted || !mongoUploadSizesField(sizes) {
		t.Fatalf("private sizes lost the managed upload contract: %#v", sizes)
	}
	variantPath, _ := query.NewPath("sizes", "thumb", "objectKey")
	if _, err := resolveMongoPredicatePath(media, variantPath, "filter", mongoPredicateScope{}); err != nil {
		t.Fatalf("private metadata blocked an internal upload lookup: %v", err)
	}
	values := mongoUploadValues("original", "thumbnail")
	if err := validateCompleteValues(media, values); err != nil {
		t.Fatalf("private image-size metadata was rejected: %v", err)
	}
	values["sizes"] = store.Object(store.Values{"thumb": store.String("invalid variant")})
	if err := validateCompleteValues(media, values); err == nil {
		t.Fatal("private sizes bypassed managed image-size value validation")
	}
	for _, mutate := range []func(*schema.Field){
		func(value *schema.Field) { value.ID = "custom-sizes" },
		func(value *schema.Field) { value.Type = schema.FieldTypeText },
		func(value *schema.Field) { value.Category = schema.FieldCategoryScalar },
		func(value *schema.Field) { value.Localized = true },
		func(value *schema.Field) { value.Admin.ReadOnly = false },
	} {
		changed := sizes
		mutate(&changed)
		if mongoUploadSizesField(changed) {
			t.Fatal("field policies weakened managed image-size storage recognition")
		}
	}
	for index, candidate := range media.Fields {
		if candidate.Name != "objectKey" {
			continue
		}
		if !candidate.QueryRestricted || candidate.Admin.Label != "Storage key" || !mongoFrameworkUploadMetadataField(candidate) {
			t.Fatalf("managed field policies were discarded: %#v", candidate)
		}
		for _, mutate := range []func(*schema.Field){
			func(value *schema.Field) { value.Required = false },
			func(value *schema.Field) { value.Index = false },
			func(value *schema.Field) { value.Unique = true },
			func(value *schema.Field) { value.Admin.ReadOnly = false },
		} {
			changed := media
			changed.Fields = append([]schema.Field(nil), media.Fields...)
			mutate(&changed.Fields[index])
			if err := validateCollectionEnvelope(changed); err == nil {
				t.Fatal("field presentation/access weakened managed storage validation")
			}
		}
	}
}

func TestMongoUploadEnvelopeAdmitsOnlyBoundedFrameworkShapes(t *testing.T) {
	media, posts, _ := mongoUploadTestSchema(t)
	if err := validateCollectionEnvelope(media); err != nil {
		t.Fatalf("upload collection envelope: %v", err)
	}
	if err := validateCollectionEnvelope(posts); err != nil {
		t.Fatalf("upload reference envelope: %v", err)
	}

	values := mongoUploadValues("original", "$historic")
	if err := validateCompleteValues(media, values); err != nil {
		t.Fatalf("dynamic partial upload sizes rejected: %v", err)
	}
	if err := validateCompleteValues(media, store.Values{
		"filename": store.String("asset.txt"), "mimeType": store.String("text/plain"),
		"filesize": store.Number(5), "url": store.String("/asset.txt"), "objectKey": store.String("original"),
		"sizes": store.Object(store.Values{"legacy": store.Object(store.Values{
			"objectKey": store.String("legacy"),
			"custom":    store.List(store.String("preserved"), store.Number(1), store.Boolean(true), store.Null()),
		})}),
	}); err != nil {
		t.Fatalf("historic upload metadata rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*schema.Collection)
		want   string
	}{
		{name: "capability disagreement", mutate: func(collection *schema.Collection) { collection.Capabilities.Upload = false }, want: "inconsistent upload capability"},
		{name: "metadata drift", mutate: func(collection *schema.Collection) {
			for index := range collection.Fields {
				if collection.Fields[index].Name == "objectKey" {
					collection.Fields[index].Index = false
				}
			}
		}, want: "exact framework metadata field \"objectKey\""},
		{name: "too many configured sizes", mutate: func(collection *schema.Collection) {
			collection.Upload.ImageSizes = make([]schema.ImageSize, maxMongoUploadImageSizes+1)
		}, want: "exceeds 64 image sizes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := media
			candidate.Fields = append([]schema.Field(nil), media.Fields...)
			upload := *media.Upload
			upload.MimeTypes = append([]string(nil), media.Upload.MimeTypes...)
			upload.ImageSizes = append([]schema.ImageSize(nil), media.Upload.ImageSizes...)
			candidate.Upload = &upload
			test.mutate(&candidate)
			if err := validateCollectionEnvelope(candidate); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("upload envelope error = %v, want containing %q", err, test.want)
			}
		})
	}

	localized := posts
	localized.Fields = append([]schema.Field(nil), posts.Fields...)
	localized.Fields[1].Localized = true
	if err := validateCollectionEnvelope(localized); err != nil {
		t.Fatalf("localized upload error = %v", err)
	}
	repeatedPath, _ := query.NewPath("rows")
	rowUploadPath, _ := query.NewPath("rows", "asset")
	repeated := schema.Collection{ID: "upload-rows", Slug: "upload-rows", Fields: []schema.Field{{
		ID: "upload-rows-rows", Name: "rows", Path: repeatedPath, Type: schema.FieldTypeArray, Category: schema.FieldCategoryNested,
		Nested: &schema.NestedField{Fields: []schema.Field{{
			ID: "upload-rows-asset", Name: "asset", Path: rowUploadPath, Type: schema.FieldTypeUpload, Category: schema.FieldCategoryUpload,
			Upload: &schema.UploadField{CollectionID: media.ID, CollectionSlug: media.Slug, OnDelete: schema.ReferenceDeleteNullify},
		}}},
	}}}
	if err := validateCollectionEnvelope(repeated); err != nil {
		t.Fatalf("repeated upload error = %v", err)
	}
}

func TestMongoUploadValuesPreserveRelationshipSemantics(t *testing.T) {
	media, posts, _ := mongoUploadTestSchema(t)
	singular := posts.Fields[1]
	group := posts.Fields[2]
	hasMany := group.Nested.ResolvedFields()[0]
	if err := validateMongoUploadValue(singular, store.String(""), "hero"); err != nil {
		t.Fatalf("optional empty upload ID rejected: %v", err)
	}
	if err := validateMongoUploadValue(hasMany, store.List(store.String("asset-b"), store.String("asset-a"), store.String("asset-a")), "content.gallery"); err != nil {
		t.Fatalf("ordered duplicate upload IDs rejected: %v", err)
	}
	if err := validateMongoUploadValue(hasMany, store.List(), "content.gallery"); err != nil {
		t.Fatalf("optional empty has-many upload rejected: %v", err)
	}
	tooMany := make([]store.Value, maxMongoDocumentReferences+1)
	for index := range tooMany {
		tooMany[index] = store.String(fmt.Sprintf("asset-%d", index))
	}
	if err := validateMongoUploadValue(hasMany, store.List(tooMany...), "content.gallery"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized upload list error = %v", err)
	}
	galleryPath, _ := query.NewPath("content", "gallery")
	for _, role := range []string{"filter", "access"} {
		if _, err := compileMongoNode(posts, query.Equal(galleryPath, query.String("asset-a")).Node(), role, mongoPredicateScope{}); err == nil || !strings.Contains(err.Error(), "unsupported nested or reference repeated field") {
			t.Fatalf("has-many upload %s error = %v", role, err)
		}
	}

	thumbObjectKey, _ := query.NewPath("sizes", "thumb", "objectKey")
	for _, role := range []string{"filter", "access"} {
		resolved, err := resolveMongoPredicatePath(media, thumbObjectKey, role, mongoPredicateScope{})
		if err != nil {
			t.Fatalf("configured upload size %s path: %v", role, err)
		}
		if resolved.storagePath != "values.sizes.thumb.objectKey" || resolved.kind != mongoStringScalar ||
			!reflect.DeepEqual(resolved.objectAncestors, []string{"values.sizes", "values.sizes.thumb"}) {
			t.Fatalf("configured upload size %s path = %#v", role, resolved)
		}
		if _, err := compileMongoNode(media, query.Equal(thumbObjectKey, query.String("variant-key")).Node(), role, mongoPredicateScope{}); err != nil {
			t.Fatalf("configured upload size %s predicate: %v", role, err)
		}
	}
	versionResolved, err := resolveMongoPredicatePath(media, thumbObjectKey, "version access", mongoPredicateScope{storagePrefix: mongoVersionSnapshotPath + "."})
	if err != nil || versionResolved.storagePath != mongoVersionSnapshotPath+".values.sizes.thumb.objectKey" {
		t.Fatalf("configured upload size version path = %#v, %v", versionResolved, err)
	}
	for _, segments := range [][]string{
		{"sizes", "legacy", "objectKey"},
		{"sizes", "thumb", "url"},
		{"sizes", "thumb", "custom", "objectKey"},
	} {
		path, err := query.NewPath(segments...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := resolveMongoPredicatePath(media, path, "filter", mongoPredicateScope{}); err == nil || !strings.Contains(err.Error(), "configured upload image-size object key") {
			t.Fatalf("arbitrary upload size path %q error = %v", path.String(), err)
		}
	}
	if _, err := resolveMongoPredicatePath(media, thumbObjectKey, "sort", mongoPredicateScope{}); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("upload size sort path error = %v", err)
	}
}

func TestMongoUploadReferenceRequestIsDetachedBoundedAndLiteralSafe(t *testing.T) {
	media, _, _ := mongoUploadTestSchema(t)
	request := store.UploadReferenceRequest{Collections: []schema.Collection{media}, ObjectKeys: []string{"z", "$hostile", "a"}}
	keys, collections, err := validateMongoUploadReferenceRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keys, []string{"$hostile", "a", "z"}) || len(collections) != 1 || collections[0].ID != media.ID {
		t.Fatalf("normalized upload reference request = %#v, %#v", keys, collections)
	}
	if !reflect.DeepEqual(request.ObjectKeys, []string{"z", "$hostile", "a"}) {
		t.Fatalf("request keys were mutated: %#v", request.ObjectKeys)
	}
	encoded, err := bson.MarshalExtJSON(mongoUploadReferenceFilter("$values", "$hostile"), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"$literal":"$hostile"`) || !strings.Contains(string(encoded), `"$objectToArray":"$values.sizes"`) {
		t.Fatalf("upload reference expression is not literal/type guarded: %s", encoded)
	}

	duplicate := request
	duplicate.ObjectKeys = []string{"same", "same"}
	if _, _, err := validateMongoUploadReferenceRequest(duplicate); err == nil || strings.Contains(err.Error(), "same") {
		t.Fatalf("duplicate key error = %v, want redacted", err)
	}
	empty := request
	empty.Collections = nil
	if _, _, err := validateMongoUploadReferenceRequest(empty); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("empty collection error = %v", err)
	}
}

func TestMongoUploadLockNormalizationAndCodecAreStrict(t *testing.T) {
	input := []string{"object-b", "object-a", "object-a"}
	keys, err := normalizedMongoUploadLockKeys(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keys, []string{"object-a", "object-b"}) || !reflect.DeepEqual(input, []string{"object-b", "object-a", "object-a"}) {
		t.Fatalf("normalized keys = %#v; input = %#v", keys, input)
	}
	if _, err := normalizedMongoUploadLockKeys([]string{"secret\x00key"}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("invalid key error = %v, want redacted", err)
	}

	owner := strings.Repeat("a", 32)
	fence := strings.Repeat("b", 32)
	record := encodeMongoUploadLock("object", owner, fence)
	decoded, err := decodeMongoUploadLock(marshalMongoSystemFixture(t, record))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Key != "object" || decoded.Owner != owner || decoded.Fence != fence {
		t.Fatalf("decoded upload lock = %#v", decoded)
	}
	unknown := append(append(bson.D(nil), record...), bson.E{Key: "future", Value: true})
	if _, err := decodeMongoUploadLock(marshalMongoSystemFixture(t, unknown)); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("unknown upload-lock field error = %v", err)
	}
	unsupported := append(bson.D(nil), record...)
	unsupported[1].Value = int32(2)
	if _, err := decodeMongoUploadLock(marshalMongoSystemFixture(t, unsupported)); err == nil || !strings.Contains(err.Error(), "codec") {
		t.Fatalf("unsupported upload-lock codec error = %v", err)
	}
	collision := append(bson.D(nil), record...)
	collision[0].Value = mongoUploadLockID("other")
	if _, err := decodeMongoUploadLock(marshalMongoSystemFixture(t, collision)); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("upload-lock collision error = %v, want ErrConflict", err)
	}
}

func TestMongoUploadLockUnknownOutcomeAndReleaseRetryStayFailClosed(t *testing.T) {
	unknown := mongoUploadLockCommitUnknown{cause: context.Canceled}
	if !isMongoUploadLockCommitUnknown(unknown) || !errors.Is(unknown, context.Canceled) {
		t.Fatalf("unknown upload-lock commit classification = %v", unknown)
	}
	if strings.Contains(unknown.Error(), "secret-object-key") {
		t.Fatal("unknown upload-lock error leaked an object key")
	}
	if isMongoUploadLockCommitUnknown(errors.New("definitive acquisition failure")) {
		t.Fatal("definitive acquisition failure classified as unknown")
	}

	releaseLifecycle, cancelReleaseLifecycle := context.WithCancel(context.Background())
	defer cancelReleaseLifecycle()
	releaseStore := &Store{uploadLockRetry: time.Millisecond, uploadLockLifecycleCtx: releaseLifecycle}
	var attempts atomic.Int32
	firstDeleteStarted := make(chan struct{})
	allowFirstDeleteFailure := make(chan struct{})
	release := newMongoUploadLockRelease(func() error {
		if attempts.Add(1) == 1 {
			close(firstDeleteStarted)
			<-allowFirstDeleteFailure
			return errors.New("injected exact-delete failure")
		}
		return nil
	}, releaseStore.waitForMongoUploadLockCleanupRetry)
	firstReleaseDone := make(chan struct{})
	go func() {
		release.run()
		close(firstReleaseDone)
	}()
	<-firstDeleteStarted
	const callers = 32
	var wait sync.WaitGroup
	wait.Add(callers - 1)
	for range callers - 1 {
		go func() {
			defer wait.Done()
			release.run()
		}()
	}
	select {
	case <-firstReleaseDone:
		t.Fatal("single release invocation returned before its exact delete succeeded")
	default:
	}
	close(allowFirstDeleteFailure)
	<-firstReleaseDone
	wait.Wait()
	if attempts.Load() != 2 {
		t.Fatalf("single-invocation retry with concurrent callers = %d attempts, want one failure and one success", attempts.Load())
	}
	release.run()
	if attempts.Load() != 2 {
		t.Fatalf("completed release retried deletion: %d attempts", attempts.Load())
	}

	terminalLifecycle, cancelTerminalLifecycle := context.WithCancel(context.Background())
	defer cancelTerminalLifecycle()
	terminalStore := &Store{uploadLockRetry: time.Millisecond, uploadLockLifecycleCtx: terminalLifecycle}
	var terminalAttempts atomic.Int32
	terminalRelease := newMongoUploadLockRelease(func() error {
		if terminalAttempts.Add(1) == 1 {
			return errors.New("injected transaction exact-delete failure")
		}
		return nil
	}, terminalStore.waitForMongoUploadLockCleanupRetry)
	terminal := &documentTransaction{
		store:              terminalStore,
		uploadLockReleases: []*mongoUploadLockRelease{terminalRelease},
		uploadLockedKeys:   map[string]struct{}{"terminal-object": {}},
	}
	if err := terminal.releaseUploadObjectLocks(); err != nil {
		t.Fatalf("transaction terminal release retry: %v", err)
	}
	if terminalAttempts.Load() != 2 || terminal.uploadLockReleases != nil || terminal.uploadLockedKeys != nil {
		t.Fatalf("transaction terminal retry = attempts %d, releases=%#v keys=%#v", terminalAttempts.Load(), terminal.uploadLockReleases, terminal.uploadLockedKeys)
	}

	persistentLifecycle, cancelPersistentLifecycle := context.WithCancel(context.Background())
	persistentStore := &Store{uploadLockRetry: time.Millisecond, uploadLockLifecycleCtx: persistentLifecycle}
	var persistentAttempts atomic.Int32
	retriedPersistentDelete := make(chan struct{})
	persistentRelease := newMongoUploadLockRelease(func() error {
		if persistentAttempts.Add(1) == 2 {
			close(retriedPersistentDelete)
		}
		return errors.New("injected persistent exact-delete failure")
	}, persistentStore.waitForMongoUploadLockCleanupRetry)
	persistent := &documentTransaction{
		store:              persistentStore,
		uploadLockReleases: []*mongoUploadLockRelease{persistentRelease},
		uploadLockedKeys:   map[string]struct{}{"persistent-object": {}},
	}
	persistentResult := make(chan error, 1)
	go func() {
		persistentResult <- persistent.releaseUploadObjectLocks()
	}()
	select {
	case <-retriedPersistentDelete:
	case <-time.After(time.Second):
		cancelPersistentLifecycle()
		<-persistentResult
		t.Fatal("persistent transaction cleanup did not retry while the Store lifecycle remained open")
	}
	cancelPersistentLifecycle()
	if err := <-persistentResult; err == nil || !strings.Contains(err.Error(), "manual reconciliation") {
		t.Fatalf("persistent transaction cleanup error = %v", err)
	}
	if len(persistent.uploadLockReleases) != 1 || persistent.uploadLockedKeys == nil {
		t.Fatalf("persistent transaction cleanup did not retain fail-closed state: releases=%#v keys=%#v", persistent.uploadLockReleases, persistent.uploadLockedKeys)
	}

	var invoked atomic.Int32
	unknownRelease := newMongoUploadLockRelease(func() error {
		invoked.Add(1)
		return nil
	}, nil)
	transaction := &documentTransaction{
		uploadLockReleases: []*mongoUploadLockRelease{unknownRelease},
		uploadLockedKeys:   map[string]struct{}{"unknown-object": {}},
	}
	transaction.abandonUploadObjectLocks()
	if invoked.Load() != 0 || transaction.uploadLockReleases != nil || transaction.uploadLockedKeys != nil {
		t.Fatalf("unknown transaction released or retained local lock handles: invoked=%d releases=%#v keys=%#v", invoked.Load(), transaction.uploadLockReleases, transaction.uploadLockedKeys)
	}
}

func mongoUploadTestSchema(t *testing.T) (schema.Collection, schema.Collection, schema.Manifest) {
	t.Helper()
	manifest, err := ridu.Resolve(mongoUploadTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	collections := mongoCollectionsBySlug(manifest.Snapshot().Collections)
	return collections["media"], collections["posts"], manifest
}

func mongoUploadTestConfig() ridu.Config {
	visibilityPath, _ := query.NewPath("visibility")
	return ridu.Config{
		Name: "MongoDB upload schema",
		Collections: []ridu.Collection{
			{
				Slug: "media", Upload: true, Trash: true, Versions: true,
				UploadConfig: ridu.UploadConfig{
					MaxFileSize: 1024, MimeTypes: []string{"text/plain"},
					ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 16, Height: 16, Fit: "cover"}},
				},
				VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields:        field.Fields{field.Text("alt")},
			},
			{
				Slug:   "posts",
				Fields: field.Fields{field.Text("visibility").Required(), field.Upload("hero", "media").OnDelete(field.ReferenceDeleteNullify), field.Group("content", field.Fields{field.Uploads("gallery", "media").OnDelete(field.ReferenceDeleteNullify)})},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					return ridu.Where(query.Equal(visibilityPath, query.String("public"))), nil
				}},
			},
		},
	}
}

func mongoUploadValues(original, variant string) store.Values {
	return store.Values{
		"alt":       store.String("Asset"),
		"filename":  store.String("asset.txt"),
		"mimeType":  store.String("text/plain"),
		"filesize":  store.Number(5),
		"url":       store.String("/asset.txt"),
		"objectKey": store.String(original),
		"sizes": store.Object(store.Values{"$historic": store.Object(store.Values{
			"objectKey": store.String(variant),
		})}),
	}
}
