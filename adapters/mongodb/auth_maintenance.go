package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (backend *Store) PruneExpiredAuth(ctx context.Context, limit int) (store.AuthPruneResult, error) {
	if err := backend.prepareAuthOperation(ctx, "authentication pruning"); err != nil {
		return store.AuthPruneResult{}, err
	}
	if err := store.ValidateAuthPruneBatch(limit); err != nil {
		return store.AuthPruneResult{}, err
	}
	var result store.AuthPruneResult
	err := backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		result = store.AuthPruneResult{}
		sessionContext, leave, enterErr := transaction.enter(ctx, true)
		if enterErr != nil {
			return enterErr
		}
		defer leave()
		var err error
		result.Sessions, err = transaction.pruneMongoAuthSessions(sessionContext, limit)
		if err != nil {
			return err
		}
		result.APIKeys, err = transaction.pruneMongoAuthAPIKeys(sessionContext, limit)
		return err
	})
	if err != nil {
		return store.AuthPruneResult{}, err
	}
	return result, nil
}

func (transaction *documentTransaction) pruneMongoAuthSessions(ctx context.Context, limit int) (int, error) {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "expiresAt", Value: bson.D{{Key: "$type", Value: "long"}}},
			{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$expiresAt", mongoTaskServerNowNanosExpression()}}}},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "expiresAt", Value: int32(1)}, {Key: "_id", Value: int32(1)}}}},
		bson.D{{Key: "$limit", Value: int64(limit)}},
	}
	cursor, err := transaction.authSessionCollection().Aggregate(ctx, pipeline)
	if err != nil {
		return 0, translateMongoError(ctx, err)
	}
	candidates := make([]mongoAuthSession, 0, limit)
	for cursor.Next(ctx) {
		session, decodeErr := decodeMongoAuthSession(cursor.Current)
		if decodeErr != nil {
			transaction.closeCursor(cursor)
			return 0, decodeErr
		}
		candidates = append(candidates, session)
	}
	if err := cursor.Err(); err != nil {
		transaction.closeCursor(cursor)
		return 0, translateMongoError(ctx, err)
	}
	transaction.closeCursor(cursor)
	for _, candidate := range candidates {
		expiresAt, _ := encodeTime(candidate.Session.ExpiresAt)
		result, err := transaction.authSessionCollection().DeleteOne(ctx, bson.D{
			{Key: "_id", Value: candidate.Session.TokenHash},
			{Key: "tokenHash", Value: candidate.Session.TokenHash},
			{Key: "expiresAt", Value: expiresAt},
			{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$expiresAt", mongoTaskServerNowNanosExpression()}}}},
		})
		if err != nil {
			return 0, translateMongoError(ctx, err)
		}
		if result.DeletedCount != 1 {
			return 0, mongoConfirmedTransactionConflict{}
		}
	}
	return len(candidates), nil
}

func (transaction *documentTransaction) pruneMongoAuthAPIKeys(ctx context.Context, limit int) (int, error) {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "expiresAt", Value: bson.D{{Key: "$type", Value: "long"}}},
			{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$expiresAt", mongoTaskServerNowNanosExpression()}}}},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "expiresAt", Value: int32(1)}, {Key: "_id", Value: int32(1)}}}},
		bson.D{{Key: "$limit", Value: int64(limit)}},
	}
	cursor, err := transaction.authAPIKeyCollection().Aggregate(ctx, pipeline)
	if err != nil {
		return 0, translateMongoError(ctx, err)
	}
	candidates := make([]mongoAuthAPIKey, 0, limit)
	for cursor.Next(ctx) {
		key, decodeErr := decodeMongoAuthAPIKey(cursor.Current)
		if decodeErr != nil {
			transaction.closeCursor(cursor)
			return 0, decodeErr
		}
		candidates = append(candidates, key)
	}
	if err := cursor.Err(); err != nil {
		transaction.closeCursor(cursor)
		return 0, translateMongoError(ctx, err)
	}
	transaction.closeCursor(cursor)
	for _, candidate := range candidates {
		expiresAt, _ := encodeTime(candidate.Key.ExpiresAt)
		result, err := transaction.authAPIKeyCollection().DeleteOne(ctx, bson.D{
			{Key: "_id", Value: candidate.Key.ID},
			{Key: "tokenHash", Value: candidate.Key.TokenHash},
			{Key: "expiresAt", Value: expiresAt},
			{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$expiresAt", mongoTaskServerNowNanosExpression()}}}},
		})
		if err != nil {
			return 0, translateMongoError(ctx, err)
		}
		if result.DeletedCount != 1 {
			return 0, mongoConfirmedTransactionConflict{}
		}
	}
	return len(candidates), nil
}

func findMongoAuthRateLimit(ctx context.Context, collection *mongo.Collection, keyHash string) (mongoAuthRateLimit, bool, error) {
	raw, err := collection.FindOne(ctx, bson.D{{Key: "_id", Value: mongoAuthRateLimitID(keyHash)}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoAuthRateLimit{}, false, nil
	}
	if err != nil {
		return mongoAuthRateLimit{}, false, translateMongoError(ctx, err)
	}
	limit, err := decodeMongoAuthRateLimit(raw)
	if err != nil {
		return mongoAuthRateLimit{}, false, err
	}
	if limit.KeyHash != keyHash {
		return mongoAuthRateLimit{}, false, mongoSystemIdentityCollision("auth rate limit")
	}
	return limit, true, nil
}

func (backend *Store) AllowAuthAttempt(ctx context.Context, keyHash string, now time.Time, window time.Duration, maximum int) (bool, error) {
	if err := backend.prepareAuthOperation(ctx, "authentication rate limiting"); err != nil {
		return false, err
	}
	if err := validateMongoAuthSecretIdentity(keyHash, "auth rate-limit digest"); err != nil {
		return false, err
	}
	if window <= 0 || maximum < 1 {
		return false, fmt.Errorf("MongoDB authentication rate-limit policy is invalid")
	}
	now, nowNanos, err := normalizeMongoSystemTime(now, "authentication rate-limit timestamp")
	if err != nil {
		return false, err
	}
	expiresAt, _, err := normalizeMongoSystemTime(now.Add(window), "authentication rate-limit deadline")
	if err != nil {
		return false, err
	}
	allowed := false
	err = backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		allowed = false
		sessionContext, leave, enterErr := transaction.enter(ctx, true)
		if enterErr != nil {
			return enterErr
		}
		defer leave()
		if err := transaction.pruneMongoAuthRateLimits(sessionContext, keyHash, nowNanos, 100); err != nil {
			return err
		}
		current, found, err := findMongoAuthRateLimit(sessionContext, transaction.authRateLimitCollection(), keyHash)
		if err != nil {
			return err
		}
		if !found {
			current = mongoAuthRateLimit{KeyHash: keyHash, Attempts: 1, ExpiresAt: expiresAt}
			encoded, err := encodeMongoAuthRateLimit(current)
			if err != nil {
				return err
			}
			if _, err := transaction.authRateLimitCollection().InsertOne(sessionContext, encoded); err != nil {
				return translateMongoError(ctx, err)
			}
			allowed = true
			return nil
		}
		previousAttempts := current.Attempts
		previousExpiry, _ := encodeTime(current.ExpiresAt)
		if !current.ExpiresAt.After(now) {
			current.Attempts = 1
			current.ExpiresAt = expiresAt
		} else {
			current.Attempts++
		}
		nextExpiry, _ := encodeTime(current.ExpiresAt)
		raw, err := transaction.authRateLimitCollection().FindOneAndUpdate(
			sessionContext,
			bson.D{{Key: "_id", Value: keyHash}, {Key: "keyHash", Value: keyHash}, {Key: "attempts", Value: int64(previousAttempts)}, {Key: "expiresAt", Value: previousExpiry}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "attempts", Value: int64(current.Attempts)}, {Key: "expiresAt", Value: nextExpiry}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if err != nil {
			return translateMongoError(ctx, err)
		}
		stored, err := decodeMongoAuthRateLimit(raw)
		if err != nil {
			return err
		}
		allowed = stored.Attempts <= maximum
		return nil
	})
	return allowed, err
}

func (transaction *documentTransaction) pruneMongoAuthRateLimits(ctx context.Context, currentKeyHash string, nowNanos int64, limit int) error {
	cursor, err := transaction.authRateLimitCollection().Find(
		ctx,
		bson.D{
			{Key: "expiresAt", Value: bson.D{{Key: "$lte", Value: nowNanos}}},
			{Key: "_id", Value: bson.D{{Key: "$ne", Value: currentKeyHash}}},
		},
		options.Find().SetSort(bson.D{{Key: "expiresAt", Value: int32(1)}, {Key: "_id", Value: int32(1)}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return translateMongoError(ctx, err)
	}
	candidates := make([]mongoAuthRateLimit, 0, limit)
	for cursor.Next(ctx) {
		candidate, decodeErr := decodeMongoAuthRateLimit(cursor.Current)
		if decodeErr != nil {
			transaction.closeCursor(cursor)
			return decodeErr
		}
		candidates = append(candidates, candidate)
	}
	if err := cursor.Err(); err != nil {
		transaction.closeCursor(cursor)
		return translateMongoError(ctx, err)
	}
	transaction.closeCursor(cursor)
	for _, candidate := range candidates {
		expiresAt, _ := encodeTime(candidate.ExpiresAt)
		result, err := transaction.authRateLimitCollection().DeleteOne(ctx, bson.D{
			{Key: "_id", Value: candidate.KeyHash},
			{Key: "keyHash", Value: candidate.KeyHash},
			{Key: "attempts", Value: int64(candidate.Attempts)},
			{Key: "expiresAt", Value: expiresAt},
			{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$expiresAt", nowNanos}}}},
		})
		if err != nil {
			return translateMongoError(ctx, err)
		}
		if result.DeletedCount != 1 {
			return mongoConfirmedTransactionConflict{}
		}
	}
	return nil
}
