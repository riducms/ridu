package mongodb

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestMongoDBReferenceShapeLifecycle exercises one representative of every
// authored container and reference shape through the public operation engine.
// The focused adapter tests cover the low-level mechanics; this test is the
// live MongoDB proof that those mechanics compose across localization,
// population, versioning, trash, and hard-delete reconciliation.
func TestMongoDBReferenceShapeLifecycle(t *testing.T) {
	backend := mongoIntegrationStore(t)
	files, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	public := mongoMustPath(t, "public")
	application, err := ridu.New(ridu.Config{
		Name:             "MongoDB reference shape lifecycle",
		Storage:          files,
		StorageNamespace: "mongodb-reference-shapes",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"},
			{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{
			{
				Slug:   "direct-records",
				Fields: field.Fields{field.Group("localizedMeta", field.Fields{field.Text("headline"), field.Text("summary")}).Localized()},
			},
			{
				Slug: "people",
				Fields: field.Fields{field.Text("name").Required().Localized(), field.Checkbox("public").Required(), field.Text("secret").Access(field.Access{Read: func(operation.AccessContext,

				) (bool, error) {
					return false, nil
				}})},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					return ridu.Where(query.Equal(public, query.Boolean(true))), nil
				}},
			},
			{Slug: "teams", Fields: field.Fields{field.Text("name").Required()}},
			{
				Slug: "media", Upload: true,
				UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
				Fields:       field.Fields{field.Text("label").Required()},
			},
			{
				Slug: "records", Trash: true, Versions: true,
				VersionConfig: ridu.VersionConfig{MaxPerDocument: 20},
				Fields:        field.Fields{field.Text("title").Required(), field.Point("location"), field.Point("localLocation").Localized(), field.Group("localizedMeta", field.Fields{field.Text("headline"), field.Text("summary")}).Localized(), field.Relationship("guard", "people").Localized().OnDelete(field.ReferenceDeleteRestrict), field.Upload("assetGuard", "media").Localized().OnDelete(field.ReferenceDeleteRestrict), field.Relationship("editor", "people").Localized().OnDelete(field.ReferenceDeleteNullify), field.Relationships("contributors", "people").Localized().OnDelete(field.ReferenceDeleteNullify), field.PolymorphicRelationships("subjects", "people", "teams").Localized().OnDelete(field.ReferenceDeleteNullify), field.Uploads("assets", "media").Localized().OnDelete(field.ReferenceDeleteNullify), field.Array("sections", field.Fields{field.Point("waypoint"), field.Relationships("reviewers", "people").OnDelete(field.ReferenceDeleteNullify), field.PolymorphicRelationship("localSubject", "people", "teams").Localized().OnDelete(field.ReferenceDeleteNullify), field.Uploads("assets", "media").Localized().OnDelete(field.ReferenceDeleteNullify)}), field.Blocks("layout", field.Block{Slug: "quote", Fields: field.Fields{field.Relationship("reviewer", "people").OnDelete(field.ReferenceDeleteNullify), field.PolymorphicRelationships("subjects", "people", "teams").OnDelete(field.ReferenceDeleteNullify), field.Upload("asset", "media").Localized().OnDelete(field.ReferenceDeleteNullify)}}), field.Array("localizedSections", field.Fields{field.Relationship("reviewer", "people").OnDelete(field.ReferenceDeleteNullify), field.Uploads("assets", "media").OnDelete(field.ReferenceDeleteNullify)}).Localized(), field.Blocks("localizedLayout", field.Block{Slug: "quote", Fields: field.Fields{field.PolymorphicRelationships("subjects", "people", "teams").OnDelete(field.ReferenceDeleteNullify), field.Upload("asset", "media").OnDelete(field.ReferenceDeleteNullify)}}).Localized()},
			},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	directCollection := mongoCollectionsBySlug(application.Manifest().Snapshot().Collections)["direct-records"]
	directLocales := []schema.LocaleCode{"en", "fr"}
	directTransaction := mongoBegin(t, backend, false)
	directDocument, err := directTransaction.Create(t.Context(), store.CreateRequest{
		Collection: directCollection, ID: "direct-localized-group", Locales: directLocales,
		Values: store.Values{"localizedMeta": store.Object(store.Values{
			"en": store.Object(store.Values{"headline": store.String("Old"), "summary": store.String("Keep")}),
		})},
	})
	if err != nil {
		mongoRollback(t, directTransaction)
		t.Fatal(err)
	}
	mongoCommit(t, directTransaction)
	directTransaction = mongoBegin(t, backend, false)
	directDocument, err = directTransaction.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: directCollection, ID: directDocument.ID, Locales: directLocales},
		Values: store.Values{"localizedMeta": store.Object(store.Values{
			"en": store.Object(store.Values{"headline": store.String("New")}),
		})},
	})
	if err != nil {
		mongoRollback(t, directTransaction)
		t.Fatal(err)
	}
	mongoCommit(t, directTransaction)
	directLocalesValue := mongoReferenceLifecycleObject(t, directDocument.Values["localizedMeta"], "direct localized group")
	directEnglish := mongoReferenceLifecycleObject(t, directLocalesValue["en"], "direct English localized group")
	if mongoReferenceLifecycleString(directEnglish["headline"]) != "New" || mongoReferenceLifecycleString(directEnglish["summary"]) != "Keep" {
		t.Fatalf("direct Mongo localized group patch lost a sibling: %#v", directEnglish)
	}

	personA := mongoReferenceLifecyclePerson(t, application, "Ada", "Adèle", true, "redact-a")
	personB := mongoReferenceLifecyclePerson(t, application, "Bea", "Béatrice", true, "redact-b")
	personC := mongoReferenceLifecyclePerson(t, application, "Cyd", "Cécile", true, "redact-c")
	team, err := application.Local().Create(t.Context(), "teams", store.Values{"name": store.String("Core")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mediaA := mongoReferenceLifecycleUpload(t, application, "a.txt", "Asset A")
	mediaB := mongoReferenceLifecycleUpload(t, application, "b.txt", "Asset B")

	base := mongoReferenceLifecycleEnglishValues(personA.ID, personC.ID, team.ID, mediaA.ID, mediaB.ID)
	primary, err := application.Local().Create(t.Context(), "records", base, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstRevision := primary.Revision
	if firstRevision != 1 {
		t.Fatalf("create revision = %d, want 1", firstRevision)
	}
	pointProjection, err := application.Local().FindWithOptions(t.Context(), "records", primary.ID, ridu.FindOptions{Select: []query.Path{
		mongoMustPath(t, "location"), mongoMustPath(t, "localLocation"), mongoMustPath(t, "sections"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	mongoReferenceLifecycleAssertPoint(t, pointProjection.Values["location"], -0.1, 51.5, "created root point")
	mongoReferenceLifecycleAssertPoint(t, pointProjection.Values["localLocation"], -0.2, 51.6, "created localized point")
	createdSections := mongoReferenceLifecycleList(t, pointProjection.Values["sections"], "created point sections")
	createdSection := mongoReferenceLifecycleObject(t, createdSections[0], "created point section")
	mongoReferenceLifecycleAssertPoint(t, createdSection["waypoint"], -0.3, 51.7, "created nested point")

	// Import uses the same admission and derived-state path as ordinary create.
	imported, err := application.Local().Import(t.Context(), "records", base, ridu.ImportOptions{
		ID: "mongo-shape-import", Status: store.StatusPublished,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(t.Context(), "records", imported.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().DeletePermanent(t.Context(), "records", imported.ID, nil); err != nil {
		t.Fatal(err)
	}
	mongoReferenceLifecycleAssertOwnerStateClean(t, backend, application, imported.ID)

	primary, err = application.Local().PublishChanges(t.Context(), "records", primary.ID,
		mongoReferenceLifecycleFrenchValues(personA.ID, personB.ID, personC.ID, team.ID, mediaB.ID), primary.Revision, nil,
		ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if primary, err = application.Local().Restore(t.Context(), "records", primary.ID, firstRevision, primary.Revision, nil,
		ridu.LocaleOptions{Locale: "en"}); err != nil {
		t.Fatal(err)
	}
	if restored := mongoReferenceLifecycleString(primary.Values["editor"]); restored != personA.ID {
		t.Fatalf("successful restore editor = %q, want %q", restored, personA.ID)
	}
	mongoReferenceLifecycleAssertPoint(t, primary.Values["location"], -0.1, 51.5, "restored root point")
	mongoReferenceLifecycleAssertPoint(t, primary.Values["localLocation"], -0.2, 51.6, "restored localized point")
	restoredSections := mongoReferenceLifecycleList(t, primary.Values["sections"], "restored point sections")
	restoredSection := mongoReferenceLifecycleObject(t, restoredSections[0], "restored point section")
	mongoReferenceLifecycleAssertPoint(t, restoredSection["waypoint"], -0.3, 51.7, "restored nested point")
	primary, err = application.Local().PublishChanges(t.Context(), "records", primary.ID,
		mongoReferenceLifecycleFrenchValues(personA.ID, personB.ID, personC.ID, team.ID, mediaB.ID), primary.Revision, nil,
		ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err = application.Local().PublishChanges(t.Context(), "records", primary.ID, store.Values{
		"localizedMeta": store.Object(store.Values{"headline": store.String("French revised")}),
		"localizedSections": store.List(store.Object(store.Values{
			"_key": store.String("localized-section-fr"), "reviewer": store.String(personB.ID),
		})),
		"localizedLayout": store.List(store.Object(store.Values{
			"_key": store.String("localized-quote-fr"), "blockType": store.String("quote"),
		})),
	}, primary.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	updatedPoints, err := application.Local().FindWithOptions(t.Context(), "records", primary.ID, ridu.FindOptions{
		Locale: "fr", Select: []query.Path{
			mongoMustPath(t, "location"), mongoMustPath(t, "localLocation"), mongoMustPath(t, "localizedMeta"),
			mongoMustPath(t, "sections"), mongoMustPath(t, "localizedSections"), mongoMustPath(t, "localizedLayout"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mongoReferenceLifecycleAssertPoint(t, updatedPoints.Values["location"], 1.1, 2.2, "updated root point")
	mongoReferenceLifecycleAssertPoint(t, updatedPoints.Values["localLocation"], 2.35, 48.85, "updated localized point")
	updatedSections := mongoReferenceLifecycleList(t, updatedPoints.Values["sections"], "updated point sections")
	updatedSection := mongoReferenceLifecycleObject(t, updatedSections[0], "updated point section")
	mongoReferenceLifecycleAssertPoint(t, updatedSection["waypoint"], 3.3, 4.4, "updated nested point")
	updatedMeta := mongoReferenceLifecycleObject(t, updatedPoints.Values["localizedMeta"], "partially updated localized group")
	if mongoReferenceLifecycleString(updatedMeta["headline"]) != "French revised" ||
		mongoReferenceLifecycleString(updatedMeta["summary"]) != "Résumé français" {
		t.Fatalf("localized group partial update lost a sibling: %#v", updatedMeta)
	}
	partialSections := mongoReferenceLifecycleList(t, updatedPoints.Values["localizedSections"], "partially updated localized sections")
	partialSection := mongoReferenceLifecycleObject(t, partialSections[0], "partially updated localized section")
	if assets := mongoReferenceLifecycleListStrings(t, partialSection["assets"], "preserved localized section assets"); len(assets) != 1 || assets[0] != mediaB.ID {
		t.Fatalf("localized array partial update lost a sibling: %#v", partialSection)
	}
	partialLayout := mongoReferenceLifecycleList(t, updatedPoints.Values["localizedLayout"], "partially updated localized layout")
	partialBlock := mongoReferenceLifecycleObject(t, partialLayout[0], "partially updated localized block")
	if mongoReferenceLifecycleString(partialBlock["asset"]) != mediaB.ID {
		t.Fatalf("localized block partial update lost a sibling: %#v", partialBlock)
	}

	duplicate, err := application.Local().Duplicate(t.Context(), "records", primary.ID,
		store.Values{"title": store.String("Duplicate")}, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	duplicateAll, err := application.Local().Find(t.Context(), "records", duplicate.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	duplicateEditors := mongoReferenceLifecycleObject(t, duplicateAll.Values["editor"], "duplicate editor locales")
	if mongoReferenceLifecycleString(duplicateEditors["en"]) != personA.ID || mongoReferenceLifecycleString(duplicateEditors["fr"]) != personB.ID {
		t.Fatalf("duplicate localized editors = %#v", duplicateEditors)
	}

	// Missing targets are rejected even when nested in a repeated relationship
	// or upload shape; neither candidate may leave a physical document behind.
	badRelationship := store.CloneValues(base)
	badRelationship["sections"] = store.List(store.Object(store.Values{
		"_key": store.String("bad-section"), "reviewers": store.List(store.String("missing-person")),
	}))
	if _, err := application.Local().Create(t.Context(), "records", badRelationship, nil); !mongoReferenceLifecycleIssue(err, "invalid_relationship", "sections.0.reviewers.0") {
		t.Fatalf("nested relationship admission error = %v", err)
	}
	badUpload := store.CloneValues(base)
	badUpload["layout"] = store.List(store.Object(store.Values{
		"_key": store.String("bad-block"), "blockType": store.String("quote"), "asset": store.String("missing-upload"),
	}))
	if _, err := application.Local().Create(t.Context(), "records", badUpload, nil); !mongoReferenceLifecycleIssue(err, "invalid_upload", "layout.0.asset.en") {
		t.Fatalf("nested upload admission error = %v; issues = %#v", err, mongoReferenceLifecycleIssues(err))
	}

	if _, err := application.Local().Update(t.Context(), "people", personC.ID, store.Values{"public": store.Boolean(false)}, nil); err != nil {
		t.Fatal(err)
	}
	populated, err := application.Local().FindWithOptions(t.Context(), "records", primary.ID, ridu.FindOptions{
		Locale: "fr", Populate: mongoReferenceLifecyclePopulations(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	mongoReferenceLifecycleAssertPopulation(t, populated, personA.ID, personB.ID, personC.ID, team.ID, mediaA.ID, mediaB.ID)
	allPopulated, err := application.Local().FindWithOptions(t.Context(), "records", primary.ID, ridu.FindOptions{
		AllLocales: true, Populate: []query.Population{{Path: mongoMustPath(t, "editor")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	allEditors := mongoReferenceLifecycleObject(t, allPopulated.Values["editor"], "all-locale editors")
	mongoReferenceLifecycleAssertDocument(t, allEditors["en"], personA.ID, true)
	mongoReferenceLifecycleAssertDocument(t, allEditors["fr"], personB.ID, true)

	// Restrict is planned before any nullification, so a blocked target delete
	// cannot partially rewrite either active owner.
	if _, err := application.Local().Delete(t.Context(), "people", personA.ID, nil); !mongoOperationCode(err, "delete_restricted") {
		t.Fatalf("relationship restrict delete = %v", err)
	}
	if _, err := application.Local().Delete(t.Context(), "media", mediaA.ID, nil); !mongoOperationCode(err, "delete_restricted") {
		t.Fatalf("upload restrict delete = %v", err)
	}
	unchanged, err := application.Local().Find(t.Context(), "records", primary.ID, nil, ridu.LocaleOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if mongoReferenceLifecycleString(unchanged.Values["editor"]) != personA.ID || mongoReferenceLifecycleListStrings(t, unchanged.Values["assets"], "unchanged assets")[0] != mediaA.ID {
		t.Fatalf("restrict planning partially nullified current data: %#v", unchanged.Values)
	}

	if _, err := application.Local().Update(t.Context(), "people", personC.ID, store.Values{"public": store.Boolean(true)}, nil); err != nil {
		t.Fatal(err)
	}
	for ownerID, revision := range map[string]int{primary.ID: primary.Revision, duplicate.ID: duplicate.Revision} {
		updatedOwner, err := application.Local().PublishChanges(t.Context(), "records", ownerID,
			store.Values{"guard": store.Null(), "assetGuard": store.Null()}, revision, nil, ridu.LocaleOptions{Locale: "en"})
		if err != nil {
			t.Fatalf("clear restrict fields for %s: %v", ownerID, err)
		}
		if ownerID == primary.ID {
			primary = updatedOwner
		} else {
			duplicate = updatedOwner
		}
	}
	if _, err := application.Local().Delete(t.Context(), "records", duplicate.ID, nil); err != nil {
		t.Fatal(err)
	}
	primaryVersionsBefore, err := application.Local().Versions(t.Context(), "records", primary.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	duplicateVersionsBefore, err := application.Local().Versions(t.Context(), "records", duplicate.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(t.Context(), "people", personA.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(t.Context(), "media", mediaA.ID, nil); err != nil {
		t.Fatal(err)
	}

	activeAfter, err := application.Local().Find(t.Context(), "records", primary.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	trashAfter, err := application.Local().FindWithOptions(t.Context(), "records", duplicate.ID, ridu.FindOptions{TrashOnly: true, AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	for label, document := range map[string]store.Document{"active": activeAfter, "trash": trashAfter} {
		if mongoReferenceLifecycleContainsString(document.Values, personA.ID) || mongoReferenceLifecycleContainsString(document.Values, mediaA.ID) {
			t.Fatalf("%s current state retained a deleted target: %#v", label, document.Values)
		}
		if !mongoReferenceLifecycleContainsString(document.Values, personB.ID) || !mongoReferenceLifecycleContainsString(document.Values, personC.ID) ||
			!mongoReferenceLifecycleContainsString(document.Values, team.ID) || !mongoReferenceLifecycleContainsString(document.Values, mediaB.ID) {
			t.Fatalf("%s nullification removed an unrelated target: %#v", label, document.Values)
		}
		mongoReferenceLifecycleAssertNestedPolymorphicNullification(t, document, personB.ID, team.ID)
	}
	primaryVersionsAfter, err := application.Local().Versions(t.Context(), "records", primary.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	duplicateVersionsAfter, err := application.Local().Versions(t.Context(), "records", duplicate.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(primaryVersionsBefore, primaryVersionsAfter) || !reflect.DeepEqual(duplicateVersionsBefore, duplicateVersionsAfter) {
		t.Fatal("hard-delete reconciliation rewrote an immutable version snapshot")
	}
	if !mongoReferenceLifecycleVersionsContain(primaryVersionsAfter, personA.ID) || !mongoReferenceLifecycleVersionsContain(primaryVersionsAfter, mediaA.ID) {
		t.Fatal("retained versions no longer contain their historical target IDs")
	}
	if len(primaryVersionsAfter) == 0 {
		t.Fatal("version history did not retain the create snapshot")
	}
	createVersion := mongoReferenceLifecycleVersion(t, primaryVersionsAfter, firstRevision)
	mongoReferenceLifecycleAssertPoint(t, createVersion.Snapshot.Values["location"], -0.1, 51.5, "versioned root point")
	versionedLocalizedPoints := mongoReferenceLifecycleObject(t, createVersion.Snapshot.Values["localLocation"], "versioned localized point locales")
	mongoReferenceLifecycleAssertPoint(t, versionedLocalizedPoints["en"], -0.2, 51.6, "versioned localized point")
	versionSections := mongoReferenceLifecycleList(t, createVersion.Snapshot.Values["sections"], "versioned point sections")
	versionSection := mongoReferenceLifecycleObject(t, versionSections[0], "versioned point section")
	mongoReferenceLifecycleAssertPoint(t, versionSection["waypoint"], -0.3, 51.7, "versioned nested point")

	currentRevision := activeAfter.Revision
	if _, err := application.Local().Restore(t.Context(), "records", primary.ID, firstRevision, currentRevision, nil,
		ridu.LocaleOptions{Locale: "en"}); !mongoReferenceLifecycleAnyIssue(err, "invalid_relationship", "invalid_upload") {
		t.Fatalf("restore with deleted historical targets = %v", err)
	}
	stillCurrent, err := application.Local().Find(t.Context(), "records", primary.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if stillCurrent.Revision != currentRevision || !reflect.DeepEqual(stillCurrent.Values, activeAfter.Values) {
		t.Fatalf("rejected restore changed current document: before %#v after %#v", activeAfter, stillCurrent)
	}

	// Nullified trash state remains restorable. Permanent deletion removes both
	// derived references and versions, permitting a clean import with the ID.
	restoredTrash, err := application.Local().RestoreDeleted(t.Context(), "records", duplicate.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if mongoReferenceLifecycleContainsString(restoredTrash.Values, personA.ID) || mongoReferenceLifecycleContainsString(restoredTrash.Values, mediaA.ID) {
		t.Fatalf("trash restore resurrected deleted targets: %#v", restoredTrash.Values)
	}
	if _, err := application.Local().Delete(t.Context(), "records", duplicate.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().DeletePermanent(t.Context(), "records", duplicate.ID, nil); err != nil {
		t.Fatal(err)
	}
	mongoReferenceLifecycleAssertOwnerStateClean(t, backend, application, duplicate.ID)
	reimported, err := application.Local().Import(t.Context(), "records", store.Values{"title": store.String("Reimported")},
		ridu.ImportOptions{ID: duplicate.ID, Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reimported.Revision != 1 {
		t.Fatalf("same-ID reimport revision = %d, want 1", reimported.Revision)
	}
}

func mongoReferenceLifecyclePerson(t *testing.T, application *ridu.App, english, french string, public bool, secret string) store.Document {
	t.Helper()
	person, err := application.Local().Create(t.Context(), "people", store.Values{
		"name": store.String(english), "public": store.Boolean(public), "secret": store.String(secret),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	person, err = application.Local().Update(t.Context(), "people", person.ID, store.Values{"name": store.String(french)}, nil,
		ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	return person
}

func mongoReferenceLifecycleUpload(t *testing.T, application *ridu.App, filename, label string) store.Document {
	t.Helper()
	document, err := application.Upload(t.Context(), "media", ridu.UploadInput{
		Filename: filename, Reader: strings.NewReader(label), Data: store.Values{"label": store.String(label)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func mongoReferenceLifecycleEnglishValues(personA, personC, team, mediaA, mediaB string) store.Values {
	return store.Values{
		"title": store.String("Lifecycle"), "location": mongoReferenceLifecyclePoint(-0.1, 51.5),
		"localLocation": mongoReferenceLifecyclePoint(-0.2, 51.6),
		"localizedMeta": store.Object(store.Values{
			"headline": store.String("English headline"), "summary": store.String("English summary"),
		}),
		"guard": store.String(personA), "assetGuard": store.String(mediaA),
		"editor":       store.String(personA),
		"contributors": store.List(store.String(personA), store.String(personC)),
		"subjects":     store.List(mongoReferenceLifecyclePoly("people", personA), mongoReferenceLifecyclePoly("teams", team)),
		"assets":       store.List(store.String(mediaA), store.String(mediaB)),
		"sections": store.List(store.Object(store.Values{
			"_key": store.String("section"), "waypoint": mongoReferenceLifecyclePoint(-0.3, 51.7),
			"reviewers":    store.List(store.String(personA), store.String(personC)),
			"localSubject": mongoReferenceLifecyclePoly("people", personA), "assets": store.List(store.String(mediaA), store.String(mediaB)),
		})),
		"layout": store.List(store.Object(store.Values{
			"_key": store.String("quote"), "blockType": store.String("quote"), "reviewer": store.String(personA),
			"subjects": store.List(mongoReferenceLifecyclePoly("people", personA), mongoReferenceLifecyclePoly("teams", team)),
			"asset":    store.String(mediaA),
		})),
		"localizedSections": store.List(store.Object(store.Values{
			"_key": store.String("localized-section"), "reviewer": store.String(personA), "assets": store.List(store.String(mediaA)),
		})),
		"localizedLayout": store.List(store.Object(store.Values{
			"_key": store.String("localized-quote"), "blockType": store.String("quote"),
			"subjects": store.List(mongoReferenceLifecyclePoly("people", personA), mongoReferenceLifecyclePoly("teams", team)), "asset": store.String(mediaA),
		})),
	}
}

func mongoReferenceLifecycleFrenchValues(personA, personB, personC, team, mediaB string) store.Values {
	return store.Values{
		"location":      mongoReferenceLifecyclePoint(1.1, 2.2),
		"localLocation": mongoReferenceLifecyclePoint(2.35, 48.85),
		"localizedMeta": store.Object(store.Values{
			"headline": store.String("Titre français"), "summary": store.String("Résumé français"),
		}),
		"editor":       store.String(personB),
		"contributors": store.List(store.String(personB), store.String(personC)),
		"subjects":     store.List(mongoReferenceLifecyclePoly("people", personB), mongoReferenceLifecyclePoly("teams", team)),
		// assets is intentionally omitted so a French read must fall back to English.
		"sections": store.List(store.Object(store.Values{
			"_key": store.String("section"), "waypoint": mongoReferenceLifecyclePoint(3.3, 4.4),
			"reviewers":    store.List(store.String(personA), store.String(personC)),
			"localSubject": mongoReferenceLifecyclePoly("people", personB), "assets": store.List(store.String(mediaB)),
		})),
		"layout": store.List(store.Object(store.Values{
			"_key": store.String("quote"), "blockType": store.String("quote"), "reviewer": store.String(personA),
			"subjects": store.List(mongoReferenceLifecyclePoly("people", personA), mongoReferenceLifecyclePoly("teams", team)),
			"asset":    store.String(mediaB),
		})),
		"localizedSections": store.List(store.Object(store.Values{
			"_key": store.String("localized-section-fr"), "reviewer": store.String(personB), "assets": store.List(store.String(mediaB)),
		})),
		"localizedLayout": store.List(store.Object(store.Values{
			"_key": store.String("localized-quote-fr"), "blockType": store.String("quote"),
			"subjects": store.List(mongoReferenceLifecyclePoly("people", personB), mongoReferenceLifecyclePoly("teams", team)),
			"asset":    store.String(mediaB),
		})),
	}
}

func mongoReferenceLifecyclePoly(collection, id string) store.Value {
	return store.Object(store.Values{"relationTo": store.String(collection), "id": store.String(id)})
}

func mongoReferenceLifecyclePoint(longitude, latitude float64) store.Value {
	return store.List(store.Number(longitude), store.Number(latitude))
}

func mongoReferenceLifecyclePopulations(t *testing.T) []query.Population {
	t.Helper()
	paths := []string{
		"editor", "contributors", "subjects", "assets", "sections.reviewers", "sections.localSubject", "sections.assets",
		"layout.quote.reviewer", "layout.quote.subjects", "layout.quote.asset",
		"localizedSections.reviewer", "localizedSections.assets",
		"localizedLayout.quote.subjects", "localizedLayout.quote.asset",
	}
	populations := make([]query.Population, len(paths))
	for index, path := range paths {
		populations[index] = query.Population{Path: mongoMustPath(t, path)}
	}
	return populations
}

func mongoReferenceLifecycleAssertPopulation(t *testing.T, document store.Document, personA, personB, personC, team, mediaA, mediaB string) {
	t.Helper()
	mongoReferenceLifecycleAssertDocument(t, document.Values["editor"], personB, true)
	contributors := mongoReferenceLifecycleList(t, document.Values["contributors"], "contributors")
	mongoReferenceLifecycleAssertDocument(t, contributors[0], personB, true)
	if id := mongoReferenceLifecycleString(contributors[1]); id != personC {
		t.Fatalf("access-filtered contributor = %#v, want retained ID %q", contributors[1], personC)
	}
	if document.LocalizationSources["assets"] != "en" {
		t.Fatalf("French assets fallback source = %q, want en", document.LocalizationSources["assets"])
	}
	assets := mongoReferenceLifecycleList(t, document.Values["assets"], "fallback assets")
	mongoReferenceLifecycleAssertDocument(t, assets[0], mediaA, false)
	mongoReferenceLifecycleAssertDocument(t, assets[1], mediaB, false)
	subjects := mongoReferenceLifecycleList(t, document.Values["subjects"], "subjects")
	mongoReferenceLifecycleAssertPolyDocument(t, subjects[0], "people", personB, true)
	mongoReferenceLifecycleAssertPolyDocument(t, subjects[1], "teams", team, false)

	sections := mongoReferenceLifecycleList(t, document.Values["sections"], "sections")
	section := mongoReferenceLifecycleObject(t, sections[0], "section")
	reviewers := mongoReferenceLifecycleList(t, section["reviewers"], "section reviewers")
	mongoReferenceLifecycleAssertDocument(t, reviewers[0], personA, true)
	if id := mongoReferenceLifecycleString(reviewers[1]); id != personC {
		t.Fatalf("access-filtered reviewer = %#v, want retained ID %q", reviewers[1], personC)
	}
	mongoReferenceLifecycleAssertPolyDocument(t, section["localSubject"], "people", personB, true)
	sectionAssets := mongoReferenceLifecycleList(t, section["assets"], "section assets")
	mongoReferenceLifecycleAssertDocument(t, sectionAssets[0], mediaB, false)

	layout := mongoReferenceLifecycleList(t, document.Values["layout"], "layout")
	quote := mongoReferenceLifecycleObject(t, layout[0], "quote")
	mongoReferenceLifecycleAssertDocument(t, quote["reviewer"], personA, true)
	quoteSubjects := mongoReferenceLifecycleList(t, quote["subjects"], "quote subjects")
	mongoReferenceLifecycleAssertPolyDocument(t, quoteSubjects[0], "people", personA, true)
	mongoReferenceLifecycleAssertPolyDocument(t, quoteSubjects[1], "teams", team, false)
	mongoReferenceLifecycleAssertDocument(t, quote["asset"], mediaB, false)

	localizedSections := mongoReferenceLifecycleList(t, document.Values["localizedSections"], "localized sections")
	localizedSection := mongoReferenceLifecycleObject(t, localizedSections[0], "localized section")
	mongoReferenceLifecycleAssertDocument(t, localizedSection["reviewer"], personB, true)
	localizedAssets := mongoReferenceLifecycleList(t, localizedSection["assets"], "localized section assets")
	mongoReferenceLifecycleAssertDocument(t, localizedAssets[0], mediaB, false)

	localizedLayout := mongoReferenceLifecycleList(t, document.Values["localizedLayout"], "localized layout")
	localizedQuote := mongoReferenceLifecycleObject(t, localizedLayout[0], "localized quote")
	localizedSubjects := mongoReferenceLifecycleList(t, localizedQuote["subjects"], "localized quote subjects")
	mongoReferenceLifecycleAssertPolyDocument(t, localizedSubjects[0], "people", personB, true)
	mongoReferenceLifecycleAssertPolyDocument(t, localizedSubjects[1], "teams", team, false)
	mongoReferenceLifecycleAssertDocument(t, localizedQuote["asset"], mediaB, false)
}

func mongoReferenceLifecycleAssertDocument(t *testing.T, value store.Value, id string, expectRedaction bool) {
	t.Helper()
	document, populated := value.CopyDocument()
	if !populated || document.ID != id {
		t.Fatalf("populated document = %#v, want %q", value, id)
	}
	if expectRedaction {
		if _, leaked := document.Values["secret"]; leaked {
			t.Fatalf("populated person %q leaked secret", id)
		}
	}
}

func mongoReferenceLifecycleAssertPolyDocument(t *testing.T, value store.Value, collection, id string, expectRedaction bool) {
	t.Helper()
	object := mongoReferenceLifecycleObject(t, value, "polymorphic relationship")
	if got := mongoReferenceLifecycleString(object["relationTo"]); got != collection {
		t.Fatalf("polymorphic relationTo = %q, want %q", got, collection)
	}
	mongoReferenceLifecycleAssertDocument(t, object["id"], id, expectRedaction)
}

func mongoReferenceLifecycleAssertNestedPolymorphicNullification(t *testing.T, document store.Document, personB, team string) {
	t.Helper()
	sections := mongoReferenceLifecycleList(t, document.Values["sections"], "post-delete sections")
	section := mongoReferenceLifecycleObject(t, sections[0], "post-delete section")
	localizedSubject := mongoReferenceLifecycleObject(t, section["localSubject"], "post-delete localized subject")
	if english, exists := localizedSubject["en"]; !exists || english.Kind() != store.ValueNull {
		t.Fatalf("post-delete English nested localized subject = %#v, want null", english)
	}
	mongoReferenceLifecycleAssertPolyReference(t, localizedSubject["fr"], "people", personB)

	localizedLayout := mongoReferenceLifecycleObject(t, document.Values["localizedLayout"], "post-delete localized layout locales")
	englishBlocks := mongoReferenceLifecycleList(t, localizedLayout["en"], "post-delete English localized layout")
	englishBlock := mongoReferenceLifecycleObject(t, englishBlocks[0], "post-delete English localized block")
	englishSubjects := mongoReferenceLifecycleList(t, englishBlock["subjects"], "post-delete English localized subjects")
	if len(englishSubjects) != 1 {
		t.Fatalf("post-delete English localized subjects = %#v, want only the unrelated team", englishSubjects)
	}
	mongoReferenceLifecycleAssertPolyReference(t, englishSubjects[0], "teams", team)

	frenchBlocks := mongoReferenceLifecycleList(t, localizedLayout["fr"], "post-delete French localized layout")
	frenchBlock := mongoReferenceLifecycleObject(t, frenchBlocks[0], "post-delete French localized block")
	frenchSubjects := mongoReferenceLifecycleList(t, frenchBlock["subjects"], "post-delete French localized subjects")
	if len(frenchSubjects) != 2 {
		t.Fatalf("post-delete French localized subjects = %#v, want person and team", frenchSubjects)
	}
	mongoReferenceLifecycleAssertPolyReference(t, frenchSubjects[0], "people", personB)
	mongoReferenceLifecycleAssertPolyReference(t, frenchSubjects[1], "teams", team)
}

func mongoReferenceLifecycleAssertPolyReference(t *testing.T, value store.Value, collection, id string) {
	t.Helper()
	object := mongoReferenceLifecycleObject(t, value, "polymorphic relationship")
	if gotCollection, gotID := mongoReferenceLifecycleString(object["relationTo"]), mongoReferenceLifecycleString(object["id"]); gotCollection != collection || gotID != id {
		t.Fatalf("polymorphic relationship = (%q, %q), want (%q, %q)", gotCollection, gotID, collection, id)
	}
}

func mongoReferenceLifecycleObject(t *testing.T, value store.Value, label string) store.Values {
	t.Helper()
	object, valid := value.CopyObject()
	if !valid {
		t.Fatalf("%s = %#v, want object", label, value)
	}
	return object
}

func mongoReferenceLifecycleList(t *testing.T, value store.Value, label string) []store.Value {
	t.Helper()
	values, valid := value.CopyList()
	if !valid || len(values) == 0 {
		t.Fatalf("%s = %#v, want non-empty list", label, value)
	}
	return values
}

func mongoReferenceLifecycleListStrings(t *testing.T, value store.Value, label string) []string {
	t.Helper()
	values := mongoReferenceLifecycleList(t, value, label)
	result := make([]string, len(values))
	for index := range values {
		result[index] = mongoReferenceLifecycleString(values[index])
	}
	return result
}

func mongoReferenceLifecycleAssertPoint(t *testing.T, value store.Value, longitude, latitude float64, label string) {
	t.Helper()
	coordinates, valid := value.CopyList()
	if !valid || len(coordinates) != 2 {
		t.Fatalf("%s = %#v, want coordinate pair", label, value)
	}
	gotLongitude, longitudeValid := coordinates[0].NumberValue()
	gotLatitude, latitudeValid := coordinates[1].NumberValue()
	if !longitudeValid || !latitudeValid || gotLongitude != longitude || gotLatitude != latitude {
		t.Fatalf("%s = [%v, %v], want [%v, %v]", label, gotLongitude, gotLatitude, longitude, latitude)
	}
}

func mongoReferenceLifecycleString(value store.Value) string {
	text, _ := value.StringValue()
	return text
}

func mongoReferenceLifecycleIssue(err error, code, path string) bool {
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "validation" {
		return false
	}
	for _, issue := range operationError.Issues {
		if issue.Code == code && issue.Path == path {
			return true
		}
	}
	return false
}

func mongoReferenceLifecycleAnyIssue(err error, codes ...string) bool {
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "validation" {
		return false
	}
	for _, issue := range operationError.Issues {
		for _, code := range codes {
			if issue.Code == code {
				return true
			}
		}
	}
	return false
}

func mongoReferenceLifecycleIssues(err error) []schema.Issue {
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) {
		return nil
	}
	return operationError.Issues
}

func mongoReferenceLifecycleContainsString(values store.Values, target string) bool {
	for _, value := range values {
		if mongoReferenceLifecycleValueContainsString(value, target) {
			return true
		}
	}
	return false
}

func mongoReferenceLifecycleValueContainsString(value store.Value, target string) bool {
	if text, valid := value.StringValue(); valid {
		return text == target
	}
	if object, valid := value.CopyObject(); valid {
		return mongoReferenceLifecycleContainsString(object, target)
	}
	if list, valid := value.CopyList(); valid {
		for _, item := range list {
			if mongoReferenceLifecycleValueContainsString(item, target) {
				return true
			}
		}
	}
	return false
}

func mongoReferenceLifecycleVersionsContain(versions []store.Version, target string) bool {
	for _, version := range versions {
		if mongoReferenceLifecycleContainsString(version.Snapshot.Values, target) {
			return true
		}
	}
	return false
}

func mongoReferenceLifecycleVersion(t *testing.T, versions []store.Version, revision int) store.Version {
	t.Helper()
	for _, version := range versions {
		if version.Revision == revision {
			return version
		}
	}
	t.Fatalf("version history does not contain revision %d: %#v", revision, versions)
	return store.Version{}
}

func mongoReferenceLifecycleAssertOwnerStateClean(t *testing.T, backend *Store, application *ridu.App, ownerID string) {
	t.Helper()
	records := mongoCollectionsBySlug(application.Manifest().Snapshot().Collections)["records"]
	referenceCount, err := backend.database.Collection(mongoReferenceCollectionName).CountDocuments(t.Context(), bson.D{
		{Key: "ownerCollection", Value: string(records.ID)}, {Key: "ownerDocument", Value: ownerID},
	})
	if err != nil || referenceCount != 0 {
		t.Fatalf("owner %q reference rows = %d, %v", ownerID, referenceCount, err)
	}
	versionCount, err := backend.database.Collection(physicalVersionCollectionName(records.ID)).CountDocuments(t.Context(), bson.D{{Key: mongoVersionOwnerPath, Value: ownerID}})
	if err != nil || versionCount != 0 {
		t.Fatalf("owner %q version rows = %d, %v", ownerID, versionCount, err)
	}
}
