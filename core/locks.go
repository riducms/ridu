package core

import (
	"context"
	"errors"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// DocumentLockState describes the current authoring lease for one document.
type DocumentLockState struct {
	Lock        *store.DocumentLock
	Owned       bool
	Acquired    bool
	CanTakeOver bool
}

// DocumentLock returns an access-checked current lock for one exact identity.
func (application *App) DocumentLock(ctx context.Context, collection, documentID string, identity *AuthIdentity) (DocumentLockState, error) {
	ownerCollection, actor, err := application.resolveAuthIdentity(ctx, identity)
	if err != nil {
		return DocumentLockState{}, err
	}
	return application.documentLock(ctx, collection, documentID, ownerCollection, &actor)
}

func (application *App) documentLock(ctx context.Context, collection, documentID string, ownerCollection schema.Collection, actor *store.Document) (DocumentLockState, error) {
	resolved, capabilities, err := application.documentLockAccess(ctx, collection, documentID, actor, ownerCollection.Slug)
	if err != nil {
		return DocumentLockState{}, err
	}
	lock, err := application.documentLocks.FindDocumentLock(ctx, resolved.ID, documentID, time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		return DocumentLockState{}, nil
	}
	if err != nil {
		return DocumentLockState{}, &operationengine.Error{Code: "store_failed", Status: 500, Message: "read document lock", Cause: err}
	}
	owned := lockOwnedBy(lock, ownerCollection.ID, actor.ID)
	return DocumentLockState{Lock: &lock, Owned: owned, CanTakeOver: capabilities.Operations.Unlock && !owned}, nil
}

// AcquireDocumentLock creates or refreshes a lease for one exact identity.
// Takeover replaces another active owner.
func (application *App) AcquireDocumentLock(ctx context.Context, collection, documentID string, takeover bool, identity *AuthIdentity) (DocumentLockState, error) {
	ownerCollection, actor, err := application.resolveAuthIdentity(ctx, identity)
	if err != nil {
		return DocumentLockState{}, err
	}
	return application.acquireDocumentLock(ctx, collection, documentID, takeover, ownerCollection, &actor)
}

func (application *App) acquireDocumentLock(ctx context.Context, collection, documentID string, takeover bool, ownerCollection schema.Collection, actor *store.Document) (DocumentLockState, error) {
	resolved, capabilities, err := application.documentLockAccess(ctx, collection, documentID, actor, ownerCollection.Slug)
	if err != nil {
		return DocumentLockState{}, err
	}
	if !capabilities.Operations.Update {
		return DocumentLockState{}, &operationengine.Error{Code: "access_denied", Status: 403, Message: "document update access is required to acquire its lock"}
	}
	if takeover && !capabilities.Operations.Unlock {
		return DocumentLockState{}, &operationengine.Error{Code: "access_denied", Status: 403, Message: "document unlock access is required to take over its lock"}
	}
	now := time.Now().UTC()
	duration := time.Duration(resolved.DocumentLock.DurationSeconds) * time.Second
	candidate := store.DocumentLock{
		CollectionID: resolved.ID, DocumentID: documentID,
		OwnerCollectionID: ownerCollection.ID, OwnerID: actor.ID, OwnerLabel: lockOwnerLabel(ownerCollection, actor),
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(duration),
	}
	lock, acquired, err := application.documentLocks.AcquireDocumentLock(ctx, candidate, now, takeover)
	if err != nil {
		return DocumentLockState{}, &operationengine.Error{Code: "store_failed", Status: 500, Message: "acquire document lock", Cause: err}
	}
	owned := lockOwnedBy(lock, ownerCollection.ID, actor.ID)
	return DocumentLockState{Lock: &lock, Owned: owned, Acquired: acquired && owned, CanTakeOver: capabilities.Operations.Unlock && !owned}, nil
}

// ReleaseDocumentLock removes only the exact authenticated identity's lease.
func (application *App) ReleaseDocumentLock(ctx context.Context, collection, documentID string, identity *AuthIdentity) error {
	ownerCollection, actor, err := application.resolveAuthIdentity(ctx, identity)
	if err != nil {
		return err
	}
	return application.releaseDocumentLock(ctx, collection, documentID, ownerCollection.ID, actor.ID)
}

func (application *App) releaseDocumentLock(ctx context.Context, collection, documentID string, ownerCollectionID schema.StableID, ownerID string) error {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.DocumentLock == nil || application.documentLocks == nil {
		return &operationengine.Error{Code: "not_found", Status: 404, Message: "document locking was not found"}
	}
	if err := application.documentLocks.ReleaseDocumentLock(ctx, resolved.ID, documentID, ownerCollectionID, ownerID); err != nil {
		return &operationengine.Error{Code: "store_failed", Status: 500, Message: "release document lock", Cause: err}
	}
	return nil
}

func (application *App) documentLockAccess(ctx context.Context, collection, documentID string, actor *store.Document, actorCollection schema.CollectionSlug) (schema.Collection, operationengine.AccessCapabilities, error) {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.DocumentLock == nil || application.documentLocks == nil {
		return schema.Collection{}, operationengine.AccessCapabilities{}, &operationengine.Error{Code: "not_found", Status: 404, Message: "document locking was not found"}
	}
	capabilities, err := application.local.engine.Capabilities(ctx, operationengine.CapabilitiesRequest{Collection: collection, ID: documentID, Actor: actor, ActorCollection: actorCollection})
	if err != nil {
		return schema.Collection{}, operationengine.AccessCapabilities{}, err
	}
	if !capabilities.Operations.Read {
		return schema.Collection{}, operationengine.AccessCapabilities{}, &operationengine.Error{Code: "not_found", Status: 404, Message: "document was not found"}
	}
	return resolved, capabilities, nil
}

func lockOwnedBy(lock store.DocumentLock, ownerCollectionID schema.StableID, ownerID string) bool {
	return lock.OwnerCollectionID == ownerCollectionID && lock.OwnerID == ownerID
}

func lockOwnerLabel(collection schema.Collection, actor *store.Document) string {
	if actor == nil {
		return "Unknown editor"
	}
	for _, path := range []string{collection.Admin.UseAsTitle, collection.Auth.IdentityField} {
		if value, ok := actor.Values[path]; ok {
			if label, valid := value.StringValue(); valid && label != "" {
				return label
			}
		}
	}
	return actor.ID
}
