package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/requiredfield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func sqliteRequiredAuditManifest(t *testing.T, collection core.Collection) schema.Manifest {
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

func sqliteRequiredAuditWrite(t *testing.T, ctx context.Context, backend *Store, write func(store.Transaction) error) {
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

func sqliteRequireMissingValues(t *testing.T, err error, fragments ...string) {
	t.Helper()
	var failure *requiredfield.MissingValuesError
	if !errors.As(err, &failure) {
		t.Fatalf("schema change error = %v, want %s", err, requiredfield.Code)
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("schema change error %q does not mention %q", err, fragment)
		}
	}
}

func sqliteLatestStepKinds(t *testing.T, directory string) []ridumigration.StepKind {
	t.Helper()
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) == 0 {
		t.Fatalf("read migration history: %d files, %v", len(files), err)
	}
	var kinds []ridumigration.StepKind
	for _, phase := range files[len(files)-1].Artifact.Phases {
		for _, step := range phase.Steps {
			kinds = append(kinds, step.Kind)
		}
	}
	return kinds
}

// Each case stores documents under the optional schema; requiring the field
// is planned with an audit, and both the artifact and development
// synchronization refuse it while a document that must be complete lacks a
// value, leaving the schema and the ledger unchanged.
func TestSQLiteRequiredValueAuditRefusesMissingValues(t *testing.T) {
	ctx := context.Background()
	versioned := func(summary field.Node) core.Collection {
		return core.Collection{Slug: "posts", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title"), summary}}
	}
	for _, test := range []struct {
		name      string
		before    core.Collection
		after     core.Collection
		prepare   func(*testing.T, store.Transaction, schema.Collection) error
		documents []store.Values
		fragments []string
	}{
		{
			name:      "top-level field",
			before:    core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary")}},
			after:     core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary").Required()}},
			documents: []store.Values{{"title": store.String("one")}, {"title": store.String("two"), "summary": store.String("")}, {"summary": store.String("kept")}},
			fragments: []string{"posts.summary in 2 documents, for example doc-0, doc-1"},
		},
		{
			name:      "added required field",
			before:    core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title")}},
			after:     core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("state").Required().Default("new")}},
			documents: []store.Values{{"title": store.String("one")}},
			fragments: []string{"posts.state in 1 document, for example doc-0"},
		},
		{
			name:      "group child",
			before:    core.Collection{Slug: "posts", Fields: field.Fields{field.Group("seo", field.Fields{field.Text("title")})}},
			after:     core.Collection{Slug: "posts", Fields: field.Fields{field.Group("seo", field.Fields{field.Text("title").Required()})}},
			documents: []store.Values{{"seo": store.Object(store.Values{"title": store.String("kept")})}, {"seo": store.Object(store.Values{})}, {}},
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
			name:      "localized group child",
			before:    core.Collection{Slug: "posts", Fields: field.Fields{field.Group("seo", field.Fields{field.Text("title")}).Localized()}},
			after:     core.Collection{Slug: "posts", Fields: field.Fields{field.Group("seo", field.Fields{field.Text("title").Required()}).Localized()}},
			documents: []store.Values{{"seo": store.Object(store.Values{"en": store.Object(store.Values{"title": store.String("kept")}), "fr": store.Object(store.Values{})})}},
			fragments: []string{"posts.seo.title (locale fr) in 1 document, for example doc-0"},
		},
		{
			name:   "published row behind a complete draft",
			before: versioned(field.Text("summary")),
			after:  versioned(field.Text("summary").Required()),
			prepare: func(t *testing.T, transaction store.Transaction, collection schema.Collection) error {
				// The working draft is complete; the published row is not.
				published, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "live", Status: store.StatusPublished, Values: store.Values{"title": store.String("Live")}})
				if err != nil {
					return err
				}
				_, err = conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
					Request: store.Request{Collection: collection, ID: "live", ExpectedRevision: published.Revision},
					Intent:  store.WriteIntentSaveDraft, Values: store.Values{"summary": store.String("Only in the draft")},
				})
				if err != nil {
					return err
				}
				// An incomplete draft is exempt.
				_, err = transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "draft", Status: store.StatusDraft, Values: store.Values{"title": store.String("Draft")}})
				return err
			},
			fragments: []string{"posts.summary in 1 document, for example live."},
		},
	} {
		for _, mode := range []string{"artifact", "development"} {
			t.Run(test.name+"/"+mode, func(t *testing.T) {
				backend := newSQLiteMigrationStore(t)
				before := sqliteRequiredAuditManifest(t, test.before)
				after := sqliteRequiredAuditManifest(t, test.after)
				directory := t.TempDir()
				if mode == "artifact" {
					if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
						t.Fatal(err)
					}
					if err := backend.ApplyArtifacts(ctx, directory); err != nil {
						t.Fatal(err)
					}
				} else if err := backend.Migrate(ctx, before); err != nil {
					t.Fatal(err)
				}
				collection := before.Snapshot().Collections[0]
				sqliteRequiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
					if test.prepare != nil {
						return test.prepare(t, transaction, collection)
					}
					for index, values := range test.documents {
						if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "doc-" + string(rune('0'+index)), Values: values, Locales: []schema.LocaleCode{"en", "fr"}}); err != nil {
							return err
						}
					}
					return nil
				})
				if mode == "development" {
					sqliteRequireMissingValues(t, backend.Migrate(ctx, after), append(test.fragments, "Kept the current schema")...)
					recorded, exists, err := backend.DevelopmentManifest(ctx)
					if err != nil || !exists || !recorded.Equal(before) {
						t.Fatalf("refused synchronization changed the recorded schema: %t, %v", exists, err)
					}
					return
				}
				if _, err := CreateArtifact(ctx, directory, "require", after, time.Unix(2, 0), false); err != nil {
					t.Fatal(err)
				}
				if kinds := sqliteLatestStepKinds(t, directory); len(kinds) != 2 || kinds[0] != ridumigration.StepAuditRequiredValues {
					t.Fatalf("require steps = %v, want the audit before the schema assertion", kinds)
				}
				sqliteRequireMissingValues(t, backend.ApplyArtifacts(ctx, directory), append(test.fragments, "ridu migrate create <name> --transform <transform>")...)
				statuses, err := backend.ArtifactStatus(ctx, directory, after)
				if err != nil || len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
					t.Fatalf("refused artifact state = %#v, %v", statuses, err)
				}
			})
		}
	}
}

// A data transform bound to the migration writes the values before the audit
// reads them. Relaxing the field again in a later migration needs no audit,
// but rolling that migration back requires the field again and is audited.
func TestSQLiteRequiredValueAuditFollowsTransformsAndRollback(t *testing.T) {
	ctx := context.Background()
	backend := newSQLiteMigrationStore(t)
	directory := t.TempDir()
	optional := sqliteRequiredAuditManifest(t, core.Collection{Slug: "notes", Fields: field.Fields{field.Group("meta", field.Fields{field.Text("body")})}})
	required := sqliteRequiredAuditManifest(t, core.Collection{Slug: "notes", Fields: field.Fields{field.Group("meta", field.Fields{field.Text("body").Required()})}})
	relaxedAgain := sqliteRequiredAuditManifest(t, core.Collection{Slug: "notes", Fields: field.Fields{field.Group("meta", field.Fields{field.Text("body")}), field.Text("tag")}})
	if _, err := CreateArtifact(ctx, directory, "initial", optional, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collection := optional.Snapshot().Collections[0]
	sqliteRequiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
		_, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "note", Values: store.Values{"meta": store.Object(store.Values{})}})
		return err
	})
	descriptor := ridumigration.DataTransformDescriptor{Name: "backfill-bodies", Checksum: ridumigration.DataTransformChecksum([]byte("backfill-bodies-v1"))}
	if _, err := CreateArtifact(ctx, directory, "require-body", required, time.Unix(2, 0), false, descriptor); err != nil {
		t.Fatal(err)
	}
	backfill := ridumigration.DataTransform{DataTransformDescriptor: descriptor,
		Up: func(ctx context.Context, transaction ridumigration.DataTransaction) error {
			_, err := transaction.Update(ctx, ridumigration.UpdateRequest{
				Request: store.Request{Collection: required.Snapshot().Collections[0], ID: "note"},
				Values:  store.Values{"meta": store.Object(store.Values{"body": store.String("Backfilled")})},
			})
			return err
		},
		Down: func(context.Context, ridumigration.DataTransaction) error { return nil },
	}
	if err := backend.ApplyArtifacts(ctx, directory, backfill); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifact(ctx, directory, "relax-body", relaxedAgain, time.Unix(3, 0), false); err != nil {
		t.Fatal(err)
	}
	if kinds := sqliteLatestStepKinds(t, directory); len(kinds) != 1 || kinds[0] != ridumigration.StepAssertSchema {
		t.Fatalf("relaxing steps = %v, want only the schema assertion", kinds)
	}
	if err := backend.ApplyArtifacts(ctx, directory, backfill); err != nil {
		t.Fatal(err)
	}
	sqliteRequiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
		_, err := transaction.Create(ctx, store.CreateRequest{Collection: relaxedAgain.Snapshot().Collections[0], ID: "later", Values: store.Values{"meta": store.Object(store.Values{})}})
		return err
	})
	sqliteRequireMissingValues(t, backend.DownArtifacts(ctx, directory, backfill), "notes.meta.body in 1 document, for example later")
	statuses, err := backend.ArtifactStatus(ctx, directory, relaxedAgain)
	if err != nil || len(statuses) != 3 || !statuses[2].Applied {
		t.Fatalf("refused rollback state = %#v, %v", statuses, err)
	}
}
