package mongodb

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func mongoIndexTestRaw(t *testing.T, value bson.D) bson.Raw {
	t.Helper()
	raw, err := bson.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestMongoCollectionContractUsesEffectiveBehavior(t *testing.T) {
	const physicalName = "z_c_contract"
	canonicalID := mongo.IndexSpecification{
		Name:         "_id_",
		KeysDocument: mongoIndexTestRaw(t, bson.D{{Key: mongoIDPath, Value: mongoAscendingDirection}}),
	}
	tests := []struct {
		name      string
		options   bson.D
		kind      string
		readOnly  bool
		clustered bool
		want      bool
	}{
		{name: "ordinary", options: bson.D{}, want: true},
		{name: "simple collation", options: bson.D{{Key: "collation", Value: bson.D{{Key: "locale", Value: "simple"}}}}, want: true},
		{name: "non-simple collation", options: bson.D{{Key: "collation", Value: bson.D{{Key: "locale", Value: "en"}}}}, want: false},
		{name: "empty validator", options: bson.D{{Key: "validator", Value: bson.D{}}}, want: true},
		{name: "validator settings without validator", options: bson.D{{Key: "validationLevel", Value: "strict"}, {Key: "validationAction", Value: "error"}}, want: true},
		{name: "warning validator", options: bson.D{{Key: "validator", Value: bson.D{{Key: "required", Value: bson.D{{Key: "$exists", Value: true}}}}}, {Key: "validationAction", Value: "warn"}}, want: true},
		{name: "disabled validator", options: bson.D{{Key: "validator", Value: bson.D{{Key: "required", Value: bson.D{{Key: "$exists", Value: true}}}}}, {Key: "validationLevel", Value: "off"}}, want: true},
		{name: "enforcing validator", options: bson.D{{Key: "validator", Value: bson.D{{Key: "required", Value: bson.D{{Key: "$exists", Value: true}}}}}}, want: false},
		{name: "uncapped false", options: bson.D{{Key: "capped", Value: false}, {Key: "size", Value: int64(4096)}}, want: true},
		{name: "capped", options: bson.D{{Key: "capped", Value: true}, {Key: "size", Value: int64(4096)}}, want: false},
		{name: "null expiry", options: bson.D{{Key: "expireAfterSeconds", Value: nil}}, want: true},
		{name: "zero expiry", options: bson.D{{Key: "expireAfterSeconds", Value: int64(0)}}, want: false},
		{name: "plain clustered storage", options: bson.D{{Key: "clusteredIndex", Value: bson.D{{Key: "key", Value: bson.D{{Key: "_id", Value: int32(1)}}}, {Key: "unique", Value: true}}}}, clustered: true, want: true},
		{name: "missing native ID metadata", options: bson.D{}, clustered: true, want: false},
		{name: "change stream observability", options: bson.D{{Key: "changeStreamPreAndPostImages", Value: bson.D{{Key: "enabled", Value: true}}}}, want: true},
		{name: "collection storage tuning", options: bson.D{{Key: "storageEngine", Value: bson.D{{Key: "wiredTiger", Value: bson.D{}}}}, {Key: "indexOptionDefaults", Value: bson.D{{Key: "storageEngine", Value: bson.D{}}}}}, want: true},
		{name: "empty encrypted fields", options: bson.D{{Key: "encryptedFields", Value: bson.D{{Key: "fields", Value: bson.A{}}}}}, want: true},
		{
			name: "effective encrypted fields",
			options: bson.D{{
				Key: "encryptedFields",
				Value: bson.D{{
					Key:   "fields",
					Value: bson.A{bson.D{{Key: "path", Value: "values.secret"}}},
				}},
			}},
			want: false,
		},
		{name: "view", options: bson.D{}, kind: "view", readOnly: true, want: false},
		{name: "read only", options: bson.D{}, kind: "collection", readOnly: true, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind := test.kind
			if kind == "" {
				kind = "collection"
			}
			specification := mongo.CollectionSpecification{
				Name: physicalName, Type: kind, ReadOnly: test.readOnly,
				Options: mongoIndexTestRaw(t, test.options), IDIndex: canonicalID,
			}
			if test.clustered {
				specification.IDIndex = mongo.IndexSpecification{}
			}
			if got := mongoCollectionContractMatches(specification, physicalName); got != test.want {
				t.Fatalf("collection contract match = %t, want %t", got, test.want)
			}
		})
	}
}

func TestMongoNativeIDAndIndexOptionsUseEffectiveSemantics(t *testing.T) {
	base := bson.D{
		{Key: "v", Value: int32(2)},
		{Key: "key", Value: bson.D{{Key: mongoIDPath, Value: mongoAscendingDirection}}},
		{Key: "name", Value: "_id_"},
	}
	for _, test := range []struct {
		name  string
		extra bson.E
		want  bool
	}{
		{name: "canonical", want: true},
		{name: "unique true is inherent", extra: bson.E{Key: "unique", Value: true}, want: true},
		{name: "sparse false", extra: bson.E{Key: "sparse", Value: false}, want: true},
		{name: "simple collation", extra: bson.E{Key: "collation", Value: bson.D{{Key: "locale", Value: "simple"}}}, want: true},
		{name: "null expiry", extra: bson.E{Key: "expireAfterSeconds", Value: nil}, want: true},
		{name: "storage tuning", extra: bson.E{Key: "storageEngine", Value: bson.D{{Key: "wiredTiger", Value: bson.D{}}}}, want: true},
		{name: "bucket size tuning", extra: bson.E{Key: "bucketSize", Value: int32(1)}, want: true},
		{name: "sparse true", extra: bson.E{Key: "sparse", Value: true}, want: false},
		{name: "non-simple collation", extra: bson.E{Key: "collation", Value: bson.D{{Key: "locale", Value: "en"}}}, want: false},
		{name: "effective expiry", extra: bson.E{Key: "expireAfterSeconds", Value: int64(0)}, want: false},
		{name: "hidden true", extra: bson.E{Key: "hidden", Value: true}, want: false},
		{name: "prepare unique true", extra: bson.E{Key: "prepareUnique", Value: true}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := append(bson.D(nil), base...)
			if test.extra.Key != "" {
				document = append(document, test.extra)
			}
			if got := mongoRawNativeIDIndexMatches(mongoIndexTestRaw(t, document)); got != test.want {
				t.Fatalf("native ID match = %t, want %t", got, test.want)
			}
		})
	}
}

func TestMongoUnmanagedIndexHazardsUseEffectiveSemantics(t *testing.T) {
	decimalAscending, err := bson.ParseDecimal128("1.00")
	if err != nil {
		t.Fatal(err)
	}
	decimalDescending, err := bson.ParseDecimal128("-1.0")
	if err != nil {
		t.Fatal(err)
	}
	decimalNearAscending, err := bson.ParseDecimal128("1.000000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		keys   bson.D
		unique bool
		extra  bson.E
		want   bool
	}{
		{name: "ordinary ascending non-unique", keys: bson.D{{Key: "values.title", Value: int32(1)}}, want: false},
		{name: "ordinary descending non-unique", keys: bson.D{{Key: "values.title", Value: int32(-1)}}, want: false},
		{name: "ordinary int64 ascending", keys: bson.D{{Key: "values.title", Value: int64(1)}}, want: false},
		{name: "ordinary double descending", keys: bson.D{{Key: "values.title", Value: float64(-1)}}, want: false},
		{name: "ordinary decimal ascending", keys: bson.D{{Key: "values.title", Value: decimalAscending}}, want: false},
		{name: "ordinary decimal descending", keys: bson.D{{Key: "values.title", Value: decimalDescending}}, want: false},
		{name: "non-unit numeric direction", keys: bson.D{{Key: "values.title", Value: int64(2)}}, want: true},
		{name: "non-unit decimal direction", keys: bson.D{{Key: "values.title", Value: decimalNearAscending}}, want: true},
		{name: "compound B-tree", keys: bson.D{{Key: "values.title", Value: int32(1)}, {Key: "values.rank", Value: int32(1)}}, want: true},
		{name: "2dsphere", keys: bson.D{{Key: "values.location", Value: "2dsphere"}}, want: true},
		{name: "hashed", keys: bson.D{{Key: "values.title", Value: "hashed"}}, want: true},
		{name: "text", keys: bson.D{{Key: "values.title", Value: "text"}}, want: true},
		{name: "wildcard", keys: bson.D{{Key: "values.$**", Value: int32(1)}}, want: true},
		{name: "unique", unique: true, want: true},
		{name: "null expiry", extra: bson.E{Key: "expireAfterSeconds", Value: nil}, want: false},
		{name: "zero-second TTL", extra: bson.E{Key: "expireAfterSeconds", Value: int64(0)}, want: true},
		{name: "prepare unique false", extra: bson.E{Key: "prepareUnique", Value: false}, want: false},
		{name: "prepare unique true", extra: bson.E{Key: "prepareUnique", Value: true}, want: true},
		{name: "hidden tuning", extra: bson.E{Key: "hidden", Value: true}, want: false},
		{name: "storage tuning", extra: bson.E{Key: "storageEngine", Value: bson.D{{Key: "wiredTiger", Value: bson.D{}}}}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			keys := test.keys
			if len(keys) == 0 {
				keys = bson.D{{Key: "values.title", Value: int32(1)}}
			}
			document := bson.D{
				{Key: "name", Value: "application_index"},
				{Key: "key", Value: keys},
			}
			if test.extra.Key != "" {
				document = append(document, test.extra)
			}
			if got := mongoUnmanagedIndexChangesBehavior(mongoIndexTestRaw(t, document), test.unique); got != test.want {
				t.Fatalf("unmanaged write or lifetime hazard = %t, want %t", got, test.want)
			}
		})
	}
}

func TestMongoIndexComparisonAllowsOperationalDifferencesAndRejectsHazards(t *testing.T) {
	keys := bson.D{{Key: "values.title", Value: mongoAscendingDirection}}
	encodedKeys := mongoIndexTestRaw(t, keys)
	definition := mongoIndexDefinition{name: "z_i_owned", keys: keys}
	ordinary := mongoActualIndex{keys: encodedKeys}

	t.Run("ordinary unmanaged non-unique index", func(t *testing.T) {
		actual := map[string]mongoActualIndex{
			"_id_":               {},
			definition.name:      ordinary,
			"application_lookup": {keys: mongoIndexTestRaw(t, bson.D{{Key: "meta.updatedAt", Value: int32(1)}})},
		}
		if err := compareMongoNamedIndexSets("test", []mongoIndexDefinition{definition}, actual, false); err != nil {
			t.Fatalf("harmless unmanaged index rejected: %v", err)
		}
	})

	for _, test := range []struct {
		name  string
		index mongoActualIndex
	}{
		{name: "unique", index: mongoActualIndex{keys: encodedKeys, unique: true, unmanagedBehaviorHazard: true}},
		{name: "ttl", index: mongoActualIndex{keys: encodedKeys, unmanagedBehaviorHazard: true}},
		{name: "prepare unique", index: mongoActualIndex{keys: encodedKeys, unmanagedBehaviorHazard: true}},
	} {
		t.Run("unmanaged "+test.name, func(t *testing.T) {
			actual := map[string]mongoActualIndex{definition.name: ordinary, "application_hazard": test.index}
			if err := compareMongoNamedIndexSets("test", []mongoIndexDefinition{definition}, actual, false); err == nil || !strings.Contains(err.Error(), "can change valid writes") {
				t.Fatalf("unmanaged hazard error = %v", err)
			}
		})
	}

	t.Run("stale Ridu-owned index", func(t *testing.T) {
		actual := map[string]mongoActualIndex{definition.name: ordinary, "z_i_stale": {keys: encodedKeys}}
		if err := compareMongoNamedIndexSets("test", []mongoIndexDefinition{definition}, actual, false); err == nil || !strings.Contains(err.Error(), "reserved for Ridu-owned") {
			t.Fatalf("stale Ridu-owned index error = %v", err)
		}
	})

	t.Run("owned index incompatible", func(t *testing.T) {
		actual := map[string]mongoActualIndex{definition.name: {keys: encodedKeys, ownedIncompatible: true}}
		if err := compareMongoNamedIndexSets("test", []mongoIndexDefinition{definition}, actual, false); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("owned incompatible index error = %v", err)
		}
	})

	t.Run("owned unique mismatch", func(t *testing.T) {
		uniqueDefinition := definition
		uniqueDefinition.unique = true
		actual := map[string]mongoActualIndex{definition.name: ordinary}
		if err := compareMongoNamedIndexSets("test", []mongoIndexDefinition{uniqueDefinition}, actual, false); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("owned uniqueness mismatch error = %v", err)
		}
	})

	t.Run("owned partial-filter mismatch", func(t *testing.T) {
		partialDefinition := definition
		partialDefinition.partialFilter = bson.D{{Key: "values.title", Value: bson.D{{Key: "$type", Value: "string"}}}}
		wrongPartial := mongoIndexTestRaw(t, bson.D{{Key: "values.title", Value: bson.D{{Key: "$type", Value: "number"}}}})
		actual := map[string]mongoActualIndex{definition.name: {keys: encodedKeys, partialFilter: wrongPartial}}
		if err := compareMongoNamedIndexSets("test", []mongoIndexDefinition{partialDefinition}, actual, false); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("owned partial-filter mismatch error = %v", err)
		}
	})

	t.Run("owned index missing", func(t *testing.T) {
		if err := compareMongoNamedIndexSets("test", []mongoIndexDefinition{definition}, nil, false); err == nil || !strings.Contains(err.Error(), "required index") {
			t.Fatalf("missing owned index error = %v", err)
		}
	})
}

func TestMongoIndexFingerprintIncludesPartialFilter(t *testing.T) {
	base := mongoIndexDefinition{
		name:   "z_u_partial_fingerprint",
		keys:   bson.D{{Key: "values.title", Value: mongoAscendingDirection}},
		unique: true,
		partialFilter: bson.D{{
			Key: "values.title", Value: bson.D{{Key: "$type", Value: "string"}},
		}},
	}
	changed := base
	changed.partialFilter = bson.D{{
		Key: "values.title", Value: bson.D{{Key: "$type", Value: "number"}},
	}}
	before, err := mongoIndexFingerprint([]mongoIndexDefinition{base})
	if err != nil {
		t.Fatal(err)
	}
	after, err := mongoIndexFingerprint([]mongoIndexDefinition{changed})
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("partial-filter-only change reused the MongoDB index fingerprint")
	}
}

func TestMongoContentIndexesAddOnlyRequiredMetadata(t *testing.T) {
	base := mongoScalarCollection(false)
	base.ID, base.Slug = "metadata-posts", "metadata-posts"
	versioned := base
	versioned.Capabilities.Versions = true
	versioned.Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 3}
	trash := base
	trash.Capabilities.Trash = true
	trashVersioned := versioned
	trashVersioned.Capabilities.Trash = true
	global := versioned
	global.Capabilities.Global = true

	tests := []struct {
		name       string
		collection schema.Collection
		want       []string
	}{
		{name: "plain collection", collection: base, want: []string{}},
		{name: "trash collection", collection: trash, want: []string{mongoContentLifecycleIndexName}},
		{name: "versioned collection", collection: versioned, want: []string{mongoContentPublicationIndexName}},
		{name: "trash versioned collection", collection: trashVersioned, want: []string{mongoContentLifecycleIndexName, mongoContentPublicationIndexName}},
		{name: "singleton global", collection: global, want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definitions, err := mongoContentIndexes(test.collection)
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, len(definitions))
			for index, definition := range definitions {
				names[index] = definition.name
				if definition.unique || len(definition.partialFilter) != 0 {
					t.Fatalf("metadata index %q has uniqueness or a partial filter: %#v", definition.name, definition)
				}
				for _, key := range definition.keys {
					if key.Key == mongoRevisionPath {
						t.Fatalf("metadata indexes redundantly include revision: %#v", definitions)
					}
				}
			}
			if !reflect.DeepEqual(names, test.want) {
				t.Fatalf("metadata index names = %#v, want %#v", names, test.want)
			}
		})
	}

	definitions, err := mongoContentIndexes(trashVersioned)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]mongoIndexDefinition, len(definitions))
	for _, definition := range definitions {
		byName[definition.name] = definition
	}
	wantLifecycle := bson.D{
		{Key: mongoDeletedAtPath, Value: mongoAscendingDirection},
		{Key: mongoIDPath, Value: mongoAscendingDirection},
	}
	wantPublication := bson.D{
		{Key: mongoStatusPath, Value: mongoAscendingDirection},
		{Key: mongoDeletedAtPath, Value: mongoAscendingDirection},
		{Key: mongoIDPath, Value: mongoAscendingDirection},
	}
	if found := byName[mongoContentLifecycleIndexName]; found.identity != "metadata:lifecycle:metadata-posts" || !reflect.DeepEqual(found.keys, wantLifecycle) {
		t.Fatalf("lifecycle metadata index = %#v", found)
	}
	if found := byName[mongoContentPublicationIndexName]; found.identity != "metadata:publication:metadata-posts" || !reflect.DeepEqual(found.keys, wantPublication) {
		t.Fatalf("publication metadata index = %#v", found)
	}

	renamed := trashVersioned
	renamed.Slug = "renamed-metadata-posts"
	renamedDefinitions, err := mongoContentIndexes(renamed)
	if err != nil {
		t.Fatal(err)
	}
	beforeFingerprint, err := mongoIndexFingerprint(definitions)
	if err != nil {
		t.Fatal(err)
	}
	afterFingerprint, err := mongoIndexFingerprint(renamedDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	if beforeFingerprint != afterFingerprint {
		t.Fatalf("metadata fingerprint changed across slug rename: %q / %q", beforeFingerprint, afterFingerprint)
	}
}

func TestMongoDeclaredIndexesUseStableIdentitiesAndExactShapes(t *testing.T) {
	collection := mongoIndexTestCollection(t)
	definitions, err := mongoDeclaredIndexes(collection)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 4 {
		t.Fatalf("declared MongoDB indexes = %d, want 4", len(definitions))
	}

	var uniqueField, nestedIndex, compoundUnique *mongoIndexDefinition
	for index := range definitions {
		definition := &definitions[index]
		switch {
		case len(definition.keys) == 1 && definition.keys[0].Key == "values.code":
			uniqueField = definition
		case len(definition.keys) == 2 && definition.keys[0].Key == "values.seo.slug":
			nestedIndex = definition
		case len(definition.keys) == 2 && definition.keys[0].Key == "values.tenant" && definition.keys[1].Key == "values.seo.slug":
			compoundUnique = definition
		}
	}
	if uniqueField == nil || !uniqueField.unique || len(uniqueField.partialFilter) == 0 || !strings.HasPrefix(uniqueField.name, "z_u_") {
		t.Fatalf("unique field index = %#v", uniqueField)
	}
	if nestedIndex == nil || nestedIndex.unique || nestedIndex.keys[1].Key != mongoIDPath || !strings.HasPrefix(nestedIndex.name, "z_i_") {
		t.Fatalf("nested field index = %#v", nestedIndex)
	}
	if compoundUnique == nil || !compoundUnique.unique || len(compoundUnique.partialFilter) == 0 {
		t.Fatalf("compound unique index = %#v", compoundUnique)
	}

	renamed := collection
	renamed.Fields = append([]schema.Field(nil), collection.Fields...)
	renamed.Fields[0].Name = "externalCode"
	renamed.Fields[0].Path = mongoIndexMustPath(t, "externalCode")
	renamedDefinitions, err := mongoDeclaredIndexes(renamed)
	if err != nil {
		t.Fatal(err)
	}
	var renamedUnique *mongoIndexDefinition
	for index := range renamedDefinitions {
		if renamedDefinitions[index].keys[0].Key == "values.externalCode" {
			renamedUnique = &renamedDefinitions[index]
			break
		}
	}
	if renamedUnique == nil || renamedUnique.name != uniqueField.name {
		t.Fatalf("field rename index name = %#v, want stable name %q", renamedUnique, uniqueField.name)
	}
	if renamedUnique.keys[0].Key == uniqueField.keys[0].Key {
		t.Fatal("field rename did not change the physical key path")
	}

	nonTrash := collection
	nonTrash.Capabilities.Trash = false
	nonTrashDefinitions, err := mongoDeclaredIndexes(nonTrash)
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range nonTrashDefinitions {
		if !definition.unique {
			continue
		}
		encoded, err := bson.MarshalExtJSON(definition.partialFilter, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), mongoDeletedAtPath) || !strings.Contains(string(encoded), `"$type":"null"`) {
			t.Fatalf("non-trash unique index does not require canonical deletedAt null: %s", encoded)
		}
	}

	duplicate := mongoScalarCollection(false)
	duplicate.Fields = append([]schema.Field(nil), duplicate.Fields[0], duplicate.Fields[0])
	duplicate.Fields[0].Index = true
	duplicate.Fields[1].Index = true
	if _, err := mongoDeclaredIndexes(duplicate); err == nil || !strings.Contains(err.Error(), "index name collision") {
		t.Fatalf("equal-shape declared index collision error = %v", err)
	}
}

func TestMongoLocalizedDeclaredIndexesExpandPerConfiguredLocale(t *testing.T) {
	collection := mongoLocalizedIndexTestCollection(t)
	locales := []schema.LocaleCode{"en", "fr"}
	if err := validateCollectionEnvelope(collection); err != nil {
		t.Fatalf("localized index envelope: %v", err)
	}
	definitions, err := mongoDeclaredIndexesForLocales(collection, locales)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 10 {
		t.Fatalf("localized MongoDB definitions = %d, want 10: %#v", len(definitions), definitions)
	}
	expected := map[string]bool{
		"values.title.en\x00_id":                           false,
		"values.title.fr\x00_id":                           false,
		"values.code.en":                                   true,
		"values.code.fr":                                   true,
		"values.seo.headline.en\x00_id":                    false,
		"values.seo.headline.fr\x00_id":                    false,
		"values.tenant\x00values.title.en":                 true,
		"values.tenant\x00values.title.fr":                 true,
		"values.title.en\x00values.seo.headline.en\x00_id": false,
		"values.title.fr\x00values.seo.headline.fr\x00_id": false,
	}
	seenNames := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		keys := make([]string, len(definition.keys))
		for index, key := range definition.keys {
			keys[index] = key.Key
		}
		shape := strings.Join(keys, "\x00")
		wantUnique, exists := expected[shape]
		if !exists || definition.unique != wantUnique {
			t.Fatalf("unexpected localized MongoDB index shape %q: %#v", shape, definition)
		}
		delete(expected, shape)
		if _, duplicate := seenNames[definition.name]; duplicate {
			t.Fatalf("duplicate localized MongoDB index name %q", definition.name)
		}
		seenNames[definition.name] = struct{}{}
		if strings.Contains(shape, ".en") && strings.Contains(shape, ".fr") {
			t.Fatalf("localized compound index formed a cross-locale tuple: %q", shape)
		}
		if !definition.unique {
			if len(definition.partialFilter) != 0 || definition.keys[len(definition.keys)-1].Key != mongoIDPath {
				t.Fatalf("localized nonunique index is not stable or unfiltered: %#v", definition)
			}
			continue
		}
		encoded, err := bson.MarshalExtJSON(definition.partialFilter, false, false)
		if err != nil {
			t.Fatal(err)
		}
		filter := string(encoded)
		if !strings.Contains(filter, mongoDeletedAtPath) || !strings.Contains(filter, `"$type":"null"`) {
			t.Fatalf("localized unique index lacks active-document guard: %s", filter)
		}
		for _, key := range definition.keys {
			if !strings.Contains(filter, key.Key) || !strings.Contains(filter, `"$type":"string"`) {
				t.Fatalf("localized unique index lacks concrete typed leaf %q: %s", key.Key, filter)
			}
		}
		for _, key := range definition.keys {
			if strings.Contains(key.Key, ".en") || strings.Contains(key.Key, ".fr") {
				localeMap := key.Key[:strings.LastIndex(key.Key, ".")]
				if !strings.Contains(filter, localeMap) || !strings.Contains(filter, `"$type":"object"`) {
					t.Fatalf("localized unique index lacks locale-map guard %q: %s", localeMap, filter)
				}
			}
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing localized MongoDB index shapes: %#v", expected)
	}

	reordered, err := mongoDeclaredIndexesForLocales(collection, []schema.LocaleCode{"fr", "en"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(definitions, reordered) {
		t.Fatalf("locale authoring order changed identical physical definitions:\n%#v\n%#v", definitions, reordered)
	}
	renamed := collection
	renamed.Slug = "localized-index-posts-renamed"
	renamedDefinitions, err := mongoDeclaredIndexesForLocales(renamed, locales)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(definitions, renamedDefinitions) {
		t.Fatal("resource slug rename changed localized physical index identities")
	}
	oneLocale, err := mongoDeclaredIndexesForLocales(collection, []schema.LocaleCode{"en"})
	if err != nil {
		t.Fatal(err)
	}
	beforeFingerprint, err := mongoIndexFingerprint(definitions)
	if err != nil {
		t.Fatal(err)
	}
	afterFingerprint, err := mongoIndexFingerprint(oneLocale)
	if err != nil {
		t.Fatal(err)
	}
	if beforeFingerprint == afterFingerprint {
		t.Fatal("localized index fingerprint ignored configured locale membership")
	}
}

func TestMongoLocalizedIndexPlanningRequiresLocalizationAndRejectsTooManyPhysicalKeys(t *testing.T) {
	localized := mongoLocalizedIndexTestCollection(t)
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Missing localization"},
		Collections: []schema.Collection{localized}, Plugins: []schema.Plugin{},
	})
	if _, err := mongoIndexPlans(manifest); err == nil || !strings.Contains(err.Error(), "require application localization settings") {
		t.Fatalf("localized index without application localization error = %v", err)
	}
	backend := &Store{verifiedIndexes: map[schema.StableID]mongoVerifiedIndexPlan{
		"previously-verified": {fingerprint: "sentinel"},
	}}
	if err := backend.SyncIndexes(t.Context(), manifest); err == nil || !strings.Contains(err.Error(), "require application localization settings") {
		t.Fatalf("localized index sync preflight error = %v", err)
	}
	backend.indexesMu.RLock()
	verifiedAfterFailure := len(backend.verifiedIndexes)
	backend.indexesMu.RUnlock()
	if verifiedAfterFailure != 0 {
		t.Fatalf("failed localized index preflight retained %d verified resources", verifiedAfterFailure)
	}

	fields := make([]schema.Field, 32)
	paths := make([]query.Path, 32)
	for index := range fields {
		name := fmt.Sprintf("field%d", index)
		paths[index] = mongoIndexMustPath(t, name)
		fields[index] = schema.Field{
			ID: schema.StableID("wide-index-" + name), Name: name, Path: paths[index],
			Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
		}
	}
	wide := schema.Collection{
		ID: "wide-index", Slug: "wide-index", Fields: fields,
		Indexes: []schema.CollectionIndex{{Fields: paths}},
	}
	if err := validateCollectionEnvelope(wide); err == nil || !strings.Contains(err.Error(), "33 keys") {
		t.Fatalf("nonunique 32-field physical index error = %v", err)
	}
	wide.Indexes[0].Unique = true
	if err := validateCollectionEnvelope(wide); err != nil {
		t.Fatalf("unique 32-field physical index rejected: %v", err)
	}
}

func TestMongoPhysicalIndexPlanningCountsLocaleAndMetadataExpansion(t *testing.T) {
	bounded := mongoLocalizedIndexLimitTestCollection(t, 31)
	bounded.Capabilities.Trash = true
	plans, err := mongoPhysicalIndexPlans(mongoLocalizedIndexTestManifest(bounded))
	if err != nil {
		t.Fatalf("63 adapter-owned indexes plus native _id_ rejected: %v", err)
	}
	if len(plans.collections) != 1 || len(plans.collections[0].definitions) != mongoMaxIndexesPerCollection-1 {
		t.Fatalf("bounded physical index plan = %#v", plans.collections)
	}

	overflow := bounded
	overflow.Capabilities.Versions = true
	overflow.Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 3}
	if _, err := mongoPhysicalIndexPlans(mongoLocalizedIndexTestManifest(overflow)); err == nil || !strings.Contains(err.Error(), "requires 65 indexes") {
		t.Fatalf("locale and metadata expanded index-count error = %v", err)
	}
}

func TestMongoPhysicalIndexPlanningEnforcesEncodedKeyPatternBytes(t *testing.T) {
	bounded := mongoIndexKeyPatternLimitTestCollection(t, "key-pattern-boundary", mongoMaxIndexKeyPatternBytes)
	plans, err := mongoPhysicalIndexPlans(mongoIndexTestManifest(bounded))
	if err != nil {
		t.Fatalf("exactly bounded key pattern rejected: %v", err)
	}
	if len(plans.collections) != 1 || len(plans.collections[0].definitions) != 1 {
		t.Fatalf("bounded key-pattern plan = %#v", plans.collections)
	}
	encoded, err := bson.Marshal(plans.collections[0].definitions[0].keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != mongoMaxIndexKeyPatternBytes {
		t.Fatalf("bounded planned key pattern = %d bytes, want %d", len(encoded), mongoMaxIndexKeyPatternBytes)
	}

	overflow := mongoIndexKeyPatternLimitTestCollection(t, "key-pattern-overflow", mongoMaxIndexKeyPatternBytes+1)
	if _, err := mongoPhysicalIndexPlans(mongoIndexTestManifest(overflow)); err == nil || !strings.Contains(err.Error(), "2049-byte key pattern") {
		t.Fatalf("oversized planned key-pattern error = %v", err)
	}
}

func TestMongoIndexPlansIncludeGlobalsAndRejectCrossKindIdentityReuse(t *testing.T) {
	collection := mongoScalarCollection(false)
	collection.ID, collection.Slug = "ordinary-posts", "ordinary-posts"
	global := mongoScalarCollection(false)
	global.ID, global.Slug = "global-settings", "settings"
	global.Capabilities = schema.Capabilities{Global: true, Versions: true}
	global.Versions = &schema.VersionSettings{MaxPerDocument: 10}
	global.Fields = append([]schema.Field(nil), global.Fields...)
	global.Fields[0].ID = "global-settings-title"
	global.Fields[0].Index = true
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "MongoDB global index plans"},
		Collections: []schema.Collection{collection}, Globals: []schema.Global{global}, Plugins: []schema.Plugin{},
	})
	plans, err := mongoIndexPlans(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || plans[0].collection.ID != global.ID || plans[1].collection.ID != collection.ID {
		t.Fatalf("MongoDB mixed resource index plans = %#v", plans)
	}
	if len(plans[0].definitions) != 1 || plans[0].definitions[0].keys[0].Key != "values.title" {
		t.Fatalf("MongoDB global content index plan = %#v", plans[0])
	}
	versionPlans := 0
	for _, plan := range mongoSystemIndexPlans(plans) {
		if plan.kind == mongoSystemVersionIndexes && plan.collectionID == global.ID {
			versionPlans++
		}
	}
	if versionPlans != 1 {
		t.Fatalf("MongoDB global version index plans = %d, want 1", versionPlans)
	}

	colliding := manifest.Snapshot()
	colliding.Globals[0].ID = collection.ID
	_, err = mongoPhysicalIndexPlans(schema.NewManifest(colliding))
	if err == nil || !strings.Contains(err.Error(), "share stable resource ID") {
		t.Fatalf("MongoDB cross-kind resource identity collision = %v", err)
	}
	sameKind := manifest.Snapshot()
	duplicateCollection := sameKind.Collections[0]
	duplicateCollection.Slug = "duplicate-posts"
	sameKind.Collections = append(sameKind.Collections, duplicateCollection)
	_, err = mongoPhysicalIndexPlans(schema.NewManifest(sameKind))
	if err == nil || !strings.Contains(err.Error(), "share stable resource ID") {
		t.Fatalf("MongoDB same-kind resource identity collision = %v", err)
	}
}

func TestMongoSystemIndexPlansCoverReferenceAndVersionQueries(t *testing.T) {
	target := mongoScalarCollection(false)
	target.ID, target.Slug = "system-targets", "system-targets"
	path := mongoIndexMustPath(t, "target")
	owner := schema.Collection{
		ID: "system-owners", Slug: "system-owners",
		Fields: []schema.Field{{
			ID: "system-owners-target", Name: "target", Path: path,
			Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
			Relationship: &schema.RelationshipField{
				CollectionID: target.ID, CollectionSlug: target.Slug, OnDelete: schema.ReferenceDeleteNullify,
			},
		}},
	}
	versioned := mongoVersionedCollection(true, 3)
	referenceFreeGlobal := mongoScalarCollection(false)
	referenceFreeGlobal.ID, referenceFreeGlobal.Slug = "system-settings", "system-settings"
	referenceFreeGlobal.Capabilities = schema.Capabilities{Global: true, Versions: true}
	referenceFreeGlobal.Versions = &schema.VersionSettings{MaxPerDocument: 2}
	for _, plan := range mongoSystemIndexPlans([]mongoCollectionIndexPlan{
		{collection: target}, {collection: versioned}, {collection: referenceFreeGlobal},
	}) {
		if plan.kind == mongoSystemReferenceIndexes {
			t.Fatalf("reference-free collection/global plan unexpectedly owns reference state: %#v", plan)
		}
	}
	uploadPath := mongoIndexMustPath(t, "asset")
	uploadOwner := schema.Collection{
		ID: "system-upload-owners", Slug: "system-upload-owners",
		Fields: []schema.Field{{
			ID: "system-upload-owners-asset", Name: "asset", Path: uploadPath,
			Type: schema.FieldTypeUpload, Category: schema.FieldCategoryUpload,
			Upload: &schema.UploadField{
				CollectionID: target.ID, CollectionSlug: target.Slug, OnDelete: schema.ReferenceDeleteNullify,
			},
		}},
	}
	for kind, resource := range map[string]schema.Collection{"relationship": owner, "upload": uploadOwner} {
		referencePlans := 0
		for _, plan := range mongoSystemIndexPlans([]mongoCollectionIndexPlan{{collection: target}, {collection: resource}}) {
			if plan.kind == mongoSystemReferenceIndexes {
				referencePlans++
			}
		}
		if referencePlans != 1 {
			t.Fatalf("MongoDB %s system reference plans = %d, want 1", kind, referencePlans)
		}
	}
	plans := mongoSystemIndexPlans([]mongoCollectionIndexPlan{
		{collection: target}, {collection: owner}, {collection: versioned},
	})
	if len(plans) != 13 {
		t.Fatalf("MongoDB system index plans = %#v, want reference, version, preference, document-lock, upload-lock, two task, and six auth plans", plans)
	}
	var referencePlan, versionPlan, preferencePlan, documentLockPlan, uploadLockPlan, taskPlan, taskConcurrencyPlan *mongoSystemIndexPlan
	authPlans := 0
	for index := range plans {
		if _, err := mongoIndexFingerprint(plans[index].definitions); err != nil {
			t.Fatalf("fingerprint system index plan %q: %v", plans[index].description, err)
		}
		switch plans[index].kind {
		case mongoSystemReferenceIndexes:
			referencePlan = &plans[index]
		case mongoSystemVersionIndexes:
			versionPlan = &plans[index]
		case mongoSystemPreferenceIndexes:
			preferencePlan = &plans[index]
		case mongoSystemDocumentLockIndexes:
			documentLockPlan = &plans[index]
		case mongoSystemUploadLockIndexes:
			uploadLockPlan = &plans[index]
		case mongoSystemTaskIndexes:
			taskPlan = &plans[index]
		case mongoSystemTaskConcurrencyIndexes:
			taskConcurrencyPlan = &plans[index]
		case mongoSystemAuthCredentialIndexes, mongoSystemAuthSessionIndexes, mongoSystemAuthTokenIndexes,
			mongoSystemAuthAPIKeyIndexes, mongoSystemAuthRateLimitIndexes, mongoSystemAuthBootstrapIndexes:
			authPlans++
		}
	}
	if authPlans != 6 {
		t.Fatalf("MongoDB auth system index plans = %d, want 6", authPlans)
	}
	if referencePlan == nil || referencePlan.physicalName != mongoReferenceCollectionName || len(referencePlan.definitions) != 2 {
		t.Fatalf("reference index plan = %#v", referencePlan)
	}
	wantOwnerKeys := bson.D{
		{Key: "ownerCollection", Value: mongoAscendingDirection},
		{Key: "ownerDocument", Value: mongoAscendingDirection},
	}
	wantTargetKeys := bson.D{
		{Key: "targetCollection", Value: mongoAscendingDirection},
		{Key: "targetDocument", Value: mongoAscendingDirection},
		{Key: "ownerCollection", Value: mongoAscendingDirection},
		{Key: "ownerDocument", Value: mongoAscendingDirection},
		{Key: "field", Value: mongoAscendingDirection},
		{Key: "locale", Value: mongoAscendingDirection},
		{Key: "occurrence", Value: mongoAscendingDirection},
	}
	byName := make(map[string]mongoIndexDefinition, len(referencePlan.definitions))
	for _, definition := range referencePlan.definitions {
		byName[definition.name] = definition
	}
	if !reflect.DeepEqual(byName[mongoReferenceOwnerIndexName].keys, wantOwnerKeys) ||
		!reflect.DeepEqual(byName[mongoReferenceTargetIndexName].keys, wantTargetKeys) {
		t.Fatalf("reference index shapes = %#v", byName)
	}
	if versionPlan == nil || versionPlan.collectionID != versioned.ID || len(versionPlan.definitions) != 1 {
		t.Fatalf("version index plan = %#v", versionPlan)
	}
	versionIndex := versionPlan.definitions[0]
	if versionIndex.name != mongoVersionOwnerIndexName || !versionIndex.unique || !reflect.DeepEqual(versionIndex.keys, bson.D{
		{Key: mongoVersionOwnerPath, Value: mongoAscendingDirection},
		{Key: mongoVersionRevisionPath, Value: int32(-1)},
	}) {
		t.Fatalf("version index shape = %#v", versionIndex)
	}
	if preferencePlan == nil || preferencePlan.physicalName != mongoPreferenceCollectionName || len(preferencePlan.definitions) != 1 {
		t.Fatalf("preference index plan = %#v", preferencePlan)
	}
	preferenceIndex := preferencePlan.definitions[0]
	if preferenceIndex.name != mongoPreferenceOwnerIndexName || !preferenceIndex.unique || !reflect.DeepEqual(preferenceIndex.keys, bson.D{
		{Key: "collection", Value: mongoAscendingDirection},
		{Key: "user", Value: mongoAscendingDirection},
		{Key: "key", Value: mongoAscendingDirection},
	}) {
		t.Fatalf("preference index shape = %#v", preferenceIndex)
	}
	if documentLockPlan == nil || documentLockPlan.physicalName != mongoDocumentLockCollectionName || len(documentLockPlan.definitions) != 2 {
		t.Fatalf("document-lock index plan = %#v", documentLockPlan)
	}
	documentLockIndexes := make(map[string]mongoIndexDefinition, len(documentLockPlan.definitions))
	for _, definition := range documentLockPlan.definitions {
		documentLockIndexes[definition.name] = definition
	}
	if targetIndex := documentLockIndexes[mongoDocumentLockTargetIndexName]; !targetIndex.unique || !reflect.DeepEqual(targetIndex.keys, bson.D{
		{Key: "collection", Value: mongoAscendingDirection},
		{Key: "document", Value: mongoAscendingDirection},
	}) {
		t.Fatalf("document-lock target index = %#v", targetIndex)
	}
	if ownerIndex := documentLockIndexes[mongoDocumentLockOwnerIndexName]; ownerIndex.unique || !reflect.DeepEqual(ownerIndex.keys, bson.D{
		{Key: "ownerCollection", Value: mongoAscendingDirection},
		{Key: "owner", Value: mongoAscendingDirection},
	}) {
		t.Fatalf("document-lock owner index = %#v", ownerIndex)
	}
	if uploadLockPlan == nil || uploadLockPlan.physicalName != mongoUploadLockCollectionName || len(uploadLockPlan.definitions) != 0 {
		t.Fatalf("upload-lock index plan = %#v", uploadLockPlan)
	}
	if taskPlan == nil || taskPlan.physicalName != mongoTaskCollectionName || len(taskPlan.definitions) != 5 {
		t.Fatalf("task index plan = %#v", taskPlan)
	}
	if taskConcurrencyPlan == nil || taskConcurrencyPlan.physicalName != mongoTaskConcurrencyCollectionName || len(taskConcurrencyPlan.definitions) != 3 {
		t.Fatalf("task concurrency index plan = %#v", taskConcurrencyPlan)
	}
	if plans := mongoSystemIndexPlans([]mongoCollectionIndexPlan{{collection: target}}); len(plans) != 11 {
		t.Fatalf("scalar manifest system plans = %#v, want preference, document-lock, upload-lock, two task, and six auth plans", plans)
	}
}

func TestMongoResourcesRequireExplicitCompletePlanVerification(t *testing.T) {
	unindexed := mongoScalarCollection(false)
	unindexed.ID, unindexed.Slug = "unindexed-resource", "unindexed-resource"
	backend := &Store{verifiedIndexes: make(map[schema.StableID]mongoVerifiedIndexPlan)}
	if err := backend.requireVerifiedIndexes(unindexed); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("unverified zero-index resource error = %v", err)
	}
	emptyFingerprint, err := mongoIndexFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	backend.verifiedIndexes[unindexed.ID] = mongoVerifiedIndexPlan{fingerprint: emptyFingerprint}
	if err := backend.requireVerifiedIndexes(unindexed); err != nil {
		t.Fatalf("verified zero-index resource rejected: %v", err)
	}
	if err := backend.requireVerifiedResourceID("unknown-resource"); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("unknown resource ID verification error = %v", err)
	}
	if err := backend.requireVerifiedResourceID(unindexed.ID); err != nil {
		t.Fatalf("verified resource ID rejected: %v", err)
	}
	versioned, err := backend.requireVerifiedResourcePlan(unindexed.ID)
	if err != nil || versioned {
		t.Fatalf("zero-index resource plan = versioned %t, error %v", versioned, err)
	}
	backend.verifiedVersionIndexes = map[schema.StableID]bool{unindexed.ID: true}
	versioned, err = backend.requireVerifiedResourcePlan(unindexed.ID)
	if err != nil || !versioned {
		t.Fatalf("version-enabled resource plan = versioned %t, error %v", versioned, err)
	}
	backend.clearVerifiedIndexes()

	collection := mongoIndexTestCollection(t)
	if err := backend.requireVerifiedIndexes(collection); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("unverified indexed collection error = %v", err)
	}
	definitions, err := mongoContentIndexes(collection)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := mongoIndexFingerprint(definitions)
	if err != nil {
		t.Fatal(err)
	}
	backend.verifiedIndexes[collection.ID] = mongoVerifiedIndexPlan{fingerprint: fingerprint}
	if err := backend.requireVerifiedIndexes(collection); err != nil {
		t.Fatalf("verified indexed collection rejected: %v", err)
	}
	capabilityChanges := []struct {
		name   string
		change func(*schema.Collection)
	}{
		{name: "trash", change: func(candidate *schema.Collection) { candidate.Capabilities.Trash = false }},
		{name: "versions", change: func(candidate *schema.Collection) {
			candidate.Capabilities.Versions = true
			candidate.Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 3}
		}},
		{name: "global", change: func(candidate *schema.Collection) {
			candidate.Capabilities.Global = true
			candidate.Capabilities.Trash = false
		}},
	}
	for _, test := range capabilityChanges {
		t.Run("changed "+test.name+" capability", func(t *testing.T) {
			candidate := collection
			test.change(&candidate)
			if err := backend.requireVerifiedIndexes(candidate); err == nil || !strings.Contains(err.Error(), "not verified") {
				t.Fatalf("changed %s capability reused stale verification: %v", test.name, err)
			}
		})
	}

	changed := collection
	changed.Indexes = nil
	changed.Fields = append([]schema.Field(nil), collection.Fields...)
	changed.Fields[1].Unique = true
	if err := backend.requireVerifiedIndexes(changed); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("changed index contract reused stale verification: %v", err)
	}
}

func TestMongoLocalizedVerificationBindsExactLocaleOrderAndIndexFingerprint(t *testing.T) {
	collection := mongoLocalizedIndexTestCollection(t)
	locales := []schema.LocaleCode{"en", "fr"}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{
			Name: "Localized verification",
			Localization: &schema.LocalizationSettings{
				DefaultLocale: "en",
				Locales:       []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}},
			},
		},
		Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
	})
	plans, err := mongoIndexPlans(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || !mongoLocaleListsEqual(plans[0].locales, locales) {
		t.Fatalf("localized MongoDB plan locales = %#v", plans)
	}
	backend := &Store{verifiedIndexes: map[schema.StableID]mongoVerifiedIndexPlan{
		collection.ID: {fingerprint: plans[0].fingerprint, locales: append([]schema.LocaleCode(nil), plans[0].locales...)},
	}}
	if err := backend.requireVerifiedIndexes(collection); err != nil {
		t.Fatalf("localized physical fingerprint rejected: %v", err)
	}
	if err := backend.requireVerifiedIndexesForLocales(collection, locales); err != nil {
		t.Fatalf("exact verified locale order rejected: %v", err)
	}
	for _, candidate := range [][]schema.LocaleCode{{"fr", "en"}, {"en"}, {"en", "fr", "de"}, nil} {
		if err := backend.requireVerifiedIndexesForLocales(collection, candidate); err == nil || !strings.Contains(err.Error(), "locale configuration") {
			t.Fatalf("locale drift %#v reused verified physical plan: %v", candidate, err)
		}
	}

	changed := collection
	changed.Fields = append([]schema.Field(nil), collection.Fields...)
	changed.Fields[0].Index = false
	if err := backend.requireVerifiedIndexes(changed); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("localized declaration drift reused verified physical plan: %v", err)
	}
}

func TestMongoSystemStoresRequireExplicitPhysicalVerification(t *testing.T) {
	backend := &Store{
		verifiedIndexes:        make(map[schema.StableID]mongoVerifiedIndexPlan),
		verifiedVersionIndexes: make(map[schema.StableID]bool),
	}
	if err := backend.requireVerifiedPreferenceIndexes(); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("unverified preference indexes error = %v", err)
	}
	if err := backend.requireVerifiedDocumentLockIndexes(); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("unverified document-lock indexes error = %v", err)
	}
	if err := backend.requireVerifiedTaskIndexes(); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("unverified task indexes error = %v", err)
	}
	if err := backend.requireVerifiedUploadLockIndexes(); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("unverified upload-lock indexes error = %v", err)
	}
	backend.indexesMu.Lock()
	backend.verifiedPreferenceIndexes = true
	backend.verifiedDocumentLockIndexes = true
	backend.verifiedTaskIndexes = true
	backend.verifiedUploadLockIndexes = true
	backend.indexesMu.Unlock()
	if err := backend.requireVerifiedPreferenceIndexes(); err != nil {
		t.Fatalf("verified preference indexes rejected: %v", err)
	}
	if err := backend.requireVerifiedDocumentLockIndexes(); err != nil {
		t.Fatalf("verified document-lock indexes rejected: %v", err)
	}
	if err := backend.requireVerifiedUploadLockIndexes(); err != nil {
		t.Fatalf("verified upload-lock indexes rejected: %v", err)
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		t.Fatalf("verified task indexes rejected: %v", err)
	}
	backend.clearVerifiedIndexes()
	if err := backend.requireVerifiedPreferenceIndexes(); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("cleared preference verification survived: %v", err)
	}
	if err := backend.requireVerifiedDocumentLockIndexes(); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("cleared document-lock verification survived: %v", err)
	}
	if err := backend.requireVerifiedTaskIndexes(); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("cleared task verification survived: %v", err)
	}
}

func TestMongoIndexPlannerRejectsNestedUniqueFields(t *testing.T) {
	collection := mongoIndexTestCollection(t)
	collection.Fields = append([]schema.Field(nil), collection.Fields...)
	group := collection.Fields[3]
	group.Nested = &schema.NestedField{Fields: append([]schema.Field(nil), group.Nested.ResolvedFields()...)}
	group.Nested.ResolvedFields()[0].Unique = true
	collection.Fields[3] = group
	if err := validateCollectionEnvelope(collection); err == nil || !strings.Contains(err.Error(), "cannot enforce unique nested field") {
		t.Fatalf("nested unique field error = %v", err)
	}
}

func mongoIndexTestCollection(t *testing.T) schema.Collection {
	t.Helper()
	code := mongoIndexMustPath(t, "code")
	rank := mongoIndexMustPath(t, "rank")
	tenant := mongoIndexMustPath(t, "tenant")
	seo := mongoIndexMustPath(t, "seo")
	slug := mongoIndexMustPath(t, "seo", "slug")
	return schema.Collection{
		ID: "indexed-posts", Slug: "indexed-posts",
		Labels:       schema.CollectionLabels{Singular: "Indexed post", Plural: "Indexed posts"},
		Capabilities: schema.Capabilities{Trash: true},
		Fields: []schema.Field{
			{ID: "indexed-posts-code", Name: "code", Path: code, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Unique: true, Text: &schema.TextField{}},
			{ID: "indexed-posts-rank", Name: "rank", Path: rank, Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Index: true, Number: &schema.NumberField{}},
			{ID: "indexed-posts-tenant", Name: "tenant", Path: tenant, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}},
			{
				ID: "indexed-posts-seo", Name: "seo", Path: seo, Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
				Nested: &schema.NestedField{Fields: []schema.Field{
					{ID: "indexed-posts-seo-slug", Name: "slug", Path: slug, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Index: true, Text: &schema.TextField{}},
				}},
			},
		},
		Indexes: []schema.CollectionIndex{{Fields: []query.Path{tenant, slug}, Unique: true}},
	}
}

func mongoLocalizedIndexTestCollection(t *testing.T) schema.Collection {
	t.Helper()
	collection := mongoLocalizedScalarCollection()
	collection.ID, collection.Slug = "localized-index-posts", "localized-index-posts"
	collection.Capabilities.Trash = true
	collection.Fields = append([]schema.Field(nil), collection.Fields...)
	collection.Fields[0].Index = true
	group := collection.Fields[1]
	group.Nested = &schema.NestedField{Fields: append([]schema.Field(nil), group.Nested.ResolvedFields()...)}
	group.Nested.ResolvedFields()[0].Index = true
	collection.Fields[1] = group
	code := mongoIndexMustPath(t, "code")
	tenant := mongoIndexMustPath(t, "tenant")
	collection.Fields = append(collection.Fields,
		schema.Field{
			ID: "localized-index-posts-code", Name: "code", Path: code,
			Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar,
			Localized: true, Unique: true, Text: &schema.TextField{},
		},
		schema.Field{
			ID: "localized-index-posts-tenant", Name: "tenant", Path: tenant,
			Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
		},
	)
	collection.Indexes = []schema.CollectionIndex{
		{Fields: []query.Path{tenant, collection.Fields[0].Path}, Unique: true},
		{Fields: []query.Path{collection.Fields[0].Path, group.Nested.ResolvedFields()[0].Path}},
	}
	return collection
}

func mongoLocalizedIndexLimitTestCollection(t *testing.T, fieldCount int) schema.Collection {
	t.Helper()
	fields := make([]schema.Field, fieldCount)
	for index := range fields {
		name := fmt.Sprintf("field%d", index)
		fields[index] = schema.Field{
			ID: schema.StableID("localized-index-limit-" + name), Name: name, Path: mongoIndexMustPath(t, name),
			Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar,
			Localized: true, Index: true, Text: &schema.TextField{},
		}
	}
	return schema.Collection{
		ID: "localized-index-limit", Slug: "localized-index-limit",
		Labels: schema.CollectionLabels{Singular: "Localized index limit", Plural: "Localized index limits"},
		Fields: fields,
	}
}

func mongoIndexKeyPatternLimitTestCollection(t *testing.T, id string, encodedBytes int) schema.Collection {
	t.Helper()
	// A one-key int32 BSON document uses 11 bytes beyond its key bytes,
	// and the stored authored path adds the "values." prefix.
	nameBytes := encodedBytes - 11 - len("values.")
	if nameBytes < 1 {
		t.Fatalf("encoded MongoDB key-pattern size %d is too small for a field path", encodedBytes)
	}
	name := strings.Repeat("a", nameBytes)
	return schema.Collection{
		ID: schema.StableID(id), Slug: schema.CollectionSlug(id),
		Labels: schema.CollectionLabels{Singular: "Key pattern", Plural: "Key patterns"},
		Fields: []schema.Field{{
			ID: schema.StableID(id + "-field"), Name: name, Path: mongoIndexMustPath(t, name),
			Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Unique: true, Text: &schema.TextField{},
		}},
	}
}

func mongoIndexMustPath(t *testing.T, segments ...string) query.Path {
	t.Helper()
	path, err := query.NewPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
