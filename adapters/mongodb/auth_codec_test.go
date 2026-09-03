package mongodb

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoAuthCodecsAreStrictDetachedAndDoNotExposeSecrets(t *testing.T) {
	base := time.Date(2026, time.August, 30, 12, 34, 56, 789123456, time.UTC)
	const incarnation = "00112233445566778899aabbccddeeff"
	credential := mongoAuthCredential{
		CollectionID: "users", UserID: "user-1", UserIncarnation: incarnation,
		PasswordHash: []byte("private-password-hash"), FailedLoginAttempts: 2,
		LockedUntil: pointerMongoAuthTime(base.Add(time.Minute)), Verified: true, Fence: 3,
	}
	encodedCredential, err := encodeMongoAuthCredential(credential)
	if err != nil {
		t.Fatal(err)
	}
	decodedCredential, err := decodeMongoAuthCredential(marshalMongoSystemFixture(t, encodedCredential))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodedCredential, credential) {
		t.Fatalf("decoded auth credential = %#v, want %#v", decodedCredential, credential)
	}
	decodedCredential.PasswordHash[0] = 'X'
	again, err := decodeMongoAuthCredential(marshalMongoSystemFixture(t, encodedCredential))
	if err != nil || !bytes.Equal(again.PasswordHash, []byte("private-password-hash")) {
		t.Fatalf("detached password hash = %q, %v", again.PasswordHash, err)
	}

	session := mongoAuthSession{Session: store.AuthSession{
		ID: "session-id", TokenHash: "private-session-digest", CollectionID: "users", UserID: "user-1",
		CreatedAt: base, LastSeenAt: base.Add(-time.Second), ExpiresAt: base.Add(time.Hour),
		IPAddress: "127.0.0.1", UserAgent: "test",
	}, UserIncarnation: incarnation}
	token := mongoAuthToken{Token: store.AuthToken{
		TokenHash: "private-recovery-digest", Purpose: store.AuthTokenPasswordReset,
		CollectionID: "users", UserID: "user-1", CreatedAt: base, ExpiresAt: base.Add(time.Hour),
	}, UserIncarnation: incarnation}
	key := mongoAuthAPIKey{Key: store.AuthAPIKey{
		ID: "api-key-id", TokenHash: "private-api-key-digest", CollectionID: "users", UserID: "user-1",
		Name: "automation", CreatedAt: base, LastUsedAt: base.Add(-time.Second), ExpiresAt: base.Add(time.Hour),
	}, UserIncarnation: incarnation}

	encodedSession, err := encodeMongoAuthSession(session)
	if err != nil {
		t.Fatal(err)
	}
	encodedToken, err := encodeMongoAuthToken(token)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := encodeMongoAuthAPIKey(key)
	if err != nil {
		t.Fatal(err)
	}
	decodedSession, err := decodeMongoAuthSession(marshalMongoSystemFixture(t, encodedSession))
	if err != nil || !reflect.DeepEqual(decodedSession, session) {
		t.Fatalf("decoded auth session = %#v, %v", decodedSession, err)
	}
	decodedToken, err := decodeMongoAuthToken(marshalMongoSystemFixture(t, encodedToken))
	if err != nil || !reflect.DeepEqual(decodedToken, token) {
		t.Fatalf("decoded auth token = %#v, %v", decodedToken, err)
	}
	decodedKey, err := decodeMongoAuthAPIKey(marshalMongoSystemFixture(t, encodedKey))
	if err != nil || !reflect.DeepEqual(decodedKey, key) {
		t.Fatalf("decoded auth API key = %#v, %v", decodedKey, err)
	}

	for name, test := range map[string]struct {
		document bson.D
		decode   func(bson.Raw) error
		secret   string
	}{
		"credential": {
			document: append(append(bson.D(nil), encodedCredential...), bson.E{Key: "future", Value: true}),
			decode:   func(raw bson.Raw) error { _, err := decodeMongoAuthCredential(raw); return err },
			secret:   "private-password-hash",
		},
		"session": {
			document: append(append(bson.D(nil), encodedSession...), bson.E{Key: "future", Value: true}),
			decode:   func(raw bson.Raw) error { _, err := decodeMongoAuthSession(raw); return err },
			secret:   "private-session-digest",
		},
		"token": {
			document: append(append(bson.D(nil), encodedToken...), bson.E{Key: "future", Value: true}),
			decode:   func(raw bson.Raw) error { _, err := decodeMongoAuthToken(raw); return err },
			secret:   "private-recovery-digest",
		},
		"API key": {
			document: append(append(bson.D(nil), encodedKey...), bson.E{Key: "future", Value: true}),
			decode:   func(raw bson.Raw) error { _, err := decodeMongoAuthAPIKey(raw); return err },
			secret:   "private-api-key-digest",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := test.decode(marshalMongoSystemFixture(t, test.document))
			if err == nil || !strings.Contains(err.Error(), "unknown key") {
				t.Fatalf("strict decode error = %v", err)
			}
			if strings.Contains(err.Error(), test.secret) {
				t.Fatalf("strict decode exposed private state: %v", err)
			}
		})
	}

	wrongID := append(bson.D(nil), encodedToken...)
	wrongID[0].Value = "wrong"
	if _, err := decodeMongoAuthToken(marshalMongoSystemFixture(t, wrongID)); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("token identity collision error = %v, want ErrConflict", err)
	}
}

func TestMongoAuthRateLimitAndBootstrapCodecsAreStrict(t *testing.T) {
	base := time.Date(2026, time.August, 30, 15, 0, 0, 123, time.UTC)
	rate := mongoAuthRateLimit{KeyHash: "private-rate-digest", Attempts: 2, ExpiresAt: base}
	encoded, err := encodeMongoAuthRateLimit(rate)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMongoAuthRateLimit(marshalMongoSystemFixture(t, encoded))
	if err != nil || !reflect.DeepEqual(decoded, rate) {
		t.Fatalf("decoded rate limit = %#v, %v", decoded, err)
	}
	guard := mongoAuthBootstrapGuard{CollectionID: "users", Generation: 3}
	encodedGuard, err := encodeMongoAuthBootstrapGuard(guard)
	if err != nil {
		t.Fatal(err)
	}
	decodedGuard, err := decodeMongoAuthBootstrapGuard(marshalMongoSystemFixture(t, encodedGuard))
	if err != nil || decodedGuard != guard {
		t.Fatalf("decoded bootstrap guard = %#v, %v", decodedGuard, err)
	}
}

func pointerMongoAuthTime(value time.Time) *time.Time { return &value }
