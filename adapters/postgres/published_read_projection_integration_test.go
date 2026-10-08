package postgres

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestPostgresPublishedReadUsesLivePredicatesOrderingAndVisibility(t *testing.T) {
	ctx := t.Context()
	backend, collection := publishedReadFixture(t, field.Fields{field.Text("title"), field.Number("rank"), field.Text("audience")}, ridu.LocalizationConfig{})
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	created := make(map[string]store.Document)
	for _, item := range []struct {
		id, title, audience string
		rank, year, updated int
	}{{"a", "Zulu", "public", 20, 2002, 2003}, {"b", "Hidden", "private", 15, 2010, 2010}, {"c", "Alpha", "public", 10, 2001, 2005}} {
		document, err := write.Create(ctx, store.CreateRequest{
			Collection: collection, ID: item.id, Status: store.StatusPublished,
			CreatedAt: time.Date(item.year, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(item.updated, 1, 1, 0, 0, 0, 0, time.UTC),
			Values: store.Values{"title": store.String(item.title), "rank": store.Number(float64(item.rank)), "audience": store.String(item.audience)},
		})
		if err != nil {
			t.Fatal(err)
		}
		created[item.id] = document
	}
	for _, item := range []struct {
		id, audience string
		rank         int
	}{{"c", "private", 100}, {"a", "private", 0}, {"b", "public", 0}} {
		if _, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{
			Request: store.Request{Collection: collection, ID: item.id, ExpectedRevision: created[item.id].Revision}, Intent: store.WriteIntentSaveDraft,
			Values: store.Values{"title": store.String("Pending " + item.id), "rank": store.Number(float64(item.rank)), "audience": store.String(item.audience)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "never-published", Status: store.StatusDraft, Values: store.Values{"title": store.String("Not live"), "audience": store.String("public")}}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	access := query.Equal("audience", "public").Node()
	filter := query.GreaterThan("rank", 15).Node()
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	selected := []query.Path{publishedReadPath(t, "title")}
	page, err := read.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Access: &access, Filter: &filter, Select: selected, Page: 1, Limit: 10})
	if err != nil || *page.Total != 1 || !slices.Equal(publishedReadIDs(page), []string{"a"}) {
		t.Fatalf("live caller/access predicates = %#v, %v", page, err)
	}
	for _, item := range []struct {
		name string
		sort query.Sort
		want []string
	}{{"rank", query.Asc("rank"), []string{"c", "a"}}, {"createdAt", query.Asc("createdAt"), []string{"c", "a"}}, {"updatedAt", query.Asc("updatedAt"), []string{"a", "c"}}, {"id", query.Desc("id"), []string{"c", "a"}}} {
		page, err := read.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Access: &access, Select: selected, Sort: []query.Sort{item.sort}, Page: 1, Limit: 10})
		if err != nil || *page.Total != 2 || !slices.Equal(publishedReadIDs(page), item.want) {
			t.Fatalf("live %s ordering = %#v, %v", item.name, page, err)
		}
		for _, document := range page.Documents {
			if len(document.Values) != 1 || postgresTitle(document) != postgresTitle(created[document.ID]) || document.Revision != 1 || document.HasDraftChanges || document.PublishedRevision != 0 || !document.UpdatedAt.Equal(created[document.ID].UpdatedAt) {
				t.Fatalf("selected live output leaked working data or unselected fields: %#v", document)
			}
		}
	}
	for number, want := range []string{"c", "a"} {
		page, err := read.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Access: &access, Select: selected, Sort: []query.Sort{query.Asc("rank")}, Page: number + 1, Limit: 1})
		if err != nil || *page.Total != 2 || !slices.Equal(publishedReadIDs(page), []string{want}) {
			t.Fatalf("live ordered page %d = %#v, %v", number+1, page, err)
		}
	}
	updated := query.GreaterThan("updatedAt", "2004-01-01T00:00:00Z").Node()
	page, err = read.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Access: &access, Filter: &updated, Select: selected, Page: 1, Limit: 10})
	if err != nil || !slices.Equal(publishedReadIDs(page), []string{"c"}) {
		t.Fatalf("live updatedAt filter = %#v, %v", page, err)
	}
	status := query.And(query.Equal("id", "a"), query.Equal("_status", "published")).Node()
	page, err = read.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Filter: &status, Select: selected, Page: 1, Limit: 10})
	if err != nil || !slices.Equal(publishedReadIDs(page), []string{"a"}) {
		t.Fatalf("live ID/status filter = %#v, %v", page, err)
	}
	for _, id := range []string{"b", "never-published"} {
		if _, err := read.Find(ctx, store.Request{Collection: collection, ID: id, PublishedOnly: true, Access: &access, Select: selected}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("live read exposed %s through pending access values: %v", id, err)
		}
	}
	if document, err := read.Find(ctx, store.Request{Collection: collection, ID: "a", PublishedOnly: true, ExpectedRevision: 1, Select: selected}); err != nil || document.Revision != 1 {
		t.Fatalf("live revision selector used the working revision: %#v, %v", document, err)
	}
	if _, err := read.Find(ctx, store.Request{Collection: collection, ID: "a", PublishedOnly: true, ExpectedRevision: 2, Select: selected}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("working revision matched an immutable live read: %v", err)
	}
	_ = read.Rollback(ctx)
	write, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	if _, err := write.Trash(ctx, store.Request{Collection: collection, ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	read, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	if _, err := read.Find(ctx, store.Request{Collection: collection, ID: "a", PublishedOnly: true}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("trash left an active live head: %v", err)
	}
	trashed, err := read.Find(ctx, store.Request{Collection: collection, ID: "a", PublishedOnly: true, Deletion: store.DeletionTrash, Access: &access, Select: selected})
	if err != nil || trashed.DeletedAt == nil || postgresTitle(trashed) != "Zulu" {
		t.Fatalf("trashed live selection = %#v, %v", trashed, err)
	}
	_ = read.Rollback(ctx)
	write, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	if _, err := write.Restore(ctx, store.Request{Collection: collection, ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	read, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	restored, err := read.Find(ctx, store.Request{Collection: collection, ID: "a", PublishedOnly: true, Access: &access, Select: selected})
	if err != nil || restored.DeletedAt != nil || postgresTitle(restored) != "Zulu" || restored.Revision != 1 {
		t.Fatalf("restored live selection = %#v, %v", restored, err)
	}
}

func TestPostgresPublishedReadUsesLiveExactLocaleAndFallbackPredicates(t *testing.T) {
	ctx := t.Context()
	backend, collection := publishedReadFixture(t, field.Fields{
		field.Text("title").Localized(), field.Number("rank").Localized(), field.Group("details", field.Fields{field.Text("caption").Localized()}),
	}, ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}})
	locales := []schema.LocaleCode{"en", "fr"}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	for _, item := range []struct {
		id, english, french, caption, pending string
		rank, pendingRank                     int
	}{{"a", "Zulu", "", "eligible", "Aardvark", 20, 1}, {"b", "Alpha", "Bravo", "private", "Zzz", 10, 100}} {
		created, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: item.id, Status: store.StatusPublished, Locales: locales, Values: store.Values{
			"title": store.Object(store.Values{"en": store.String(item.english), "fr": store.String(item.french)}),
			"rank":  store.Object(store.Values{"en": store.Number(float64(item.rank))}),
			"details": store.Object(store.Values{"caption": store.Object(store.Values{
				"en": store.String(item.caption), "fr": store.String(""),
			})}),
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: created.Revision, Locales: locales}, Intent: store.WriteIntentSaveDraft, Values: store.Values{
			"title":   store.Object(store.Values{"fr": store.String(item.pending)}),
			"rank":    store.Object(store.Values{"fr": store.Number(float64(item.pendingRank))}),
			"details": store.Object(store.Values{"caption": store.Object(store.Values{"fr": store.String("pending")})}),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	filter := query.Equal("title", "Zulu").Node()
	for _, item := range []struct {
		chain []schema.LocaleCode
		want  []string
	}{{[]schema.LocaleCode{"fr"}, nil}, {[]schema.LocaleCode{"fr", "en"}, []string{"a"}}} {
		page, err := read.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Locales: locales, LocaleChain: item.chain, Filter: &filter, Select: []query.Path{publishedReadPath(t, "title")}, Page: 1, Limit: 10})
		if err != nil || !slices.Equal(publishedReadIDs(page), item.want) {
			t.Fatalf("live locale chain %v filter = %#v, %v", item.chain, page, err)
		}
	}
	access := query.Equal("details.caption", "eligible").Node()
	page, err := read.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Locales: locales, LocaleChain: []schema.LocaleCode{"fr", "en"}, Access: &access, Select: []query.Path{publishedReadPath(t, "title")}, Page: 1, Limit: 10})
	if err != nil || *page.Total != 1 || !slices.Equal(publishedReadIDs(page), []string{"a"}) {
		t.Fatalf("live nested localized access predicate = %#v, %v", page, err)
	}
	for _, name := range []string{"title", "rank"} {
		page, err := read.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Locales: locales, LocaleChain: []schema.LocaleCode{"fr", "en"}, Select: []query.Path{publishedReadPath(t, "title")}, Sort: []query.Sort{query.Asc(name)}, Page: 1, Limit: 10})
		if err != nil || !slices.Equal(publishedReadIDs(page), []string{"b", "a"}) {
			t.Fatalf("live localized %s ordering = %#v, %v", name, page, err)
		}
	}
	document, err := read.Find(ctx, store.Request{Collection: collection, ID: "a", PublishedOnly: true, Locales: locales, LocaleChain: []schema.LocaleCode{"fr", "en"}, Select: []query.Path{publishedReadPath(t, "title")}})
	english, _ := document.Values["title"].Get("en").StringValue()
	french, _ := document.Values["title"].Get("fr").StringValue()
	if err != nil || english != "Zulu" || french != "" || len(document.Values) != 1 {
		t.Fatalf("selected canonical locale values = %#v, %v", document, err)
	}
}

func TestPostgresPublishedSelectionReadsOnlySelectedColumns(t *testing.T) {
	ctx := t.Context()
	backend, collection := publishedReadFixture(t, field.Fields{
		field.Text("title"), field.Text("nullable"), field.Text("absent"), field.JSON("heavy"), field.Group("details", field.Fields{field.Text("label"), field.Text("private")}),
	}, ridu.LocalizationConfig{})
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	if _, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "selected", Status: store.StatusPublished, Values: store.Values{
		"title": store.String("Visible"), "nullable": store.Null(), "heavy": store.Object(store.Values{"valid": store.Boolean(true)}),
		"details": store.Object(store.Values{"label": store.String("Selected child"), "private": store.String("Do not return")}),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// PostgreSQL JSONB can retain numeric values outside the transport's finite
	// number contract. A field projection must not read an unrelated column.
	heavy := fieldColumn(fieldNamed(collection.Fields, "heavy").ID)
	if _, err := backend.pool.Exec(ctx, `UPDATE `+quote(publishedCollectionTable(collection.ID))+` SET `+quote(heavy)+` = '{"oversized":1e1000}'::jsonb WHERE id = $1`, "selected"); err != nil {
		t.Fatal(err)
	}
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	selected := []query.Path{publishedReadPath(t, "title"), publishedReadPath(t, "nullable"), publishedReadPath(t, "absent")}
	document, err := read.Find(ctx, store.Request{Collection: collection, ID: "selected", PublishedOnly: true, Select: selected})
	if err != nil || postgresTitle(document) != "Visible" || len(document.Values) != 3 {
		t.Fatalf("selected live fields = %#v, %v", document, err)
	}
	// Typed columns store an absent value as NULL, exactly like the working
	// table, so both heads project the same selection.
	working, err := read.Find(ctx, store.Request{Collection: collection, ID: "selected", Select: selected})
	if err != nil || !reflect.DeepEqual(working.Values, document.Values) {
		t.Fatalf("working selection = %#v, %v; live = %#v", working.Values, err, document.Values)
	}
	for _, name := range []string{"nullable", "absent"} {
		if value, exists := document.Values[name]; !exists || value.Kind() != store.ValueNull {
			t.Fatalf("selection did not return %s as null: %#v", name, document.Values)
		}
	}
	page, err := read.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Select: selected, Page: 1, Limit: 10})
	if err != nil || *page.Total != 1 || len(page.Documents) != 1 || len(page.Documents[0].Values) != 3 {
		t.Fatalf("selected live list = %#v, %v", page, err)
	}
	metadata, err := read.Find(ctx, store.Request{Collection: collection, ID: "selected", PublishedOnly: true, Select: []query.Path{}})
	if err != nil || len(metadata.Values) != 0 || metadata.Revision != 1 || metadata.Status != store.StatusPublished {
		t.Fatalf("metadata-only live selection = %#v, %v", metadata, err)
	}
	child, err := read.Find(ctx, store.Request{Collection: collection, ID: "selected", PublishedOnly: true, Select: []query.Path{publishedReadPath(t, "details")}})
	label, _ := child.Values["details"].Get("label").StringValue()
	if err != nil || len(child.Values) != 1 || child.Values["details"].Len() != 2 || label != "Selected child" {
		t.Fatalf("selected live group = %#v, %v", child, err)
	}
	if _, err := read.Find(ctx, store.Request{Collection: collection, ID: "selected", PublishedOnly: true}); err == nil {
		t.Fatal("full selection silently accepted the malformed live JSON column")
	}
}

func TestPostgresPublishedReadIDOptimizationPreservesVersionAccess(t *testing.T) {
	ctx := t.Context()
	backend, collection := publishedReadFixture(t, field.Fields{field.Text("title")}, ridu.LocalizationConfig{})
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	created, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "owner", Status: store.StatusPublished, Values: store.Values{"title": store.String("Live")}})
	if err != nil {
		t.Fatal(err)
	}
	versions := write.(store.VersionTransaction)
	if _, err := versions.SaveVersion(ctx, collection, created, 20); err != nil {
		t.Fatal(err)
	}
	staged, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: created.Revision}, Intent: store.WriteIntentSaveDraft, Values: store.Values{"title": store.String("Pending")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := versions.SaveVersion(ctx, collection, staged, 20); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	for _, item := range []struct {
		id   string
		want int
	}{{"owner", 1}, {"other", 0}} {
		access := query.And(query.Equal("id", item.id), query.Equal("title", "Live")).Node()
		request := store.VersionRequest{Collection: collection, DocumentID: created.ID, Access: &access}
		count, err := read.(store.VersionTransaction).CountVersions(ctx, request)
		if err != nil || count != item.want {
			t.Fatalf("version count under ID/snapshot access = %d, %v; want %d", count, err, item.want)
		}
		history, err := read.(store.VersionTransaction).ListVersions(ctx, request)
		if err != nil || len(history) != item.want {
			t.Fatalf("version list under ID/snapshot access = %#v, %v", history, err)
		}
		if len(history) == 1 && (history[0].Revision != 1 || postgresTitle(history[0].Snapshot) != "Live") {
			t.Fatalf("version access used current working values: %#v", history)
		}
	}
}

func TestPostgresLockedPublishedReadStillLocksWorkingDocument(t *testing.T) {
	for _, lock := range []store.LockMode{store.LockReference, store.LockMutation} {
		t.Run(string(lock), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			backend, collection := publishedReadFixture(t, field.Fields{field.Text("title")}, ridu.LocalizationConfig{})
			write, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer write.Rollback(ctx)
			created, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "target", Status: store.StatusPublished, Values: store.Values{"title": store.String("Live")}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: created.Revision}, Intent: store.WriteIntentSaveDraft, Values: store.Values{"title": store.String("Pending")}}); err != nil {
				t.Fatal(err)
			}
			if err := write.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			owner, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Rollback(context.Background())
			if document, err := owner.Find(ctx, store.Request{Collection: collection, ID: "target", PublishedOnly: true, ExpectedRevision: created.Revision, Lock: lock, Select: []query.Path{}}); err != nil || document.Revision != created.Revision {
				t.Fatalf("locked live revision read = %#v, %v", document, err)
			}
			mutator, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer mutator.Rollback(context.Background())
			var pid int
			if err := mutator.(*documentTransaction).transaction.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				_, err := mutator.Find(ctx, store.Request{Collection: collection, ID: "target", Lock: store.LockMutation, Select: []query.Path{}})
				result <- err
			}()
			defer func() {
				_ = owner.Rollback(context.Background())
				cancel()
				<-finished
			}()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case err := <-result:
					t.Fatalf("working mutation lock bypassed the live %s lock: %v", lock, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-ticker.C:
				}
				var blocked bool
				if err := backend.pool.QueryRow(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0", pid).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
			}
			if err := owner.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if err != nil {
					t.Fatalf("working mutation lock after live lock release: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func publishedReadFixture(t *testing.T, fields field.Fields, localization ridu.LocalizationConfig) (*Store, schema.Collection) {
	t.Helper()
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Published read projection", Localization: localization,
		Collections: []ridu.Collection{{Slug: "posts", Trash: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: fields}},
	})
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

func publishedReadPath(t *testing.T, value string) query.Path {
	t.Helper()
	path, err := query.ParsePath(value)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func publishedReadIDs(page store.Page) []string {
	ids := make([]string, len(page.Documents))
	for index, document := range page.Documents {
		ids[index] = document.ID
	}
	return ids
}
