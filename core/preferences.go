package core

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

var preferenceKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9:._-]{0,199}$`)

// Preference reads one preference owned by the exact authenticated identity.
func (application *App) Preference(ctx context.Context, identity *AuthIdentity, key string) (json.RawMessage, error) {
	if err := validatePreferenceKey(key); err != nil {
		return nil, err
	}
	collection, actor, err := application.resolveAuthIdentity(ctx, identity)
	if err != nil {
		return nil, err
	}
	return application.preference(ctx, collection.ID, actor.ID, key)
}

func (application *App) preference(ctx context.Context, collectionID schema.StableID, actorID, key string) (json.RawMessage, error) {
	if application.preferences == nil {
		return nil, preferenceStoreUnavailable()
	}
	preference, err := application.preferences.GetPreference(ctx, collectionID, actorID, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return json.RawMessage("null"), nil
		}
		return nil, &operationengine.Error{Code: "store_failed", Status: 500, Message: "read preference", Cause: err}
	}
	return append(json.RawMessage(nil), preference.Value...), nil
}

// SetPreference writes one preference owned by the exact authenticated identity.
func (application *App) SetPreference(ctx context.Context, identity *AuthIdentity, key string, value json.RawMessage) (json.RawMessage, error) {
	if err := validatePreferenceInput(key, value); err != nil {
		return nil, err
	}
	collection, actor, err := application.resolveAuthIdentity(ctx, identity)
	if err != nil {
		return nil, err
	}
	return application.setPreference(ctx, collection.ID, actor.ID, key, value)
}

func (application *App) setPreference(ctx context.Context, collectionID schema.StableID, actorID, key string, value json.RawMessage) (json.RawMessage, error) {
	if application.preferences == nil {
		return nil, preferenceStoreUnavailable()
	}
	preference, err := application.preferences.SetPreference(ctx, store.Preference{CollectionID: collectionID, UserID: actorID, Key: key, Value: append(json.RawMessage(nil), value...)})
	if err != nil {
		return nil, &operationengine.Error{Code: "store_failed", Status: 500, Message: "save preference", Cause: err}
	}
	return append(json.RawMessage(nil), preference.Value...), nil
}

// DeletePreference removes one preference owned by the exact authenticated identity.
func (application *App) DeletePreference(ctx context.Context, identity *AuthIdentity, key string) error {
	if err := validatePreferenceKey(key); err != nil {
		return err
	}
	collection, actor, err := application.resolveAuthIdentity(ctx, identity)
	if err != nil {
		return err
	}
	return application.deletePreference(ctx, collection.ID, actor.ID, key)
}

func (application *App) deletePreference(ctx context.Context, collectionID schema.StableID, actorID, key string) error {
	if application.preferences == nil {
		return preferenceStoreUnavailable()
	}
	if err := application.preferences.DeletePreference(ctx, collectionID, actorID, key); err != nil {
		return &operationengine.Error{Code: "store_failed", Status: 500, Message: "delete preference", Cause: err}
	}
	return nil
}

// ResetPreferences deletes every preference owned by the exact authenticated identity.
func (application *App) ResetPreferences(ctx context.Context, identity *AuthIdentity) error {
	collection, actor, err := application.resolveAuthIdentity(ctx, identity)
	if err != nil {
		return err
	}
	return application.resetPreferences(ctx, collection.ID, actor.ID)
}

func (application *App) resetPreferences(ctx context.Context, collectionID schema.StableID, actorID string) error {
	if application.preferences == nil {
		return preferenceStoreUnavailable()
	}
	if err := application.preferences.DeletePreferences(ctx, collectionID, actorID); err != nil {
		return &operationengine.Error{Code: "store_failed", Status: 500, Message: "reset preferences", Cause: err}
	}
	return nil
}

func validatePreferenceKey(key string) error {
	if !preferenceKeyPattern.MatchString(key) {
		return &operationengine.Error{Code: "bad_request", Status: 400, Message: "preference key is invalid"}
	}
	return nil
}

func validatePreferenceInput(key string, value json.RawMessage) error {
	if err := validatePreferenceKey(key); err != nil {
		return err
	}
	if len(value) == 0 || len(value) > 64*1024 || !json.Valid(value) {
		return &operationengine.Error{Code: "bad_request", Status: 400, Message: "preference value must be valid JSON no larger than 64 KiB"}
	}
	return nil
}

func preferenceStoreUnavailable() error {
	return &operationengine.Error{Code: "store_failed", Status: 500, Message: "store does not support preferences"}
}
