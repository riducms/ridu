package mongodb

import (
	"fmt"
	"math"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const mongoAuthCodecVersion int32 = 1

type mongoAuthCredential struct {
	CollectionID        schema.StableID
	UserID              string
	UserIncarnation     string
	PasswordHash        []byte
	FailedLoginAttempts int
	LockedUntil         *time.Time
	Verified            bool
	Fence               int64
}

type mongoAuthSession struct {
	Session         store.AuthSession
	UserIncarnation string
	Fence           int64
}

type mongoAuthToken struct {
	Token           store.AuthToken
	UserIncarnation string
}

type mongoAuthAPIKey struct {
	Key             store.AuthAPIKey
	UserIncarnation string
}

type mongoAuthRateLimit struct {
	KeyHash   string
	Attempts  int
	ExpiresAt time.Time
}

type mongoAuthBootstrapGuard struct {
	CollectionID schema.StableID
	Generation   int64
}

func mongoAuthCredentialID(collectionID schema.StableID, userID string) string {
	return mongoSystemRecordID("auth_credential", string(collectionID), userID)
}

func mongoAuthSessionID(tokenHash string) string {
	return tokenHash
}

func mongoAuthTokenID(collectionID schema.StableID, userID string, purpose store.AuthTokenPurpose) string {
	return mongoSystemRecordID("auth_token", string(collectionID), userID, string(purpose))
}

func mongoAuthAPIKeyID(id string) string {
	return id
}

func mongoAuthRateLimitID(keyHash string) string {
	return keyHash
}

func mongoAuthBootstrapID(collectionID schema.StableID) string {
	return mongoSystemRecordID("auth_bootstrap", string(collectionID))
}

func validateMongoAuthOwner(collectionID schema.StableID, userID, scope string) error {
	return validateMongoSystemReference(store.DocumentReference{
		CollectionID: collectionID,
		DocumentID:   userID,
	}, scope)
}

func validateMongoAuthSecretIdentity(value, scope string) error {
	if value == "" {
		return fmt.Errorf("MongoDB %s is required", scope)
	}
	return validateMongoSystemString(value, scope)
}

func validateMongoAuthCredential(credential mongoAuthCredential) error {
	if err := validateMongoAuthOwner(credential.CollectionID, credential.UserID, "auth credential owner"); err != nil {
		return err
	}
	if !validDocumentIncarnation(credential.UserIncarnation) {
		return fmt.Errorf("MongoDB auth credential has an invalid owner incarnation")
	}
	if credential.FailedLoginAttempts < 0 {
		return fmt.Errorf("MongoDB auth credential has invalid failed-login state")
	}
	if credential.LockedUntil != nil {
		if credential.FailedLoginAttempts == 0 {
			return fmt.Errorf("MongoDB auth credential has inconsistent lock state")
		}
		if _, err := encodeTime(*credential.LockedUntil); err != nil {
			return fmt.Errorf("MongoDB auth credential lock timestamp: %w", err)
		}
	}
	if credential.Fence < 0 {
		return fmt.Errorf("MongoDB auth credential has invalid mutation fence")
	}
	return nil
}

func encodeMongoAuthCredential(credential mongoAuthCredential) (bson.D, error) {
	if err := validateMongoAuthCredential(credential); err != nil {
		return nil, err
	}
	lockedUntil := any(nil)
	if credential.LockedUntil != nil {
		encoded, _ := encodeTime(*credential.LockedUntil)
		lockedUntil = encoded
	}
	return bson.D{
		{Key: "_id", Value: mongoAuthCredentialID(credential.CollectionID, credential.UserID)},
		{Key: "codec", Value: mongoAuthCodecVersion},
		{Key: "collection", Value: string(credential.CollectionID)},
		{Key: "user", Value: credential.UserID},
		{Key: "userIncarnation", Value: credential.UserIncarnation},
		{Key: "passwordHash", Value: bson.Binary{Subtype: 0, Data: append([]byte(nil), credential.PasswordHash...)}},
		{Key: "failedLoginAttempts", Value: int64(credential.FailedLoginAttempts)},
		{Key: "lockedUntil", Value: lockedUntil},
		{Key: "verified", Value: credential.Verified},
		{Key: "fence", Value: credential.Fence},
	}, nil
}

func decodeMongoAuthCredential(raw bson.Raw) (mongoAuthCredential, error) {
	if err := requireExactKeys(raw, "MongoDB auth credential", "_id", "codec", "collection", "user", "userIncarnation", "passwordHash", "failedLoginAttempts", "lockedUntil", "verified", "fence"); err != nil {
		return mongoAuthCredential{}, err
	}
	if codec, ok := raw.Lookup("codec").Int32OK(); !ok || codec != mongoAuthCodecVersion {
		return mongoAuthCredential{}, fmt.Errorf("stored MongoDB auth credential has an unsupported codec version")
	}
	collection, collectionOK := raw.Lookup("collection").StringValueOK()
	user, userOK := raw.Lookup("user").StringValueOK()
	incarnation, incarnationOK := raw.Lookup("userIncarnation").StringValueOK()
	subtype, passwordHash, hashOK := raw.Lookup("passwordHash").BinaryOK()
	attempts64, attemptsOK := raw.Lookup("failedLoginAttempts").Int64OK()
	verified, verifiedOK := raw.Lookup("verified").BooleanOK()
	fence, fenceOK := raw.Lookup("fence").Int64OK()
	if !collectionOK || !userOK || !incarnationOK || !hashOK || subtype != 0 || !attemptsOK || attempts64 < 0 || attempts64 > int64(math.MaxInt) || !verifiedOK || !fenceOK {
		return mongoAuthCredential{}, fmt.Errorf("stored MongoDB auth credential has invalid state")
	}
	lockedUntil, err := decodeMongoAuthOptionalTime(raw.Lookup("lockedUntil"), "auth credential lockedUntil")
	if err != nil {
		return mongoAuthCredential{}, err
	}
	credential := mongoAuthCredential{
		CollectionID: schema.StableID(collection), UserID: user, UserIncarnation: incarnation,
		PasswordHash: append([]byte(nil), passwordHash...), FailedLoginAttempts: int(attempts64),
		LockedUntil: lockedUntil, Verified: verified, Fence: fence,
	}
	if err := validateMongoAuthCredential(credential); err != nil {
		return mongoAuthCredential{}, fmt.Errorf("stored MongoDB auth credential: %w", err)
	}
	id, ok := raw.Lookup("_id").StringValueOK()
	if !ok || id != mongoAuthCredentialID(credential.CollectionID, credential.UserID) {
		return mongoAuthCredential{}, mongoSystemIdentityCollision("auth credential")
	}
	return credential, nil
}

func validateMongoAuthSession(session mongoAuthSession) error {
	record := session.Session
	if err := validateMongoAuthOwner(record.CollectionID, record.UserID, "auth session owner"); err != nil {
		return err
	}
	for value, scope := range map[string]string{
		record.ID: "auth session ID", record.TokenHash: "auth session token digest",
	} {
		if err := validateMongoAuthSecretIdentity(value, scope); err != nil {
			return err
		}
	}
	for value, scope := range map[string]string{
		record.IPAddress: "auth session IP address", record.UserAgent: "auth session user agent",
	} {
		if err := validateMongoSystemString(value, scope); err != nil {
			return err
		}
	}
	if !validDocumentIncarnation(session.UserIncarnation) {
		return fmt.Errorf("MongoDB auth session has an invalid owner incarnation")
	}
	if session.Fence < 0 {
		return fmt.Errorf("MongoDB auth session has an invalid mutation fence")
	}
	createdAt, err := encodeTime(record.CreatedAt)
	if err != nil {
		return fmt.Errorf("MongoDB auth session createdAt: %w", err)
	}
	lastSeenAt, err := encodeTime(record.LastSeenAt)
	if err != nil {
		return fmt.Errorf("MongoDB auth session lastSeenAt: %w", err)
	}
	expiresAt, err := encodeTime(record.ExpiresAt)
	if err != nil {
		return fmt.Errorf("MongoDB auth session expiresAt: %w", err)
	}
	if expiresAt < createdAt || lastSeenAt > expiresAt {
		return fmt.Errorf("MongoDB auth session has invalid timestamp chronology")
	}
	return nil
}

func encodeMongoAuthSession(session mongoAuthSession) (bson.D, error) {
	if err := validateMongoAuthSession(session); err != nil {
		return nil, err
	}
	record := session.Session
	expiresAt, _ := encodeTime(record.ExpiresAt)
	createdAt, _ := encodeTime(record.CreatedAt)
	lastSeenAt, _ := encodeTime(record.LastSeenAt)
	return bson.D{
		{Key: "_id", Value: mongoAuthSessionID(record.TokenHash)},
		{Key: "codec", Value: mongoAuthCodecVersion},
		{Key: "id", Value: record.ID},
		{Key: "tokenHash", Value: record.TokenHash},
		{Key: "collection", Value: string(record.CollectionID)},
		{Key: "user", Value: record.UserID},
		{Key: "userIncarnation", Value: session.UserIncarnation},
		{Key: "expiresAt", Value: expiresAt},
		{Key: "createdAt", Value: createdAt},
		{Key: "lastSeenAt", Value: lastSeenAt},
		{Key: "ipAddress", Value: record.IPAddress},
		{Key: "userAgent", Value: record.UserAgent},
		{Key: "fence", Value: session.Fence},
	}, nil
}

func decodeMongoAuthSession(raw bson.Raw) (mongoAuthSession, error) {
	if err := requireExactKeys(raw, "MongoDB auth session", "_id", "codec", "id", "tokenHash", "collection", "user", "userIncarnation", "expiresAt", "createdAt", "lastSeenAt", "ipAddress", "userAgent", "fence"); err != nil {
		return mongoAuthSession{}, err
	}
	if codec, ok := raw.Lookup("codec").Int32OK(); !ok || codec != mongoAuthCodecVersion {
		return mongoAuthSession{}, fmt.Errorf("stored MongoDB auth session has an unsupported codec version")
	}
	values := make([]string, 0, 7)
	for _, key := range []string{"id", "tokenHash", "collection", "user", "userIncarnation", "ipAddress", "userAgent"} {
		value, ok := raw.Lookup(key).StringValueOK()
		if !ok {
			return mongoAuthSession{}, fmt.Errorf("stored MongoDB auth session has invalid %s", key)
		}
		values = append(values, value)
	}
	expiresAt, err := decodeMongoAuthTime(raw.Lookup("expiresAt"), "auth session expiresAt")
	if err != nil {
		return mongoAuthSession{}, err
	}
	createdAt, err := decodeMongoAuthTime(raw.Lookup("createdAt"), "auth session createdAt")
	if err != nil {
		return mongoAuthSession{}, err
	}
	lastSeenAt, err := decodeMongoAuthTime(raw.Lookup("lastSeenAt"), "auth session lastSeenAt")
	if err != nil {
		return mongoAuthSession{}, err
	}
	fence, ok := raw.Lookup("fence").Int64OK()
	if !ok {
		return mongoAuthSession{}, fmt.Errorf("stored MongoDB auth session has invalid fence")
	}
	session := mongoAuthSession{Session: store.AuthSession{
		ID: values[0], TokenHash: values[1], CollectionID: schema.StableID(values[2]), UserID: values[3],
		ExpiresAt: expiresAt, CreatedAt: createdAt, LastSeenAt: lastSeenAt, IPAddress: values[5], UserAgent: values[6],
	}, UserIncarnation: values[4], Fence: fence}
	if err := validateMongoAuthSession(session); err != nil {
		return mongoAuthSession{}, fmt.Errorf("stored MongoDB auth session: %w", err)
	}
	id, ok := raw.Lookup("_id").StringValueOK()
	if !ok || id != mongoAuthSessionID(session.Session.TokenHash) {
		return mongoAuthSession{}, mongoSystemIdentityCollision("auth session")
	}
	return session, nil
}

func validateMongoAuthToken(token mongoAuthToken) error {
	record := token.Token
	if err := validateMongoAuthOwner(record.CollectionID, record.UserID, "auth token owner"); err != nil {
		return err
	}
	if err := validateMongoAuthSecretIdentity(record.TokenHash, "auth token digest"); err != nil {
		return err
	}
	if record.Purpose != store.AuthTokenPasswordReset && record.Purpose != store.AuthTokenVerifyEmail {
		return fmt.Errorf("MongoDB auth token has invalid purpose")
	}
	if !validDocumentIncarnation(token.UserIncarnation) {
		return fmt.Errorf("MongoDB auth token has an invalid owner incarnation")
	}
	createdAt, err := encodeTime(record.CreatedAt)
	if err != nil {
		return fmt.Errorf("MongoDB auth token createdAt: %w", err)
	}
	expiresAt, err := encodeTime(record.ExpiresAt)
	if err != nil {
		return fmt.Errorf("MongoDB auth token expiresAt: %w", err)
	}
	if expiresAt < createdAt {
		return fmt.Errorf("MongoDB auth token has invalid timestamp chronology")
	}
	return nil
}

func encodeMongoAuthToken(token mongoAuthToken) (bson.D, error) {
	if err := validateMongoAuthToken(token); err != nil {
		return nil, err
	}
	record := token.Token
	expiresAt, _ := encodeTime(record.ExpiresAt)
	createdAt, _ := encodeTime(record.CreatedAt)
	return bson.D{
		{Key: "_id", Value: mongoAuthTokenID(record.CollectionID, record.UserID, record.Purpose)},
		{Key: "codec", Value: mongoAuthCodecVersion},
		{Key: "tokenHash", Value: record.TokenHash},
		{Key: "purpose", Value: string(record.Purpose)},
		{Key: "collection", Value: string(record.CollectionID)},
		{Key: "user", Value: record.UserID},
		{Key: "userIncarnation", Value: token.UserIncarnation},
		{Key: "expiresAt", Value: expiresAt},
		{Key: "createdAt", Value: createdAt},
	}, nil
}

func decodeMongoAuthToken(raw bson.Raw) (mongoAuthToken, error) {
	if err := requireExactKeys(raw, "MongoDB auth token", "_id", "codec", "tokenHash", "purpose", "collection", "user", "userIncarnation", "expiresAt", "createdAt"); err != nil {
		return mongoAuthToken{}, err
	}
	if codec, ok := raw.Lookup("codec").Int32OK(); !ok || codec != mongoAuthCodecVersion {
		return mongoAuthToken{}, fmt.Errorf("stored MongoDB auth token has an unsupported codec version")
	}
	values := make([]string, 0, 5)
	for _, key := range []string{"tokenHash", "purpose", "collection", "user", "userIncarnation"} {
		value, ok := raw.Lookup(key).StringValueOK()
		if !ok {
			return mongoAuthToken{}, fmt.Errorf("stored MongoDB auth token has invalid %s", key)
		}
		values = append(values, value)
	}
	expiresAt, err := decodeMongoAuthTime(raw.Lookup("expiresAt"), "auth token expiresAt")
	if err != nil {
		return mongoAuthToken{}, err
	}
	createdAt, err := decodeMongoAuthTime(raw.Lookup("createdAt"), "auth token createdAt")
	if err != nil {
		return mongoAuthToken{}, err
	}
	token := mongoAuthToken{Token: store.AuthToken{
		TokenHash: values[0], Purpose: store.AuthTokenPurpose(values[1]), CollectionID: schema.StableID(values[2]),
		UserID: values[3], ExpiresAt: expiresAt, CreatedAt: createdAt,
	}, UserIncarnation: values[4]}
	if err := validateMongoAuthToken(token); err != nil {
		return mongoAuthToken{}, fmt.Errorf("stored MongoDB auth token: %w", err)
	}
	id, ok := raw.Lookup("_id").StringValueOK()
	if !ok || id != mongoAuthTokenID(token.Token.CollectionID, token.Token.UserID, token.Token.Purpose) {
		return mongoAuthToken{}, mongoSystemIdentityCollision("auth token")
	}
	return token, nil
}

func validateMongoAuthAPIKey(key mongoAuthAPIKey) error {
	record := key.Key
	if err := validateMongoAuthOwner(record.CollectionID, record.UserID, "auth API-key owner"); err != nil {
		return err
	}
	for value, scope := range map[string]string{record.ID: "auth API-key ID", record.TokenHash: "auth API-key token digest"} {
		if err := validateMongoAuthSecretIdentity(value, scope); err != nil {
			return err
		}
	}
	if err := validateMongoSystemString(record.Name, "auth API-key name"); err != nil {
		return err
	}
	if !validDocumentIncarnation(key.UserIncarnation) {
		return fmt.Errorf("MongoDB auth API key has an invalid owner incarnation")
	}
	createdAt, err := encodeTime(record.CreatedAt)
	if err != nil {
		return fmt.Errorf("MongoDB auth API key createdAt: %w", err)
	}
	if !record.LastUsedAt.IsZero() {
		_, err := encodeTime(record.LastUsedAt)
		if err != nil {
			return fmt.Errorf("MongoDB auth API key lastUsedAt: %w", err)
		}
	}
	if !record.ExpiresAt.IsZero() {
		expiresAt, err := encodeTime(record.ExpiresAt)
		if err != nil {
			return fmt.Errorf("MongoDB auth API key expiresAt: %w", err)
		}
		if expiresAt < createdAt {
			return fmt.Errorf("MongoDB auth API key has invalid timestamp chronology")
		}
	}
	return nil
}

func encodeMongoAuthAPIKey(key mongoAuthAPIKey) (bson.D, error) {
	if err := validateMongoAuthAPIKey(key); err != nil {
		return nil, err
	}
	record := key.Key
	createdAt, _ := encodeTime(record.CreatedAt)
	lastUsedAt := any(nil)
	if !record.LastUsedAt.IsZero() {
		lastUsedAt, _ = encodeTime(record.LastUsedAt)
	}
	expiresAt := any(nil)
	if !record.ExpiresAt.IsZero() {
		expiresAt, _ = encodeTime(record.ExpiresAt)
	}
	return bson.D{
		{Key: "_id", Value: mongoAuthAPIKeyID(record.ID)},
		{Key: "codec", Value: mongoAuthCodecVersion},
		{Key: "id", Value: record.ID},
		{Key: "tokenHash", Value: record.TokenHash},
		{Key: "collection", Value: string(record.CollectionID)},
		{Key: "user", Value: record.UserID},
		{Key: "userIncarnation", Value: key.UserIncarnation},
		{Key: "name", Value: record.Name},
		{Key: "createdAt", Value: createdAt},
		{Key: "lastUsedAt", Value: lastUsedAt},
		{Key: "expiresAt", Value: expiresAt},
	}, nil
}

func decodeMongoAuthAPIKey(raw bson.Raw) (mongoAuthAPIKey, error) {
	if err := requireExactKeys(raw, "MongoDB auth API key", "_id", "codec", "id", "tokenHash", "collection", "user", "userIncarnation", "name", "createdAt", "lastUsedAt", "expiresAt"); err != nil {
		return mongoAuthAPIKey{}, err
	}
	if codec, ok := raw.Lookup("codec").Int32OK(); !ok || codec != mongoAuthCodecVersion {
		return mongoAuthAPIKey{}, fmt.Errorf("stored MongoDB auth API key has an unsupported codec version")
	}
	values := make([]string, 0, 6)
	for _, field := range []string{"id", "tokenHash", "collection", "user", "userIncarnation", "name"} {
		value, ok := raw.Lookup(field).StringValueOK()
		if !ok {
			return mongoAuthAPIKey{}, fmt.Errorf("stored MongoDB auth API key has invalid %s", field)
		}
		values = append(values, value)
	}
	createdAt, err := decodeMongoAuthTime(raw.Lookup("createdAt"), "auth API key createdAt")
	if err != nil {
		return mongoAuthAPIKey{}, err
	}
	lastUsedAt, err := decodeMongoAuthOptionalTime(raw.Lookup("lastUsedAt"), "auth API key lastUsedAt")
	if err != nil {
		return mongoAuthAPIKey{}, err
	}
	expiresAt, err := decodeMongoAuthOptionalTime(raw.Lookup("expiresAt"), "auth API key expiresAt")
	if err != nil {
		return mongoAuthAPIKey{}, err
	}
	key := mongoAuthAPIKey{Key: store.AuthAPIKey{
		ID: values[0], TokenHash: values[1], CollectionID: schema.StableID(values[2]), UserID: values[3],
		Name: values[5], CreatedAt: createdAt,
	}, UserIncarnation: values[4]}
	if lastUsedAt != nil {
		key.Key.LastUsedAt = *lastUsedAt
	}
	if expiresAt != nil {
		key.Key.ExpiresAt = *expiresAt
	}
	if err := validateMongoAuthAPIKey(key); err != nil {
		return mongoAuthAPIKey{}, fmt.Errorf("stored MongoDB auth API key: %w", err)
	}
	id, ok := raw.Lookup("_id").StringValueOK()
	if !ok || id != mongoAuthAPIKeyID(key.Key.ID) {
		return mongoAuthAPIKey{}, mongoSystemIdentityCollision("auth API key")
	}
	return key, nil
}

func encodeMongoAuthRateLimit(limit mongoAuthRateLimit) (bson.D, error) {
	if err := validateMongoAuthSecretIdentity(limit.KeyHash, "auth rate-limit digest"); err != nil {
		return nil, err
	}
	if limit.Attempts < 1 {
		return nil, fmt.Errorf("MongoDB auth rate limit has invalid attempts")
	}
	expiresAt, err := encodeTime(limit.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("MongoDB auth rate-limit expiresAt: %w", err)
	}
	return bson.D{
		{Key: "_id", Value: mongoAuthRateLimitID(limit.KeyHash)},
		{Key: "codec", Value: mongoAuthCodecVersion},
		{Key: "keyHash", Value: limit.KeyHash},
		{Key: "attempts", Value: int64(limit.Attempts)},
		{Key: "expiresAt", Value: expiresAt},
	}, nil
}

func decodeMongoAuthRateLimit(raw bson.Raw) (mongoAuthRateLimit, error) {
	if err := requireExactKeys(raw, "MongoDB auth rate limit", "_id", "codec", "keyHash", "attempts", "expiresAt"); err != nil {
		return mongoAuthRateLimit{}, err
	}
	if codec, ok := raw.Lookup("codec").Int32OK(); !ok || codec != mongoAuthCodecVersion {
		return mongoAuthRateLimit{}, fmt.Errorf("stored MongoDB auth rate limit has an unsupported codec version")
	}
	keyHash, keyOK := raw.Lookup("keyHash").StringValueOK()
	attempts64, attemptsOK := raw.Lookup("attempts").Int64OK()
	if !keyOK || !attemptsOK || attempts64 < 1 || attempts64 > int64(math.MaxInt) {
		return mongoAuthRateLimit{}, fmt.Errorf("stored MongoDB auth rate limit has invalid state")
	}
	expiresAt, err := decodeMongoAuthTime(raw.Lookup("expiresAt"), "auth rate-limit expiresAt")
	if err != nil {
		return mongoAuthRateLimit{}, err
	}
	limit := mongoAuthRateLimit{KeyHash: keyHash, Attempts: int(attempts64), ExpiresAt: expiresAt}
	if _, err := encodeMongoAuthRateLimit(limit); err != nil {
		return mongoAuthRateLimit{}, fmt.Errorf("stored MongoDB auth rate limit: %w", err)
	}
	id, ok := raw.Lookup("_id").StringValueOK()
	if !ok || id != mongoAuthRateLimitID(limit.KeyHash) {
		return mongoAuthRateLimit{}, mongoSystemIdentityCollision("auth rate limit")
	}
	return limit, nil
}

func encodeMongoAuthBootstrapGuard(guard mongoAuthBootstrapGuard) (bson.D, error) {
	if !schema.IsValidStableID(string(guard.CollectionID)) || guard.Generation < 1 {
		return nil, fmt.Errorf("MongoDB auth bootstrap guard has invalid state")
	}
	return bson.D{
		{Key: "_id", Value: mongoAuthBootstrapID(guard.CollectionID)},
		{Key: "codec", Value: mongoAuthCodecVersion},
		{Key: "collection", Value: string(guard.CollectionID)},
		{Key: "generation", Value: guard.Generation},
	}, nil
}

func decodeMongoAuthBootstrapGuard(raw bson.Raw) (mongoAuthBootstrapGuard, error) {
	if err := requireExactKeys(raw, "MongoDB auth bootstrap guard", "_id", "codec", "collection", "generation"); err != nil {
		return mongoAuthBootstrapGuard{}, err
	}
	if codec, ok := raw.Lookup("codec").Int32OK(); !ok || codec != mongoAuthCodecVersion {
		return mongoAuthBootstrapGuard{}, fmt.Errorf("stored MongoDB auth bootstrap guard has an unsupported codec version")
	}
	collection, collectionOK := raw.Lookup("collection").StringValueOK()
	generation, generationOK := raw.Lookup("generation").Int64OK()
	guard := mongoAuthBootstrapGuard{CollectionID: schema.StableID(collection), Generation: generation}
	if !collectionOK || !generationOK {
		return mongoAuthBootstrapGuard{}, fmt.Errorf("stored MongoDB auth bootstrap guard has invalid state")
	}
	if _, err := encodeMongoAuthBootstrapGuard(guard); err != nil {
		return mongoAuthBootstrapGuard{}, fmt.Errorf("stored MongoDB auth bootstrap guard: %w", err)
	}
	id, ok := raw.Lookup("_id").StringValueOK()
	if !ok || id != mongoAuthBootstrapID(guard.CollectionID) {
		return mongoAuthBootstrapGuard{}, mongoSystemIdentityCollision("auth bootstrap guard")
	}
	return guard, nil
}

func decodeMongoAuthTime(value bson.RawValue, scope string) (time.Time, error) {
	nanoseconds, ok := value.Int64OK()
	if !ok {
		return time.Time{}, fmt.Errorf("stored MongoDB %s is invalid", scope)
	}
	return decodeTime(nanoseconds), nil
}

func decodeMongoAuthOptionalTime(value bson.RawValue, scope string) (*time.Time, error) {
	if value.Type == bson.TypeNull {
		return nil, nil
	}
	decoded, err := decodeMongoAuthTime(value, scope)
	if err != nil {
		return nil, err
	}
	return &decoded, nil
}

func cloneMongoAuthSession(session store.AuthSession) store.AuthSession { return session }

func cloneMongoAuthAPIKey(key store.AuthAPIKey) store.AuthAPIKey { return key }
