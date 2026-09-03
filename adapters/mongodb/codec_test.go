package mongodb

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDocumentCodecPreservesCanonicalValuesAndNanoseconds(t *testing.T) {
	createdAt := time.Date(2026, time.August, 30, 9, 10, 11, 123456789, time.FixedZone("test", 3600))
	updatedAt := createdAt.Add(987654321 * time.Nanosecond)
	deletedAt := updatedAt.Add(time.Second)
	document := store.Document{
		ID: "posts/custom key", CreatedAt: createdAt, UpdatedAt: updatedAt, DeletedAt: &deletedAt,
		Status: store.StatusPublished, Revision: 7,
		Values: store.Values{
			"title": store.String("MongoDB"),
			"rank":  store.Number(math.Copysign(0, -1)),
			"flag":  store.Boolean(true),
			"empty": store.Null(),
			"opaque": store.Object(store.Values{
				"": store.String("empty key"), "a.b": store.Number(2), "$literal": store.Boolean(false),
			}),
			"items": store.List(store.String("first"), store.Null(), store.Number(3.5)),
		},
	}
	encoded, err := encodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	rawBytes, err := bson.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeDocument(bson.Raw(rawBytes))
	if err != nil {
		t.Fatal(err)
	}
	document.CreatedAt = document.CreatedAt.UTC()
	document.UpdatedAt = document.UpdatedAt.UTC()
	normalizedDeletedAt := document.DeletedAt.UTC()
	document.DeletedAt = &normalizedDeletedAt
	// The value vocabulary canonicalizes both signed zero encodings.
	document.Values["rank"] = store.Number(0)
	if !reflect.DeepEqual(decoded, document) {
		t.Fatalf("decoded document = %#v, want %#v", decoded, document)
	}
	decoded.Values["title"] = store.String("mutated")
	opaque, _ := decoded.Values["opaque"].ObjectValue()
	opaque["a.b"] = store.Number(99)
	decodedAgain, err := decodeDocument(bson.Raw(rawBytes))
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := decodedAgain.Values["title"].StringValue(); title != "MongoDB" {
		t.Fatalf("caller mutation changed later decode: %q", title)
	}
	metadata, ok := bson.Raw(rawBytes).Lookup("meta").DocumentOK()
	if !ok {
		t.Fatal("encoded metadata is not a BSON document")
	}
	if got, ok := metadata.Lookup("createdAt").Int64OK(); !ok || got != createdAt.UnixNano() {
		t.Fatalf("createdAt BSON = %d, %v; want exact Unix nanoseconds", got, ok)
	}
	if metadata.Lookup("deletedAt").Type != bson.TypeInt64 {
		t.Fatalf("deletedAt BSON type = %s, want long", metadata.Lookup("deletedAt").Type)
	}
}

func TestMongoNumericCodecCanonicalizesSignedZeroAtStorageBoundaries(t *testing.T) {
	negativeZero := math.Copysign(0, -1)
	values := store.Values{
		"single": store.Number(negativeZero),
		"localized": store.Object(store.Values{
			"en": store.Number(negativeZero),
		}),
	}
	encoded, err := encodeValues(values)
	if err != nil {
		t.Fatal(err)
	}
	rawBytes, err := bson.Marshal(bson.D{{Key: "values", Value: encoded}})
	if err != nil {
		t.Fatal(err)
	}
	rawValues, ok := bson.Raw(rawBytes).Lookup("values").DocumentOK()
	if !ok {
		t.Fatal("encoded values are not a BSON document")
	}
	assertMongoRawPositiveZero(t, rawValues, "single")
	localized, ok := rawValues.Lookup("localized").DocumentOK()
	if !ok {
		t.Fatal("encoded localized value is not a BSON document")
	}
	assertMongoRawPositiveZero(t, localized, "en")

	rawNegativeBytes, err := bson.Marshal(bson.D{
		{Key: "single", Value: negativeZero},
		{Key: "localized", Value: bson.D{{Key: "en", Value: negativeZero}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeValues(bson.Raw(rawNegativeBytes))
	if err != nil {
		t.Fatal(err)
	}
	assertStorePositiveZero(t, decoded["single"])
	decodedLocalized, ok := decoded["localized"].ObjectValue()
	if !ok {
		t.Fatalf("decoded localized value = %#v, want object", decoded["localized"])
	}
	assertStorePositiveZero(t, decodedLocalized["en"])

	collection := schema.Collection{
		ID: "signed-zero-codec", Slug: "signed-zero-codec",
		Fields: []schema.Field{
			{
				ID: "signed-zero-codec-single", Name: "single", Path: mongoIndexMustPath(t, "single"),
				Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Number: &schema.NumberField{},
			},
			{
				ID: "signed-zero-codec-localized", Name: "localized", Path: mongoIndexMustPath(t, "localized"),
				Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Localized: true, Number: &schema.NumberField{},
			},
		},
	}
	assignments, err := mongoPatchAssignments(collection, values)
	if err != nil {
		t.Fatal(err)
	}
	assertMongoExpressionPositiveZeros(t, assignments, 2)
}

func assertMongoRawPositiveZero(t testing.TB, document bson.Raw, key string) {
	t.Helper()
	value, ok := document.Lookup(key).DoubleOK()
	if !ok || value != 0 || math.Signbit(value) {
		t.Fatalf("stored MongoDB number %q = %v (double=%v, signbit=%v), want positive zero double", key, value, ok, math.Signbit(value))
	}
}

func assertStorePositiveZero(t testing.TB, value store.Value) {
	t.Helper()
	number, ok := value.NumberValue()
	if !ok || number != 0 || math.Signbit(number) {
		t.Fatalf("decoded store number = %v (number=%v, signbit=%v), want positive zero", number, ok, math.Signbit(number))
	}
}

func assertMongoExpressionPositiveZeros(t testing.TB, value any, want int) {
	t.Helper()
	zeros := 0
	var visit func(any)
	visit = func(current any) {
		switch current := current.(type) {
		case bson.D:
			for _, element := range current {
				visit(element.Value)
			}
		case bson.A:
			for _, element := range current {
				visit(element)
			}
		case float64:
			if current != 0 {
				return
			}
			zeros++
			if math.Signbit(current) {
				t.Errorf("MongoDB update expression contains negative zero: %#v", value)
			}
		}
	}
	visit(value)
	if zeros != want {
		t.Fatalf("MongoDB update expression contains %d zero values, want %d: %#v", zeros, want, value)
	}
}

func TestMongoDocumentCodecUsesExplicitNullDeletionMetadata(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 11, 12, 13, time.UTC)
	encoded, err := encodeDocument(store.Document{ID: "post-1", CreatedAt: now, UpdatedAt: now, Values: store.Values{}})
	if err != nil {
		t.Fatal(err)
	}
	rawBytes, err := bson.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := bson.Raw(rawBytes).Lookup("meta").DocumentOK()
	if !ok || metadata.Lookup("deletedAt").Type != bson.TypeNull {
		t.Fatalf("active deletion metadata = %s, want explicit BSON null", metadata.Lookup("deletedAt").Type)
	}
}

func TestMongoDocumentCodecRejectsResponseOnlyAndNonFiniteValues(t *testing.T) {
	for name, value := range map[string]store.Value{
		"nan":      store.Number(math.NaN()),
		"infinity": store.Number(math.Inf(1)),
		"populated": store.Populated(store.Document{
			ID: "related", Values: store.Values{},
		}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := encodeValue(value); err == nil {
				t.Fatal("unsafe value was accepted")
			}
		})
	}
}

func TestMongoDocumentCodecFailsClosedOnPhysicalDrift(t *testing.T) {
	now := time.Now().UTC()
	base, err := encodeDocument(store.Document{ID: "post-1", CreatedAt: now, UpdatedAt: now, Values: store.Values{}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(bson.D) bson.D
		want   string
	}{
		{
			name: "unknown root", want: "unknown key",
			mutate: func(document bson.D) bson.D {
				return append(document, bson.E{Key: "secret", Value: "leak"})
			},
		},
		{
			name: "unknown metadata", want: "unknown key",
			mutate: func(document bson.D) bson.D {
				metadata := append(bson.D(nil), document[1].Value.(bson.D)...)
				metadata = append(metadata, bson.E{Key: "future", Value: "unreviewed"})
				document[1].Value = metadata
				return document
			},
		},
		{
			name: "missing values", want: "missing required keys",
			mutate: func(document bson.D) bson.D { return document[:2] },
		},
		{
			name: "wrong codec", want: "unsupported codec",
			mutate: func(document bson.D) bson.D {
				metadata := append(bson.D(nil), document[1].Value.(bson.D)...)
				metadata[0].Value = int32(2)
				document[1].Value = metadata
				return document
			},
		},
		{
			name: "millisecond date", want: "invalid createdAt",
			mutate: func(document bson.D) bson.D {
				metadata := append(bson.D(nil), document[1].Value.(bson.D)...)
				metadata[2].Value = now
				document[1].Value = metadata
				return document
			},
		},
		{
			name: "invalid incarnation", want: "invalid incarnation",
			mutate: func(document bson.D) bson.D {
				metadata := append(bson.D(nil), document[1].Value.(bson.D)...)
				metadata[1].Value = "not-an-incarnation"
				document[1].Value = metadata
				return document
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := append(bson.D(nil), base...)
			document = test.mutate(document)
			encoded, err := bson.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeDocument(bson.Raw(encoded)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decode error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMongoCollectionDecodeRejectsSchemaAndMetadataDrift(t *testing.T) {
	collection := mongoScalarCollection(false)
	collection.Fields = append([]schema.Field(nil), collection.Fields...)
	collection.Fields[0].Required = true
	now := time.Now().UTC()
	tests := []struct {
		name     string
		document store.Document
		want     string
	}{
		{
			name: "unknown authored key",
			document: store.Document{ID: "post-unknown", CreatedAt: now, UpdatedAt: now, Values: store.Values{
				"title": store.String("safe"), "rank": store.Number(1), "removed": store.String("secret"),
			}},
			want: "not a stored field",
		},
		{
			name: "scalar stored as list",
			document: store.Document{ID: "post-list", CreatedAt: now, UpdatedAt: now, Values: store.Values{
				"title": store.List(store.String("unsafe")), "rank": store.Number(1),
			}},
			want: "does not match field type",
		},
		{
			name:     "missing required field",
			document: store.Document{ID: "post-missing", CreatedAt: now, UpdatedAt: now, Values: store.Values{"rank": store.Number(1)}},
			want:     "missing required field",
		},
		{
			name: "version metadata in unversioned collection",
			document: store.Document{ID: "post-version", CreatedAt: now, UpdatedAt: now, Revision: 1, Values: store.Values{
				"title": store.String("unsafe"), "rank": store.Number(1),
			}},
			want: "version metadata",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := encodeDocument(test.document)
			if err != nil {
				t.Fatal(err)
			}
			bytes, err := bson.Marshal(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeCollectionDocument(bson.Raw(bytes), collection); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("collection decode error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMongoCollectionCodecPreservesNestedGroupObjects(t *testing.T) {
	now := time.Now().UTC()
	document := store.Document{
		ID: "nested", CreatedAt: now, UpdatedAt: now,
		Values: store.Values{
			"title": store.String("group"),
			"seo": store.Object(store.Values{
				"headline": store.String("safe"),
				"rank":     store.Number(2),
				"details":  store.Object(store.Values{"summary": store.String("deep")}),
			}),
		},
	}
	encoded, err := encodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := bson.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeCollectionDocument(bson.Raw(raw), mongoGroupCollection())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, document) {
		t.Fatalf("nested document round trip = %#v, want %#v", decoded, document)
	}
}

func TestMongoCollectionCodecPreservesAndStrictlyValidatesRepeatedRoots(t *testing.T) {
	collection := mongoRepeatedCollection()
	now := time.Date(2026, time.August, 30, 12, 34, 56, 789, time.UTC)
	valid := store.Document{
		ID: "repeated", CreatedAt: now, UpdatedAt: now,
		Values: mongoRepeatedValues(),
	}
	encoded, err := encodeDocument(valid)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := bson.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeCollectionDocument(bson.Raw(raw), collection)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, valid) {
		t.Fatalf("repeated document round trip = %#v, want %#v", decoded, valid)
	}

	tests := []struct {
		name  string
		value store.Value
		field string
		want  string
	}{
		{name: "select root scalar", field: "tags", value: store.String("alpha"), want: "does not match field type"},
		{name: "select item number", field: "tags", value: store.List(store.Number(1)), want: "does not match field type"},
		{name: "array item scalar", field: "rows", value: store.List(store.String("row")), want: "must be an object"},
		{name: "array unknown key", field: "rows", value: store.List(store.Object(store.Values{
			"kind": store.String("primary"), "label": store.String("visible"), "unknown": store.String("leak"),
		})), want: "not a stored field"},
		{name: "array wrong child type", field: "rows", value: store.List(store.Object(store.Values{
			"kind": store.Number(1), "label": store.String("visible"),
		})), want: "does not match field type"},
		{name: "array missing required child", field: "rows", value: store.List(store.Object(store.Values{
			"kind": store.String("primary"),
		})), want: "missing required field"},
		{name: "array invalid select choice", field: "rows", value: store.List(store.Object(store.Values{
			"kind": store.String("primary"), "label": store.String("visible"), "state": store.String("unknown"),
		})), want: "unknown select choice"},
		{name: "array invalid radio choice", field: "rows", value: store.List(store.Object(store.Values{
			"kind": store.String("primary"), "label": store.String("visible"), "tone": store.String("unknown"),
		})), want: "unknown select choice"},
		{name: "block missing discriminator", field: "layout", value: store.List(store.Object(store.Values{
			"heading": store.String("Welcome"),
		})), want: "blockType"},
		{name: "block unknown discriminator", field: "layout", value: store.List(store.Object(store.Values{
			"blockType": store.String("unknown"), "heading": store.String("Welcome"),
		})), want: "invalid blockType"},
		{name: "block field from another type", field: "layout", value: store.List(store.Object(store.Values{
			"blockType": store.String("quote"), "heading": store.String("Quote"), "tone": store.String("bright"),
		})), want: "not a stored field"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := valid
			document.Values = mongoRepeatedValues()
			document.Values[test.field] = test.value
			encoded, err := encodeDocument(document)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := bson.Marshal(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeCollectionDocument(bson.Raw(raw), collection); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("collection decode error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMongoPhysicalCollectionNameIsStableAndOpaque(t *testing.T) {
	first := physicalCollectionName(schema.StableID("articles"))
	if first != physicalCollectionName(schema.StableID("articles")) {
		t.Fatal("physical collection name is not deterministic")
	}
	if first == physicalCollectionName(schema.StableID("article")) || strings.Contains(first, "articles") || !strings.HasPrefix(first, "z_c_") || len(first) != len("z_c_")+16 {
		t.Fatalf("physical collection name = %q, want opaque z_c_ plus 16 hex characters", first)
	}
}

func TestMongoDocumentCodecPreservesCanonicalStringIDs(t *testing.T) {
	now := time.Now().UTC()
	ids := []string{
		"018f6b2a-9786-7c2e-bb2f-50d9f14c47a1",
		"00042",
		"Case-Sensitive/界",
		strings.Repeat("x", store.MaxDocumentIDBytes),
	}
	for _, id := range ids {
		encoded, err := encodeDocument(store.Document{ID: id, CreatedAt: now, UpdatedAt: now, Values: store.Values{}})
		if err != nil {
			t.Fatalf("encode ID %q: %v", id, err)
		}
		bytes, err := bson.Marshal(encoded)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeDocument(bson.Raw(bytes))
		if err != nil || decoded.ID != id {
			t.Fatalf("ID round trip = %q, %v; want %q", decoded.ID, err, id)
		}
	}
}
