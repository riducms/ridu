package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

func TestSQLiteOnlineBackupRestoreDrillIncludesWALState(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{
		Name:  "SQLite recovery drill",
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{
				Slug: "users", Auth: true,
				Fields: field.Fields{field.Email("email").Required().Unique()},
			},
			{
				Slug: "posts", Versions: true,
				VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields:        field.Fields{field.Text("title").Required(), field.Relationship("author", "users").Required()},
			},
		},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	migrations := filepath.Join(t.TempDir(), "migrations")
	if _, err := CreateArtifact(ctx, migrations, "initial", manifest, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}

	sourcePath := filepath.Join(t.TempDir(), "source.sqlite")
	source, err := Open(ctx, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	if err := source.ApplyArtifacts(ctx, migrations); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config, source)
	if err != nil {
		t.Fatal(err)
	}
	user, err := application.CreateAuthUser(ctx, "users", store.Values{
		"email": store.String("restore@example.test"),
	}, "recovery-drill-password", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Before backup"), "author": store.String(user.ID),
	}, ridu.MutationOptions{Actor: &user})
	if err != nil {
		t.Fatal(err)
	}
	published, err := application.Local().PublishChanges(ctx, "posts", draft.ID, store.Values{
		"title": store.String("Survives restore"),
	}, ridu.MutationOptions{Actor: &user, ExpectedRevision: draft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.Stat(sourcePath + "-wal")
	if err != nil {
		t.Fatalf("inspect source WAL before online backup: %v", err)
	}
	if wal.Size() == 0 {
		t.Fatal("source WAL is empty before online backup")
	}

	backupPath := filepath.Join(t.TempDir(), "online-backup.sqlite")
	if _, err := source.db.ExecContext(ctx, `VACUUM INTO ?`, backupPath); err != nil {
		t.Fatalf("create SQLite online backup: %v", err)
	}
	if _, err := application.Local().PublishChanges(ctx, "posts", published.ID, store.Values{
		"title": store.String("Changed after backup"),
	}, ridu.MutationOptions{Actor: &user, ExpectedRevision: published.Revision}); err != nil {
		t.Fatal(err)
	}

	restored, err := Open(ctx, backupPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	if err := restored.Ready(ctx, manifest); err != nil {
		t.Fatalf("restored SQLite readiness: %v", err)
	}
	statuses, err := restored.ArtifactStatus(ctx, migrations, manifest)
	if err != nil || len(statuses) != 1 || !statuses[0].Applied {
		t.Fatalf("restored SQLite migration ledger = %#v, %v", statuses, err)
	}
	restoredApplication, err := ridu.New(config, restored)
	if err != nil {
		t.Fatal(err)
	}
	login, err := restoredApplication.Login(ctx, "users", "restore@example.test", "recovery-drill-password")
	if err != nil {
		t.Fatalf("restored SQLite credentials: %v", err)
	}
	restoredPost, err := restoredApplication.Local().Find(ctx, "posts", published.ID, ridu.FindOptions{Actor: &login.User})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := restoredPost.Values["title"].StringValue(); title != "Survives restore" || restoredPost.Revision != published.Revision {
		t.Fatalf("restored SQLite document = %#v", restoredPost)
	}
	versions, err := restoredApplication.Local().Versions(ctx, "posts", published.ID, ridu.FindOptions{Actor: &login.User})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("restored SQLite versions = %#v", versions)
	}
}
