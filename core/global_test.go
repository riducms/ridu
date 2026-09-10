package core_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestGlobalSingletonAccessHooksDraftsAndVersions(t *testing.T) {
	var operations []operation.Kind
	application, err := ridu.New(ridu.Config{
		Name: "Globals",
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title")},
		}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Label: "Site settings",
			Admin:    ridu.GlobalAdmin{Group: " Settings ", Description: " Site-wide presentation. "},
			Fields:   field.Fields{field.Text("siteName").Required(), field.Text("announcement").Default("Welcome")},
			Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Access: ridu.GlobalAccess{
				Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if ctx.CollectionID != "" || ctx.GlobalID != "global-site-settings" {
						t.Fatalf("access resource IDs = %q, %q", ctx.CollectionID, ctx.GlobalID)
					}
					return ridu.Allow(), nil
				},
				Update: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if ctx.Actor == nil || ctx.Actor.ID != "admin" {
						return ridu.Deny(), nil
					}
					return ridu.Allow(), nil
				},
			},
			Hooks: ridu.CollectionHooks{
				BeforeOperation: []ridu.Hook{func(ctx ridu.HookContext) error {
					if ctx.CollectionID != "" || ctx.GlobalID != "global-site-settings" {
						t.Fatalf("hook resource IDs = %q, %q", ctx.CollectionID, ctx.GlobalID)
					}
					operations = append(operations, ctx.Operation)
					return nil
				}},
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	snapshot := application.Manifest().Snapshot()
	if len(snapshot.Globals) != 1 || snapshot.Globals[0].Slug != "site-settings" || !snapshot.Globals[0].Capabilities.Global {
		t.Fatalf("globals = %#v", snapshot.Globals)
	}
	if snapshot.Globals[0].Admin.Group != "Settings" || snapshot.Globals[0].Admin.Description != "Site-wide presentation." {
		t.Fatalf("global admin = %#v", snapshot.Globals[0].Admin)
	}
	actor := &store.Document{ID: "admin"}
	initial, err := application.Local().Global(context.Background(), "site-settings", actor)
	if err != nil {
		t.Fatal(err)
	}
	if initial.ID != "site-settings" || stringValue(initial.Values["announcement"]) != "Welcome" {
		t.Fatalf("initial global = %#v", initial)
	}
	if _, err := application.Local().UpdateGlobal(context.Background(), "site-settings", store.Values{"siteName": store.String("Denied")}, 0, nil); err == nil {
		t.Fatal("anonymous global update succeeded")
	} else {
		var operationError *ridu.OperationError
		if !errors.As(err, &operationError) || operationError.Code != "access_denied" {
			t.Fatalf("anonymous update error = %v", err)
		}
	}

	created, err := application.Local().UpdateGlobal(context.Background(), "site-settings", store.Values{"siteName": store.String("Ridu")}, 0, actor)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "site-settings" || created.Revision != 1 || created.Status != store.StatusDraft || stringValue(created.Values["announcement"]) != "Welcome" {
		t.Fatalf("created global = %#v", created)
	}
	updated, err := application.Local().UpdateGlobal(context.Background(), "site-settings", store.Values{"announcement": store.String("Hello")}, created.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	published, err := application.Local().PublishGlobal(context.Background(), "site-settings", updated.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != store.StatusPublished || published.Revision != 3 {
		t.Fatalf("published global = %#v", published)
	}
	versions, err := application.Local().GlobalVersions(context.Background(), "site-settings", actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("versions = %d, want 3", len(versions))
	}
	restored, err := application.Local().RestoreGlobal(context.Background(), "site-settings", 1, published.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(restored.Values["siteName"]) != "Ridu" || stringValue(restored.Values["announcement"]) != "Welcome" {
		t.Fatalf("restored global = %#v", restored)
	}
	restoredDraft, err := application.Local().RestoreGlobalAsDraft(context.Background(), "site-settings", 3, restored.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	if restoredDraft.Status != store.StatusDraft || restoredDraft.Revision != 5 || stringValue(restoredDraft.Values["announcement"]) != "Hello" {
		t.Fatalf("restored global as draft = %#v", restoredDraft)
	}
	if want := []operation.Kind{operation.Read, operation.Update, operation.Update, operation.Publish, operation.ReadVersions, operation.Unpublish, operation.Unpublish}; !reflect.DeepEqual(operations, want) {
		t.Fatalf("hook operations = %#v, want %#v", operations, want)
	}
}

func TestGlobalRESTUsesSingletonRoutes(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name:        "Global REST",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Fields: field.Fields{field.Text("siteName").Required()},
			Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	created := requestJSON(t, client, http.MethodPatch, "http://ridu.test/api/globals/site-settings", strings.NewReader(`{"siteName":"Ridu"}`), "")
	if created.StatusCode != http.StatusOK {
		t.Fatalf("update global status = %d: %s", created.StatusCode, readBody(t, created))
	}
	var envelope struct {
		Doc map[string]any `json:"doc"`
	}
	decodeResponse(t, created, &envelope)
	if envelope.Doc["id"] != "site-settings" || envelope.Doc["siteName"] != "Ridu" || envelope.Doc["_status"] != "draft" {
		t.Fatalf("updated global = %#v", envelope.Doc)
	}
	published := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/globals/site-settings/publish", nil, "")
	if published.StatusCode != http.StatusOK {
		t.Fatalf("publish global status = %d: %s", published.StatusCode, readBody(t, published))
	}
	published.Body.Close()
	versions := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/globals/site-settings/versions", nil, "")
	if versions.StatusCode != http.StatusOK {
		t.Fatalf("global versions status = %d: %s", versions.StatusCode, readBody(t, versions))
	}
	var history struct {
		Versions []store.Version `json:"versions"`
	}
	decodeResponse(t, versions, &history)
	if len(history.Versions) != 2 {
		t.Fatalf("global versions = %d", len(history.Versions))
	}
	unknown := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/globals/missing", nil, "")
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown global status = %d", unknown.StatusCode)
	}
	unknown.Body.Close()
}

func TestGlobalVersionHistoryAppliesFilteredAccessToSnapshots(t *testing.T) {
	siteName, err := query.NewPath("siteName")
	if err != nil {
		t.Fatal(err)
	}
	filtered := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Operation != operation.ReadVersions {
			t.Fatalf("version access operation = %q", ctx.Operation)
		}
		return ridu.Where(query.Equal(siteName, query.String("Ridu"))), nil
	}
	allow := func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }
	tests := []struct {
		name   string
		access ridu.GlobalAccess
	}{
		{name: "explicit readVersions", access: ridu.GlobalAccess{Read: allow, ReadVersions: filtered}},
		{name: "fallback to read", access: ridu.GlobalAccess{Read: filtered}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application, err := ridu.New(ridu.Config{
				Name:        "Global version access",
				Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
				Globals: []ridu.Global{{
					Slug: "site-settings", Fields: field.Fields{field.Text("siteName").Required()},
					Versions: true, Access: test.access,
				}},
			}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			first, err := application.Local().UpdateGlobal(context.Background(), "site-settings", store.Values{"siteName": store.String("Ridu")}, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := application.Local().PublishGlobalChanges(context.Background(), "site-settings", store.Values{"siteName": store.String("Hidden")}, first.Revision, nil); err != nil {
				t.Fatal(err)
			}
			versions, err := application.Local().GlobalVersions(context.Background(), "site-settings", nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(versions) != 1 || versions[0].Revision != 1 || stringValue(versions[0].Snapshot.Values["siteName"]) != "Ridu" {
				t.Fatalf("filtered global versions = %#v", versions)
			}
			if version, err := application.Local().GlobalVersion(context.Background(), "site-settings", 1, nil); err != nil || version.Revision != 1 {
				t.Fatalf("matching global version = %#v, %v", version, err)
			}
			if _, err := application.Local().GlobalVersion(context.Background(), "site-settings", 2, nil); !operationCode(err, "not_found") {
				t.Fatalf("non-matching global version error = %v", err)
			}
			client := handlerClient(application.Handler(ridu.HandlerOptions{}))
			response := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/globals/site-settings/versions", nil, "")
			if response.StatusCode != http.StatusOK {
				t.Fatalf("REST version list status = %d: %s", response.StatusCode, readBody(t, response))
			}
			var history struct {
				Versions []store.Version `json:"versions"`
			}
			decodeResponse(t, response, &history)
			if len(history.Versions) != 1 || history.Versions[0].Revision != 1 {
				t.Fatalf("REST filtered global versions = %#v", history.Versions)
			}
			matching := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/globals/site-settings/versions/1", nil, "")
			if matching.StatusCode != http.StatusOK {
				t.Fatalf("REST matching version status = %d: %s", matching.StatusCode, readBody(t, matching))
			}
			matching.Body.Close()
			nonMatching := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/globals/site-settings/versions/2", nil, "")
			if nonMatching.StatusCode != http.StatusNotFound {
				t.Fatalf("REST non-matching version status = %d: %s", nonMatching.StatusCode, readBody(t, nonMatching))
			}
			nonMatching.Body.Close()
		})
	}
}

func TestGlobalFilteredAccessUsesPersistedSingletonAndCannotCreateIt(t *testing.T) {
	siteName, err := query.NewPath("siteName")
	if err != nil {
		t.Fatal(err)
	}
	filtered := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(siteName, query.String("Ridu"))), nil
	}
	allowInitialization := false
	afterChange, afterCommit := 0, 0
	application, err := ridu.New(ridu.Config{
		Name:        "Filtered global access",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Fields: field.Fields{field.Text("siteName").Required()},
			Access: ridu.GlobalAccess{
				Read: filtered,
				Update: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if allowInitialization {
						return ridu.Allow(), nil
					}
					return filtered(ctx)
				},
			},
			Hooks: ridu.CollectionHooks{
				AfterChange: []ridu.Hook{func(ridu.HookContext) error { afterChange++; return nil }},
				AfterCommit: []ridu.Hook{func(ridu.HookContext) error { afterCommit++; return nil }},
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().UpdateGlobal(context.Background(), "site-settings", store.Values{"siteName": store.String("Ridu")}, 0, nil); !operationCode(err, "not_found") {
		t.Fatalf("filtered first update error = %v", err)
	}
	if afterChange != 0 || afterCommit != 0 {
		t.Fatalf("filtered first update ran hooks: afterChange=%d afterCommit=%d", afterChange, afterCommit)
	}
	allowInitialization = true
	created, err := application.Local().UpdateGlobal(context.Background(), "site-settings", store.Values{"siteName": store.String("Ridu")}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	allowInitialization = false
	if current, err := application.Local().Global(context.Background(), "site-settings", nil); err != nil || stringValue(current.Values["siteName"]) != "Ridu" {
		t.Fatalf("matching current global = %#v, %v", current, err)
	}
	changed, err := application.Local().UpdateGlobal(context.Background(), "site-settings", store.Values{"siteName": store.String("Hidden")}, created.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Global(context.Background(), "site-settings", nil); !operationCode(err, "not_found") {
		t.Fatalf("non-matching current read error = %v", err)
	}
	beforeDeniedUpdate := afterCommit
	if _, err := application.Local().UpdateGlobal(context.Background(), "site-settings", store.Values{"siteName": store.String("Ridu")}, changed.Revision, nil); !operationCode(err, "not_found") {
		t.Fatalf("non-matching current update error = %v", err)
	}
	if afterChange != 2 || afterCommit != beforeDeniedUpdate {
		t.Fatalf("denied filtered update ran hooks: afterChange=%d afterCommit=%d (before=%d)", afterChange, afterCommit, beforeDeniedUpdate)
	}
}

func TestGlobalCapabilitiesMatchFilteredSingletonAndVersionState(t *testing.T) {
	siteName, err := query.NewPath("siteName")
	if err != nil {
		t.Fatal(err)
	}
	allowInitialization := false
	filtered := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(siteName, query.String("Ridu"))), nil
	}
	application, err := ridu.New(ridu.Config{
		Name:        "Filtered global capabilities",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields: field.Fields{field.Text("siteName").Required()},
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
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	capabilities := func() ridu.AccessCapabilities {
		t.Helper()
		result, capabilityError := application.Local().Capabilities(context.Background(), "global:site-settings", "site-settings", ridu.CapabilityOptions{})
		if capabilityError != nil {
			t.Fatal(capabilityError)
		}
		return result
	}
	assertOperations := func(stage string, got ridu.OperationCapabilities, read, readVersions, update, publish, unpublish bool) {
		t.Helper()
		if got.Read != read || got.ReadVersions != readVersions || got.Update != update || got.Publish != publish || got.Unpublish != unpublish {
			t.Fatalf("%s operations = %#v, want read=%t readVersions=%t update=%t publish=%t unpublish=%t", stage, got, read, readVersions, update, publish, unpublish)
		}
	}

	assertOperations("missing filtered singleton", capabilities().Operations, false, false, false, false, false)
	allowInitialization = true
	assertOperations("initializable singleton", capabilities().Operations, false, false, true, false, false)
	created, err := application.Local().UpdateGlobal(context.Background(), "site-settings", store.Values{"siteName": store.String("Ridu")}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	allowInitialization = false
	assertOperations("matching singleton", capabilities().Operations, true, true, true, true, true)
	published, err := application.Local().PublishGlobal(context.Background(), "site-settings", created.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := application.Local().PublishGlobalChanges(context.Background(), "site-settings", store.Values{"siteName": store.String("Hidden")}, published.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertOperations("hidden current with readable history", capabilities().Operations, false, true, false, false, false)
	if _, err := application.Local().UnpublishGlobal(context.Background(), "site-settings", hidden.Revision, nil); !operationCode(err, "not_found") {
		t.Fatalf("filtered unpublish error = %v", err)
	}
	if _, err := application.Local().RestoreGlobal(context.Background(), "site-settings", 1, hidden.Revision, nil); !operationCode(err, "not_found") {
		t.Fatalf("filtered restore error = %v", err)
	}
}

func TestMissingGlobalReadCapabilitiesMatchSyntheticReadPolicy(t *testing.T) {
	titlePath, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{
		Name:        "Missing global capabilities",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
		Globals: []ridu.Global{
			{Slug: "allowed", Fields: field.Fields{field.Text("title").Default("Default")}},
			{
				Slug: "filtered", Fields: field.Fields{field.Text("title").Default("Default")},
				Access: ridu.GlobalAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					return ridu.Where(query.Equal(titlePath, query.String("Default"))), nil
				}},
			},
			{
				Slug: "denied", Fields: field.Fields{field.Text("title").Default("Default")},
				Access: ridu.GlobalAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil }},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := application.Local().Global(context.Background(), "allowed", nil)
	if err != nil || stringValue(allowed.Values["title"]) != "Default" {
		t.Fatalf("synthetic allowed global = %#v, %v", allowed, err)
	}
	for _, test := range []struct {
		slug string
		read bool
	}{
		{slug: "allowed", read: true},
		{slug: "filtered", read: false},
		{slug: "denied", read: false},
	} {
		capabilities, capabilityError := application.Local().Capabilities(context.Background(), "global:"+test.slug, test.slug, ridu.CapabilityOptions{})
		if capabilityError != nil {
			t.Fatal(capabilityError)
		}
		if capabilities.Operations.Read != test.read {
			t.Errorf("%s missing global Read capability = %t, want %t", test.slug, capabilities.Operations.Read, test.read)
		}
	}
	if _, err := application.Local().Global(context.Background(), "filtered", nil); !operationCode(err, "not_found") {
		t.Fatalf("filtered missing global error = %v", err)
	}
	if _, err := application.Local().Global(context.Background(), "denied", nil); !operationCode(err, "access_denied") {
		t.Fatalf("denied missing global error = %v", err)
	}
}

func TestGlobalStatusCapabilitiesDoNotDependOnReadAccess(t *testing.T) {
	denyRead := func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil }
	allowUpdate := func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }
	application, err := ridu.New(ridu.Config{
		Name:        "Unreadable global mutation capabilities",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
		Globals: []ridu.Global{
			{Slug: "persisted", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title")}, Access: ridu.GlobalAccess{Read: denyRead, Update: allowUpdate}},
			{Slug: "missing", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title")}, Access: ridu.GlobalAccess{Read: denyRead, Update: allowUpdate}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	current, err := application.Local().UpdateGlobal(context.Background(), "persisted", store.Values{"title": store.String("Site")}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := application.Local().Capabilities(context.Background(), "global:persisted", "persisted", ridu.CapabilityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.Operations.Read || !capabilities.Operations.Update || !capabilities.Operations.Publish || !capabilities.Operations.Unpublish {
		t.Fatalf("persisted unreadable global capabilities = %#v", capabilities.Operations)
	}
	current, err = application.Local().UnpublishGlobal(context.Background(), "persisted", current.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().PublishGlobal(context.Background(), "persisted", current.Revision, nil); err != nil {
		t.Fatal(err)
	}
	missing, err := application.Local().Capabilities(context.Background(), "global:missing", "missing", ridu.CapabilityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !missing.Operations.Update || missing.Operations.Publish || missing.Operations.Unpublish {
		t.Fatalf("missing unreadable global capabilities = %#v", missing.Operations)
	}
}

func TestGlobalAllLocalesFilteredAccessRequiresEveryLocale(t *testing.T) {
	title, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	filtered := func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(title, query.String("Public"))), nil
	}
	application, err := ridu.New(ridu.Config{
		Name: "All-locales global access",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Versions: true,
			Fields: field.Fields{field.Text("title").Required().Localized()},
			Access: ridu.GlobalAccess{Read: filtered, ReadVersions: filtered},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	current, err := application.Local().UpdateGlobal(ctx, "site-settings", store.Values{"title": store.String("Public")}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err = application.Local().PublishGlobalChanges(ctx, "site-settings", store.Values{"title": store.String("Public")}, current.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	publicRevision := current.Revision
	current, err = application.Local().PublishGlobalChanges(ctx, "site-settings", store.Values{"title": store.String("Private")}, current.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Global(ctx, "site-settings", nil, ridu.LocaleOptions{Locale: "en"}); err != nil {
		t.Fatalf("English global read: %v", err)
	}
	if _, err := application.Local().Global(ctx, "site-settings", nil, ridu.LocaleOptions{Locale: "fr"}); !operationCode(err, "not_found") {
		t.Fatalf("French global read error = %v", err)
	}
	if _, err := application.Local().Global(ctx, "site-settings", nil, ridu.LocaleOptions{AllLocales: true}); !operationCode(err, "not_found") {
		t.Fatalf("all-locales global read error = %v", err)
	}
	versions, err := application.Local().GlobalVersions(ctx, "site-settings", nil, ridu.LocaleOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Revision != publicRevision {
		t.Fatalf("all-locales global versions = %#v", versions)
	}
	capabilities, err := application.Local().Capabilities(ctx, "global:site-settings", "site-settings", ridu.CapabilityOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.Operations.Read || !capabilities.Operations.ReadVersions || capabilities.Operations.Update {
		t.Fatalf("all-locales global capabilities = %#v", capabilities.Operations)
	}
}

func TestGlobalSlugsHaveAnIndependentNamespace(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name:        "Independent global slugs",
		Collections: []ridu.Collection{{Slug: "settings", Fields: field.Fields{field.Text("title")}}},
		Globals:     []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Text("name").Required()}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(context.Background(), "settings", store.Values{"title": store.String("Collection")}, nil); err != nil {
		t.Fatal(err)
	}
	global, err := application.Local().UpdateGlobal(context.Background(), "settings", store.Values{"name": store.String("Global")}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(global.Values["name"]) != "Global" {
		t.Fatalf("global = %#v", global)
	}
}

func stringValue(value store.Value) string {
	result, _ := value.StringValue()
	return result
}
