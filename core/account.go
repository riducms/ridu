package core

import (
	"context"
	"fmt"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/store"
)

// ForceUnlock clears an account lock for an exact authenticated identity.
func (application *App) ForceUnlock(ctx context.Context, collection, id string, identity *AuthIdentity) error {
	if identity == nil {
		return application.local.engine.ForceUnlockAuth(ctx, collection, id, nil, "")
	}
	authCollection, actor, err := application.resolveAuthIdentity(ctx, identity)
	if err != nil {
		return err
	}
	return application.local.engine.ForceUnlockAuth(ctx, collection, id, &actor, authCollection.Slug)
}

// UnlockAuthUser selects an account by its configured identity and unlocks it
// for an exact authenticated identity. Missing users remain indistinguishable
// from denied access.
func (application *App) UnlockAuthUser(ctx context.Context, collection, identity string, actorIdentity *AuthIdentity) error {
	resolved, exists := application.authBySlug[collection]
	if !exists || resolved.Auth == nil || resolved.Auth.MaxLoginAttempts == 0 {
		return &operationengine.Error{Code: "unknown_auth_collection", Status: 404, Message: fmt.Sprintf("auth collection %q with lockout was not found", collection)}
	}
	credential, err := application.auth.FindAuthCredential(ctx, resolved, store.CanonicalAuthIdentity(identity))
	if err != nil {
		return &operationengine.Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	return application.ForceUnlock(ctx, collection, credential.User.ID, actorIdentity)
}
