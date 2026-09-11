package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestSQLiteStoreRunsThePayloadDocumentAndAuthVertical(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	statusPath, err := query.NewPath("status")
	if err != nil {
		t.Fatal(err)
	}
	config := ridu.Config{
		Name:  "SQLite integration",
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{
				Slug: "users", Auth: true,
				Fields: field.Fields{field.Email("email").Required().Unique(), field.Text("name")},
			},
			{
				Slug:   "posts",
				Fields: field.Fields{field.Text("title").Required().Unique(), field.Select("status", "draft", "published").Required(), field.Relationship("author", "users").Required(), field.Relationships("watchers", "users"), field.Group("metadata", field.Fields{field.Text("source")})},
				Access: ridu.CollectionAccess{
					Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
						return ridu.Where(query.Equal(statusPath, query.String("published"))), nil
					},
					Update: func(ridu.AccessContext) (ridu.AccessDecision, error) {
						return ridu.Where(query.Equal(statusPath, query.String("published"))), nil
					},
					Delete: func(ridu.AccessContext) (ridu.AccessDecision, error) {
						return ridu.Where(query.Equal(statusPath, query.String("published"))), nil
					},
				},
			},
		},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(t.TempDir(), "ridu.sqlite")
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, manifest); err == nil {
		t.Fatal("Ready accepted a database before explicit migration")
	}
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}

	user, err := application.CreateAuthUser(ctx, "users", store.Values{
		"email": store.String("ada@example.test"),
		"name":  store.String("Ada"),
	}, "correct-horse-battery", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	login, err := application.Login(ctx, "users", "ADA@EXAMPLE.TEST", "correct-horse-battery")
	if err != nil || login.User.ID != user.ID {
		t.Fatalf("case-insensitive login = %#v, %v", login, err)
	}
	if current, err := application.Session(ctx, login.Token); err != nil || current.User.ID != user.ID {
		t.Fatalf("session = %#v, %v", current, err)
	}
	if _, err := application.Local().Create(ctx, "users", store.Values{
		"email": store.String("ada@example.test"),
	}, ridu.MutationOptions{}); !sqliteOperationCode(err, "conflict") {
		t.Fatalf("duplicate unique email error = %v", err)
	}
	if _, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Missing relation"), "status": store.String("published"),
		"author": store.String("missing"),
	}, ridu.MutationOptions{}); err == nil {
		t.Fatal("missing relationship target was accepted")
	}

	private, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Private"), "status": store.String("draft"), "author": store.String(user.ID),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	public, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Public"), "status": store.String("published"), "author": store.String(user.ID),
		"watchers": store.List(store.String(user.ID)),
		"metadata": store.Object(store.Values{"source": store.String("sqlite")}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	authorPath, _ := query.NewPath("author")
	page, err := application.Local().List(ctx, "posts", ridu.ListOptions{
		Page: 1, Limit: 10, Populate: []query.Population{{Path: authorPath}},
	})
	if err != nil || page.Total != 1 || len(page.Documents) != 1 || page.Documents[0].ID != public.ID {
		t.Fatalf("access-filtered page = %#v, %v", page, err)
	}
	if populated, ok := page.Documents[0].Values["author"].CopyDocument(); !ok || populated.ID != user.ID {
		t.Fatalf("populated author = %#v", page.Documents[0].Values["author"])
	}
	if _, err := application.Local().Find(ctx, "posts", private.ID, ridu.FindOptions{}); !sqliteOperationCode(err, "not_found") {
		t.Fatalf("filtered find error = %v", err)
	}
	updated, err := application.Local().Update(ctx, "posts", public.ID, store.Values{
		"title": store.String("Public updated"),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := updated.Values["title"].StringValue(); title != "Public updated" {
		t.Fatalf("updated title = %q", title)
	}

	if err := application.Logout(ctx, login.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Session(ctx, login.Token); !sqliteOperationCode(err, "access_denied") {
		t.Fatalf("logged-out session error = %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.Ready(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	reopenedApplication, err := ridu.New(config, reopened)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reopenedApplication.Local().Find(ctx, "posts", public.ID, ridu.FindOptions{})
	if err != nil || stored.ID != public.ID {
		t.Fatalf("reopened document = %#v, %v", stored, err)
	}
}

func TestSQLiteVersionAndTrashLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	config := ridu.Config{Name: "SQLite versions", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, Trash: true,
		VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields:        field.Fields{field.Text("title").Required()},
	}}}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Draft")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	published, err := application.Local().PublishChanges(ctx, "posts", draft.ID, store.Values{
		"title": store.String("Published"),
	}, ridu.MutationOptions{ExpectedRevision: draft.Revision})
	if err != nil || published.Status != store.StatusPublished {
		t.Fatalf("publish = %#v, %v", published, err)
	}
	if _, err := application.Local().PublishChanges(ctx, "posts", draft.ID, store.Values{
		"title": store.String("Stale"),
	}, ridu.MutationOptions{ExpectedRevision: draft.Revision}); !sqliteOperationCode(err, "conflict") {
		t.Fatalf("stale revision error = %v (published revision %d)", err, published.Revision)
	}
	versions, err := application.Local().Versions(ctx, "posts", draft.ID, ridu.FindOptions{})
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions = %#v, %v", versions, err)
	}
	trashed, err := application.Local().Delete(ctx, "posts", draft.ID, ridu.MutationOptions{})
	if err != nil || trashed.DeletedAt == nil {
		t.Fatalf("trash = %#v, %v", trashed, err)
	}
	if _, err := application.Local().Find(ctx, "posts", draft.ID, ridu.FindOptions{}); !sqliteOperationCode(err, "not_found") {
		t.Fatalf("active find after trash = %v", err)
	}
	restored, err := application.Local().RestoreDeleted(ctx, "posts", draft.ID, ridu.MutationOptions{})
	if err != nil || restored.DeletedAt != nil {
		t.Fatalf("restore deleted = %#v, %v", restored, err)
	}
	if _, err := application.Local().Delete(ctx, "posts", draft.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().DeletePermanent(ctx, "posts", draft.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().RestoreDeleted(ctx, "posts", draft.ID, ridu.MutationOptions{}); !sqliteOperationCode(err, "not_found") {
		t.Fatalf("restore after permanent delete = %v", err)
	}
}

func TestSQLiteNestedJoinAndRestoreShareTheOuterHookTransaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var categoryID, postID string
	var postRevision, restoreRevision int
	config := ridu.Config{Name: "SQLite nested mutation transactions", Collections: []ridu.Collection{
		{
			Slug:   "categories",
			Fields: field.Fields{field.Text("name").Required(), field.Join("posts", "posts", "category")},
		},
		{
			Slug: "posts", Versions: true,
			Fields: field.Fields{field.Text("title").Required(), field.Relationship("category", "categories")},
		},
		{
			Slug:   "triggers",
			Fields: field.Fields{field.Text("action").Required()},
			Hooks: ridu.CollectionHooks{BeforeOperation: []ridu.Hook{func(hook ridu.HookContext) error {
				action, _ := hook.Data["action"].StringValue()
				switch action {
				case "join":
					if _, err := hook.Local.MutateJoin(hook.Context, "categories", categoryID, "posts", []string{postID}, nil, ridu.MutationOptions{}); err != nil {
						return err
					}
				case "restore":
					if _, err := hook.Local.Restore(hook.Context, "posts", postID, restoreRevision, ridu.MutationOptions{ExpectedRevision: postRevision}); err != nil {
						return err
					}
				default:
					return nil
				}
				return errors.New("force the outer operation to roll back")
			}}},
		},
	}}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "nested.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	category, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("News")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	categoryID = category.ID
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Old")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	restoreRevision = post.Revision
	post, err = application.Local().PublishChanges(ctx, "posts", post.ID, store.Values{"title": store.String("Current")}, ridu.MutationOptions{ExpectedRevision: post.Revision})
	if err != nil {
		t.Fatal(err)
	}
	postID, postRevision = post.ID, post.Revision
	trigger, err := application.Local().Create(ctx, "triggers", store.Values{"action": store.String("idle")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for _, action := range []string{"join", "restore"} {
		operationContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, mutationError := application.Local().Update(operationContext, "triggers", trigger.ID, store.Values{"action": store.String(action)}, ridu.MutationOptions{})
		cancel()
		if !sqliteOperationCode(mutationError, "hook_failed") {
			t.Fatalf("nested %s failure = %v, want hook_failed before the deadline", action, mutationError)
		}
	}

	storedPost, err := application.Local().Find(ctx, "posts", postID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if category, present := storedPost.Values["category"].StringValue(); present || category != "" {
		t.Fatalf("rolled-back nested join persisted category %q", category)
	}
	if title, _ := storedPost.Values["title"].StringValue(); title != "Current" || storedPost.Revision != postRevision {
		t.Fatalf("rolled-back nested restore persisted %#v", storedPost)
	}
}

func sqliteOperationCode(err error, code string) bool {
	var operationError *ridu.OperationError
	return errors.As(err, &operationError) && operationError.Code == code
}
