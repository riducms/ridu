package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresMigrationsAndStoreConformance(t *testing.T) {
	ctx := context.Background()
	backend, manifest := integrationBackend(t, ctx, integrationConfig())
	directory := t.TempDir()

	artifact, err := postgres.BuildArtifact(ctx, "initial-schema", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Phases) == 0 {
		t.Fatal("initial migration plan is empty")
	}
	created, err := migrationartifact.Create(directory, "initial-schema", artifact, time.Unix(1, 0))
	if err != nil || !strings.HasSuffix(created.Path, ".ridu.json") {
		t.Fatalf("migration = %#v, %v", created, err)
	}
	status, err := backend.ArtifactStatus(ctx, directory)
	if err != nil || len(status) != 1 || status[0].Applied {
		t.Fatalf("pending status = %#v, %v", status, err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	status, err = backend.ArtifactStatus(ctx, directory)
	if err != nil || !status[0].Applied {
		t.Fatalf("applied status = %#v, %v", status, err)
	}
	current, err := backend.Plan(ctx, manifest)
	if err != nil || len(current) != 0 {
		t.Fatalf("post-migration plan = %#v, %v", current, err)
	}

	application, err := ridu.New(integrationConfig(), backend)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("manager@example.test")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	author, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("ada@example.test"), "manager": store.String(manager.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(ctx, "users", author.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(ctx, "users", "ada@example.test", "correct-horse")
	if err != nil || session.User.ID != author.ID {
		t.Fatalf("PostgreSQL login session = %#v, %v", session, err)
	}
	currentSession, err := application.Session(ctx, session.Token)
	if err != nil || currentSession.User.ID != author.ID {
		t.Fatalf("PostgreSQL current session = %#v, %v", currentSession, err)
	}
	if err := application.Logout(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Session(ctx, session.Token); !hasOperationCode(err, "access_denied") {
		t.Fatalf("logged-out PostgreSQL session error = %v", err)
	}
	if _, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("ada@example.test")}, nil); !hasOperationCode(err, "conflict") {
		t.Fatalf("duplicate unique value error = %v", err)
	}
	if _, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("invalid relation"), "status": store.String("draft"), "author": store.String("missing"),
	}, nil); !hasRelationshipIssue(err, "author") {
		t.Fatalf("missing PostgreSQL relationship error = %v", err)
	}
	private, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("private"), "status": store.String("draft"), "author": store.String(author.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	public, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("public"), "status": store.String("published"), "author": store.String(author.ID),
		"summary": store.String("PostgreSQL recursive field contract"), "contact": store.String("editor@example.test"),
		"publishedOn": store.String("2026-08-04"), "score": store.Number(9.5), "featured": store.Boolean(true),
		"metadata": store.Object(store.Values{"source": store.String("integration")}),
		"seo":      store.Object(store.Values{"description": store.String("Projected copy")}),
		"tags":     store.List(store.Object(store.Values{"_key": store.String("tag-1"), "label": store.String("Go")})),
		"layout":   store.List(store.Object(store.Values{"_key": store.String("block-1"), "blockType": store.String("quote"), "quote": store.String("One binary")})),
		"content":  richTextDocument("Portable rich text"),
		"watchers": store.List(store.String(author.ID)),
		"related":  store.Object(store.Values{"relationTo": store.String("users"), "id": store.String(author.ID)}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	watchersPath, _ := query.NewPath("watchers")
	relatedPath, _ := query.NewPath("related")
	page, err := application.Local().List(ctx, "posts", ridu.ListOptions{Page: 1, Limit: 20, Populate: []query.Population{{Path: watchersPath}, {Path: relatedPath, Depth: 2}}})
	if err != nil || page.Total != 1 || page.Documents[0].ID != public.ID {
		t.Fatalf("access-filtered page = %#v, %v", page, err)
	}
	if score, valid := page.Documents[0].Values["score"].NumberValue(); !valid || score != 9.5 {
		t.Fatalf("number field round trip = %#v", page.Documents[0].Values["score"])
	}
	if content, valid := page.Documents[0].Values["content"].ObjectValue(); !valid || content["root"].Kind() != store.ValueObject {
		t.Fatalf("rich-text JSONB round trip = %#v", page.Documents[0].Values["content"])
	}
	watchers, _ := page.Documents[0].Values["watchers"].Values()
	if watcher, populated := watchers[0].DocumentValue(); !populated || watcher.ID != author.ID {
		t.Fatalf("has-many PostgreSQL population = %#v", watchers)
	}
	related, _ := page.Documents[0].Values["related"].ObjectValue()
	if relatedUser, populated := related["id"].DocumentValue(); !populated || relatedUser.ID != author.ID {
		t.Fatalf("polymorphic PostgreSQL population = %#v", related)
	} else if populatedManager, populated := relatedUser.Values["manager"].DocumentValue(); !populated || populatedManager.ID != manager.ID {
		t.Fatalf("depth-2 PostgreSQL population = %#v", relatedUser.Values["manager"])
	}
	if _, err := application.Local().Find(ctx, "posts", private.ID, nil); !hasOperationCode(err, "not_found") {
		t.Fatalf("access-filtered find error = %v", err)
	}
	if _, err := application.Local().Update(ctx, "posts", private.ID, store.Values{"title": store.String("changed")}, nil); !hasOperationCode(err, "not_found") {
		t.Fatalf("access-filtered update error = %v", err)
	}
	if _, err := application.Local().Delete(ctx, "posts", private.ID, nil); !hasOperationCode(err, "not_found") {
		t.Fatalf("access-filtered delete error = %v", err)
	}
}

func TestPostgresExpectedRevisionDoesNotRevealFilteredDocuments(t *testing.T) {
	ctx := context.Background()
	statusPath, err := query.NewPath("status")
	if err != nil {
		t.Fatal(err)
	}
	config := ridu.Config{Name: "PostgreSQL filtered optimistic concurrency", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: []field.Definition{
			field.Text("title", field.Required()),
			field.Select("status", field.Required(), field.Choices(
				field.Choice{Value: "draft", Label: "Draft"},
				field.Choice{Value: "published", Label: "Published"},
			)),
		},
		Access: ridu.CollectionAccess{Update: func(ridu.AccessContext) (ridu.AccessDecision, error) {
			return ridu.Where(query.Equal(statusPath, query.String("published"))), nil
		}},
	}}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Hidden"), "status": store.String("draft"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().UpdateWithOptions(ctx, "posts", hidden.ID, store.Values{
		"title": store.String("Probe"),
	}, ridu.MutationOptions{ExpectedRevision: hidden.Revision}); !hasOperationCode(err, "not_found") {
		t.Fatalf("filtered exact-revision mutation error = %v, want not_found", err)
	}

	visible, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Visible"), "status": store.String("published"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := application.Local().UpdateWithOptions(ctx, "posts", visible.ID, store.Values{
		"title": store.String("Fresh"),
	}, ridu.MutationOptions{ExpectedRevision: visible.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().UpdateWithOptions(ctx, "posts", visible.ID, store.Values{
		"title": store.String("Stale"),
	}, ridu.MutationOptions{ExpectedRevision: visible.Revision}); !hasOperationCode(err, "conflict") {
		t.Fatalf("visible stale-revision mutation error = %v, want conflict (current revision %d)", err, updated.Revision)
	}
}

func TestPostgresGlobalFilteredAccessUsesRowsAndVersionSnapshots(t *testing.T) {
	ctx := context.Background()
	siteName, err := query.NewPath("siteName")
	if err != nil {
		t.Fatal(err)
	}
	filtered := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(siteName, query.String("Ridu"))), nil
	}
	allowInitialization := false
	config := ridu.Config{
		Name:        "PostgreSQL filtered global access",
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Versions: true,
			Fields: []field.Definition{field.Text("siteName", field.Required())},
			Access: ridu.GlobalAccess{
				Read:         filtered,
				ReadVersions: filtered,
				Update: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if allowInitialization {
						return ridu.Allow(), nil
					}
					return filtered(ctx)
				},
			},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().UpdateGlobal(ctx, "site-settings", store.Values{"siteName": store.String("Ridu")}, 0, nil); !hasOperationCode(err, "not_found") {
		t.Fatalf("filtered PostgreSQL first update error = %v", err)
	}
	allowInitialization = true
	created, err := application.Local().UpdateGlobal(ctx, "site-settings", store.Values{"siteName": store.String("Ridu")}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	allowInitialization = false
	if current, err := application.Local().Global(ctx, "site-settings", nil); err != nil || stringValue(current.Values["siteName"]) != "Ridu" {
		t.Fatalf("matching PostgreSQL global = %#v, %v", current, err)
	}
	changed, err := application.Local().PublishGlobalChanges(ctx, "site-settings", store.Values{"siteName": store.String("Hidden")}, created.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Global(ctx, "site-settings", nil); !hasOperationCode(err, "not_found") {
		t.Fatalf("non-matching PostgreSQL global read error = %v", err)
	}
	if _, err := application.Local().PublishGlobalChanges(ctx, "site-settings", store.Values{"siteName": store.String("Ridu")}, changed.Revision, nil); !hasOperationCode(err, "not_found") {
		t.Fatalf("non-matching PostgreSQL global update error = %v", err)
	}
	versions, err := application.Local().GlobalVersions(ctx, "site-settings", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Revision != 1 || stringValue(versions[0].Snapshot.Values["siteName"]) != "Ridu" {
		t.Fatalf("filtered PostgreSQL global versions = %#v", versions)
	}
	if _, err := application.Local().GlobalVersion(ctx, "site-settings", 2, nil); !hasOperationCode(err, "not_found") {
		t.Fatalf("non-matching PostgreSQL global version error = %v", err)
	}
}

func TestPostgresGlobalAllLocalesAccessRequiresEveryLocalizedSnapshot(t *testing.T) {
	ctx := context.Background()
	title, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	audience, err := query.NewPath("audiences", "name")
	if err != nil {
		t.Fatal(err)
	}
	predicate, err := query.And(
		query.Equal(title, query.String("Public")),
		query.Equal(audience, query.String("Public")),
	)
	if err != nil {
		t.Fatal(err)
	}
	filtered := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(predicate), nil
	}
	config := ridu.Config{
		Name: "PostgreSQL all-locales global access",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Versions: true,
			Fields: []field.Definition{
				field.Text("title", field.Required(), field.Localized()),
				field.Array("audiences", field.Localized(), field.Fields(field.Text("name", field.Required()))),
			},
			Access: ridu.GlobalAccess{Read: filtered, ReadVersions: filtered},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	values := func(titleValue, audienceValue string) store.Values {
		return store.Values{
			"title":     store.String(titleValue),
			"audiences": store.List(store.Object(store.Values{"name": store.String(audienceValue)})),
		}
	}
	current, err := application.Local().UpdateGlobal(ctx, "site-settings", values("Public", "Public"), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err = application.Local().PublishGlobalChanges(ctx, "site-settings", values("Public", "Public"), current.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	publicRevision := current.Revision
	if _, err := application.Local().PublishGlobalChanges(ctx, "site-settings", values("Private", "Private"), current.Revision, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Global(ctx, "site-settings", nil, ridu.LocaleOptions{Locale: "en"}); err != nil {
		t.Fatalf("English PostgreSQL global read: %v", err)
	}
	if _, err := application.Local().Global(ctx, "site-settings", nil, ridu.LocaleOptions{Locale: "fr"}); !hasOperationCode(err, "not_found") {
		t.Fatalf("French PostgreSQL global read error = %v", err)
	}
	if _, err := application.Local().Global(ctx, "site-settings", nil, ridu.LocaleOptions{AllLocales: true}); !hasOperationCode(err, "not_found") {
		t.Fatalf("all-locales PostgreSQL global read error = %v", err)
	}
	versions, err := application.Local().GlobalVersions(ctx, "site-settings", nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Revision != publicRevision {
		t.Fatalf("all-locales PostgreSQL global versions = %#v", versions)
	}
}

func TestPostgresAllLocalesPopulationRequiresTargetAccessForEveryLocale(t *testing.T) {
	ctx := context.Background()
	name, err := query.NewPath("name")
	if err != nil {
		t.Fatal(err)
	}
	config := ridu.Config{
		Name: "PostgreSQL all-locales population access",
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
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	person, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("Public")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Story"), "editor": store.String(person.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Admission checks every effective locale for a non-localized reference on
	// create. Establish the valid relationship first, then make the target
	// unreadable in French to prove later population remains fail closed as
	// target content and access predicates evolve.
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
		t.Fatalf("English PostgreSQL populated target = %#v", english.Values["editor"])
	}
	all, err := application.Local().FindWithOptions(ctx, "posts", post.ID, ridu.FindOptions{
		AllLocales: true, Populate: []query.Population{{Path: editor}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, populated := all.Values["editor"].DocumentValue(); populated || stringValue(all.Values["editor"]) != person.ID {
		t.Fatalf("all-locales PostgreSQL population leaked target = %#v", all.Values["editor"])
	}
}

func TestPostgresAllLocalesStatusHooksReceiveLocaleProjectedPopulatedTargets(t *testing.T) {
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
	ctx := context.Background()
	config := ridu.Config{
		Name: "PostgreSQL all-locales populated status hooks",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{Slug: "people", Fields: []field.Definition{field.Text("name", field.Required(), field.Localized())}},
			{
				Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields: []field.Definition{
					field.Text("title", field.Required()),
					field.Relationship("editor", field.To("people"), field.Required()),
				},
				Hooks: ridu.CollectionHooks{
					AfterChange:    []ridu.Hook{record("afterChange")},
					AfterOperation: []ridu.Hook{record("afterOperation")},
					AfterCommit:    []ridu.Hook{record("afterCommit")},
				},
			},
		},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	person, err := application.Local().Create(ctx, "people", store.Values{"name": store.String("Editor")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "people", person.ID, store.Values{
		"name": store.String("Éditrice"),
	}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Story"), "editor": store.String(person.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	editorPath, err := query.NewPath("editor")
	if err != nil {
		t.Fatal(err)
	}
	published, err := application.Local().PublishWithOptions(ctx, "posts", post.ID, ridu.MutationOptions{
		ExpectedRevision: post.Revision, AllLocales: true,
		Populate: []query.Population{{Path: editorPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	populated, valid := published.Values["editor"].DocumentValue()
	if !valid {
		t.Fatalf("all-locales PostgreSQL response editor = %#v", published.Values["editor"])
	}
	if _, valid := populated.Values["name"].ObjectValue(); !valid {
		t.Fatalf("all-locales PostgreSQL response target name = %#v, want locale object", populated.Values["name"])
	}
	wantPhases := []string{"afterChange", "afterOperation", "afterCommit"}
	if len(observations) != len(wantPhases) {
		t.Fatalf("PostgreSQL hook observations = %#v", observations)
	}
	for index, observed := range observations {
		if observed.phase != wantPhases[index] || observed.locale != "en" || observed.allLocales || observed.name != "Editor" || observed.nameObject {
			t.Fatalf("PostgreSQL hook observation %d = %#v", index, observed)
		}
	}
}

func TestPostgresLocalizedIntermediateContainerPredicatesMatchProjectedDocuments(t *testing.T) {
	ctx := context.Background()
	detailsName, err := query.NewPath("content", "details", "name")
	if err != nil {
		t.Fatal(err)
	}
	rowLabel, err := query.NewPath("content", "rows", "label")
	if err != nil {
		t.Fatal(err)
	}
	heading, err := query.NewPath("content", "layout", "hero", "heading")
	if err != nil {
		t.Fatal(err)
	}
	visible, err := query.And(
		query.Equal(detailsName, query.String("visible")),
		query.Equal(rowLabel, query.String("visible")),
		query.Equal(heading, query.String("visible")),
	)
	if err != nil {
		t.Fatal(err)
	}
	filtered := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(visible), nil
	}
	config := ridu.Config{
		Name: "PostgreSQL localized intermediate predicates",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug: "pages", Versions: true,
			Fields: []field.Definition{field.Group("content", field.Fields(
				field.Group("details", field.Localized(), field.Fields(field.Text("name", field.Required()))),
				field.Array("rows", field.Localized(), field.Fields(field.Text("label", field.Required()))),
				field.Blocks("layout", field.Localized(), field.BlockTypes(
					field.BlockType("hero", "Hero", field.Text("heading", field.Required())),
				)),
			))},
			Access: ridu.CollectionAccess{Read: filtered, ReadVersions: filtered},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	values := func(value string) store.Values {
		return store.Values{"content": store.Object(store.Values{
			"details": store.Object(store.Values{"name": store.String(value)}),
			"rows": store.List(store.Object(store.Values{
				"_key": store.String("row-1"), "label": store.String(value),
			})),
			"layout": store.List(store.Object(store.Values{
				"_key": store.String("block-1"), "blockType": store.String("hero"), "heading": store.String(value),
			})),
		})}
	}
	document, err := application.Local().Create(ctx, "pages", values("visible"), nil)
	if err != nil {
		t.Fatal(err)
	}
	document, err = application.Local().PublishChanges(ctx, "pages", document.ID, values("visible"), document.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	publicRevision := document.Revision
	for _, path := range []query.Path{detailsName, rowLabel, heading} {
		page, err := application.Local().List(ctx, "pages", ridu.ListOptions{
			Locale: "fr", Where: query.Equal(path, query.String("visible")),
		})
		if err != nil || page.Total != 1 || page.Documents[0].ID != document.ID {
			t.Fatalf("French predicate %s = %#v, %v", path.String(), page, err)
		}
	}
	if _, err := application.Local().Find(ctx, "pages", document.ID, nil, ridu.LocaleOptions{AllLocales: true}); err != nil {
		t.Fatalf("matching all-locales access: %v", err)
	}
	if _, err := application.Local().PublishChanges(ctx, "pages", document.ID, values("hidden"), document.Revision, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Find(ctx, "pages", document.ID, nil, ridu.LocaleOptions{Locale: "en"}); err != nil {
		t.Fatalf("English exact access: %v", err)
	}
	if _, err := application.Local().Find(ctx, "pages", document.ID, nil, ridu.LocaleOptions{Locale: "fr"}); !hasOperationCode(err, "not_found") {
		t.Fatalf("French exact access error = %v", err)
	}
	if _, err := application.Local().Find(ctx, "pages", document.ID, nil, ridu.LocaleOptions{AllLocales: true}); !hasOperationCode(err, "not_found") {
		t.Fatalf("all-locales access error = %v", err)
	}
	versions, err := application.Local().Versions(ctx, "pages", document.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Revision != publicRevision {
		t.Fatalf("all-locales version predicates = %#v, want revision %d", versions, publicRevision)
	}
}

func TestPostgresExactLocaleAccessPreservesEmptyLocalizedScalars(t *testing.T) {
	ctx := context.Background()
	gate, err := query.NewPath("gate")
	if err != nil {
		t.Fatal(err)
	}
	nullGate := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(gate, query.Null())), nil
	}
	config := ridu.Config{
		Name: "PostgreSQL exact empty localized access",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug: "pages", Versions: true,
			Fields: []field.Definition{field.Text("gate", field.Localized())},
			Access: ridu.CollectionAccess{Read: nullGate, ReadVersions: nullGate},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(ctx, "pages", store.Values{"gate": store.String("")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().PublishChanges(ctx, "pages", document.ID, store.Values{"gate": store.String("")}, document.Revision, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	for _, options := range []ridu.LocaleOptions{{Locale: "en"}, {Locale: "fr"}, {AllLocales: true}} {
		if _, err := application.Local().Find(ctx, "pages", document.ID, nil, options); !hasOperationCode(err, "not_found") {
			t.Fatalf("empty localized value was treated as null for options %#v: %v", options, err)
		}
	}
	page, err := application.Local().List(ctx, "pages", ridu.ListOptions{Locale: "en"})
	if err != nil || page.Total != 0 {
		t.Fatalf("empty localized access list = %#v, %v", page, err)
	}
	versions, err := application.Local().Versions(ctx, "pages", document.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil || len(versions) != 0 {
		t.Fatalf("empty localized version access = %#v, %v", versions, err)
	}
}

func TestPostgresRepeatedNullPredicatesMatchProjectedAccessSemantics(t *testing.T) {
	ctx := context.Background()
	title, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	rowsLabel, err := query.NewPath("rows", "label")
	if err != nil {
		t.Fatal(err)
	}
	heading, err := query.NewPath("layout", "hero", "heading")
	if err != nil {
		t.Fatal(err)
	}
	localizedLabel, err := query.NewPath("localizedRows", "label")
	if err != nil {
		t.Fatal(err)
	}
	current := query.Equal(title, query.String("never"))
	filtered := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(current), nil
	}
	config := ridu.Config{
		Name: "PostgreSQL repeated null predicates",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug: "pages", Versions: true,
			Fields: []field.Definition{
				field.Text("title", field.Required()),
				field.Array("rows", field.Fields(field.Text("label"))),
				field.Blocks("layout", field.BlockTypes(field.BlockType("hero", "Hero", field.Text("heading")))),
				field.Array("localizedRows", field.Localized(), field.Fields(field.Text("label"))),
			},
			Access: ridu.CollectionAccess{Read: filtered, ReadVersions: filtered},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	values := func(kind string) store.Values {
		result := store.Values{"title": store.String(kind)}
		switch kind {
		case "empty":
			result["rows"] = store.List()
			result["layout"] = store.List()
			result["localizedRows"] = store.List()
		case "mixed":
			result["rows"] = store.List(
				store.Object(store.Values{"_key": store.String("row-visible"), "label": store.String("visible")}),
				store.Object(store.Values{"_key": store.String("row-missing")}),
			)
			result["layout"] = store.List(
				store.Object(store.Values{"_key": store.String("block-visible"), "blockType": store.String("hero"), "heading": store.String("visible")}),
				store.Object(store.Values{"_key": store.String("block-missing"), "blockType": store.String("hero")}),
			)
			result["localizedRows"] = store.List(
				store.Object(store.Values{"_key": store.String("localized-visible"), "label": store.String("visible")}),
				store.Object(store.Values{"_key": store.String("localized-missing")}),
			)
		case "null":
			result["rows"] = store.List(store.Object(store.Values{"_key": store.String("row-null"), "label": store.Null()}))
			result["layout"] = store.List(store.Object(store.Values{"_key": store.String("block-null"), "blockType": store.String("hero"), "heading": store.Null()}))
			result["localizedRows"] = store.List(store.Object(store.Values{"_key": store.String("localized-null"), "label": store.Null()}))
		case "other":
			result["rows"] = store.List(store.Object(store.Values{"_key": store.String("row-other"), "label": store.String("other")}))
			result["layout"] = store.List(store.Object(store.Values{"_key": store.String("block-other"), "blockType": store.String("hero"), "heading": store.String("other")}))
			result["localizedRows"] = store.List(store.Object(store.Values{"_key": store.String("localized-other"), "label": store.String("other")}))
		}
		return result
	}
	documents := make(map[string]store.Document)
	for _, kind := range []string{"absent", "empty", "mixed", "null", "other"} {
		document, err := application.Local().Create(ctx, "pages", values(kind), nil)
		if err != nil {
			t.Fatalf("create %s: %v", kind, err)
		}
		document, err = application.Local().PublishChanges(ctx, "pages", document.ID, values(kind), document.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
		if err != nil {
			t.Fatalf("update %s French locale: %v", kind, err)
		}
		documents[kind] = document
	}
	assertList := func(label string, expression query.Expression, options ridu.ListOptions, want ...string) {
		t.Helper()
		current = expression
		page, err := application.Local().List(ctx, "pages", options)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		remaining := make(map[string]bool, len(want))
		for _, kind := range want {
			remaining[documents[kind].ID] = true
		}
		for _, document := range page.Documents {
			if !remaining[document.ID] {
				t.Fatalf("%s returned unexpected document %s: %#v", label, document.ID, page.Documents)
			}
			delete(remaining, document.ID)
		}
		if page.Total != len(want) || len(remaining) != 0 {
			t.Fatalf("%s returned %#v; missing IDs %#v", label, page.Documents, remaining)
		}
	}
	nullRows := query.In(rowsLabel, query.Null())
	mixedRows := query.In(rowsLabel, query.Null(), query.String("visible"))
	notNullRows, err := query.Not(nullRows)
	if err != nil {
		t.Fatal(err)
	}
	assertList("array null-only IN", nullRows, ridu.ListOptions{}, "absent", "empty", "null")
	assertList("array mixed null IN", mixedRows, ridu.ListOptions{}, "absent", "empty", "mixed", "null")
	assertList("array outer NOT IN", notNullRows, ridu.ListOptions{}, "mixed", "other")
	assertList("array equal null", query.Equal(rowsLabel, query.Null()), ridu.ListOptions{}, "absent", "empty", "null")
	assertList("array not-equal null", query.NotEqual(rowsLabel, query.Null()), ridu.ListOptions{}, "mixed", "other")
	assertList("block null-only IN", query.In(heading, query.Null()), ridu.ListOptions{}, "absent", "empty", "null")
	assertList("localized array exact null-only IN", query.In(localizedLabel, query.Null()), ridu.ListOptions{Locale: "fr"}, "absent", "empty", "null")
	assertList("localized array all-locales null-only IN", query.In(localizedLabel, query.Null()), ridu.ListOptions{AllLocales: true}, "absent", "empty", "null")

	current = nullRows
	for _, kind := range []string{"absent", "empty", "null"} {
		versions, err := application.Local().Versions(ctx, "pages", documents[kind].ID, nil)
		if err != nil || len(versions) != 2 {
			t.Fatalf("null-only versions for %s = %#v, %v", kind, versions, err)
		}
	}
	if versions, err := application.Local().Versions(ctx, "pages", documents["mixed"].ID, nil); err != nil || len(versions) != 0 {
		t.Fatalf("mixed missing-child versions = %#v, %v", versions, err)
	}
	current = notNullRows
	if versions, err := application.Local().Versions(ctx, "pages", documents["mixed"].ID, nil); err != nil || len(versions) != 2 {
		t.Fatalf("outer-NOT versions = %#v, %v", versions, err)
	}
}

func TestPostgresJSONStringPredicatesRemainTypeAwareUnderNot(t *testing.T) {
	ctx := context.Background()
	payload, err := query.NewPath("payload")
	if err != nil {
		t.Fatal(err)
	}
	current := query.Like(payload, "never")
	filtered := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(current), nil
	}
	config := ridu.Config{
		Name: "PostgreSQL JSON string predicates",
		Collections: []ridu.Collection{{
			Slug: "pages", Versions: true,
			Fields: []field.Definition{field.Text("title", field.Required()), field.JSON("payload")},
			Access: ridu.CollectionAccess{Read: filtered, ReadVersions: filtered},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]store.Value{
		"empty":  store.String(""),
		"string": store.String("visible"),
		"object": store.Object(store.Values{"text": store.String("visible")}),
		"number": store.Number(9),
		"null":   store.Null(),
	}
	documents := make(map[string]store.Document, len(inputs)+1)
	absent, err := application.Local().Create(ctx, "pages", store.Values{"title": store.String("absent")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	documents["absent"] = absent
	for kind, value := range inputs {
		document, err := application.Local().Create(ctx, "pages", store.Values{"title": store.String(kind), "payload": value}, nil)
		if err != nil {
			t.Fatalf("create %s JSON value: %v", kind, err)
		}
		documents[kind] = document
	}
	assertList := func(label string, expression query.Expression, want ...string) {
		t.Helper()
		current = expression
		page, err := application.Local().List(ctx, "pages", ridu.ListOptions{})
		if err != nil {
			t.Fatalf("%s: %v (cause: %v)", label, err, errors.Unwrap(err))
		}
		remaining := make(map[string]bool, len(want))
		for _, kind := range want {
			remaining[documents[kind].ID] = true
		}
		for _, document := range page.Documents {
			if !remaining[document.ID] {
				t.Fatalf("%s returned unexpected document %s", label, document.ID)
			}
			delete(remaining, document.ID)
		}
		if page.Total != len(want) || len(remaining) != 0 {
			t.Fatalf("%s returned %#v; missing IDs %#v", label, page.Documents, remaining)
		}
	}
	likeVisible := query.Like(payload, "visible")
	likeEmpty := query.Like(payload, "")
	notVisible, err := query.Not(likeVisible)
	if err != nil {
		t.Fatal(err)
	}
	notEmpty, err := query.Not(likeEmpty)
	if err != nil {
		t.Fatal(err)
	}
	assertList("JSON like", likeVisible, "string")
	assertList("JSON empty like", likeEmpty, "empty", "string")
	assertList("JSON contains", query.Contains(payload, "visible"), "string")
	assertList("JSON not like", notVisible, "absent", "empty", "object", "number", "null")
	assertList("JSON not empty like", notEmpty, "absent", "object", "number", "null")
	assertList("JSON string equality", query.Equal(payload, query.String("visible")), "string")
	assertList("JSON string inequality", query.NotEqual(payload, query.String("visible")), "absent", "empty", "object", "number", "null")
	assertList("JSON string kind mismatch", query.Equal(payload, query.String("9")))
	assertList("JSON number equality", query.Equal(payload, query.Number(9)), "number")
	assertList("JSON null equality", query.Equal(payload, query.Null()), "absent", "null")
	assertList("JSON mixed membership", query.In(payload, query.Null(), query.String("visible"), query.Number(9)), "absent", "string", "number", "null")
	assertList("JSON ordered number", query.GreaterThan(payload, query.Number(5)), "number")
	notMixed, err := query.Not(query.In(payload, query.Null(), query.String("visible")))
	if err != nil {
		t.Fatal(err)
	}
	assertList("JSON outer NOT membership", notMixed, "empty", "object", "number")

	current = notVisible
	if versions, err := application.Local().Versions(ctx, "pages", documents["string"].ID, nil); err != nil || len(versions) != 0 {
		t.Fatalf("matching-string NOT versions = %#v, %v", versions, err)
	}
	for _, kind := range []string{"absent", "object", "number", "null"} {
		versions, err := application.Local().Versions(ctx, "pages", documents[kind].ID, nil)
		if err != nil || len(versions) != 1 {
			t.Fatalf("non-string NOT versions for %s = %#v, %v", kind, versions, err)
		}
	}
}

func TestPostgresLocalizedScalarStorageQueryAndFallback(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{
		Name: "Localized PostgreSQL",
		Localization: ridu.LocalizationConfig{
			DefaultLocale: "en",
			Locales: []ridu.Locale{
				{Code: "en", Label: "English"},
				{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
				{Code: "ar", Label: "Arabic", RTL: true, FallbackLocales: []schema.LocaleCode{"en"}},
			},
		},
		Collections: []ridu.Collection{{Slug: "posts", Versions: true, Fields: []field.Definition{
			field.Text("title", field.Required(), field.Unique(), field.Localized()),
			field.Text("summary", field.Localized()),
			field.Text("slug", field.Required()),
			field.Group("seo", field.Fields(field.Text("description", field.Required(), field.Localized()))),
			field.Group("details", field.Localized(), field.Fields(field.Text("name", field.Required()))),
			field.Array("links", field.Fields(field.Text("label", field.Required(), field.Localized()), field.Text("href", field.Required()))),
			field.Blocks("layout", field.BlockTypes(field.BlockType("hero", "Hero", field.Text("heading", field.Required(), field.Localized())))),
		}}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Hello"), "summary": store.String("English summary"), "slug": store.String("hello"),
		"seo":     store.Object(store.Values{"description": store.String("English description")}),
		"details": store.Object(store.Values{"name": store.String("English details")}),
		"links":   store.List(store.Object(store.Values{"_key": store.String("link-1"), "label": store.String("About"), "href": store.String("/about")})),
		"layout":  store.List(store.Object(store.Values{"_key": store.String("block-1"), "blockType": store.String("hero"), "heading": store.String("Welcome")})),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().PublishChanges(ctx, "posts", document.ID, store.Values{
		"title":   store.String("Bonjour"),
		"seo":     store.Object(store.Values{"description": store.String("Description française")}),
		"details": store.Object(store.Values{"name": store.String("Détails français")}),
		"links":   store.List(store.Object(store.Values{"_key": store.String("link-1"), "label": store.String("À propos"), "href": store.String("/about")})),
		"layout":  store.List(store.Object(store.Values{"_key": store.String("block-1"), "blockType": store.String("hero"), "heading": store.String("Bienvenue")})),
	}, document.Revision, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	english, err := application.Local().Find(ctx, "posts", document.ID, nil, ridu.LocaleOptions{Locale: "en"})
	if err != nil || stringValue(english.Values["title"]) != "Hello" {
		t.Fatalf("English document = %#v, %v", english, err)
	}
	french, err := application.Local().Find(ctx, "posts", document.ID, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil || stringValue(french.Values["title"]) != "Bonjour" {
		t.Fatalf("French document = %#v, %v", french, err)
	}
	if postgresNestedString(french.Values["seo"], "description") != "Description française" || postgresNestedString(french.Values["details"], "name") != "Détails français" || postgresNestedRowString(french.Values["links"], 0, "label") != "À propos" || postgresNestedRowString(french.Values["layout"], 0, "heading") != "Bienvenue" {
		t.Fatalf("French nested document = %#v", french.Values)
	}
	if postgresNestedString(english.Values["seo"], "description") != "English description" || postgresNestedString(english.Values["details"], "name") != "English details" || postgresNestedRowString(english.Values["links"], 0, "label") != "About" || postgresNestedRowString(english.Values["layout"], 0, "heading") != "Welcome" {
		t.Fatalf("English nested document was overwritten = %#v", english.Values)
	}
	arabic, err := application.Local().Find(ctx, "posts", document.ID, nil, ridu.LocaleOptions{Locale: "ar"})
	if err != nil || stringValue(arabic.Values["title"]) != "Hello" {
		t.Fatalf("Arabic fallback document = %#v, %v", arabic, err)
	}
	if _, err := application.Local().PublishChanges(ctx, "posts", document.ID, store.Values{"summary": store.String("")}, 0, nil, ridu.LocaleOptions{Locale: "ar"}); err != nil {
		t.Fatal(err)
	}
	arabic, err = application.Local().Find(ctx, "posts", document.ID, nil, ridu.LocaleOptions{Locale: "ar"})
	if err != nil || stringValue(arabic.Values["summary"]) != "English summary" || arabic.LocalizationSources["summary"] != "en" {
		t.Fatalf("Arabic empty-string fallback = %#v, %v", arabic, err)
	}
	title, _ := query.NewPath("title")
	page, err := application.Local().List(ctx, "posts", ridu.ListOptions{Locale: "fr", Where: query.Equal(title, query.String("Bonjour"))})
	if err != nil || page.Total != 1 || page.Documents[0].ID != document.ID {
		t.Fatalf("French query = %#v, %v", page, err)
	}
	summary, _ := query.NewPath("summary")
	page, err = application.Local().List(ctx, "posts", ridu.ListOptions{Locale: "ar", Where: query.Equal(summary, query.String("English summary"))})
	if err != nil || page.Total != 1 || page.Documents[0].ID != document.ID {
		t.Fatalf("Arabic empty-string fallback query = %#v, %v", page, err)
	}
	description, _ := query.NewPath("seo", "description")
	page, err = application.Local().List(ctx, "posts", ridu.ListOptions{Locale: "fr", Where: query.Equal(description, query.String("Description française"))})
	if err != nil || page.Total != 1 || page.Documents[0].ID != document.ID {
		t.Fatalf("French nested query = %#v, %v", page, err)
	}
	detailName, _ := query.NewPath("details", "name")
	page, err = application.Local().List(ctx, "posts", ridu.ListOptions{Locale: "fr", Where: query.Equal(detailName, query.String("Détails français"))})
	if err != nil || page.Total != 1 || page.Documents[0].ID != document.ID {
		t.Fatalf("French localized-parent query = %#v, %v", page, err)
	}
	label, _ := query.NewPath("links", "label")
	page, err = application.Local().List(ctx, "posts", ridu.ListOptions{Locale: "fr", Where: query.Equal(label, query.String("À propos"))})
	if err != nil || page.Total != 1 || page.Documents[0].ID != document.ID {
		t.Fatalf("French localized array query = %#v, %v", page, err)
	}
	all, err := application.Local().Find(ctx, "posts", document.ID, nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	values, ok := all.Values["title"].ObjectValue()
	if !ok || stringValue(values["en"]) != "Hello" || stringValue(values["fr"]) != "Bonjour" {
		t.Fatalf("all locale values = %#v", all.Values["title"])
	}
}

func TestPostgresFilteredSelectionUsesOneCanonicalOverflowSentinel(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{Name: "PostgreSQL filtered selection", Collections: []ridu.Collection{{
		Slug: "posts", Fields: []field.Definition{field.Text("title", field.Required())},
	}}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 101; index++ {
		id := fmt.Sprintf("post-%03d", index)
		if _, err := application.Local().Import(ctx, "posts", store.Values{"title": store.String(id)}, ridu.ImportOptions{ID: id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	collection := manifest.Snapshot().Collections[0]
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := transaction.ResolveFilteredSelection(ctx, store.FilteredSelectionRequest{Collection: collection, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !selection.Overflow || len(selection.IDs) != 100 || selection.IDs[0] != "post-000" || selection.IDs[99] != "post-099" {
		t.Fatalf("PostgreSQL overflow selection = %#v", selection)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := transaction.ResolveFilteredSelection(canceled, store.FilteredSelectionRequest{Collection: collection, Limit: 100}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled PostgreSQL selection error = %v", err)
	}
	if err := transaction.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAuthHardeningConformance(t *testing.T) {
	ctx := context.Background()
	var verificationToken, resetToken string
	config := ridu.Config{
		Name: "PostgreSQL auth hardening", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{
				Password: ridu.PasswordPolicy{MinLength: 12}, MaxLoginAttempts: 2, LockDuration: time.Second,
				Verify: &ridu.VerifyEmailConfig{Send: func(_ context.Context, notification ridu.VerifyEmailNotification) error {
					verificationToken = notification.Token
					return nil
				}},
				PasswordReset: ridu.PasswordResetConfig{Send: func(_ context.Context, notification ridu.PasswordResetNotification) error {
					resetToken = notification.Token
					return nil
				}},
				APIKeys: true,
			},
			Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	user, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("  Secure@Example.Test ")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(ctx, "users", user.ID, "correct-horse"); err != nil || verificationToken == "" {
		t.Fatalf("set password = %v, verification token present = %t", err, verificationToken != "")
	}
	if _, err := application.Login(ctx, "users", "secure@example.test", "correct-horse"); !hasOperationCode(err, "email_not_verified") {
		t.Fatalf("unverified login = %v", err)
	}
	if err := application.VerifyEmail(ctx, "users", verificationToken); err != nil {
		t.Fatal(err)
	}
	first, err := application.Login(ctx, "users", "SECURE@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Login(ctx, "users", "secure@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := application.Sessions(ctx, second.Token)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions = %#v, %v", sessions, err)
	}
	key, err := application.CreateAPIKey(ctx, second.Token, "Integration", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	actor, err := application.AuthenticateAPIKey(ctx, key.Key)
	if err != nil || actor.ID != user.ID {
		t.Fatalf("API key actor = %#v, %v", actor, err)
	}
	if err := application.RequestPasswordReset(ctx, "users", "secure@example.test"); err != nil || resetToken == "" {
		t.Fatalf("request reset = %v, token present = %t", err, resetToken != "")
	}
	if err := application.ResetPassword(ctx, "users", resetToken, "changed-horse"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{first.Token, second.Token} {
		if _, err := application.Session(ctx, token); !hasOperationCode(err, "access_denied") {
			t.Fatalf("session survived reset: %v", err)
		}
	}
	if _, err := application.Login(ctx, "users", "secure@example.test", "changed-horse"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	allowed, err := backend.AllowAuthAttempt(ctx, "shared-rate-key", now, time.Minute, 1)
	if err != nil || !allowed {
		t.Fatalf("first rate attempt = %t, %v", allowed, err)
	}
	allowed, err = backend.AllowAuthAttempt(ctx, "shared-rate-key", now, time.Minute, 1)
	if err != nil || allowed {
		t.Fatalf("second rate attempt = %t, %v", allowed, err)
	}
}

func TestArtifactRunnerPreservesNestedFieldsReferencesAndVersionHistory(t *testing.T) {
	ctx := context.Background()
	beforeConfig := ridu.Config{Name: "Artifact rename", Admin: ridu.AdminConfig{User: "users"}, Collections: []ridu.Collection{
		{Slug: "users", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique()), field.Relationship("manager", field.To("users"))}},
		{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10}, Fields: []field.Definition{
			field.Group("seo", field.Fields(field.Text("title", field.Required()))),
			field.Relationship("related", field.ToAny("users", "posts"), field.OnDelete(field.ReferenceDeleteRestrict)),
		}},
		{Slug: "news", Versions: true, LockDocuments: true, VersionConfig: ridu.VersionConfig{Drafts: true}, DocumentLockConfig: ridu.DocumentLockConfig{Duration: time.Minute}, Fields: []field.Definition{field.Text("title", field.Required())}},
	}}
	backend, before := integrationBackend(t, ctx, beforeConfig)
	directory := t.TempDir()
	initial, err := postgres.BuildArtifact(ctx, "initial", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	beforeApp, err := ridu.New(beforeConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	user, err := beforeApp.CreateAuthUser(ctx, "users", store.Values{"email": store.String("ada@example.test")}, "rename-password-value", nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := beforeApp.Local().Create(ctx, "posts", store.Values{
		"seo":     store.Object(store.Values{"title": store.String("Preserved nested value")}),
		"related": store.Object(store.Values{"relationTo": store.String("users"), "id": store.String(user.ID)}),
	}, &store.Document{ID: "editor"})
	if err != nil {
		t.Fatal(err)
	}
	news, err := beforeApp.Local().Create(ctx, "news", store.Values{"title": store.String("Durable rename")}, &user)
	if err != nil {
		t.Fatal(err)
	}
	beforeIDs := map[schema.CollectionSlug]schema.StableID{}
	for _, collection := range before.Snapshot().Collections {
		beforeIDs[collection.Slug] = collection.ID
	}
	if _, err := backend.SetPreference(ctx, store.Preference{CollectionID: beforeIDs["users"], UserID: user.ID, Key: "rename-proof", Value: json.RawMessage(`{"kept":true}`)}); err != nil {
		t.Fatal(err)
	}
	scheduled, err := beforeApp.SchedulePublish(ctx, "news", news.ID, time.Now().Add(-time.Second), news.Revision, &ridu.AuthIdentity{Collection: "users", Actor: user})
	if err != nil {
		t.Fatal(err)
	}
	custom, err := backend.EnqueueTask(ctx, store.Task{
		Slug: "application-owned-rename-proof", Queue: "default", ConcurrencyKey: "opaque-users-key",
		Input: json.RawMessage(`{"collectionID":"application-owned-old-value"}`), RunAt: time.Now().Add(time.Hour),
		MaxAttempts: 3, RetryDelay: time.Second, MaxRetryDelay: time.Minute, Backoff: store.TaskBackoffFixed, Timeout: time.Minute, Retention: time.Hour,
		Target:      &store.DocumentReference{CollectionID: beforeIDs["users"], DocumentID: user.ID},
		RequestedBy: &store.DocumentReference{CollectionID: beforeIDs["users"], DocumentID: user.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, acquired, err := backend.AcquireDocumentLock(ctx, store.DocumentLock{
		CollectionID: beforeIDs["news"], DocumentID: news.ID, OwnerCollectionID: beforeIDs["users"], OwnerID: user.ID,
		OwnerLabel: "Ada", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
	}, now, false); err != nil || !acquired {
		t.Fatalf("seed document lock = %t, %v", acquired, err)
	}

	afterConfig := ridu.Config{Name: "Artifact rename", Admin: ridu.AdminConfig{User: "members"}, Collections: []ridu.Collection{
		{Slug: "members", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique()), field.Relationship("manager", field.To("members"))}},
		{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10}, Fields: []field.Definition{
			field.Group("seo", field.Fields(field.Text("headline", field.Required()))),
			field.Relationship("related", field.ToAny("members", "posts"), field.OnDelete(field.ReferenceDeleteRestrict)),
		}},
		{Slug: "articles", Versions: true, LockDocuments: true, VersionConfig: ridu.VersionConfig{Drafts: true}, DocumentLockConfig: ridu.DocumentLockConfig{Duration: time.Minute}, Fields: []field.Definition{field.Text("title", field.Required())}},
	}}
	after, err := ridu.Resolve(afterConfig)
	if err != nil {
		t.Fatal(err)
	}
	restrictPolicy := false
	for _, collection := range after.Snapshot().Collections {
		if collection.Slug != "posts" {
			continue
		}
		for _, candidate := range collection.Fields {
			if candidate.Name == "related" {
				restrictPolicy = candidate.Relationship != nil && candidate.Relationship.OnDelete == schema.ReferenceDeleteRestrict
			}
		}
	}
	if !restrictPolicy {
		t.Fatal("resolved renamed relationship is missing the restrict delete policy")
	}
	candidates := schemadiff.RenameCandidates(before, after)
	var renames []postgres.Rename
	for _, candidate := range candidates {
		rename := postgres.Rename{
			Kind: postgres.RenameKind(candidate.Kind), BeforeCollection: candidate.BeforeCollection, AfterCollection: candidate.AfterCollection,
			BeforeField: candidate.BeforeField, AfterField: candidate.AfterField,
		}
		for _, pair := range candidate.Fields {
			rename.Fields = append(rename.Fields, postgres.FieldRename{Before: pair.Before, After: pair.After})
		}
		renames = append(renames, rename)
	}
	if len(renames) != 3 {
		t.Fatalf("rename candidates = %#v", candidates)
	}
	second, err := postgres.BuildArtifact(ctx, "preserve-renames", &before, after, renames, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "preserve-renames", second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); !errors.Is(err, postgres.ErrMaintenanceRequired) {
		t.Fatalf("content rename without maintenance admission = %v", err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("refused content rename changed migration history: %#v, %v", statuses, err)
	}
	unchanged, err := beforeApp.Local().Find(ctx, "posts", post.ID, &store.Document{ID: "editor"})
	if err != nil {
		t.Fatal(err)
	}
	unchangedSEO, _ := unchanged.Values["seo"].ObjectValue()
	if title, _ := unchangedSEO["title"].StringValue(); title != "Preserved nested value" {
		t.Fatalf("refused content rename changed stored content: %#v", unchanged.Values)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, postgres.RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}
	afterApp, err := ridu.New(afterConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	afterIDs := map[schema.CollectionSlug]schema.StableID{}
	for _, collection := range after.Snapshot().Collections {
		afterIDs[collection.Slug] = collection.ID
	}
	if _, err := afterApp.Login(ctx, "members", "ada@example.test", "rename-password-value"); err != nil {
		t.Fatalf("renamed auth credential: %v", err)
	}
	preference, err := backend.GetPreference(ctx, afterIDs["members"], user.ID, "rename-proof")
	var preferenceValue map[string]bool
	if err == nil {
		err = json.Unmarshal(preference.Value, &preferenceValue)
	}
	if err != nil || !preferenceValue["kept"] {
		t.Fatalf("renamed preference = %#v, %v", preference, err)
	}
	storedCustom, err := backend.FindTask(ctx, custom.ID)
	var customInput map[string]string
	if err == nil {
		err = json.Unmarshal(storedCustom.Input, &customInput)
	}
	if err != nil || storedCustom.Target == nil || storedCustom.RequestedBy == nil ||
		storedCustom.Target.CollectionID != afterIDs["members"] || storedCustom.RequestedBy.CollectionID != afterIDs["members"] ||
		customInput["collectionID"] != "application-owned-old-value" || storedCustom.ConcurrencyKey != "opaque-users-key" {
		t.Fatalf("renamed custom task = %#v, %v", storedCustom, err)
	}
	lock, err := backend.FindDocumentLock(ctx, afterIDs["articles"], news.ID, time.Now())
	if err != nil || lock.OwnerCollectionID != afterIDs["members"] {
		t.Fatalf("renamed document lock = %#v, %v", lock, err)
	}
	jobs, err := afterApp.ScheduledPublishes(ctx, "articles", news.ID, &ridu.AuthIdentity{Collection: "members", Actor: user})
	if err != nil || len(jobs) != 1 || jobs[0].ID != scheduled.ID || jobs[0].CollectionID != afterIDs["articles"] || jobs[0].RequestedByCollectionID != afterIDs["members"] {
		t.Fatalf("renamed durable publish = %#v, %v", jobs, err)
	}
	if completed, err := afterApp.RunScheduledPublishes(ctx, 10, nil); err != nil || completed != 1 {
		t.Fatalf("execute renamed durable publish = %d, %v", completed, err)
	}
	if published, err := afterApp.Local().Find(ctx, "articles", news.ID, nil); err != nil || published.Status != store.StatusPublished {
		t.Fatalf("renamed scheduled target = %#v, %v", published, err)
	}
	actor := &store.Document{ID: "editor"}
	preserved, err := afterApp.Local().Find(ctx, "posts", post.ID, actor)
	if err != nil {
		t.Fatal(err)
	}
	seo, _ := preserved.Values["seo"].ObjectValue()
	if headline, _ := seo["headline"].StringValue(); headline != "Preserved nested value" {
		t.Fatalf("nested value = %#v", seo)
	}
	related, _ := preserved.Values["related"].ObjectValue()
	if relationTo, _ := related["relationTo"].StringValue(); relationTo != "members" {
		t.Fatalf("relationship target = %#v", related)
	}
	versions, err := afterApp.Local().Versions(ctx, "posts", post.ID, actor)
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions = %#v, %v", versions, err)
	}
	versionSEO, _ := versions[0].Snapshot.Values["seo"].ObjectValue()
	if headline, _ := versionSEO["headline"].StringValue(); headline != "Preserved nested value" {
		t.Fatalf("version nested value = %#v", versionSEO)
	}
	versionRelated, _ := versions[0].Snapshot.Values["related"].ObjectValue()
	if relationTo, _ := versionRelated["relationTo"].StringValue(); relationTo != "members" {
		t.Fatalf("version relationship target = %#v", versionRelated)
	}
	if _, err := afterApp.Local().Delete(ctx, "members", user.ID, nil); !hasOperationCode(err, "delete_restricted") {
		t.Fatalf("renamed derived reference did not protect target: %v", err)
	}
	statuses, err = backend.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) != 2 || !statuses[0].Applied || !statuses[1].Applied {
		t.Fatalf("artifact status = %#v, %v", statuses, err)
	}
	if err := postgres.VerifyArtifactsWithOptions(ctx, os.Getenv("RIDU_POSTGRES_URL"), directory, postgres.RunnerOptions{AllowMaintenance: true, AllowInsecureDatabase: true}); err != nil {
		t.Fatalf("shadow verification: %v", err)
	}
	if err := backend.ApplyPlan(ctx, []postgres.Statement{{Kind: "test_drift", SQL: `CREATE TABLE "z_c_unexpected_drift" ("id" text PRIMARY KEY)`}}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "physical schema drift") {
		t.Fatalf("expected physical drift failure, got %v", err)
	}
	if err := backend.ApplyPlan(ctx, []postgres.Statement{{Kind: "remove_test_drift", SQL: `DROP TABLE "z_c_unexpected_drift"`}}); err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(files[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	var edited ridumigration.Artifact
	if err := json.Unmarshal(encoded, &edited); err != nil {
		t.Fatal(err)
	}
	edited.Risks = append(edited.Risks, ridumigration.Risk{Code: "EDITED", Level: ridumigration.RiskNotice, Message: "post-application edit"})
	encoded, err = json.MarshalIndent(edited, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[1].Path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "changed after application") {
		t.Fatalf("expected applied-artifact integrity failure, got %v", err)
	}
}

func TestPostgresRollbackReferenceLocksAndOptimisticConcurrency(t *testing.T) {
	ctx := context.Background()
	rollbackConfig := ridu.Config{Name: "PostgreSQL transaction safety", Collections: []ridu.Collection{
		{Slug: "targets", Fields: []field.Definition{field.Text("name", field.Required())}},
		{Slug: "entries", Fields: []field.Definition{
			field.Text("title", field.Required()), field.Relationship("target", field.To("targets"), field.Required()),
		}, Hooks: ridu.CollectionHooks{AfterOperation: []ridu.Hook{func(ctx ridu.HookContext) error {
			if ctx.Operation == ridu.OperationCreate {
				return errors.New("force rollback after insert")
			}
			return nil
		}}}},
	}}
	backend, manifest := integrationBackend(t, ctx, rollbackConfig)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(rollbackConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(ctx, "targets", store.Values{"name": store.String("Locked")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"title": store.String("Rollback"), "target": store.String(target.ID),
	}, nil); !hasOperationCode(err, "hook_failed") {
		t.Fatalf("PostgreSQL after-operation rollback error = %v", err)
	}
	page, err := application.Local().List(ctx, "entries", ridu.ListOptions{})
	if err != nil || page.Total != 0 {
		t.Fatalf("rolled-back PostgreSQL entries = %#v, %v", page, err)
	}

	targetCollection := manifest.Snapshot().Collections[0]
	collections := map[schema.StableID]schema.Collection{targetCollection.ID: targetCollection}
	locker, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback(context.Background())
	if _, err := locker.Find(ctx, store.Request{
		Collection: targetCollection, Collections: collections, ID: target.ID, Lock: store.LockReference,
	}); err != nil {
		t.Fatal(err)
	}
	deleter, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleter.Rollback(context.Background())
	deleteContext, cancelDelete := context.WithTimeout(ctx, 5*time.Second)
	defer cancelDelete()
	deleteResult := make(chan error, 1)
	go func() {
		_, deleteError := deleter.Delete(deleteContext, store.Request{Collection: targetCollection, Collections: collections, ID: target.ID})
		deleteResult <- deleteError
	}()
	select {
	case err := <-deleteResult:
		t.Fatalf("reference-locked target delete completed before commit: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if err := locker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-deleteResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reference-locked target delete did not resume after commit")
	}
	if err := deleter.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	versionConfig := ridu.Config{Name: "PostgreSQL optimistic concurrency", Collections: []ridu.Collection{{
		Slug: "documents", Versions: true, Fields: []field.Definition{field.Text("title", field.Required())},
	}}}
	versionBackend, versionManifest := integrationBackend(t, ctx, versionConfig)
	applyInitialArtifact(t, ctx, versionBackend, versionManifest)
	versionApplication, err := ridu.New(versionConfig, versionBackend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := versionApplication.Local().Create(ctx, "documents", store.Values{"title": store.String("Initial")}, nil)
	if err != nil || document.Revision != 1 {
		t.Fatalf("versioned document = %#v, %v", document, err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, title := range []string{"First", "Second"} {
		title := title
		go func() {
			<-start
			_, updateError := versionApplication.Local().PublishChanges(ctx, "documents", document.ID, store.Values{"title": store.String(title)}, 1, nil)
			results <- updateError
		}()
	}
	close(start)
	var successes, conflicts int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case hasOperationCode(err, "conflict"):
			conflicts++
		default:
			t.Fatalf("concurrent update error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent update outcomes: successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestPostgresAtomicJoinMutationRollsBackMidBatch(t *testing.T) {
	ctx := context.Background()
	var rejectedID string
	config := ridu.Config{Name: "PostgreSQL atomic inverse joins", Collections: []ridu.Collection{
		{Slug: "categories", Fields: []field.Definition{
			field.Text("name", field.Required()),
			field.Join("posts", "posts", "category"),
		}},
		{Slug: "posts", Versions: true, Fields: []field.Definition{
			field.Text("title", field.Required()),
			field.Relationship("category", field.To("categories")),
		}, Hooks: ridu.CollectionHooks{BeforeOperation: []ridu.Hook{func(hook ridu.HookContext) error {
			if hook.Operation != ridu.OperationPublish || hook.Original == nil {
				return nil
			}
			if hook.Original.ID == rejectedID {
				return errors.New("reject the second join target")
			}
			return nil
		}}}},
	}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	source, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("Source")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	addition, err := application.Local().Import(ctx, "posts", store.Values{"title": store.String("Addition")}, ridu.ImportOptions{ID: "join-a-addition", Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := application.Local().Import(ctx, "posts", store.Values{"title": store.String("Removal"), "category": store.String(source.ID)}, ridu.ImportOptions{ID: "join-z-removal", Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rejectedID = removal.ID
	if _, err := application.Local().MutateJoin(ctx, "categories", source.ID, "posts", []string{addition.ID}, []string{removal.ID}, nil); !hasOperationCode(err, "hook_failed") {
		t.Fatalf("mid-batch join failure = %v, want hook_failed", err)
	}
	assertPostgresRelationship(t, application, addition.ID, "")
	assertPostgresRelationship(t, application, removal.ID, source.ID)
}

func TestPostgresPublishLocksTheRowBeforeStatusHooks(t *testing.T) {
	ctx := context.Background()
	hookEntered := make(chan struct{})
	releasePublish := make(chan struct{})
	var hookOnce sync.Once
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePublish) }) }
	defer release()
	config := ridu.Config{Name: "PostgreSQL status mutation row lock", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: []field.Definition{
			field.Text("title", field.Required()),
			field.Textarea("summary"),
		},
		Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(hook ridu.HookContext) error {
			if hook.Operation != ridu.OperationPublish {
				return nil
			}
			hook.Data["summary"] = store.String("written by publish hook")
			hookOnce.Do(func() {
				close(hookEntered)
				<-releasePublish
			})
			return nil
		}}},
	}}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	armProbe := make(chan struct{})
	lockAttempted := make(chan struct{})
	lockReturned := make(chan struct{})
	probe := &mutationLockProbeStore{
		Store: backend, TaskStore: backend, arm: armProbe, attempted: lockAttempted, returned: lockReturned,
	}
	application, err := ridu.New(config, probe)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Initial"), "summary": store.String("Initial summary"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	publishResult := make(chan error, 1)
	go func() {
		_, publishError := application.Local().Publish(ctx, "posts", document.ID, document.Revision, nil)
		publishResult <- publishError
	}()
	select {
	case <-hookEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("publish did not enter the blocking status hook")
	}
	close(armProbe)
	updateResult := make(chan error, 1)
	go func() {
		_, updateError := application.Local().PublishChanges(ctx, "posts", document.ID, store.Values{
			"title": store.String("Concurrent"),
		}, 0, nil)
		updateResult <- updateError
	}()
	select {
	case <-lockAttempted:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent update did not attempt to acquire the mutation row lock")
	}
	select {
	case <-lockReturned:
		t.Fatal("concurrent update acquired the row while the publish hook still held its transaction")
	case <-time.After(200 * time.Millisecond):
	}
	release()
	for name, result := range map[string]<-chan error{"publish": publishResult, "update": updateResult} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("%s mutation error = %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s mutation did not finish after releasing the publish hook", name)
		}
	}
	final, err := application.Local().Find(ctx, "posts", document.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if title := stringValue(final.Values["title"]); title != "Concurrent" {
		t.Fatalf("final title = %q, want concurrent update", title)
	}
	if summary := stringValue(final.Values["summary"]); summary != "written by publish hook" {
		t.Fatalf("final summary = %q, want committed publish-hook change", summary)
	}
	if final.Status != store.StatusPublished || final.Revision != 3 {
		t.Fatalf("final status/revision = %s/%d, want published/3", final.Status, final.Revision)
	}
}

func TestPostgresConcurrentJoinReparentUsesObservedValueConflict(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{Name: "PostgreSQL concurrent inverse joins", Collections: []ridu.Collection{
		{Slug: "categories", Fields: []field.Definition{
			field.Text("name", field.Required()),
			field.Join("posts", "posts", "category"),
		}},
		{Slug: "posts", Fields: []field.Definition{
			field.Text("title", field.Required()),
			field.Relationship("category", field.To("categories")),
		}},
	}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	barrier := make(chan struct{}, 2)
	release := make(chan struct{})
	application, err := ridu.New(config, &joinBarrierStore{Store: backend, barrier: barrier, release: release})
	if err != nil {
		t.Fatal(err)
	}
	left, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("Left")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	right, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("Right")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Concurrent")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, sourceID := range []string{left.ID, right.ID} {
		sourceID := sourceID
		go func() {
			_, mutationError := application.Local().MutateJoin(ctx, "categories", sourceID, "posts", []string{target.ID}, nil, nil)
			results <- mutationError
		}()
	}
	for range 2 {
		select {
		case <-barrier:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent join mutation did not complete preflight")
		}
	}
	close(release)
	var successes, conflicts int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case hasOperationCode(err, "conflict"):
			conflicts++
		default:
			t.Fatalf("concurrent join mutation error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent join outcomes: successes=%d conflicts=%d", successes, conflicts)
	}
	document, err := application.Local().Find(ctx, "posts", target.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent, valid := document.Values["category"].StringValue()
	if !valid || parent != left.ID && parent != right.ID {
		t.Fatalf("concurrent target parent = %#v", document.Values["category"])
	}
}

func TestPostgresSelfJoinMutationsAcquireCrossedRowsWithoutDeadlock(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{Name: "PostgreSQL self inverse joins", Collections: []ridu.Collection{{
		Slug: "nodes", Fields: []field.Definition{
			field.Text("name", field.Required()),
			field.Relationship("parent", field.To("nodes")),
			field.Join("children", "nodes", "parent"),
		},
	}}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	barrier := make(chan struct{}, 2)
	release := make(chan struct{})
	application, err := ridu.New(config, &joinBarrierStore{Store: backend, barrier: barrier, release: release})
	if err != nil {
		t.Fatal(err)
	}
	left, err := application.Local().Create(ctx, "nodes", store.Values{"name": store.String("Left")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	right, err := application.Local().Create(ctx, "nodes", store.Values{"name": store.String("Right")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, mutation := range []struct{ source, target string }{{left.ID, right.ID}, {right.ID, left.ID}} {
		mutation := mutation
		go func() {
			_, mutationError := application.Local().MutateJoin(ctx, "nodes", mutation.source, "children", []string{mutation.target}, nil, nil)
			results <- mutationError
		}()
	}
	for range 2 {
		select {
		case <-barrier:
		case <-time.After(5 * time.Second):
			t.Fatal("crossed self-join mutations did not complete preflight")
		}
	}
	close(release)
	var successes int
	for range 2 {
		var resultError error
		select {
		case resultError = <-results:
		case <-time.After(5 * time.Second):
			t.Fatal("crossed self-join mutation did not finish")
		}
		switch {
		case resultError == nil:
			successes++
		default:
			t.Fatalf("crossed self-join mutation error = %v", resultError)
		}
	}
	if successes != 2 {
		t.Fatalf("crossed self-join successes = %d, want 2", successes)
	}
}

type joinBarrierStore struct {
	store.Store
	barrier chan<- struct{}
	release <-chan struct{}
}

type mutationLockProbeStore struct {
	store.Store
	store.TaskStore
	arm       <-chan struct{}
	attempted chan struct{}
	returned  chan struct{}
	once      sync.Once
}

func (backend *mutationLockProbeStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &mutationLockProbeTransaction{Transaction: transaction, backend: backend}, nil
}

type mutationLockProbeTransaction struct {
	store.Transaction
	backend *mutationLockProbeStore
}

func (transaction *mutationLockProbeTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	tracked := false
	if request.Lock == store.LockMutation {
		select {
		case <-transaction.backend.arm:
			transaction.backend.once.Do(func() {
				tracked = true
				close(transaction.backend.attempted)
			})
		default:
		}
	}
	document, err := transaction.Transaction.Find(ctx, request)
	if tracked {
		close(transaction.backend.returned)
	}
	return document, err
}

func (transaction *mutationLockProbeTransaction) SaveVersion(ctx context.Context, collection schema.Collection, document store.Document, max int) (store.Version, error) {
	return transaction.Transaction.(store.VersionTransaction).SaveVersion(ctx, collection, document, max)
}

func (transaction *mutationLockProbeTransaction) ListVersions(ctx context.Context, request store.VersionRequest) ([]store.Version, error) {
	return transaction.Transaction.(store.VersionTransaction).ListVersions(ctx, request)
}

func (transaction *mutationLockProbeTransaction) FindVersion(ctx context.Context, collection schema.Collection, documentID string, revision int) (store.Version, error) {
	return transaction.Transaction.(store.VersionTransaction).FindVersion(ctx, collection, documentID, revision)
}

func (backend *joinBarrierStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &joinBarrierTransaction{Transaction: transaction, barrier: backend.barrier, release: backend.release}, nil
}

type joinBarrierTransaction struct {
	store.Transaction
	barrier chan<- struct{}
	release <-chan struct{}
	finds   int
}

func (transaction *joinBarrierTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	if request.Lock == store.LockNone {
		transaction.finds++
		if transaction.finds == 3 {
			transaction.barrier <- struct{}{}
			<-transaction.release
		}
	}
	return transaction.Transaction.Find(ctx, request)
}

func assertPostgresRelationship(t *testing.T, application *ridu.App, documentID, expected string) {
	t.Helper()
	document, err := application.Local().Find(context.Background(), "posts", documentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := document.Values["category"].StringValue()
	if actual != expected {
		t.Fatalf("relationship = %q, want %q", actual, expected)
	}
}

func TestPostgresDestructiveArtifactRequiresApprovalAndAppliesInIsolation(t *testing.T) {
	ctx := context.Background()
	beforeConfig := ridu.Config{Name: "Destructive migration", Collections: []ridu.Collection{{
		Slug: "posts", Fields: []field.Definition{field.Text("title", field.Required()), field.Text("summary")},
	}}}
	afterConfig := ridu.Config{Name: "Destructive migration", Collections: []ridu.Collection{{
		Slug: "posts", Fields: []field.Definition{field.Text("title", field.Required())},
	}}}
	backend, before := integrationBackend(t, ctx, beforeConfig)
	directory := t.TempDir()
	initial, err := postgres.BuildArtifact(ctx, "initial", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	beforeApplication, err := ridu.New(beforeConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := beforeApplication.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Keep"), "summary": store.String("Remove intentionally"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ridu.Resolve(afterConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.BuildArtifact(ctx, "drop-summary", &before, after, nil, false); err == nil {
		t.Fatal("destructive PostgreSQL artifact was created without approval")
	}
	destructive, err := postgres.BuildArtifact(ctx, "drop-summary", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "drop-summary", destructive, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if plan, err := backend.Plan(ctx, after); err != nil || len(plan) != 0 {
		t.Fatalf("post-destructive migration plan = %#v, %v", plan, err)
	}
	afterApplication, err := ridu.New(afterConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	preserved, err := afterApplication.Local().Find(ctx, "posts", document.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := preserved.Values["title"].StringValue(); title != "Keep" {
		t.Fatalf("preserved title = %q", title)
	}
	if _, exists := preserved.Values["summary"]; exists {
		t.Fatal("removed field remained after explicitly approved destructive migration")
	}
}

type integrationMigrationPlugin struct{}

func (integrationMigrationPlugin) Key() string { return "audit" }

func (integrationMigrationPlugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version: "1.0.0", GoPackage: "example.com/plugins/audit", APIVersion: ridu.PluginAPIVersion,
		Ridu: ridu.RiduCompatibility{Minimum: "0.0.0-dev", MaximumExclusive: "1.0.0"},
		DatabaseContributions: []ridu.PluginDatabaseContribution{{
			Adapter: ridu.PluginDatabaseAdapterPostgres, Tables: []string{"ridu_plugin_audit_events"},
			Migrations: []ridu.PluginMigration{{
				Version: 1, Name: "create-events",
				UpSQL:   []string{`CREATE TABLE ridu_plugin_audit_events (id text PRIMARY KEY, payload jsonb NOT NULL)`, `INSERT INTO ridu_plugin_audit_events (id, payload) VALUES ('fixture', '{"ok":true}'::jsonb)`},
				DownSQL: []string{`DROP TABLE ridu_plugin_audit_events`},
			}},
		}},
	}
}

type integrationMissingTablePlugin struct{}

func (integrationMissingTablePlugin) Key() string { return "missing-table" }

func (integrationMissingTablePlugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version: "1.0.0", GoPackage: "example.com/plugins/missing-table", APIVersion: ridu.PluginAPIVersion,
		Ridu: ridu.RiduCompatibility{Minimum: "0.0.0-dev", MaximumExclusive: "1.0.0"},
		DatabaseContributions: []ridu.PluginDatabaseContribution{{
			Adapter: ridu.PluginDatabaseAdapterPostgres, Tables: []string{"ridu_plugin_missing_table_state"},
			Migrations: []ridu.PluginMigration{{Version: 1, Name: "missing-state", UpSQL: []string{"SELECT 1"}, DownSQL: []string{"SELECT 1"}}},
		}},
	}
}

func TestPostgresRejectsDeclaredPluginTableMissingFromMigration(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{
		Name: "Missing plugin table", Plugins: []ridu.Plugin{integrationMissingTablePlugin{}},
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	artifact, err := postgres.BuildArtifact(ctx, "missing-plugin-table", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if _, err := migrationartifact.Create(directory, "missing-plugin-table", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "declared plugin table") {
		t.Fatalf("missing declared plugin table error = %v", err)
	}
}

func TestPluginMigrationsUpgradeDowngradeAndReplayAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	withPlugin := ridu.Config{Name: "Plugin migration", Plugins: []ridu.Plugin{integrationMigrationPlugin{}}, Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}}}
	withoutPlugin := ridu.Config{Name: "Plugin migration", Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}}}
	backend, before := integrationBackend(t, ctx, withPlugin)
	directory := t.TempDir()
	initial, err := postgres.BuildArtifact(ctx, "initial-with-plugin", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial-with-plugin", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	after, err := ridu.Resolve(withoutPlugin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.BuildArtifact(ctx, "remove-plugin", &before, after, nil, false); err == nil {
		t.Fatal("plugin downgrade was planned without destructive approval")
	}
	removal, err := postgres.BuildArtifact(ctx, "remove-plugin", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "remove-plugin", removal, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := postgres.VerifyArtifactsWithOptions(ctx, os.Getenv("RIDU_POSTGRES_URL"), directory, postgres.RunnerOptions{AllowInsecureDatabase: true}); err != nil {
		t.Fatal(err)
	}
}

func integrationConfig() ridu.Config {
	statusPath, _ := query.NewPath("status")
	return ridu.Config{
		Name:    "PostgreSQL integration",
		Admin:   ridu.AdminConfig{User: "users"},
		Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: []field.Definition{
				field.Text("email", field.Required(), field.Unique()),
				field.Relationship("manager", field.To("users")),
			}},
			{
				Slug: "posts",
				Fields: []field.Definition{
					field.Text("title", field.Required()),
					field.Select("status", field.Required(), field.Choices(
						field.Choice{Value: "draft", Label: "Draft"}, field.Choice{Value: "published", Label: "Published"},
					)),
					field.Relationship("author", field.To("users"), field.Required()),
					field.Textarea("summary"),
					field.Email("contact"),
					field.Date("publishedOn"),
					field.Number("score"),
					field.Checkbox("featured"),
					field.JSON("metadata"),
					field.Group("seo", field.Fields(
						field.Text("description"),
					)),
					field.Array("tags", field.Fields(
						field.Text("label", field.Required()),
					)),
					field.Blocks("layout", field.BlockTypes(
						field.BlockType("quote", "Quote", field.Text("quote", field.Required())),
					)),
					richtext.Field("content"),
					field.Relationship("watchers", field.ToMany("users")),
					field.Relationship("related", field.ToAny("users", "posts")),
				},
				Access: ridu.CollectionAccess{
					Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
						return ridu.Where(query.Equal(statusPath, query.String("published"))), nil
					},
					Update: func(ridu.AccessContext) (ridu.AccessDecision, error) {
						return ridu.Where(query.Equal(statusPath, query.String("published"))), nil
					},
					Delete: func(ridu.AccessContext) (ridu.AccessDecision, error) {
						return ridu.Where(query.Equal(statusPath, query.String("published"))), nil
					},
				},
			},
		},
	}
}

func postgresNestedString(value store.Value, name string) string {
	object, _ := value.ObjectValue()
	return stringValue(object[name])
}

func postgresNestedRowString(value store.Value, index int, name string) string {
	rows, _ := value.Values()
	if index < 0 || index >= len(rows) {
		return ""
	}
	return postgresNestedString(rows[index], name)
}

func TestPostgresPermanentDeleteRemovesDocumentState(t *testing.T) {
	ctx := context.Background()
	var resetToken string
	config := ridu.Config{
		Name:  "PostgreSQL delete lifecycle",
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{
				Slug: "users", Auth: true, Versions: true,
				VersionConfig: ridu.VersionConfig{Drafts: true},
				AuthConfig: ridu.AuthConfig{
					APIKeys: true,
					PasswordReset: ridu.PasswordResetConfig{Send: func(_ context.Context, notification ridu.PasswordResetNotification) error {
						resetToken = notification.Token
						return nil
					}},
				},
				Fields: []field.Definition{field.Text("email", field.Required(), field.Unique()), field.Text("name")},
			},
			{Slug: "staff", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())}},
			{
				Slug: "posts", Versions: true, LockDocuments: true,
				VersionConfig:      ridu.VersionConfig{Drafts: true},
				DocumentLockConfig: ridu.DocumentLockConfig{Duration: time.Minute},
				Fields:             []field.Definition{field.Text("title", field.Required())},
			},
		},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := application.CreateAuthUser(ctx, "users", store.Values{
		"email": store.String("deleted@example.test"), "name": store.String("First"),
	}, "old-password-value", nil)
	if err != nil {
		t.Fatal(err)
	}
	staffCollision, err := application.Local().Import(ctx, "staff", store.Values{"email": store.String("staff-collision@example.test")}, ridu.ImportOptions{
		ID: actor.ID, Status: store.StatusPublished,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(ctx, "staff", staffCollision.ID, "staff-password-value"); err != nil {
		t.Fatal(err)
	}
	staffSession, err := application.Login(ctx, "staff", "staff-collision@example.test", "staff-password-value")
	if err != nil {
		t.Fatal(err)
	}
	actor, err = application.Local().Update(ctx, "users", actor.ID, store.Values{"name": store.String("Second")}, &actor)
	if err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(ctx, "users", "deleted@example.test", "old-password-value")
	if err != nil {
		t.Fatal(err)
	}
	key, err := application.CreateAPIKey(ctx, session.Token, "old key", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := application.RequestPasswordReset(ctx, "users", "deleted@example.test"); err != nil || resetToken == "" {
		t.Fatalf("password reset token = %q, %v", resetToken, err)
	}
	actorIdentity := &ridu.AuthIdentity{Collection: "users", Actor: actor}
	if _, err := application.SetPreference(ctx, actorIdentity, "theme", json.RawMessage(`"dark"`)); err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Scheduled")}, &actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SchedulePublish(ctx, "posts", post.ID, time.Now().Add(time.Hour), post.Revision, actorIdentity); err != nil {
		t.Fatal(err)
	}
	if state, err := application.AcquireDocumentLock(ctx, "posts", post.ID, false, actorIdentity); err != nil || !state.Acquired {
		t.Fatalf("lock = %#v, %v", state, err)
	}
	deletedID := actor.ID
	if _, err := application.Local().Delete(ctx, "users", deletedID, &actor); err != nil {
		t.Fatal(err)
	}
	collectionIDs := make(map[schema.CollectionSlug]schema.StableID)
	for _, collection := range manifest.Snapshot().Collections {
		collectionIDs[collection.Slug] = collection.ID
	}
	if err := backend.CreateSession(ctx, store.AuthSession{ID: "00000000-0000-0000-0000-000000000001", TokenHash: "late-session", CollectionID: collectionIDs["users"], UserID: deletedID, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(), LastSeenAt: time.Now()}, []byte("missing-hash")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late session = %v", err)
	}
	if err := backend.RotateSession(ctx, "missing-session", store.AuthSession{TokenHash: "rotated", CollectionID: collectionIDs["users"], UserID: deletedID}, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late session rotation = %v", err)
	}
	if err := backend.CreateAuthToken(ctx, store.AuthToken{TokenHash: "late-token", CollectionID: collectionIDs["users"], UserID: deletedID, Purpose: store.AuthTokenPasswordReset, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late auth token = %v", err)
	}
	if err := backend.CreateAPIKey(ctx, store.AuthAPIKey{ID: "late-key", TokenHash: "late-key-hash", CollectionID: collectionIDs["users"], UserID: deletedID, Name: "late", CreatedAt: time.Now()}, "missing-session", time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late API key = %v", err)
	}
	if _, err := backend.SetPreference(ctx, store.Preference{CollectionID: collectionIDs["users"], UserID: deletedID, Key: "late", Value: json.RawMessage(`true`)}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late preference = %v", err)
	}
	if _, err := backend.EnqueueTask(ctx, store.Task{
		Slug: "late-requester", Queue: "default", Input: json.RawMessage(`{}`), RunAt: time.Now().Add(time.Hour),
		MaxAttempts: 3, RetryDelay: time.Second, MaxRetryDelay: time.Minute, Backoff: store.TaskBackoffFixed, Timeout: time.Minute, Retention: time.Hour,
		Target:      &store.DocumentReference{CollectionID: collectionIDs["posts"], DocumentID: post.ID},
		RequestedBy: &store.DocumentReference{CollectionID: collectionIDs["users"], DocumentID: deletedID},
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late requester task = %v", err)
	}
	if _, _, err := backend.AcquireDocumentLock(ctx, store.DocumentLock{CollectionID: collectionIDs["posts"], DocumentID: post.ID, OwnerCollectionID: collectionIDs["users"], OwnerID: deletedID, OwnerLabel: "deleted", CreatedAt: time.Now(), UpdatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}, time.Now(), false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late owner lock = %v", err)
	}
	if _, _, err := backend.AcquireDocumentLock(ctx, store.DocumentLock{CollectionID: collectionIDs["posts"], DocumentID: post.ID, OwnerCollectionID: collectionIDs["users"], OwnerLabel: "partial", CreatedAt: time.Now(), UpdatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}, time.Now(), false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("partial lock owner = %v", err)
	}
	if current, err := application.Session(ctx, staffSession.Token); err != nil || current.User.ID != deletedID || current.Collection != "staff" {
		t.Fatalf("same-ID staff session = %#v, %v", current, err)
	}

	recreated, err := application.Local().Import(ctx, "users", store.Values{
		"email": store.String("deleted@example.test"), "name": store.String("Recreated"),
	}, ridu.ImportOptions{ID: deletedID, Status: store.StatusDraft}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(ctx, "users", recreated.ID, "new-password-value"); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Login(ctx, "users", "deleted@example.test", "old-password-value"); !hasOperationCode(err, "access_denied") {
		t.Fatalf("old password after ID reuse = %v", err)
	}
	if _, err := application.Login(ctx, "users", "deleted@example.test", "new-password-value"); err != nil {
		t.Fatalf("new password after ID reuse = %v", err)
	}
	if _, err := application.Session(ctx, session.Token); !hasOperationCode(err, "access_denied") {
		t.Fatalf("old session after ID reuse = %v", err)
	}
	if _, err := application.AuthenticateAPIKey(ctx, key.Key); !hasOperationCode(err, "access_denied") {
		t.Fatalf("old API key after ID reuse = %v", err)
	}
	if err := application.ResetPassword(ctx, "users", resetToken, "stale-reset-value"); !hasOperationCode(err, "invalid_auth_token") {
		t.Fatalf("old reset token after ID reuse = %v", err)
	}
	recreatedIdentity := &ridu.AuthIdentity{Collection: "users", Actor: recreated}
	if value, err := application.Preference(ctx, recreatedIdentity, "theme"); err != nil || string(value) != "null" {
		t.Fatalf("recreated preference = %s, %v", value, err)
	}
	if jobs, err := application.ScheduledPublishes(ctx, "posts", post.ID, recreatedIdentity); err != nil || len(jobs) != 0 {
		t.Fatalf("scheduled jobs after requester deletion = %#v, %v", jobs, err)
	}
	if state, err := application.DocumentLock(ctx, "posts", post.ID, recreatedIdentity); err != nil || state.Lock != nil {
		t.Fatalf("lock after owner deletion = %#v, %v", state, err)
	}
	if versions, err := application.Local().Versions(ctx, "users", recreated.ID, &recreated); err != nil || len(versions) != 1 || versions[0].Revision != 1 {
		t.Fatalf("versions after ID reuse = %#v, %v", versions, err)
	}
	targetOnly, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Target cleanup")}, &recreated)
	if err != nil {
		t.Fatal(err)
	}
	targetSchedule, err := application.SchedulePublish(ctx, "posts", targetOnly.ID, time.Now().Add(time.Hour), targetOnly.Revision, recreatedIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.AcquireDocumentLock(ctx, store.DocumentLock{CollectionID: collectionIDs["posts"], DocumentID: targetOnly.ID, OwnerCollectionID: collectionIDs["users"], OwnerID: recreated.ID, OwnerLabel: "recreated", CreatedAt: time.Now(), UpdatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(ctx, "posts", targetOnly.ID, &recreated); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindTask(ctx, targetSchedule.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("target task after deletion = %v", err)
	}
	if _, err := backend.FindDocumentLock(ctx, collectionIDs["posts"], targetOnly.ID, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("target lock after deletion = %v", err)
	}
}

func richTextDocument(text string) store.Value {
	return store.Object(store.Values{
		"version": store.Number(richtext.DocumentVersion),
		"root": store.Object(store.Values{
			"type": store.String("root"),
			"children": store.List(store.Object(store.Values{
				"type":     store.String("paragraph"),
				"children": store.List(store.Object(store.Values{"type": store.String("text"), "text": store.String(text)})),
			})),
		}),
	})
}

func applyInitialArtifact(t *testing.T, ctx context.Context, backend *postgres.Store, manifest schema.Manifest) string {
	t.Helper()
	directory := t.TempDir()
	artifact, err := postgres.BuildArtifact(ctx, "initial", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	return directory
}

func integrationBackend(t *testing.T, ctx context.Context, config ridu.Config) (*postgres.Store, schema.Manifest) {
	return integrationBackendWithPoolConfig(t, ctx, config, postgres.PoolConfig{})
}

func integrationBackendWithPoolConfig(t *testing.T, ctx context.Context, config ridu.Config, poolConfig postgres.PoolConfig) (*postgres.Store, schema.Manifest) {
	t.Helper()
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	schemaName := fmt.Sprintf("ridu_test_%d", time.Now().UnixNano())
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA "`+schemaName+`" CASCADE`)
		admin.Close()
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	parsed.RawQuery = parameters.Encode()
	poolConfig.DatabaseURL = parsed.String()
	poolConfig.AllowInsecureTransport = true
	backend, err := postgres.OpenWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	return backend, manifest
}

func hasOperationCode(err error, code string) bool {
	var operationError *ridu.OperationError
	return errors.As(err, &operationError) && operationError.Code == code
}

func stringValue(value store.Value) string {
	text, _ := value.StringValue()
	return text
}

func hasRelationshipIssue(err error, path string) bool {
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "validation" {
		return false
	}
	for _, issue := range operationError.Issues {
		if issue.Code == "invalid_relationship" && issue.Path == path && issue.Message == "relationship target is unavailable" {
			return true
		}
	}
	return false
}
