package core_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// Make full history reads fail so a passing count or capability check proves
// that neither path materializes snapshots through ListVersions.
type versionCountStore struct {
	*teststore.Store
	counts int
	fail   bool
}

func (backend *versionCountStore) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.BeginSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return &versionCountTransaction{
		Transaction: transaction, VersionTransaction: transaction.(store.VersionTransaction), backend: backend,
	}, nil
}

type versionCountTransaction struct {
	store.Transaction
	store.VersionTransaction
	backend *versionCountStore
}

func (*versionCountTransaction) ListVersions(context.Context, store.VersionRequest) ([]store.Version, error) {
	return nil, errors.New("count-only read tried to load snapshots")
}

func (transaction *versionCountTransaction) CountVersions(ctx context.Context, request store.VersionRequest) (int, error) {
	transaction.backend.counts++
	if transaction.backend.fail {
		return 0, errors.New("version count query failed")
	}
	return transaction.VersionTransaction.CountVersions(ctx, request)
}

func TestVersionCountUsesRetainedSnapshotAccessWithoutHistoryReads(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "collection"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			backend := &versionCountStore{Store: teststore.New()}
			owner, err := query.NewPath("owner")
			if err != nil {
				t.Fatal(err)
			}
			access := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				if ctx.Operation != operation.ReadVersions {
					t.Fatalf("count access operation = %q", ctx.Operation)
				}
				if ctx.Actor == nil {
					return ridu.Deny(), nil
				}
				return ridu.Where(query.Equal(owner, ctx.Actor.ID)), nil
			}
			var phases []string
			record := func(phase string) ridu.Hook {
				return func(ctx ridu.HookContext) error {
					if ctx.Operation == operation.ReadVersions {
						phases = append(phases, phase)
					}
					return nil
				}
			}
			hooks := ridu.CollectionHooks{
				BeforeRead: []ridu.Hook{record("beforeRead")}, BeforeOperation: []ridu.Hook{record("beforeOperation")},
				AfterOperation: []ridu.Hook{record("afterOperation")}, AfterError: []ridu.Hook{record("afterError")},
				AfterRead: []ridu.Hook{func(ridu.HookContext) error {
					return errors.New("count-only read tried to resolve snapshot output")
				}},
			}
			fields := field.Fields{field.Text("owner").Required()}
			config := ridu.Config{Name: "Retained version counts"}
			if global {
				config.Collections = []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}
				config.Globals = []ridu.Global{{Slug: "settings", Versions: true, VersionConfig: ridu.VersionConfig{MaxPerDocument: 3},
					Fields: fields, Access: ridu.GlobalAccess{ReadVersions: access}, Hooks: hooks}}
			} else {
				config.Collections = []ridu.Collection{{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{MaxPerDocument: 3},
					Fields: fields, Access: ridu.CollectionAccess{ReadVersions: access}, Hooks: hooks}}
			}
			application, err := ridu.New(config, backend)
			if err != nil {
				t.Fatal(err)
			}
			// Keep AfterRead failures specific to the reads under test. Mutations
			// also run those hooks while producing their response documents.
			// Seed through an application without read hooks over the same store.
			if global {
				config.Globals[0].Hooks = ridu.CollectionHooks{}
			} else {
				config.Collections[0].Hooks = ridu.CollectionHooks{}
			}
			writer, err := ridu.New(config, backend)
			if err != nil {
				t.Fatal(err)
			}
			var document store.Document
			for index, id := range []string{"owner-a", "owner-b", "owner-a", "owner-b"} {
				values := store.Values{"owner": store.String(id)}
				options := ridu.MutationOptions{ExpectedRevision: document.Revision}
				if global {
					if index == 0 {
						document, err = writer.Local().UpdateGlobal(t.Context(), "settings", values, options)
					} else {
						document, err = writer.Local().PublishGlobalChanges(t.Context(), "settings", values, options)
					}
				} else if index == 0 {
					document, err = writer.Local().Create(t.Context(), "posts", values, options)
				} else {
					document, err = writer.Local().PublishChanges(t.Context(), "posts", document.ID, values, options)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if document.Revision != 4 {
				t.Fatalf("revision = %d, want 4", document.Revision)
			}
			count := func(actor *store.Document) (int, error) {
				options := ridu.FindOptions{Actor: actor}
				if global {
					return application.Local().CountGlobalVersions(t.Context(), "settings", options)
				}
				return application.Local().CountVersions(t.Context(), "posts", document.ID, options)
			}
			for _, test := range []struct {
				owner string
				want  int
			}{{"owner-a", 1}, {"owner-b", 2}, {"unrelated", 0}} {
				phases = nil
				total, err := count(&store.Document{ID: test.owner})
				if err != nil || total != test.want {
					t.Fatalf("%s retained count = %d, %v; want %d", test.owner, total, err, test.want)
				}
				if !reflect.DeepEqual(phases, []string{"beforeRead", "beforeOperation", "afterOperation"}) {
					t.Fatalf("count hook phases = %v", phases)
				}
				before := backend.counts
				var capabilities ridu.AccessCapabilities
				if global {
					capabilities, err = application.Local().Capabilities(t.Context(), "global:settings", "settings", ridu.CapabilityOptions{Actor: &store.Document{ID: test.owner}})
				} else {
					capabilities, err = application.Local().Capabilities(t.Context(), "posts", document.ID, ridu.CapabilityOptions{Actor: &store.Document{ID: test.owner}})
				}
				if err != nil || capabilities.Operations.ReadVersions != (test.want > 0) || backend.counts != before+1 {
					t.Fatalf("%s version capability = %#v, %v; count query calls %d -> %d", test.owner, capabilities, err, before, backend.counts)
				}
			}
			before := backend.counts
			if _, err := count(nil); !operationCode(err, "access_denied") || backend.counts != before {
				t.Fatalf("denied count = %v; count query calls %d -> %d", err, before, backend.counts)
			}
			backend.fail = true
			phases = nil
			if _, err := count(&store.Document{ID: "owner-a"}); !operationCode(err, "store_failed") {
				t.Fatalf("query failure = %v", err)
			}
			if !reflect.DeepEqual(phases, []string{"beforeRead", "beforeOperation", "afterError"}) {
				t.Fatalf("failed count hook phases = %v", phases)
			}
			if events := backend.Events(); events[len(events)-1] != "rollback" {
				t.Fatalf("failed count transaction events = %v", events)
			}
		})
	}
}
