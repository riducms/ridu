package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestPostgresIncompleteUniqueWritesDoNotWaitForUnrelatedAdmission(t *testing.T) {
	for _, compound := range []bool{false, true} {
		name := "nullable-direct"
		fields := field.Fields{field.Text("title"), field.Text("tenant"), field.Text("slug").Unique()}
		var indexes []ridu.CollectionIndex
		if compound {
			name = "incomplete-compound"
			fields = field.Fields{field.Text("title"), field.Text("tenant"), field.Text("slug")}
			indexes = []ridu.CollectionIndex{{Fields: []string{"tenant", "slug"}, Unique: true}}
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			backend, collection := publishedUniqueAdmissionFixture(t, ridu.Config{
				Name:        "Incomplete unique admission",
				Collections: []ridu.Collection{{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: fields, Indexes: indexes}},
			})
			seed, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer seed.Rollback(context.Background())
			for _, candidate := range []struct {
				id     string
				slug   store.Value
				status store.Status
			}{{"save", store.Null(), store.StatusDraft}, {"replace", store.String("retained-live"), store.StatusPublished}} {
				if _, err := seed.Create(ctx, store.CreateRequest{
					Collection: collection, ID: candidate.id, Status: candidate.status,
					Values: store.Values{"title": store.String("Before"), "tenant": store.String("tenant"), "slug": candidate.slug},
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := seed.Commit(ctx); err != nil {
				t.Fatal(err)
			}

			// This transaction owns a real unique reservation until the test ends.
			// Incomplete writes must commit while it remains unfinished.
			holder, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(context.Background())
			if _, err := holder.Create(ctx, store.CreateRequest{
				Collection: collection, ID: "holder", Status: store.StatusDraft,
				Values: store.Values{"tenant": store.String("tenant"), "slug": store.String("occupied")},
			}); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"create", "save", "replace"} {
				t.Run(action, func(t *testing.T) {
					done := make(chan error, 1)
					go func() {
						write, err := backend.Begin(ctx)
						if err != nil {
							done <- err
							return
						}
						defer write.Rollback(context.Background())
						values := store.Values{"title": store.String("After"), "tenant": store.String("tenant")}
						var document store.Document
						if action == "create" {
							document, err = write.Create(ctx, store.CreateRequest{Collection: collection, ID: "created", Status: store.StatusDraft, Values: values})
						} else {
							document, err = conformance.LockedUpdate(ctx, write, store.UpdateRequest{
								Request: store.Request{Collection: collection, ID: action, ExpectedRevision: 1},
								Intent:  store.WriteIntentSaveDraft, Values: values, ReplaceValues: action == "replace",
							})
						}
						if err == nil && document.Values["slug"].Kind() != store.ValueNull {
							err = fmt.Errorf("incomplete write retained a working slug: %#v", document.Values["slug"])
						}
						if err == nil {
							err = write.Commit(ctx)
						}
						done <- err
					}()
					select {
					case err := <-done:
						if err != nil {
							t.Fatalf("incomplete write could not finish during unrelated admission: %v", err)
						}
					case <-ctx.Done():
						t.Fatalf("incomplete write waited for unrelated unique admission: %v", ctx.Err())
					}
				})
			}
		})
	}
}
