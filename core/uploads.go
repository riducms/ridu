package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/remotefile"
	"github.com/riducms/ridu/internal/uploads"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

// UploadInput contains a file and document metadata for an upload operation.
type UploadInput struct {
	// Filename is the original client-supplied file name.
	Filename string
	// Reader provides the uploaded bytes.
	Reader io.Reader
	// Data contains application-owned upload document fields.
	Data store.Values
	// Actor is the authenticated document used by access rules.
	Actor *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// Locale selects the content locale for application-owned upload metadata.
	Locale LocaleOptions
}

// RemoteUploadInput identifies an HTTP(S) asset and its document metadata.
type RemoteUploadInput struct {
	URL             string
	Data            store.Values
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	Locale          LocaleOptions
}

// UpdateUploadImageInput controls focal-point-aware regeneration of an image
// upload's configured variants.
type UpdateUploadImageInput struct {
	// FocalX is the horizontal focal coordinate from 0 (left) to 100 (right).
	FocalX float64
	// FocalY is the vertical focal coordinate from 0 (top) to 100 (bottom).
	FocalY float64
	// CropX and CropY are the top-left of an optional normalized crop rectangle.
	CropX float64
	CropY float64
	// CropWidth and CropHeight are zero to clear the crop, otherwise each must
	// be positive and the rectangle must remain within the original image.
	CropWidth  float64
	CropHeight float64
	// ExpectedRevision rejects changes based on a stale document revision.
	ExpectedRevision int
	// Actor is the authenticated document used by read and update access rules.
	Actor *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
}

// ReconcileResult summarizes an upload-storage reconciliation pass.
type ReconcileResult struct {
	// Scanned is the number of stored objects inspected.
	Scanned int
	// Candidates is the number of unreferenced objects older than the safety window.
	Candidates int
	// Deleted is the number of unreferenced objects removed.
	Deleted int
}

const (
	minimumUploadReconciliationAge = 5 * time.Minute
	committedUploadCleanupTimeout  = 30 * time.Second
	committedUploadCleanupWorkers  = 4
)

// DuplicateForIdentity duplicates through one exact authenticated collection
// identity. A nil identity remains anonymous.
func (application *App) DuplicateForIdentity(ctx context.Context, collection, id string, overrides store.Values, identity *AuthIdentity, localeOptions LocaleOptions) (store.Document, error) {
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return store.Document{}, err
	}
	options := MutationOptions{Actor: actor, Locale: localeOptions.Locale, FallbackLocales: localeOptions.FallbackLocales, DisableFallback: localeOptions.DisableFallback, AllLocales: localeOptions.AllLocales}
	options.ActorCollection = actorCollection
	return application.Duplicate(ctx, collection, id, overrides, options)
}

// Duplicate prepares fresh storage keys for uploads and cleans up staged objects on failure.
// It preserves exact actor identity and response selection
// while duplicating ordinary or upload documents.
func (application *App) Duplicate(ctx context.Context, collection, id string, overrides store.Values, options MutationOptions) (store.Document, error) {
	resolved, exists := application.bySlug[collection]
	if !exists {
		return store.Document{}, &operationengine.Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", collection)}
	}
	if resolved.Upload == nil {
		return application.local.Duplicate(ctx, collection, id, overrides, options)
	}
	overrides = uploads.ApplicationValues(overrides)
	source, err := application.local.Find(ctx, collection, id, FindOptions{
		Actor: options.Actor, ActorCollection: options.ActorCollection,
		Locale: options.Locale, FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
	})
	if err != nil {
		return store.Document{}, err
	}
	capabilityData := store.CloneValues(source.Values)
	for name, value := range overrides {
		capabilityData[name] = value
	}
	capabilities, err := application.local.engine.Capabilities(ctx, operationengine.CapabilitiesRequest{Collection: collection, Data: capabilityData, Actor: options.Actor, ActorCollection: options.ActorCollection})
	if err != nil {
		return store.Document{}, err
	}
	if !capabilities.Operations.Create {
		return store.Document{}, &operationengine.Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	prepared, err := application.uploads.Duplicate(ctx, resolved, source.Values)
	if err != nil {
		return store.Document{}, &operationengine.Error{Code: "storage_failed", Status: 500, Message: "duplicate upload objects", Cause: err}
	}
	values := store.CloneValues(overrides)
	for name, value := range prepared.Values {
		values[name] = value
	}
	resource := application.uploadTransactionResource(prepared)
	document, err := application.local.duplicateStoragePrepared(ctx, collection, id, values, options, resource)
	if err != nil {
		if resource.Claimed() {
			return store.Document{}, err
		}
		if rollbackError := application.uploads.Rollback(ctx, prepared); rollbackError != nil {
			return store.Document{}, &operationengine.Error{
				Code: "storage_failed", Status: 500,
				Message: "duplicate document failed and staged objects could not be removed",
				Cause:   errors.Join(err, rollbackError),
			}
		}
		return store.Document{}, err
	}
	return document, nil
}

func (application *App) Upload(ctx context.Context, collection string, input UploadInput) (store.Document, error) {
	return application.upload(ctx, collection, input, false)
}

// UploadForIdentity stores a file for one exact authenticated collection
// identity. A nil identity remains anonymous.
func (application *App) UploadForIdentity(ctx context.Context, collection string, input UploadInput, identity *AuthIdentity) (store.Document, error) {
	return application.uploadForIdentity(ctx, collection, input, identity, false)
}

func (application *App) uploadForIdentity(ctx context.Context, collection string, input UploadInput, identity *AuthIdentity, fileAdmissionHeld bool) (store.Document, error) {
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return store.Document{}, err
	}
	input.Actor = actor
	input.ActorCollection = actorCollection
	return application.upload(ctx, collection, input, fileAdmissionHeld)
}

func (application *App) upload(ctx context.Context, collection string, input UploadInput, fileAdmissionHeld bool) (store.Document, error) {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.Upload == nil {
		return store.Document{}, &operationengine.Error{Code: "unknown_upload_collection", Status: 404, Message: fmt.Sprintf("upload collection %q was not found", collection)}
	}
	capabilities, err := application.local.engine.Capabilities(ctx, operationengine.CapabilitiesRequest{Collection: collection, Data: input.Data, Actor: input.Actor, ActorCollection: input.ActorCollection, Locale: string(input.Locale.Locale), FallbackLocales: input.Locale.FallbackLocales, DisableFallback: input.Locale.DisableFallback, AllLocales: input.Locale.AllLocales})
	if err != nil {
		return store.Document{}, err
	}
	if !capabilities.Operations.Create {
		return store.Document{}, &operationengine.Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	prepared, err := application.uploads.Prepare(ctx, resolved, uploads.Input{Filename: input.Filename, Reader: input.Reader, Values: input.Data, FileAdmissionHeld: fileAdmissionHeld})
	if err != nil {
		return store.Document{}, uploadPreparationError(err, "upload could not be stored")
	}
	resource := application.uploadTransactionResource(prepared)
	document, err := application.local.createStoragePrepared(ctx, collection, prepared.Values, MutationOptions{
		Actor: input.Actor, ActorCollection: input.ActorCollection,
		Locale: input.Locale.Locale, FallbackLocales: append([]schema.LocaleCode(nil), input.Locale.FallbackLocales...),
		DisableFallback: input.Locale.DisableFallback, AllLocales: input.Locale.AllLocales,
	}, resource)
	if err != nil {
		if resource.Claimed() {
			return store.Document{}, err
		}
		rollbackError := application.uploads.Rollback(ctx, prepared)
		if rollbackError != nil {
			return store.Document{}, &operationengine.Error{
				Code: "storage_failed", Status: 500,
				Message: "upload document failed and staged objects could not be removed",
				Cause:   errors.Join(err, rollbackError),
			}
		}
		return store.Document{}, err
	}
	return document, nil
}

// UploadFromURL safely downloads a public HTTP(S) asset before passing it
// through the same MIME, size, storage, access, validation, and hook pipeline
// as a multipart upload.
func (application *App) UploadFromURL(ctx context.Context, collection string, input RemoteUploadInput) (store.Document, error) {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.Upload == nil {
		return store.Document{}, &operationengine.Error{Code: "unknown_upload_collection", Status: 404, Message: fmt.Sprintf("upload collection %q was not found", collection)}
	}
	capabilities, err := application.local.engine.Capabilities(ctx, operationengine.CapabilitiesRequest{Collection: collection, Data: input.Data, Actor: input.Actor, ActorCollection: input.ActorCollection, Locale: string(input.Locale.Locale), FallbackLocales: input.Locale.FallbackLocales, DisableFallback: input.Locale.DisableFallback, AllLocales: input.Locale.AllLocales})
	if err != nil {
		return store.Document{}, err
	}
	if !capabilities.Operations.Create {
		return store.Document{}, &operationengine.Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	release, admissionError := application.uploads.AcquireFile(ctx, resolved.Upload.MaxFileSize*2)
	if admissionError != nil {
		return store.Document{}, uploadPreparationError(admissionError, "remote upload could not be admitted")
	}
	defer release()
	remote, err := remotefile.Fetch(ctx, input.URL, resolved.Upload.MaxFileSize)
	if err != nil {
		return store.Document{}, &operationengine.Error{Code: "validation", Status: 422, Message: err.Error(), Cause: err}
	}
	return application.upload(ctx, collection, UploadInput{Filename: remote.Filename, Reader: bytes.NewReader(remote.Bytes), Data: input.Data, Actor: input.Actor, ActorCollection: input.ActorCollection, Locale: input.Locale}, true)
}

// UploadFromURLForIdentity fetches and stores a remote file for one exact
// authenticated collection identity. Identity is rechecked before networking.
func (application *App) UploadFromURLForIdentity(ctx context.Context, collection string, input RemoteUploadInput, identity *AuthIdentity) (store.Document, error) {
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return store.Document{}, err
	}
	input.Actor = actor
	input.ActorCollection = actorCollection
	return application.UploadFromURL(ctx, collection, input)
}

// UpdateUploadImage regenerates configured image sizes around a new focal
// point, then commits their metadata through the ordinary operation engine.
func (application *App) UpdateUploadImage(ctx context.Context, collection, id string, input UpdateUploadImageInput) (store.Document, error) {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.Upload == nil {
		return store.Document{}, &operationengine.Error{Code: "unknown_upload_collection", Status: 404, Message: fmt.Sprintf("upload collection %q was not found", collection)}
	}
	capabilities, err := application.local.engine.Capabilities(ctx, operationengine.CapabilitiesRequest{Collection: collection, ID: id, Actor: input.Actor, ActorCollection: input.ActorCollection})
	if err != nil {
		return store.Document{}, err
	}
	if !capabilities.Operations.Update {
		return store.Document{}, &operationengine.Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	current, err := application.local.Find(ctx, collection, id, FindOptions{Actor: input.Actor, ActorCollection: input.ActorCollection})
	if err != nil {
		return store.Document{}, err
	}
	publishedVersioned := resolved.Versions != nil && current.Status == store.StatusPublished
	if publishedVersioned && !capabilities.Operations.Publish {
		return store.Document{}, &operationengine.Error{Code: "access_denied", Status: 403, Message: "publishing regenerated image variants is not permitted"}
	}
	mimeType, _ := current.Values["mimeType"].StringValue()
	objectKey, _ := current.Values["objectKey"].StringValue()
	if !strings.HasPrefix(mimeType, "image/") || objectKey == "" {
		return store.Document{}, &operationengine.Error{Code: "validation", Status: 422, Message: "only image uploads can be regenerated"}
	}
	expectedRevision := input.ExpectedRevision
	if expectedRevision == 0 {
		expectedRevision = current.Revision
	} else if expectedRevision != current.Revision {
		return store.Document{}, &operationengine.Error{Code: "conflict", Status: 409, Message: "document revision is stale", Cause: store.ErrConflict}
	}
	prepared, err := application.uploads.RegenerateImage(ctx, resolved, uploads.ImageInput{ObjectKey: objectKey, FocalX: input.FocalX, FocalY: input.FocalY, CropX: input.CropX, CropY: input.CropY, CropWidth: input.CropWidth, CropHeight: input.CropHeight})
	if err != nil {
		return store.Document{}, uploadPreparationError(err, "image variants could not be stored")
	}
	resource := application.uploadTransactionResource(prepared)
	mutationOptions := MutationOptions{Actor: input.Actor, ActorCollection: input.ActorCollection, ExpectedRevision: expectedRevision}
	var updated store.Document
	if publishedVersioned {
		updated, err = application.local.publishStoragePrepared(ctx, collection, id, prepared.Values, mutationOptions, resource)
	} else {
		updated, err = application.local.updateStoragePrepared(ctx, collection, id, prepared.Values, mutationOptions, resource)
	}
	if err != nil {
		if resource.Claimed() {
			return store.Document{}, err
		}
		rollbackError := application.uploads.Rollback(ctx, prepared)
		if rollbackError != nil {
			return store.Document{}, &operationengine.Error{
				Code: "storage_failed", Status: 500,
				Message: "image update failed and staged variants could not be removed",
				Cause:   errors.Join(err, rollbackError),
			}
		}
		return store.Document{}, err
	}
	// Superseded variants are left to reconciliation. Its safety window protects
	// concurrent/nested updates, and retained versions keep their keys referenced
	// until version pruning makes those objects eligible for deletion.
	return updated, nil
}

// UpdateUploadImageForIdentity regenerates image variants for one exact
// authenticated collection identity. A nil identity remains anonymous.
func (application *App) UpdateUploadImageForIdentity(ctx context.Context, collection, id string, input UpdateUploadImageInput, identity *AuthIdentity) (store.Document, error) {
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return store.Document{}, err
	}
	input.Actor = actor
	input.ActorCollection = actorCollection
	return application.UpdateUploadImage(ctx, collection, id, input)
}

func (application *App) uploadTransactionResource(prepared uploads.Prepared) *operationengine.TransactionResource {
	return &operationengine.TransactionResource{
		Commit: prepared.Release,
		Rollback: func(ctx context.Context) error {
			return application.uploads.Rollback(ctx, prepared)
		},
		Unknown: prepared.Release,
	}
}

func (application *App) OpenUpload(ctx context.Context, collection, key string, actor *store.Document) (io.ReadCloser, storage.Object, error) {
	return application.OpenUploadWithOptions(ctx, collection, key, FindOptions{Actor: actor})
}

// OpenUploadForIdentity opens an upload through one exact authenticated
// collection identity. A nil identity remains anonymous.
func (application *App) OpenUploadForIdentity(ctx context.Context, collection, key string, identity *AuthIdentity) (io.ReadCloser, storage.Object, error) {
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return nil, storage.Object{}, err
	}
	return application.OpenUploadWithOptions(ctx, collection, key, FindOptions{Actor: actor, ActorCollection: actorCollection})
}

// OpenUploadWithOptions preserves exact actor identity while proving that the
// requested object belongs to an access-visible upload document.
func (application *App) OpenUploadWithOptions(ctx context.Context, collection, key string, options FindOptions) (io.ReadCloser, storage.Object, error) {
	resolved, exact := application.bySlug[collection]
	exactUpload := exact && resolved.Upload != nil
	if !application.uploads.OwnsKey(key) {
		return nil, storage.Object{}, uploadNotFound()
	}
	candidates := make([]schema.Collection, 0, len(application.bySlug))
	if exactUpload {
		candidates = append(candidates, resolved)
	}
	remaining := make([]schema.Collection, 0, len(application.bySlug))
	for _, candidate := range application.bySlug {
		if candidate.Upload != nil && (!exactUpload || candidate.Slug != resolved.Slug) {
			remaining = append(remaining, candidate)
		}
	}
	sort.Slice(remaining, func(left, right int) bool { return remaining[left].Slug < remaining[right].Slug })
	candidates = append(candidates, remaining...)
	var exactAccessError error
	for _, candidate := range candidates {
		if candidate.Upload.Private && options.Actor == nil {
			if exactUpload && candidate.Slug == resolved.Slug {
				exactAccessError = &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"}
			}
			continue
		}
		result, err := application.local.engine.ReadUploadOwner(ctx, key, listRequest(string(candidate.Slug), ListOptions{
			Limit: 1, Actor: options.Actor, ActorCollection: options.ActorCollection,
			Locale: options.Locale, FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
			DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
		}))
		if err != nil {
			var operationError *operationengine.Error
			if errors.As(err, &operationError) && (operationError.Code == "access_denied" || operationError.Code == "not_found") {
				if exactUpload && candidate.Slug == resolved.Slug {
					exactAccessError = err
				}
				continue
			}
			return nil, storage.Object{}, err
		}
		if result.Page != nil && len(result.Page.Documents) != 0 {
			return application.uploads.Backend.Open(ctx, key)
		}
	}
	if exactAccessError != nil {
		return nil, storage.Object{}, exactAccessError
	}
	return nil, storage.Object{}, uploadNotFound()
}

func uploadNotFound() error {
	return &operationengine.Error{Code: "not_found", Status: 404, Message: "upload was not found"}
}

func uploadPreparationError(err error, storageMessage string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if uploads.IsBusy(err) {
		return &operationengine.Error{Code: "rate_limited", Status: 429, Message: err.Error(), Cause: err}
	}
	if uploads.IsStorageError(err) {
		return &operationengine.Error{Code: "storage_failed", Status: 500, Message: storageMessage, Cause: err}
	}
	return &operationengine.Error{Code: "validation", Status: 422, Message: err.Error(), Cause: err}
}

// ReconcileUploads reports unreferenced application-owned objects older than
// the safety window without deleting them. Reconciliation uses an
// ACL-independent store snapshot.
func (application *App) ReconcileUploads(ctx context.Context, olderThan time.Duration) (ReconcileResult, error) {
	return application.reconcileUploads(ctx, olderThan, false)
}

// CleanupUploads deletes the candidates reported by ReconcileUploads. A
// positive grace period prevents in-flight storage preparation from being
// mistaken for an orphan before its document transaction commits.
func (application *App) CleanupUploads(ctx context.Context, olderThan time.Duration) (ReconcileResult, error) {
	return application.reconcileUploads(ctx, olderThan, true)
}

func (application *App) reconcileUploads(ctx context.Context, olderThan time.Duration, cleanup bool) (result ReconcileResult, err error) {
	if application.uploads.Backend == nil {
		return ReconcileResult{}, &operationengine.Error{Code: "upload_unavailable", Status: 503, Message: "upload storage is unavailable"}
	}
	if olderThan < minimumUploadReconciliationAge {
		return ReconcileResult{}, &operationengine.Error{Code: "bad_request", Status: 400, Message: fmt.Sprintf("upload reconciliation requires a safety window of at least %s", minimumUploadReconciliationAge)}
	}
	referenceTransaction, referenceLookup, err := application.beginUploadReferenceSnapshot(ctx, cleanup)
	if err != nil {
		return ReconcileResult{}, err
	}
	defer func() {
		if rollbackError := rollbackTransaction(ctx, referenceTransaction); rollbackError != nil {
			err = errors.Join(err, fmt.Errorf("close upload reconciliation snapshot: %w", rollbackError))
		}
	}()
	prefix := "ridu/" + application.uploads.Namespace + "/"
	cutoff := time.Now().UTC().Add(-olderThan)
	batch := make([]storage.Object, 0, store.MaxUploadReferenceCandidates)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		keys := make([]string, len(batch))
		for index, object := range batch {
			keys[index] = object.Key
		}
		referenced, referenceError := application.lookupUploadReferences(ctx, referenceLookup, keys)
		if referenceError != nil {
			return referenceError
		}
		candidates := make([]string, 0, len(keys))
		for _, key := range keys {
			if !referenced[key] {
				candidates = append(candidates, key)
			}
		}
		result.Candidates += len(candidates)
		if cleanup && len(candidates) != 0 {
			// Object storage and the document store cannot share one transaction.
			// Lock and re-read the complete bounded batch immediately before
			// deleting so a reference admitted after enumeration wins the race.
			deleted, deleteError := application.deleteUploadObjectsIfUnreferenced(ctx, candidates, true)
			if deleteError != nil {
				return deleteError
			}
			if deleted < 0 || deleted > len(candidates) {
				return &operationengine.Error{Code: "storage_failed", Status: 500, Message: "upload cleanup returned an invalid deletion count"}
			}
			result.Deleted += deleted
		}
		batch = batch[:0]
		return nil
	}
	cursor := ""
	seenCursors := map[string]struct{}{"": {}}
	lastKey := ""
	for {
		page, listError := application.uploads.Backend.List(ctx, storage.ListRequest{Prefix: prefix, Cursor: cursor, Limit: storage.MaxListPageSize})
		if listError != nil {
			return result, listError
		}
		if len(page.Objects) > storage.MaxListPageSize {
			return result, &operationengine.Error{Code: "storage_failed", Status: 500, Message: "storage listing exceeded its requested page limit"}
		}
		for _, object := range page.Objects {
			if object.Key <= lastKey {
				return result, &operationengine.Error{Code: "storage_failed", Status: 500, Message: fmt.Sprintf("storage listing is not uniquely key-ordered at %q", object.Key)}
			}
			lastKey = object.Key
			if !strings.HasPrefix(object.Key, prefix) || !application.uploads.OwnsKey(object.Key) {
				continue
			}
			result.Scanned++
			if object.ModifiedAt.IsZero() {
				return result, &operationengine.Error{Code: "storage_failed", Status: 500, Message: fmt.Sprintf("storage object %q has no modification time", object.Key)}
			}
			if object.ModifiedAt.After(cutoff) {
				continue
			}
			batch = append(batch, object)
			if len(batch) == store.MaxUploadReferenceCandidates {
				if err := flush(); err != nil {
					return result, err
				}
			}
		}
		if page.NextCursor == "" {
			break
		}
		if _, duplicate := seenCursors[page.NextCursor]; duplicate {
			return result, &operationengine.Error{Code: "storage_failed", Status: 500, Message: "storage listing cursor did not advance"}
		}
		seenCursors[page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
	if err := flush(); err != nil {
		return result, err
	}
	return result, nil
}

func (application *App) uploadReferences(ctx context.Context, keys []string, requireSnapshot bool) (referenced map[string]bool, err error) {
	ordered, err := boundedUploadReferenceKeys(keys)
	if err != nil {
		return nil, err
	}
	transaction, lookup, err := application.beginUploadReferenceSnapshot(ctx, requireSnapshot)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rollbackError := rollbackTransaction(ctx, transaction); rollbackError != nil {
			err = errors.Join(err, fmt.Errorf("close upload reference snapshot: %w", rollbackError))
		}
	}()
	return application.lookupUploadReferences(ctx, lookup, ordered)
}

func (application *App) beginUploadReferenceSnapshot(ctx context.Context, requireSnapshot bool) (store.Transaction, store.UploadReferenceTransaction, error) {
	snapshotStore, supportsSnapshot := application.documentStore.(store.SnapshotStore)
	if requireSnapshot && !supportsSnapshot {
		return nil, nil, &operationengine.Error{Code: "store_failed", Status: 500, Message: "upload cleanup requires store.SnapshotStore"}
	}
	var transaction store.Transaction
	var err error
	if supportsSnapshot {
		transaction, err = snapshotStore.BeginSnapshot(ctx)
	} else {
		transaction, err = application.documentStore.Begin(ctx)
	}
	if err != nil {
		return nil, nil, err
	}
	lookup, supported := transaction.(store.UploadReferenceTransaction)
	if !supported {
		rollbackError := rollbackTransaction(ctx, transaction)
		return nil, nil, errors.Join(&operationengine.Error{Code: "store_failed", Status: 500, Message: "upload cleanup requires store.UploadReferenceTransaction"}, rollbackError)
	}
	return transaction, lookup, nil
}

func (application *App) lookupUploadReferences(ctx context.Context, lookup store.UploadReferenceTransaction, keys []string) (map[string]bool, error) {
	ordered, err := boundedUploadReferenceKeys(keys)
	if err != nil {
		return nil, err
	}
	collections := make([]schema.Collection, 0, len(application.bySlug))
	for _, collection := range application.bySlug {
		if collection.Upload != nil {
			collections = append(collections, collection)
		}
	}
	sort.Slice(collections, func(left, right int) bool { return collections[left].ID < collections[right].ID })
	items, err := lookup.ReferencedUploadObjects(ctx, store.UploadReferenceRequest{Collections: collections, ObjectKeys: ordered})
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(ordered))
	for _, key := range ordered {
		wanted[key] = struct{}{}
	}
	referenced := make(map[string]bool, len(items))
	for _, key := range items {
		if _, exists := wanted[key]; !exists || referenced[key] {
			return nil, &operationengine.Error{Code: "store_failed", Status: 500, Message: "upload reference lookup returned an invalid object key"}
		}
		referenced[key] = true
	}
	return referenced, nil
}

func boundedUploadReferenceKeys(keys []string) ([]string, error) {
	unique := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key != "" {
			unique[key] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return nil, nil
	}
	if len(unique) > store.MaxUploadReferenceCandidates {
		return nil, &operationengine.Error{Code: "store_failed", Status: 500, Message: fmt.Sprintf("upload cleanup supports at most %d object keys per document or batch", store.MaxUploadReferenceCandidates)}
	}
	ordered := make([]string, 0, len(unique))
	for key := range unique {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	return ordered, nil
}

func (application *App) deleteUploadObjectsIfUnreferenced(ctx context.Context, keys []string, requireSnapshot bool) (int, error) {
	ordered, err := boundedUploadReferenceKeys(keys)
	if err != nil {
		return 0, err
	}
	if len(ordered) == 0 {
		return 0, nil
	}
	locker, supported := application.documentStore.(store.UploadObjectLocker)
	if !supported {
		return 0, &operationengine.Error{Code: "store_failed", Status: 500, Message: "destructive upload cleanup requires store.UploadObjectLocker"}
	}
	release, err := locker.LockUploadObjects(ctx, ordered)
	if err != nil {
		return 0, err
	}
	defer release()
	referenced, err := application.uploadReferences(ctx, ordered, requireSnapshot)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, key := range ordered {
		if referenced[key] {
			continue
		}
		if err := application.uploads.Backend.Delete(ctx, key); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (application *App) cleanupPermanentUploadDeletes(ctx context.Context, deletes []operationengine.PermanentDelete) error {
	timeout := application.uploadCleanupTimeout
	if timeout <= 0 {
		timeout = committedUploadCleanupTimeout
	}
	cleanupContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if application.uploadCleanupAdmission == nil {
		return &operationengine.Error{Code: "store_failed", Status: 500, Message: "committed upload cleanup admission is unavailable"}
	}
	select {
	case application.uploadCleanupAdmission <- struct{}{}:
	case <-cleanupContext.Done():
		return cleanupContext.Err()
	}
	completed := make(chan error, 1)
	go func() {
		defer func() { <-application.uploadCleanupAdmission }()
		completed <- application.runPermanentUploadDeleteBatches(cleanupContext, deletes)
	}()
	select {
	case err := <-completed:
		return err
	case <-cleanupContext.Done():
		return cleanupContext.Err()
	}
}

func (application *App) runPermanentUploadDeleteBatches(ctx context.Context, deletes []operationengine.PermanentDelete) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("committed upload cleanup panicked")
		}
	}()
	return application.cleanupPermanentUploadDeleteBatches(ctx, deletes)
}

func (application *App) cleanupPermanentUploadDeleteBatches(ctx context.Context, deletes []operationengine.PermanentDelete) error {
	const maximumCleanupKeys = operationengine.MaxBatchDocuments * store.MaxUploadReferenceCandidates
	unique := make(map[string]struct{}, min(len(deletes)*2, maximumCleanupKeys))
	for _, deleted := range deletes {
		if deleted.Collection.Upload == nil {
			continue
		}
		keys, err := uploadObjectKeys(deleted.Original.Values)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if !application.uploads.OwnsKey(key) {
				continue
			}
			unique[key] = struct{}{}
			if len(unique) > maximumCleanupKeys {
				return &operationengine.Error{Code: "store_failed", Status: 500, Message: fmt.Sprintf("committed upload cleanup exceeds the %d-key batch bound", maximumCleanupKeys)}
			}
		}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for start := 0; start < len(keys); start += store.MaxUploadReferenceCandidates {
		end := min(start+store.MaxUploadReferenceCandidates, len(keys))
		if _, err := application.deleteUploadObjectsIfUnreferenced(ctx, keys[start:end], true); err != nil {
			return err
		}
	}
	return nil
}

func validateImportedUploadObjects(ctx context.Context, manager uploads.Manager, collection schema.Collection, values store.Values) error {
	objectKeys, err := uploadObjectKeys(values)
	if err != nil {
		return err
	}
	keys, err := boundedUploadReferenceKeys(objectKeys)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("upload object metadata is missing")
	}
	for _, key := range keys {
		if !manager.OwnsKey(key) {
			return fmt.Errorf("upload object key is outside the configured collection namespace")
		}
		reader, object, err := manager.Backend.Open(ctx, key)
		if err != nil {
			return fmt.Errorf("open imported upload object: %w", err)
		}
		closeError := reader.Close()
		if closeError != nil {
			return fmt.Errorf("close imported upload object: %w", closeError)
		}
		if object.Key != key {
			return fmt.Errorf("storage returned a different imported upload object key")
		}
	}
	return nil
}

func uploadObjectKeys(values store.Values) ([]string, error) {
	keys := make([]string, 0, store.MaxUploadReferenceCandidates)
	if key, exists := values["objectKey"]; exists {
		if text, valid := key.StringValue(); valid && text != "" {
			keys = append(keys, text)
		}
	}
	sizes, exists := values["sizes"]
	if !exists {
		return keys, nil
	}
	if sizes.Kind() != store.ValueObject {
		return keys, nil
	}
	if sizes.Len() > store.MaxUploadReferenceCandidates-1 {
		return nil, fmt.Errorf("upload metadata contains more than %d image variants", store.MaxUploadReferenceCandidates-1)
	}
	for _, size := range sizes.Entries() {
		key, exists := size.Lookup("objectKey")
		if text, valid := key.StringValue(); exists && valid && text != "" {
			keys = append(keys, text)
		}
	}
	return keys, nil
}
