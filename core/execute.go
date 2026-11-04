package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/riducms/ridu/internal/httpapi"
	"github.com/riducms/ridu/internal/project"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

// executableMigrationHistoryDigest is set only by `ridu build`. It binds the
// production binary to the exact ordered migration filenames, artifact digests,
// recorded head and storage schema validated immediately before compilation.
// Admin presentation may differ at runtime. A direct `go build`
// deliberately leaves it empty so official adapters fail closed in production.
var executableMigrationHistoryDigest string

// StoreFactory lazily opens the application's singular document-store adapter.
// Project commands never call it, which keeps schema discovery and generation
// independent from database connectivity. Execute closes a returned adapter at
// shutdown when it implements Close() or Close() error.
type StoreFactory func(context.Context) (store.Store, error)

// StorageFactory lazily opens an object-storage adapter for upload bytes.
// It is runtime infrastructure, not a Plugin and never contributes secrets to
// the schema manifest. Execute closes a returned adapter before the document
// store when it implements Close() or Close() error.
type StorageFactory func(context.Context) (storage.Backend, error)

type ExecuteOption func(*executeOptions)

// ServerOptions configures production socket bounds and graceful drain. Zero
// values use Ridu's conservative defaults; a negative timeout explicitly
// disables that individual bound for a known streaming requirement.
type ServerOptions struct {
	ReadHeaderTimeout   time.Duration
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	IdleTimeout         time.Duration
	MaxHeaderBytes      int
	ShutdownTimeout     time.Duration
	WorkerDrainTimeout  time.Duration
	ReadinessDrainDelay time.Duration
	// AllowUnverifiableReadiness permits Execute with custom database or upload
	// adapters that cannot prove readiness. It is an explicit production safety
	// escape hatch; official adapters do not require it.
	AllowUnverifiableReadiness bool
	// SkipReadinessPreflight lets a development server bind before dependency
	// verification. Its /readyz probe still checks database connectivity,
	// storage, and custom checks, but permits an unapplied migration ledger
	// while ridu dev owns non-destructive schema synchronization.
	SkipReadinessPreflight bool
}

type executeOptions struct {
	storeFactory      StoreFactory
	address           string
	addressConfigured bool
	handler           HandlerOptions
	storageFactory    StorageFactory
	server            ServerOptions
	projectMigrations migration.ProjectDriver
}

// WithUploadStorage configures upload bytes lazily so manifest generation
// never touches the filesystem or network.
func WithUploadStorage(factory StorageFactory) ExecuteOption {
	return func(options *executeOptions) {
		options.storageFactory = factory
	}
}

// WithStore configures the singular document-store adapter used by the
// operation engine. The factory is invoked only by the server runtime.
func WithStore(factory StoreFactory) ExecuteOption {
	return func(options *executeOptions) {
		options.storeFactory = factory
	}
}

// WithProjectMigrations registers the selected adapter's migration driver and
// its compiled data transforms. The CLI uses it through the private
// project-command path, and a binary built with ridu build uses it to apply its
// own migrations with `<binary> migrate up`. Manifest generation and the HTTP
// runtime never use it.
func WithProjectMigrations(driver migration.ProjectDriver) ExecuteOption {
	return func(options *executeOptions) {
		options.projectMigrations = driver
	}
}

// WithAddress configures the HTTP listen address independently from the
// document-store adapter, in place of RIDU_ADDRESS and PORT. Without either,
// the server listens on 127.0.0.1:8080, which accepts connections only from
// this machine; use :8080 to listen on every interface.
func WithAddress(address string) ExecuteOption {
	return func(options *executeOptions) {
		options.address = address
		options.addressConfigured = true
	}
}

// WithHandlerOptions customizes the framework HTTP handler, including the
// generated application's embedded admin assets. Execute fills each field
// left at its zero value that has a standard variable, such as AllowedHosts
// from RIDU_ALLOWED_HOSTS; see Execute.
func WithHandlerOptions(handler HandlerOptions) ExecuteOption {
	return func(options *executeOptions) {
		options.handler = handler
	}
}

// WithServerOptions customizes production socket and drain bounds without
// replacing Ridu's framework-owned http.Server lifecycle. Execute fills the
// drain and readiness fields left at their zero values from their standard
// variables; see Execute.
func WithServerOptions(server ServerOptions) ExecuteOption {
	return func(options *executeOptions) { options.server = server }
}

// Execute runs the framework-owned project command driver for a generated
// application. Project commands resolve config without invoking the lazy
// runtime store factory; `migrate up` applies the build's migrations; the
// no-argument branch starts the HTTP server.
//
// The server reads Ridu's standard variables for every option the
// application left unset, so a variable added in a later release works
// without editing main.go. A malformed value stops the server with an error
// naming the variable. Options set in code take precedence:
//
//   - RIDU_ADDRESS, or :$PORT when only PORT is set: WithAddress
//   - RIDU_ALLOWED_ORIGINS, RIDU_ALLOWED_HOSTS and RIDU_TRUSTED_PROXY_CIDRS
//     (comma-separated), RIDU_TRUSTED_PROXY_HOPS, RIDU_SETUP_TOKEN,
//     RIDU_READINESS_TIMEOUT and RIDU_STRICT_TRANSPORT_SECURITY: the
//     HandlerOptions fields of the same names
//   - RIDU_SHUTDOWN_TIMEOUT, RIDU_WORKER_DRAIN_TIMEOUT and
//     RIDU_READINESS_DRAIN_DELAY: the ServerOptions fields of the same names
//   - RIDU_SECURE_COOKIES=false: cookies without the Secure attribute, for
//     plain-HTTP development
//
// ServerOptions.AllowUnverifiableReadiness and SkipReadinessPreflight have no
// variable here: each turns off a production readiness check, so only the
// application's code sets them, as a generated server does for ridu dev.
//
// The official adapters read their own connection variables, such as
// DATABASE_URL and RIDU_SQLITE_PATH, in their ConfigFromEnvironment functions.
func Execute(applicationConfig Config, supplied ...ExecuteOption) (result error) {
	options := executeOptions{address: "127.0.0.1:8080"}
	for _, option := range supplied {
		option(&options)
	}
	if len(os.Args) > 1 && os.Args[1] == migrateCommand {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runMigrateCommand(ctx, os.Args[2:], os.Stdout, os.Stderr, applicationConfig, options.projectMigrations, executableMigrationHistoryDigest)
	}
	if len(os.Args) > 1 {
		return project.Run(
			os.Args[1:], os.Stdout, os.Stderr, FrameworkVersion,
			func() (schema.Manifest, error) { return Resolve(applicationConfig) },
			func(requests []project.ArtifactRequest) (schema.Manifest, []project.Artifact, error) {
				return resolveProjectGeneration(applicationConfig, requests)
			},
			options.projectMigrations,
		)
	}
	if err := applyEnvironment(&options); err != nil {
		return err
	}
	serverOptions := normalizedServerOptions(options.server)
	var runtimeResources []runtimeResource
	defer func() {
		result = errors.Join(result, closeRuntimeResources(runtimeResources, serverOptions.ShutdownTimeout))
	}()
	if options.storeFactory == nil {
		return fmt.Errorf("the Ridu server requires a lazy store adapter configured with ridu.WithStore")
	}
	if err := httpapi.ValidateAllowedOrigins(options.handler.AllowedOrigins); err != nil {
		return err
	}
	if err := httpapi.ValidateAllowedHosts(options.handler.AllowedHosts); err != nil {
		return err
	}
	if token := options.handler.SetupToken; token != "" && len(token) < 32 {
		return fmt.Errorf("the setup token must be at least 32 characters long; generate one with: openssl rand -hex 32")
	}
	serverContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	backend, err := options.storeFactory(serverContext)
	if err != nil {
		return fmt.Errorf("open document store: %w", err)
	}
	if closer, ok := backend.(interface{ Close() }); ok {
		runtimeResources = append(runtimeResources, runtimeResource{name: "document store", close: func() error { closer.Close(); return nil }})
	} else if closer, ok := backend.(interface{ Close() error }); ok {
		runtimeResources = append(runtimeResources, runtimeResource{name: "document store", close: closer.Close})
	}
	if options.storageFactory != nil {
		uploadStorage, storageError := options.storageFactory(serverContext)
		if storageError != nil {
			return fmt.Errorf("open upload storage: %w", storageError)
		}
		applicationConfig.Storage = uploadStorage
		if closer, ok := uploadStorage.(interface{ Close() }); ok {
			runtimeResources = append(runtimeResources, runtimeResource{name: "upload storage", close: func() error { closer.Close(); return nil }})
		} else if closer, ok := uploadStorage.(interface{ Close() error }); ok {
			runtimeResources = append(runtimeResources, runtimeResource{name: "upload storage", close: closer.Close})
		}
	}
	application, err := New(applicationConfig, backend)
	if err != nil {
		return err
	}
	handlerOptions := options.handler
	handlerOptions.AllowedHosts = listenerAllowedHosts(options.address, handlerOptions.AllowedHosts)
	handlerOptions.allowUnverifiableReadiness = serverOptions.AllowUnverifiableReadiness
	handlerOptions.developmentReadiness = serverOptions.SkipReadinessPreflight
	if !serverOptions.AllowUnverifiableReadiness {
		if application.readiness == nil && application.migrationReadiness == nil {
			return fmt.Errorf("production server requires a store.ReadinessStore; set ServerOptions.AllowUnverifiableReadiness only after providing equivalent external checks")
		}
		if application.migrationReadiness != nil && application.migrationHistoryDigest == "" && !serverOptions.SkipReadinessPreflight {
			return fmt.Errorf("production server requires an executable migration history; build it with ridu build, or set ServerOptions.AllowUnverifiableReadiness only after providing equivalent external checks")
		}
		if application.uploads.Backend != nil && application.storageHealth == nil {
			return fmt.Errorf("production server requires upload storage.HealthBackend; set ServerOptions.AllowUnverifiableReadiness only after providing equivalent external checks")
		}
		if application.auth != nil && application.authMaintenance == nil {
			return fmt.Errorf("production server with auth-enabled collections requires a store.AuthMaintenanceStore; set ServerOptions.AllowUnverifiableReadiness only after providing equivalent external expired-credential maintenance")
		}
	}
	if !serverOptions.SkipReadinessPreflight {
		if err := application.checkReadiness(handlerOptions)(serverContext); err != nil {
			return fmt.Errorf("production readiness preflight: %w", err)
		}
	}
	requestRoot, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	server := &http.Server{
		Addr: options.address, Handler: application.Handler(handlerOptions),
		ReadHeaderTimeout: serverOptions.ReadHeaderTimeout, ReadTimeout: serverOptions.ReadTimeout,
		WriteTimeout: serverOptions.WriteTimeout, IdleTimeout: serverOptions.IdleTimeout,
		MaxHeaderBytes: serverOptions.MaxHeaderBytes,
		BaseContext:    func(net.Listener) context.Context { return requestRoot },
	}
	listener, err := net.Listen("tcp", options.address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", options.address, err)
	}
	defer listener.Close()
	var workers sync.WaitGroup
	if application.tasks != nil || application.authMaintenance != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			application.runTaskWorker(serverContext, handlerOptions)
		}()
	}
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	select {
	case err := <-serveError:
		if err != nil && err != http.ErrServerClosed {
			result = fmt.Errorf("serve HTTP: %w", err)
		}
		_ = server.Close()
	case <-serverContext.Done():
		application.draining.Store(true)
		if serverOptions.ReadinessDrainDelay > 0 {
			timer := time.NewTimer(serverOptions.ReadinessDrainDelay)
			<-timer.C
		}
		shutdownContext, cancel := context.WithTimeout(context.Background(), serverOptions.ShutdownTimeout)
		if err := server.Shutdown(shutdownContext); err != nil {
			cancelRequests()
			closeError := server.Close()
			result = fmt.Errorf("shut down HTTP server: %w", errors.Join(err, closeError))
		}
		cancel()
	}
	application.draining.Store(true)
	stop()
	cancelRequests()
	workersDone := make(chan struct{})
	go func() {
		workers.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-time.After(serverOptions.WorkerDrainTimeout):
		if result == nil {
			result = fmt.Errorf("background workers did not drain within %s; leased tasks will recover after expiry", serverOptions.WorkerDrainTimeout)
		}
	}
	return result
}

type runtimeResource struct {
	name  string
	close func() error
}

func closeRuntimeResources(resources []runtimeResource, timeout time.Duration) error {
	if len(resources) == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var closeErrors []error
	for index := len(resources) - 1; index >= 0; index-- {
		resource := resources[index]
		completed := make(chan error, 1)
		go func() {
			defer func() {
				if recover() != nil {
					completed <- errors.New("resource close panic recovered")
				}
			}()
			completed <- resource.close()
		}()
		select {
		case err := <-completed:
			if err != nil {
				closeErrors = append(closeErrors, fmt.Errorf("close runtime resource %s: %w", resource.name, err))
			}
		case <-timer.C:
			pending := make([]string, 0, index+1)
			for pendingIndex := index; pendingIndex >= 0; pendingIndex-- {
				pending = append(pending, resources[pendingIndex].name)
			}
			return fmt.Errorf("runtime resource close exceeded %s for %s", timeout, strings.Join(pending, ", "))
		}
	}
	return errors.Join(closeErrors...)
}

// listenerAllowedHosts returns the configured hosts or, when none are and the
// server listens only on loopback, the local names. A page on another site can
// resolve its own name to 127.0.0.1 and so reach a local server.
func listenerAllowedHosts(address string, configured []string) []string {
	if len(configured) == 0 && loopbackAddress(address) {
		return []string{"localhost", "127.0.0.1", "[::1]"}
	}
	return configured
}

// loopbackAddress reports whether a listen address accepts connections only
// from this machine.
func loopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func normalizedServerOptions(options ServerOptions) ServerOptions {
	options.ReadHeaderTimeout = durationOrDefault(options.ReadHeaderTimeout, 10*time.Second)
	options.ReadTimeout = durationOrDefault(options.ReadTimeout, 2*time.Minute)
	options.WriteTimeout = durationOrDefault(options.WriteTimeout, 2*time.Minute)
	options.IdleTimeout = durationOrDefault(options.IdleTimeout, 90*time.Second)
	options.ShutdownTimeout = positiveDurationOrDefault(options.ShutdownTimeout, 15*time.Second)
	options.WorkerDrainTimeout = positiveDurationOrDefault(options.WorkerDrainTimeout, 15*time.Second)
	if options.ReadinessDrainDelay == 0 {
		options.ReadinessDrainDelay = 2 * time.Second
	} else if options.ReadinessDrainDelay < 0 {
		options.ReadinessDrainDelay = 0
	}
	if options.MaxHeaderBytes == 0 {
		options.MaxHeaderBytes = 64 << 10
	}
	return options
}

func durationOrDefault(value, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	if value < 0 {
		return 0
	}
	return value
}

func positiveDurationOrDefault(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func (application *App) runTaskWorker(ctx context.Context, options HandlerOptions) {
	interval := options.TaskInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	batch := options.TaskBatch
	if batch <= 0 {
		batch = 50
	}
	leaseDuration := options.TaskLeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}
	heartbeat := options.TaskHeartbeatInterval
	if heartbeat <= 0 || heartbeat >= leaseDuration {
		heartbeat = leaseDuration / 3
	}
	prune := options.TaskPruneBatch
	if prune <= 0 {
		prune = 100
	}
	authPrune := options.AuthPruneBatch
	if authPrune <= 0 {
		authPrune = 100
	}
	if authPrune > store.MaxAuthPruneBatch {
		authPrune = store.MaxAuthPruneBatch
	}
	run := func() {
		runWorkerCycle(ctx, "durable task and auth maintenance worker", options.JobError, func() error {
			var cycleErrors []error
			if application.tasks != nil {
				_, err := application.runTasks(ctx, taskRunOptions{
					limit: batch, queues: append([]string(nil), options.TaskQueues...),
					leaseDuration: leaseDuration, heartbeatInterval: heartbeat,
					pruneLimit: prune,
				})
				cycleErrors = append(cycleErrors, err)
			}
			if application.authMaintenance != nil {
				_, err := application.PruneExpiredAuth(ctx, authPrune)
				cycleErrors = append(cycleErrors, err)
			}
			return errors.Join(cycleErrors...)
		})
	}

	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func runWorkerCycle(ctx context.Context, name string, report func(error), run func() error) {
	defer func() {
		if recover() != nil {
			reportWorkerError(name, report, errors.New("background worker panic recovered"), string(debug.Stack()))
		}
	}()
	if err := run(); err != nil && ctx.Err() == nil {
		reportWorkerError(name, report, err, "")
	}
}

func reportWorkerError(name string, report func(error), err error, stack string) {
	if report == nil {
		slog.Error(name, "error", err, "stack", stack)
		return
	}
	defer func() {
		if recover() != nil {
			slog.Error(name+" error callback panicked", "error", err, "stack", stack)
		}
	}()
	report(err)
}
