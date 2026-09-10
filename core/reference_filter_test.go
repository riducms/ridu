package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestReferenceOptionFiltersAreServerEnforcedAcrossRelationshipsUploadsAndNestedShapes(t *testing.T) {
	ctx := context.Background()
	storageBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	visiblePath, err := query.NewPath("visible")
	if err != nil {
		t.Fatal(err)
	}
	var hookCategory string
	application, err := ridu.New(ridu.Config{
		Name: "Reference option filter admission", Storage: storageBackend, StorageNamespace: "reference-option-filter-admission",
		Collections: []ridu.Collection{
			{
				Slug: "people", Fields: field.Fields{field.Text("category"), field.Text("region"), field.Checkbox("visible").Required()},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					return ridu.Where(query.Equal(visiblePath, query.Boolean(true))), nil
				}},
			},
			{Slug: "teams", Fields: field.Fields{field.Text("category")}},
			{
				Slug: "media", Upload: true, UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
				Fields: field.Fields{field.Text("assetType"), field.Checkbox("visible").Required()},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					return ridu.Where(query.Equal(visiblePath, query.Boolean(true))), nil
				}},
			},
			{
				Slug: "entries", Trash: true,
				Fields: field.Fields{field.Text("category"), field.Text("region"), field.Relationship("author", "people").FilterOptionRules(field.OptionFilter("category", field.FilterEquals, "category"), field.OptionFilter("region", field.FilterEquals, "region")), field.PolymorphicRelationship("subject", "people", "teams").FilterOptionRules(field.OptionFilterFor("people", "region", field.FilterEquals, "region"),
					field.OptionFilterFor("teams", "category", field.FilterEquals, "category")), field.Upload("hero", "media").FilterOptionRules(field.OptionFilter("assetType", field.FilterEquals, "category")), field.Group("meta", field.Fields{field.Relationship("author", "people").FilterOptionRules(field.OptionFilter("category", field.FilterEquals, "category"))}), field.Array("rows", field.Fields{field.Relationship("author", "people").FilterOptionRules(field.OptionFilter("category", field.FilterEquals, "category"))}), field.Blocks("content", field.Block{Slug: "quote", Fields: field.Fields{field.Relationship("author", "people").FilterOptionRules(field.OptionFilter("category", field.FilterEquals, "category"))}}),
				},
				Hooks: ridu.CollectionHooks{BeforeOperation: []ridu.Hook{func(hookContext ridu.HookContext) error {
					if hookContext.Operation == operation.Update && hookCategory != "" {
						hookContext.Data["category"] = store.String(hookCategory)
					}
					return nil
				}}},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	matching, err := application.Local().Create(ctx, "people", store.Values{
		"category": store.String("article"), "region": store.String("eu"), "visible": store.Boolean(true),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongRegion, err := application.Local().Create(ctx, "people", store.Values{
		"category": store.String("article"), "region": store.String("us"), "visible": store.Boolean(true),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := application.Local().Create(ctx, "people", store.Values{
		"category": store.String("article"), "region": store.String("eu"), "visible": store.Boolean(false),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	emptyCategory, err := application.Local().Create(ctx, "people", store.Values{
		"category": store.String(""), "visible": store.Boolean(true),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongTeam, err := application.Local().Create(ctx, "teams", store.Values{"category": store.String("video")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	matchingUpload, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "article.txt", Reader: strings.NewReader("article"),
		Data: store.Values{"assetType": store.String("article"), "visible": store.Boolean(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongUpload, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "video.txt", Reader: strings.NewReader("video"),
		Data: store.Values{"assetType": store.String("video"), "visible": store.Boolean(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	hiddenUpload, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "hidden.txt", Reader: strings.NewReader("hidden"),
		Data: store.Values{"assetType": store.String("article"), "visible": store.Boolean(false)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("article"), "region": store.String("eu"), "author": store.String(wrongRegion.ID),
	}, nil); !relationshipIssue(err, "author") {
		t.Fatalf("AND-filtered relationship error = %v", err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("article"), "region": store.String("eu"), "author": store.String(hidden.ID),
	}, nil); !relationshipIssue(err, "author") {
		t.Fatalf("read-filtered relationship error = %v", err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String(""), "author": store.String(matching.ID),
	}, nil); !relationshipIssue(err, "author") {
		t.Fatalf("empty-string option predicate was omitted: %v", err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String(""), "author": store.String(emptyCategory.ID),
	}, nil); err != nil {
		t.Fatalf("matching empty-string option predicate = %v", err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("article"), "region": store.String("eu"),
		"subject": store.Object(store.Values{"relationTo": store.String("teams"), "id": store.String(wrongTeam.ID)}),
	}, nil); !relationshipIssue(err, "subject") {
		t.Fatalf("polymorphic relationship filter error = %v", err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"region":  store.String("eu"),
		"subject": store.Object(store.Values{"relationTo": store.String("people"), "id": store.String(wrongRegion.ID)}),
	}, nil); !relationshipIssue(err, "subject") {
		t.Fatalf("target-scoped polymorphic relationship filter error = %v", err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("does-not-apply"), "region": store.String("eu"),
		"subject": store.Object(store.Values{"relationTo": store.String("people"), "id": store.String(matching.ID)}),
	}, nil); err != nil {
		t.Fatalf("other polymorphic target's option filter leaked across scopes: %v", err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("article"), "hero": store.String(wrongUpload.ID),
	}, nil); !uploadReferenceIssue(err, "hero") {
		t.Fatalf("option-filtered upload error = %v", err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("article"), "hero": store.String(hiddenUpload.ID),
	}, nil); !uploadReferenceIssue(err, "hero") {
		t.Fatalf("read-filtered upload error = %v", err)
	}

	for _, test := range []struct {
		name   string
		path   string
		values store.Values
	}{
		{name: "group", path: "meta.author", values: store.Values{
			"category": store.String("other"), "meta": store.Object(store.Values{"author": store.String(matching.ID)}),
		}},
		{name: "array", path: "rows.0.author", values: store.Values{
			"category": store.String("other"), "rows": store.List(store.Object(store.Values{"author": store.String(matching.ID)})),
		}},
		{name: "block", path: "content.0.author", values: store.Values{
			"category": store.String("other"), "content": store.List(store.Object(store.Values{
				"blockType": store.String("quote"), "author": store.String(matching.ID),
			})),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := application.Local().Create(ctx, "entries", test.values, nil); !relationshipIssue(err, test.path) {
				t.Fatalf("nested reference filter error = %v", err)
			}
		})
	}

	// A missing optional source produces no option predicate, but target Read is
	// still mandatory and cannot be widened by the picker configuration.
	if _, err := application.Local().Create(ctx, "entries", store.Values{"author": store.String(wrongRegion.ID)}, nil); err != nil {
		t.Fatalf("missing optional source should omit its predicates: %v", err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{"author": store.String(hidden.ID)}, nil); !relationshipIssue(err, "author") {
		t.Fatalf("missing source widened target Read access: %v", err)
	}

	accepted, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("article"), "region": store.String("eu"),
		"author": store.String(matching.ID), "hero": store.String(matchingUpload.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "entries", accepted.ID, store.Values{"category": store.String("other")}, nil); !relationshipIssue(err, "author") || !uploadReferenceIssue(err, "hero") {
		t.Fatalf("source-only update did not revalidate unchanged references: %v", err)
	}
	hookCategory = "other"
	if _, err := application.Local().Update(ctx, "entries", accepted.ID, store.Values{"region": store.String("eu")}, nil); !relationshipIssue(err, "author") || !uploadReferenceIssue(err, "hero") {
		t.Fatalf("post-hook candidate did not revalidate unchanged references: %v", err)
	}
}

func TestLiteralPublishedReferenceFilterIsServerEnforced(t *testing.T) {
	ctx := context.Background()
	application, err := ridu.New(ridu.Config{
		Name: "Published reference admission",
		Collections: []ridu.Collection{
			{Slug: "lessons", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}},
			{Slug: "islands", Fields: field.Fields{field.Text("title"), field.Relationship("lesson", "lessons").Required().FilterOptionRules(field.OptionFilterValue("_status", field.FilterEquals, "published"))}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	draft, err := application.Local().Create(ctx, "lessons", store.Values{"title": store.String("Draft")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "islands", store.Values{
		"title": store.String("Rejected"), "lesson": store.String(draft.ID),
	}, nil); !relationshipIssue(err, "lesson") {
		t.Fatalf("draft relationship error = %v", err)
	}

	published, err := application.Local().Publish(ctx, "lessons", draft.ID, draft.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	island, err := application.Local().Create(ctx, "islands", store.Values{
		"title": store.String("Accepted"), "lesson": store.String(published.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	unpublished, err := application.Local().Unpublish(ctx, "lessons", published.ID, published.Revision, nil)
	if err != nil || unpublished.Status != store.StatusDraft {
		t.Fatalf("unpublish = %#v, %v", unpublished, err)
	}
	if _, err := application.Local().Update(ctx, "islands", island.ID, store.Values{"title": store.String("Still checked")}, nil); !relationshipIssue(err, "lesson") {
		t.Fatalf("retained draft relationship error = %v", err)
	}
}

func TestReferenceOptionFiltersRevalidateEveryRetainedLocale(t *testing.T) {
	ctx := context.Background()
	application, err := ridu.New(ridu.Config{
		Name: "Localized reference option filter admission",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{Slug: "people", Fields: field.Fields{field.Text("category")}},
			{Slug: "entries", Fields: field.Fields{field.Text("category"), field.Relationship("editor", "people").Localized().FilterOptionRules(field.OptionFilter("category", field.FilterEquals, "category"))}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	person, err := application.Local().Create(ctx, "people", store.Values{"category": store.String("article")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("article"), "editor": store.String(person.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, err = application.Local().Update(ctx, "entries", entry.ID, store.Values{"editor": store.String(person.ID)}, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.Local().Update(ctx, "entries", entry.ID, store.Values{"category": store.String("video")}, nil, ridu.LocaleOptions{Locale: "en"})
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) {
		t.Fatalf("shared source update error = %v", err)
	}
	want := map[string]bool{"editor.en": false, "editor.fr": false}
	for _, issue := range operationError.Issues {
		if issue.Code == "invalid_relationship" {
			if _, expected := want[issue.Path]; expected {
				want[issue.Path] = true
			}
		}
	}
	for path, observed := range want {
		if !observed {
			t.Fatalf("shared source update did not revalidate retained %s: %#v", path, operationError.Issues)
		}
	}
}

func TestReferenceAdmissionChecksFallbackSourcedLocales(t *testing.T) {
	ctx := context.Background()
	denyFrenchAccess := false
	application, err := ridu.New(ridu.Config{
		Name: "Fallback-sourced reference admission",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{
				Slug: "people", Fields: field.Fields{field.Text("name")},
				Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if denyFrenchAccess && ctx.Locale == "fr" {
						return ridu.Deny(), nil
					}
					return ridu.Allow(), nil
				}},
			},
			{Slug: "categories", Fields: field.Fields{field.Text("category").Localized()}},
			{Slug: "entries", Fields: field.Fields{field.Text("category").Localized(), field.Text("marker"), field.Relationship("accessEditor", "people").Localized(), field.Relationship("filteredEditor", "categories").Localized().FilterOptionRules(field.OptionFilter("category", field.FilterEquals, "category"))}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	person, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("Editor")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	matching, err := application.Local().Create(ctx, "categories", store.Values{"category": store.String("article")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	matching, err = application.Local().Update(ctx, "categories", matching.ID, store.Values{"category": store.String("article")}, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("article"), "marker": store.String("initial"),
		"accessEditor": store.String(person.ID), "filteredEditor": store.String(matching.ID),
	}, nil)
	if err != nil {
		t.Fatalf("valid fallback-sourced references: %v", err)
	}

	denyFrenchAccess = true
	_, err = application.Local().Update(ctx, "entries", entry.ID, store.Values{"marker": store.String("changed")}, nil)
	assertLocalizedReferenceIssue(t, err, "accessEditor.fr", "accessEditor.en")
	denyFrenchAccess = false

	if _, err := application.Local().Update(ctx, "categories", matching.ID, store.Values{"category": store.String("news")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "categories", matching.ID, store.Values{"category": store.String("video")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	_, err = application.Local().Update(ctx, "entries", entry.ID, store.Values{"category": store.String("news")}, nil)
	assertLocalizedReferenceIssue(t, err, "filteredEditor.fr", "filteredEditor.en")

	denyFrenchAccess = true
	_, err = application.Local().Create(ctx, "entries", store.Values{"accessEditor": store.String(person.ID)}, nil)
	assertLocalizedReferenceIssue(t, err, "accessEditor.fr", "accessEditor.en")
	denyFrenchAccess = false

	mismatching, err := application.Local().Create(ctx, "categories", store.Values{"category": store.String("article")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "categories", mismatching.ID, store.Values{"category": store.String("video")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	_, err = application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("article"), "filteredEditor": store.String(mismatching.ID),
	}, nil)
	assertLocalizedReferenceIssue(t, err, "filteredEditor.fr", "filteredEditor.en")
}

func assertLocalizedReferenceIssue(t *testing.T, err error, want, unwanted string) {
	t.Helper()
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) {
		t.Fatalf("reference admission error = %v", err)
	}
	found := false
	for _, issue := range operationError.Issues {
		if issue.Code != "invalid_relationship" {
			continue
		}
		if issue.Path == want {
			found = true
		}
		if issue.Path == unwanted {
			t.Fatalf("fallback failure was attributed to source locale %q: %#v", unwanted, operationError.Issues)
		}
	}
	if !found {
		t.Fatalf("missing fallback-locale reference issue %q: %#v", want, operationError.Issues)
	}
}

func TestReferenceOptionFilterOperatorsAndCacheIdentity(t *testing.T) {
	ctx := context.Background()
	application, err := ridu.New(ridu.Config{
		Name: "Reference option filter operators",
		Collections: []ridu.Collection{
			{Slug: "targets", Fields: field.Fields{field.Text("label"), field.Number("score")}},
			{Slug: "entries", Fields: field.Fields{field.Text("likeNeedle"), field.Text("containsNeedle"), field.Text("notLabel"), field.Number("gtThreshold"), field.Number("gteThreshold"), field.Number("ltThreshold"), field.Number("lteThreshold"), field.Text("lexicalThreshold"), field.Text("allowedLabel"), field.Text("deniedLabel"), field.Relationship("likeRef", "targets").FilterOptionRules(field.OptionFilter("label", field.FilterLike, "likeNeedle")), field.Relationship("containsRef", "targets").FilterOptionRules(field.OptionFilter("label", field.FilterContains, "containsNeedle")), field.Relationship("notEqualRef", "targets").FilterOptionRules(field.OptionFilter("label", field.FilterNotEquals, "notLabel")), field.Relationship("greaterThanRef", "targets").FilterOptionRules(field.OptionFilter("score", field.FilterGreaterThan, "gtThreshold")), field.Relationship("greaterThanEqualRef", "targets").FilterOptionRules(field.OptionFilter("score", field.FilterGreaterThanEqual, "gteThreshold")), field.Relationship("lessThanRef", "targets").FilterOptionRules(field.OptionFilter("score", field.FilterLessThan, "ltThreshold")), field.Relationship("lessThanEqualRef", "targets").FilterOptionRules(field.OptionFilter("score", field.FilterLessThanEqual, "lteThreshold")), field.Relationship("orderedTextRef", "targets").FilterOptionRules(field.OptionFilter("label", field.FilterGreaterThan, "lexicalThreshold")), field.Relationship("allowedRef", "targets").FilterOptionRules(field.OptionFilter("label", field.FilterEquals, "allowedLabel")), field.Relationship("deniedRef", "targets").FilterOptionRules(field.OptionFilter("label", field.FilterEquals, "deniedLabel"))}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(ctx, "targets", store.Values{
		"label": store.String("Ångström Alpha Beta"), "score": store.Number(10),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := store.Values{
		"likeNeedle": store.String("alpha beta"), "containsNeedle": store.String("pha"), "notLabel": store.String("Other"),
		"gtThreshold": store.Number(9), "gteThreshold": store.Number(10), "ltThreshold": store.Number(11), "lteThreshold": store.Number(10), "lexicalThreshold": store.String("Z"),
		"likeRef": store.String(target.ID), "containsRef": store.String(target.ID), "notEqualRef": store.String(target.ID),
		"greaterThanRef": store.String(target.ID), "greaterThanEqualRef": store.String(target.ID),
		"lessThanRef": store.String(target.ID), "lessThanEqualRef": store.String(target.ID),
		"orderedTextRef": store.String(target.ID),
	}
	entry, err := application.Local().Create(ctx, "entries", base, nil)
	if err != nil {
		t.Fatalf("valid option-filter operators: %v", err)
	}
	for _, test := range []struct {
		name    string
		field   string
		value   store.Value
		refPath string
	}{
		{name: "like", field: "likeNeedle", value: store.String("alpha gamma"), refPath: "likeRef"},
		{name: "contains", field: "containsNeedle", value: store.String("zzz"), refPath: "containsRef"},
		{name: "not equals", field: "notLabel", value: store.String("Ångström Alpha Beta"), refPath: "notEqualRef"},
		{name: "greater than", field: "gtThreshold", value: store.Number(10), refPath: "greaterThanRef"},
		{name: "greater than equal", field: "gteThreshold", value: store.Number(11), refPath: "greaterThanEqualRef"},
		{name: "less than", field: "ltThreshold", value: store.Number(10), refPath: "lessThanRef"},
		{name: "less than equal", field: "lteThreshold", value: store.Number(9), refPath: "lessThanEqualRef"},
		{name: "ordered Unicode text", field: "lexicalThreshold", value: store.String("🧭"), refPath: "orderedTextRef"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := application.Local().Update(ctx, "entries", entry.ID, store.Values{test.field: test.value}, nil); !relationshipIssue(err, test.refPath) {
				t.Fatalf("operator-filtered update error = %v", err)
			}
		})
	}

	_, err = application.Local().Create(ctx, "entries", store.Values{
		"allowedLabel": store.String("Ångström Alpha Beta"), "deniedLabel": store.String("Other"),
		"allowedRef": store.String(target.ID), "deniedRef": store.String(target.ID),
	}, nil)
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || !relationshipIssue(err, "deniedRef") {
		t.Fatalf("field-specific filter cache error = %v", err)
	}
	for _, issue := range operationError.Issues {
		if issue.Path == "allowedRef" {
			t.Fatalf("filter cache reused the denied predicate for an allowed field: %#v", operationError.Issues)
		}
	}
}

func TestReferenceOptionFiltersGuardDuplicateStatusVersionAndTrashRestore(t *testing.T) {
	ctx := context.Background()
	application, err := ridu.New(ridu.Config{
		Name: "Reference option filter lifecycle",
		Collections: []ridu.Collection{
			{Slug: "people", Fields: field.Fields{field.Text("category")}},
			{
				Slug: "entries", Versions: true, Trash: true,
				Fields: field.Fields{field.Text("category"), field.Relationship("author", "people").FilterOptionRules(field.OptionFilter("category", field.FilterEquals, "category"))},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	person, err := application.Local().Create(ctx, "people", store.Values{"category": store.String("article")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := application.Local().Create(ctx, "entries", store.Values{
		"category": store.String("article"), "author": store.String(person.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	articleRevision := entry.Revision
	if _, err := application.Local().Update(ctx, "people", person.ID, store.Values{"category": store.String("news")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Duplicate(ctx, "entries", entry.ID, nil, nil); !relationshipIssue(err, "author") {
		t.Fatalf("duplicate option-filter error = %v", err)
	}
	if _, err := application.Local().Publish(ctx, "entries", entry.ID, entry.Revision, nil); !relationshipIssue(err, "author") {
		t.Fatalf("publish option-filter error = %v", err)
	}
	entry, err = application.Local().PublishChanges(ctx, "entries", entry.ID, store.Values{"category": store.String("news")}, entry.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Restore(ctx, "entries", entry.ID, articleRevision, entry.Revision, nil); !relationshipIssue(err, "author") {
		t.Fatalf("version restore option-filter error = %v", err)
	}
	entry, err = application.Local().Publish(ctx, "entries", entry.ID, entry.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "people", person.ID, store.Values{"category": store.String("article")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Unpublish(ctx, "entries", entry.ID, entry.Revision, nil); !relationshipIssue(err, "author") {
		t.Fatalf("unpublish option-filter error = %v", err)
	}
	if _, err := application.Local().Delete(ctx, "entries", entry.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().RestoreDeleted(ctx, "entries", entry.ID, nil); !relationshipIssue(err, "author") {
		t.Fatalf("trash restore option-filter error = %v", err)
	}
}
