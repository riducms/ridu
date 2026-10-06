package mongodb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/requiredfield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

var requiredAuditLocales = []schema.LocaleCode{"en", "fr"}

func mongoRequiredAuditManifest(t *testing.T, collection core.Collection) schema.Manifest {
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

func mongoRequireMissingValues(t *testing.T, err error, fragments ...string) {
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

func mongoRequiredAuditWrite(t *testing.T, ctx context.Context, backend *Store, write func(store.Transaction) error) {
	t.Helper()
	transaction := mongoBegin(t, backend, false)
	if err := write(transaction); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	mongoCommit(t, transaction)
}

// Draft saves defer required fields on MongoDB as on every adapter: an
// incomplete draft saves and reads, a nested required child may be missing,
// and only publication requires the values.
func TestMongoDBDraftsDeferRequiredValues(t *testing.T) {
	ctx := t.Context()
	allow := func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }
	config := ridu.Config{Name: "Drafts", Collections: []ridu.Collection{{
		Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{
			field.Text("title").Required(),
			field.Group("seo", field.Fields{field.Text("headline").Required()}),
			field.Array("items", field.Fields{field.Text("caption").Required()}).MinRows(2),
		},
		Access: ridu.CollectionAccess{ReadDrafts: allow, ReadVersions: allow},
	}}}
	backend := mongoIntegrationStore(t)
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.syncIndexes(ctx, app.Manifest()); err != nil {
		t.Fatal(err)
	}
	draft := true
	created, err := app.Local().Create(ctx, "pages", store.Values{
		"seo":   store.Object(store.Values{}),
		"items": store.List(store.Object(store.Values{"_key": store.String("one")})),
	}, ridu.MutationOptions{Draft: &draft})
	if err != nil {
		t.Fatalf("incomplete draft was refused: %v", err)
	}
	stored, err := app.Local().Find(ctx, "pages", created.ID, ridu.FindOptions{Draft: &draft})
	if err != nil {
		t.Fatalf("incomplete draft failed a read: %v", err)
	}
	if _, present := stored.Values["title"]; present && stored.Values["title"].Kind() != store.ValueNull {
		t.Fatalf("draft title = %#v", stored.Values["title"])
	}
	if _, err := app.Local().Publish(ctx, "pages", created.ID, ridu.MutationOptions{ExpectedRevision: stored.Revision}); err == nil {
		t.Fatal("an incomplete draft was published")
	}
	published, err := app.Local().PublishChanges(ctx, "pages", created.ID, store.Values{
		"title": store.String("Complete"), "seo": store.Object(store.Values{"headline": store.String("Headline")}),
		"items": store.List(
			store.Object(store.Values{"_key": store.String("one"), "caption": store.String("One")}),
			store.Object(store.Values{"_key": store.String("two"), "caption": store.String("Two")}),
		),
	}, ridu.MutationOptions{ExpectedRevision: stored.Revision})
	if err != nil || published.Status != store.StatusPublished {
		t.Fatalf("complete publication = %#v, %v", published, err)
	}
}

// Like PostgreSQL and SQLite, MongoDB audits every working document except
// drafts and every published head. Each case stores documents under the
// optional schema; requiring the field is refused by the migration and by
// development synchronization, which keep the schema.
func TestMongoDBRequiredValueAuditRefusesMissingValues(t *testing.T) {
	ctx := t.Context()
	versioned := func(summary field.Node) core.Collection {
		return core.Collection{Slug: "posts", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title"), summary}}
	}
	for _, test := range []struct {
		name      string
		before    core.Collection
		after     core.Collection
		prepare   func(store.Transaction, schema.Collection) error
		documents []store.Values
		fragments []string
	}{
		{
			name:      "top-level field",
			before:    core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary")}},
			after:     core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary").Required()}},
			documents: []store.Values{{"title": store.String("one")}, {"title": store.String("two"), "summary": store.String("kept")}},
			fragments: []string{"posts.summary in 1 document, for example doc-0"},
		},
		{
			name:      "added required field",
			before:    core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title")}},
			after:     core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Number("rank").Required()}},
			documents: []store.Values{{"title": store.String("one")}},
			fragments: []string{"posts.rank in 1 document, for example doc-0"},
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
				{"layout": store.List(store.Object(store.Values{"_key": store.String("a"), "blockType": store.String("quote"), "blockName": store.String("")}))},
				{"layout": store.List(store.Object(store.Values{"_key": store.String("b"), "blockType": store.String("hero"), "blockName": store.String("")}))},
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
			name:   "published head behind a complete draft",
			before: versioned(field.Text("summary")),
			after:  versioned(field.Text("summary").Required()),
			prepare: func(transaction store.Transaction, collection schema.Collection) error {
				// The working draft is complete; the published head is not.
				published, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "live", Status: store.StatusPublished, Values: store.Values{"title": store.String("Live")}, Locales: requiredAuditLocales})
				if err != nil {
					return err
				}
				_, err = conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
					Request: store.Request{Collection: collection, ID: "live", ExpectedRevision: published.Revision, Locales: requiredAuditLocales},
					Intent:  store.WriteIntentSaveDraft, Values: store.Values{"summary": store.String("Only in the draft")},
				})
				if err != nil {
					return err
				}
				// Incomplete drafts, a new one and pending changes over a
				// complete published head, are exempt, and so are snapshots.
				if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "draft", Status: store.StatusDraft, Values: store.Values{"title": store.String("Draft")}, Locales: requiredAuditLocales}); err != nil {
					return err
				}
				complete, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "complete", Status: store.StatusPublished, Values: store.Values{"title": store.String("Complete"), "summary": store.String("Kept")}, Locales: requiredAuditLocales})
				if err != nil {
					return err
				}
				if _, err := transaction.(store.VersionTransaction).SaveVersion(ctx, collection, store.Document{ID: "complete", Status: store.StatusPublished, Revision: complete.Revision, CreatedAt: complete.CreatedAt, UpdatedAt: complete.UpdatedAt, Values: store.Values{"title": store.String("First")}}, 10); err != nil {
					return err
				}
				_, err = conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
					Request: store.Request{Collection: collection, ID: "complete", ExpectedRevision: complete.Revision, Locales: requiredAuditLocales},
					Intent:  store.WriteIntentSaveDraft, Values: store.Values{"summary": store.Null()},
				})
				return err
			},
			fragments: []string{"posts.summary in 1 document, for example live.", "except draft working copies"},
		},
	} {
		for _, mode := range []string{"artifact", "development"} {
			t.Run(test.name+"/"+mode, func(t *testing.T) {
				backend := mongoIntegrationStore(t)
				before := mongoRequiredAuditManifest(t, test.before)
				after := mongoRequiredAuditManifest(t, test.after)
				directory := t.TempDir()
				if mode == "artifact" {
					if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
						t.Fatal(err)
					}
					if err := backend.ApplyArtifacts(ctx, directory); err != nil {
						t.Fatal(err)
					}
				} else if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
					t.Fatal(err)
				}
				collection := before.Snapshot().Collections[0]
				mongoRequiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
					if test.prepare != nil {
						return test.prepare(transaction, collection)
					}
					for index, values := range test.documents {
						if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "doc-" + string(rune('0'+index)), Values: values, Locales: requiredAuditLocales}); err != nil {
							return err
						}
					}
					return nil
				})
				if mode == "development" {
					mongoRequireMissingValues(t, backend.SyncDevelopmentSchema(ctx, after), append(test.fragments, "Kept the current schema")...)
					recorded, exists, err := backend.DevelopmentManifest(ctx)
					if err != nil || !exists || !recorded.Equal(before) {
						t.Fatalf("refused synchronization changed the recorded schema: %t, %v", exists, err)
					}
					return
				}
				if _, err := CreateArtifact(ctx, directory, "require", after, time.Unix(2, 0), ArtifactOptions{}); err != nil {
					t.Fatal(err)
				}
				files, err := migrationartifact.ReadAll(directory)
				if err != nil {
					t.Fatal(err)
				}
				audited := false
				for _, phase := range files[len(files)-1].Artifact.Phases {
					for _, step := range phase.Steps {
						audited = audited || step.Kind == ridumigration.StepAuditRequiredValues && phase.Mode == ridumigration.PhaseTransaction
					}
				}
				if !audited {
					t.Fatalf("artifact does not plan the audit: %#v", files[len(files)-1].Artifact.Phases)
				}
				if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "maintenance admission") {
					t.Fatalf("the audit ran without stopped writers: %v", err)
				}
				mongoRequireMissingValues(t, backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}), append(test.fragments, "ridu migrate create <name> --transform <transform>")...)
				statuses, err := backend.ArtifactStatus(ctx, directory)
				if err != nil || len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
					t.Fatalf("refused artifact state = %#v, %v", statuses, err)
				}
			})
		}
	}
}

// A data transform bound to the same migration writes the values before the
// audit reads them, and development synchronization admits the field once
// stored documents are complete.
func TestMongoDBRequiredValueAuditAdmitsBackfilledValues(t *testing.T) {
	ctx := t.Context()
	before := mongoRequiredAuditManifest(t, core.Collection{Slug: "notes", Fields: field.Fields{field.Group("meta", field.Fields{field.Text("body")})}})
	after := mongoRequiredAuditManifest(t, core.Collection{Slug: "notes", Fields: field.Fields{field.Group("meta", field.Fields{field.Text("body").Required()})}})
	seed := func(t *testing.T, backend *Store) {
		mongoRequiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
			_, err := transaction.Create(ctx, store.CreateRequest{Collection: before.Snapshot().Collections[0], ID: "note", Values: store.Values{"meta": store.Object(store.Values{})}, Locales: requiredAuditLocales})
			return err
		})
	}
	t.Run("artifact", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		directory := t.TempDir()
		if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
			t.Fatal(err)
		}
		if err := backend.ApplyArtifacts(ctx, directory); err != nil {
			t.Fatal(err)
		}
		seed(t, backend)
		descriptor := ridumigration.DataTransformDescriptor{Name: "backfill-bodies", Checksum: ridumigration.DataTransformChecksum([]byte("backfill-bodies-v1"))}
		if _, err := CreateArtifact(ctx, directory, "require-body", after, time.Unix(2, 0), ArtifactOptions{DataTransforms: []ridumigration.DataTransformDescriptor{descriptor}}); err != nil {
			t.Fatal(err)
		}
		// The transform reads and updates the incomplete document with the
		// after shape, as on PostgreSQL and SQLite.
		backfill := ridumigration.DataTransform{DataTransformDescriptor: descriptor,
			Up: func(ctx context.Context, transaction ridumigration.DataTransaction) error {
				_, err := transaction.Update(ctx, ridumigration.UpdateRequest{
					Request: store.Request{Collection: after.Snapshot().Collections[0], ID: "note", Locales: requiredAuditLocales},
					Values:  store.Values{"meta": store.Object(store.Values{"body": store.String("Backfilled")})},
				})
				return err
			},
			Down: func(context.Context, ridumigration.DataTransaction) error { return nil },
		}
		if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}, backfill); err != nil {
			t.Fatal(err)
		}
		statuses, err := backend.ArtifactStatus(ctx, directory)
		if err != nil || len(statuses) != 2 || !statuses[1].Applied {
			t.Fatalf("backfilled artifact state = %#v, %v", statuses, err)
		}
	})
	t.Run("development", func(t *testing.T) {
		backend := mongoIntegrationStore(t)
		if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
			t.Fatal(err)
		}
		seed(t, backend)
		mongoRequireMissingValues(t, backend.SyncDevelopmentSchema(ctx, after), "notes.meta.body in 1 document, for example note")
		// The refused synchronization leaves the accepted schema serving.
		if err := backend.VerifyIndexes(ctx, before); err != nil {
			t.Fatal(err)
		}
		mongoRequiredAuditWrite(t, ctx, backend, func(transaction store.Transaction) error {
			_, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
				Request: store.Request{Collection: before.Snapshot().Collections[0], ID: "note", Locales: requiredAuditLocales},
				Values:  store.Values{"meta": store.Object(store.Values{"body": store.String("Filled")})},
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
	})
}
