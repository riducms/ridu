package mongodb

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoLocalizedScalarEnvelopeAndCanonicalValuesStayBounded(t *testing.T) {
	collection := mongoLocalizedScalarCollection()
	if err := validateCollectionEnvelope(collection); err != nil {
		t.Fatalf("localized scalar envelope: %v", err)
	}
	valid := store.Values{
		"title": store.Object(store.Values{"en": store.String("Hello"), "fr": store.Null()}),
		"seo": store.Object(store.Values{
			"headline": store.Object(store.Values{"en": store.String("Safe"), "fr": store.String("Sûr")}),
			"rank":     store.Number(2),
			"details":  store.Object(store.Values{"summary": store.String("Summary")}),
		}),
	}
	if err := validateCompleteValuesForLocales(collection, valid, []schema.LocaleCode{"en", "fr"}); err != nil {
		t.Fatalf("canonical localized values: %v", err)
	}

	tests := []struct {
		name   string
		values store.Values
		want   string
	}{
		{name: "scalar instead of locale map", values: store.Values{"title": store.String("Hello"), "seo": valid["seo"]}, want: "canonical locale-keyed"},
		{name: "unknown locale", values: store.Values{"title": store.Object(store.Values{"de": store.String("Hallo")}), "seo": valid["seo"]}, want: "unconfigured locale"},
		{name: "invalid locale key", values: store.Values{"title": store.Object(store.Values{"en.bad": store.String("bad")}), "seo": valid["seo"]}, want: "invalid locale"},
		{name: "wrong localized scalar type", values: store.Values{"title": store.Object(store.Values{"en": store.Number(1)}), "seo": valid["seo"]}, want: "does not match field type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateCompleteValuesForLocales(collection, test.values, []schema.LocaleCode{"en", "fr"}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("localized value error = %v, want containing %q", err, test.want)
			}
		})
	}
	if err := validateCompleteValuesForLocales(collection, valid, []schema.LocaleCode{"en", "en"}); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate locale error = %v", err)
	}
}

func TestMongoLocalizedEnvelopeAdmitsPortableShapes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*schema.Collection)
	}{
		{name: "localized group", mutate: func(collection *schema.Collection) {
			collection.Fields[1].Localized = true
			collection.Fields[1].Nested.ResolvedFields()[0].Localized = false
		}},
		{name: "localized relationship", mutate: func(collection *schema.Collection) {
			collection.Fields[0].Type = schema.FieldTypeRelationship
			collection.Fields[0].Category = schema.FieldCategoryRelationship
			collection.Fields[0].Text = nil
			collection.Fields[0].Relationship = &schema.RelationshipField{
				CollectionID: "users", CollectionSlug: "users", OnDelete: schema.ReferenceDeleteRestrict,
			}
		}},
		{name: "localized repeated select", mutate: func(collection *schema.Collection) {
			collection.Fields[0].Type = schema.FieldTypeSelect
			collection.Fields[0].Text = nil
			collection.Fields[0].Select = &schema.SelectField{HasMany: true, Options: []schema.SelectOption{{Value: "one", Label: "One"}}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			collection := mongoLocalizedScalarCollection()
			test.mutate(&collection)
			if err := validateCollectionEnvelope(collection); err != nil {
				t.Fatalf("localized envelope error = %v", err)
			}
		})
	}
}

func TestMongoLocalizedPredicatesUseOrderedFallbackAndPerLocaleAccess(t *testing.T) {
	collection := mongoLocalizedScalarCollection()
	title := collection.Fields[0].Path
	filter := query.Equal(title, query.String("visible")).Node()
	compiled, err := compileMongoNode(collection, filter, "filter", mongoPredicateScope{localeChain: []schema.LocaleCode{"fr", "en"}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bson.MarshalExtJSON(compiled, false, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, fragment := range []string{`"$let"`, `"$cond"`, `"$type"`, `"$expr"`, `$values.title.fr`, `$values.title.en`} {
		if !strings.Contains(text, fragment) {
			t.Errorf("localized fallback predicate lacks %q: %s", fragment, text)
		}
	}
	if !strings.Contains(text, `"$ne":["$values.title.fr",""]`) {
		t.Fatalf("localized fallback predicate lacks non-final empty-string guard: %s", text)
	}

	access, err := requestPredicate(store.Request{
		Collection:  collection,
		Access:      &filter,
		Locales:     []schema.LocaleCode{"en", "fr"},
		LocaleChain: []schema.LocaleCode{"fr", "en"},
		AllLocales:  true,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = bson.MarshalExtJSON(access, false, false)
	if err != nil {
		t.Fatal(err)
	}
	text = string(encoded)
	if strings.Count(text, `$values.title.en`) == 0 || strings.Count(text, `$values.title.fr`) == 0 {
		t.Fatalf("all-locales access did not compile every exact locale: %s", text)
	}
}

func TestMongoLocalizedSortProjectionAndVersionScopeRemainCanonical(t *testing.T) {
	collection := mongoLocalizedScalarCollection()
	title := collection.Fields[0].Path
	descending, err := query.NewSort(title, query.Descending)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := requestSort(store.Request{
		Collection:  collection,
		LocaleChain: []schema.LocaleCode{"fr", "en"},
		Sort:        []query.Sort{descending},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := (bson.D{{Key: "__riduLocalizedSort0", Value: int32(-1)}, {Key: mongoIDPath, Value: int32(1)}}); !reflect.DeepEqual(plan.order, want) {
		t.Fatalf("localized sort order = %#v, want %#v", plan.order, want)
	}
	if len(plan.computed) != 1 || !reflect.DeepEqual(plan.temporary, bson.A{"__riduLocalizedSort0"}) {
		t.Fatalf("localized sort plan = %#v", plan)
	}
	projection, err := requestProjection(store.Request{Collection: collection, Select: []query.Path{title}})
	if err != nil {
		t.Fatal(err)
	}
	if got := mongoDirectValue(projection, "values.title"); got != int32(1) {
		t.Fatalf("localized projection = %#v, want whole canonical locale map", projection)
	}

	versioned := collection
	versioned.Capabilities.Versions = true
	versioned.Versions = &schema.VersionSettings{Drafts: true}
	versionPredicate, err := compileMongoNode(
		versioned,
		query.Equal(title, query.String("visible")).Node(),
		"version access",
		mongoPredicateScope{localeChain: []schema.LocaleCode{"fr", "en"}, storagePrefix: mongoVersionSnapshotPath + "."},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bson.MarshalExtJSON(versionPredicate, false, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `$snapshot.values.title.fr`) || !strings.Contains(text, `$snapshot.values.title.en`) {
		t.Fatalf("localized version predicate is not snapshot-scoped: %s", text)
	}
	if strings.Contains(text, `"$values.title`) {
		t.Fatalf("localized version predicate retained an unscoped field reference: %s", text)
	}
}

func TestMongoLocalizedPatchExpressionsMergeMapsAndGroupSiblings(t *testing.T) {
	collection := mongoLocalizedScalarCollection()
	assignments, err := mongoPatchAssignments(collection, store.Values{
		"title": store.Object(store.Values{"fr": store.String("$Bonjour")}),
		"seo": store.Object(store.Values{
			"headline": store.Object(store.Values{"fr": store.String("Accueil")}),
			"details":  store.Object(store.Values{"summary": store.String("changed")}),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bson.MarshalExtJSON(assignments, false, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, fragment := range []string{
		`"values.title":{"$mergeObjects":["$values.title",{"fr":{"$literal":"$Bonjour"}}]}`,
		`"values.seo":{"$mergeObjects":["$values.seo"`,
		`"headline":{"$mergeObjects":["$values.seo.headline"`,
		`"details":{"$mergeObjects":["$values.seo.details"`,
		`"summary":{"$literal":"changed"}`,
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("atomic localized patch lacks %q: %s", fragment, text)
		}
	}

	collection.Fields[1].Localized = true
	collection.Fields[1].Nested.ResolvedFields()[0].Localized = false
	localizedGroup, err := mongoPatchAssignments(collection, store.Values{
		"seo": store.Object(store.Values{
			"en": store.Object(store.Values{
				"details": store.Object(store.Values{"summary": store.String("changed")}),
			}),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = bson.MarshalExtJSON(localizedGroup, false, false)
	if err != nil {
		t.Fatal(err)
	}
	text = string(encoded)
	for _, fragment := range []string{
		`"values.seo":{"$mergeObjects":["$values.seo",{"en":{"$mergeObjects":["$values.seo.en"`,
		`"details":{"$mergeObjects":["$values.seo.en.details"`,
		`"summary":{"$literal":"changed"}`,
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("atomic localized group patch lacks %q: %s", fragment, text)
		}
	}
}

func TestMongoRequestAwareDecodeRejectsUnconfiguredLocales(t *testing.T) {
	collection := mongoLocalizedScalarCollection()
	now := time.Unix(1_700_000_000, 0).UTC()
	encoded, err := encodeDocument(store.Document{
		ID: "posts_locales", CreatedAt: now, UpdatedAt: now,
		Values: store.Values{
			"title": store.Object(store.Values{"en": store.String("Hello"), "de": store.String("Hallo")}),
			"seo": store.Object(store.Values{
				"headline": store.Object(store.Values{"en": store.String("Home")}),
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := bson.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeCollectionDocument(bson.Raw(raw), collection); err != nil {
		t.Fatalf("sparse-compatible storage decode rejected a syntactically valid locale: %v", err)
	}
	if _, err := decodeCollectionDocumentForLocales(bson.Raw(raw), collection, nil); err != nil {
		t.Fatalf("request without configured locale membership rejected sparse storage: %v", err)
	}
	if _, err := decodeCollectionDocumentForLocales(bson.Raw(raw), collection, []schema.LocaleCode{"en", "fr"}); err == nil || !strings.Contains(err.Error(), `unconfigured locale "de"`) {
		t.Fatalf("request-aware locale decode error = %v", err)
	}
}

func mongoLocalizedScalarCollection() schema.Collection {
	collection := mongoGroupCollection()
	collection.Fields[0].Localized = true
	collection.Fields[1].Nested.ResolvedFields()[0].Localized = true
	return collection
}
