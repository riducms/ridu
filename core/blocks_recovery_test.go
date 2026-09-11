package core_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// Two real application schemas share the store. This is a deployed schema
// removal, not a client manifest that merely hides a still-known variant.
func TestRemovedBlockSchemaRequiresRecoveryAtOperationBoundary(t *testing.T) {
	ctx := context.Background()
	draft := true
	backend := teststore.New()
	config := ridu.Config{Name: "Retired blocks", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{{Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Localized(), field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}}, field.Block{Slug: "retired", Fields: field.Fields{field.Text("private")}})}}}}
	original, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := original.Local().Create(ctx, "pages", store.Values{"title": store.String("Before"), "layout": store.List(store.Object(store.Values{"blockType": store.String("retired"), "private": store.String("never disclose undeclared payload")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	config.Collections[0].Fields[1] = field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}})
	current, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	assertRecovery := func(t *testing.T, err error) {
		t.Helper()
		var issue *ridu.OperationError
		if !errors.As(err, &issue) || issue.Code != "block_recovery_required" || issue.Status != 409 || len(issue.Issues) != 1 || issue.Issues[0].Code != "unknown_block_schema" || issue.Issues[0].Path != "layout.0.blockType" {
			t.Fatalf("expected precise recovery error, got %#v (%v)", issue, err)
		}
		if strings.Contains(err.Error(), "never disclose") || strings.Contains(err.Error(), "retired") {
			t.Fatalf("recovery error disclosed stored content: %v", err)
		}
	}
	checks := []struct {
		name string
		run  func() error
	}{
		{"read", func() error {
			doc, err := current.Local().Find(ctx, "pages", created.ID, ridu.FindOptions{Draft: &draft})
			if len(doc.Values) > 0 {
				t.Fatal("failed read returned payload")
			}
			return err
		}},
		{"list", func() error {
			page, err := current.Local().List(ctx, "pages", ridu.ListOptions{Draft: &draft})
			if len(page.Documents) > 0 {
				t.Fatal("failed list returned payload")
			}
			return err
		}},
		{"delete rows", func() error {
			_, err := current.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List()}, ridu.MutationOptions{})
			return err
		}},
		{"unrelated localized edit", func() error {
			_, err := current.Local().Update(ctx, "pages", created.ID, store.Values{"title": store.String("Après")}, ridu.MutationOptions{Locale: "fr"})
			return err
		}},
		{"replace rows", func() error {
			_, err := current.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List(store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("Replacement")}))}, ridu.MutationOptions{})
			return err
		}},
		{"duplicate", func() error {
			_, err := current.Local().Duplicate(ctx, "pages", created.ID, nil, ridu.MutationOptions{})
			return err
		}},
		{"publish", func() error {
			_, err := current.Local().Publish(ctx, "pages", created.ID, ridu.MutationOptions{ExpectedRevision: created.Revision})
			return err
		}},
		{"versions", func() error {
			versions, err := current.Local().Versions(ctx, "pages", created.ID, ridu.FindOptions{})
			if len(versions) > 0 {
				t.Fatal("failed versions read returned payload")
			}
			return err
		}},
		{"restore", func() error {
			_, err := current.Local().RestoreAsDraft(ctx, "pages", created.ID, created.Revision, ridu.MutationOptions{ExpectedRevision: created.Revision})
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) { assertRecovery(t, check.run()) })
	}
	metadata, err := current.Local().Find(ctx, "pages", created.ID, ridu.FindOptions{Draft: &draft, Select: []query.Path{}})
	if err != nil || metadata.ID != created.ID || len(metadata.Values) != 0 {
		t.Fatalf("metadata-only projection must not require payload recovery: %#v %v", metadata, err)
	}
	// Restoring the schema must reveal exactly the original persisted values and
	// revision: denied mutations may not change content or create revisions.
	restored, err := original.Local().Find(ctx, "pages", created.ID, ridu.FindOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, created) {
		t.Fatalf("recovery failures changed stored document: got %#v want %#v", restored, created)
	}
}

func TestRemovedBlockSchemaHistoricalSnapshotRequiresRecovery(t *testing.T) {
	ctx := context.Background()
	draft := true
	backend := teststore.New()
	config := ridu.Config{Name: "Historical retired blocks", Collections: []ridu.Collection{{Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}}, field.Block{Slug: "retired", Fields: field.Fields{field.Text("private")}})}}}}
	original, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := original.Local().Create(ctx, "pages", store.Values{"layout": store.List(store.Object(store.Values{"blockType": store.String("retired"), "private": store.String("historical secret")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := original.Local().Update(ctx, "pages", retired.ID, store.Values{"layout": store.List()}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	config.Collections[0].Fields[0] = field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}})
	current, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = current.Local().Find(ctx, "pages", healthy.ID, ridu.FindOptions{Draft: &draft}); err != nil {
		t.Fatalf("healthy current document must remain usable: %v", err)
	}
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{"historical read", func() error {
			version, err := current.Local().Version(ctx, "pages", healthy.ID, retired.Revision, ridu.FindOptions{})
			if len(version.Snapshot.Values) > 0 {
				t.Fatal("failed historical read disclosed payload")
			}
			return err
		}},
		{"historical restore", func() error {
			_, err := current.Local().RestoreAsDraft(ctx, "pages", healthy.ID, retired.Revision, ridu.MutationOptions{ExpectedRevision: healthy.Revision})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			var issue *ridu.OperationError
			if !errors.As(err, &issue) || issue.Code != "block_recovery_required" || len(issue.Issues) != 1 || issue.Issues[0].Path != "layout.0.blockType" {
				t.Fatalf("expected historical recovery error, got %v", err)
			}
		})
	}
	stored, err := current.Local().Find(ctx, "pages", healthy.ID, ridu.FindOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, healthy) {
		t.Fatal("failed historical restoration changed current document")
	}
}

func TestRemovedBlockSchemaInAnotherLocalePreventsMutation(t *testing.T) {
	ctx := context.Background()
	backend := teststore.New()
	config := ridu.Config{Name: "Localized retired blocks", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}}, field.Block{Slug: "retired", Fields: field.Fields{field.Text("private")}}).Localized()}}}}
	original, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := original.Local().Create(ctx, "pages", store.Values{"layout": store.List(store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("English")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = original.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List(store.Object(store.Values{"blockType": store.String("retired"), "private": store.String("French secret")}))}, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	config.Collections[0].Fields[0] = field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}}).Localized()
	current, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	_, err = current.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List()}, ridu.MutationOptions{Locale: "en"})
	var issue *ridu.OperationError
	if !errors.As(err, &issue) || issue.Code != "block_recovery_required" || len(issue.Issues) != 1 || issue.Issues[0].Path != "layout.fr.0.blockType" {
		t.Fatalf("mutation must inspect every stored locale: %v", err)
	}
	french, err := original.Local().Find(ctx, "pages", created.ID, ridu.FindOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if secret, _ := blockRows(french.Values["layout"])[0]["private"].StringValue(); secret != "French secret" {
		t.Fatal("unknown locale data changed")
	}
}
