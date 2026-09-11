package postgres_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

func TestPostgresForceUnlockAuthorizationAndCredentialResetAreAtomic(t *testing.T) {
	ctx := context.Background()
	mayUnlockPath, err := query.NewPath("mayUnlock")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var blockFirst atomic.Bool
	var administratorID, revokerID string
	config := ridu.Config{
		Name: "PostgreSQL atomic account unlock", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{
				Password:         ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
				MaxLoginAttempts: 1, LockDuration: time.Hour,
			},
			Fields: field.Fields{field.Email("email").Required().Unique(), field.Checkbox("mayUnlock").Required()},
			Access: ridu.CollectionAccess{Update: func(access ridu.AccessContext) (ridu.AccessDecision, error) {
				if access.Actor == nil {
					return ridu.Deny(), nil
				}
				if access.Actor.ID == revokerID {
					return ridu.Allow(), nil
				}
				if access.Actor.ID != administratorID {
					return ridu.Deny(), nil
				}
				if len(access.Data) == 0 && blockFirst.CompareAndSwap(false, true) {
					close(entered)
					select {
					case <-release:
					case <-access.Context.Done():
						return ridu.Deny(), access.Context.Err()
					}
				}
				return ridu.Where(query.Equal(mayUnlockPath, query.Boolean(true))), nil
			}},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(ctx, "users", store.Values{
		"email": store.String("target@example.test"), "mayUnlock": store.Boolean(true),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	administrator, err := application.Local().Create(ctx, "users", store.Values{
		"email": store.String("administrator@example.test"), "mayUnlock": store.Boolean(false),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	revoker, err := application.Local().Create(ctx, "users", store.Values{
		"email": store.String("revoker@example.test"), "mayUnlock": store.Boolean(false),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	administratorID, revokerID = administrator.ID, revoker.ID
	if err := application.SetPassword(ctx, "users", target.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	_, _ = application.Login(ctx, "users", "target@example.test", "wrong-horse")

	unlockResult := make(chan error, 1)
	go func() {
		unlockResult <- application.ForceUnlock(ctx, "users", target.ID, &ridu.AuthIdentity{Collection: "users", Actor: administrator})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("force unlock did not reach its filtered update authorization")
	}
	if _, err := application.Local().Update(ctx, "users", target.ID, store.Values{"mayUnlock": store.Boolean(false)}, ridu.MutationOptions{Actor: &revoker}); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-unlockResult:
		if !hasOperationCode(err, "access_denied") {
			t.Fatalf("force unlock after concurrent authorization revocation = %v, want access_denied", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("force unlock did not finish after authorization revocation")
	}
	if _, err := application.Login(ctx, "users", "target@example.test", "correct-horse"); !hasOperationCode(err, "access_denied") {
		t.Fatalf("denied force unlock cleared private credential state: %v", err)
	}

	if _, err := application.Local().Update(ctx, "users", target.ID, store.Values{"mayUnlock": store.Boolean(true)}, ridu.MutationOptions{Actor: &revoker}); err != nil {
		t.Fatal(err)
	}
	if err := application.ForceUnlock(ctx, "users", target.ID, &ridu.AuthIdentity{Collection: "users", Actor: administrator}); err != nil {
		t.Fatalf("authorized force unlock = %v", err)
	}
	if _, err := application.Login(ctx, "users", "target@example.test", "correct-horse"); err != nil {
		t.Fatalf("login after committed force unlock = %v", err)
	}
}
