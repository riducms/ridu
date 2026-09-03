package mongodb

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoPortablePointShapesValidateSchemaAndProjection(t *testing.T) {
	collection := mongoPortableShapeCollection(t)
	locales := []schema.LocaleCode{"en", "fr"}
	if err := validateCollectionEnvelope(collection); err != nil {
		t.Fatalf("portable point envelope rejected: %v", err)
	}

	valid := store.Values{
		"location": store.List(store.Number(-0.1276), store.Number(51.5072)),
		"details": store.Object(store.Values{
			"focus": store.List(store.Number(151.2093), store.Number(-33.8688)),
		}),
		"localizedLocation": store.Object(store.Values{
			"en": store.List(store.Number(-3.1883), store.Number(55.9533)),
			"fr": store.Null(),
		}),
	}
	if err := validateCompleteValuesForLocales(collection, valid, locales); err != nil {
		t.Fatalf("portable point values rejected: %v", err)
	}

	pointSchema := bson.D{
		{Key: "bsonType", Value: bson.A{"array", "null"}},
		{Key: "items", Value: bson.A{
			bson.D{{Key: "bsonType", Value: "double"}, {Key: "minimum", Value: -180.0}, {Key: "maximum", Value: 180.0}},
			bson.D{{Key: "bsonType", Value: "double"}, {Key: "minimum", Value: -90.0}, {Key: "maximum", Value: 90.0}},
		}},
		{Key: "minItems", Value: 2},
		{Key: "maxItems", Value: 2},
	}
	for _, path := range [][]string{{"location"}, {"details", "focus"}} {
		resolved := mongoPortableShapeField(t, collection.Fields, path...)
		if got := mongoCollectionFieldJSONSchema(*resolved, locales); !reflect.DeepEqual(got, pointSchema) {
			t.Fatalf("point schema for %q = %#v, want %#v", strings.Join(path, "."), got, pointSchema)
		}
	}
	localizedPoint := mongoPortableShapeField(t, collection.Fields, "localizedLocation")
	wantLocalizedSchema := bson.D{
		{Key: "bsonType", Value: "object"},
		{Key: "additionalProperties", Value: false},
		{Key: "properties", Value: bson.D{
			{Key: "en", Value: pointSchema},
			{Key: "fr", Value: pointSchema},
		}},
	}
	if got := mongoCollectionFieldJSONSchema(*localizedPoint, locales); !reflect.DeepEqual(got, wantLocalizedSchema) {
		t.Fatalf("localized point schema = %#v, want %#v", got, wantLocalizedSchema)
	}

	paths := []query.Path{
		mongoPortableShapePath(t, "location"),
		mongoPortableShapePath(t, "details.focus"),
		mongoPortableShapePath(t, "localizedLocation"),
	}
	projection, err := requestProjection(store.Request{Collection: collection, Select: paths})
	if err != nil {
		t.Fatalf("portable point projection: %v", err)
	}
	for _, path := range []string{"values.location", "values.details.focus", "values.localizedLocation"} {
		if got := mongoPortableShapeDirectValue(projection, path); got != int32(1) {
			t.Errorf("point projection %q = %#v, want inclusion", path, got)
		}
	}
	if got := mongoPortableShapeDirectValue(projection, "values.localizedLocation.en"); got != nil {
		t.Fatalf("localized point projection selected one locale instead of the canonical map: %#v", projection)
	}

	locationPath := mongoPortableShapePath(t, "location")
	for _, role := range []string{"filter", "access", "sort", "distinct", "index"} {
		if _, err := resolveMongoPredicatePath(collection, locationPath, role, mongoPredicateScope{}); err == nil ||
			!strings.Contains(err.Error(), `unsupported non-scalar field type "point"`) {
			t.Fatalf("Point %s boundary error = %v", role, err)
		}
	}

	tests := []struct {
		name   string
		mutate func(store.Values)
		want   string
	}{
		{
			name: "root point is not a tuple",
			mutate: func(values store.Values) {
				values["location"] = store.String("51.5072,-0.1276")
			},
			want: `value "location" does not match field type "point"`,
		},
		{
			name: "nested point has wrong arity",
			mutate: func(values store.Values) {
				details, _ := values["details"].ObjectValue()
				details["focus"] = store.List(store.Number(151.2093))
				values["details"] = store.Object(details)
			},
			want: `value "details.focus" does not match field type "point"`,
		},
		{
			name: "localized point requires locale map",
			mutate: func(values store.Values) {
				values["localizedLocation"] = store.List(store.Number(2.3522), store.Number(48.8566))
			},
			want: `localized field "localizedLocation" requires canonical locale-keyed storage`,
		},
		{
			name: "localized point longitude is bounded",
			mutate: func(values store.Values) {
				values["localizedLocation"] = store.Object(store.Values{
					"en": store.List(store.Number(180.0001), store.Number(51.5072)),
				})
			},
			want: `value "localizedLocation.en" does not match field type "point"`,
		},
		{
			name: "localized point latitude is bounded",
			mutate: func(values store.Values) {
				values["localizedLocation"] = store.Object(store.Values{
					"fr": store.List(store.Number(2.3522), store.Number(-90.0001)),
				})
			},
			want: `value "localizedLocation.fr" does not match field type "point"`,
		},
		{
			name: "localized point rejects unconfigured locale",
			mutate: func(values store.Values) {
				values["localizedLocation"] = store.Object(store.Values{
					"es": store.List(store.Number(-3.7038), store.Number(40.4168)),
				})
			},
			want: `localized field "localizedLocation" contains unconfigured locale "es"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := store.CloneValues(valid)
			test.mutate(values)
			if err := validateCompleteValuesForLocales(collection, values, locales); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("point validation error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMongoPortableLocalizedRepeatedNestedReferencesAdmitCanonicalValues(t *testing.T) {
	collection := mongoPortableShapeCollection(t)
	locales := []schema.LocaleCode{"en", "fr"}
	if err := validateCollectionEnvelope(collection); err != nil {
		t.Fatalf("portable reference envelope rejected: %v", err)
	}
	if _, err := mongoCollectionEnvelopePredicate(collection, locales); err != nil {
		t.Fatalf("portable reference storage schema rejected: %v", err)
	}

	valid := mongoPortableReferenceValues()
	if err := validateCompleteValuesForLocales(collection, valid, locales); err != nil {
		t.Fatalf("portable reference values rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(store.Values)
		want   string
	}{
		{
			name: "localized relationship requires locale map",
			mutate: func(values store.Values) {
				values["localizedOwner"] = store.String("person-a")
			},
			want: `localized field "localizedOwner" requires canonical locale-keyed storage`,
		},
		{
			name: "localized polymorphic member requires configured target",
			mutate: func(values store.Values) {
				values["localizedSubjects"] = store.Object(store.Values{
					"en": store.List(mongoPortablePolymorphicReference("media", "asset-a")),
				})
			},
			want: `polymorphic relationship value "localizedSubjects.en.0" has an invalid target`,
		},
		{
			name: "localized upload members are document IDs",
			mutate: func(values store.Values) {
				values["localizedGallery"] = store.Object(store.Values{
					"fr": store.List(store.Number(42)),
				})
			},
			want: `upload value "localizedGallery.fr.0" must be a document ID string`,
		},
		{
			name: "relationship inside nested array keeps has-many shape",
			mutate: func(values store.Values) {
				content, _ := values["content"].ObjectValue()
				rows, _ := content["rows"].Values()
				row, _ := rows[0].ObjectValue()
				row["reviewers"] = mongoPortablePolymorphicReference("people", "person-a")
				rows[0] = store.Object(row)
				content["rows"] = store.List(rows...)
				values["content"] = store.Object(content)
			},
			want: `relationship value "content.rows.0.reviewers" must be a list`,
		},
		{
			name: "localized relationship inside repeated row keeps locale shape",
			mutate: func(values store.Values) {
				content, _ := values["content"].ObjectValue()
				rows, _ := content["rows"].Values()
				row, _ := rows[0].ObjectValue()
				row["localizedReviewer"] = store.Object(store.Values{"en": store.Number(1)})
				rows[0] = store.Object(row)
				content["rows"] = store.List(rows...)
				values["content"] = store.Object(content)
			},
			want: `relationship value "content.rows.0.localizedReviewer.en" must be a document ID string`,
		},
		{
			name: "relationship inside doubly nested array stays exact",
			mutate: func(values store.Values) {
				content, _ := values["content"].ObjectValue()
				rows, _ := content["rows"].Values()
				row, _ := rows[0].ObjectValue()
				children, _ := row["children"].Values()
				child, _ := children[0].ObjectValue()
				child["subject"] = store.Object(store.Values{
					"relationTo": store.String("teams"),
					"id":         store.String("team-a"),
					"extra":      store.String("not-portable"),
				})
				children[0] = store.Object(child)
				row["children"] = store.List(children...)
				rows[0] = store.Object(row)
				content["rows"] = store.List(rows...)
				values["content"] = store.Object(content)
			},
			want: `polymorphic relationship value "content.rows.0.children.0.subject" must contain only relationTo and id`,
		},
		{
			name: "upload inside block keeps has-many shape",
			mutate: func(values store.Values) {
				layout, _ := values["layout"].Values()
				block, _ := layout[0].ObjectValue()
				block["assets"] = store.String("asset-a")
				layout[0] = store.Object(block)
				values["layout"] = store.List(layout...)
			},
			want: `upload value "layout.0.assets" must be a list`,
		},
		{
			name: "localized blocks reject unconfigured locale",
			mutate: func(values store.Values) {
				localized, _ := values["localizedLayout"].ObjectValue()
				localized["es"] = localized["en"]
				values["localizedLayout"] = store.Object(localized)
			},
			want: `localized field "localizedLayout" contains unconfigured locale "es"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := store.CloneValues(valid)
			test.mutate(values)
			if err := validateCompleteValuesForLocales(collection, values, locales); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("portable reference validation error = %v, want containing %q", err, test.want)
			}
		})
	}

	envelopeTests := []struct {
		name   string
		mutate func(*schema.Collection)
		want   string
	}{
		{
			name: "nested relationship still requires contract",
			mutate: func(candidate *schema.Collection) {
				mongoPortableShapeField(t, candidate.Fields, "content", "rows", "reviewers").Relationship = nil
			},
			want: `relationship field "content.rows.reviewers" does not have a relationship contract`,
		},
		{
			name: "block upload still requires contract",
			mutate: func(candidate *schema.Collection) {
				mongoPortableShapeField(t, candidate.Fields, "layout", "feature", "assets").Upload = nil
			},
			want: `upload field "layout.feature.assets" does not have an upload contract`,
		},
		{
			name: "repeated descendants remain non-indexable",
			mutate: func(candidate *schema.Collection) {
				mongoPortableShapeField(t, candidate.Fields, "content", "rows", "localizedReviewer").Index = true
			},
			want: `does not support indexes on field "content.rows.localizedReviewer" inside a repeated field`,
		},
	}
	for _, test := range envelopeTests {
		t.Run(test.name, func(t *testing.T) {
			candidate := mongoPortableShapeCollection(t)
			test.mutate(&candidate)
			if err := validateCollectionEnvelope(candidate); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("portable reference envelope error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func mongoPortableShapeCollection(t *testing.T) schema.Collection {
	t.Helper()
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "MongoDB portable document shapes",
		Localization: ridu.LocalizationConfig{
			DefaultLocale: "en",
			Locales: []ridu.Locale{
				{Code: "en", Label: "English"},
				{Code: "fr", Label: "French"},
			},
		},
		Collections: []ridu.Collection{
			{Slug: "people", Fields: []field.Definition{field.Text("name")}},
			{Slug: "teams", Fields: []field.Definition{field.Text("name")}},
			{
				Slug: "media", Upload: true,
				UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"image/png"}},
				Fields:       []field.Definition{field.Text("alt")},
			},
			{
				Slug: "entries",
				Fields: []field.Definition{
					field.Point("location"),
					field.Group("details", field.Fields(field.Point("focus"))),
					field.Point("localizedLocation", field.Localized()),
					field.Relationship("localizedOwner", field.To("people"), field.Localized()),
					field.Relationship("localizedSubjects", field.ToAny("people", "teams"), field.HasMany(), field.Localized()),
					field.Upload("localizedGallery", field.ToMany("media"), field.Localized()),
					field.Group("content", field.Fields(
						field.Array("rows", field.Fields(
							field.Relationship("reviewers", field.ToAny("people", "teams"), field.HasMany()),
							field.Upload("assets", field.ToMany("media")),
							field.Relationship("localizedReviewer", field.To("people"), field.Localized()),
							field.Array("children", field.Fields(
								field.Relationship("subject", field.ToAny("people", "teams")),
								field.Upload("asset", field.To("media")),
							)),
						)),
					)),
					field.Blocks("layout", field.BlockTypes(
						field.BlockType("feature", "Feature",
							field.Relationship("subjects", field.ToAny("people", "teams"), field.HasMany()),
							field.Upload("assets", field.ToMany("media")),
						),
					)),
					field.Blocks("localizedLayout", field.Localized(), field.BlockTypes(
						field.BlockType("image", "Image",
							field.Relationship("owner", field.To("people")),
							field.Upload("asset", field.To("media")),
						),
					)),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("resolve portable MongoDB shape fixture: %v", err)
	}
	for _, collection := range manifest.Snapshot().Collections {
		if collection.Slug == "entries" {
			return collection
		}
	}
	t.Fatal("portable MongoDB shape fixture did not resolve entries collection")
	return schema.Collection{}
}

func mongoPortableReferenceValues() store.Values {
	return store.Values{
		"localizedOwner": store.Object(store.Values{
			"en": store.String("person-a"),
			"fr": store.Null(),
		}),
		"localizedSubjects": store.Object(store.Values{
			"en": store.List(
				mongoPortablePolymorphicReference("people", "person-a"),
				mongoPortablePolymorphicReference("teams", "team-a"),
			),
			"fr": store.List(),
		}),
		"localizedGallery": store.Object(store.Values{
			"en": store.List(store.String("asset-a"), store.String("asset-b")),
			"fr": store.Null(),
		}),
		"content": store.Object(store.Values{
			"rows": store.List(store.Object(store.Values{
				"_key": store.String("row-a"),
				"reviewers": store.List(
					mongoPortablePolymorphicReference("people", "person-a"),
					mongoPortablePolymorphicReference("teams", "team-a"),
				),
				"assets": store.List(store.String("asset-a"), store.String("asset-b")),
				"localizedReviewer": store.Object(store.Values{
					"en": store.String("person-a"),
					"fr": store.Null(),
				}),
				"children": store.List(store.Object(store.Values{
					"_key":    store.String("child-a"),
					"subject": mongoPortablePolymorphicReference("teams", "team-a"),
					"asset":   store.String("asset-a"),
				})),
			})),
		}),
		"layout": store.List(store.Object(store.Values{
			"_key":      store.String("feature-a"),
			"blockType": store.String("feature"),
			"subjects": store.List(
				mongoPortablePolymorphicReference("people", "person-a"),
				mongoPortablePolymorphicReference("teams", "team-a"),
			),
			"assets": store.List(store.String("asset-a"), store.String("asset-b")),
		})),
		"localizedLayout": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{
				"_key":      store.String("image-a"),
				"blockType": store.String("image"),
				"owner":     store.String("person-a"),
				"asset":     store.String("asset-a"),
			})),
			"fr": store.List(),
		}),
	}
}

func mongoPortablePolymorphicReference(collection, id string) store.Value {
	return store.Object(store.Values{
		"relationTo": store.String(collection),
		"id":         store.String(id),
	})
}

func mongoPortableShapePath(t *testing.T, value string) query.Path {
	t.Helper()
	path, err := query.ParsePath(value)
	if err != nil {
		t.Fatalf("parse portable shape path %q: %v", value, err)
	}
	return path
}

func mongoPortableShapeField(t *testing.T, fields []schema.Field, segments ...string) *schema.Field {
	t.Helper()
	if len(segments) == 0 {
		t.Fatal("portable shape field path is empty")
	}
	current := fields
	for index := 0; index < len(segments); index++ {
		var resolved *schema.Field
		for fieldIndex := range current {
			if current[fieldIndex].Name == segments[index] {
				resolved = &current[fieldIndex]
				break
			}
		}
		if resolved == nil {
			t.Fatalf("portable shape field %q was not resolved", strings.Join(segments, "."))
		}
		if index == len(segments)-1 {
			return resolved
		}
		switch resolved.Type {
		case schema.FieldTypeGroup, schema.FieldTypeArray:
			if resolved.Nested == nil {
				t.Fatalf("portable shape container %q lacks nested metadata", resolved.Name)
			}
			current = resolved.Nested.Fields
		case schema.FieldTypeBlocks:
			index++
			if index >= len(segments) || resolved.Blocks == nil {
				t.Fatalf("portable blocks field %q lacks a block path", resolved.Name)
			}
			var found bool
			for blockIndex := range resolved.Blocks.Types {
				if resolved.Blocks.Types[blockIndex].Key == segments[index] {
					current = resolved.Blocks.Types[blockIndex].Fields
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("portable blocks field %q lacks block %q", resolved.Name, segments[index])
			}
		default:
			t.Fatalf("portable shape path %q continues through non-container field %q", strings.Join(segments, "."), resolved.Name)
		}
	}
	t.Fatalf("portable shape field %q was not resolved", strings.Join(segments, "."))
	return nil
}

func mongoPortableShapeDirectValue(document bson.D, key string) any {
	for _, element := range document {
		if element.Key == key {
			return element.Value
		}
	}
	return nil
}
