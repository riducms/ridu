package mongodb

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func physicalCollectionName(id schema.StableID) string {
	digest := sha256.Sum256([]byte(id))
	return "z_c_" + hex.EncodeToString(digest[:8])
}

func physicalVersionCollectionName(id schema.StableID) string {
	digest := sha256.Sum256([]byte(id))
	return "z_v_" + hex.EncodeToString(digest[:8])
}

func newDocumentID(collectionID schema.StableID) (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate document ID: %w", err)
	}
	return string(collectionID) + "_" + hex.EncodeToString(random), nil
}

func newDocumentIncarnation() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate document incarnation: %w", err)
	}
	return hex.EncodeToString(random), nil
}

func validDocumentIncarnation(value string) bool {
	if len(value) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func encodeTime(value time.Time) (int64, error) {
	value = value.UTC()
	nanoseconds := value.UnixNano()
	if !time.Unix(0, nanoseconds).UTC().Equal(value) {
		return 0, fmt.Errorf("timestamp is outside MongoDB adapter's nanosecond range")
	}
	return nanoseconds, nil
}

func decodeTime(nanoseconds int64) time.Time {
	return time.Unix(0, nanoseconds).UTC()
}

func encodeDocument(document store.Document) (bson.D, error) {
	if err := store.ValidateDocumentID(document.ID); err != nil {
		return nil, err
	}
	values, err := encodeValues(document.Values)
	if err != nil {
		return nil, err
	}
	createdAt, err := encodeTime(document.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("encode createdAt metadata: %w", err)
	}
	updatedAt, err := encodeTime(document.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("encode updatedAt metadata: %w", err)
	}
	incarnation, err := newDocumentIncarnation()
	if err != nil {
		return nil, err
	}
	metadata := bson.D{
		{Key: "codec", Value: int32(1)},
		{Key: "incarnation", Value: incarnation},
		{Key: "createdAt", Value: createdAt},
		{Key: "updatedAt", Value: updatedAt},
		{Key: "deletedAt", Value: nil},
		{Key: "status", Value: string(document.Status)},
		{Key: "revision", Value: int64(document.Revision)},
		{Key: "fence", Value: int64(0)},
	}
	if document.DeletedAt != nil {
		deletedAt, err := encodeTime(*document.DeletedAt)
		if err != nil {
			return nil, fmt.Errorf("encode deletedAt metadata: %w", err)
		}
		metadata[4].Value = deletedAt
	}
	return bson.D{
		{Key: "_id", Value: document.ID},
		{Key: "meta", Value: metadata},
		{Key: "values", Value: values},
	}, nil
}

func encodeValues(values store.Values) (bson.D, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		if !utf8.ValidString(key) || stringsContainNUL(key) {
			return nil, fmt.Errorf("stored value key %q must be valid UTF-8 without NUL", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	encoded := make(bson.D, 0, len(keys))
	for _, key := range keys {
		value, err := encodeValue(values[key])
		if err != nil {
			return nil, fmt.Errorf("encode stored value %q: %w", key, err)
		}
		encoded = append(encoded, bson.E{Key: key, Value: value})
	}
	return encoded, nil
}

func encodeValue(value store.Value) (any, error) {
	switch value.Kind() {
	case store.ValueNull:
		return nil, nil
	case store.ValueString:
		text, _ := value.StringValue()
		if !utf8.ValidString(text) || stringsContainNUL(text) {
			return nil, fmt.Errorf("string must be valid UTF-8 without NUL")
		}
		return text, nil
	case store.ValueNumber:
		number, _ := value.NumberValue()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("number must be finite")
		}
		if number == 0 {
			number = 0
		}
		return number, nil
	case store.ValueBoolean:
		boolean, _ := value.BooleanValue()
		return boolean, nil
	case store.ValueObject:
		keys := make([]string, 0, value.Len())
		for key := range value.Entries() {
			if !utf8.ValidString(key) || stringsContainNUL(key) {
				return nil, fmt.Errorf("stored value key %q must be valid UTF-8 without NUL", key)
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		encoded := make(bson.D, 0, len(keys))
		for _, key := range keys {
			child, err := encodeValue(value.Get(key))
			if err != nil {
				return nil, fmt.Errorf("encode stored value %q: %w", key, err)
			}
			encoded = append(encoded, bson.E{Key: key, Value: child})
		}
		return encoded, nil
	case store.ValueList:
		encoded := make(bson.A, 0, value.Len())
		for item := range value.Elements() {
			value, err := encodeValue(item)
			if err != nil {
				return nil, fmt.Errorf("list item %d: %w", len(encoded), err)
			}
			encoded = append(encoded, value)
		}
		return encoded, nil
	case store.ValueDocument:
		return nil, fmt.Errorf("populated documents are response-only and cannot be persisted")
	default:
		return nil, fmt.Errorf("unsupported store value kind %q", value.Kind())
	}
}

func stringsContainNUL(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] == 0 {
			return true
		}
	}
	return false
}

func decodeDocument(raw bson.Raw) (store.Document, error) {
	if err := requireExactKeys(raw, "MongoDB document", "_id", "meta", "values"); err != nil {
		return store.Document{}, err
	}
	id, ok := raw.Lookup("_id").StringValueOK()
	if !ok {
		return store.Document{}, fmt.Errorf("stored MongoDB document has an invalid ID")
	}
	if err := store.ValidateDocumentID(id); err != nil {
		return store.Document{}, fmt.Errorf("stored MongoDB document ID: %w", err)
	}
	metadata, ok := raw.Lookup("meta").DocumentOK()
	if !ok {
		return store.Document{}, fmt.Errorf("stored MongoDB document has invalid metadata")
	}
	if err := requireExactKeys(metadata, "MongoDB document metadata", "codec", "incarnation", "createdAt", "updatedAt", "deletedAt", "status", "revision", "fence"); err != nil {
		return store.Document{}, err
	}
	codec, ok := metadata.Lookup("codec").Int32OK()
	if !ok || codec != 1 {
		return store.Document{}, fmt.Errorf("stored MongoDB document has an unsupported codec version")
	}
	incarnation, ok := metadata.Lookup("incarnation").StringValueOK()
	if !ok || !validDocumentIncarnation(incarnation) {
		return store.Document{}, fmt.Errorf("stored MongoDB document has invalid incarnation metadata")
	}
	createdAtNanos, ok := metadata.Lookup("createdAt").Int64OK()
	if !ok {
		return store.Document{}, fmt.Errorf("stored MongoDB document has invalid createdAt metadata")
	}
	updatedAtNanos, ok := metadata.Lookup("updatedAt").Int64OK()
	if !ok {
		return store.Document{}, fmt.Errorf("stored MongoDB document has invalid updatedAt metadata")
	}
	revision64, ok := metadata.Lookup("revision").Int64OK()
	if !ok || revision64 < 0 || int64(int(revision64)) != revision64 {
		return store.Document{}, fmt.Errorf("stored MongoDB document has invalid revision metadata")
	}
	var deletedAt *time.Time
	deletedValue := metadata.Lookup("deletedAt")
	if deletedValue.Type != bson.TypeNull {
		nanoseconds, valid := deletedValue.Int64OK()
		if !valid {
			return store.Document{}, fmt.Errorf("stored MongoDB document has invalid deletedAt metadata")
		}
		value := decodeTime(nanoseconds)
		deletedAt = &value
	}
	var status store.Status
	statusValue := metadata.Lookup("status")
	encodedStatus, valid := statusValue.StringValueOK()
	if !valid || (encodedStatus != "" && encodedStatus != string(store.StatusDraft) && encodedStatus != string(store.StatusPublished)) {
		return store.Document{}, fmt.Errorf("stored MongoDB document has invalid status metadata")
	}
	status = store.Status(encodedStatus)
	if _, ok := metadata.Lookup("fence").Int64OK(); !ok {
		return store.Document{}, fmt.Errorf("stored MongoDB document has invalid fence metadata")
	}
	valuesRaw, ok := raw.Lookup("values").DocumentOK()
	if !ok {
		return store.Document{}, fmt.Errorf("stored MongoDB document has invalid values")
	}
	values, err := decodeValues(valuesRaw)
	if err != nil {
		return store.Document{}, err
	}
	return store.Document{
		ID: id, CreatedAt: decodeTime(createdAtNanos), UpdatedAt: decodeTime(updatedAtNanos),
		DeletedAt: deletedAt, Status: status, Revision: int(revision64), Values: values,
	}, nil
}

func decodeDocumentIncarnation(raw bson.Raw) (string, error) {
	if _, err := decodeDocument(raw); err != nil {
		return "", err
	}
	metadata, _ := raw.Lookup("meta").DocumentOK()
	incarnation, _ := metadata.Lookup("incarnation").StringValueOK()
	return incarnation, nil
}

func decodeCollectionDocument(raw bson.Raw, collection schema.Collection) (store.Document, error) {
	document, err := decodeDocument(raw)
	if err != nil {
		return store.Document{}, err
	}
	if collection.Versions == nil {
		if document.Status != "" || document.Revision != 0 {
			return store.Document{}, fmt.Errorf("stored MongoDB document has version metadata in an unversioned collection")
		}
	} else {
		if err := validateMongoVersionMetadata(collection, document.Status, document.Revision); err != nil {
			return store.Document{}, fmt.Errorf("stored MongoDB document has invalid version metadata: %w", err)
		}
	}
	if err := validateCompleteValues(collection, document.Values); err != nil {
		return store.Document{}, fmt.Errorf("stored MongoDB document does not match collection %q: %w", collection.ID, storedSchemaRecoveryError(err))
	}
	return document, nil
}

// decodeCollectionDocumentForLocales applies request-owned locale membership
// validation after the sparse-compatible storage codec has decoded the row.
// Request-less maintenance and FindVersion callers intentionally continue to
// use decodeCollectionDocument so snapshots written before a locale is removed
// remain inspectable without exposing that locale through an operation read.
func decodeCollectionDocumentForLocales(raw bson.Raw, collection schema.Collection, locales []schema.LocaleCode) (store.Document, error) {
	document, err := decodeCollectionDocument(raw, collection)
	if err != nil {
		return store.Document{}, err
	}
	if len(locales) == 0 {
		return document, nil
	}
	if err := validateCompleteValuesForLocales(collection, document.Values, locales); err != nil {
		return store.Document{}, fmt.Errorf("stored MongoDB document does not match configured locales for collection %q: %w", collection.ID, storedSchemaRecoveryError(err))
	}
	return document, nil
}

// Structural embedded failures on stored content use the same recovery port
// as ordinary blocks. Admission stays strict; only the read diagnostic changes.
func storedSchemaRecoveryError(err error) error {
	var failure *embedded.Error
	if errors.As(err, &failure) {
		return &store.SchemaRecoveryError{Issues: []schema.Issue{failure.Issue}}
	}
	return err
}

func validateMongoVersionMetadata(collection schema.Collection, status store.Status, revision int) error {
	if collection.Versions == nil {
		if status != "" || revision != 0 {
			return fmt.Errorf("unversioned collection requires empty status and zero revision")
		}
		return nil
	}
	if revision < 1 {
		return fmt.Errorf("revision must be positive")
	}
	switch status {
	case store.StatusPublished:
	case store.StatusDraft:
		if !collection.Versions.Drafts {
			return fmt.Errorf("draft status requires drafts to be enabled")
		}
	default:
		return fmt.Errorf("status %q is invalid", status)
	}
	return nil
}

func requireExactKeys(raw bson.Raw, scope string, expected ...string) error {
	elements, err := raw.Elements()
	if err != nil {
		return fmt.Errorf("stored %s contains invalid BSON", scope)
	}
	allowed := make(map[string]struct{}, len(expected))
	for _, key := range expected {
		allowed[key] = struct{}{}
	}
	seen := make(map[string]struct{}, len(elements))
	for _, element := range elements {
		key := element.Key()
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("stored %s contains unknown key %q", scope, key)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("stored %s contains duplicate key %q", scope, key)
		}
		seen[key] = struct{}{}
	}
	if len(seen) != len(allowed) {
		return fmt.Errorf("stored %s is missing required keys", scope)
	}
	return nil
}

func decodeValues(raw bson.Raw) (store.Values, error) {
	elements, err := raw.Elements()
	if err != nil {
		return nil, fmt.Errorf("decode stored MongoDB values: invalid BSON")
	}
	values := make(store.Values, len(elements))
	for _, element := range elements {
		key := element.Key()
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("decode stored MongoDB values: duplicate key %q", key)
		}
		value, err := decodeValue(element.Value())
		if err != nil {
			return nil, fmt.Errorf("decode stored MongoDB value %q: %w", key, err)
		}
		values[key] = value
	}
	return values, nil
}

func decodeValue(raw bson.RawValue) (store.Value, error) {
	switch raw.Type {
	case bson.TypeNull:
		return store.Null(), nil
	case bson.TypeString:
		value, ok := raw.StringValueOK()
		if !ok || !utf8.ValidString(value) || stringsContainNUL(value) {
			return store.Value{}, fmt.Errorf("invalid string")
		}
		return store.String(value), nil
	case bson.TypeDouble:
		value, ok := raw.DoubleOK()
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
			return store.Value{}, fmt.Errorf("invalid number")
		}
		if value == 0 {
			value = 0
		}
		return store.Number(value), nil
	case bson.TypeBoolean:
		value, ok := raw.BooleanOK()
		if !ok {
			return store.Value{}, fmt.Errorf("invalid boolean")
		}
		return store.Boolean(value), nil
	case bson.TypeEmbeddedDocument:
		value, ok := raw.DocumentOK()
		if !ok {
			return store.Value{}, fmt.Errorf("invalid object")
		}
		decoded, err := decodeValues(value)
		if err != nil {
			return store.Value{}, err
		}
		return store.Object(decoded), nil
	case bson.TypeArray:
		value, ok := raw.ArrayOK()
		if !ok {
			return store.Value{}, fmt.Errorf("invalid list")
		}
		items, err := value.Values()
		if err != nil {
			return store.Value{}, fmt.Errorf("invalid list BSON")
		}
		decoded := make([]store.Value, len(items))
		for index, item := range items {
			decoded[index], err = decodeValue(item)
			if err != nil {
				return store.Value{}, fmt.Errorf("list item %d: %w", index, err)
			}
		}
		return store.List(decoded...), nil
	default:
		return store.Value{}, fmt.Errorf("unsupported BSON type %s", raw.Type)
	}
}
