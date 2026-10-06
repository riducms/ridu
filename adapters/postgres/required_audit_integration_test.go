package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/requiredfield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func requiredAuditManifest(t *testing.T, collection core.Collection) schema.Manifest {
	t.Helper()
	manifest, err := core.Resolve(core.Config{
		Name: "Required audit",
		Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []core.Collection{collection},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func requiredAuditWrite(t *testing.T, ctx context.Context, backend *Store, write func(store.Transaction) error) {
	t.Helper()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := write(transaction); err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func requireMissingValues(t *testing.T, err error, fragments ...string) {
	t.Helper()
	var failure *requiredfield.MissingValuesError
	if !errors.As(err, &failure) || !strings.Contains(err.Error(), requiredfield.Code) {
		t.Fatalf("schema change error = %v, want %s", err, requiredfield.Code)
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("schema change error %q does not mention %q", err, fragment)
		}
	}
}

// Making a field required audits every stored place it can occur. Each case
// stores documents under the optional schema, then a migration or development
// synchronization that requires the field is refused with the address, locale,
// count and example IDs, and leaves the schema unchanged.
func TestPostgresRequiredValueAuditRefusesMissingValues(t *testing.T) {
	for _, test := range []struct {
		name      string
		before    core.Collection
		after     core.Collection
		documents []store.Values
		fragments []string
	}{
		{
			name:      "top-level field",
			before:    core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary")}},
			after:     core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary").Required()}},
			documents: []store.Values{{"title": store.String("one")}, {"title": store.String("two"), "summary": store.String("")}, {"title": store.String("three"), "summary": store.String("kept")}},
			fragments: []string{"posts.summary in 2 documents, for example doc-0, doc-1"},
		},
		{
			name:      "added required field",
			before:    core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title")}},
			after:     core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Number("rank").Required()}},
			documents: []store.Values{{"title": store.String("one")}},
			fragments: []string{"posts.rank in 1 document, for example doc-0"},
		},
		{
			name:   "group child",
			before: core.Collection{Slug: "posts", Fields: field.Fields{field.Group("seo", field.Fields{field.Text("title")})}},
			after:  core.Collection{Slug: "posts", Fields: field.Fields{field.Group("seo", field.Fields{field.Text("title").Required()})}},
			documents: []store.Values{
				{"seo": store.Object(store.Values{"title": store.String("kept")})},
				{"seo": store.Object(store.Values{})},
				{},
			},
			fragments: []string{"posts.seo.title in 1 document, for example doc-1"},
		},
		{
			name:   "array row child",
			before: core.Collection{Slug: "posts", Fields: field.Fields{field.Array("items", field.Fields{field.Text("caption")})}},
			after:  core.Collection{Slug: "posts", Fields: field.Fields{field.Array("items", field.Fields{field.Text("caption").Required()})}},
			documents: []store.Values{{"items": store.List(
				store.Object(store.Values{"_key": store.String("a"), "caption": store.String("kept")}),
				store.Object(store.Values{"_key": store.String("b")}),
			)}},
			fragments: []string{"posts.items.caption in 1 document, for example doc-0"},
		},
		{
			name: "block child",
			before: core.Collection{Slug: "posts", Fields: field.Fields{field.Blocks("layout",
				field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}},
				field.Block{Slug: "quote", Fields: field.Fields{field.Text("heading")}},
			)}},
			after: core.Collection{Slug: "posts", Fields: field.Fields{field.Blocks("layout",
				field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading").Required()}},
				field.Block{Slug: "quote", Fields: field.Fields{field.Text("heading")}},
			)}},
			documents: []store.Values{
				{"layout": store.List(store.Object(store.Values{"_key": store.String("a"), "blockType": store.String("quote")}))},
				{"layout": store.List(store.Object(store.Values{"_key": store.String("b"), "blockType": store.String("hero")}))},
			},
			fragments: []string{"block hero.heading in 1 document, for example doc-1"},
		},
		{
			name:   "localized field per locale",
			before: core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title").Localized()}},
			after:  core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title").Localized().Required()}},
			documents: []store.Values{
				{"title": store.Object(store.Values{"en": store.String("English only")})},
				{"title": store.Object(store.Values{"en": store.String("English"), "fr": store.String("")})},
				{"title": store.Object(store.Values{})},
			},
			fragments: []string{"posts.title (no translation in any locale) in 1 document, for example doc-2", "posts.title (locale fr) in 1 document, for example doc-1"},
		},
		{
			name:   "localized group child",
			before: core.Collection{Slug: "posts", Fields: field.Fields{field.Group("seo", field.Fields{field.Text("title")}).Localized()}},
			after:  core.Collection{Slug: "posts", Fields: field.Fields{field.Group("seo", field.Fields{field.Text("title").Required()}).Localized()}},
			documents: []store.Values{
				{"seo": store.Object(store.Values{"en": store.Object(store.Values{"title": store.String("kept")}), "fr": store.Object(store.Values{})})},
			},
			fragments: []string{"posts.seo.title (locale fr) in 1 document, for example doc-0"},
		},
	} {
		for _, mode := range []string{"artifact", "development"} {
			t.Run(test.name+"/"+mode, func(t *testing.T) {
				ctx := t.Context()
				backend := migrationArtifactTestBackend(t)
				before := requiredAuditManifest(t, test.before)
				after := requiredAuditManifest(t, test.after)
				directory := t.TempDir()
				if mode == "artifact" {
					if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), nil, false); err != nil {
						t.Fatal(err)
					}
					if err := backend.ApplyArtifacts(ctx, directory); err != nil {
						t.Fatal(err)
					}
				} else if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
					t.Fatal(err)
				}
				collection := before.Snapshot().Collections[0]
				requiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
					for index, values := range test.documents {
						if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "doc-" + string(rune('0'+index)), Values: values, Locales: []schema.LocaleCode{"en", "fr"}}); err != nil {
							return err
						}
					}
					return nil
				})
				if mode == "development" {
					requireMissingValues(t, backend.SyncDevelopmentSchema(ctx, after), append(test.fragments, "Kept the current schema")...)
					recorded, exists, err := backend.DevelopmentManifest(ctx)
					if err != nil || !exists || !recorded.Equal(before) {
						t.Fatalf("refused synchronization changed the recorded schema: %t, %v", exists, err)
					}
					return
				}
				created, err := CreateArtifact(ctx, directory, "require", after, time.Unix(2, 0), nil, false)
				if err != nil {
					t.Fatal(err)
				}
				audits := 0
				for _, risk := range created.Artifact.Risks {
					if risk.Code == requiredfield.RiskCode {
						audits++
					}
				}
				if audits == 0 || !artifactHasStep(created.Artifact, ridumigration.StepAuditRequiredValues) {
					t.Fatalf("artifact does not plan the audit: %#v", created.Artifact.Risks)
				}
				requireMissingValues(t, backend.ApplyArtifacts(ctx, directory), append(test.fragments, "ridu migrate create <name> --transform <transform>")...)
				statuses, err := backend.ArtifactStatus(ctx, directory)
				if err != nil || len(statuses) != 2 || statuses[1].Applied {
					t.Fatalf("refused artifact state = %#v, %v", statuses, err)
				}
				for _, phase := range statuses[1].Phases {
					if phase.State != "pending" {
						t.Fatalf("refused artifact committed phase %s before its audit: %#v", phase.ID, statuses[1].Phases)
					}
				}
			})
		}
	}
}

func artifactHasStep(artifact ridumigration.Artifact, kind ridumigration.StepKind) bool {
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind == kind {
				return true
			}
		}
	}
	return false
}

// Drafts defer required fields, so a draft working row may stay incomplete.
// Its published row may not: publication validated the field before it was
// required, and readers see the published row.
func TestPostgresRequiredValueAuditExemptsDraftsButNotPublishedRows(t *testing.T) {
	ctx := t.Context()
	versioned := func(summary field.Node) core.Collection {
		return core.Collection{Slug: "posts", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title"), summary}}
	}
	before := requiredAuditManifest(t, versioned(field.Text("summary")))
	after := requiredAuditManifest(t, versioned(field.Text("summary").Required()))
	collection := before.Snapshot().Collections[0]
	for _, test := range []struct {
		name    string
		prepare func(store.Transaction) error
		missing string
	}{
		{name: "incomplete draft working row", prepare: func(transaction store.Transaction) error {
			_, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "draft", Status: store.StatusDraft, Values: store.Values{"title": store.String("Draft")}})
			if err != nil {
				return err
			}
			published, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "published", Status: store.StatusPublished, Values: store.Values{"title": store.String("Live"), "summary": store.String("Complete")}})
			if err != nil {
				return err
			}
			_, err = conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
				Request: store.Request{Collection: collection, ID: "published", ExpectedRevision: published.Revision},
				Intent:  store.WriteIntentSaveDraft, Values: store.Values{"summary": store.Null()},
			})
			return err
		}},
		{name: "incomplete published row", missing: "posts.summary in 1 document, for example live", prepare: func(transaction store.Transaction) error {
			published, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "live", Status: store.StatusPublished, Values: store.Values{"title": store.String("Live")}})
			if err != nil {
				return err
			}
			_, err = conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
				Request: store.Request{Collection: collection, ID: "live", ExpectedRevision: published.Revision},
				Intent:  store.WriteIntentSaveDraft, Values: store.Values{"summary": store.String("Only in the draft")},
			})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := migrationArtifactTestBackend(t)
			directory := t.TempDir()
			if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), nil, false); err != nil {
				t.Fatal(err)
			}
			if err := backend.ApplyArtifacts(ctx, directory); err != nil {
				t.Fatal(err)
			}
			requiredAuditWrite(t, ctx, backend, test.prepare)
			if _, err := CreateArtifact(ctx, directory, "require", after, time.Unix(2, 0), nil, false); err != nil {
				t.Fatal(err)
			}
			err := backend.ApplyArtifacts(ctx, directory)
			if test.missing == "" {
				if err != nil {
					t.Fatalf("an incomplete draft blocked the migration: %v", err)
				}
				return
			}
			requireMissingValues(t, err, test.missing)
		})
	}
}

// A data transform bound to the same migration backfills the values before
// the audit runs, and the NOT NULL constraint the audit admits follows it in
// the same transaction.
func TestPostgresRequiredValueAuditAdmitsBackfillingTransform(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	directory := t.TempDir()
	before := requiredAuditManifest(t, core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Group("seo", field.Fields{field.Text("title")})}})
	after := requiredAuditManifest(t, core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title").Required(), field.Group("seo", field.Fields{field.Text("title").Required()})}})
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collection := before.Snapshot().Collections[0]
	requiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
		for _, id := range []string{"one", "two"} {
			if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: id, Values: store.Values{"seo": store.Object(store.Values{})}}); err != nil {
				return err
			}
		}
		return nil
	})
	descriptor := ridumigration.DataTransformDescriptor{Name: "backfill-titles", Checksum: ridumigration.DataTransformChecksum([]byte("backfill-titles-v1"))}
	created, err := CreateArtifact(ctx, directory, "require-titles", after, time.Unix(2, 0), nil, false, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	final := created.Artifact.Phases[len(created.Artifact.Phases)-1].Steps
	kinds := make([]ridumigration.StepKind, len(final))
	for index, step := range final {
		kinds[index] = step.Kind
	}
	if len(kinds) < 4 || kinds[0] != ridumigration.StepDataTransform || kinds[1] != ridumigration.StepAuditRequiredValues ||
		kinds[2] != ridumigration.StepSQL || kinds[len(kinds)-1] != ridumigration.StepAssertSchema {
		t.Fatalf("final phase = %v, want transform, audit, NOT NULL, assertion", kinds)
	}
	afterCollection := after.Snapshot().Collections[0]
	backfill := func(ctx context.Context, transaction ridumigration.DataTransaction) error {
		for _, id := range []string{"one", "two"} {
			if _, err := transaction.Update(ctx, ridumigration.UpdateRequest{
				Request: store.Request{Collection: afterCollection, ID: id},
				Values:  store.Values{"title": store.String("Title " + id), "seo": store.Object(store.Values{"title": store.String("SEO " + id)})},
			}); err != nil {
				return err
			}
		}
		return nil
	}
	request := ridumigration.ProjectRequest{
		Action: ridumigration.ProjectApply, DatabaseURL: backend.pool.Config().ConnConfig.ConnString(), Directory: directory,
		AllowInsecureDatabase: true, AllowMaintenance: true,
	}
	noop := func(context.Context, ridumigration.DataTransaction) error { return nil }
	if err := ProjectMigrations(ridumigration.DataTransform{DataTransformDescriptor: descriptor, Up: noop, Down: noop}).RunProjectMigration(ctx, request, after); err == nil {
		t.Fatal("a transform that writes nothing passed the audit")
	} else {
		requireMissingValues(t, err, "posts.title in 2 documents", "posts.seo.title in 2 documents")
	}
	if err := ProjectMigrations(ridumigration.DataTransform{DataTransformDescriptor: descriptor, Up: backfill, Down: noop}).RunProjectMigration(ctx, request, after); err != nil {
		t.Fatal(err)
	}
	var nullable string
	if err := backend.pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
		collectionTable(afterCollection.ID), fieldColumn(afterCollection.Fields[0].ID)).Scan(&nullable); err != nil || nullable != "NO" {
		t.Fatalf("required column nullability = %q, %v", nullable, err)
	}
}

// An artifact without transforms is audited before any phase commits, so a
// refused requirement does not leave an earlier concurrent-index phase behind.
func TestPostgresRequiredValueAuditPrecedesEarlierPhases(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	directory := t.TempDir()
	before := requiredAuditManifest(t, core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary")}})
	after := requiredAuditManifest(t, core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary").Required().Index()}})
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	requiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
		_, err := transaction.Create(ctx, store.CreateRequest{Collection: before.Snapshot().Collections[0], ID: "post", Values: store.Values{"title": store.String("Title")}})
		return err
	})
	created, err := CreateArtifact(ctx, directory, "require-indexed-summary", after, time.Unix(2, 0), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Artifact.Phases) < 2 || created.Artifact.Phases[0].Mode != ridumigration.PhaseNoTransaction {
		t.Fatalf("artifact phases = %#v, want a concurrent-index phase before the audit", created.Artifact.Phases)
	}
	requireMissingValues(t, backend.ApplyArtifacts(ctx, directory), "posts.summary in 1 document, for example post")
	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) != 2 {
		t.Fatalf("refused artifact state = %#v, %v", statuses, err)
	}
	for _, phase := range statuses[1].Phases {
		if phase.State != "pending" {
			t.Fatalf("phase %s committed before the audit refused the migration: %#v", phase.ID, statuses[1].Phases)
		}
	}
}

// A required field added with a default needs no transform on PostgreSQL:
// adding its column writes the default into every existing row.
func TestPostgresRequiredValueAuditAcceptsColumnDefault(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	directory := t.TempDir()
	before := requiredAuditManifest(t, core.Collection{Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title")}})
	after := requiredAuditManifest(t, core.Collection{Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title"), field.Text("state").Required().Default("draft")}})
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collection := before.Snapshot().Collections[0]
	requiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
		_, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "one", Status: store.StatusPublished, Values: store.Values{"title": store.String("Live")}})
		return err
	})
	if _, err := CreateArtifact(ctx, directory, "add-state", after, time.Unix(2, 0), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
}

// Development synchronization applies the same audit, and accepts the change
// once every listed document has a value.
func TestPostgresDevelopmentRequiredValueAuditAcceptsCompleteDocuments(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	before := requiredAuditManifest(t, core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title")}})
	after := requiredAuditManifest(t, core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title").Required()}})
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	collection := before.Snapshot().Collections[0]
	requiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
		_, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "one", Values: store.Values{}})
		return err
	})
	requireMissingValues(t, backend.SyncDevelopmentSchema(ctx, after), "posts.title in 1 document, for example one")
	requiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
		_, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
			Request: store.Request{Collection: collection, ID: "one"}, Values: store.Values{"title": store.String("Filled")},
		})
		return err
	})
	if err := backend.SyncDevelopmentSchema(ctx, after); err != nil {
		t.Fatal(err)
	}
	recorded, exists, err := backend.DevelopmentManifest(ctx)
	if err != nil || !exists || !recorded.Equal(after) {
		t.Fatalf("complete documents did not admit the required field: %t, %v", exists, err)
	}
}
