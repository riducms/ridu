package core

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

type authStoreWithoutMaintenance struct {
	store.Store
	store.AuthStore
	store.ReadinessStore
}

type failingReadinessStore struct {
	store.Store
	calls *atomic.Int32
}

func (backend failingReadinessStore) Ready(context.Context, schema.Manifest) error {
	backend.calls.Add(1)
	return errors.New("readiness should not run")
}

type developmentReadinessStore struct {
	store.Store
	readyCalls *atomic.Int32
	pingCalls  *atomic.Int32
}

func (backend developmentReadinessStore) Ready(context.Context, schema.Manifest) error {
	backend.readyCalls.Add(1)
	return errors.New("development migration ledger is incomplete")
}

func (backend developmentReadinessStore) Ping(context.Context) error {
	backend.pingCalls.Add(1)
	return nil
}

type migrationReadinessStore struct {
	store.Store
	legacyCalls *atomic.Int32
	exactCalls  *atomic.Int32
	digest      *atomic.Pointer[string]
	legacyError error
}

func (backend migrationReadinessStore) Ready(context.Context, schema.Manifest) error {
	backend.legacyCalls.Add(1)
	return backend.legacyError
}

func (backend migrationReadinessStore) ReadyWithMigrationHistory(_ context.Context, _ schema.Manifest, digest string) error {
	backend.exactCalls.Add(1)
	value := digest
	backend.digest.Store(&value)
	return nil
}

func TestProductionReadinessUsesTheExecutableMigrationHistory(t *testing.T) {
	previousDigest := executableMigrationHistoryDigest
	executableMigrationHistoryDigest = strings.Repeat("a", 64)
	defer func() { executableMigrationHistoryDigest = previousDigest }()

	var legacyCalls atomic.Int32
	var exactCalls atomic.Int32
	var digest atomic.Pointer[string]
	backend := migrationReadinessStore{
		Store: teststore.New(), legacyCalls: &legacyCalls, exactCalls: &exactCalls, digest: &digest,
		legacyError: errors.New("legacy readiness must not admit a production binary with exact history"),
	}
	application, err := New(
		Config{Name: "exact migration readiness", Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}},
		backend,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.checkReadiness(HandlerOptions{})(context.Background()); err != nil {
		t.Fatalf("exact migration readiness: %v", err)
	}
	if legacyCalls.Load() != 0 || exactCalls.Load() != 1 || digest.Load() == nil || *digest.Load() != strings.Repeat("a", 64) {
		t.Fatalf("legacy calls = %d, exact calls = %d, digest = %v", legacyCalls.Load(), exactCalls.Load(), digest.Load())
	}
}

func TestExplicitUnverifiableReadinessEscapeUsesTheLegacyContract(t *testing.T) {
	previousDigest := executableMigrationHistoryDigest
	executableMigrationHistoryDigest = ""
	defer func() { executableMigrationHistoryDigest = previousDigest }()

	var legacyCalls atomic.Int32
	var exactCalls atomic.Int32
	var digest atomic.Pointer[string]
	application, err := New(
		Config{Name: "explicit readiness escape", Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}},
		migrationReadinessStore{
			Store: teststore.New(), legacyCalls: &legacyCalls, exactCalls: &exactCalls, digest: &digest,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.checkReadiness(HandlerOptions{allowUnverifiableReadiness: true})(context.Background()); err != nil {
		t.Fatalf("explicit unverifiable readiness escape: %v", err)
	}
	if legacyCalls.Load() != 1 || exactCalls.Load() != 0 {
		t.Fatalf("readiness escape calls: legacy=%d exact=%d", legacyCalls.Load(), exactCalls.Load())
	}
}

func TestApplicationOwnedReadinessRequiresTheBuildBoundMigrationHistory(t *testing.T) {
	previousDigest := executableMigrationHistoryDigest
	executableMigrationHistoryDigest = ""
	defer func() { executableMigrationHistoryDigest = previousDigest }()

	var legacyCalls atomic.Int32
	var exactCalls atomic.Int32
	var digest atomic.Pointer[string]
	application, err := New(
		Config{Name: "missing application-owned history", Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}},
		migrationReadinessStore{
			Store: teststore.New(), legacyCalls: &legacyCalls, exactCalls: &exactCalls, digest: &digest,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.checkReadiness(HandlerOptions{})(context.Background()); err == nil || !strings.Contains(err.Error(), "build with ridu build") {
		t.Fatalf("missing application-owned history error = %v", err)
	}
	if legacyCalls.Load() != 0 || exactCalls.Load() != 0 {
		t.Fatalf("readiness fell back without executable history: legacy=%d exact=%d", legacyCalls.Load(), exactCalls.Load())
	}
}

func TestDevelopmentReadinessChecksConnectivityWithoutRequiringTheMigrationLedger(t *testing.T) {
	var readyCalls atomic.Int32
	var pingCalls atomic.Int32
	backend := developmentReadinessStore{Store: teststore.New(), readyCalls: &readyCalls, pingCalls: &pingCalls}
	application, err := New(
		Config{Name: "development readiness", Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}},
		backend,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.checkReadiness(HandlerOptions{developmentReadiness: true})(context.Background()); err != nil {
		t.Fatalf("development readiness: %v", err)
	}
	if readyCalls.Load() != 0 || pingCalls.Load() != 1 {
		t.Fatalf("readiness calls = %d, ping calls = %d; want 0 and 1", readyCalls.Load(), pingCalls.Load())
	}

	unknown, err := New(
		Config{Name: "unknown development readiness", Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}},
		struct{ store.Store }{Store: teststore.New()},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := unknown.checkReadiness(HandlerOptions{developmentReadiness: true})(context.Background()); err == nil || !strings.Contains(err.Error(), "development readiness is unavailable") {
		t.Fatalf("unknown development readiness error = %v", err)
	}
}

type authMaintenanceRecorder struct {
	calls  atomic.Int32
	limit  atomic.Int32
	cancel context.CancelFunc
}

func (maintenance *authMaintenanceRecorder) PruneExpiredAuth(_ context.Context, limit int) (store.AuthPruneResult, error) {
	maintenance.calls.Add(1)
	maintenance.limit.Store(int32(limit))
	maintenance.cancel()
	return store.AuthPruneResult{}, nil
}

func TestWorkerCycleRecoversAndContinuesAfterWorkerAndCallbackPanics(t *testing.T) {
	cycles := 0
	reports := 0
	report := func(error) {
		reports++
		panic("diagnostic callback panic")
	}
	runWorkerCycle(context.Background(), "test worker", report, func() error {
		cycles++
		panic("handler panic")
	})
	runWorkerCycle(context.Background(), "test worker", report, func() error {
		cycles++
		return errors.New("next cycle failure")
	})
	if cycles != 2 || reports != 2 {
		t.Fatalf("worker cycles=%d reports=%d, want 2/2", cycles, reports)
	}
}

func TestRuntimeResourceCloseIsBounded(t *testing.T) {
	release := make(chan struct{})
	started := time.Now()
	err := closeRuntimeResources([]runtimeResource{{name: "blocked store", close: func() error { <-release; return nil }}}, 10*time.Millisecond)
	close(release)
	if err == nil || !strings.Contains(err.Error(), "blocked store") {
		t.Fatalf("bounded close error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded close took %s", elapsed)
	}
}

func TestRuntimeResourcesCloseSequentiallyInReverseDependencyOrder(t *testing.T) {
	var closed []string
	resources := []runtimeResource{
		{name: "document store", close: func() error { closed = append(closed, "document store"); return nil }},
		{name: "upload storage", close: func() error { closed = append(closed, "upload storage"); return nil }},
	}
	if err := closeRuntimeResources(resources, time.Second); err != nil {
		t.Fatal(err)
	}
	if strings.Join(closed, ",") != "upload storage,document store" {
		t.Fatalf("close order = %v", closed)
	}
}

func TestBlockedDependentResourceDoesNotRaceStoreClose(t *testing.T) {
	release := make(chan struct{})
	storeClosed := false
	resources := []runtimeResource{
		{name: "document store", close: func() error { storeClosed = true; return nil }},
		{name: "upload storage", close: func() error { <-release; return nil }},
	}
	err := closeRuntimeResources(resources, 10*time.Millisecond)
	close(release)
	if err == nil || storeClosed {
		t.Fatalf("blocked dependent close error=%v storeClosed=%t", err, storeClosed)
	}
}

func TestNormalizedServerOptionsProvideBoundedProductionDefaults(t *testing.T) {
	options := normalizedServerOptions(ServerOptions{})
	if options.ReadHeaderTimeout != 10*time.Second || options.ReadTimeout != 2*time.Minute || options.WriteTimeout != 2*time.Minute || options.IdleTimeout != 90*time.Second {
		t.Fatalf("socket defaults = %#v", options)
	}
	if options.MaxHeaderBytes != 64<<10 || options.ShutdownTimeout != 15*time.Second || options.WorkerDrainTimeout != 15*time.Second || options.ReadinessDrainDelay != 2*time.Second {
		t.Fatalf("drain defaults = %#v", options)
	}
	disabled := normalizedServerOptions(ServerOptions{ReadHeaderTimeout: -1, ReadTimeout: -1, WriteTimeout: -1, IdleTimeout: -1, ReadinessDrainDelay: -1})
	if disabled.ReadHeaderTimeout != 0 || disabled.ReadTimeout != 0 || disabled.WriteTimeout != 0 || disabled.IdleTimeout != 0 || disabled.ReadinessDrainDelay != 0 {
		t.Fatalf("explicit disabled bounds = %#v", disabled)
	}
}

func TestExecuteBindsListenerBeforeStartingDurableWorkers(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	var calls atomic.Int32
	task := NewTask("bind-order", func(TaskContext, struct{}) (struct{}, error) {
		calls.Add(1)
		return struct{}{}, nil
	})
	config := Config{Name: "bind order", Tasks: []TaskDefinition{task}, Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}}
	backend := teststore.New()
	application, err := New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := task.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{}); err != nil {
		t.Fatal(err)
	}

	arguments := os.Args
	os.Args = []string{"ridu-test-app"}
	defer func() { os.Args = arguments }()
	err = Execute(
		config,
		WithStore(func(context.Context) (store.Store, error) { return backend, nil }),
		WithAddress(listener.Addr().String()),
	)
	if err == nil || !strings.Contains(err.Error(), "listen on") {
		t.Fatalf("Execute error = %v", err)
	}
	time.Sleep(25 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("worker ran %d tasks before listener bind", calls.Load())
	}
}

func TestExecuteRejectsAStoreThatCannotProveReadiness(t *testing.T) {
	arguments := os.Args
	os.Args = []string{"ridu-test-app"}
	defer func() { os.Args = arguments }()
	backend := struct{ store.Store }{Store: teststore.New()}
	err := Execute(
		Config{Name: "strict readiness", Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}},
		WithStore(func(context.Context) (store.Store, error) { return backend, nil }),
		WithAddress("127.0.0.1:0"),
	)
	if err == nil || !strings.Contains(err.Error(), "store.ReadinessStore") {
		t.Fatalf("strict readiness error = %v", err)
	}
}

func TestExecuteRejectsAnOfficialStoreWithoutEmbeddedMigrationHistory(t *testing.T) {
	arguments := os.Args
	os.Args = []string{"ridu-test-app"}
	defer func() { os.Args = arguments }()
	previousDigest := executableMigrationHistoryDigest
	executableMigrationHistoryDigest = ""
	defer func() { executableMigrationHistoryDigest = previousDigest }()

	var legacyCalls atomic.Int32
	var exactCalls atomic.Int32
	var digest atomic.Pointer[string]
	backend := migrationReadinessStore{
		Store: teststore.New(), legacyCalls: &legacyCalls, exactCalls: &exactCalls, digest: &digest,
	}
	err := Execute(
		Config{Name: "missing executable migration history", Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}},
		WithStore(func(context.Context) (store.Store, error) { return backend, nil }),
		WithAddress("127.0.0.1:0"),
	)
	if err == nil || !strings.Contains(err.Error(), "build it with ridu build") {
		t.Fatalf("missing executable migration history error = %v", err)
	}
	if legacyCalls.Load() != 0 || exactCalls.Load() != 0 {
		t.Fatalf("readiness ran before missing history rejection: legacy=%d exact=%d", legacyCalls.Load(), exactCalls.Load())
	}
}

func TestExecuteSkipsReadinessPreflightOnlyWhenExplicitlyRequested(t *testing.T) {
	arguments := os.Args
	os.Args = []string{"ridu-test-app"}
	defer func() { os.Args = arguments }()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	var readinessCalls atomic.Int32
	backend := failingReadinessStore{Store: teststore.New(), calls: &readinessCalls}
	err = Execute(
		Config{Name: "development readiness escape", Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}},
		WithStore(func(context.Context) (store.Store, error) { return backend, nil }),
		WithAddress(listener.Addr().String()),
		WithServerOptions(ServerOptions{SkipReadinessPreflight: true}),
	)
	if err == nil || !strings.Contains(err.Error(), "listen on") {
		t.Fatalf("explicit readiness escape error = %v", err)
	}
	if readinessCalls.Load() != 0 {
		t.Fatalf("readiness preflight ran %d times with explicit escape", readinessCalls.Load())
	}
}

func TestExecuteRequiresExpiredAuthMaintenanceOrExplicitExternalEscape(t *testing.T) {
	arguments := os.Args
	os.Args = []string{"ridu-test-app"}
	defer func() { os.Args = arguments }()

	base := teststore.New()
	backend := authStoreWithoutMaintenance{Store: base, AuthStore: base, ReadinessStore: base}
	config := Config{
		Name: "strict auth retention", Admin: AdminConfig{User: "users"},
		Collections: []Collection{{
			Slug: "users", Auth: true,
			AuthConfig: AuthConfig{Password: PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Fields:     field.Fields{field.Email("email").Required().Unique()},
		}},
	}
	if _, err := New(config, backend); err != nil {
		t.Fatalf("local application rejected optional maintenance capability: %v", err)
	}
	err := Execute(
		config,
		WithStore(func(context.Context) (store.Store, error) { return backend, nil }),
		WithAddress("127.0.0.1:0"),
	)
	if err == nil || !strings.Contains(err.Error(), "store.AuthMaintenanceStore") || !strings.Contains(err.Error(), "expired-credential maintenance") {
		t.Fatalf("strict auth maintenance error = %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	err = Execute(
		config,
		WithStore(func(context.Context) (store.Store, error) { return backend, nil }),
		WithAddress(listener.Addr().String()),
		WithServerOptions(ServerOptions{AllowUnverifiableReadiness: true}),
	)
	if err == nil || !strings.Contains(err.Error(), "listen on") || strings.Contains(err.Error(), "AuthMaintenanceStore") {
		t.Fatalf("explicit external-maintenance escape = %v", err)
	}
}

func TestAuthMaintenanceWorkerRunsWithoutATaskStore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	maintenance := &authMaintenanceRecorder{cancel: cancel}
	application := &App{authMaintenance: maintenance}
	application.runTaskWorker(ctx, HandlerOptions{TaskInterval: time.Hour, AuthPruneBatch: 7})
	if maintenance.calls.Load() != 1 || maintenance.limit.Load() != 7 {
		t.Fatalf("auth-only maintenance calls/limit = %d/%d", maintenance.calls.Load(), maintenance.limit.Load())
	}
}
