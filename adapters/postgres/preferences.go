package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func (backend *Store) GetPreference(ctx context.Context, collectionID schema.StableID, userID, key string) (store.Preference, error) {
	var value []byte
	var preference store.Preference
	err := backend.pool.QueryRow(ctx, `SELECT value, updated_at FROM ridu_preferences WHERE collection_id = $1 AND user_id = $2 AND preference_key = $3`, collectionID, userID, key).Scan(&value, &preference.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Preference{}, store.ErrNotFound
	}
	if err != nil {
		return store.Preference{}, err
	}
	preference.CollectionID, preference.UserID, preference.Key = collectionID, userID, key
	preference.Value = append(json.RawMessage(nil), value...)
	return preference, nil
}

func (backend *Store) SetPreference(ctx context.Context, preference store.Preference) (store.Preference, error) {
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return store.Preference{}, err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	if err := lockDocumentReferences(ctx, transaction, store.DocumentReference{CollectionID: preference.CollectionID, DocumentID: preference.UserID}); err != nil {
		return store.Preference{}, err
	}
	var value []byte
	err = transaction.QueryRow(ctx, `INSERT INTO ridu_preferences (collection_id, user_id, preference_key, value) VALUES ($1, $2, $3, $4::jsonb) ON CONFLICT (collection_id, user_id, preference_key) DO UPDATE SET value = EXCLUDED.value, updated_at = now() RETURNING value, updated_at`, preference.CollectionID, preference.UserID, preference.Key, []byte(preference.Value)).Scan(&value, &preference.UpdatedAt)
	if err != nil {
		return store.Preference{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.Preference{}, err
	}
	preference.Value = append(json.RawMessage(nil), value...)
	return preference, nil
}

func (backend *Store) DeletePreference(ctx context.Context, collectionID schema.StableID, userID, key string) error {
	_, err := backend.pool.Exec(ctx, `DELETE FROM ridu_preferences WHERE collection_id = $1 AND user_id = $2 AND preference_key = $3`, collectionID, userID, key)
	return err
}

func (backend *Store) DeletePreferences(ctx context.Context, collectionID schema.StableID, userID string) error {
	_, err := backend.pool.Exec(ctx, `DELETE FROM ridu_preferences WHERE collection_id = $1 AND user_id = $2`, collectionID, userID)
	return err
}
