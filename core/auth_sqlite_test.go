package core_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/riducms/ridu/adapters/sqlite"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"golang.org/x/crypto/bcrypt"
)

func TestSQLiteAuthInitializedOverlapsHeldWriter(t *testing.T) {
	ctx := context.Background()
	backend, application := sqliteAuthSnapshotFixture(t, nil)

	writer, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writerOpen := true
	defer func() {
		if writerOpen {
			_ = writer.Rollback(context.Background())
		}
	}()

	type result struct {
		initialized bool
		err         error
	}
	completed := make(chan result, 1)
	go func() {
		initialized, probeError := application.AuthInitialized(context.Background(), "users")
		completed <- result{initialized: initialized, err: probeError}
	}()

	select {
	case probe := <-completed:
		if probe.err != nil || probe.initialized {
			t.Fatalf("auth initialization probe = %t, %v; want false, nil", probe.initialized, probe.err)
		}
	case <-time.After(time.Second):
		if rollbackError := writer.Rollback(context.Background()); rollbackError != nil {
			t.Fatal(rollbackError)
		}
		writerOpen = false
		<-completed
		t.Fatal("auth initialization probe waited behind SQLite's writer gate")
	}

	if err := writer.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	writerOpen = false
}

func TestSQLiteAuthOwnerPhysicalProbeOverlapsHeldWriter(t *testing.T) {
	ctx := context.Background()
	backend, application := sqliteAuthSnapshotFixture(t, nil)
	user := createAuthUser(t, application, "snapshot-owner@example.test")
	if err := application.SetPassword(ctx, "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(ctx, "users", "snapshot-owner@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}

	emailPath, err := query.NewPath("email")
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := ridu.New(sqliteAuthSnapshotConfig(func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(emailPath, query.String("someone-else@example.test"))), nil
	}), backend)
	if err != nil {
		t.Fatal(err)
	}

	writer, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writerOpen := true
	defer func() {
		if writerOpen {
			_ = writer.Rollback(context.Background())
		}
	}()

	completed := make(chan error, 1)
	go func() {
		_, probeError := filtered.Session(context.Background(), session.Token)
		completed <- probeError
	}()

	select {
	case probeError := <-completed:
		if !operationCode(probeError, "access_denied") {
			t.Fatalf("filtered session probe = %v, want access_denied", probeError)
		}
	case <-time.After(time.Second):
		if rollbackError := writer.Rollback(context.Background()); rollbackError != nil {
			t.Fatal(rollbackError)
		}
		writerOpen = false
		<-completed
		t.Fatal("physical auth-owner probe waited behind SQLite's writer gate")
	}

	if err := writer.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	writerOpen = false
	if recovered, err := application.Session(ctx, session.Token); err != nil || recovered.User.ID != user.ID {
		t.Fatalf("physical owner probe revoked the valid session: %#v, %v", recovered, err)
	}
}

func sqliteAuthSnapshotFixture(t *testing.T, read ridu.AccessRule) (*sqlite.Store, *ridu.App) {
	t.Helper()
	ctx := context.Background()
	backend, err := sqlite.OpenWithConfig(ctx, sqlite.Config{
		Path:           filepath.Join(t.TempDir(), "auth-snapshots.sqlite"),
		MaxConnections: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	application, err := ridu.New(sqliteAuthSnapshotConfig(read), backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(ctx, application.Manifest()); err != nil {
		t.Fatal(err)
	}
	return backend, application
}

func sqliteAuthSnapshotConfig(read ridu.AccessRule) ridu.Config {
	return ridu.Config{
		Name: "SQLite auth snapshots", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Access:     ridu.CollectionAccess{Read: read},
			Fields:     []field.Definition{field.Email("email", field.Required(), field.Unique())},
		}},
	}
}
