package core_test

import (
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestImportUsesSourcePublicationStatusForAccessAndValidation(t *testing.T) {
	allowPublish := false
	app, err := ridu.New(ridu.Config{Name: "import publication status", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{
			field.Text("title").Required().MinLength(3).Validate(func(ctx operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
				text, _ := value.Get()
				if ctx.WritePhase == operation.WritePhasePublished && text == "blocked" {
					return []operation.Issue{{Code: "publication_title", Message: "title cannot be published"}}, nil
				}
				return nil, nil
			}),
			field.Text("summary").Required(),
			field.Email("contact"),
		},
		Access: ridu.CollectionAccess{Publish: func(ridu.AccessContext) (ridu.AccessDecision, error) {
			if !allowPublish {
				return ridu.Deny(), nil
			}
			return ridu.Allow(), nil
		}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	ctx := t.Context()

	if _, err := local.Import(ctx, "posts", store.Values{"title": store.String("valid"), "summary": store.String("ready")}, ridu.ImportOptions{ID: "denied", Status: store.StatusPublished}); !operationCode(err, "access_denied") {
		t.Fatalf("published import bypassed publish access: %v", err)
	}
	draft, err := local.Import(ctx, "posts", store.Values{"title": store.String("x")}, ridu.ImportOptions{ID: "draft", Status: store.StatusDraft})
	if err != nil || draft.Status != store.StatusDraft || draft.PublishedRevision != 0 {
		t.Fatalf("incomplete draft import = %#v, %v", draft, err)
	}
	if _, err := local.Import(ctx, "posts", store.Values{"title": store.String("x"), "contact": store.String("invalid")}, ridu.ImportOptions{ID: "malformed-draft", Status: store.StatusDraft}); !operationIssue(err, "invalid_email", "contact") {
		t.Fatalf("draft import skipped structural validation: %v", err)
	}
	if _, err := local.Import(ctx, "posts", store.Values{"title": store.String("valid"), "summary": store.String("ready")}, ridu.ImportOptions{ID: "invalid-status", Status: store.Status("queued")}); !operationIssue(err, "invalid_status", "_status") {
		t.Fatalf("import accepted an unknown source status: %v", err)
	}

	allowPublish = true
	for _, test := range []struct {
		name   string
		id     string
		values store.Values
		code   string
		path   string
	}{
		{"required", "missing-summary", store.Values{"title": store.String("valid")}, "required", "summary"},
		{"minimum length", "short-title", store.Values{"title": store.String("x"), "summary": store.String("ready")}, "min_length", "title"},
		{"publication rule", "blocked-title", store.Values{"title": store.String("blocked"), "summary": store.String("ready")}, "publication_title", "title"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := local.Import(ctx, "posts", test.values, ridu.ImportOptions{ID: test.id, Status: store.StatusPublished})
			if !operationIssue(err, test.code, test.path) {
				t.Fatalf("published import error = %v, want %s at %s", err, test.code, test.path)
			}
		})
	}
	published, err := local.Import(ctx, "posts", store.Values{"title": store.String("valid"), "summary": store.String("ready")}, ridu.ImportOptions{ID: "published", Status: store.StatusPublished})
	if err != nil || published.Status != store.StatusPublished || published.PublishedRevision != published.Revision {
		t.Fatalf("valid published import = %#v, %v", published, err)
	}
}

func TestDuplicateUnversionedDocumentHasNoPublicationStatus(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "unversioned duplicate", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "posts", store.Values{"title": store.String("original")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := app.Local().Duplicate(t.Context(), "posts", created.ID, nil, ridu.MutationOptions{})
	if err != nil || duplicate.Status != "" {
		t.Fatalf("unversioned duplicate status = %q, error = %v", duplicate.Status, err)
	}
	if _, err := app.Local().Import(t.Context(), "posts", store.Values{"title": store.String("draft")}, ridu.ImportOptions{ID: "unsupported-draft", Status: store.StatusDraft}); !operationIssue(err, "invalid_status", "_status") {
		t.Fatalf("unversioned import accepted draft status: %v", err)
	}
	imported, err := app.Local().Import(t.Context(), "posts", store.Values{"title": store.String("published")}, ridu.ImportOptions{ID: "published-source", Status: store.StatusPublished})
	if err != nil || imported.Status != "" {
		t.Fatalf("unversioned import status = %q, error = %v", imported.Status, err)
	}
}
