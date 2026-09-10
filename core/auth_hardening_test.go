package core_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

type authOwnerReadFailureStore struct {
	*teststore.Store

	mu                  sync.Mutex
	nextFindError       error
	cancelOnFind        context.CancelFunc
	nextProbeError      error
	probeCallback       func() error
	probeCallbackError  error
	deleteCalls         int
	deleteContextError  error
	deleteHasDeadline   bool
	deleteDeadlineAfter time.Duration
	sessionDeleteCalls  int
	probeCalls          int
	probeContextError   error
	probeHasDeadline    bool
	probeDeadlineAfter  time.Duration
}

func (backend *authOwnerReadFailureStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	return backend.wrap(transaction, err)
}

func (backend *authOwnerReadFailureStore) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.BeginSnapshot(ctx)
	return backend.wrap(transaction, err)
}

func (backend *authOwnerReadFailureStore) wrap(transaction store.Transaction, err error) (store.Transaction, error) {
	if err != nil {
		return nil, err
	}
	backend.mu.Lock()
	armed := backend.nextFindError != nil || backend.nextProbeError != nil
	backend.mu.Unlock()
	if !armed {
		return transaction, nil
	}
	return &authOwnerReadFailureTransaction{Transaction: transaction, backend: backend}, nil
}

func (backend *authOwnerReadFailureStore) armProbeFailure(err error) {
	backend.armProbeFailureWithCallback(err, nil)
}

func (backend *authOwnerReadFailureStore) armProbeFailureWithCallback(err error, callback func() error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.nextProbeError = err
	backend.probeCallback = callback
}

func (backend *authOwnerReadFailureStore) armFindFailure(err error, cancel context.CancelFunc) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.nextFindError = err
	backend.cancelOnFind = cancel
}

func (backend *authOwnerReadFailureStore) takeProbeFailure(ctx context.Context) (error, bool) {
	backend.mu.Lock()
	backend.probeCalls++
	backend.probeContextError = ctx.Err()
	deadline, hasDeadline := ctx.Deadline()
	backend.probeHasDeadline = hasDeadline
	if hasDeadline {
		backend.probeDeadlineAfter = time.Until(deadline)
	}
	err := backend.nextProbeError
	callback := backend.probeCallback
	backend.nextProbeError = nil
	backend.probeCallback = nil
	backend.mu.Unlock()
	if callback != nil {
		callbackError := callback()
		backend.mu.Lock()
		backend.probeCallbackError = callbackError
		backend.mu.Unlock()
	}
	return err, err != nil
}

func (backend *authOwnerReadFailureStore) takeFindFailure() (error, context.CancelFunc) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	err, cancel := backend.nextFindError, backend.cancelOnFind
	backend.nextFindError = nil
	backend.cancelOnFind = nil
	return err, cancel
}

func (backend *authOwnerReadFailureStore) resetDeleteObservation() {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.deleteCalls = 0
	backend.deleteContextError = nil
	backend.deleteHasDeadline = false
	backend.deleteDeadlineAfter = 0
	backend.sessionDeleteCalls = 0
	backend.probeCalls = 0
	backend.probeContextError = nil
	backend.probeHasDeadline = false
	backend.probeDeadlineAfter = 0
	backend.probeCallback = nil
	backend.probeCallbackError = nil
}

func (backend *authOwnerReadFailureStore) sessionDeleteObservation() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.sessionDeleteCalls
}

func (backend *authOwnerReadFailureStore) deleteObservation() (int, error, bool, time.Duration) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.deleteCalls, backend.deleteContextError, backend.deleteHasDeadline, backend.deleteDeadlineAfter
}

func (backend *authOwnerReadFailureStore) probeObservation() (int, error, bool, time.Duration) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.probeCalls, backend.probeContextError, backend.probeHasDeadline, backend.probeDeadlineAfter
}

func (backend *authOwnerReadFailureStore) probeCallbackObservation() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.probeCallbackError
}

func (backend *authOwnerReadFailureStore) DeleteAPIKey(ctx context.Context, collectionID schema.StableID, userID, id string) error {
	backend.mu.Lock()
	backend.deleteCalls++
	backend.deleteContextError = ctx.Err()
	deadline, hasDeadline := ctx.Deadline()
	backend.deleteHasDeadline = hasDeadline
	if hasDeadline {
		backend.deleteDeadlineAfter = time.Until(deadline)
	}
	backend.mu.Unlock()
	return backend.Store.DeleteAPIKey(ctx, collectionID, userID, id)
}

func (backend *authOwnerReadFailureStore) DeleteSession(ctx context.Context, tokenHash string) error {
	backend.mu.Lock()
	backend.sessionDeleteCalls++
	backend.mu.Unlock()
	return backend.Store.DeleteSession(ctx, tokenHash)
}

type authOwnerReadFailureTransaction struct {
	store.Transaction
	backend *authOwnerReadFailureStore
}

func (transaction *authOwnerReadFailureTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	if request.Deletion == store.DeletionAll && request.Access == nil {
		if err, armed := transaction.backend.takeProbeFailure(ctx); armed {
			return store.Document{}, err
		}
	}
	if err, cancel := transaction.backend.takeFindFailure(); err != nil {
		if cancel != nil {
			cancel()
		}
		return store.Document{}, err
	}
	return transaction.Transaction.Find(ctx, request)
}

func TestPasswordPolicyHashUpgradeAndSessionRevocation(t *testing.T) {
	backend := teststore.New()
	application := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{
			MinLength:  12,
			BcryptCost: bcrypt.MinCost,
			Validate: func(password string) error {
				if strings.Contains(strings.ToLower(password), "password") {
					return errors.New("password must not contain the word password")
				}
				return nil
			},
		},
	})
	user := createAuthUser(t, application, "policy@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "short"); !operationCode(err, "validation") {
		t.Fatalf("short password error = %v", err)
	}
	if err := application.SetPassword(context.Background(), "users", user.ID, "password-safe-value"); !operationCode(err, "validation") {
		t.Fatalf("custom password error = %v", err)
	}
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	first, err := application.Login(context.Background(), "users", "POLICY@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := backend.FindAuthCredential(context.Background(), application.Manifest().Snapshot().Collections[0], "policy@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if cost, _ := bcrypt.Cost(credential.PasswordHash); cost != bcrypt.MinCost {
		t.Fatalf("initial bcrypt cost = %d", cost)
	}

	upgraded := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{MinLength: 12, BcryptCost: bcrypt.MinCost + 1},
	})
	if _, err := upgraded.Login(context.Background(), "users", "policy@example.test", "correct-horse"); err != nil {
		t.Fatal(err)
	}
	credential, err = backend.FindAuthCredential(context.Background(), upgraded.Manifest().Snapshot().Collections[0], "policy@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if cost, _ := bcrypt.Cost(credential.PasswordHash); cost != bcrypt.MinCost+1 {
		t.Fatalf("upgraded bcrypt cost = %d", cost)
	}
	if err := upgraded.SetPassword(context.Background(), "users", user.ID, "changed-horse"); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.Session(context.Background(), first.Token); !operationCode(err, "access_denied") {
		t.Fatalf("pre-change session remained valid: %v", err)
	}
}

func TestPasswordReplacementFencesInFlightLoginAndBcryptUpgrade(t *testing.T) {
	t.Run("session creation", func(t *testing.T) {
		backend := teststore.New()
		entered, release := make(chan struct{}), make(chan struct{})
		var block atomic.Bool
		application := authFixture(t, backend, ridu.AuthConfig{
			Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
			Hooks: ridu.AuthHooks{BeforeLogin: []ridu.AuthHook{func(ridu.AuthContext) error {
				if block.Load() {
					close(entered)
					<-release
				}
				return nil
			}}},
		})
		user := createAuthUser(t, application, "session-fence@example.test")
		if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
			t.Fatal(err)
		}
		block.Store(true)
		result := make(chan error, 1)
		go func() {
			_, err := application.Login(context.Background(), "users", "session-fence@example.test", "correct-horse")
			result <- err
		}()
		<-entered
		if err := application.SetPassword(context.Background(), "users", user.ID, "changed-horse"); err != nil {
			t.Fatal(err)
		}
		block.Store(false)
		close(release)
		if err := <-result; !operationCode(err, "access_denied") {
			t.Fatalf("stale verified login = %v", err)
		}
		collection := application.Manifest().Snapshot().Collections[0]
		if sessions, err := backend.ListSessions(context.Background(), collection.ID, user.ID, time.Now()); err != nil || len(sessions) != 0 {
			t.Fatalf("sessions after fenced login = %#v, %v", sessions, err)
		}
		if _, err := application.Login(context.Background(), "users", "session-fence@example.test", "changed-horse"); err != nil {
			t.Fatalf("replacement password login = %v", err)
		}
	})

	t.Run("bcrypt upgrade", func(t *testing.T) {
		backend := teststore.New()
		base := authFixture(t, backend, ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}})
		user := createAuthUser(t, base, "upgrade-fence@example.test")
		if err := base.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
			t.Fatal(err)
		}
		entered, release := make(chan struct{}), make(chan struct{})
		var block atomic.Bool
		upgraded := authFixture(t, backend, ridu.AuthConfig{
			Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost + 1},
			Hooks: ridu.AuthHooks{BeforeLogin: []ridu.AuthHook{func(ridu.AuthContext) error {
				if block.Load() {
					close(entered)
					<-release
				}
				return nil
			}}},
		})
		block.Store(true)
		result := make(chan error, 1)
		go func() {
			_, err := upgraded.Login(context.Background(), "users", "upgrade-fence@example.test", "correct-horse")
			result <- err
		}()
		<-entered
		if err := base.SetPassword(context.Background(), "users", user.ID, "changed-horse"); err != nil {
			t.Fatal(err)
		}
		block.Store(false)
		close(release)
		if err := <-result; !operationCode(err, "access_denied") {
			t.Fatalf("stale bcrypt upgrade login = %v", err)
		}
		collection := upgraded.Manifest().Snapshot().Collections[0]
		credential, err := backend.FindAuthCredential(context.Background(), collection, "upgrade-fence@example.test")
		if err != nil || bcrypt.CompareHashAndPassword(credential.PasswordHash, []byte("changed-horse")) != nil {
			t.Fatalf("newer reset hash was overwritten: %#v, %v", credential, err)
		}
		if _, err := upgraded.Login(context.Background(), "users", "upgrade-fence@example.test", "changed-horse"); err != nil {
			t.Fatalf("replacement password login = %v", err)
		}
	})
}

func TestPasswordReplacementFencesInFlightAPIKeyCreation(t *testing.T) {
	backend := teststore.New()
	entered, release := make(chan struct{}), make(chan struct{})
	var block atomic.Bool
	application := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}, APIKeys: true,
		Hooks: ridu.AuthHooks{BeforeAPIKey: []ridu.AuthHook{func(ridu.AuthContext) error {
			if block.Load() {
				close(entered)
				<-release
			}
			return nil
		}}},
	})
	user := createAuthUser(t, application, "api-key-fence@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(context.Background(), "users", "api-key-fence@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	block.Store(true)
	result := make(chan error, 1)
	go func() {
		_, err := application.CreateAPIKey(context.Background(), session.Token, "Late key", time.Now().Add(time.Hour))
		result <- err
	}()
	<-entered
	if err := application.SetPassword(context.Background(), "users", user.ID, "changed-horse"); err != nil {
		t.Fatal(err)
	}
	block.Store(false)
	close(release)
	if err := <-result; !operationCode(err, "access_denied") {
		t.Fatalf("late API key creation = %v", err)
	}
	collection := application.Manifest().Snapshot().Collections[0]
	if keys, err := backend.ListAPIKeys(context.Background(), collection.ID, user.ID, time.Now()); err != nil || len(keys) != 0 {
		t.Fatalf("API keys after reset fence = %#v, %v", keys, err)
	}
}

func TestCredentialHashFenceSurvivesHardDeleteAndSameIDRecreation(t *testing.T) {
	t.Run("login and bcrypt upgrade", func(t *testing.T) {
		backend := teststore.New()
		base := authFixture(t, backend, ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}})
		user := createAuthUser(t, base, "recreated-login@example.test")
		if err := base.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
			t.Fatal(err)
		}
		collection := base.Manifest().Snapshot().Collections[0]
		original, err := backend.FindAuthCredential(context.Background(), collection, "recreated-login@example.test")
		if err != nil {
			t.Fatal(err)
		}

		entered, release := make(chan struct{}), make(chan struct{})
		var block atomic.Bool
		upgraded := authFixture(t, backend, ridu.AuthConfig{
			Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost + 1},
			Hooks: ridu.AuthHooks{BeforeLogin: []ridu.AuthHook{func(ridu.AuthContext) error {
				if block.Load() {
					close(entered)
					<-release
				}
				return nil
			}}},
		})
		block.Store(true)
		result := make(chan error, 1)
		go func() {
			_, err := upgraded.Login(context.Background(), "users", "recreated-login@example.test", "correct-horse")
			result <- err
		}()
		<-entered
		if _, err := base.Local().Delete(context.Background(), "users", user.ID, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := base.Local().Import(context.Background(), "users", store.Values{"email": store.String("recreated-login@example.test")}, ridu.ImportOptions{ID: user.ID}, nil); err != nil {
			t.Fatal(err)
		}
		if err := base.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
			t.Fatal(err)
		}
		replacement, err := backend.FindAuthCredential(context.Background(), collection, "recreated-login@example.test")
		if err != nil {
			t.Fatal(err)
		}
		if string(replacement.PasswordHash) == string(original.PasswordHash) {
			t.Fatal("same-ID credential recreation reused the previous bcrypt incarnation")
		}
		block.Store(false)
		close(release)
		if err := <-result; !operationCode(err, "access_denied") {
			t.Fatalf("stale login for recreated credential = %v", err)
		}
		after, err := backend.FindAuthCredential(context.Background(), collection, "recreated-login@example.test")
		if err != nil || string(after.PasswordHash) != string(replacement.PasswordHash) {
			t.Fatalf("stale upgrade overwrote recreated credential: %#v, %v", after, err)
		}
		if _, err := upgraded.Login(context.Background(), "users", "recreated-login@example.test", "correct-horse"); err != nil {
			t.Fatalf("replacement credential login = %v", err)
		}
	})

	t.Run("password change", func(t *testing.T) {
		backend := teststore.New()
		entered, release := make(chan struct{}), make(chan struct{})
		var block atomic.Bool
		application := authFixture(t, backend, ridu.AuthConfig{
			Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
			Hooks: ridu.AuthHooks{BeforePasswordReset: []ridu.AuthHook{func(ridu.AuthContext) error {
				if block.Load() {
					close(entered)
					<-release
				}
				return nil
			}}},
		})
		user := createAuthUser(t, application, "recreated-change@example.test")
		if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
			t.Fatal(err)
		}
		session, err := application.Login(context.Background(), "users", "recreated-change@example.test", "correct-horse")
		if err != nil {
			t.Fatal(err)
		}
		block.Store(true)
		result := make(chan error, 1)
		go func() {
			result <- application.ChangePassword(context.Background(), session.Token, "correct-horse", "changed-horse")
		}()
		<-entered
		if _, err := application.Local().Delete(context.Background(), "users", user.ID, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := application.Local().Import(context.Background(), "users", store.Values{"email": store.String("recreated-change@example.test")}, ridu.ImportOptions{ID: user.ID}, nil); err != nil {
			t.Fatal(err)
		}
		if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
			t.Fatal(err)
		}
		block.Store(false)
		close(release)
		if err := <-result; !operationCode(err, "access_denied") {
			t.Fatalf("stale password change for recreated credential = %v", err)
		}
		if _, err := application.Login(context.Background(), "users", "recreated-change@example.test", "changed-horse"); !operationCode(err, "access_denied") {
			t.Fatalf("stale next password authenticated: %v", err)
		}
		if _, err := application.Login(context.Background(), "users", "recreated-change@example.test", "correct-horse"); err != nil {
			t.Fatalf("replacement credential was changed: %v", err)
		}
	})
}

func TestCreateAuthUserStoresCredentialInsideCreateTransaction(t *testing.T) {
	backend := teststore.New()
	application := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{MinLength: 12, BcryptCost: bcrypt.MinCost},
	})
	user, err := application.CreateAuthUser(
		context.Background(),
		"users",
		store.Values{"email": store.String("created@example.test")},
		"correct-horse",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exposed := user.Values["password"]; exposed {
		t.Fatalf("password entered document values: %#v", user.Values)
	}
	if _, err := application.Login(context.Background(), "users", "created@example.test", "correct-horse"); err != nil {
		t.Fatalf("created user could not log in: %v", err)
	}
	if strings.Join(backend.Events(), ",") != "begin,create,create-auth-credential,commit,begin,find,commit" {
		t.Fatalf("transaction events = %#v", backend.Events())
	}

	if _, err := application.CreateAuthUser(
		context.Background(), "users", store.Values{"email": store.String("weak@example.test")}, "short", nil,
	); !operationCode(err, "validation") {
		t.Fatalf("weak password create = %v", err)
	}
	page, err := application.Local().List(context.Background(), "users", ridu.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("users after rejected create = %d, want 1", page.Total)
	}
}

func TestAnonymousFirstAuthUserBootstrapIsAtomic(t *testing.T) {
	backend := teststore.New()
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	application, err := ridu.New(ridu.Config{
		Name: "Atomic auth bootstrap", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Fields:     field.Fields{field.Email("email").Required().Unique()},
			Hooks: ridu.CollectionHooks{BeforeOperation: []ridu.Hook{func(hook ridu.HookContext) error {
				if hook.Operation == operation.Create && hook.Actor == nil {
					arrived <- struct{}{}
					<-release
				}
				return nil
			}}},
		}},
	}, backend)
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
			document, err := application.CreateAuthUserForTransport(context.Background(), "users", store.Values{"email": store.String(email)}, "correct-horse", nil)
			results <- result{document: document, err: err}
		}()
	}
	for index := 0; index < 2; index++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent bootstrap did not reach the transaction barrier")
		}
	}
	close(release)
	succeeded, denied := 0, 0
	var winner store.Document
	for index := 0; index < 2; index++ {
		current := <-results
		switch {
		case current.err == nil:
			succeeded++
			winner = current.document
		case operationCode(current.err, "access_denied"):
			denied++
		default:
			t.Fatalf("bootstrap result = %#v, %v", current.document, current.err)
		}
	}
	if succeeded != 1 || denied != 1 {
		t.Fatalf("bootstrap outcomes = %d success, %d denied", succeeded, denied)
	}
	page, err := application.Local().List(context.Background(), "users", ridu.ListOptions{})
	if err != nil || page.Total != 1 || page.Documents[0].ID != winner.ID {
		t.Fatalf("bootstrapped users = %#v, %v", page, err)
	}
	identity, _ := winner.Values["email"].StringValue()
	collection := application.Manifest().Snapshot().Collections[0]
	if credential, err := backend.FindAuthCredential(context.Background(), collection, identity); err != nil || credential.User.ID != winner.ID || len(credential.PasswordHash) == 0 {
		t.Fatalf("bootstrapped credential = %#v, %v", credential, err)
	}
}

func TestAuthBootstrapAvailableIsLimitedToUninitializedAdminCollectionWithOmittedCreatePolicy(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Auth bootstrap availability", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
			{Slug: "staff", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if available, err := application.AuthBootstrapAvailable(context.Background(), "users"); err != nil || !available {
		t.Fatalf("empty admin bootstrap = %v, %v", available, err)
	}
	if available, err := application.AuthBootstrapAvailable(context.Background(), "staff"); err != nil || available {
		t.Fatalf("secondary auth bootstrap = %v, %v", available, err)
	}
	if _, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("admin@example.test")}, nil); err != nil {
		t.Fatal(err)
	}
	if available, err := application.AuthBootstrapAvailable(context.Background(), "users"); err != nil || available {
		t.Fatalf("initialized admin bootstrap = %v, %v", available, err)
	}
	if _, err := application.AuthBootstrapAvailable(context.Background(), "missing"); !operationCode(err, "unknown_auth_collection") {
		t.Fatalf("unknown auth bootstrap = %v", err)
	}

	publicRegistration, err := ridu.New(ridu.Config{
		Name: "Public registration", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			Fields: field.Fields{field.Email("email").Required().Unique()},
			Access: ridu.CollectionAccess{Create: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Allow(), nil
			}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if available, err := publicRegistration.AuthBootstrapAvailable(context.Background(), "users"); err != nil || available {
		t.Fatalf("explicit create policy bootstrap = %v, %v", available, err)
	}
}

func TestAnonymousFirstAdminBootstrapCanSetFieldsProtectedFromAnonymousCreation(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Protected bootstrap fields", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Fields: field.Fields{field.Email("email").Required().Unique(), field.Text("role").Required().Access(field.Access{Create: func(ctx operation.AccessContext,

			) (bool, error) {
				return ctx.Actor.ID != "",

					nil
			}})},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	user, err := application.CreateAuthUserForTransport(context.Background(), "users", store.Values{
		"email": store.String("admin@example.test"),
		"role":  store.String("administrator"),
	}, "correct-horse", nil)
	if err != nil {
		t.Fatal(err)
	}
	if role, _ := user.Values["role"].StringValue(); role != "administrator" {
		t.Fatalf("bootstrapped role = %q", role)
	}
	if _, err := application.Local().Create(context.Background(), "users", store.Values{
		"email": store.String("ordinary@example.test"),
		"role":  store.String("administrator"),
	}, nil); !operationCode(err, "field_access_denied") {
		t.Fatalf("ordinary anonymous field access = %v", err)
	}
}

func TestAnonymousFirstAdminBootstrapDoesNotDependOnEmailVerification(t *testing.T) {
	backend := teststore.New()
	deliveries := 0
	application := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
		Verify: &ridu.VerifyEmailConfig{Send: func(context.Context, ridu.VerifyEmailNotification) error {
			deliveries++
			return nil
		}},
	})
	if _, err := application.CreateAuthUserForTransport(context.Background(), "users", store.Values{
		"email": store.String("bootstrap-verified@example.test"),
	}, "correct-horse", nil); err != nil {
		t.Fatal(err)
	}
	if deliveries != 0 {
		t.Fatalf("first-admin bootstrap verification deliveries = %d, want 0", deliveries)
	}
	if _, err := application.Login(context.Background(), "users", "bootstrap-verified@example.test", "correct-horse"); err != nil {
		t.Fatalf("first admin was left unverified: %v", err)
	}
}

func TestCanceledSessionResolutionDoesNotRevokeValidSession(t *testing.T) {
	backend := teststore.New()
	application := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
	})
	user := createAuthUser(t, application, "cancelled@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(context.Background(), "users", "cancelled@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := application.Session(canceled, session.Token); !operationCode(err, "access_denied") {
		t.Fatalf("canceled session resolution = %v", err)
	}
	if resolved, err := application.Session(context.Background(), session.Token); err != nil || resolved.User.ID != user.ID {
		t.Fatalf("session was revoked by canceled request: %#v, %v", resolved, err)
	}
}

func TestAccountLockoutPersistsAndUsesGenericCredentialFailure(t *testing.T) {
	backend := teststore.New()
	config := ridu.AuthConfig{
		Password:         ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
		MaxLoginAttempts: 2,
		LockDuration:     time.Minute,
	}
	application := authFixture(t, backend, config)
	user := createAuthUser(t, application, "locked@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		_, err := application.Login(context.Background(), "users", "locked@example.test", "wrong-horse")
		if !operationCode(err, "access_denied") || err.Error() != "invalid email or password" {
			t.Fatalf("failed login %d = %v", attempt+1, err)
		}
	}
	if _, err := application.Login(context.Background(), "users", "missing@example.test", "wrong-horse"); !operationCode(err, "access_denied") || err.Error() != "invalid email or password" {
		t.Fatalf("missing identity failure = %v", err)
	}
	restarted := authFixture(t, backend, config)
	if _, err := restarted.Login(context.Background(), "users", "locked@example.test", "correct-horse"); !operationCode(err, "access_denied") {
		t.Fatalf("locked account logged in after restart: %v", err)
	}
}

func TestAuthorizedAccountUnlockClearsPersistentLockout(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{
		Name: "Auth unlock", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}, MaxLoginAttempts: 2, LockDuration: time.Hour},
			Access:     ridu.CollectionAccess{Update: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }},
			Fields:     field.Fields{field.Text("email").Required().Unique()},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	user := createAuthUser(t, application, "unlock@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		_, _ = application.Login(context.Background(), "users", "unlock@example.test", "wrong-horse")
	}
	if err := application.ForceUnlock(context.Background(), "users", user.ID, &ridu.AuthIdentity{Collection: "users", Actor: user}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Login(context.Background(), "users", "unlock@example.test", "correct-horse"); err != nil {
		t.Fatalf("login after force unlock = %v", err)
	}
}

func TestAccountUnlockWithOmittedUpdateAccessFailsClosed(t *testing.T) {
	backend := teststore.New()
	application := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}, MaxLoginAttempts: 1, LockDuration: time.Hour,
	})
	user := createAuthUser(t, application, "closed-unlock@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	_, _ = application.Login(context.Background(), "users", "closed-unlock@example.test", "wrong-horse")
	if err := application.ForceUnlock(context.Background(), "users", user.ID, nil); !operationCode(err, "access_denied") {
		t.Fatalf("anonymous unlock with omitted update access = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/users/"+user.ID+"/unlock", nil)
	response := httptest.NewRecorder()
	application.Handler(ridu.HandlerOptions{}).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("anonymous REST unlock status = %d, want 403: %s", response.Code, response.Body.String())
	}
	if err := application.ForceUnlock(context.Background(), "users", user.ID, &ridu.AuthIdentity{Collection: "users", Actor: user}); !operationCode(err, "access_denied") {
		t.Fatalf("authenticated unlock with omitted update access = %v", err)
	}
	if _, err := application.Login(context.Background(), "users", "closed-unlock@example.test", "correct-horse"); !operationCode(err, "access_denied") {
		t.Fatalf("denied unlock cleared persistent lockout: %v", err)
	}
}

func TestLoginMetadataIsBoundedBeforePersistenceAndHooks(t *testing.T) {
	backend := teststore.New()
	var hooked ridu.AuthContext
	application := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
		Hooks: ridu.AuthHooks{BeforeLogin: []ridu.AuthHook{func(authContext ridu.AuthContext) error {
			hooked = authContext
			return nil
		}}},
	})
	user := createAuthUser(t, application, "metadata@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session, err := application.LoginWithOptions(context.Background(), "users", "metadata@example.test", "correct-horse", ridu.LoginOptions{
		IPAddress: strings.Repeat("1", 200),
		UserAgent: strings.Repeat("界", 300),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hooked.IPAddress) != 128 || len(hooked.UserAgent) > 512 || !strings.HasSuffix(hooked.UserAgent, "界") {
		t.Fatalf("bounded hook metadata = IP %d bytes, user agent %d bytes", len(hooked.IPAddress), len(hooked.UserAgent))
	}
	sessions, err := application.Sessions(context.Background(), session.Token)
	if err != nil || len(sessions) != 1 || sessions[0].IPAddress != hooked.IPAddress || sessions[0].UserAgent != hooked.UserAgent {
		t.Fatalf("persisted metadata = %#v, %v", sessions, err)
	}
}

func TestRESTSessionInventoryRevocationAndLogoutAll(t *testing.T) {
	application := httpFixture(t)
	user := createAuthUser(t, application, "sessions@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	first := loginRequest(t, client, "Browser one")
	second := loginRequest(t, client, "Browser two")

	response := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/auth/sessions", nil, second.String())
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list sessions = %d: %s", response.StatusCode, readBody(t, response))
	}
	var listed protocol.AuthSessionsEnvelope
	decodeResponse(t, response, &listed)
	if len(listed.Sessions) != 2 {
		t.Fatalf("sessions = %#v", listed.Sessions)
	}
	var firstID string
	for _, session := range listed.Sessions {
		if session.UserAgent == "Browser one" {
			firstID = session.ID
		}
	}
	if firstID == "" {
		t.Fatalf("first browser session missing: %#v", listed.Sessions)
	}
	revoked := requestJSON(t, client, http.MethodDelete, "http://ridu.test/api/auth/sessions/"+firstID, nil, second.String())
	if revoked.StatusCode != http.StatusOK {
		t.Fatalf("revoke session = %d: %s", revoked.StatusCode, readBody(t, revoked))
	}
	if current := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/auth/me", nil, first.String()); current.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d", current.StatusCode)
	}
	loggedOut := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/logout-all", nil, second.String())
	if loggedOut.StatusCode != http.StatusOK || loggedOut.Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout-all = %d %#v", loggedOut.StatusCode, loggedOut.Cookies())
	}
	if current := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/auth/me", nil, second.String()); current.StatusCode != http.StatusUnauthorized {
		t.Fatalf("logout-all session status = %d", current.StatusCode)
	}
}

func TestVerificationAndPasswordResetTokensAreSingleUse(t *testing.T) {
	backend := teststore.New()
	var verificationToken, resetToken string
	application := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{MinLength: 12, BcryptCost: bcrypt.MinCost},
		Verify: &ridu.VerifyEmailConfig{Send: func(_ context.Context, notification ridu.VerifyEmailNotification) error {
			verificationToken = notification.Token
			return nil
		}},
		PasswordReset: ridu.PasswordResetConfig{Send: func(_ context.Context, notification ridu.PasswordResetNotification) error {
			resetToken = notification.Token
			return nil
		}},
	})
	user := createAuthUser(t, application, "verify@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	if verificationToken == "" {
		t.Fatal("verification token was not delivered")
	}
	if _, err := application.Login(context.Background(), "users", "verify@example.test", "correct-horse"); !operationCode(err, "email_not_verified") {
		t.Fatalf("unverified login = %v", err)
	}
	if err := application.VerifyEmail(context.Background(), "users", verificationToken); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(context.Background(), "users", user.ID, store.Values{"email": store.String("changed@example.test")}, &user); !operationCode(err, "validation") {
		t.Fatalf("verified identity update = %v", err)
	}
	if err := application.VerifyEmail(context.Background(), "users", verificationToken); !operationCode(err, "invalid_auth_token") {
		t.Fatalf("reused verification token = %v", err)
	}
	session, err := application.Login(context.Background(), "users", "verify@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	deliveries := 0
	applicationWithCount := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{MinLength: 12, BcryptCost: bcrypt.MinCost},
		Verify:   &ridu.VerifyEmailConfig{Send: func(context.Context, ridu.VerifyEmailNotification) error { return nil }},
		PasswordReset: ridu.PasswordResetConfig{Send: func(_ context.Context, notification ridu.PasswordResetNotification) error {
			deliveries++
			resetToken = notification.Token
			return nil
		}},
	})
	if err := applicationWithCount.RequestPasswordReset(context.Background(), "users", "missing@example.test"); err != nil || deliveries != 0 {
		t.Fatalf("unknown reset request = %v, deliveries = %d", err, deliveries)
	}
	if err := applicationWithCount.RequestPasswordReset(context.Background(), "users", "VERIFY@example.test"); err != nil || deliveries != 1 || resetToken == "" {
		t.Fatalf("reset request = %v, deliveries = %d", err, deliveries)
	}
	if err := applicationWithCount.ResetPassword(context.Background(), "users", resetToken, "too-short"); !operationCode(err, "validation") {
		t.Fatalf("weak reset password = %v", err)
	}
	if err := applicationWithCount.ResetPassword(context.Background(), "users", resetToken, "changed-horse"); err != nil {
		t.Fatal(err)
	}
	if err := applicationWithCount.ResetPassword(context.Background(), "users", resetToken, "another-horse"); !operationCode(err, "invalid_auth_token") {
		t.Fatalf("reused reset token = %v", err)
	}
	if _, err := applicationWithCount.Session(context.Background(), session.Token); !operationCode(err, "access_denied") {
		t.Fatalf("session survived reset = %v", err)
	}
	if _, err := applicationWithCount.Login(context.Background(), "users", "verify@example.test", "correct-horse"); !operationCode(err, "access_denied") {
		t.Fatalf("old password login = %v", err)
	}
	if _, err := applicationWithCount.Login(context.Background(), "users", "verify@example.test", "changed-horse"); err != nil {
		t.Fatal(err)
	}
}

func TestAPIKeysExternalStrategiesAuthHooksAndDistributedRateLimit(t *testing.T) {
	backend := teststore.New()
	var userID string
	var beforeLogin, afterLogin, afterMe, beforeRefresh, afterRefresh, beforeLogout, afterLogout int
	var beforeAPIKey, afterAPIKey int
	application := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
		APIKeys:  true,
		Hooks: ridu.AuthHooks{
			BeforeLogin:   []ridu.AuthHook{func(ridu.AuthContext) error { beforeLogin++; return nil }},
			AfterLogin:    []ridu.AuthHook{func(ridu.AuthContext) error { afterLogin++; return nil }},
			AfterMe:       []ridu.AuthHook{func(ridu.AuthContext) error { afterMe++; return nil }},
			BeforeRefresh: []ridu.AuthHook{func(ridu.AuthContext) error { beforeRefresh++; return nil }},
			AfterRefresh:  []ridu.AuthHook{func(ridu.AuthContext) error { afterRefresh++; return nil }},
			BeforeLogout:  []ridu.AuthHook{func(ridu.AuthContext) error { beforeLogout++; return nil }},
			AfterLogout:   []ridu.AuthHook{func(ridu.AuthContext) error { afterLogout++; return nil }},
			BeforeAPIKey:  []ridu.AuthHook{func(ridu.AuthContext) error { beforeAPIKey++; return nil }},
			AfterAPIKey:   []ridu.AuthHook{func(ridu.AuthContext) error { afterAPIKey++; return nil }},
		},
		Strategies: []ridu.AuthStrategy{{Name: "test-header", Authenticate: func(authContext ridu.AuthStrategyContext) (ridu.AuthStrategyResult, error) {
			values := authContext.Headers["X-Test-Auth"]
			return ridu.AuthStrategyResult{Authenticated: len(values) == 1 && values[0] == "accepted", UserID: userID}, nil
		}}},
	})
	user := createAuthUser(t, application, "keys@example.test")
	userID = user.ID
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(context.Background(), "users", "keys@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if beforeLogin != 1 || afterLogin != 1 {
		t.Fatalf("login hooks = before %d after %d", beforeLogin, afterLogin)
	}
	if current, err := application.Session(context.Background(), session.Token); err != nil || current.User.ID != user.ID || afterMe != 1 {
		t.Fatalf("me = %#v, %v; hooks %d", current, err, afterMe)
	}
	key, err := application.CreateAPIKey(context.Background(), session.Token, "Deploy", time.Now().Add(time.Hour))
	if err != nil || !strings.HasPrefix(key.Key, "ridu_") {
		t.Fatalf("create API key = %#v, %v", key, err)
	}
	actor, err := application.AuthenticateAPIKey(context.Background(), key.Key)
	if err != nil || actor.ID != user.ID {
		t.Fatalf("API key actor = %#v, %v", actor, err)
	}
	apiKeyIdentity, err := application.AuthenticateAPIKeyIdentity(context.Background(), key.Key)
	if err != nil || apiKeyIdentity.Collection != "users" || apiKeyIdentity.Actor.ID != user.ID {
		t.Fatalf("API key identity = %#v, %v", apiKeyIdentity, err)
	}
	mutated := key.Key[:len(key.Key)-1] + "0"
	if mutated == key.Key {
		mutated = key.Key[:len(key.Key)-1] + "1"
	}
	if _, err := application.AuthenticateAPIKey(context.Background(), mutated); !operationCode(err, "access_denied") {
		t.Fatalf("mutated API key = %v", err)
	}
	keys, err := application.APIKeys(context.Background(), session.Token)
	if err != nil || len(keys) != 1 || keys[0].LastUsedAt.IsZero() {
		t.Fatalf("API keys = %#v, %v", keys, err)
	}
	if err := application.RevokeAPIKey(context.Background(), session.Token, key.ID); err != nil {
		t.Fatal(err)
	}
	if beforeAPIKey != 3 || afterAPIKey != 3 {
		t.Fatalf("API key hooks = before %d after %d", beforeAPIKey, afterAPIKey)
	}
	if _, err := application.AuthenticateAPIKey(context.Background(), key.Key); !operationCode(err, "access_denied") {
		t.Fatalf("revoked API key = %v", err)
	}
	external, err := application.AuthenticateExternal(context.Background(), map[string][]string{"X-Test-Auth": {"accepted"}})
	if err != nil || external.ID != user.ID {
		t.Fatalf("external actor = %#v, %v", external, err)
	}
	externalIdentity, err := application.AuthenticateExternalIdentity(context.Background(), map[string][]string{"X-Test-Auth": {"accepted"}})
	if err != nil || externalIdentity.Collection != "users" || externalIdentity.Actor.ID != user.ID {
		t.Fatalf("external identity = %#v, %v", externalIdentity, err)
	}
	rotated, err := application.RotateSession(context.Background(), session.Token)
	if err != nil || beforeRefresh != 1 || afterRefresh != 1 {
		t.Fatalf("refresh = %#v, %v; hooks before %d after %d", rotated, err, beforeRefresh, afterRefresh)
	}
	if err := application.Logout(context.Background(), rotated.Token); err != nil || beforeLogout != 1 || afterLogout != 1 {
		t.Fatalf("logout = %v; hooks before %d after %d", err, beforeLogout, afterLogout)
	}

	firstHandler := handlerClient(application.Handler(ridu.HandlerOptions{AuthRateLimit: 1, AuthRateWindow: time.Minute}))
	secondHandler := handlerClient(application.Handler(ridu.HandlerOptions{AuthRateLimit: 1, AuthRateWindow: time.Minute}))
	first := requestJSON(t, firstHandler, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"keys@example.test","password":"wrong-horse"}`), "")
	if first.StatusCode != http.StatusUnauthorized {
		t.Fatalf("first distributed attempt = %d", first.StatusCode)
	}
	second := requestJSON(t, secondHandler, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"keys@example.test","password":"correct-horse"}`), "")
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second distributed attempt = %d: %s", second.StatusCode, readBody(t, second))
	}
}

func TestAPIKeyOwnerReloadOnlyDeletesConfirmedOrphans(t *testing.T) {
	backend := &authOwnerReadFailureStore{Store: teststore.New()}
	application := authFixture(t, backend, ridu.AuthConfig{
		Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
		APIKeys:  true,
	})
	user := createAuthUser(t, application, "owner-reload@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(context.Background(), "users", "owner-reload@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	key, err := application.CreateAPIKey(context.Background(), session.Token, "Deploy", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "request cancellation", err: context.Canceled},
		{name: "concurrent conflict", err: store.ErrConflict},
		{name: "database deadlock", err: errors.New("deadlock detected")},
		{name: "read hook failure", err: errors.New("before-read hook failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend.resetDeleteObservation()
			backend.armFindFailure(test.err, nil)
			if _, err := application.AuthenticateAPIKeyIdentity(context.Background(), key.Key); !operationCode(err, "access_denied") {
				t.Fatalf("authentication error = %v", err)
			}
			if calls, _, _, _ := backend.deleteObservation(); calls != 0 {
				t.Fatalf("transient owner read deleted the API key %d times", calls)
			}
			if _, err := backend.FindAPIKey(context.Background(), key.ID, time.Now().UTC()); err != nil {
				t.Fatalf("transient owner read removed the durable key: %v", err)
			}
			if identity, err := application.AuthenticateAPIKeyIdentity(context.Background(), key.Key); err != nil || identity.Actor.ID != user.ID {
				t.Fatalf("retained API key did not recover: %#v, %v", identity, err)
			}
		})
	}

	backend.resetDeleteObservation()
	parent, cancel := context.WithCancel(context.Background())
	backend.armFindFailure(store.ErrNotFound, cancel)
	now := time.Now().UTC()
	collection := application.Manifest().Snapshot().Collections[0]
	replacementKey := store.AuthAPIKey{
		ID: "replacement-key-after-probe", TokenHash: "replacement-key-hash",
		CollectionID: collection.ID, UserID: user.ID, Name: "Replacement",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	backend.armProbeFailureWithCallback(store.ErrNotFound, func() error {
		return backend.Store.CreateAPIKey(context.Background(), replacementKey, authTestTokenDigest(session.Token), now)
	})
	if _, err := application.AuthenticateAPIKeyIdentity(parent, key.Key); !operationCode(err, "access_denied") {
		t.Fatalf("orphan authentication error = %v", err)
	}
	probeCalls, probeError, probeHasDeadline, probeDeadlineAfter := backend.probeObservation()
	if probeCalls != 1 || probeError != nil {
		t.Fatalf("confirmed orphan physical probes = %d, context error %v", probeCalls, probeError)
	}
	if !probeHasDeadline || probeDeadlineAfter <= 0 || probeDeadlineAfter > 10*time.Second {
		t.Fatalf("confirmed orphan probe deadline = present %t, remaining %s", probeHasDeadline, probeDeadlineAfter)
	}
	calls, cleanupError, hasDeadline, deadlineAfter := backend.deleteObservation()
	if calls != 1 {
		t.Fatalf("confirmed orphan cleanup calls = %d, want 1", calls)
	}
	if cleanupError != nil {
		t.Fatalf("confirmed orphan cleanup inherited canceled request: %v", cleanupError)
	}
	if !hasDeadline || deadlineAfter <= 0 || deadlineAfter > 10*time.Second {
		t.Fatalf("confirmed orphan cleanup deadline = present %t, remaining %s", hasDeadline, deadlineAfter)
	}
	if _, err := backend.FindAPIKey(context.Background(), key.ID, time.Now().UTC()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("confirmed orphan key remained stored: %v", err)
	}
	if callbackError := backend.probeCallbackObservation(); callbackError != nil {
		t.Fatalf("same-ID owner reincarnation callback: %v", callbackError)
	}
	if survived, err := backend.FindAPIKey(context.Background(), replacementKey.ID, time.Now().UTC()); err != nil || survived.TokenHash != replacementKey.TokenHash {
		t.Fatalf("stale-key cleanup revoked a newly issued credential: %#v, %v", survived, err)
	}
}

func TestAuthOwnerFilteredReadDoesNotRevokePhysicalCredentials(t *testing.T) {
	backend := teststore.New()
	auth := ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}, APIKeys: true}
	unfiltered := authFixture(t, backend, auth)
	user := createAuthUser(t, unfiltered, "filtered-owner@example.test")
	if err := unfiltered.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session, err := unfiltered.Login(context.Background(), "users", "filtered-owner@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	key, err := unfiltered.CreateAPIKey(context.Background(), session.Token, "Filtered owner", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	emailPath, err := query.NewPath("email")
	if err != nil {
		t.Fatal(err)
	}
	filtered := authReadFixture(t, backend, auth, func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(emailPath, query.String("another-owner@example.test"))), nil
	})
	if _, err := filtered.Session(context.Background(), session.Token); !operationCode(err, "access_denied") {
		t.Fatalf("filtered session authentication = %v", err)
	}
	if _, err := filtered.AuthenticateAPIKeyIdentity(context.Background(), key.Key); !operationCode(err, "access_denied") {
		t.Fatalf("filtered API-key authentication = %v", err)
	}
	if recovered, err := unfiltered.Session(context.Background(), session.Token); err != nil || recovered.User.ID != user.ID {
		t.Fatalf("filtered Read revoked the physical session: %#v, %v", recovered, err)
	}
	if recovered, err := unfiltered.AuthenticateAPIKeyIdentity(context.Background(), key.Key); err != nil || recovered.Actor.ID != user.ID || recovered.Collection != "users" {
		t.Fatalf("filtered Read revoked the physical API key: %#v, %v", recovered, err)
	}
}

func TestAuthOwnerPhysicalProbeFailuresPreserveCredentials(t *testing.T) {
	backend := &authOwnerReadFailureStore{Store: teststore.New()}
	auth := ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}, APIKeys: true}
	unfiltered := authFixture(t, backend, auth)
	user := createAuthUser(t, unfiltered, "probe-owner@example.test")
	if err := unfiltered.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session, err := unfiltered.Login(context.Background(), "users", "probe-owner@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	key, err := unfiltered.CreateAPIKey(context.Background(), session.Token, "Probe owner", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	emailPath, err := query.NewPath("email")
	if err != nil {
		t.Fatal(err)
	}
	filtered := authReadFixture(t, backend, auth, func(ridu.AccessContext) (ridu.AccessDecision, error) {
		return ridu.Where(query.Equal(emailPath, query.String("not-the-owner@example.test"))), nil
	})

	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "probe cancellation", err: context.Canceled},
		{name: "probe store failure", err: errors.New("physical owner probe failed")},
		{name: "ambiguous joined not found", err: errors.Join(store.ErrNotFound, errors.New("deadlock while confirming owner"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend.resetDeleteObservation()
			backend.armProbeFailure(test.err)
			if _, err := filtered.Session(context.Background(), session.Token); !operationCode(err, "access_denied") {
				t.Fatalf("filtered session authentication = %v", err)
			}
			assertBoundedPhysicalProbe(t, backend)
			if backend.sessionDeleteObservation() != 0 {
				t.Fatal("ambiguous physical probe deleted the valid session")
			}
			if recovered, err := unfiltered.Session(context.Background(), session.Token); err != nil || recovered.User.ID != user.ID {
				t.Fatalf("ambiguous physical probe revoked the session: %#v, %v", recovered, err)
			}

			backend.resetDeleteObservation()
			backend.armProbeFailure(test.err)
			if _, err := filtered.AuthenticateAPIKeyIdentity(context.Background(), key.Key); !operationCode(err, "access_denied") {
				t.Fatalf("filtered API-key authentication = %v", err)
			}
			assertBoundedPhysicalProbe(t, backend)
			if calls, _, _, _ := backend.deleteObservation(); calls != 0 {
				t.Fatalf("ambiguous physical probe deleted the valid API key %d times", calls)
			}
			if recovered, err := unfiltered.AuthenticateAPIKeyIdentity(context.Background(), key.Key); err != nil || recovered.Actor.ID != user.ID {
				t.Fatalf("ambiguous physical probe revoked the API key: %#v, %v", recovered, err)
			}
		})
	}

	backend.resetDeleteObservation()
	collection := unfiltered.Manifest().Snapshot().Collections[0]
	credential, err := backend.FindAuthCredential(context.Background(), collection, "probe-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	replacementSession := store.AuthSession{
		ID: "replacement-session-after-probe", TokenHash: "replacement-session-token-hash",
		CollectionID: collection.ID, UserID: user.ID, CreatedAt: now, LastSeenAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	backend.armProbeFailureWithCallback(store.ErrNotFound, func() error {
		return backend.Store.CreateSession(context.Background(), replacementSession, credential.PasswordHash)
	})
	if _, err := filtered.Session(context.Background(), session.Token); !operationCode(err, "access_denied") {
		t.Fatalf("confirmed orphan session authentication = %v", err)
	}
	assertBoundedPhysicalProbe(t, backend)
	if backend.sessionDeleteObservation() != 1 {
		t.Fatalf("confirmed physical absence deleted the session %d times", backend.sessionDeleteObservation())
	}
	if _, err := unfiltered.Session(context.Background(), session.Token); !operationCode(err, "access_denied") {
		t.Fatalf("confirmed orphan session remained usable: %v", err)
	}
	if callbackError := backend.probeCallbackObservation(); callbackError != nil {
		t.Fatalf("same-ID owner reincarnation callback: %v", callbackError)
	}
	if survived, err := backend.FindSession(context.Background(), replacementSession.TokenHash, time.Now().UTC()); err != nil || survived.ID != replacementSession.ID {
		t.Fatalf("stale-session cleanup revoked a newly issued credential: %#v, %v", survived, err)
	}
	if recovered, err := unfiltered.AuthenticateAPIKeyIdentity(context.Background(), key.Key); err != nil || recovered.Actor.ID != user.ID {
		t.Fatalf("session cleanup crossed credential family or owner identity: %#v, %v", recovered, err)
	}
}

func assertBoundedPhysicalProbe(t *testing.T, backend *authOwnerReadFailureStore) {
	t.Helper()
	calls, contextError, hasDeadline, deadlineAfter := backend.probeObservation()
	if calls != 1 || contextError != nil {
		t.Fatalf("physical owner probes = %d, context error %v", calls, contextError)
	}
	if !hasDeadline || deadlineAfter <= 0 || deadlineAfter > 10*time.Second {
		t.Fatalf("physical owner probe deadline = present %t, remaining %s", hasDeadline, deadlineAfter)
	}
}

func TestAuthAccessCanDenyAnOtherwiseValidLogin(t *testing.T) {
	application := authFixture(t, teststore.New(), ridu.AuthConfig{
		Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
		Access:   ridu.AuthAccess{Login: func(ridu.AuthContext) (bool, error) { return false, nil }},
	})
	user := createAuthUser(t, application, "denied@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Login(context.Background(), "users", "denied@example.test", "correct-horse"); !operationCode(err, "access_denied") {
		t.Fatalf("denied login = %v", err)
	}
}

func TestChangePasswordRequiresCurrentPasswordAndRevokesSessions(t *testing.T) {
	application := authFixture(t, teststore.New(), ridu.AuthConfig{Password: ridu.PasswordPolicy{MinLength: 12, BcryptCost: bcrypt.MinCost}, APIKeys: true})
	user := createAuthUser(t, application, "change@example.test")
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	first, err := application.Login(context.Background(), "users", "change@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Login(context.Background(), "users", "change@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	key, err := application.CreateAPIKey(context.Background(), second.Token, "Compromised", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ChangePassword(context.Background(), first.Token, "wrong-horse", "changed-horse"); !operationCode(err, "access_denied") {
		t.Fatalf("wrong current password = %v", err)
	}
	if _, err := application.Session(context.Background(), first.Token); err != nil {
		t.Fatalf("failed change revoked session: %v", err)
	}
	if err := application.ChangePassword(context.Background(), first.Token, "correct-horse", "changed-horse"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{first.Token, second.Token} {
		if _, err := application.Session(context.Background(), token); !operationCode(err, "access_denied") {
			t.Fatalf("session survived password change: %v", err)
		}
	}
	if _, err := application.AuthenticateAPIKey(context.Background(), key.Key); !operationCode(err, "access_denied") {
		t.Fatalf("API key survived password change: %v", err)
	}
	if _, err := application.Login(context.Background(), "users", "change@example.test", "changed-horse"); err != nil {
		t.Fatal(err)
	}
}

func TestAuthIdentityIsValidatedCanonicalizedAndExactlyUnique(t *testing.T) {
	application := authFixture(t, teststore.New(), ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}})
	user := createAuthUser(t, application, "  Ada@Example.Test ")
	identity, _ := user.Values["email"].StringValue()
	if identity != "ada@example.test" {
		t.Fatalf("normalized identity = %q", identity)
	}
	if _, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("ADA@example.test")}, nil); !operationCode(err, "conflict") {
		t.Fatalf("canonical duplicate = %v", err)
	}
	if _, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("not-an-email")}, nil); !operationCode(err, "validation") {
		t.Fatalf("invalid email = %v", err)
	}
}

func authFixture(t *testing.T, backend store.Store, auth ridu.AuthConfig) *ridu.App {
	t.Helper()
	return authReadFixture(t, backend, auth, nil)
}

func authReadFixture(t *testing.T, backend store.Store, auth ridu.AuthConfig, read ridu.AccessRule) *ridu.App {
	t.Helper()
	application, err := ridu.New(ridu.Config{
		Name: "Auth hardening", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true, AuthConfig: auth,
			Access: ridu.CollectionAccess{Read: read},
			Fields: field.Fields{field.Text("email").Required().Unique()},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	return application
}

func createAuthUser(t *testing.T, application *ridu.App, email string) store.Document {
	t.Helper()
	user, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String(email)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func authTestTokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func loginRequest(t *testing.T, client *http.Client, userAgent string) *http.Cookie {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"sessions@example.test","password":"correct-horse"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", userAgent)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login = %d: %s", response.StatusCode, readBody(t, response))
	}
	return response.Cookies()[0]
}
