package payload_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	ridu "github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	payloadmigration "github.com/riducms/ridu/migration/payload"
	"github.com/riducms/ridu/store"
)

func TestPayloadIDDecodesStringAndIntegerWithoutFloatLoss(t *testing.T) {
	var source payloadmigration.Export
	if err := json.Unmarshal([]byte(`{"collections":[{"slug":"posts","documents":[{"ID":9007199254740993,"Data":{},"Status":"published"},{"ID":"018f6b2a-9786-7c2e-bb2f-50d9f14c47a1","Data":{},"Status":"published"}]}]}`), &source); err != nil {
		t.Fatal(err)
	}
	if got, want := source.Collections[0].Documents[0].ID.String(), "9007199254740993"; got != want {
		t.Fatalf("numeric ID = %q, want %q", got, want)
	}
	if got, want := source.Collections[0].Documents[1].ID.String(), "018f6b2a-9786-7c2e-bb2f-50d9f14c47a1"; got != want {
		t.Fatalf("string ID = %q, want %q", got, want)
	}

	for _, encoded := range []string{`1.5`, `1e3`, `null`, `"bad\u0000id"`} {
		var id payloadmigration.ID
		if err := json.Unmarshal([]byte(encoded), &id); err == nil {
			t.Fatalf("ID %s decoded successfully", encoded)
		}
	}
}

func TestImportRejectsCanonicalPayloadIDCollisionsBeforeWriting(t *testing.T) {
	target := &recordingTarget{}
	source := payloadmigration.Export{Collections: []payloadmigration.Collection{{Slug: "posts", Documents: []payloadmigration.Record{
		{ID: "1", Data: json.RawMessage(`{}`)},
		{ID: "1", Data: json.RawMessage(`{}`)},
	}}}}
	if _, err := payloadmigration.Import(context.Background(), target, source, nil); err == nil {
		t.Fatal("duplicate canonical IDs imported successfully")
	}
	if target.calls != 0 {
		t.Fatalf("target calls = %d, want 0", target.calls)
	}
}

func TestPayloadExportRejectsRepeatedCollectionBlocksBeforeWriting(t *testing.T) {
	target := &recordingTarget{}
	source := payloadmigration.Export{Collections: []payloadmigration.Collection{
		{Slug: "posts", Documents: []payloadmigration.Record{{ID: "1", Data: json.RawMessage(`{}`)}}},
		{Slug: "posts", Documents: []payloadmigration.Record{{ID: "1", Data: json.RawMessage(`{}`)}}},
	}}
	manifest, err := ridu.Resolve(ridu.Config{Name: "migration", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("title")},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	assessment := payloadmigration.Assess(manifest, source)
	joined := strings.Join(assessment.Issues, "\n")
	if !strings.Contains(joined, "appears more than once") || !strings.Contains(joined, "duplicate canonical ID") {
		t.Fatalf("assessment issues = %v", assessment.Issues)
	}
	if _, err := payloadmigration.Import(context.Background(), target, source, nil); err == nil || !strings.Contains(err.Error(), "appears more than once") {
		t.Fatalf("repeated collection import error = %v", err)
	}
	if target.calls != 0 {
		t.Fatalf("target calls = %d, want 0", target.calls)
	}
}

type recordingTarget struct{ calls int }

func (target *recordingTarget) Import(context.Context, string, store.Values, ridu.ImportOptions, *store.Document) (store.Document, error) {
	target.calls++
	return store.Document{}, nil
}

func TestImportPreservesIDsTimestampsAndChosenVersion(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "migration", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	source := payloadmigration.Export{Collections: []payloadmigration.Collection{{Slug: "posts", Documents: []payloadmigration.Record{{
		ID: "payload-id", Data: json.RawMessage("{\"title\":\"Current\"}"), Status: store.StatusPublished, CreatedAt: createdAt, UpdatedAt: createdAt,
		Versions:         []payloadmigration.Version{{Revision: 2, Data: json.RawMessage("{\"title\":\"Chosen\"}"), Status: store.StatusDraft, CreatedAt: createdAt, UpdatedAt: createdAt}},
		SelectedRevision: 2,
	}}}}}
	result, err := payloadmigration.Import(context.Background(), application.Local(), source, &store.Document{ID: "migrator"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 {
		t.Fatalf("imported = %d", result.Imported)
	}
	document, err := application.Local().Find(context.Background(), "posts", "payload-id", &store.Document{ID: "migrator"})
	if err != nil {
		t.Fatal(err)
	}
	title, _ := document.Values["title"].StringValue()
	if title != "Chosen" || !document.CreatedAt.Equal(createdAt) || document.Status != store.StatusDraft {
		t.Fatalf("document = %#v", document)
	}
}
