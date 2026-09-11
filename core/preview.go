package core

import (
	"context"
	"errors"
	"sync"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const (
	previewTokenDuration     = 5 * time.Minute
	maxPreviewTokens         = 4_096
	maxPreviewTokensPerActor = 64
)

// PreviewToken is a short-lived, read-only credential scoped to one preview target.
// It is safe to hand to a separately deployed preview frontend instead of an admin session.
type PreviewToken struct {
	Token      string
	Resource   string
	Slug       string
	DocumentID string
	ExpiresAt  time.Time
}

type previewTokenGrant struct {
	resource          string
	slug              string
	documentID        string
	actorCollectionID schema.StableID
	actorID           string
	actorCreatedAt    time.Time
	targetCreatedAt   time.Time
	expiresAt         time.Time
}

type previewTokenRegistry struct {
	mu     sync.Mutex
	grants map[string]previewTokenGrant
	epoch  uint64
}

func (registry *previewTokenRegistry) beginPermanentDeleteFence(_ context.Context, deletes []operationengine.PermanentDelete) func(bool) {
	registry.mu.Lock()
	return func(commitAttempted bool) {
		defer registry.mu.Unlock()
		if !commitAttempted {
			return
		}
		registry.epoch++
		for digest, grant := range registry.grants {
			for _, deletion := range deletes {
				isTarget := grant.resource == "collection" && grant.slug == string(deletion.Collection.Slug) && grant.documentID == deletion.DocumentID
				isActor := grant.actorCollectionID == deletion.Collection.ID && grant.actorID == deletion.DocumentID
				if isTarget || isActor {
					delete(registry.grants, digest)
					break
				}
			}
		}
	}
}

// CreateCollectionPreviewToken mints a short-lived capability after
// proving the exact authenticated actor can read the configured preview document.
func (application *App) CreateCollectionPreviewToken(ctx context.Context, collection, documentID string, identity *AuthIdentity) (PreviewToken, error) {
	resolved, exists := application.bySlug[collection]
	if !exists {
		return PreviewToken{}, &operationengine.Error{Code: "not_found", Status: 404, Message: "preview collection was not found"}
	}
	if resolved.Admin.LivePreview == nil || resolved.Versions == nil || !resolved.Versions.Drafts {
		return PreviewToken{}, previewUnavailable()
	}
	return application.createPreviewToken(ctx, "collection", collection, documentID, collection, documentID, identity)
}

// CreateGlobalPreviewToken mints a short-lived capability for one
// configured draft global and one exact authenticated actor.
func (application *App) CreateGlobalPreviewToken(ctx context.Context, slug string, identity *AuthIdentity) (PreviewToken, error) {
	resolved, exists := application.globalsBySlug[slug]
	if !exists {
		return PreviewToken{}, &operationengine.Error{Code: "not_found", Status: 404, Message: "preview global was not found"}
	}
	if resolved.Admin.LivePreview == nil || resolved.Versions == nil || !resolved.Versions.Drafts {
		return PreviewToken{}, previewUnavailable()
	}
	return application.createPreviewToken(ctx, "global", slug, slug, "global:"+slug, slug, identity)
}

// FindCollectionPreview resolves a scoped token and re-runs the ordinary read
// operation so current access predicates, field redaction, and hooks still apply.
func (application *App) FindCollectionPreview(ctx context.Context, token, collection, documentID string) (store.Document, error) {
	actor, grant, err := application.resolvePreviewGrant(ctx, token, "collection", collection, documentID, time.Now().UTC())
	if err != nil {
		return store.Document{}, err
	}
	actorCollection, exists := application.authByID[grant.actorCollectionID]
	if !exists {
		return store.Document{}, invalidPreviewToken()
	}
	document, err := application.local.Find(ctx, collection, documentID, FindOptions{Actor: actor, ActorCollection: actorCollection.Slug})
	if err != nil {
		return store.Document{}, err
	}
	if !document.CreatedAt.Equal(grant.targetCreatedAt) {
		application.revokePreviewToken(token)
		return store.Document{}, invalidPreviewToken()
	}
	if err := application.revalidatePreviewGrant(ctx, token, grant); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

// FindGlobalPreview resolves a scoped token through the same global read engine.
func (application *App) FindGlobalPreview(ctx context.Context, token, slug string) (store.Document, error) {
	actor, grant, err := application.resolvePreviewGrant(ctx, token, "global", slug, slug, time.Now().UTC())
	if err != nil {
		return store.Document{}, err
	}
	actorCollection, exists := application.authByID[grant.actorCollectionID]
	if !exists {
		return store.Document{}, invalidPreviewToken()
	}
	document, err := application.local.Find(ctx, "global:"+slug, slug, FindOptions{Actor: actor, ActorCollection: actorCollection.Slug})
	if err != nil {
		return store.Document{}, err
	}
	if !document.CreatedAt.Equal(grant.targetCreatedAt) {
		application.revokePreviewToken(token)
		return store.Document{}, invalidPreviewToken()
	}
	if err := application.revalidatePreviewGrant(ctx, token, grant); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func (application *App) createPreviewToken(ctx context.Context, resource, slug, documentID, collectionKey, engineID string, identity *AuthIdentity) (PreviewToken, error) {
	for {
		if err := ctx.Err(); err != nil {
			return PreviewToken{}, err
		}
		application.previewTokens.mu.Lock()
		startEpoch := application.previewTokens.epoch + 1
		application.previewTokens.mu.Unlock()
		if identity != nil && identity.PreviewEpoch != 0 && identity.PreviewEpoch != startEpoch {
			return PreviewToken{}, previewIdentityConflict()
		}

		actorCollection, actor, err := application.resolveAuthIdentity(ctx, identity)
		if err != nil {
			return PreviewToken{}, err
		}
		target, err := application.local.Find(ctx, collectionKey, engineID, FindOptions{Actor: &actor, ActorCollection: actorCollection.Slug})
		if err != nil {
			return PreviewToken{}, err
		}
		raw, err := newOpaqueToken(32)
		if err != nil {
			return PreviewToken{}, err
		}
		now := time.Now().UTC()
		expiresAt := now.Add(previewTokenDuration)
		grant := previewTokenGrant{
			resource: resource, slug: slug, documentID: documentID,
			actorCollectionID: actorCollection.ID, actorID: actor.ID, actorCreatedAt: actor.CreatedAt,
			targetCreatedAt: target.CreatedAt, expiresAt: expiresAt,
		}
		application.previewTokens.mu.Lock()
		if application.previewTokens.epoch+1 != startEpoch {
			application.previewTokens.mu.Unlock()
			return PreviewToken{}, previewIdentityConflict()
		}
		for digest, current := range application.previewTokens.grants {
			if !current.expiresAt.After(now) {
				delete(application.previewTokens.grants, digest)
			}
		}
		actorTokens := 0
		for _, current := range application.previewTokens.grants {
			if current.actorCollectionID == actorCollection.ID && current.actorID == actor.ID {
				actorTokens++
			}
		}
		if actorTokens >= maxPreviewTokensPerActor {
			application.previewTokens.mu.Unlock()
			return PreviewToken{}, &operationengine.Error{Code: "rate_limited", Status: 429, Message: "too many active preview tokens for this actor"}
		}
		if len(application.previewTokens.grants) >= maxPreviewTokens {
			application.previewTokens.mu.Unlock()
			return PreviewToken{}, &operationengine.Error{Code: "rate_limited", Status: 429, Message: "too many active preview tokens"}
		}
		application.previewTokens.grants[tokenDigest(raw)] = grant
		application.previewTokens.mu.Unlock()
		return PreviewToken{Token: raw, Resource: resource, Slug: slug, DocumentID: documentID, ExpiresAt: expiresAt}, nil
	}
}

func (application *App) previewIdentityEpoch() uint64 {
	application.previewTokens.mu.Lock()
	defer application.previewTokens.mu.Unlock()
	return application.previewTokens.epoch + 1
}

func previewIdentityConflict() error {
	return &operationengine.Error{
		Code: "conflict", Status: 409,
		Message: "preview identity changed while authorization was being created; authenticate again",
	}
}

func (application *App) resolvePreviewToken(ctx context.Context, raw, resource, slug, documentID string, now time.Time) (*store.Document, error) {
	actor, _, err := application.resolvePreviewGrant(ctx, raw, resource, slug, documentID, now)
	return actor, err
}

func (application *App) resolvePreviewGrant(ctx context.Context, raw, resource, slug, documentID string, now time.Time) (*store.Document, previewTokenGrant, error) {
	if raw == "" {
		return nil, previewTokenGrant{}, invalidPreviewToken()
	}
	digest := tokenDigest(raw)
	application.previewTokens.mu.Lock()
	grant, exists := application.previewTokens.grants[digest]
	if exists && !grant.expiresAt.After(now) {
		delete(application.previewTokens.grants, digest)
		exists = false
	}
	application.previewTokens.mu.Unlock()
	if !exists || grant.resource != resource || grant.slug != slug || grant.documentID != documentID {
		return nil, previewTokenGrant{}, invalidPreviewToken()
	}
	collection, exists := application.authByID[grant.actorCollectionID]
	if !exists {
		return nil, previewTokenGrant{}, invalidPreviewToken()
	}
	actor, err := application.authUser(ctx, collection, grant.actorID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			application.previewTokens.mu.Lock()
			delete(application.previewTokens.grants, digest)
			application.previewTokens.mu.Unlock()
			return nil, previewTokenGrant{}, invalidPreviewToken()
		}
		return nil, previewTokenGrant{}, err
	}
	if !actor.CreatedAt.Equal(grant.actorCreatedAt) {
		application.revokePreviewToken(raw)
		return nil, previewTokenGrant{}, invalidPreviewToken()
	}
	return &actor, grant, nil
}

// RevokePreviewToken removes one capability only when it belongs to
// the exact currently authenticated collection actor. Missing or expired tokens
// are treated as already revoked.
func (application *App) RevokePreviewToken(ctx context.Context, raw string, identity *AuthIdentity) error {
	collection, actor, err := application.resolveAuthIdentity(ctx, identity)
	if err != nil {
		return err
	}
	digest := tokenDigest(raw)
	application.previewTokens.mu.Lock()
	defer application.previewTokens.mu.Unlock()
	grant, exists := application.previewTokens.grants[digest]
	if !exists {
		return nil
	}
	if grant.actorCollectionID != collection.ID || grant.actorID != actor.ID || !grant.actorCreatedAt.Equal(actor.CreatedAt) {
		return invalidPreviewToken()
	}
	delete(application.previewTokens.grants, digest)
	return nil
}

func (application *App) revokePreviewToken(raw string) {
	application.previewTokens.mu.Lock()
	delete(application.previewTokens.grants, tokenDigest(raw))
	application.previewTokens.mu.Unlock()
}

func (application *App) revalidatePreviewGrant(ctx context.Context, raw string, grant previewTokenGrant) error {
	collection, exists := application.authByID[grant.actorCollectionID]
	if !exists {
		return invalidPreviewToken()
	}
	actor, err := application.authUser(ctx, collection, grant.actorID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			application.revokePreviewToken(raw)
			return invalidPreviewToken()
		}
		return err
	}
	if !actor.CreatedAt.Equal(grant.actorCreatedAt) {
		application.revokePreviewToken(raw)
		return invalidPreviewToken()
	}
	digest := tokenDigest(raw)
	application.previewTokens.mu.Lock()
	current, exists := application.previewTokens.grants[digest]
	if exists && !current.expiresAt.After(time.Now().UTC()) {
		delete(application.previewTokens.grants, digest)
		exists = false
	}
	application.previewTokens.mu.Unlock()
	if !exists || current != grant {
		return invalidPreviewToken()
	}
	return nil
}

func (application *App) resolveAuthIdentity(ctx context.Context, identity *AuthIdentity) (schema.Collection, store.Document, error) {
	if identity == nil || identity.Collection == "" || identity.Actor.ID == "" {
		return schema.Collection{}, store.Document{}, authenticationRequired()
	}
	collection, exists := application.authBySlug[string(identity.Collection)]
	if !exists {
		return schema.Collection{}, store.Document{}, authenticationRequired()
	}
	actor, err := application.authUser(ctx, collection, identity.Actor.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return schema.Collection{}, store.Document{}, authenticationRequired()
		}
		return schema.Collection{}, store.Document{}, err
	}
	return collection, actor, nil
}

func previewUnavailable() error {
	return &operationengine.Error{Code: "bad_operation", Status: 400, Message: "server-rendered preview requires configured live preview and drafts"}
}

func invalidPreviewToken() error {
	return &operationengine.Error{Code: "invalid_preview_token", Status: 401, Message: "the preview token is invalid or expired"}
}
