package contracts_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// nestedWriteBackends opens each official store this checkout can reach and
// prepares it for manifest. PostgreSQL runs when RIDU_POSTGRES_URL is set.
var nestedWriteBackends = map[string]func(*testing.T, context.Context, schema.Manifest) store.Store{
	"memory": func(*testing.T, context.Context, schema.Manifest) store.Store { return teststore.New() },
	"sqlite": func(t *testing.T, ctx context.Context, manifest schema.Manifest) store.Store {
		backend, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "nested.sqlite")})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = backend.Close() })
		if err := backend.Migrate(ctx, manifest); err != nil {
			t.Fatal(err)
		}
		return backend
	},
	"postgres": func(t *testing.T, ctx context.Context, manifest schema.Manifest) store.Store {
		databaseURL := os.Getenv("RIDU_POSTGRES_URL")
		if databaseURL == "" {
			t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL contracts")
		}
		schemaName := fmt.Sprintf("ridu_nested_%d", time.Now().UnixNano())
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
		backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: parsed.String(), AllowInsecureTransport: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(backend.Close)
		if err := backend.SyncDevelopmentSchema(ctx, manifest); err != nil {
			t.Fatal(err)
		}
		return backend
	},
}

// A hook may write the document its operation is saving through the shared
// transaction. On every store, the outer write then applies on top of the
// nested one: both persist, the revision advances once per write, each write
// keeps its own version, and the caller's expected revision still names the
// document as the outer operation found it.
func TestNestedSameDocumentWritesAcrossStores(t *testing.T) {
	draft, live := true, false
	type write func(ctx context.Context, local *ridu.LocalAPI, id string, options ridu.MutationOptions) error
	update := func(values store.Values, draft *bool) write {
		return func(ctx context.Context, local *ridu.LocalAPI, id string, options ridu.MutationOptions) error {
			options.Draft = draft
			_, err := local.Update(ctx, "posts", id, values, options)
			return err
		}
	}
	publish := func(values store.Values) write {
		return func(ctx context.Context, local *ridu.LocalAPI, id string, options ridu.MutationOptions) error {
			_, err := local.PublishChanges(ctx, "posts", id, values, options)
			return err
		}
	}
	unpublish := func(ctx context.Context, local *ridu.LocalAPI, id string, options ridu.MutationOptions) error {
		_, err := local.Unpublish(ctx, "posts", id, options)
		return err
	}
	title := store.Values{"title": store.String("outer")}
	summary := store.Values{"summary": store.String("nested")}
	scenarios := map[string]struct {
		seedPublished bool
		outer, nested write
		live          map[string]string // nil when no live head remains
		working       map[string]string
		status        store.Status
		pending       bool
	}{
		"update inside update": {
			outer: update(title, nil), nested: update(summary, nil), status: store.StatusDraft,
			working: map[string]string{"title": "outer", "summary": "nested"},
		},
		"publish inside draft update": {
			outer: update(title, &draft), nested: publish(summary), status: store.StatusPublished, pending: true,
			live:    map[string]string{"title": "seed", "summary": "nested"},
			working: map[string]string{"title": "outer", "summary": "nested"},
		},
		"unpublish inside draft update": {
			seedPublished: true, outer: update(title, &draft), nested: unpublish, status: store.StatusDraft,
			working: map[string]string{"title": "outer", "summary": "seed"},
		},
		"update inside publish": {
			outer: publish(title), nested: update(summary, nil), status: store.StatusPublished,
			live:    map[string]string{"title": "outer", "summary": "nested"},
			working: map[string]string{"title": "outer", "summary": "nested"},
		},
	}
	for backendName, open := range nestedWriteBackends {
		for name, scenario := range scenarios {
			t.Run(backendName+"/"+name, func(t *testing.T) {
				ctx := context.Background()
				var nested func(ridu.HookContext) error
				running := false
				config := ridu.Config{Name: "Nested same-document writes", Collections: []ridu.Collection{{
					Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
					Fields: field.Fields{field.Text("title"), field.Text("summary")},
					Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
						if nested == nil || running {
							return nil
						}
						running = true
						defer func() { running = false }()
						return nested(ctx)
					}}},
				}}}
				manifest, err := ridu.Resolve(config)
				if err != nil {
					t.Fatal(err)
				}
				app, err := ridu.New(config, open(t, ctx, manifest))
				if err != nil {
					t.Fatal(err)
				}
				local := app.Local()
				seedOptions := ridu.MutationOptions{System: true}
				if scenario.seedPublished {
					seedOptions.Draft = &live
				}
				seeded, err := local.Create(ctx, "posts", store.Values{"title": store.String("seed"), "summary": store.String("seed")}, seedOptions)
				if err != nil {
					t.Fatal(err)
				}
				nestedRuns := 0
				nested = func(hook ridu.HookContext) error {
					nestedRuns++
					return scenario.nested(hook.Context, hook.Local, seeded.ID, ridu.MutationOptions{System: true})
				}
				if err := scenario.outer(ctx, local, seeded.ID, ridu.MutationOptions{System: true, ExpectedRevision: seeded.Revision}); err != nil {
					var failure *ridu.OperationError
					if errors.As(err, &failure) {
						t.Fatalf("outer write: %v (cause: %v)", err, failure.Cause)
					}
					t.Fatalf("outer write: %v", err)
				}
				nested = nil
				if nestedRuns != 1 {
					t.Fatalf("nested write ran %d times", nestedRuns)
				}
				want := seeded.Revision + 2
				working, err := local.Find(ctx, "posts", seeded.ID, ridu.FindOptions{System: true, Draft: &draft})
				if err != nil {
					t.Fatal(err)
				}
				if working.Revision != want || working.Status != scenario.status || working.HasDraftChanges != scenario.pending {
					t.Fatalf("working revision/status/pending = %d/%s/%t, want %d/%s/%t", working.Revision, working.Status, working.HasDraftChanges, want, scenario.status, scenario.pending)
				}
				assertNestedWriteValues(t, "working", working, scenario.working)
				published, err := local.Find(ctx, "posts", seeded.ID, ridu.FindOptions{System: true, Draft: &live})
				var failure *ridu.OperationError
				switch {
				case scenario.live == nil && (err == nil || !errors.As(err, &failure) || failure.Code != "not_found"):
					t.Fatalf("live head = %v, %v; want none", published.Values, err)
				case scenario.live != nil && err != nil:
					t.Fatal(err)
				case scenario.live != nil:
					assertNestedWriteValues(t, "live", published, scenario.live)
				}
				versions, err := local.Versions(ctx, "posts", seeded.ID, ridu.FindOptions{System: true})
				if err != nil {
					t.Fatal(err)
				}
				revisions := make([]int, 0, len(versions))
				for _, version := range versions {
					revisions = append(revisions, version.Revision)
					if version.Revision == want {
						assertNestedWriteValues(t, "outer version", version.Snapshot, scenario.working)
					}
				}
				slices.Sort(revisions)
				if !slices.Equal(revisions, []int{seeded.Revision, seeded.Revision + 1, want}) {
					t.Fatalf("version revisions = %v", revisions)
				}
			})
		}
	}
}

func assertNestedWriteValues(t *testing.T, label string, document store.Document, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if got, _ := document.Values[name].StringValue(); got != value {
			t.Fatalf("%s %s = %q, want %q (values %v)", label, name, got, value, document.Values)
		}
	}
}
