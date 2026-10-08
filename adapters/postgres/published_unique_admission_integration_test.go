package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestPostgresPublishedUniqueAdmissionUsesExactLocales(t *testing.T) {
	for _, compound := range []bool{false, true} {
		name := "direct"
		fields := field.Fields{field.Text("code").Localized().Unique()}
		var indexes []ridu.CollectionIndex
		if compound {
			name = "nested-compound"
			fields = field.Fields{field.Text("tenant"), field.Group("seo", field.Fields{field.Text("code").Localized()})}
			indexes = []ridu.CollectionIndex{{Fields: []string{"tenant", "seo.code"}, Unique: true}}
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			backend, collection := publishedUniqueAdmissionFixture(t, ridu.Config{
				Name: "Published unique exact locales",
				Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
					{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
				}},
				Collections: []ridu.Collection{{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: fields, Indexes: indexes}},
			})
			locales := []schema.LocaleCode{"en", "fr"}
			values := func(locale schema.LocaleCode, code string) store.Values {
				localized := store.Object(store.Values{string(locale): store.String(code)})
				if compound {
					return store.Values{"tenant": store.String("tenant"), "seo": store.Object(store.Values{"code": localized})}
				}
				return store.Values{"code": localized}
			}
			write, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer write.Rollback(ctx)
			first, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "first", Status: store.StatusPublished, Values: values("en", "shared"), Locales: locales})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: first.ID, ExpectedRevision: first.Revision, Locales: locales}, Intent: store.WriteIntentSaveDraft, Values: values("en", "pending")}); err != nil {
				t.Fatal(err)
			}
			if err := write.Commit(ctx); err != nil {
				t.Fatal(err)
			}

			write, err = backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer write.Rollback(ctx)
			other, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "other-locale", Status: store.StatusDraft, Values: values("fr", "shared"), Locales: locales})
			if err != nil {
				t.Fatalf("English live value conflicted with a French exact translation: %v", err)
			}
			if err := write.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			for _, code := range []string{"shared", "pending"} {
				probe, err := backend.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				_, err = probe.Create(ctx, store.CreateRequest{Collection: collection, ID: "collision-" + code, Status: store.StatusDraft, Values: values("en", code), Locales: locales})
				_ = probe.Rollback(ctx)
				if !errors.Is(err, store.ErrConflict) {
					t.Fatalf("create claimed reserved English %q: %v", code, err)
				}
			}
			probe, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = conformance.LockedUpdate(ctx, probe, store.UpdateRequest{Request: store.Request{Collection: collection, ID: other.ID, ExpectedRevision: other.Revision, Locales: locales}, Intent: store.WriteIntentSaveDraft, Values: values("en", "shared")})
			_ = probe.Rollback(ctx)
			if !errors.Is(err, store.ErrConflict) {
				t.Fatalf("draft update claimed retained English live value: %v", err)
			}
		})
	}
}

func TestPostgresPublishedUniqueConcurrentRestoreRejectsSharedLiveTuple(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	backend, collection := publishedUniqueAdmissionFixture(t, ridu.Config{
		Name:        "Concurrent published unique restore",
		Collections: []ridu.Collection{{Slug: "posts", Trash: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("slug").Unique()}}},
	})
	for _, candidate := range []struct{ id, working string }{{"first", "working-first"}, {"second", "working-second"}} {
		write, err := backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer write.Rollback(ctx)
		created, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: candidate.id, Status: store.StatusPublished, Values: store.Values{"slug": store.String("shared-live")}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: created.Revision}, Intent: store.WriteIntentSaveDraft, Values: store.Values{"slug": store.String(candidate.working)}}); err != nil {
			t.Fatal(err)
		}
		if _, err := write.Trash(ctx, store.Request{Collection: collection, ID: created.ID}); err != nil {
			t.Fatal(err)
		}
		if err := write.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	first, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback(context.Background())
	if _, err := first.Restore(ctx, store.Request{Collection: collection, ID: "first"}); err != nil {
		t.Fatal(err)
	}
	second, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback(context.Background())
	var secondPID int
	if err := second.(*documentTransaction).transaction.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	secondResult := make(chan error, 1)
	secondFinished := make(chan struct{})
	go func() {
		defer close(secondFinished)
		_, err := second.Restore(ctx, store.Request{Collection: collection, ID: "second"})
		if err == nil {
			err = second.Commit(ctx)
		} else {
			_ = second.Rollback(ctx)
		}
		secondResult <- err
	}()
	defer func() {
		_ = first.Rollback(context.Background())
		cancel()
		<-secondFinished
	}()
	// Both working keys differ. Admission must still coordinate their shared
	// live key, before either transaction can commit a conflicting restore.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-secondResult:
			t.Fatalf("second restore completed before the first admission committed: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
		var blocked bool
		if err := backend.pool.QueryRow(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0", secondPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
	}
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-secondResult:
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("second restore of retained live key = %v, want conflict", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	for _, publishedOnly := range []bool{false, true} {
		if _, err := read.Find(ctx, store.Request{Collection: collection, ID: "second", PublishedOnly: publishedOnly}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("failed restore exposed second head publishedOnly=%t: %v", publishedOnly, err)
		}
	}
	working, err := read.Find(ctx, store.Request{Collection: collection, ID: "second", Deletion: store.DeletionTrash})
	slug, _ := working.Values["slug"].StringValue()
	if err != nil || slug != "working-second" {
		t.Fatalf("failed restore changed trashed working state: %#v, %v", working, err)
	}
}

func TestPostgresPublishedUniqueRestoreChecksLiveWithNullWorkingTuple(t *testing.T) {
	ctx := t.Context()
	backend, collection := publishedUniqueAdmissionFixture(t, ridu.Config{
		Name: "Restore incomplete working unique tuple",
		Collections: []ridu.Collection{{
			Slug: "posts", Trash: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields:  field.Fields{field.Text("tenant"), field.Group("seo", field.Fields{field.Text("slug")})},
			Indexes: []ridu.CollectionIndex{{Fields: []string{"tenant", "seo.slug"}, Unique: true}},
		}},
	})
	values := store.Values{"tenant": store.String("tenant"), "seo": store.Object(store.Values{"slug": store.String("live")})}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	created, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "first", Status: store.StatusPublished, Values: values})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: created.Revision},
		Intent:  store.WriteIntentSaveDraft, Values: store.Values{"seo": store.Object(store.Values{"slug": store.Null()})},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := write.Trash(ctx, store.Request{Collection: collection, ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	write, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	if _, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "competitor", Status: store.StatusDraft, Values: values}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	probe, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = probe.Restore(ctx, store.Request{Collection: collection, ID: created.ID})
	_ = probe.Rollback(ctx)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("incomplete working tuple bypassed retained live tuple conflict: %v", err)
	}
}

func publishedUniqueAdmissionFixture(t *testing.T, config ridu.Config) (*Store, schema.Collection) {
	t.Helper()
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend := migrationArtifactTestBackend(t)
	directory := t.TempDir()
	if _, err := CreateArtifact(t.Context(), directory, "initial", manifest, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(t.Context(), directory); err != nil {
		t.Fatal(err)
	}
	return backend, manifest.Snapshot().Collections[0]
}
