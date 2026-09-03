package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riducms/ridu"
	riducore "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

type authHookGate struct {
	enabled atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newAuthHookGate() *authHookGate {
	return &authHookGate{entered: make(chan struct{}), release: make(chan struct{})}
}

func (gate *authHookGate) wait() {
	gate.once.Do(func() { close(gate.entered) })
	<-gate.release
}

func TestPostgresCredentialHashFencesLoginUpgradeAndAPIKeyCreation(t *testing.T) {
	ctx := context.Background()
	loginGate, apiKeyGate := newAuthHookGate(), newAuthHookGate()
	config := ridu.Config{
		Name: "PostgreSQL credential hash fences", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{
				Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}, APIKeys: true,
				Hooks: ridu.AuthHooks{
					BeforeLogin: []ridu.AuthHook{func(ridu.AuthContext) error {
						if loginGate.enabled.Load() {
							loginGate.wait()
						}
						return nil
					}},
					BeforeAPIKey: []ridu.AuthHook{func(ridu.AuthContext) error {
						if apiKeyGate.enabled.Load() {
							apiKeyGate.wait()
						}
						return nil
					}},
				},
			},
			Fields: []field.Definition{field.Email("email", field.Required(), field.Unique())},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	user, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("fence@example.test")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(ctx, "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}

	loginGate.enabled.Store(true)
	loginResult := make(chan error, 1)
	go func() {
		_, err := application.Login(ctx, "users", "fence@example.test", "correct-horse")
		loginResult <- err
	}()
	select {
	case <-loginGate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("PostgreSQL login did not reach the verified-password barrier")
	}
	if err := application.SetPassword(ctx, "users", user.ID, "changed-horse"); err != nil {
		t.Fatal(err)
	}
	loginGate.enabled.Store(false)
	close(loginGate.release)
	if err := <-loginResult; !hasOperationCode(err, "access_denied") {
		t.Fatalf("stale PostgreSQL login = %v", err)
	}
	collection := manifest.Snapshot().Collections[0]
	if sessions, err := backend.ListSessions(ctx, collection.ID, user.ID, time.Now()); err != nil || len(sessions) != 0 {
		t.Fatalf("sessions after fenced PostgreSQL login = %#v, %v", sessions, err)
	}

	session, err := application.Login(ctx, "users", "fence@example.test", "changed-horse")
	if err != nil {
		t.Fatal(err)
	}
	apiKeyGate.enabled.Store(true)
	apiKeyResult := make(chan error, 1)
	go func() {
		_, err := application.CreateAPIKey(ctx, session.Token, "Late key", time.Now().Add(time.Hour))
		apiKeyResult <- err
	}()
	select {
	case <-apiKeyGate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("PostgreSQL API-key creation did not reach the session barrier")
	}
	credential, err := backend.FindAuthCredential(ctx, collection, "fence@example.test")
	if err != nil {
		t.Fatal(err)
	}
	stalePasswordHash := append([]byte(nil), credential.PasswordHash...)
	if err := application.SetPassword(ctx, "users", user.ID, "final-horse"); err != nil {
		t.Fatal(err)
	}
	apiKeyGate.enabled.Store(false)
	close(apiKeyGate.release)
	if err := <-apiKeyResult; !hasOperationCode(err, "access_denied") {
		t.Fatalf("late PostgreSQL API key = %v", err)
	}
	if keys, err := backend.ListAPIKeys(ctx, collection.ID, user.ID, time.Now()); err != nil || len(keys) != 0 {
		t.Fatalf("API keys after PostgreSQL reset fence = %#v, %v", keys, err)
	}

	staleUpgrade, err := bcrypt.GenerateFromPassword([]byte("changed-horse"), bcrypt.MinCost+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.UpgradePasswordHash(ctx, collection, user.ID, stalePasswordHash, staleUpgrade); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale PostgreSQL bcrypt upgrade = %v", err)
	}
	credential, err = backend.FindAuthCredential(ctx, collection, "fence@example.test")
	if err != nil || bcrypt.CompareHashAndPassword(credential.PasswordHash, []byte("final-horse")) != nil {
		t.Fatalf("PostgreSQL reset hash was overwritten: %#v, %v", credential, err)
	}
}

func TestPostgresCredentialHashFenceSurvivesHardDeleteAndSameIDRecreation(t *testing.T) {
	ctx := context.Background()
	loginGate, passwordGate := newAuthHookGate(), newAuthHookGate()
	baseConfig := ridu.Config{
		Name: "PostgreSQL credential reincarnation fence", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Fields:     []field.Definition{field.Email("email", field.Required(), field.Unique())},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, baseConfig)
	applyInitialArtifact(t, ctx, backend, manifest)
	base, err := ridu.New(baseConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	user, err := base.Local().Create(ctx, "users", store.Values{"email": store.String("recreated@example.test")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := base.SetPassword(ctx, "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	original, err := backend.FindAuthCredential(ctx, collection, "recreated@example.test")
	if err != nil {
		t.Fatal(err)
	}

	upgradedConfig := baseConfig
	upgradedConfig.Collections = append([]ridu.Collection(nil), baseConfig.Collections...)
	upgradedConfig.Collections[0].AuthConfig = ridu.AuthConfig{
		Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost + 1},
		Hooks: ridu.AuthHooks{
			BeforeLogin: []ridu.AuthHook{func(ridu.AuthContext) error {
				if loginGate.enabled.Load() {
					loginGate.wait()
				}
				return nil
			}},
			BeforePasswordReset: []ridu.AuthHook{func(ridu.AuthContext) error {
				if passwordGate.enabled.Load() {
					passwordGate.wait()
				}
				return nil
			}},
		},
	}
	upgraded, err := ridu.New(upgradedConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	loginGate.enabled.Store(true)
	staleLogin := make(chan error, 1)
	go func() {
		_, err := upgraded.Login(ctx, "users", "recreated@example.test", "correct-horse")
		staleLogin <- err
	}()
	select {
	case <-loginGate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("PostgreSQL stale login did not reach the verified-password barrier")
	}
	if _, err := base.Local().Delete(ctx, "users", user.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Local().Import(ctx, "users", store.Values{"email": store.String("recreated@example.test")}, riducore.ImportOptions{ID: user.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if err := base.SetPassword(ctx, "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	replacement, err := backend.FindAuthCredential(ctx, collection, "recreated@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(original.PasswordHash, replacement.PasswordHash) {
		t.Fatal("same-ID PostgreSQL credential recreation reused the previous bcrypt incarnation")
	}
	loginGate.enabled.Store(false)
	close(loginGate.release)
	if err := <-staleLogin; !hasOperationCode(err, "access_denied") {
		t.Fatalf("stale PostgreSQL login for recreated credential = %v", err)
	}
	after, err := backend.FindAuthCredential(ctx, collection, "recreated@example.test")
	if err != nil || !bytes.Equal(after.PasswordHash, replacement.PasswordHash) {
		t.Fatalf("stale PostgreSQL upgrade overwrote recreated credential: %#v, %v", after, err)
	}

	session, err := upgraded.Login(ctx, "users", "recreated@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	passwordGate.enabled.Store(true)
	staleChange := make(chan error, 1)
	go func() {
		staleChange <- upgraded.ChangePassword(ctx, session.Token, "correct-horse", "changed-horse")
	}()
	select {
	case <-passwordGate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("PostgreSQL stale password change did not reach the verified-password barrier")
	}
	if _, err := base.Local().Delete(ctx, "users", user.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Local().Import(ctx, "users", store.Values{"email": store.String("recreated@example.test")}, riducore.ImportOptions{ID: user.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if err := base.SetPassword(ctx, "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	passwordGate.enabled.Store(false)
	close(passwordGate.release)
	if err := <-staleChange; !hasOperationCode(err, "access_denied") {
		t.Fatalf("stale PostgreSQL password change for recreated credential = %v", err)
	}
	if _, err := upgraded.Login(ctx, "users", "recreated@example.test", "changed-horse"); !hasOperationCode(err, "access_denied") {
		t.Fatalf("stale PostgreSQL next password authenticated: %v", err)
	}
	if _, err := upgraded.Login(ctx, "users", "recreated@example.test", "correct-horse"); err != nil {
		t.Fatalf("replacement PostgreSQL credential was changed: %v", err)
	}
}

func TestPostgresAnonymousFirstAuthUserBootstrapHasOneWinner(t *testing.T) {
	ctx := context.Background()
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	config := ridu.Config{
		Name: "PostgreSQL atomic auth bootstrap", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Fields:     []field.Definition{field.Email("email", field.Required(), field.Unique())},
			Hooks: ridu.CollectionHooks{BeforeOperation: []ridu.Hook{func(hook ridu.HookContext) error {
				if hook.Operation == ridu.OperationCreate && hook.Actor == nil {
					arrived <- struct{}{}
					<-release
				}
				return nil
			}}},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		document store.Document
		err      error
	}
	results := make(chan result, 2)
	for _, email := range []string{"first@example.test", "second@example.test"} {
		email := email
		go func() {
			document, err := application.CreateAuthUserForTransport(ctx, "users", store.Values{"email": store.String(email)}, "correct-horse", nil)
			results <- result{document: document, err: err}
		}()
	}
	for index := 0; index < 2; index++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("PostgreSQL bootstrap did not reach both transaction barriers")
		}
	}
	close(release)
	succeeded, denied := 0, 0
	for index := 0; index < 2; index++ {
		current := <-results
		switch {
		case current.err == nil:
			succeeded++
		case hasOperationCode(current.err, "access_denied"):
			denied++
		default:
			t.Fatalf("PostgreSQL bootstrap result = %#v, %v", current.document, current.err)
		}
	}
	if succeeded != 1 || denied != 1 {
		t.Fatalf("PostgreSQL bootstrap outcomes = %d success, %d denied", succeeded, denied)
	}
	page, err := application.Local().List(ctx, "users", ridu.ListOptions{})
	if err != nil || page.Total != 1 {
		t.Fatalf("PostgreSQL bootstrapped users = %#v, %v", page, err)
	}
}
