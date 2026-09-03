package mongodb

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBLocalizedScalarOperationEngineParity(t *testing.T) {
	backend := mongoIntegrationStore(t)
	titlePath := mongoMustPath(t, "title")
	summaryPath := mongoMustPath(t, "summary")
	kindPath := mongoMustPath(t, "kind")
	headlinePath := mongoMustPath(t, "seo.headline")
	gatePath := mongoMustPath(t, "gate")
	labelPath := mongoMustPath(t, "label")
	authorPath := mongoMustPath(t, "author")
	publicGate := query.Equal(gatePath, query.String("Public"))
	publicAccess := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(publicGate), nil
	}
	config := ridu.Config{
		Name: "MongoDB localized scalar parity",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"},
			{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{
			{
				Slug: "posts",
				Fields: []field.Definition{
					field.Text("title", field.Localized()),
					field.Text("summary", field.Localized()),
					field.Text("kind", field.Required()),
					field.Group("seo", field.Required(), field.Fields(
						field.Text("headline", field.Required(), field.Localized()),
						field.Text("slug", field.Required()),
					)),
				},
			},
			{
				Slug: "secured",
				Fields: []field.Definition{
					field.Text("gate", field.Required(), field.Localized()),
					field.Text("label", field.Required(), field.Localized()),
				},
				Access: ridu.CollectionAccess{Read: publicAccess},
			},
			{
				Slug:   "authors",
				Fields: []field.Definition{field.Text("name", field.Required(), field.Localized())},
			},
			{
				Slug: "articles",
				Fields: []field.Definition{
					field.Text("title", field.Required()),
					field.Relationship("author", field.To("authors"), field.Required()),
				},
			},
			{
				Slug: "history", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields: []field.Definition{
					field.Text("gate", field.Required(), field.Localized()),
					field.Text("note", field.Required(), field.Localized()),
				},
				Access: ridu.CollectionAccess{ReadVersions: publicAccess},
			},
		},
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	collections := mongoCollectionsBySlug(application.Manifest().Snapshot().Collections)

	primary, err := application.Local().Import(t.Context(), "posts", store.Values{
		"title": store.String("Hello"), "summary": store.String("English summary"), "kind": store.String("primary"),
		"seo": store.Object(store.Values{"headline": store.String("Home"), "slug": store.String("home")}),
	}, ridu.ImportOptions{ID: "primary"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := application.Local().Find(t.Context(), "posts", primary.ID, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil || mongoLocalizedString(fallback.Values["title"]) != "Hello" || fallback.LocalizationSources["title"] != "en" {
		t.Fatalf("French fallback = %#v, %v", fallback, err)
	}
	if mongoLocalizedNestedString(fallback.Values["seo"], "headline") != "Home" || fallback.LocalizationSources["seo.headline"] != "en" {
		t.Fatalf("nested French fallback = %#v", fallback)
	}
	updated, err := application.Local().Update(t.Context(), "posts", primary.ID, store.Values{
		"title": store.String("Bonjour"), "summary": store.String(""),
		"seo": store.Object(store.Values{"headline": store.String("Accueil"), "slug": store.String("home")}),
	}, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if mongoLocalizedString(updated.Values["title"]) != "Bonjour" || mongoLocalizedNestedString(updated.Values["seo"], "headline") != "Accueil" {
		t.Fatalf("French update = %#v", updated.Values)
	}
	fallback, err = application.Local().Find(t.Context(), "posts", primary.ID, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil || mongoLocalizedString(fallback.Values["summary"]) != "English summary" || fallback.LocalizationSources["summary"] != "en" {
		t.Fatalf("non-final empty fallback = %#v, %v", fallback, err)
	}
	exact, err := application.Local().Find(t.Context(), "posts", primary.ID, nil, ridu.LocaleOptions{Locale: "fr", DisableFallback: true})
	exactSummary, exactSummaryIsString := exact.Values["summary"].StringValue()
	if err != nil || !exactSummaryIsString || exactSummary != "" || exact.LocalizationSources["summary"] != "fr" {
		t.Fatalf("final empty value = %#v, %v", exact, err)
	}
	all, err := application.Local().FindWithOptions(t.Context(), "posts", primary.ID, ridu.FindOptions{
		Select: []query.Path{titlePath, summaryPath, mongoMustPath(t, "seo")}, AllLocales: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	titles, titleMap := all.Values["title"].ObjectValue()
	summaries, summaryMap := all.Values["summary"].ObjectValue()
	seo, seoMap := all.Values["seo"].ObjectValue()
	headlines, headlineMap := seo["headline"].ObjectValue()
	frenchSummary, frenchSummaryIsString := summaries["fr"].StringValue()
	if !titleMap || !summaryMap || !seoMap || !headlineMap ||
		mongoLocalizedString(titles["en"]) != "Hello" || mongoLocalizedString(titles["fr"]) != "Bonjour" ||
		mongoLocalizedString(summaries["en"]) != "English summary" || !frenchSummaryIsString || frenchSummary != "" ||
		mongoLocalizedString(headlines["en"]) != "Home" || mongoLocalizedString(headlines["fr"]) != "Accueil" {
		t.Fatalf("all-locales canonical projection = %#v", all.Values)
	}

	directWrite := mongoBegin(t, backend, false)
	directDocument, err := directWrite.Create(t.Context(), store.CreateRequest{
		Collection: collections["posts"], ID: "direct-localized-merge", Locales: []schema.LocaleCode{"en", "fr"},
		Values: store.Values{
			"title": store.Object(store.Values{"en": store.String("Direct"), "fr": store.String("Direct FR")}),
			"kind":  store.String("direct"),
			"seo": store.Object(store.Values{
				"headline": store.Object(store.Values{"en": store.String("Direct home"), "fr": store.String("Direct accueil")}),
				"slug":     store.String("direct-slug"),
			}),
		},
	})
	if err != nil {
		mongoRollback(t, directWrite)
		t.Fatal(err)
	}
	directDocument, err = directWrite.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{
			Collection: collections["posts"], ID: directDocument.ID,
			Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: []schema.LocaleCode{"fr", "en"},
		},
		Values: store.Values{
			"title": store.Object(store.Values{"fr": store.String("$Direct bonjour")}),
			"seo": store.Object(store.Values{
				"headline": store.Object(store.Values{"fr": store.String("Direct bienvenue")}),
			}),
		},
	})
	if err != nil {
		mongoRollback(t, directWrite)
		t.Fatal(err)
	}
	directTitles, _ := directDocument.Values["title"].ObjectValue()
	directSEO, _ := directDocument.Values["seo"].ObjectValue()
	directHeadlines, _ := directSEO["headline"].ObjectValue()
	if mongoLocalizedString(directTitles["en"]) != "Direct" || mongoLocalizedString(directTitles["fr"]) != "$Direct bonjour" ||
		mongoLocalizedString(directHeadlines["en"]) != "Direct home" || mongoLocalizedString(directHeadlines["fr"]) != "Direct bienvenue" ||
		mongoLocalizedString(directSEO["slug"]) != "direct-slug" {
		mongoRollback(t, directWrite)
		t.Fatalf("atomic partial localized merge = %#v", directDocument.Values)
	}
	mongoCommit(t, directWrite)

	author := mongoLocalizedImport(t, application, "authors", "localized-author", store.Values{"name": store.String("Author")})
	if _, err := application.Local().Update(t.Context(), "authors", author.ID, store.Values{"name": store.String("Auteur")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	article, err := application.Local().Create(t.Context(), "articles", store.Values{
		"title": store.String("Population"), "author": store.String(author.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	populated, err := application.Local().FindWithOptions(t.Context(), "articles", article.ID, ridu.FindOptions{
		Locale: "fr", Populate: []query.Population{{Path: authorPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	populatedAuthor, populatedDocument := populated.Values["author"].DocumentValue()
	if !populatedDocument || mongoLocalizedString(populatedAuthor.Values["name"]) != "Auteur" || populatedAuthor.LocalizationSources["name"] != "fr" {
		t.Fatalf("single-locale populated localized target = %#v", populated.Values["author"])
	}
	allPopulated, err := application.Local().FindWithOptions(t.Context(), "articles", article.ID, ridu.FindOptions{
		AllLocales: true, Populate: []query.Population{{Path: authorPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	allPopulatedAuthor, populatedDocument := allPopulated.Values["author"].DocumentValue()
	allNames, canonicalNames := allPopulatedAuthor.Values["name"].ObjectValue()
	if !populatedDocument || !canonicalNames || mongoLocalizedString(allNames["en"]) != "Author" || mongoLocalizedString(allNames["fr"]) != "Auteur" {
		t.Fatalf("all-locales populated localized target = %#v", allPopulated.Values["author"])
	}
	for _, test := range []struct {
		path query.Path
		want string
	}{
		{path: titlePath, want: "Bonjour"},
		{path: summaryPath, want: "English summary"},
		{path: headlinePath, want: "Accueil"},
	} {
		page, err := application.Local().List(t.Context(), "posts", ridu.ListOptions{
			Locale: "fr", Where: query.Equal(test.path, query.String(test.want)), Limit: 10,
		})
		if err != nil || page.Total != 1 || page.Documents[0].ID != primary.ID {
			t.Fatalf("localized filter %s = %#v, %v", test.path.String(), page, err)
		}
	}

	for _, fixture := range []struct {
		id      string
		english string
		french  *string
	}{
		{id: "sort-a", english: "Alpha"},
		{id: "sort-a2", english: "Zulu", french: mongoLocalizedStringPointer("Alpha")},
		{id: "sort-b", english: "Bravo"},
		{id: "sort-empty", english: "Charlie", french: mongoLocalizedStringPointer("")},
	} {
		document, err := application.Local().Import(t.Context(), "posts", store.Values{
			"title": store.String(fixture.english), "kind": store.String("sort"),
			"seo": store.Object(store.Values{"headline": store.String(fixture.english), "slug": store.String(fixture.id)}),
		}, ridu.ImportOptions{ID: fixture.id}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if fixture.french != nil {
			if _, err := application.Local().Update(t.Context(), "posts", document.ID, store.Values{"title": store.String(*fixture.french)}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	ascendingTitle, err := query.NewSort(titlePath, query.Ascending)
	if err != nil {
		t.Fatal(err)
	}
	sorted, err := application.Local().List(t.Context(), "posts", ridu.ListOptions{
		Locale: "fr", Where: query.Equal(kindPath, query.String("sort")), Sort: []query.Sort{ascendingTitle}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(sorted.Documents))
	for index, document := range sorted.Documents {
		ids[index] = document.ID
	}
	if want := []string{"sort-a", "sort-a2", "sort-b", "sort-empty"}; !slices.Equal(ids, want) {
		t.Fatalf("localized stable sort = %#v, want %#v", ids, want)
	}
	distinct, err := application.Local().Distinct(t.Context(), "posts", ridu.DistinctOptions{
		Field: titlePath, Where: query.Equal(kindPath, query.String("sort")), Locale: "fr", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	distinctTitles := make([]string, len(distinct.Values))
	for index, value := range distinct.Values {
		distinctTitles[index] = mongoLocalizedString(value)
	}
	if distinct.Total != 3 || !slices.Equal(distinctTitles, []string{"Alpha", "Bravo", "Charlie"}) {
		t.Fatalf("localized distinct = %#v", distinct)
	}

	allowed := mongoLocalizedImport(t, application, "secured", "allowed", store.Values{
		"gate": store.String("Public"), "label": store.String("Match"),
	})
	if _, err := application.Local().Update(t.Context(), "secured", allowed.ID, store.Values{
		"gate": store.String("Public"), "label": store.String("Correspond"),
	}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	mixed := mongoLocalizedImport(t, application, "secured", "mixed", store.Values{
		"gate": store.String("Public"), "label": store.String("Match"),
	})
	if _, err := application.Local().Update(t.Context(), "secured", mixed.ID, store.Values{
		"gate": store.String("Private"), "label": store.String("Correspond"),
	}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Find(t.Context(), "secured", mixed.ID, nil, ridu.LocaleOptions{Locale: "en"}); err != nil {
		t.Fatalf("English localized access: %v", err)
	}
	if _, err := application.Local().Find(t.Context(), "secured", mixed.ID, nil, ridu.LocaleOptions{Locale: "fr"}); !mongoOperationCode(err, "not_found") {
		t.Fatalf("French localized access error = %v", err)
	}
	if _, err := application.Local().Find(t.Context(), "secured", mixed.ID, nil, ridu.LocaleOptions{AllLocales: true}); !mongoOperationCode(err, "not_found") {
		t.Fatalf("all-locales access error = %v", err)
	}
	if _, err := application.Local().Find(t.Context(), "secured", allowed.ID, nil, ridu.LocaleOptions{AllLocales: true}); err != nil {
		t.Fatalf("all-locales public access: %v", err)
	}
	securedPage, err := application.Local().List(t.Context(), "secured", ridu.ListOptions{
		Locale: "fr", Where: query.Equal(labelPath, query.String("Correspond")), Limit: 10,
	})
	if err != nil || securedPage.Total != 1 || securedPage.Documents[0].ID != allowed.ID {
		t.Fatalf("atomic localized filter plus access = %#v, %v", securedPage, err)
	}

	history := mongoLocalizedImport(t, application, "history", "history", store.Values{
		"gate": store.String("Public"), "note": store.String("First"),
	})
	if _, err := application.Local().Update(t.Context(), "history", history.ID, store.Values{
		"gate": store.String("Private"), "note": store.String("Second"),
	}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	englishVersions, err := application.Local().Versions(t.Context(), "history", history.ID, nil, ridu.LocaleOptions{Locale: "en"})
	if err != nil || len(englishVersions) != 2 {
		t.Fatalf("English localized versions = %#v, %v", englishVersions, err)
	}
	frenchVersions, err := application.Local().Versions(t.Context(), "history", history.ID, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil || len(frenchVersions) != 1 || frenchVersions[0].Revision != 1 || mongoLocalizedString(frenchVersions[0].Snapshot.Values["gate"]) != "Public" {
		t.Fatalf("French fallback version access = %#v, %v", frenchVersions, err)
	}
	exactFrenchVersions, err := application.Local().Versions(t.Context(), "history", history.ID, nil, ridu.LocaleOptions{Locale: "fr", DisableFallback: true})
	if err != nil || len(exactFrenchVersions) != 0 {
		t.Fatalf("exact French version access = %#v, %v", exactFrenchVersions, err)
	}
	allVersions, err := application.Local().Versions(t.Context(), "history", history.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil || len(allVersions) != 0 {
		t.Fatalf("all-locales version access = %#v, %v", allVersions, err)
	}
	if _, err := backend.database.Collection(physicalVersionCollectionName(collections["history"].ID)).UpdateOne(
		t.Context(),
		bson.D{{Key: "_id", Value: versionID(history.ID, 1)}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "snapshot.values.gate.de", Value: "Öffentlich"}}}},
	); err != nil {
		t.Fatal(err)
	}
	sparseVersionRead := mongoBegin(t, backend, true)
	storedVersion, err := sparseVersionRead.(store.VersionTransaction).FindVersion(t.Context(), collections["history"], history.ID, 1)
	if err != nil {
		mongoRollback(t, sparseVersionRead)
		t.Fatalf("request-less FindVersion rejected sparse locale history: %v", err)
	}
	storedGates, _ := storedVersion.Snapshot.Values["gate"].ObjectValue()
	if mongoLocalizedString(storedGates["de"]) != "Öffentlich" {
		mongoRollback(t, sparseVersionRead)
		t.Fatalf("request-less FindVersion sparse locale = %#v", storedVersion.Snapshot.Values["gate"])
	}
	mongoCommit(t, sparseVersionRead)
	strictVersionRead := mongoBegin(t, backend, true)
	publicGateNode := publicGate.Node()
	if _, err := strictVersionRead.(store.VersionTransaction).ListVersions(t.Context(), store.VersionRequest{
		Collection: collections["history"], DocumentID: history.ID, Access: &publicGateNode,
		Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: []schema.LocaleCode{"en"},
	}); err == nil || !strings.Contains(err.Error(), `unconfigured locale "de"`) {
		mongoRollback(t, strictVersionRead)
		t.Fatalf("request-aware ListVersions locale membership error = %v", err)
	}
	mongoCommit(t, strictVersionRead)
	if _, err := application.Local().Versions(t.Context(), "history", history.ID, nil, ridu.LocaleOptions{Locale: "en"}); err == nil {
		t.Fatal("operation-engine ListVersions exposed an unconfigured stored locale")
	}

	unconfigured := mongoLocalizedImport(t, application, "posts", "unconfigured-locale", store.Values{
		"title": store.String("Configured"), "kind": store.String("unconfigured"),
		"seo": store.Object(store.Values{"headline": store.String("Configured"), "slug": store.String("unconfigured")}),
	})
	if _, err := backend.database.Collection(physicalCollectionName(collections["posts"].ID)).UpdateOne(
		t.Context(),
		bson.D{{Key: "_id", Value: unconfigured.ID}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "values.title.de", Value: "Nicht konfiguriert"}}}},
	); err != nil {
		t.Fatal(err)
	}
	strictRead := mongoBegin(t, backend, true)
	strictRequest := store.Request{
		Collection: collections["posts"], ID: unconfigured.ID,
		Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: []schema.LocaleCode{"en"}, AllLocales: true,
	}
	if _, err := strictRead.Find(t.Context(), strictRequest); err == nil || !strings.Contains(err.Error(), `unconfigured locale "de"`) {
		mongoRollback(t, strictRead)
		t.Fatalf("request-aware Find locale membership error = %v", err)
	}
	strictRequest.ID = ""
	strictRequest.Filter = nil
	strictRequest.Page, strictRequest.Limit = 1, 100
	strictPage, err := strictRead.List(t.Context(), strictRequest)
	if err != nil || strictPage.Total != len(strictPage.Documents) {
		mongoRollback(t, strictRead)
		t.Fatalf("request-aware List did not fail closed over an unconfigured stored locale: %#v, %v", strictPage, err)
	}
	for _, document := range strictPage.Documents {
		if document.ID == unconfigured.ID {
			mongoRollback(t, strictRead)
			t.Fatalf("request-aware List exposed an unconfigured stored locale: %#v", strictPage)
		}
	}
	mongoCommit(t, strictRead)
	if _, err := application.Local().Find(t.Context(), "posts", unconfigured.ID, nil, ridu.LocaleOptions{AllLocales: true}); err == nil {
		t.Fatal("operation-engine Find exposed an unconfigured stored locale")
	}
	closedPage, err := application.Local().List(t.Context(), "posts", ridu.ListOptions{
		AllLocales: true, Where: query.Equal(kindPath, query.String("unconfigured")), Limit: 10,
	})
	if err != nil || closedPage.Total != 0 || len(closedPage.Documents) != 0 {
		t.Fatalf("operation-engine List did not fail closed over an unconfigured stored locale: %#v, %v", closedPage, err)
	}
	rejectedMutation := mongoBegin(t, backend, false)
	if _, err := rejectedMutation.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{
			Collection: collections["posts"], ID: unconfigured.ID,
			Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: []schema.LocaleCode{"en"},
		},
		Values: store.Values{"kind": store.String("must-rollback")},
	}); err == nil || !strings.Contains(err.Error(), `unconfigured locale "de"`) {
		mongoRollback(t, rejectedMutation)
		t.Fatalf("request-aware mutation locale membership error = %v", err)
	}
	mongoRollback(t, rejectedMutation)

	corrupt := mongoLocalizedImport(t, application, "posts", "corrupt", store.Values{
		"title": store.String("Safe"), "kind": store.String("corrupt"),
		"seo": store.Object(store.Values{"headline": store.String("Safe"), "slug": store.String("corrupt")}),
	})
	if _, err := backend.database.Collection(physicalCollectionName(collections["posts"].ID)).UpdateOne(
		t.Context(),
		bson.D{{Key: "_id", Value: corrupt.ID}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "values.title.en", Value: float64(42)}}}},
	); err != nil {
		t.Fatal(err)
	}
	closed, err := application.Local().List(t.Context(), "posts", ridu.ListOptions{
		Where: query.Equal(titlePath, query.String("Safe")), Limit: 10,
	})
	if err != nil || closed.Total != 0 || len(closed.Documents) != 0 {
		t.Fatalf("malformed localized scalar satisfied a typed filter: %#v, %v", closed, err)
	}
	if _, err := application.Local().Find(t.Context(), "posts", corrupt.ID, nil); err == nil {
		t.Fatal("operation-engine Find exposed malformed localized BSON")
	}
}

func TestMongoDBDecoderFreeSparseLocaleEnvelopeRejectsNonCanonicalKeys(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoLocalizedScalarCollection()
	rank := mongoMustPath(t, "rank")
	collection.Fields = append(collection.Fields, schema.Field{
		ID: "posts-rank", Name: "rank", Path: rank,
		Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Number: &schema.NumberField{},
	})
	if err := backend.SyncIndexes(t.Context(), mongoLocalizedIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}
	locales := []schema.LocaleCode{"en", "fr"}
	write := mongoBegin(t, backend, false)
	for index, id := range []string{"sparse-locale-valid", "sparse-locale-reserved", "sparse-locale-oversized"} {
		if _, err := write.Create(t.Context(), store.CreateRequest{
			Collection: collection, ID: id, Locales: locales,
			Values: store.Values{
				"title": store.Object(store.Values{"en": store.String(id)}),
				"rank":  store.Number(float64(index + 1)),
				"seo": store.Object(store.Values{
					"headline": store.Object(store.Values{"en": store.String("headline")}),
				}),
			},
		}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	mongoCommit(t, write)

	physical := backend.database.Collection(physicalCollectionName(collection.ID))
	for _, corruption := range []struct {
		id     string
		locale string
	}{
		{id: "sparse-locale-reserved", locale: "none"},
		{id: "sparse-locale-oversized", locale: strings.Repeat("a", 36)},
	} {
		if _, err := physical.UpdateOne(
			t.Context(),
			bson.D{{Key: "_id", Value: corruption.id}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "values.title." + corruption.locale, Value: "corrupt"}}}},
		); err != nil {
			t.Fatal(err)
		}
	}

	filter := query.GreaterThanEqual(rank, query.Number(0)).Node()
	read := mongoBegin(t, backend, true)
	page, err := read.List(t.Context(), store.Request{
		Collection: collection, Filter: &filter, Page: math.MaxInt, Limit: 1, Locales: locales,
	})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Documents) != 0 {
		mongoRollback(t, read)
		t.Fatalf("sparse locale corruption entered decoder-free list total: %#v", page)
	}
	selection, err := read.ResolveFilteredSelection(t.Context(), store.FilteredSelectionRequest{
		Collection: collection, Filter: &filter, Limit: 1, Locales: locales,
	})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if selection.Overflow || len(selection.IDs) != 1 || selection.IDs[0] != "sparse-locale-valid" {
		mongoRollback(t, read)
		t.Fatalf("sparse locale corruption entered filtered selection: %#v", selection)
	}
	distinct := read.(store.DistinctTransaction)
	values, err := distinct.Distinct(t.Context(), store.DistinctRequest{
		Collection: collection, Field: rank, Filter: &filter, Page: math.MaxInt, Limit: 1, Locales: locales,
	})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if values.Total != 1 || len(values.Values) != 0 {
		mongoRollback(t, read)
		t.Fatalf("sparse locale corruption entered decoder-free distinct total: %#v", values)
	}
	for _, id := range []string{"sparse-locale-reserved", "sparse-locale-oversized"} {
		if _, err := read.Find(t.Context(), store.Request{Collection: collection, ID: id, Locales: locales}); err == nil {
			mongoRollback(t, read)
			t.Fatalf("strict decode accepted non-canonical stored locale in %q", id)
		}
	}
	mongoCommit(t, read)
}

func mongoLocalizedImport(t *testing.T, application *ridu.App, collection, id string, values store.Values) store.Document {
	t.Helper()
	document, err := application.Local().Import(t.Context(), collection, values, ridu.ImportOptions{ID: id}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func mongoLocalizedString(value store.Value) string {
	text, _ := value.StringValue()
	return text
}

func mongoLocalizedNestedString(value store.Value, name string) string {
	object, _ := value.ObjectValue()
	return mongoLocalizedString(object[name])
}

func mongoLocalizedStringPointer(value string) *string {
	return &value
}
