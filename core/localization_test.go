package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestLocalizedUploadMetadataUsesTheRequestedLocale(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{
		Name: "Localized uploads", Storage: backend, StorageNamespace: "localized-uploads",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug: "media", Upload: true,
			UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
			Fields:       []field.Definition{field.Text("alt", field.Required(), field.Localized())},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{
		Filename: "bonjour.txt", Reader: bytes.NewBufferString("bonjour"),
		Data: store.Values{"alt": store.String("Bonjour")}, Locale: ridu.LocaleOptions{Locale: "fr"},
	})
	if err != nil {
		t.Fatal(err)
	}
	all, err := application.Local().Find(context.Background(), "media", document.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	localized, ok := all.Values["alt"].ObjectValue()
	if !ok || stringValue(localized["fr"]) != "Bonjour" {
		t.Fatalf("localized upload metadata = %#v", all.Values["alt"])
	}
	if _, exists := localized["en"]; exists {
		t.Fatalf("French upload unexpectedly populated English metadata: %#v", localized)
	}
}

func TestLocalizedScalarCRUDQueryFallbackAndAccessContext(t *testing.T) {
	var accessLocales []schema.LocaleCode
	application, err := ridu.New(ridu.Config{
		Name: "Localized operations",
		Localization: ridu.LocalizationConfig{
			DefaultLocale: "en",
			Locales: []ridu.Locale{
				{Code: "en", Label: "English"},
				{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
				{Code: "ar", Label: "Arabic", RTL: true, FallbackLocales: []schema.LocaleCode{"en"}},
			},
		},
		Collections: []ridu.Collection{{
			Slug: "posts", Versions: true,
			Fields: []field.Definition{
				field.Text("title", field.Required(), field.Localized()),
				field.Text("tagline", field.Localized()),
				field.Text("code", field.Unique(), field.Localized()),
				field.Text("slug", field.Required()),
			},
			Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				accessLocales = append(accessLocales, ctx.Locale)
				return ridu.Allow(), nil
			}, Update: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				accessLocales = append(accessLocales, ctx.Locale)
				return ridu.Allow(), nil
			}},
		}},
		Globals: []ridu.Global{{
			Slug: "settings",
			Fields: []field.Definition{
				field.Text("announcement", field.Required(), field.Localized()),
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	english, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello"), "tagline": store.String("Welcome"), "slug": store.String("hello")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := english.Values["title"].StringValue(); got != "Hello" {
		t.Fatalf("default-locale create title = %q", got)
	}

	french, err := application.Local().PublishChanges(ctx, "posts", english.ID, store.Values{"title": store.String("Bonjour")}, english.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := french.Values["title"].StringValue(); got != "Bonjour" {
		t.Fatalf("French update title = %q", got)
	}
	stillEnglish, err := application.Local().Find(ctx, "posts", english.ID, nil, ridu.LocaleOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := stillEnglish.Values["title"].StringValue(); got != "Hello" {
		t.Fatalf("French update erased English title: %q", got)
	}
	fallback, err := application.Local().Find(ctx, "posts", english.ID, nil, ridu.LocaleOptions{Locale: "ar"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := fallback.Values["title"].StringValue(); got != "Hello" {
		t.Fatalf("Arabic fallback title = %q", got)
	}
	if fallback.LocalizationSources["title"] != "en" {
		t.Fatalf("Arabic fallback source = %#v", fallback.LocalizationSources)
	}
	exact, err := application.Local().Find(ctx, "posts", english.ID, nil, ridu.LocaleOptions{Locale: "ar", DisableFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := exact.Values["title"]; exists {
		t.Fatalf("exact missing locale unexpectedly returned title: %#v", exact.Values)
	}
	copied, err := application.Local().CopyLocale(ctx, "posts", english.ID, "en", "ar", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := copied.Values["title"].StringValue(); got != "Hello" || copied.LocalizationSources["title"] != "ar" {
		t.Fatalf("copied Arabic title = %#v with sources %#v", copied.Values["title"], copied.LocalizationSources)
	}
	if len(accessLocales) < 3 || accessLocales[len(accessLocales)-3] != "en" || accessLocales[len(accessLocales)-2] != "ar" || accessLocales[len(accessLocales)-1] != "ar" {
		t.Fatalf("copy-to-locale access contexts = %#v", accessLocales)
	}
	if _, err := application.Local().PublishChanges(ctx, "posts", english.ID, store.Values{"tagline": store.String("")}, copied.Revision, nil, ridu.LocaleOptions{Locale: "ar"}); err != nil {
		t.Fatal(err)
	}
	emptyFallback, err := application.Local().Find(ctx, "posts", english.ID, nil, ridu.LocaleOptions{Locale: "ar"})
	if err != nil || stringValue(emptyFallback.Values["tagline"]) != "Welcome" || emptyFallback.LocalizationSources["tagline"] != "en" {
		t.Fatalf("empty localized value fallback = %#v, %v", emptyFallback, err)
	}
	emptyExact, err := application.Local().Find(ctx, "posts", english.ID, nil, ridu.LocaleOptions{Locale: "ar", DisableFallback: true})
	if err != nil || stringValue(emptyExact.Values["tagline"]) != "" || emptyExact.LocalizationSources["tagline"] != "ar" {
		t.Fatalf("exact empty localized value = %#v, %v", emptyExact, err)
	}

	all, err := application.Local().Find(ctx, "posts", english.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	localized, ok := all.Values["title"].ObjectValue()
	if !ok || stringValue(localized["en"]) != "Hello" || stringValue(localized["fr"]) != "Bonjour" {
		t.Fatalf("all-locales title = %#v", all.Values["title"])
	}
	duplicate, err := application.Local().Duplicate(ctx, "posts", english.ID, store.Values{
		"title": store.String("Copie"),
		"slug":  store.String("hello-copy"),
	}, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	duplicateAll, err := application.Local().Find(ctx, "posts", duplicate.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	duplicateTitles, ok := duplicateAll.Values["title"].ObjectValue()
	if !ok || stringValue(duplicateTitles["en"]) != "Hello" || stringValue(duplicateTitles["fr"]) != "Copie" {
		t.Fatalf("duplicate did not retain and override locales independently: %#v", duplicateAll.Values["title"])
	}

	title, _ := query.NewPath("title")
	where := query.Equal(title, query.String("Bonjour"))
	page, err := application.Local().List(ctx, "posts", ridu.ListOptions{Locale: "fr", Where: where})
	if err != nil || page.Total != 1 || page.Documents[0].ID != english.ID {
		t.Fatalf("French localized query = %#v, %v", page, err)
	}
	if len(accessLocales) == 0 || accessLocales[len(accessLocales)-1] != "fr" {
		t.Fatalf("access locales = %#v", accessLocales)
	}
	if _, err := application.Local().UpdateGlobal(ctx, "settings", store.Values{"announcement": store.String("Welcome")}, 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().UpdateGlobal(ctx, "settings", store.Values{"announcement": store.String("Bienvenue")}, 0, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	global, err := application.Local().Global(ctx, "settings", nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	announcements, ok := global.Values["announcement"].ObjectValue()
	if !ok || stringValue(announcements["en"]) != "Welcome" || stringValue(announcements["fr"]) != "Bienvenue" {
		t.Fatalf("localized global = %#v", global.Values["announcement"])
	}

	_, err = application.Local().Create(ctx, "posts", store.Values{"title": store.String("Autre"), "code": store.String("bonjour-code"), "slug": store.String("other")}, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.Local().Create(ctx, "posts", store.Values{"title": store.String("Encore"), "code": store.String("bonjour-code"), "slug": store.String("another")}, nil, ridu.LocaleOptions{Locale: "fr"})
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "conflict" {
		t.Fatalf("per-locale unique conflict = %v", err)
	}
	if _, err := application.Local().Find(ctx, "posts", english.ID, nil, ridu.LocaleOptions{Locale: "missing"}); !errors.As(err, &operationError) || operationError.Code != "bad_locale" {
		t.Fatalf("invalid locale error = %v", err)
	}

	versions, err := application.Local().Versions(ctx, "posts", english.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil || len(versions) < 2 {
		t.Fatalf("localized versions = %#v, %v", versions, err)
	}
	latest, ok := versions[0].Snapshot.Values["title"].ObjectValue()
	if !ok || stringValue(latest["en"]) != "Hello" || stringValue(latest["fr"]) != "Bonjour" {
		t.Fatalf("version did not preserve every locale: %#v", versions[0].Snapshot.Values["title"])
	}
	if _, err := application.Local().PublishChanges(ctx, "posts", english.ID, store.Values{"title": store.String("Changed")}, 0, nil, ridu.LocaleOptions{Locale: "en"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Restore(ctx, "posts", english.ID, versions[0].Revision, 0, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	restoredAll, err := application.Local().Find(ctx, "posts", english.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	restoredTitles, _ := restoredAll.Values["title"].ObjectValue()
	if stringValue(restoredTitles["en"]) != "Hello" || stringValue(restoredTitles["fr"]) != "Bonjour" {
		t.Fatalf("restore did not restore one canonical all-locale snapshot: %#v", restoredAll.Values["title"])
	}
}

func TestAllLocalesSortUsesTheDefaultLocaleValue(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "All-locales ordering",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title", field.Localized())}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Zulu")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Alpha")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	title, _ := query.NewPath("title")
	ascending, _ := query.NewSort(title, query.Ascending)
	page, err := application.Local().List(context.Background(), "posts", ridu.ListOptions{AllLocales: true, Sort: []query.Sort{ascending}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Documents) != 2 || page.Documents[0].ID != second.ID || page.Documents[1].ID != first.ID {
		t.Fatalf("all-locale default-locale ordering = %#v", page.Documents)
	}
}

func TestDuplicateAuthorizesEveryRetainedLocale(t *testing.T) {
	localization := ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
		{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
	}}
	t.Run("collection read", func(t *testing.T) {
		ownerPath, _ := query.NewPath("owner")
		application, err := ridu.New(ridu.Config{
			Name: "Duplicate localized collection access", Localization: localization,
			Collections: []ridu.Collection{{
				Slug: "posts", Fields: []field.Definition{field.Text("owner", field.Required()), field.Text("title", field.Localized())},
				Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if ctx.Locale == "fr" {
						if ctx.Actor == nil {
							return ridu.Deny(), nil
						}
						return ridu.Where(query.Equal(ownerPath, query.String(ctx.Actor.ID))), nil
					}
					return ridu.Allow(), nil
				}},
			}},
		}, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		ownerA := &store.Document{ID: "owner-a"}
		ownerB := &store.Document{ID: "owner-b"}
		source, err := application.Local().Create(context.Background(), "posts", store.Values{
			"owner": store.String(ownerA.ID), "title": store.String("English"),
		}, ownerA)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := application.Local().Update(context.Background(), "posts", source.ID, store.Values{"title": store.String("Français")}, ownerA, ridu.LocaleOptions{Locale: "fr"}); err != nil {
			t.Fatal(err)
		}
		if _, err := application.Local().Duplicate(context.Background(), "posts", source.ID, store.Values{"owner": store.String(ownerB.ID)}, ownerB); !operationCode(err, "access_denied") {
			t.Fatalf("duplicate retained unreadable French locale: %v", err)
		}
	})

	t.Run("field create", func(t *testing.T) {
		application, err := ridu.New(ridu.Config{
			Name: "Duplicate localized field access", Localization: localization,
			Collections: []ridu.Collection{{
				Slug: "posts", Fields: []field.Definition{field.Text("secret", field.Localized())},
				FieldAccess: map[string]ridu.FieldAccess{"secret": {Create: func(ctx ridu.FieldAccessContext) (bool, error) {
					return ctx.Locale != "fr", nil
				}}},
			}},
		}, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		source, err := application.Local().Create(context.Background(), "posts", store.Values{"secret": store.String("English")}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := application.Local().Update(context.Background(), "posts", source.ID, store.Values{"secret": store.String("French")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
			t.Fatal(err)
		}
		if _, err := application.Local().Duplicate(context.Background(), "posts", source.ID, nil, nil); !fieldAccessIssue(err, "secret.fr") {
			t.Fatalf("duplicate skipped retained-locale field create access: %v", err)
		}
	})

	t.Run("relationship validation", func(t *testing.T) {
		personReadable := true
		application, err := ridu.New(ridu.Config{
			Name: "Duplicate localized relationship validation", Localization: localization,
			Collections: []ridu.Collection{
				{
					Slug: "people", Fields: []field.Definition{field.Text("name")},
					Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
						if !personReadable {
							return ridu.Deny(), nil
						}
						return ridu.Allow(), nil
					}},
				},
				{Slug: "posts", Fields: []field.Definition{field.Text("title"), field.Relationship("editor", field.To("people"), field.Localized())}},
			},
		}, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		person, err := application.Local().Create(context.Background(), "people", store.Values{"name": store.String("Editor")}, nil)
		if err != nil {
			t.Fatal(err)
		}
		source, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Story")}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := application.Local().Update(context.Background(), "posts", source.ID, store.Values{"editor": store.String(person.ID)}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
			t.Fatal(err)
		}
		personReadable = false
		if _, err := application.Local().Duplicate(context.Background(), "posts", source.ID, nil, nil); !relationshipIssue(err, "editor.fr") {
			t.Fatalf("duplicate skipped retained-locale relationship validation: %v", err)
		}
	})
}

func TestLocalizedInverseJoinUsesTheRequestedLocale(t *testing.T) {
	var updateLocale schema.LocaleCode
	application, err := ridu.New(ridu.Config{
		Name: "Localized inverse joins",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"},
			{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{
			{Slug: "categories", Fields: []field.Definition{
				field.Text("name", field.Required()),
				field.Join("posts", "posts", "category", field.JoinDefaultSort("title")),
			}},
			{Slug: "posts", Fields: []field.Definition{
				field.Text("title", field.Required(), field.Localized()),
				field.Relationship("category", field.To("categories")),
			}, Access: ridu.CollectionAccess{Update: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				updateLocale = ctx.Locale
				return ridu.Allow(), nil
			}}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	category, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("News")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("English title"), "category": store.String(category.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "posts", post.ID, store.Values{"title": store.String("Titre français")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	found, err := application.Local().Find(ctx, "categories", category.ID, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	joined, ok := found.Values["posts"].Values()
	if !ok || len(joined) != 1 {
		t.Fatalf("joined posts = %#v", joined)
	}
	joinedPost, ok := joined[0].DocumentValue()
	if !ok {
		t.Fatalf("joined post = %#v", joined[0])
	}
	if title, _ := joinedPost.Values["title"].StringValue(); title != "Titre français" {
		t.Fatalf("localized joined title = %q", title)
	}
	unlinked, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Second English title")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "posts", unlinked.ID, store.Values{"title": store.String("Second titre français")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	updateLocale = ""
	mutated, err := application.Local().MutateJoin(ctx, "categories", category.ID, "posts", []string{unlinked.ID}, nil, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if updateLocale != "fr" {
		t.Fatalf("join update locale = %q", updateLocale)
	}
	mutatedPosts, ok := mutated.Document.Values["posts"].Values()
	if !ok || len(mutatedPosts) != 2 {
		t.Fatalf("localized mutated join = %#v", mutated.Document.Values["posts"])
	}
	for _, item := range mutatedPosts {
		document, populated := item.DocumentValue()
		if !populated {
			t.Fatalf("localized mutated join item = %#v", item)
		}
		if _, stringValue := document.Values["title"].StringValue(); !stringValue {
			t.Fatalf("localized mutated title = %#v", document.Values["title"])
		}
	}
}

func TestRESTLocalizationParametersMatchLocalAPI(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Localized REST",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
			{Code: "ar", Label: "Arabic", FallbackLocales: []schema.LocaleCode{"en"}},
		}, AvailableLocales: func(ctx ridu.LocaleAvailabilityContext) ([]schema.LocaleCode, error) {
			if ctx.Context == nil || ctx.Local == nil {
				t.Fatal("locale availability context is incomplete")
			}
			return []schema.LocaleCode{"en", "fr"}, nil
		}},
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title", field.Required(), field.Localized())}}},
		Globals:     []ridu.Global{{Slug: "settings", Fields: []field.Definition{field.Text("announcement", field.Required(), field.Localized())}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	schemaResponse := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/schema", nil, "")
	if schemaResponse.StatusCode != http.StatusOK {
		t.Fatalf("available locale schema = %d: %s", schemaResponse.StatusCode, readBody(t, schemaResponse))
	}
	var schemaEnvelope struct {
		Schema schema.Snapshot `json:"schema"`
	}
	decodeResponse(t, schemaResponse, &schemaEnvelope)
	available := schemaEnvelope.Schema.Application.Localization
	if available == nil || len(available.Locales) != 2 || available.Locales[0].Code != "en" || available.Locales[1].Code != "fr" {
		t.Fatalf("request-available locales = %#v", available)
	}
	created := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts?locale=en", strings.NewReader(`{"title":"Hello"}`), "")
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("localized create = %d: %s", created.StatusCode, readBody(t, created))
	}
	var envelope struct {
		Doc map[string]any `json:"doc"`
	}
	decodeResponse(t, created, &envelope)
	id, _ := envelope.Doc["id"].(string)
	updated := requestJSON(t, client, http.MethodPatch, "http://ridu.test/api/collections/posts/"+id+"?locale=fr", strings.NewReader(`{"title":"Bonjour"}`), "")
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("localized update = %d: %s", updated.StatusCode, readBody(t, updated))
	}
	fallback := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/"+id+"?locale=fr&fallback-locale=false", nil, "")
	if fallback.StatusCode != http.StatusOK {
		t.Fatalf("localized find = %d: %s", fallback.StatusCode, readBody(t, fallback))
	}
	decodeResponse(t, fallback, &envelope)
	if envelope.Doc["title"] != "Bonjour" {
		t.Fatalf("French REST title = %#v", envelope.Doc["title"])
	}
	inherited := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/"+id+"?locale=ar", nil, "")
	if inherited.StatusCode != http.StatusOK {
		t.Fatalf("fallback-localized find = %d: %s", inherited.StatusCode, readBody(t, inherited))
	}
	decodeResponse(t, inherited, &envelope)
	metadata, _ := envelope.Doc["_localization"].(map[string]any)
	sources, _ := metadata["sources"].(map[string]any)
	if envelope.Doc["title"] != "Hello" || sources["title"] != "en" {
		t.Fatalf("REST fallback provenance = %#v", envelope.Doc)
	}
	copiedLocale := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts/"+id+"/copy-locale", strings.NewReader(`{"from":"fr","to":"ar"}`), "")
	if copiedLocale.StatusCode != http.StatusOK {
		t.Fatalf("copy locale = %d: %s", copiedLocale.StatusCode, readBody(t, copiedLocale))
	}
	decodeResponse(t, copiedLocale, &envelope)
	if envelope.Doc["title"] != "Bonjour" {
		t.Fatalf("copied locale REST title = %#v", envelope.Doc)
	}
	all := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/"+id+"?locale=all", nil, "")
	if all.StatusCode != http.StatusOK {
		t.Fatalf("all-locales find = %d: %s", all.StatusCode, readBody(t, all))
	}
	var raw map[string]json.RawMessage
	decodeResponse(t, all, &raw)
	var document map[string]any
	if err := json.Unmarshal(raw["doc"], &document); err != nil {
		t.Fatal(err)
	}
	titles, _ := document["title"].(map[string]any)
	if titles["en"] != "Hello" || titles["fr"] != "Bonjour" {
		t.Fatalf("REST all-locales title = %#v", document["title"])
	}
	duplicated := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/posts/"+id+"/duplicate?locale=fr", strings.NewReader(`{"title":"Copie"}`), "")
	if duplicated.StatusCode != http.StatusCreated {
		t.Fatalf("localized REST duplicate = %d: %s", duplicated.StatusCode, readBody(t, duplicated))
	}
	decodeResponse(t, duplicated, &envelope)
	duplicateID, _ := envelope.Doc["id"].(string)
	duplicateAll := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/"+duplicateID+"?locale=all", nil, "")
	if duplicateAll.StatusCode != http.StatusOK {
		t.Fatalf("all-locales duplicate read = %d: %s", duplicateAll.StatusCode, readBody(t, duplicateAll))
	}
	decodeResponse(t, duplicateAll, &envelope)
	duplicateTitles, _ := envelope.Doc["title"].(map[string]any)
	if duplicateTitles["en"] != "Hello" || duplicateTitles["fr"] != "Copie" {
		t.Fatalf("REST duplicate locales = %#v", envelope.Doc["title"])
	}
	for locale, announcement := range map[string]string{"en": "Welcome", "fr": "Bienvenue"} {
		response := requestJSON(t, client, http.MethodPatch, "http://ridu.test/api/globals/settings?locale="+locale, strings.NewReader(`{"announcement":"`+announcement+`"}`), "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("localized global %s update = %d: %s", locale, response.StatusCode, readBody(t, response))
		}
	}
	globalAll := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/globals/settings?locale=all", nil, "")
	if globalAll.StatusCode != http.StatusOK {
		t.Fatalf("localized global read = %d: %s", globalAll.StatusCode, readBody(t, globalAll))
	}
	decodeResponse(t, globalAll, &envelope)
	announcements, _ := envelope.Doc["announcement"].(map[string]any)
	if announcements["en"] != "Welcome" || announcements["fr"] != "Bienvenue" {
		t.Fatalf("REST global locales = %#v", envelope.Doc["announcement"])
	}
	invalid := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/"+id+"?locale=unknown", nil, "")
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid locale status = %d", invalid.StatusCode)
	}
	allWrite := requestJSON(t, client, http.MethodPatch, "http://ridu.test/api/collections/posts/"+id+"?locale=all", strings.NewReader(`{"title":{"en":"Unsafe"}}`), "")
	if allWrite.StatusCode != http.StatusBadRequest {
		t.Fatalf("all-locales mutation status = %d: %s", allWrite.StatusCode, readBody(t, allWrite))
	}
}

func TestLocalizedDescendantsPreserveSharedNestedStructure(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Localized nested fields",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{{Slug: "pages", Fields: []field.Definition{
			field.Group("seo", field.Fields(field.Text("title", field.Required(), field.Localized()), field.Text("slug", field.Required()))),
			field.Array("links", field.Fields(field.Text("label", field.Required(), field.Localized()), field.Text("href", field.Required()))),
			field.Blocks("layout", field.BlockTypes(field.BlockType("hero", "Hero", field.Text("heading", field.Required(), field.Localized()), field.Text("theme", field.Required())))),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := application.Local().Create(ctx, "pages", store.Values{
		"seo":    store.Object(store.Values{"title": store.String("Home"), "slug": store.String("home")}),
		"links":  store.List(store.Object(store.Values{"_key": store.String("link-1"), "label": store.String("About"), "href": store.String("/about")})),
		"layout": store.List(store.Object(store.Values{"_key": store.String("block-1"), "blockType": store.String("hero"), "heading": store.String("Welcome"), "theme": store.String("dark")})),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.Local().Update(ctx, "pages", created.ID, store.Values{
		"seo":    store.Object(store.Values{"title": store.String("Accueil"), "slug": store.String("home")}),
		"links":  store.List(store.Object(store.Values{"_key": store.String("link-1"), "label": store.String("À propos"), "href": store.String("/about")})),
		"layout": store.List(store.Object(store.Values{"_key": store.String("block-1"), "blockType": store.String("hero"), "heading": store.String("Bienvenue"), "theme": store.String("dark")})),
	}, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	english, err := application.Local().Find(ctx, "pages", created.ID, nil, ridu.LocaleOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	french, err := application.Local().Find(ctx, "pages", created.ID, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if nestedString(english.Values["seo"], "title") != "Home" || nestedString(french.Values["seo"], "title") != "Accueil" {
		t.Fatalf("localized group values = en %#v, fr %#v", english.Values["seo"], french.Values["seo"])
	}
	if nestedRowString(english.Values["links"], 0, "label") != "About" || nestedRowString(french.Values["links"], 0, "label") != "À propos" {
		t.Fatalf("localized array values = en %#v, fr %#v", english.Values["links"], french.Values["links"])
	}
	if nestedRowString(english.Values["layout"], 0, "heading") != "Welcome" || nestedRowString(french.Values["layout"], 0, "heading") != "Bienvenue" {
		t.Fatalf("localized block values = en %#v, fr %#v", english.Values["layout"], french.Values["layout"])
	}
	if french.LocalizationSources["seo.title"] != "fr" || french.LocalizationSources["links.0.label"] != "fr" || french.LocalizationSources["layout.0.heading"] != "fr" {
		t.Fatalf("nested localization sources = %#v", french.LocalizationSources)
	}
	all, err := application.Local().Find(ctx, "pages", created.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	seo, _ := all.Values["seo"].ObjectValue()
	titles, ok := seo["title"].ObjectValue()
	if !ok || stringValue(titles["en"]) != "Home" || stringValue(titles["fr"]) != "Accueil" {
		t.Fatalf("all-locales nested title = %#v", seo["title"])
	}
	path, _ := query.NewPath("seo", "title")
	page, err := application.Local().List(ctx, "pages", ridu.ListOptions{Locale: "fr", Where: query.Equal(path, query.String("Accueil"))})
	if err != nil || page.Total != 1 {
		t.Fatalf("localized nested query = %#v, %v", page, err)
	}
}

func TestCopyLocaleRejectsKeylessStructuredLocalizedRows(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Keyed localized copy",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{Slug: "pages", Fields: []field.Definition{
			field.Array("links", field.Fields(field.Text("label", field.Localized()), field.Text("href"))),
			field.Blocks("layout", field.BlockTypes(field.BlockType("hero", "Hero", field.Text("heading", field.Localized()), field.Text("theme")))),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(context.Background(), "pages", store.Values{
		"links": store.List(store.Object(store.Values{
			"label": store.String("About"), "href": store.String("/about"),
		})),
		"layout": store.List(store.Object(store.Values{
			"blockType": store.String("hero"), "heading": store.String("Welcome"), "theme": store.String("dark"),
		})),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.Local().CopyLocale(context.Background(), "pages", created.ID, "en", "fr", created.Revision, nil)
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "validation" {
		t.Fatalf("keyless copy error = %v", err)
	}
	want := map[string]string{"links.0._key": "missing_row_key", "layout.0._key": "missing_row_key"}
	for _, issue := range operationError.Issues {
		if want[issue.Path] == issue.Code {
			delete(want, issue.Path)
		}
	}
	if len(want) != 0 {
		t.Fatalf("keyless copy issues = %#v, missing %#v", operationError.Issues, want)
	}
	current, err := application.Local().Find(context.Background(), "pages", created.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != created.Revision {
		t.Fatalf("failed copy changed revision from %d to %d", created.Revision, current.Revision)
	}
}

func TestAllLocalesFieldAccessTraversesLocalizedContainers(t *testing.T) {
	type observation struct {
		locale schema.LocaleCode
		path   string
	}
	var observations []observation
	readSecret := func(ctx ridu.FieldAccessContext) (bool, error) {
		observations = append(observations, observation{locale: ctx.Locale, path: ctx.RuntimePath})
		return ctx.Locale == "en", nil
	}
	application, err := ridu.New(ridu.Config{
		Name: "All-locales nested field access",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug: "pages",
			Fields: []field.Definition{
				field.Text("summary", field.Localized()),
				field.Group("details", field.Localized(), field.Fields(field.Text("secret"), field.Text("public"))),
				field.Array("rows", field.Localized(), field.Fields(field.Text("secret"), field.Text("public"))),
				field.Blocks("layout", field.Localized(), field.BlockTypes(field.BlockType("hero", "Hero", field.Text("secret"), field.Text("public")))),
			},
			FieldAccess: map[string]ridu.FieldAccess{
				"summary":            {Read: readSecret},
				"details.secret":     {Read: readSecret},
				"rows.secret":        {Read: readSecret},
				"layout.hero.secret": {Read: readSecret},
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := application.Local().Create(ctx, "pages", store.Values{
		"summary": store.String("English summary"),
		"details": store.Object(store.Values{"secret": store.String("English secret"), "public": store.String("English public")}),
		"rows":    store.List(store.Object(store.Values{"secret": store.String("English row secret"), "public": store.String("English row public")})),
		"layout":  store.List(store.Object(store.Values{"blockType": store.String("hero"), "secret": store.String("English block secret"), "public": store.String("English block public")})),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "pages", created.ID, store.Values{
		"summary": store.String("French summary"),
		"details": store.Object(store.Values{"secret": store.String("French secret"), "public": store.String("French public")}),
		"rows":    store.List(store.Object(store.Values{"secret": store.String("French row secret"), "public": store.String("French row public")})),
		"layout":  store.List(store.Object(store.Values{"blockType": store.String("hero"), "secret": store.String("French block secret"), "public": store.String("French block public")})),
	}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}

	observations = nil
	all, err := application.Local().Find(ctx, "pages", created.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	requireLocaleSecret := func(name string, value store.Value, nested func(store.Value) store.Values) {
		t.Helper()
		locales, ok := value.ObjectValue()
		if !ok {
			t.Fatalf("%s = %#v, want locale object", name, value)
		}
		english := nested(locales["en"])
		french := nested(locales["fr"])
		if stringValue(english["secret"]) == "" || stringValue(english["public"]) == "" {
			t.Fatalf("%s English values = %#v", name, english)
		}
		if _, exists := french["secret"]; exists {
			t.Fatalf("%s French secret was not redacted: %#v", name, french)
		}
		if stringValue(french["public"]) == "" {
			t.Fatalf("%s French public value was redacted: %#v", name, french)
		}
	}
	object := func(value store.Value) store.Values {
		result, _ := value.ObjectValue()
		return result
	}
	firstRow := func(value store.Value) store.Values {
		rows, _ := value.Values()
		if len(rows) == 0 {
			return nil
		}
		result, _ := rows[0].ObjectValue()
		return result
	}
	requireLocaleSecret("details", all.Values["details"], object)
	requireLocaleSecret("rows", all.Values["rows"], firstRow)
	requireLocaleSecret("layout", all.Values["layout"], firstRow)
	summaries, ok := all.Values["summary"].ObjectValue()
	if !ok || stringValue(summaries["en"]) != "English summary" {
		t.Fatalf("all-locales summary = %#v", all.Values["summary"])
	}
	if _, exists := summaries["fr"]; exists {
		t.Fatalf("French summary was not redacted: %#v", summaries)
	}

	want := map[observation]bool{
		{locale: "en", path: "summary.en"}:         true,
		{locale: "fr", path: "summary.fr"}:         true,
		{locale: "en", path: "details.en.secret"}:  true,
		{locale: "fr", path: "details.fr.secret"}:  true,
		{locale: "en", path: "rows.en.0.secret"}:   true,
		{locale: "fr", path: "rows.fr.0.secret"}:   true,
		{locale: "en", path: "layout.en.0.secret"}: true,
		{locale: "fr", path: "layout.fr.0.secret"}: true,
	}
	if len(observations) != len(want) {
		t.Fatalf("field access observations = %#v", observations)
	}
	for _, observed := range observations {
		if !want[observed] {
			t.Fatalf("unexpected field access observation %#v", observed)
		}
	}
}

func TestAllLocalesFieldAccessProjectsSiblingDataAndDocumentPerLocale(t *testing.T) {
	readFromDocument := func(ctx ridu.FieldAccessContext) (bool, error) {
		hidden, _ := ctx.Document.Values["hidden"].BooleanValue()
		return !hidden, nil
	}
	readFromSiblings := func(ctx ridu.FieldAccessContext) (bool, error) {
		hidden, _ := ctx.SiblingData["hidden"].BooleanValue()
		return !hidden, nil
	}
	application, err := ridu.New(ridu.Config{
		Name: "Locale-projected field context",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug: "pages",
			Fields: []field.Definition{
				field.Checkbox("hidden", field.Localized()),
				field.Text("secret", field.Localized()),
				field.Group("details", field.Fields(field.Checkbox("hidden", field.Localized()), field.Text("secret", field.Localized()))),
				field.Array("rows", field.Fields(field.Checkbox("hidden", field.Localized()), field.Text("secret", field.Localized()))),
				field.Blocks("layout", field.BlockTypes(field.BlockType("hero", "Hero", field.Checkbox("hidden", field.Localized()), field.Text("secret", field.Localized())))),
			},
			FieldAccess: map[string]ridu.FieldAccess{
				"secret":             {Read: readFromDocument},
				"details.secret":     {Read: readFromSiblings},
				"rows.secret":        {Read: readFromSiblings},
				"layout.hero.secret": {Read: readFromSiblings},
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(context.Background(), "pages", store.Values{
		"hidden": store.Boolean(false), "secret": store.String("English top secret"),
		"details": store.Object(store.Values{"hidden": store.Boolean(false), "secret": store.String("English group secret")}),
		"rows": store.List(store.Object(store.Values{
			"_key": store.String("row-1"), "hidden": store.Boolean(false), "secret": store.String("English row secret"),
		})),
		"layout": store.List(store.Object(store.Values{
			"_key": store.String("block-1"), "blockType": store.String("hero"), "hidden": store.Boolean(false), "secret": store.String("English block secret"),
		})),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(context.Background(), "pages", created.ID, store.Values{
		"hidden": store.Boolean(true), "secret": store.String("French top secret"),
		"details": store.Object(store.Values{"hidden": store.Boolean(true), "secret": store.String("French group secret")}),
		"rows": store.List(store.Object(store.Values{
			"_key": store.String("row-1"), "hidden": store.Boolean(true), "secret": store.String("French row secret"),
		})),
		"layout": store.List(store.Object(store.Values{
			"_key": store.String("block-1"), "blockType": store.String("hero"), "hidden": store.Boolean(true), "secret": store.String("French block secret"),
		})),
	}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	all, err := application.Local().Find(context.Background(), "pages", created.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	assertFrenchRemoved := func(path string, value store.Value) {
		t.Helper()
		locales, valid := value.ObjectValue()
		if !valid || stringValue(locales["en"]) == "" {
			t.Fatalf("%s English value = %#v", path, value)
		}
		if _, exposed := locales["fr"]; exposed {
			t.Fatalf("%s leaked French secret: %#v", path, value)
		}
	}
	assertFrenchRemoved("secret", all.Values["secret"])
	details, _ := all.Values["details"].ObjectValue()
	assertFrenchRemoved("details.secret", details["secret"])
	rows, _ := all.Values["rows"].Values()
	row, _ := rows[0].ObjectValue()
	assertFrenchRemoved("rows.secret", row["secret"])
	blocks, _ := all.Values["layout"].Values()
	block, _ := blocks[0].ObjectValue()
	assertFrenchRemoved("layout.hero.secret", block["secret"])

	capabilities, err := application.Local().Capabilities(context.Background(), "pages", created.ID, ridu.CapabilityOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"secret", "details.secret", "rows.secret", "layout.hero.secret"} {
		if capabilities.Fields[path].Read {
			t.Fatalf("canonical all-locale field capability %s failed open: %#v", path, capabilities.Fields)
		}
	}
	if !capabilities.Fields["secret.en"].Read || capabilities.Fields["secret.fr"].Read {
		t.Fatalf("top-level localized field capabilities = %#v", capabilities.Fields)
	}
	if !capabilities.Fields["details.secret.en"].Read || capabilities.Fields["details.secret.fr"].Read {
		t.Fatalf("nested localized field capabilities = %#v", capabilities.Fields)
	}
}

func TestFieldRedactionRemovesFallbackLocalizationProvenance(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Redacted localization provenance",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: []field.Definition{field.Text("title", field.Localized()), field.Text("secret", field.Localized())},
			FieldAccess: map[string]ridu.FieldAccess{"secret": {Read: func(ridu.FieldAccessContext) (bool, error) { return false, nil }}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(context.Background(), "posts", store.Values{
		"title": store.String("Public"), "secret": store.String("Hidden"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	french, err := application.Local().Find(context.Background(), "posts", created.ID, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exposed := french.Values["secret"]; exposed {
		t.Fatalf("redacted fallback value leaked: %#v", french.Values)
	}
	if _, exposed := french.LocalizationSources["secret"]; exposed {
		t.Fatalf("redacted fallback provenance leaked: %#v", french.LocalizationSources)
	}
	if french.LocalizationSources["title"] != "en" {
		t.Fatalf("public fallback provenance = %#v", french.LocalizationSources)
	}
}

func TestPopulationSelectionRemovesUnselectedFallbackLocalizationProvenance(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Projected population localization provenance",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{
			{
				Slug: "people",
				Fields: []field.Definition{
					field.Text("name", field.Localized()),
					field.Text("secret", field.Localized()),
				},
				FieldAccess: map[string]ridu.FieldAccess{
					"secret": {Read: func(ridu.FieldAccessContext) (bool, error) { return false, nil }},
				},
			},
			{Slug: "posts", Fields: []field.Definition{field.Relationship("editor", field.To("people"))}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	person, err := application.Local().Create(ctx, "people", store.Values{
		"name": store.String("Public"), "secret": store.String("Hidden"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"editor": store.String(person.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	editor, _ := query.NewPath("editor")
	name, _ := query.NewPath("name")

	metadataOnly, err := application.Local().FindWithOptions(ctx, "posts", post.ID, ridu.FindOptions{
		Locale: "fr", Populate: []query.Population{{Path: editor, Select: []query.Path{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	metadataTarget, populated := metadataOnly.Values["editor"].DocumentValue()
	if !populated || len(metadataTarget.Values) != 0 || len(metadataTarget.LocalizationSources) != 0 {
		t.Fatalf("metadata-only populated target = %#v", metadataTarget)
	}

	selected, err := application.Local().FindWithOptions(ctx, "posts", post.ID, ridu.FindOptions{
		Locale: "fr", Populate: []query.Population{{Path: editor, Select: []query.Path{name}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	selectedTarget, populated := selected.Values["editor"].DocumentValue()
	if !populated || stringValue(selectedTarget.Values["name"]) != "Public" {
		t.Fatalf("selected populated target = %#v", selectedTarget)
	}
	if selectedTarget.LocalizationSources["name"] != "en" {
		t.Fatalf("selected provenance = %#v", selectedTarget.LocalizationSources)
	}
	if _, exposed := selectedTarget.LocalizationSources["secret"]; exposed {
		t.Fatalf("unselected provenance leaked: %#v", selectedTarget.LocalizationSources)
	}
}

func TestLocalizedVersionRestoreAuthorizesEveryPersistedLocale(t *testing.T) {
	denyUpdate := false
	var deniedLocales []schema.LocaleCode
	denyCollection := false
	var collectionLocales []schema.LocaleCode
	mutateRestore := false
	mutateBeforeValidate := false
	observeAllLocaleRestore := false
	observeOriginalLocales := false
	originalByLocale := map[schema.LocaleCode]string{}
	var allLocaleHookPhases []string
	type hookShape struct {
		phase       string
		locale      schema.LocaleCode
		allLocales  bool
		dataObject  bool
		docObject   bool
		hasDocument bool
	}
	var allLocaleHookShapes []hookShape
	recordAllLocaleHook := func(phase string) ridu.Hook {
		return func(ctx ridu.HookContext) error {
			if observeAllLocaleRestore {
				allLocaleHookPhases = append(allLocaleHookPhases, phase)
				_, dataObject := ctx.Data["secret"].ObjectValue()
				docObject := false
				if ctx.Document != nil {
					_, docObject = ctx.Document.Values["secret"].ObjectValue()
				}
				allLocaleHookShapes = append(allLocaleHookShapes, hookShape{
					phase: phase, locale: ctx.Locale, allLocales: ctx.AllLocales,
					dataObject: dataObject, docObject: docObject, hasDocument: ctx.Document != nil,
				})
			}
			return nil
		}
	}
	application, err := ridu.New(ridu.Config{
		Name: "Localized restore field access",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug: "posts", Versions: true,
			Fields: []field.Definition{field.Text("secret", field.Localized())},
			Access: ridu.CollectionAccess{Update: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				if denyCollection {
					collectionLocales = append(collectionLocales, ctx.Locale)
					if ctx.Locale == "fr" {
						return ridu.Deny(), nil
					}
				}
				return ridu.Allow(), nil
			}},
			FieldAccess: map[string]ridu.FieldAccess{"secret": {Update: func(ctx ridu.FieldAccessContext) (bool, error) {
				if observeOriginalLocales && ctx.Original != nil {
					originalByLocale[ctx.Locale] = stringValue(ctx.Original.Values["secret"])
				}
				if value, valid := ctx.Value.StringValue(); valid && value == "Forbidden" {
					return false, nil
				}
				if denyUpdate {
					deniedLocales = append(deniedLocales, ctx.Locale)
					return ctx.Locale != "fr", nil
				}
				return true, nil
			}}},
			FieldHooks: map[string]ridu.CollectionHooks{"secret": {
				BeforeValidate: []ridu.Hook{func(ctx ridu.HookContext) error {
					if mutateBeforeValidate {
						ctx.Data["secret"] = store.String("Forbidden")
					}
					return nil
				}},
				BeforeOperation: []ridu.Hook{func(ctx ridu.HookContext) error {
					if mutateRestore {
						ctx.Data["secret"] = store.String("Hooked")
					}
					return recordAllLocaleHook("beforeOperation")(ctx)
				}},
				AfterChange:    []ridu.Hook{recordAllLocaleHook("afterChange")},
				AfterOperation: []ridu.Hook{recordAllLocaleHook("afterOperation")},
				AfterRead:      []ridu.Hook{recordAllLocaleHook("afterRead")},
				AfterCommit:    []ridu.Hook{recordAllLocaleHook("afterCommit")},
			}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	document, err := application.Local().Create(ctx, "posts", store.Values{"secret": store.String("Old")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	document, err = application.Local().PublishChanges(ctx, "posts", document.ID, store.Values{"secret": store.String("Ancien")}, document.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	restoreRevision := document.Revision
	document, err = application.Local().PublishChanges(ctx, "posts", document.ID, store.Values{"secret": store.String("New")}, document.Revision, nil, ridu.LocaleOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	mutateBeforeValidate = true
	if _, err := application.Local().Restore(ctx, "posts", document.ID, restoreRevision, document.Revision, nil, ridu.LocaleOptions{Locale: "en"}); !fieldAccessIssue(err, "secret.en") {
		t.Fatalf("post-before-validate field access error = %v", err)
	}
	mutateBeforeValidate = false
	denyCollection = true
	if _, err := application.Local().Restore(ctx, "posts", document.ID, restoreRevision, document.Revision, nil, ridu.LocaleOptions{Locale: "en"}); !operationCode(err, "access_denied") {
		t.Fatalf("collection-filtered restore error = %v", err)
	}
	if len(collectionLocales) != 2 || collectionLocales[0] != "en" || collectionLocales[1] != "fr" {
		t.Fatalf("collection restore access locales = %v", collectionLocales)
	}
	denyCollection = false
	denyUpdate = true
	for _, options := range []ridu.LocaleOptions{{Locale: "fr"}, {AllLocales: true}} {
		deniedLocales = nil
		if _, err := application.Local().Restore(ctx, "posts", document.ID, restoreRevision, document.Revision, nil, options); !fieldAccessIssue(err, "secret.fr") {
			t.Fatalf("restore with %#v error = %v", options, err)
		}
		if len(deniedLocales) != 2 || deniedLocales[0] != "en" || deniedLocales[1] != "fr" {
			t.Fatalf("restore with %#v evaluated locales %v", options, deniedLocales)
		}
	}
	denyUpdate = false
	current, err := application.Local().Find(ctx, "posts", document.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	secrets, _ := current.Values["secret"].ObjectValue()
	if stringValue(secrets["en"]) != "New" || stringValue(secrets["fr"]) != "Ancien" {
		t.Fatalf("denied restore changed localized values: %#v", secrets)
	}
	mutateRestore = true
	restored, err := application.Local().Restore(ctx, "posts", document.ID, restoreRevision, document.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	mutateRestore = false
	restoredAll, err := application.Local().Find(ctx, "posts", restored.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	restoredSecrets, _ := restoredAll.Values["secret"].ObjectValue()
	if stringValue(restoredSecrets["en"]) != "Old" || stringValue(restoredSecrets["fr"]) != "Hooked" {
		t.Fatalf("restore hook mutations were not merged into canonical values: %#v", restoredSecrets)
	}
	observeAllLocaleRestore = true
	observeOriginalLocales = true
	if _, err := application.Local().Restore(ctx, "posts", restored.ID, restoreRevision, restored.Revision, nil, ridu.LocaleOptions{AllLocales: true}); err != nil {
		t.Fatal(err)
	}
	observeOriginalLocales = false
	observeAllLocaleRestore = false
	if want := []string{"beforeOperation", "afterChange", "afterOperation", "afterRead", "afterCommit"}; !slices.Equal(allLocaleHookPhases, want) {
		t.Fatalf("all-locale restore field hook phases = %v, want %v", allLocaleHookPhases, want)
	}
	for _, observed := range allLocaleHookShapes {
		switch observed.phase {
		case "beforeOperation":
			if observed.locale != "en" || observed.allLocales || observed.dataObject {
				t.Fatalf("all-locale restore write hook shape = %#v", observed)
			}
		case "afterChange", "afterOperation", "afterCommit":
			if observed.locale != "en" || observed.allLocales || !observed.hasDocument || observed.docObject {
				t.Fatalf("all-locale restore post-write hook shape = %#v", observed)
			}
		case "afterRead":
			if observed.locale != "" || !observed.allLocales || !observed.hasDocument || !observed.docObject {
				t.Fatalf("all-locale restore response hook shape = %#v", observed)
			}
		}
	}
	if originalByLocale["en"] != "Old" || originalByLocale["fr"] != "Hooked" {
		t.Fatalf("restore field Original values were not locale-projected: %#v", originalByLocale)
	}
}

func TestLocalizedVersionRestoreValidatesEveryRelationshipLocale(t *testing.T) {
	idPath, err := query.NewPath("id")
	if err != nil {
		t.Fatal(err)
	}
	restrictTargets := false
	publicID := ""
	application, err := ridu.New(ridu.Config{
		Name: "Localized restore relationships",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{
				Slug: "people", Fields: []field.Definition{field.Text("name", field.Required())},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					if restrictTargets {
						return ridu.Where(query.Equal(idPath, query.String(publicID))), nil
					}
					return ridu.Allow(), nil
				}},
			},
			{
				Slug: "posts", Versions: true,
				Fields: []field.Definition{field.Text("title"), field.Relationship("editor", field.To("people"), field.Localized())},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	public, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("Public")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	publicID = public.ID
	private, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("Private")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Story"), "editor": store.String(public.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err = application.Local().PublishChanges(ctx, "posts", post.ID, store.Values{"editor": store.String(private.ID)}, post.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	restoreRevision := post.Revision
	post, err = application.Local().PublishChanges(ctx, "posts", post.ID, store.Values{"editor": store.String(public.ID)}, post.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	restrictTargets = true
	if _, err := application.Local().Restore(ctx, "posts", post.ID, restoreRevision, post.Revision, nil, ridu.LocaleOptions{Locale: "en"}); !relationshipIssue(err, "editor.fr") {
		t.Fatalf("localized relationship restore error = %v", err)
	}
	current, err := application.Local().Find(ctx, "posts", post.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	editors, _ := current.Values["editor"].ObjectValue()
	if stringValue(editors["en"]) != public.ID || stringValue(editors["fr"]) != public.ID || current.Revision != post.Revision {
		t.Fatalf("failed restore changed document: %#v", current)
	}
}

func TestExactLocaleRestorePopulationDoesNotExposeOtherTargetLocales(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Localized restore population",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{
				Slug:   "people",
				Fields: []field.Definition{field.Text("name", field.Localized()), field.Text("secret", field.Localized())},
				FieldAccess: map[string]ridu.FieldAccess{"secret": {Read: func(ctx ridu.FieldAccessContext) (bool, error) {
					return ctx.Locale == "fr", nil
				}}},
			},
			{Slug: "posts", Versions: true, Fields: []field.Definition{field.Text("title"), field.Relationship("editor", field.To("people"))}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	person, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("English"), "secret": store.String("english-secret")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "people", person.ID, store.Values{"name": store.String("French"), "secret": store.String("french-secret")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("First"), "editor": store.String(person.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	restoreRevision := post.Revision
	post, err = application.Local().PublishChanges(ctx, "posts", post.ID, store.Values{"title": store.String("Second")}, post.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	editorPath, _ := query.NewPath("editor")
	restored, err := application.Local().RestoreVersionWithOptions(ctx, "posts", post.ID, restoreRevision, false, ridu.MutationOptions{
		ExpectedRevision: post.Revision, Locale: "fr", Populate: []query.Population{{Path: editorPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	editor, populated := restored.Values["editor"].DocumentValue()
	if !populated || stringValue(editor.Values["name"]) != "French" || stringValue(editor.Values["secret"]) != "french-secret" {
		t.Fatalf("French restore population = %#v", restored.Values["editor"])
	}
	if _, localized := editor.Values["secret"].ObjectValue(); localized {
		t.Fatalf("exact-locale restore leaked locale map: %#v", editor.Values["secret"])
	}
}

func TestLocalizedRelationshipPopulationUsesTheSameEmptyStringFallbackAsProjection(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Localized relationship fallback population",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{
			{Slug: "people", Fields: []field.Definition{field.Text("name", field.Required())}},
			{Slug: "posts", Fields: []field.Definition{field.Relationship("editor", field.To("people"), field.Localized())}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	person, err := application.Local().Create(context.Background(), "people", store.Values{"name": store.String("Editor")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(context.Background(), "posts", store.Values{"editor": store.String(person.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(context.Background(), "posts", post.ID, store.Values{"editor": store.String("")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	editor, _ := query.NewPath("editor")
	fallback, err := application.Local().FindWithOptions(context.Background(), "posts", post.ID, ridu.FindOptions{
		Locale: "fr", Populate: []query.Population{{Path: editor}},
	})
	if err != nil {
		t.Fatal(err)
	}
	populated, valid := fallback.Values["editor"].DocumentValue()
	if !valid || populated.ID != person.ID || stringValue(populated.Values["name"]) != "Editor" || fallback.LocalizationSources["editor"] != "en" {
		t.Fatalf("fallback relationship population = %#v with sources %#v", fallback.Values["editor"], fallback.LocalizationSources)
	}
	exact, err := application.Local().FindWithOptions(context.Background(), "posts", post.ID, ridu.FindOptions{
		Locale: "fr", DisableFallback: true, Populate: []query.Population{{Path: editor}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if value, valid := exact.Values["editor"].StringValue(); !valid || value != "" || exact.LocalizationSources["editor"] != "fr" {
		t.Fatalf("exact empty relationship = %#v with sources %#v", exact.Values["editor"], exact.LocalizationSources)
	}
}

func TestAllLocalesPopulationRequiresTargetAccessForEveryLocale(t *testing.T) {
	name, err := query.NewPath("name")
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{
		Name: "All-locales population access",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{
				Slug: "people", Fields: []field.Definition{field.Text("name", field.Required(), field.Localized())},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					return ridu.Where(query.Equal(name, query.String("Public"))), nil
				}},
			},
			{Slug: "posts", Fields: []field.Definition{field.Text("title"), field.Relationship("editor", field.To("people"))}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	person, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("Public")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Story"), "editor": store.String(person.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "people", person.ID, store.Values{"name": store.String("Private")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	editor, _ := query.NewPath("editor")
	english, err := application.Local().FindWithOptions(ctx, "posts", post.ID, ridu.FindOptions{
		Locale: "en", Populate: []query.Population{{Path: editor}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if populated, ok := english.Values["editor"].DocumentValue(); !ok || stringValue(populated.Values["name"]) != "Public" {
		t.Fatalf("English populated target = %#v", english.Values["editor"])
	}
	all, err := application.Local().FindWithOptions(ctx, "posts", post.ID, ridu.FindOptions{
		AllLocales: true, Populate: []query.Population{{Path: editor}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, populated := all.Values["editor"].DocumentValue(); populated || stringValue(all.Values["editor"]) != person.ID {
		t.Fatalf("all-locales population leaked target = %#v", all.Values["editor"])
	}
}

func TestAllLocalesStatusHooksReceiveLocaleProjectedPopulatedTargets(t *testing.T) {
	type observation struct {
		phase      string
		locale     schema.LocaleCode
		allLocales bool
		name       string
		nameObject bool
	}
	var observations []observation
	record := func(phase string) ridu.Hook {
		return func(ctx ridu.HookContext) error {
			if ctx.Operation != ridu.OperationPublish || ctx.Document == nil {
				return nil
			}
			item := observation{phase: phase, locale: ctx.Locale, allLocales: ctx.AllLocales}
			if editor, populated := ctx.Document.Values["editor"].DocumentValue(); populated {
				item.name, _ = editor.Values["name"].StringValue()
				_, item.nameObject = editor.Values["name"].ObjectValue()
			}
			observations = append(observations, item)
			return nil
		}
	}
	application, err := ridu.New(ridu.Config{
		Name: "All-locales populated status hooks",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{Slug: "people", Fields: []field.Definition{field.Text("name", field.Required(), field.Localized())}},
			{
				Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields: []field.Definition{field.Text("title", field.Required()), field.Relationship("editor", field.To("people"), field.Required())},
				Hooks: ridu.CollectionHooks{
					AfterChange:    []ridu.Hook{record("afterChange")},
					AfterOperation: []ridu.Hook{record("afterOperation")},
					AfterCommit:    []ridu.Hook{record("afterCommit")},
				},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	person, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("Editor")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "people", person.ID, store.Values{"name": store.String("Éditrice")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Story"), "editor": store.String(person.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	editorPath, _ := query.NewPath("editor")
	published, err := application.Local().PublishWithOptions(ctx, "posts", post.ID, ridu.MutationOptions{
		ExpectedRevision: post.Revision, AllLocales: true,
		Populate: []query.Population{{Path: editorPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	populated, valid := published.Values["editor"].DocumentValue()
	if !valid {
		t.Fatalf("all-locales response editor = %#v", published.Values["editor"])
	}
	if _, valid := populated.Values["name"].ObjectValue(); !valid {
		t.Fatalf("all-locales response target name = %#v, want locale object", populated.Values["name"])
	}
	wantPhases := []string{"afterChange", "afterOperation", "afterCommit"}
	if len(observations) != len(wantPhases) {
		t.Fatalf("hook observations = %#v", observations)
	}
	for index, observed := range observations {
		if observed.phase != wantPhases[index] || observed.locale != "en" || observed.allLocales || observed.name != "Editor" || observed.nameObject {
			t.Fatalf("hook observation %d = %#v", index, observed)
		}
	}
}

func TestLocalizedRelationshipPopulationUsesTheRequestLocale(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Localized relationships",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{Slug: "people", Fields: []field.Definition{field.Text("name", field.Required(), field.Localized())}},
			{Slug: "posts", Fields: []field.Definition{field.Text("title", field.Required()), field.Relationship("editor", field.To("people"), field.Required(), field.Localized())}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	alice, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("Alice")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "people", alice.ID, store.Values{"name": store.String("Alicia")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	bob, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("Bob")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "people", bob.ID, store.Values{"name": store.String("Robert")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Story"), "editor": store.String(alice.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "posts", post.ID, store.Values{"editor": store.String(bob.ID)}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	editor, _ := query.NewPath("editor")
	idPath, _ := query.NewPath("id")
	englishPage, err := application.Local().List(ctx, "posts", ridu.ListOptions{Locale: "en", Where: query.Equal(idPath, query.String(post.ID)), Populate: []query.Population{{Path: editor}}})
	if err != nil || len(englishPage.Documents) != 1 {
		t.Fatal(err)
	}
	frenchPage, err := application.Local().List(ctx, "posts", ridu.ListOptions{Locale: "fr", Where: query.Equal(idPath, query.String(post.ID)), Populate: []query.Population{{Path: editor}}})
	if err != nil || len(frenchPage.Documents) != 1 {
		t.Fatal(err)
	}
	english, french := englishPage.Documents[0], frenchPage.Documents[0]
	englishEditor, englishPopulated := english.Values["editor"].DocumentValue()
	frenchEditor, frenchPopulated := french.Values["editor"].DocumentValue()
	if !englishPopulated || englishEditor.ID != alice.ID || stringValue(englishEditor.Values["name"]) != "Alice" {
		t.Fatalf("English populated editor = %#v", english.Values["editor"])
	}
	if !frenchPopulated || frenchEditor.ID != bob.ID || stringValue(frenchEditor.Values["name"]) != "Robert" {
		t.Fatalf("French populated editor = %#v", french.Values["editor"])
	}
}

func nestedString(value store.Value, name string) string {
	object, _ := value.ObjectValue()
	return stringValue(object[name])
}

func nestedRowString(value store.Value, index int, name string) string {
	rows, _ := value.Values()
	if index < 0 || index >= len(rows) {
		return ""
	}
	return nestedString(rows[index], name)
}
