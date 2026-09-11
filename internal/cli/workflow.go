package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/internal/commandrun"
	"github.com/riducms/ridu/internal/generate"
	"github.com/riducms/ridu/internal/goworkspace"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

const defaultDevelopmentDatabaseURL = "postgres://ridu:ridu@127.0.0.1:54329/ridu?sslmode=disable"
const defaultDevelopmentMongoDBURL = "mongodb://127.0.0.1:27029/ridu?directConnection=true&replicaSet=ridu-rs0"
const defaultDevelopmentSQLitePath = ".ridu/development.sqlite"

const adminSchemaReloadSignal = ".ridu/admin-schema.reload"
const internalDevelopmentServerArgument = "--ridu-internal-development-server"
const executableMigrationHistoryLinkerVariable = "github.com/riducms/ridu/core.executableMigrationHistoryDigest"
const disabledDevelopmentReadinessDrain = "RIDU_READINESS_DRAIN_DELAY=-1s"

const (
	defaultManagedProcessStopTimeout = 5 * time.Second
	developmentServerStopBuffer      = 5 * time.Second
)

type developmentPreparation struct {
	manifest                   schema.Manifest
	schemaChanged              bool
	adminPluginRegistryChanged bool
	generatedGoChanged         bool
	manifestDuration           time.Duration
	contractDuration           time.Duration
	schemaSyncDuration         time.Duration
	mongoIndexesSynchronized   bool
}

type developmentDatabase struct {
	databaseURL   string
	databasePath  string
	startPostgres bool
	startMongoDB  bool
}

var errDevelopmentSourceChanged = errors.New("development source changed while preparing a replacement")

type adminSchemaUpdateSignal struct {
	Version    int    `json:"version"`
	Revision   string `json:"revision"`
	FullReload bool   `json:"fullReload"`
}

// developmentProxy owns the stable public development address while server
// candidates bind private loopback addresses. A replacement is promoted only
// after its own runtime Store and /readyz check succeed, so the previous
// process remains available throughout every rejected reload.
type developmentProxy struct {
	listener net.Listener
	server   *http.Server
	target   atomic.Pointer[url.URL]
	done     chan error
	stopOnce sync.Once
	stopErr  error
}

func startDevelopmentProxy(address string) (*developmentProxy, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", address, err)
	}
	proxy := &developmentProxy{listener: listener, done: make(chan error, 1)}
	reverse := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			target := proxy.target.Load()
			if target == nil {
				return
			}
			request.SetURL(target)
			request.Out.Host = request.In.Host
			request.SetXForwarded()
		},
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "development server is unavailable", http.StatusBadGateway)
		},
	}
	proxy.server = &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if proxy.target.Load() == nil {
				http.Error(writer, "development server is preparing", http.StatusServiceUnavailable)
				return
			}
			reverse.ServeHTTP(writer, request)
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		err := proxy.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		proxy.done <- err
		close(proxy.done)
	}()
	return proxy, nil
}

func (proxy *developmentProxy) setTarget(rawURL string) error {
	if proxy == nil {
		return fmt.Errorf("development proxy is unavailable")
	}
	target, err := url.Parse(rawURL)
	if err != nil || target.Scheme != "http" || target.Host == "" || target.Hostname() != "127.0.0.1" || target.User != nil || target.Fragment != "" {
		return fmt.Errorf("invalid development server target")
	}
	proxy.target.Store(target)
	return nil
}

func (proxy *developmentProxy) stop() error {
	if proxy == nil {
		return nil
	}
	proxy.stopOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownError := proxy.server.Shutdown(ctx)
		if shutdownError != nil {
			shutdownError = errors.Join(shutdownError, proxy.server.Close())
		}
		serveError := <-proxy.done
		proxy.stopErr = errors.Join(shutdownError, serveError)
	})
	return proxy.stopErr
}

func availableDevelopmentServerAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("reserve development server address: %w", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("release development server address: %w", err)
	}
	return address, nil
}

func developmentServerEnvironment(base []string, address string) []string {
	environment := append([]string(nil), base...)
	return append(environment, "RIDU_ADDRESS="+address)
}

func developmentServerBaseEnvironment(adapter projectfile.DatabaseAdapter) []string {
	// The CLI-owned proxy removes a previous candidate from routing
	// synchronously. Production's readiness propagation delay would only hold
	// the local reload and admin update after that handoff has completed.
	environment := []string{"RIDU_SECURE_COOKIES=false", disabledDevelopmentReadinessDrain}
	if adapter == projectfile.DatabaseMongoDB {
		return environment
	}
	return append(environment,
		"RIDU_ALLOW_UNVERIFIABLE_READINESS=true",
		"RIDU_SKIP_READINESS_PREFLIGHT=true",
	)
}

func developmentServerArguments(adapter projectfile.DatabaseAdapter) []string {
	if adapter == projectfile.DatabaseMongoDB {
		return []string{internalDevelopmentServerArgument}
	}
	return nil
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	flags := flag.NewFlagSet("ridu doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "ridu doctor does not accept positional arguments")
		return 2
	}

	healthy := true
	definition, projectError := projectfile.Discover(options.WorkingDirectory)
	manager := projectfile.PackageManagerNPM
	if projectError == nil {
		manager = definition.FrontendPackageManager()
	}
	for _, tool := range []struct {
		name string
		args []string
	}{{"go", []string{"version"}}, {string(manager), []string{"--version"}}} {
		path, err := exec.LookPath(tool.name)
		if err != nil {
			fmt.Fprintf(stderr, "missing  %-8s install %s and ensure it is on PATH\n", tool.name, tool.name)
			healthy = false
			continue
		}
		version, _ := commandOutput(ctx, options.WorkingDirectory, nil, tool.name, tool.args...)
		fmt.Fprintf(stdout, "ok       %-8s %s (%s)\n", tool.name, strings.TrimSpace(version), path)
	}

	if projectError != nil {
		fmt.Fprintf(stderr, "missing  project  %v\n", projectError)
		healthy = false
	} else {
		fmt.Fprintf(stdout, "ok       project  %s\n", definition.Root)
		if definition.Admin != "" {
			if _, err := os.Stat(filepath.Join(definition.Root, "node_modules")); err == nil {
				fmt.Fprintln(stdout, "ok       frontend dependencies installed")
			} else {
				fmt.Fprintf(stdout, "ready    frontend dependencies will be installed by ridu dev/build using %s\n", manager)
			}
		}
	}

	if projectError == nil {
		switch definition.Database {
		case projectfile.DatabaseSQLite:
			if configured := strings.TrimSpace(os.Getenv("RIDU_SQLITE_PATH")); configured != "" {
				if configured == ":memory:" {
					fmt.Fprintln(stderr, "invalid  database RIDU_SQLITE_PATH must be a file because migrations and the server use separate processes")
					healthy = false
				} else if resolved, err := resolveSQLiteMigrationDatabasePath(definition.Root, configured); err != nil {
					fmt.Fprintf(stderr, "invalid  database RIDU_SQLITE_PATH: %v\n", err)
					healthy = false
				} else if resolved != configured {
					fmt.Fprintln(stderr, "invalid  database RIDU_SQLITE_PATH must be an absolute file path or URI; use --database-path for a project-relative development path")
					healthy = false
				} else {
					fmt.Fprintf(stdout, "ok       database SQLite at %s; Docker is not required\n", resolved)
				}
			} else {
				fmt.Fprintf(stdout, "ok       database SQLite; ridu dev uses %s and Docker is not required\n", defaultDevelopmentSQLitePath)
			}
		case projectfile.DatabaseMongoDB:
			if strings.TrimSpace(os.Getenv("DATABASE_URL")) != "" {
				fmt.Fprintln(stdout, "ok       database MongoDB DATABASE_URL is set; Docker orchestration is not required")
			} else if _, err := os.Stat(filepath.Join(definition.Root, "compose.yaml")); err != nil {
				fmt.Fprintln(stderr, "missing  database set DATABASE_URL to a MongoDB replica-set database or restore the scaffolded compose.yaml")
				healthy = false
			} else if _, err := exec.LookPath("docker"); err != nil {
				fmt.Fprintln(stderr, "missing  docker   install Docker/OrbStack or set DATABASE_URL to a MongoDB replica-set database")
				healthy = false
			} else {
				fmt.Fprintln(stdout, "ok       docker   available for the scaffolded development MongoDB replica set")
			}
		case projectfile.DatabasePostgres:
			if _, err := exec.LookPath("docker"); err == nil {
				fmt.Fprintln(stdout, "ok       docker   available for the development PostgreSQL service")
			} else if os.Getenv("DATABASE_URL") == "" {
				fmt.Fprintln(stderr, "missing  docker   install Docker/OrbStack or set DATABASE_URL")
				healthy = false
			} else {
				fmt.Fprintln(stdout, "ok       database DATABASE_URL is set; Docker is optional")
			}
		}
	}
	if !healthy {
		output.Error("Doctor found configuration issues", nil)
		return 1
	}
	fmt.Fprintln(stdout, "Ridu is ready.")
	return 0
}

func runCheck(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	flags := flag.NewFlagSet("ridu check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "ridu check does not accept positional arguments")
		return 2
	}
	definition, err := projectfile.Discover(options.WorkingDirectory)
	if err != nil {
		output.Error("discover project", err)
		return 1
	}
	frameworkVersion := frameworkVersion(options)
	result, err := generate.Run(ctx, definition, frameworkVersion, true)
	if err != nil {
		output.Error("generated contracts", err)
		return 1
	}
	printGenerationWarnings(output, result)
	fmt.Fprintln(stdout, "ok       generated contracts")
	if definition.Migrations == "" {
		output.Error("project ridu.toml does not configure a migrations directory", nil)
		return 1
	}
	if _, err := requireProjectMigrationHistory(definition, result.Manifest); err != nil {
		output.Error("migration artifacts", err)
		return 1
	}
	fmt.Fprintln(stdout, "ok       migration artifact history")

	unformatted, err := unformattedGoFiles(ctx, definition.Root)
	if err != nil {
		output.Error("check Go formatting", err)
		return 1
	}
	if len(unformatted) != 0 {
		output.Error("Go files need formatting", errors.New(strings.Join(unformatted, "\n")))
		return 1
	}
	fmt.Fprintln(stdout, "ok       Go formatting")
	for _, command := range [][]string{{"go", "vet", "./..."}, {"go", "test", "./..."}} {
		if err := runForeground(ctx, definition.Root, nil, stdout, stderr, command[0], command[1:]...); err != nil {
			output.Error(strings.Join(command, " "), err)
			return 1
		}
	}
	fmt.Fprintln(stdout, "ok       Go vet and tests")
	if definition.Admin != "" {
		if _, err := os.Stat(filepath.Join(definition.Root, "node_modules")); err != nil {
			output.Error("frontend dependencies are missing; run ridu dev or "+packageManagerInstallHint(definition), nil)
			return 1
		}
		if err := runPackageManagerScript(ctx, definition, definition.Absolute(definition.Admin), "check", nil, stdout, stderr); err != nil {
			output.Error("frontend checks", err)
			return 1
		}
		fmt.Fprintln(stdout, "ok       TypeScript and Svelte")
		if err := checkAdminRegistrations(ctx, definition, stdout, stderr); err != nil {
			output.Error("admin registrations", err)
			return 1
		}
		fmt.Fprintln(stdout, "ok       admin registrations")
	}
	fmt.Fprintln(stdout, "All checks passed.")
	return 0
}

func runBuild(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	reporter := outputFor(stdout, stderr, options)
	flags := flag.NewFlagSet("ridu build", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "output binary path (defaults to dist/<project>)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "ridu build does not accept positional arguments")
		return 2
	}
	definition, err := projectfile.Discover(options.WorkingDirectory)
	if err != nil {
		reporter.Error("discover project", err)
		return 1
	}
	result, err := generate.Run(ctx, definition, frameworkVersion(options), false)
	if err != nil {
		reporter.Error("generate project", err)
		return 1
	}
	printGenerationWarnings(reporter, result)
	for _, artifact := range result.Artifacts {
		if artifact.Changed {
			fmt.Fprintf(stdout, "generated %s\n", relativePath(definition.Root, artifact.Path))
		}
	}
	if definition.Migrations == "" {
		reporter.Error("project ridu.toml does not configure a migrations directory", nil)
		return 1
	}
	files, err := requireProjectMigrationHistory(definition, result.Manifest)
	if err != nil {
		reporter.Error("migration artifacts", err)
		return 1
	}
	historyDigest, err := migrationArtifactHistoryDigest(files)
	if err != nil {
		reporter.Error("migration artifacts", err)
		return 1
	}
	reporter.Info("Verified migration artifact history")
	if definition.Admin != "" {
		if err := ensureFrontendDependencies(ctx, definition, stdout, stderr, reporter); err != nil {
			reporter.Error("install frontend dependencies", err)
			return 1
		}
		if err := checkAdminRegistrations(ctx, definition, stdout, stderr); err != nil {
			reporter.Error("admin registrations", err)
			return 1
		}
		if err := runPackageManagerScript(ctx, definition, definition.Absolute(definition.Admin), "build", nil, stdout, stderr); err != nil {
			reporter.Error("build admin", err)
			return 1
		}
		if err := restoreAdminAssetsPlaceholder(definition); err != nil {
			reporter.Error("restore embedded admin placeholder", err)
			return 1
		}
	}
	target := *output
	if target == "" {
		name := filepath.Base(definition.Root)
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		target = filepath.Join(definition.Root, "dist", name)
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(definition.Root, target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		reporter.Error("create build directory", err)
		return 1
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".ridu-build-*")
	if err != nil {
		reporter.Error("create temporary build output", err)
		return 1
	}
	temporaryPath := temporary.Name()
	_ = temporary.Close()
	_ = os.Remove(temporaryPath)
	defer os.Remove(temporaryPath)
	entry := "./" + strings.TrimPrefix(filepath.ToSlash(definition.Entry), "./")
	linkerFlags := "-X=" + executableMigrationHistoryLinkerVariable + "=" + historyDigest
	if err := runForeground(ctx, definition.Root, nil, stdout, stderr, "go", "build", "-trimpath", "-ldflags="+linkerFlags, "-o", temporaryPath, entry); err != nil {
		reporter.Error("build server", err)
		return 1
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		reporter.Error("install production binary", err)
		return 1
	}
	fmt.Fprintf(stdout, "Built %s\n", relativePath(definition.Root, target))
	return 0
}

func requireProjectMigrationHistory(definition projectfile.File, manifest schema.Manifest) ([]migrationartifact.File, error) {
	plannerName, err := projectMigrationPlannerName(definition.Database)
	if err != nil {
		return nil, err
	}
	return migrationartifact.RequireCurrentHistoryForPlanner(
		definition.Absolute(definition.Migrations), manifest, plannerName,
	)
}

func projectMigrationPlannerName(adapter projectfile.DatabaseAdapter) (string, error) {
	switch adapter {
	case projectfile.DatabasePostgres:
		return "atlas", nil
	case projectfile.DatabaseSQLite:
		return "ridu-sqlite", nil
	case projectfile.DatabaseMongoDB:
		return "mongodb", nil
	default:
		return "", fmt.Errorf("unsupported database adapter %q", adapter)
	}
}

func migrationArtifactHistoryDigest(files []migrationartifact.File) (string, error) {
	identities := make([]migration.ArtifactIdentity, len(files))
	for index, file := range files {
		identities[index] = migration.ArtifactIdentity{Name: file.Name, Digest: file.Digest}
	}
	return migration.DigestArtifactHistory(identities)
}

func restoreAdminAssetsPlaceholder(definition projectfile.File) error {
	if definition.Assets == "" {
		return nil
	}
	directory := filepath.Join(definition.Root, definition.Assets)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(directory, "placeholder.txt"),
		[]byte("This keeps go:embed valid before the first admin build. Ridu replaces it during ridu build.\n"),
		0o644,
	)
}

func resolveDevelopmentDatabase(definition projectfile.File, databaseURL, databasePath string, databaseURLFlag, databasePathFlag, noDocker bool) (developmentDatabase, error) {
	switch definition.Database {
	case projectfile.DatabasePostgres:
		if databasePathFlag {
			return developmentDatabase{}, fmt.Errorf("PostgreSQL projects do not accept --database-path")
		}
		databaseURL = strings.TrimSpace(databaseURL)
		startPostgres := databaseURL == "" && !noDocker
		if databaseURL == "" {
			databaseURL = defaultDevelopmentDatabaseURL
		}
		return developmentDatabase{databaseURL: databaseURL, startPostgres: startPostgres}, nil
	case projectfile.DatabaseSQLite:
		if databaseURLFlag {
			return developmentDatabase{}, fmt.Errorf("SQLite projects do not accept --database-url")
		}
		databasePath = strings.TrimSpace(databasePath)
		configuredPath := databasePath != ""
		if databasePath == "" {
			databasePath = defaultDevelopmentSQLitePath
		}
		if databasePath == ":memory:" {
			return developmentDatabase{}, fmt.Errorf("ridu dev requires a SQLite file because schema synchronization and the server use separate processes")
		}
		resolved, err := resolveSQLiteMigrationDatabasePath(definition.Root, databasePath)
		if err != nil {
			return developmentDatabase{}, err
		}
		if configuredPath && !databasePathFlag && resolved != databasePath {
			return developmentDatabase{}, fmt.Errorf("RIDU_SQLITE_PATH must be an absolute file path or URI; use --database-path for a project-relative development path")
		}
		return developmentDatabase{databasePath: resolved}, nil
	case projectfile.DatabaseMongoDB:
		if databasePathFlag {
			return developmentDatabase{}, fmt.Errorf("MongoDB projects do not accept --database-path")
		}
		databaseURL = strings.TrimSpace(databaseURL)
		if databaseURL == "" {
			if noDocker {
				return developmentDatabase{}, fmt.Errorf("MongoDB projects require --database-url or DATABASE_URL when --no-docker is set")
			}
			return developmentDatabase{databaseURL: defaultDevelopmentMongoDBURL, startMongoDB: true}, nil
		}
		return developmentDatabase{databaseURL: databaseURL}, nil
	default:
		return developmentDatabase{}, fmt.Errorf("unsupported database adapter %q", definition.Database)
	}
}

func runDev(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	flags := flag.NewFlagSet("ridu dev", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databaseURL := flags.String("database-url", "", "development PostgreSQL or MongoDB URL (defaults to DATABASE_URL)")
	databasePath := flags.String("database-path", "", "development SQLite path (defaults to RIDU_SQLITE_PATH)")
	address := flags.String("address", "127.0.0.1:8080", "Go API address")
	adminPort := flags.Int("admin-port", 5173, "Vite admin port")
	noDocker := flags.Bool("no-docker", false, "do not start the scaffolded Docker service")
	noInstall := flags.Bool("no-install", false, "do not install missing frontend dependencies")
	noSync := flags.Bool("no-sync", false, "do not apply the non-destructive development schema plan")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "ridu dev does not accept positional arguments")
		return 2
	}
	definition, err := projectfile.Discover(options.WorkingDirectory)
	if err != nil {
		output.Error("discover project", err)
		return 1
	}
	databaseURLFlag := false
	databasePathFlag := false
	flags.Visit(func(selected *flag.Flag) {
		switch selected.Name {
		case "database-url":
			databaseURLFlag = true
		case "database-path":
			databasePathFlag = true
		}
	})
	if !databaseURLFlag {
		*databaseURL = os.Getenv("DATABASE_URL")
	}
	if !databasePathFlag {
		*databasePath = os.Getenv("RIDU_SQLITE_PATH")
	}
	database, err := resolveDevelopmentDatabase(definition, *databaseURL, *databasePath, databaseURLFlag, databasePathFlag, *noDocker)
	if err != nil {
		output.Error("configure development database", err)
		return 2
	}
	if !*noInstall {
		if err := ensureFrontendDependencies(ctx, definition, stdout, stderr, output); err != nil {
			return developmentFailure(ctx, output, "install frontend dependencies", err)
		}
	}
	if database.startPostgres {
		if _, err := os.Stat(filepath.Join(definition.Root, "compose.yaml")); err != nil {
			output.Error("DATABASE_URL is unset and the project has no compose.yaml", nil)
			return 1
		}
		output.Info("Starting development PostgreSQL")
		if err := runForeground(ctx, definition.Root, nil, stdout, stderr, "docker", "compose", "up", "-d", "postgres"); err != nil {
			return developmentFailure(ctx, output, "start PostgreSQL", err)
		}
	}
	if database.startMongoDB {
		if _, err := os.Stat(filepath.Join(definition.Root, "compose.yaml")); err != nil {
			output.Error("DATABASE_URL is unset and the MongoDB project has no compose.yaml", nil)
			return 1
		}
		output.Info("Starting development MongoDB replica set")
		if err := runForeground(ctx, definition.Root, nil, stdout, stderr, "docker", "compose", "up", "-d", "mongodb"); err != nil {
			return developmentFailure(ctx, output, "start MongoDB", err)
		}
	}
	version := frameworkVersion(options)
	watcher, err := newGoSourceWatcher(definition.Root)
	if err != nil {
		output.Error("watch Go configuration", err)
		return 1
	}
	defer watcher.Close()
	developmentStarted := time.Now()
	var activeBinary developmentBinary
	var buildDuration time.Duration
	var preparation developmentPreparation
	var acceptedInitialRevision uint64
	for {
		initialRevision := watcher.Revision()
		activeBinary, buildDuration, err = buildDevelopmentBinary(ctx, definition, stdout, stderr)
		if err != nil {
			return developmentFailure(ctx, output, "prepare development runtime", err)
		}
		fresh := func() bool { return watcher.Revision() == initialRevision }
		activeBinary, buildDuration, preparation, err = stabilizeDevelopmentCandidate(ctx, definition, version, activeBinary, buildDuration, !*noSync, fresh, stdout, stderr, output)
		if errors.Is(err, errDevelopmentSourceChanged) {
			_ = activeBinary.remove()
			output.Info("Go changed during initial preparation; rebuilding the latest source")
			continue
		}
		if err != nil {
			_ = activeBinary.remove()
			return developmentFailure(ctx, output, "prepare development runtime", err)
		}
		preparation, err = synchronizeDevelopmentSchema(ctx, definition.Database, database.databaseURL, database.databasePath, !*noSync, true, preparation, output)
		if err != nil {
			_ = activeBinary.remove()
			return developmentFailure(ctx, output, "prepare development runtime", err)
		}
		if !fresh() && canDiscardStaleDevelopmentCandidate(definition.Database, preparation) {
			_ = activeBinary.remove()
			output.Info("Go changed during initial schema synchronization; rebuilding the latest source")
			continue
		}
		if !fresh() {
			output.Info("Newer Go change is queued; starting the MongoDB candidate that matches the synchronized indexes first")
		}
		acceptedInitialRevision = initialRevision
		break
	}
	servedRevision := acceptedInitialRevision
	if definition.Admin != "" {
		if err := writeAdminSchemaReloadSignal(definition.Root, preparation.adminPluginRegistryChanged); err != nil {
			_ = activeBinary.remove()
			output.Error("prepare admin schema reload signal", err)
			return 1
		}
	}

	serverURL := developmentServerURL(*address)
	publicServerURL, err := url.Parse(serverURL)
	if err != nil || publicServerURL.Host == "" {
		_ = activeBinary.remove()
		output.Error("start server", fmt.Errorf("invalid public development address %q", *address))
		return 1
	}
	publicServerHost := publicServerURL.Host
	adminURL := serverURL + "/admin/"
	if definition.Admin != "" {
		adminURL = fmt.Sprintf("http://127.0.0.1:%d/admin/", *adminPort)
	}
	serverEnvironment := developmentServerBaseEnvironment(definition.Database)
	if definition.Database == projectfile.DatabaseSQLite {
		serverEnvironment = append(serverEnvironment, "RIDU_SQLITE_PATH="+database.databasePath)
	} else {
		serverEnvironment = append(serverEnvironment,
			"DATABASE_URL="+database.databaseURL,
			"RIDU_ALLOW_INSECURE_DATABASE=true",
		)
	}
	if definition.Admin != "" {
		adminOrigin := fmt.Sprintf("http://127.0.0.1:%d", *adminPort)
		if existing := strings.TrimSpace(os.Getenv("RIDU_ALLOWED_ORIGINS")); existing != "" {
			adminOrigin = existing + "," + adminOrigin
		}
		serverEnvironment = append(serverEnvironment, "RIDU_ALLOWED_ORIGINS="+adminOrigin)
	}
	trustedProxyCIDRs := "127.0.0.1/32"
	if existing := strings.TrimSpace(os.Getenv("RIDU_TRUSTED_PROXY_CIDRS")); existing != "" {
		trustedProxyCIDRs = existing + "," + trustedProxyCIDRs
	}
	serverEnvironment = append(serverEnvironment, "RIDU_TRUSTED_PROXY_CIDRS="+trustedProxyCIDRs)

	handoffProxy, err := startDevelopmentProxy(*address)
	if err != nil {
		_ = activeBinary.remove()
		output.Error("start development proxy", err)
		return 1
	}
	defer func() {
		if err := handoffProxy.stop(); err != nil {
			output.Warn("development proxy shutdown", err)
		}
	}()
	activeServerAddress, err := availableDevelopmentServerAddress()
	if err != nil {
		_ = activeBinary.remove()
		return developmentFailure(ctx, output, "start server", err)
	}
	activeServerURL := developmentServerURL(activeServerAddress)
	activeServerEnvironment := developmentServerEnvironment(serverEnvironment, activeServerAddress)
	serverArguments := developmentServerArguments(definition.Database)
	adminEnvironment := []string{"RIDU_DEV_API=" + serverURL}
	server, err := startManagedProcess(ctx, "server", definition.Root, activeServerEnvironment, output, activeBinary.path, serverArguments...)
	if err != nil {
		_ = activeBinary.remove()
		return developmentFailure(ctx, output, "start server", err)
	}
	defer func() { _ = activeBinary.remove() }()
	var admin *managedProcess
	if definition.Admin != "" {
		managerCommand, managerArguments := packageManagerRunCommand(definition.FrontendPackageManager(), "dev", "--host", "127.0.0.1", "--port", fmt.Sprint(*adminPort), "--strictPort", "--logLevel", "warn")
		admin, err = startManagedProcess(ctx, "admin", definition.Absolute(definition.Admin), adminEnvironment, output, managerCommand, managerArguments...)
		if err != nil {
			server.stop()
			return developmentFailure(ctx, output, "start admin", err)
		}
	}
	if err := waitForDevelopmentURLWithHost(ctx, activeServerURL+"/readyz", publicServerHost, server); err != nil {
		server.stop()
		if admin != nil {
			admin.stop()
		}
		return developmentFailure(ctx, output, "start server", err)
	}
	if err := handoffProxy.setTarget(activeServerURL); err != nil {
		server.stop()
		if admin != nil {
			admin.stop()
		}
		output.Error("start server: configure development proxy", err)
		return 1
	}
	if admin != nil {
		if err := waitForDevelopmentURL(ctx, adminURL, admin); err != nil {
			server.stop()
			admin.stop()
			return developmentFailure(ctx, output, "start admin", err)
		}
	}
	output.DevelopmentReady(version, developmentDuration(time.Since(developmentStarted)), adminURL, serverURL)
	output.Info("Watching Go configuration; press Ctrl+C to stop")

watchLoop:
	for {
		select {
		case <-ctx.Done():
			server.stop()
			if admin != nil {
				admin.stop()
			}
			return developmentFailure(ctx, output, "development stopped", ctx.Err())
		case <-server.done:
			if ctx.Err() == nil && !server.stopping.Load() {
				if admin != nil {
					admin.stop()
				}
				output.Error("server stopped", server.waitError())
				return 1
			}
		case proxyError := <-handoffProxy.done:
			server.stop()
			if admin != nil {
				admin.stop()
			}
			output.Error("development proxy stopped unexpectedly", proxyError)
			return 1
		case <-managedDone(admin):
			if ctx.Err() == nil && admin != nil && !admin.stopping.Load() {
				server.stop()
				output.Error("admin stopped", admin.waitError())
				return 1
			}
		case watchError := <-watcher.Errors():
			output.Warn("watcher warning", watchError)
		case notifiedRevision := <-watcher.Changes():
			changeRevision := newestDevelopmentRevision(notifiedRevision, watcher.Revision())
			if changeRevision <= servedRevision {
				continue watchLoop
			}
			for {
				changeRevision = drainDevelopmentChanges(watcher.Changes(), changeRevision)
				changeRevision = newestDevelopmentRevision(changeRevision, watcher.Revision())
				changeStarted := time.Now()
				output.Info("Go configuration changed")
				candidate, candidateBuildDuration, buildError := buildDevelopmentBinary(ctx, definition, stdout, stderr)
				if buildError != nil {
					developmentFailure(ctx, output, "Reload rejected; the previous server is still running", buildError)
					continue watchLoop
				}
				if watcher.Revision() != changeRevision {
					_ = candidate.remove()
					output.Info("Newer Go change arrived during the build; skipping the stale replacement")
					continue
				}
				fresh := func() bool { return watcher.Revision() == changeRevision }
				candidate, candidateBuildDuration, preparation, prepareError := stabilizeDevelopmentCandidate(
					ctx, definition, version, candidate, candidateBuildDuration, !*noSync, fresh, stdout, stderr, output,
				)
				if prepareError != nil {
					_ = candidate.remove()
					if errors.Is(prepareError, errDevelopmentSourceChanged) {
						output.Info("Newer Go change arrived; skipping the stale replacement")
						continue
					}
					developmentFailure(ctx, output, "Reload rejected; the previous server is still running", prepareError)
					continue watchLoop
				}
				if watcher.Revision() != changeRevision {
					_ = candidate.remove()
					output.Info("Newer Go change arrived; skipping the stale replacement")
					continue
				}
				preparation, prepareError = synchronizeDevelopmentSchema(ctx, definition.Database, database.databaseURL, database.databasePath, !*noSync, false, preparation, output)
				if prepareError != nil {
					_ = candidate.remove()
					developmentFailure(ctx, output, "Reload rejected; the previous server is still running", prepareError)
					continue watchLoop
				}
				if watcher.Revision() != changeRevision && canDiscardStaleDevelopmentCandidate(definition.Database, preparation) {
					_ = candidate.remove()
					output.Info("Newer Go change arrived during database synchronization; skipping the stale replacement")
					continue
				}

				serverStarted := time.Now()
				candidateAddress, addressError := availableDevelopmentServerAddress()
				if addressError != nil {
					_ = candidate.remove()
					output.Error("Reload rejected; the previous server is still running", fmt.Errorf("%w%s", addressError, rejectedDevelopmentCandidateSuffix(preparation)))
					continue watchLoop
				}
				candidateURL := developmentServerURL(candidateAddress)
				candidateEnvironment := developmentServerEnvironment(serverEnvironment, candidateAddress)
				candidateServer, startError := startManagedProcess(ctx, "server", definition.Root, candidateEnvironment, output, candidate.path, serverArguments...)
				if startError != nil {
					_ = candidate.remove()
					developmentFailure(ctx, output, "replacement failed to start", fmt.Errorf("%w%s", startError, rejectedDevelopmentCandidateSuffix(preparation)))
					continue watchLoop
				}
				if readyError := waitForDevelopmentURLWithHost(ctx, candidateURL+"/readyz", publicServerHost, candidateServer); readyError != nil {
					candidateServer.stop()
					_ = candidate.remove()
					developmentFailure(ctx, output, "replacement server was unhealthy", fmt.Errorf("%w%s", readyError, rejectedDevelopmentCandidateSuffix(preparation)))
					continue watchLoop
				}
				if watcher.Revision() != changeRevision && canDiscardStaleDevelopmentCandidate(definition.Database, preparation) {
					candidateServer.stop()
					_ = candidate.remove()
					output.Info("Newer Go change arrived during replacement startup; kept the previous server")
					continue
				}
				if watcher.Revision() != changeRevision {
					output.Info("Newer Go change is queued; promoting the MongoDB candidate that matches the synchronized indexes first")
				}
				if proxyError := handoffProxy.setTarget(candidateURL); proxyError != nil {
					candidateServer.stop()
					_ = candidate.remove()
					output.Error("replacement proxy handoff failed", fmt.Errorf("%w%s", proxyError, rejectedDevelopmentCandidateSuffix(preparation)))
					continue watchLoop
				}
				serverDuration := time.Since(serverStarted)

				previousServer := server
				previousBinary := activeBinary
				server = candidateServer
				activeBinary = candidate
				servedRevision = changeRevision
				printDevelopmentTiming(output, "Server reloaded", time.Since(changeStarted), candidateBuildDuration, serverDuration, preparation)
				if preparation.schemaChanged && admin != nil {
					if err := writeAdminSchemaReloadSignal(definition.Root, preparation.adminPluginRegistryChanged); err != nil {
						output.Warn("admin reload", err)
					} else if preparation.adminPluginRegistryChanged {
						output.Info("Admin plugin registry changed; reloading the admin with draft recovery")
					} else {
						output.Info("Schema changed; refreshing the admin in place")
					}
				}
				previousServer.stop()
				if err := previousBinary.remove(); err != nil {
					output.Warn("development binary cleanup", err)
				}
				continue watchLoop
			}
		}
	}
}

func waitForDevelopmentURL(ctx context.Context, url string, process *managedProcess) error {
	return waitForDevelopmentURLWithHost(ctx, url, "", process)
}

func waitForDevelopmentURLWithHost(ctx context.Context, url, host string, process *managedProcess) error {
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if host != "" {
			request.Host = host
		}
		response, requestError := client.Do(request)
		if requestError == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 400 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-process.done:
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("process exited before %s became ready: %v", url, process.waitError())
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s", url)
		case <-ticker.C:
		}
	}
}

func prepareDevelopment(ctx context.Context, definition projectfile.File, version, executable string, syncSchema bool, fresh func() bool, output *cliOutput) (developmentPreparation, error) {
	manifestStarted := time.Now()
	resolved, err := generate.ResolveProjectExecutable(ctx, definition, version, executable)
	if err != nil {
		return developmentPreparation{}, fmt.Errorf("resolve executable config: %w", err)
	}
	manifestDuration := time.Since(manifestStarted)
	if fresh != nil && !fresh() {
		return developmentPreparation{}, errDevelopmentSourceChanged
	}
	contractStarted := time.Now()
	result, err := generate.RunResolvedProjectDevelopment(definition, resolved)
	if err != nil {
		return developmentPreparation{}, fmt.Errorf("generate contracts: %w", err)
	}
	contractDuration := time.Since(contractStarted)
	printGenerationWarnings(output, result)
	if syncSchema && len(result.Warnings) != 0 {
		return developmentPreparation{}, developmentSchemaWarningError(definition.Database)
	}
	changed := 0
	for _, artifact := range result.Artifacts {
		if artifact.Changed {
			changed++
		}
	}
	if changed != 0 {
		output.Info("Generated changed contracts", "count", changed)
	}
	schemaChanged := false
	adminPluginRegistryChanged := false
	generatedGoChanged := false
	schemaPath := definition.Absolute(definition.Schema)
	adminPluginRegistryPath := ""
	generatedGoPath := filepath.Join(filepath.Dir(schemaPath), "ridu.generated.go")
	if definition.Admin != "" {
		adminPluginRegistryPath = filepath.Join(definition.Absolute(definition.Admin), "src", "ridu.plugins.generated.ts")
	}
	for _, artifact := range result.Artifacts {
		if artifact.Path == schemaPath && artifact.Changed {
			schemaChanged = true
		}
		if artifact.Path == adminPluginRegistryPath && artifact.Changed {
			adminPluginRegistryChanged = true
		}
		if artifact.Path == generatedGoPath && artifact.Changed {
			generatedGoChanged = true
		}
	}
	preparation := developmentPreparation{
		manifest:                   result.Manifest,
		schemaChanged:              schemaChanged,
		adminPluginRegistryChanged: adminPluginRegistryChanged,
		generatedGoChanged:         generatedGoChanged,
		manifestDuration:           manifestDuration,
		contractDuration:           contractDuration,
	}
	return preparation, nil
}

func developmentSchemaWarningError(adapter projectfile.DatabaseAdapter) error {
	if adapter == projectfile.DatabaseMongoDB {
		return fmt.Errorf("automatic MongoDB development index sync paused for a possible rename; restore the last accepted config, or create and review an immutable artifact with ridu migrate create --name <name>, explicitly confirm each rename or bind a registered transform, drain every application process and writer, run ridu migrate verify, capture a matched database-and-upload recovery point, apply with ridu migrate up --allow-maintenance, require ridu migrate status to be current, then restart so exact readiness can admit the new manifest")
	}
	return fmt.Errorf("automatic development schema sync paused for a possible rename; run ridu migrate create --name <name>, review the migration, then run ridu migrate up")
}

func synchronizeDevelopmentSchema(ctx context.Context, adapter projectfile.DatabaseAdapter, databaseURL, databasePath string, syncSchema, forceSchemaSync bool, preparation developmentPreparation, output *cliOutput) (developmentPreparation, error) {
	if !syncSchema || (!forceSchemaSync && !preparation.schemaChanged) {
		return preparation, nil
	}
	schemaSyncStarted := time.Now()
	if adapter == projectfile.DatabaseSQLite {
		backend, err := sqlite.Open(ctx, databasePath)
		if err != nil {
			return developmentPreparation{}, fmt.Errorf("connect to development SQLite: %w", err)
		}
		defer backend.Close()
		if err := backend.Migrate(ctx, preparation.manifest); err != nil {
			return developmentPreparation{}, fmt.Errorf("apply SQLite development schema: %w", err)
		}
		if err := backend.Ready(ctx, preparation.manifest); err != nil {
			return developmentPreparation{}, fmt.Errorf("verify SQLite development schema: %w", err)
		}
		output.Info("Synchronized SQLite development schema")
		preparation.schemaSyncDuration = time.Since(schemaSyncStarted)
		return preparation, nil
	}
	if adapter == projectfile.DatabaseMongoDB {
		backend, err := openDevelopmentMongoDB(ctx, databaseURL)
		if err != nil {
			return developmentPreparation{}, err
		}
		defer backend.Close()
		if err := backend.SyncIndexes(ctx, preparation.manifest); err != nil {
			return developmentPreparation{}, fmt.Errorf("synchronize MongoDB development indexes: %w", err)
		}
		output.Info("Synchronized MongoDB development indexes")
		preparation.schemaSyncDuration = time.Since(schemaSyncStarted)
		preparation.mongoIndexesSynchronized = true
		return preparation, nil
	}
	if adapter != projectfile.DatabasePostgres {
		return developmentPreparation{}, fmt.Errorf("unsupported development database adapter %q", adapter)
	}
	backend, err := openDevelopmentPostgres(ctx, databaseURL)
	if err != nil {
		return developmentPreparation{}, err
	}
	defer backend.Close()
	plan, err := backend.Plan(ctx, preparation.manifest)
	if err != nil {
		return developmentPreparation{}, fmt.Errorf("plan development schema: %w", err)
	}
	if len(plan) != 0 {
		if err := backend.ApplyPlan(ctx, plan); err != nil {
			return developmentPreparation{}, fmt.Errorf("apply development schema: %w", err)
		}
		output.Info("Applied non-destructive development schema changes", "count", len(plan))
	}
	preparation.schemaSyncDuration = time.Since(schemaSyncStarted)
	return preparation, nil
}

func stabilizeDevelopmentCandidate(ctx context.Context, definition projectfile.File, version string, candidate developmentBinary, buildDuration time.Duration, syncSchema bool, fresh func() bool, stdout, stderr io.Writer, output *cliOutput) (developmentBinary, time.Duration, developmentPreparation, error) {
	var combined developmentPreparation
	for round := 0; round < 4; round++ {
		preparation, err := prepareDevelopment(ctx, definition, version, candidate.path, syncSchema, fresh, output)
		if err != nil {
			return candidate, buildDuration, combined, err
		}
		combined = mergeDevelopmentPreparation(combined, preparation)
		if !preparation.generatedGoChanged {
			return candidate, buildDuration, combined, nil
		}
		rebuilt, nextBuildDuration, err := rebuildForGeneratedGo(ctx, definition, candidate, buildDuration, preparation, stdout, stderr, output)
		if err != nil {
			return developmentBinary{}, nextBuildDuration, combined, err
		}
		if rebuilt.path == candidate.path {
			return candidate, nextBuildDuration, combined, nil
		}
		candidate = rebuilt
		buildDuration = nextBuildDuration
		if fresh != nil && !fresh() {
			_ = candidate.remove()
			return developmentBinary{}, buildDuration, combined, errDevelopmentSourceChanged
		}
	}
	_ = candidate.remove()
	return developmentBinary{}, buildDuration, combined, fmt.Errorf("generated Go contracts did not stabilize after 4 development builds")
}

func mergeDevelopmentPreparation(previous, current developmentPreparation) developmentPreparation {
	current.schemaChanged = current.schemaChanged || previous.schemaChanged
	current.adminPluginRegistryChanged = current.adminPluginRegistryChanged || previous.adminPluginRegistryChanged
	current.generatedGoChanged = current.generatedGoChanged || previous.generatedGoChanged
	current.manifestDuration += previous.manifestDuration
	current.contractDuration += previous.contractDuration
	return current
}

func rebuildForGeneratedGo(ctx context.Context, definition projectfile.File, candidate developmentBinary, buildDuration time.Duration, preparation developmentPreparation, stdout, stderr io.Writer, output *cliOutput) (developmentBinary, time.Duration, error) {
	if !preparation.generatedGoChanged {
		return candidate, buildDuration, nil
	}
	rebuild, err := developmentEntryImportsGeneratedGo(ctx, definition)
	if err != nil {
		output.Warn("generated Go dependency check failed; rebuilding conservatively", err)
		rebuild = true
	}
	if !rebuild {
		output.Info("Generated Go contract changed outside the server dependency graph; skipping a redundant rebuild")
		return candidate, buildDuration, nil
	}
	if err := candidate.remove(); err != nil {
		return developmentBinary{}, buildDuration, fmt.Errorf("replace development binary after generated Go changed: %w", err)
	}
	output.Info("Generated Go contract changed; rebuilding the same replacement")
	rebuilt, duration, err := buildDevelopmentBinary(ctx, definition, stdout, stderr)
	if err != nil {
		return developmentBinary{}, buildDuration + duration, err
	}
	return rebuilt, buildDuration + duration, nil
}

func developmentEntryImportsGeneratedGo(ctx context.Context, definition projectfile.File) (bool, error) {
	entry := "./" + strings.TrimPrefix(filepath.ToSlash(definition.Entry), "./")
	output, err := commandOutput(ctx, definition.Root, nil, "go", "list", "-deps", "-f={{.Dir}}", entry)
	if err != nil {
		return false, fmt.Errorf("inspect development server dependencies: %w: %s", err, strings.TrimSpace(output))
	}
	generatedDirectory := canonicalDevelopmentDirectory(filepath.Dir(definition.Absolute(definition.Schema)))
	for _, directory := range strings.Split(output, "\n") {
		if canonicalDevelopmentDirectory(strings.TrimSpace(directory)) == generatedDirectory {
			return true, nil
		}
	}
	return false, nil
}

func canonicalDevelopmentDirectory(directory string) string {
	directory = filepath.Clean(directory)
	if resolved, err := filepath.EvalSymlinks(directory); err == nil {
		return filepath.Clean(resolved)
	}
	return directory
}

func printDevelopmentTiming(output *cliOutput, label string, total, build, server time.Duration, preparation developmentPreparation) {
	parts := []string{
		"build " + developmentDuration(build),
		"manifest " + developmentDuration(preparation.manifestDuration),
		"contracts " + developmentDuration(preparation.contractDuration),
	}
	if preparation.schemaSyncDuration > 0 {
		parts = append(parts, "database "+developmentDuration(preparation.schemaSyncDuration))
	}
	parts = append(parts, "start "+developmentDuration(server))
	output.Info(fmt.Sprintf("%s in %s (%s)", label, developmentDuration(total), strings.Join(parts, ", ")))
}

func developmentDuration(duration time.Duration) string {
	if duration < time.Millisecond {
		return "<1ms"
	}
	return duration.Round(time.Millisecond).String()
}

func drainDevelopmentChanges(changes <-chan uint64, revision uint64) uint64 {
	for {
		select {
		case current := <-changes:
			if current > revision {
				revision = current
			}
		default:
			return revision
		}
	}
}

func newestDevelopmentRevision(notified, current uint64) uint64 {
	if current > notified {
		return current
	}
	return notified
}

func writeAdminSchemaReloadSignal(root string, fullReload bool) error {
	path := filepath.Join(root, filepath.FromSlash(adminSchemaReloadSignal))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.Marshal(adminSchemaUpdateSignal{
		Version:    1,
		Revision:   fmt.Sprint(time.Now().UnixNano()),
		FullReload: fullReload,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func canDiscardStaleDevelopmentCandidate(adapter projectfile.DatabaseAdapter, preparation developmentPreparation) bool {
	return adapter != projectfile.DatabaseMongoDB || !preparation.mongoIndexesSynchronized
}

func rejectedDevelopmentCandidateSuffix(preparation developmentPreparation) string {
	if preparation.mongoIndexesSynchronized {
		return "; kept the previous server; synchronized additive MongoDB indexes may remain, and saving again retries the replacement"
	}
	return "; kept the previous server"
}

func openDevelopmentPostgres(ctx context.Context, databaseURL string) (*postgres.Store, error) {
	var lastError error
	for attempt := 0; attempt < 30; attempt++ {
		backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{
			DatabaseURL: databaseURL, AllowInsecureTransport: true, ApplicationName: "ridu-development",
		})
		if err == nil {
			return backend, nil
		}
		lastError = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("connect to development PostgreSQL: %w", lastError)
}

func openDevelopmentMongoDB(ctx context.Context, databaseURL string) (*mongodb.Store, error) {
	var lastError error
	for attempt := 0; attempt < 30; attempt++ {
		backend, err := mongodb.OpenWithConfig(ctx, mongodb.Config{
			DatabaseURL:            databaseURL,
			AllowInsecureTransport: true,
			ApplicationName:        "ridu-development-schema",
			ConnectTimeout:         time.Second,
			ServerSelectionTimeout: time.Second,
		})
		if err == nil {
			return backend, nil
		}
		lastError = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("connect to development MongoDB: %w", lastError)
}

func ensureFrontendDependencies(ctx context.Context, definition projectfile.File, stdout, stderr io.Writer, output *cliOutput) error {
	if definition.Admin == "" {
		return nil
	}
	fingerprint, err := frontendDependencyFingerprint(definition)
	if err != nil {
		return err
	}
	fingerprintPath := filepath.Join(definition.Root, ".ridu", "frontend-dependencies.sha256")
	installedFingerprint, _ := os.ReadFile(fingerprintPath)
	if _, err := os.Stat(filepath.Join(definition.Root, "node_modules")); err == nil && strings.TrimSpace(string(installedFingerprint)) == fingerprint {
		return nil
	}
	command, arguments := packageManagerInstallCommand(ctx, definition)
	output.Info("Installing frontend dependencies")
	if err := runForeground(ctx, definition.Root, nil, stdout, stderr, command, arguments...); err != nil {
		return err
	}
	fingerprint, err = frontendDependencyFingerprint(definition)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fingerprintPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(fingerprintPath, []byte(fingerprint+"\n"), 0o600)
}

func frontendDependencyFingerprint(definition projectfile.File) (string, error) {
	paths := []string{
		definition.Path,
		filepath.Join(definition.Root, "package.json"),
	}
	paths = append(paths, packageManagerLockfiles(definition)...)
	if definition.Admin != "" {
		paths = append(paths, filepath.Join(definition.Absolute(definition.Admin), "package.json"))
	}
	hash := sha256.New()
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read frontend dependency input %s: %w", relativePath(definition.Root, path), err)
		}
		_, _ = io.WriteString(hash, relativePath(definition.Root, path)+"\x00")
		_, _ = hash.Write(content)
		_, _ = io.WriteString(hash, "\x00")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type managedProcess struct {
	cancel      context.CancelFunc
	command     *exec.Cmd
	done        chan struct{}
	err         error
	mutex       sync.Mutex
	stopping    atomic.Bool
	once        sync.Once
	stopTimeout time.Duration
}

func startManagedProcess(parent context.Context, label, directory string, environment []string, output *cliOutput, name string, arguments ...string) (*managedProcess, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	// The lifecycle owner translates parent cancellation into a graceful
	// process-group termination. Detaching the command context prevents
	// exec.CommandContext from sending an immediate SIGKILL before the server
	// can drain HTTP requests, workers, and Store resources.
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), environment...)
	stdoutWriter := output.sourceWriter(output.stdout, label, output.stdoutProfile)
	stderrWriter := output.sourceWriter(output.stderr, label, output.stderrProfile)
	command.Stdout = stdoutWriter
	command.Stderr = stderrWriter
	configureProcess(command)
	if err := command.Start(); err != nil {
		cancel()
		return nil, err
	}
	process := &managedProcess{
		cancel:      cancel,
		command:     command,
		done:        make(chan struct{}),
		stopTimeout: managedProcessStopTimeout(label, environment),
	}
	go func() {
		err := command.Wait()
		err = errors.Join(err, stdoutWriter.Close(), stderrWriter.Close())
		cancel()
		process.mutex.Lock()
		process.err = err
		process.mutex.Unlock()
		close(process.done)
	}()
	return process, nil
}

func (process *managedProcess) stop() {
	if process == nil {
		return
	}
	process.once.Do(func() {
		process.stopping.Store(true)
		terminateProcess(process.command, false)
		timer := time.NewTimer(process.stopTimeout)
		defer timer.Stop()
		select {
		case <-process.done:
			// The group leader may finish its graceful drain while a descendant
			// ignores the signal. On qualified Unix hosts the process group still
			// exists, so remove any survivors before returning.
			terminateProcess(process.command, true)
			process.cancel()
		case <-timer.C:
			terminateProcess(process.command, true)
			process.cancel()
			<-process.done
		}
	})
}

func managedProcessStopTimeout(label string, environment []string) time.Duration {
	if label != "server" {
		return defaultManagedProcessStopTimeout
	}
	readinessDrain := effectiveManagedProcessDuration(environment, "RIDU_READINESS_DRAIN_DELAY", 2*time.Second, true)
	shutdown := effectiveManagedProcessDuration(environment, "RIDU_SHUTDOWN_TIMEOUT", 15*time.Second, false)
	workerDrain := effectiveManagedProcessDuration(environment, "RIDU_WORKER_DRAIN_TIMEOUT", 15*time.Second, false)
	return saturatedDurationSum(readinessDrain, shutdown, shutdown, workerDrain, developmentServerStopBuffer)
}

func effectiveManagedProcessDuration(environment []string, name string, fallback time.Duration, negativeDisables bool) time.Duration {
	raw := ""
	if inherited, exists := os.LookupEnv(name); exists {
		raw = inherited
	}
	prefix := name + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			raw = strings.TrimPrefix(entry, prefix)
		}
	}
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if strings.TrimSpace(raw) == "" || err != nil || value == 0 || (!negativeDisables && value < 0) {
		return fallback
	}
	if value < 0 {
		return 0
	}
	return value
}

func saturatedDurationSum(values ...time.Duration) time.Duration {
	const maximum = time.Duration(1<<63 - 1)
	var total time.Duration
	for _, value := range values {
		if value > maximum-total {
			return maximum
		}
		total += value
	}
	return total
}

func (process *managedProcess) waitError() error {
	if process == nil {
		return nil
	}
	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.err
}

func managedDone(process *managedProcess) <-chan struct{} {
	if process == nil {
		return nil
	}
	return process.done
}

func unformattedGoFiles(ctx context.Context, root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root && shouldSkipDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil || len(files) == 0 {
		return nil, err
	}
	arguments := append([]string{"-l"}, files...)
	output, err := commandOutput(ctx, root, nil, "gofmt", arguments...)
	if err != nil {
		return nil, err
	}
	var relative []string
	for _, path := range strings.Fields(output) {
		relative = append(relative, relativePath(root, path))
	}
	return relative, nil
}

func shouldSkipDirectory(name string) bool {
	switch name {
	case ".git", ".ridu", "node_modules", "dist", "generated":
		return true
	default:
		return false
	}
}

func frameworkVersion(options Options) string {
	if options.FrameworkVersion != "" {
		return options.FrameworkVersion
	}
	return options.Version
}

func printGenerationWarnings(output *cliOutput, result generate.Result) {
	for _, warning := range result.Warnings {
		output.Warn(warning, nil)
	}
}

func runForeground(ctx context.Context, directory string, environment []string, stdout, stderr io.Writer, name string, arguments ...string) error {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), environment...)
	if name == "go" {
		command.Env = goworkspace.IsolateUnlistedModule(directory, command.Env)
	}
	command.Stdout = unwrapCLIWriter(stdout)
	command.Stderr = unwrapCLIWriter(stderr)
	return commandrun.Run(ctx, command)
}

type cliWriterUnwrapper interface {
	unwrappedCLIWriter() io.Writer
}

func unwrapCLIWriter(writer io.Writer) io.Writer {
	if wrapped, ok := writer.(cliWriterUnwrapper); ok {
		return wrapped.unwrappedCLIWriter()
	}
	return writer
}

func commandOutput(ctx context.Context, directory string, environment []string, name string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), environment...)
	if name == "go" {
		command.Env = goworkspace.IsolateUnlistedModule(directory, command.Env)
	}
	var encoded bytes.Buffer
	command.Stdout = &encoded
	command.Stderr = &encoded
	err := commandrun.Run(ctx, command)
	return encoded.String(), err
}

func relativePath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func developmentServerURL(address string) string {
	host := address
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if strings.HasPrefix(host, "0.0.0.0:") {
		host = "127.0.0.1:" + strings.TrimPrefix(host, "0.0.0.0:")
	}
	return "http://" + host
}

// Use the same script as the shipped build, including its mode, config arguments
// and environment. The shared Vite config substitutes an in-memory registry check.
func checkAdminRegistrations(ctx context.Context, definition projectfile.File, stdout, stderr io.Writer) error {
	cache := definition.Absolute(".ridu")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(cache, "admin-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	receipt := filepath.Join(temporary, "complete")
	command, arguments := packageManagerRunCommand(definition.FrontendPackageManager(), "build")
	if err := runForeground(ctx, definition.Absolute(definition.Admin), []string{
		"RIDU_ADMIN_CHECK_SCHEMA=" + definition.Absolute(definition.Schema),
		"RIDU_ADMIN_CHECK_RECEIPT=" + receipt,
	}, stdout, stderr, command, arguments...); err != nil {
		return err
	}
	checked, err := os.ReadFile(receipt)
	if err != nil || string(checked) != "checked\n" {
		return errors.New("admin build did not verify admin registrations; retain createAdminApplicationConfig from @riducms/build/vite in the Vite configuration used by the build script")
	}
	return nil
}
