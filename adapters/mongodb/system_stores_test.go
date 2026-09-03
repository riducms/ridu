package mongodb

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoSystemRecordIDsAreDeterministicAndBoundarySafe(t *testing.T) {
	first := mongoSystemRecordID("preference", "ab", "c")
	if first != mongoSystemRecordID("preference", "ab", "c") {
		t.Fatal("system record ID is not deterministic")
	}
	for name, other := range map[string]string{
		"component boundary": mongoSystemRecordID("preference", "a", "bc"),
		"namespace":          mongoSystemRecordID("document_lock", "ab", "c"),
		"component order":    mongoSystemRecordID("preference", "c", "ab"),
	} {
		if first == other {
			t.Fatalf("system record ID did not distinguish %s", name)
		}
	}
	if !strings.HasPrefix(first, "z_preference_") || len(first) != len("z_preference_")+64 {
		t.Fatalf("system record ID = %q", first)
	}
	if err := mongoSystemIdentityCollision("preference"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("identity collision error = %v, want ErrConflict", err)
	}
}

func TestMongoPreferenceCodecIsStrictAndDetached(t *testing.T) {
	updatedAt := time.Date(2026, time.August, 30, 17, 18, 19, 123456789, time.FixedZone("test", 3600))
	preference := store.Preference{
		CollectionID: "users", UserID: "Case-Sensitive/界", Key: "collection:posts:preset",
		Value: json.RawMessage(`{"nested":{"mode":"dark"}}`), UpdatedAt: updatedAt,
	}
	encoded, err := encodeMongoPreference(preference)
	if err != nil {
		t.Fatal(err)
	}
	raw := marshalMongoSystemFixture(t, encoded)
	decoded, err := decodeMongoPreference(raw)
	if err != nil {
		t.Fatal(err)
	}
	preference.UpdatedAt = preference.UpdatedAt.UTC()
	if !reflect.DeepEqual(decoded, preference) {
		t.Fatalf("decoded preference = %#v, want %#v", decoded, preference)
	}
	decoded.Value[0] = '['
	again, err := decodeMongoPreference(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(again.Value) != `{"nested":{"mode":"dark"}}` {
		t.Fatalf("caller mutation changed stored preference decode: %s", again.Value)
	}
	if subtype, value, ok := raw.Lookup("value").BinaryOK(); !ok || subtype != 0 || string(value) != `{"nested":{"mode":"dark"}}` {
		t.Fatalf("preference BSON value = subtype %d, %q, %t", subtype, value, ok)
	}

	tests := []struct {
		name         string
		mutate       func(bson.D) bson.D
		want         string
		wantConflict bool
	}{
		{
			name: "unknown key", want: "unknown key",
			mutate: func(document bson.D) bson.D {
				return append(document, bson.E{Key: "future", Value: true})
			},
		},
		{
			name: "wrong codec", want: "unsupported codec",
			mutate: func(document bson.D) bson.D {
				document[1].Value = int32(2)
				return document
			},
		},
		{
			name: "wrong deterministic ID", want: "deterministic ID", wantConflict: true,
			mutate: func(document bson.D) bson.D {
				document[0].Value = "z_preference_wrong"
				return document
			},
		},
		{
			name: "JSON as string", want: "invalid JSON value",
			mutate: func(document bson.D) bson.D {
				document[5].Value = `{"unsafe":true}`
				return document
			},
		},
		{
			name: "invalid JSON binary", want: "invalid JSON value",
			mutate: func(document bson.D) bson.D {
				document[5].Value = bson.Binary{Subtype: 0, Data: []byte(`{`)}
				return document
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := append(bson.D(nil), encoded...)
			fixture = test.mutate(fixture)
			_, err := decodeMongoPreference(marshalMongoSystemFixture(t, fixture))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decode error = %v, want containing %q", err, test.want)
			}
			if test.wantConflict && !errors.Is(err, store.ErrConflict) {
				t.Fatalf("decode error = %v, want ErrConflict", err)
			}
		})
	}
}

func TestMongoDocumentLockCodecIsStrictAndExact(t *testing.T) {
	createdAt := time.Date(2026, time.August, 30, 18, 19, 20, 123456789, time.FixedZone("test", -4*3600))
	lock := store.DocumentLock{
		CollectionID: "posts", DocumentID: "post/界",
		OwnerCollectionID: "users", OwnerID: "user-1", OwnerLabel: "Editor 界",
		CreatedAt: createdAt, UpdatedAt: createdAt.Add(time.Second), ExpiresAt: createdAt.Add(time.Minute),
	}
	encoded, err := encodeMongoDocumentLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMongoDocumentLock(marshalMongoSystemFixture(t, encoded))
	if err != nil {
		t.Fatal(err)
	}
	lock.CreatedAt = lock.CreatedAt.UTC()
	lock.UpdatedAt = lock.UpdatedAt.UTC()
	lock.ExpiresAt = lock.ExpiresAt.UTC()
	if !reflect.DeepEqual(decoded, lock) {
		t.Fatalf("decoded document lock = %#v, want %#v", decoded, lock)
	}

	tests := []struct {
		name         string
		mutate       func(bson.D) bson.D
		want         string
		wantConflict bool
	}{
		{
			name: "unknown key", want: "unknown key",
			mutate: func(document bson.D) bson.D {
				return append(document, bson.E{Key: "future", Value: true})
			},
		},
		{
			name: "wrong deterministic ID", want: "deterministic ID", wantConflict: true,
			mutate: func(document bson.D) bson.D {
				document[0].Value = "z_document_lock_wrong"
				return document
			},
		},
		{
			name: "wrong timestamp type", want: "invalid expiresAt",
			mutate: func(document bson.D) bson.D {
				document[9].Value = time.Now()
				return document
			},
		},
		{
			name: "invalid owner collection", want: "identity",
			mutate: func(document bson.D) bson.D {
				document[4].Value = "Bad Collection"
				return document
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := append(bson.D(nil), encoded...)
			fixture = test.mutate(fixture)
			_, err := decodeMongoDocumentLock(marshalMongoSystemFixture(t, fixture))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decode error = %v, want containing %q", err, test.want)
			}
			if test.wantConflict && !errors.Is(err, store.ErrConflict) {
				t.Fatalf("decode error = %v, want ErrConflict", err)
			}
		})
	}
}

func marshalMongoSystemFixture(t *testing.T, document bson.D) bson.Raw {
	t.Helper()
	encoded, err := bson.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return bson.Raw(encoded)
}
