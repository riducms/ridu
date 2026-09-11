package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/project"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestRestoreAdminAssetsPlaceholderAfterFrontendBuild(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	definition := projectfile.File{Root: root, Assets: "internal/adminassets/dist"}
	if err := restoreAdminAssetsPlaceholder(definition); err != nil {
		t.Fatalf("restoreAdminAssetsPlaceholder: %v", err)
	}
	placeholder := filepath.Join(root, "internal", "adminassets", "dist", "placeholder.txt")
	encoded, err := os.ReadFile(placeholder)
	if err != nil {
		t.Fatalf("read placeholder: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("placeholder is empty")
	}
}

func TestProjectMigrationPlannerNameMatchesOfficialDatabaseSelection(t *testing.T) {
	for _, test := range []struct {
		adapter projectfile.DatabaseAdapter
		planner string
	}{
		{adapter: projectfile.DatabasePostgres, planner: "atlas"},
		{adapter: projectfile.DatabaseSQLite, planner: "ridu-sqlite"},
		{adapter: projectfile.DatabaseMongoDB, planner: "mongodb"},
	} {
		planner, err := projectMigrationPlannerName(test.adapter)
		if err != nil || planner != test.planner {
			t.Errorf("projectMigrationPlannerName(%q) = %q, %v; want %q", test.adapter, planner, err, test.planner)
		}
	}
	if planner, err := projectMigrationPlannerName("unknown"); err == nil || planner != "" || !strings.Contains(err.Error(), "unsupported database adapter") {
		t.Fatalf("unknown project migration planner = %q, %v", planner, err)
	}
}

func TestDevelopmentDurationKeepsReloadTimingsReadable(t *testing.T) {
	for _, test := range []struct {
		duration time.Duration
		want     string
	}{
		{duration: 300 * time.Microsecond, want: "<1ms"},
		{duration: 1534 * time.Millisecond, want: "1.534s"},
	} {
		if got := developmentDuration(test.duration); got != test.want {
			t.Errorf("developmentDuration(%v) = %q, want %q", test.duration, got, test.want)
		}
	}
}

func TestDevelopmentTimingReportsCandidateStartup(t *testing.T) {
	var output bytes.Buffer
	reporter := newCLIOutput(&output, io.Discard, cliOutputOptions{})
	printDevelopmentTiming(reporter, "Server reloaded", 877*time.Millisecond, 365*time.Millisecond, 100*time.Millisecond, developmentPreparation{
		manifestDuration: 409 * time.Millisecond,
		contractDuration: 3 * time.Millisecond,
	})
	encoded := output.String()
	for _, required := range []string{"Server reloaded in 877ms", "build 365ms", "manifest 409ms", "contracts 3ms", "start 100ms"} {
		if !strings.Contains(encoded, required) {
			t.Errorf("development timing is missing %q: %s", required, encoded)
		}
	}
}

func TestDevelopmentProxyPromotesOnlyAnAcceptedLoopbackTarget(t *testing.T) {
	backend := func(name string, observations chan<- *http.Request) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			select {
			case observations <- request.Clone(request.Context()):
			default:
			}
			writer.Header().Set("X-Ridu-Backend", name)
			writer.WriteHeader(http.StatusNoContent)
		}))
	}
	observations := make(chan *http.Request, 4)
	previous := backend("previous", observations)
	t.Cleanup(previous.Close)
	candidate := backend("candidate", observations)
	t.Cleanup(candidate.Close)

	proxy, err := startDevelopmentProxy("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := proxy.stop(); err != nil {
			t.Errorf("stop development proxy: %v", err)
		}
	})
	proxyURL := "http://" + proxy.listener.Addr().String()
	client := &http.Client{Timeout: 2 * time.Second}

	response, err := client.Get(proxyURL + "/before-promotion")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("proxy without a target status = %d, want %d", response.StatusCode, http.StatusServiceUnavailable)
	}

	for _, invalid := range []string{
		"https://127.0.0.1:8080",
		"http://localhost:8080",
		"http://example.com:8080",
		"http://user:secret@127.0.0.1:8080",
	} {
		if err := proxy.setTarget(invalid); err == nil {
			t.Errorf("setTarget(%q) succeeded, want a loopback-only rejection", invalid)
		}
	}
	if err := proxy.setTarget(previous.URL); err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequest(http.MethodGet, proxyURL+"/kept", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Forwarded-For", "203.0.113.40")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if backendName := response.Header.Get("X-Ridu-Backend"); backendName != "previous" {
		t.Fatalf("rejected candidate changed backend to %q", backendName)
	}
	observed := <-observations
	if observed.Host != request.URL.Host || strings.Contains(observed.Header.Get("X-Forwarded-For"), "203.0.113.40") {
		t.Fatalf("proxy forwarding boundary host=%q x-forwarded-for=%q", observed.Host, observed.Header.Get("X-Forwarded-For"))
	}

	if err := proxy.setTarget(candidate.URL); err != nil {
		t.Fatal(err)
	}
	response, err = client.Get(proxyURL + "/promoted")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if backendName := response.Header.Get("X-Ridu-Backend"); backendName != "candidate" {
		t.Fatalf("accepted candidate backend = %q", backendName)
	}
}

func TestDevelopmentServerAdmissionEnvironmentIsMongoDBSpecific(t *testing.T) {
	mongodbEnvironment := strings.Join(developmentServerBaseEnvironment(projectfile.DatabaseMongoDB), "\n")
	for _, publicOverride := range []string{"RIDU_ALLOW_UNVERIFIABLE_READINESS", "RIDU_SKIP_READINESS_PREFLIGHT", "RIDU_INTERNAL_DEVELOPMENT_SERVER"} {
		if strings.Contains(mongodbEnvironment, publicOverride) {
			t.Errorf("MongoDB development environment exposes readiness admission override %q", publicOverride)
		}
	}
	arguments := developmentServerArguments(projectfile.DatabaseMongoDB)
	if len(arguments) != 1 || arguments[0] != internalDevelopmentServerArgument {
		t.Fatalf("MongoDB internal development arguments = %q", arguments)
	}

	for _, adapter := range []projectfile.DatabaseAdapter{projectfile.DatabasePostgres, projectfile.DatabaseSQLite, projectfile.DatabaseMongoDB} {
		environment := strings.Join(developmentServerBaseEnvironment(adapter), "\n")
		if !strings.Contains(environment, disabledDevelopmentReadinessDrain) {
			t.Errorf("%s development environment retained the production readiness drain: %q", adapter, environment)
		}
		if adapter != projectfile.DatabaseMongoDB {
			if !strings.Contains(environment, "RIDU_ALLOW_UNVERIFIABLE_READINESS=true") ||
				!strings.Contains(environment, "RIDU_SKIP_READINESS_PREFLIGHT=true") {
				t.Errorf("%s development admission environment changed: %q", adapter, environment)
			}
			if arguments := developmentServerArguments(adapter); len(arguments) != 0 {
				t.Errorf("%s received MongoDB internal development arguments %q", adapter, arguments)
			}
		}
	}
}

func TestWaitForDevelopmentURLUsesThePublicHostForPrivateCandidates(t *testing.T) {
	const publicHost = "cms.ridu.test:8080"
	observedHost := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observedHost <- request.Host
		if request.Host != publicHost {
			http.Error(writer, "unexpected host", http.StatusMisdirectedRequest)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	process := &managedProcess{done: make(chan struct{})}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := waitForDevelopmentURLWithHost(ctx, server.URL+"/readyz", publicHost, process); err != nil {
		t.Fatal(err)
	}
	if host := <-observedHost; host != publicHost {
		t.Fatalf("private readiness probe Host = %q, want public Host %q", host, publicHost)
	}
}

func TestSynchronizedMongoDBCandidateCannotBeDiscardedAsStaleDuringInitialOrReload(t *testing.T) {
	committed := developmentPreparation{mongoIndexesSynchronized: true}
	if canDiscardStaleDevelopmentCandidate(projectfile.DatabaseMongoDB, committed) {
		t.Fatal("MongoDB candidate was discardable after its additive index sync committed")
	}
	if !canDiscardStaleDevelopmentCandidate(projectfile.DatabaseMongoDB, developmentPreparation{}) {
		t.Fatal("unsynchronized MongoDB candidate could not be discarded")
	}
	if !canDiscardStaleDevelopmentCandidate(projectfile.DatabasePostgres, committed) {
		t.Fatal("MongoDB-specific promotion rule changed PostgreSQL reload behavior")
	}
	if suffix := rejectedDevelopmentCandidateSuffix(committed); !strings.Contains(suffix, "additive MongoDB indexes may remain") || !strings.Contains(suffix, "saving again retries") {
		t.Fatalf("post-sync rejection guidance = %q", suffix)
	}
	if suffix := rejectedDevelopmentCandidateSuffix(developmentPreparation{}); suffix != "; kept the previous server" {
		t.Fatalf("ordinary rejection suffix = %q", suffix)
	}
}

func TestDevelopmentSchemaWarningIsAdapterSpecific(t *testing.T) {
	mongodbError := developmentSchemaWarningError(projectfile.DatabaseMongoDB).Error()
	for _, required := range []string{
		"restore the last accepted config",
		"ridu migrate create --name <name>",
		"confirm each rename or bind a registered transform",
		"drain every application process and writer",
		"ridu migrate verify",
		"capture a matched database-and-upload recovery point",
		"ridu migrate up --allow-maintenance",
		"ridu migrate status",
		"exact readiness",
	} {
		if !strings.Contains(mongodbError, required) {
			t.Errorf("MongoDB rename warning is missing %q: %s", required, mongodbError)
		}
	}
	for _, stale := range []string{"not qualified", "additive optional field"} {
		if strings.Contains(mongodbError, stale) {
			t.Fatalf("MongoDB rename warning retains stale development-only guidance %q: %s", stale, mongodbError)
		}
	}
	previous := -1
	for _, marker := range []string{
		"drain every application process and writer",
		"ridu migrate verify",
		"capture a matched database-and-upload recovery point",
		"ridu migrate up --allow-maintenance",
		"ridu migrate status",
		"exact readiness",
	} {
		position := strings.Index(mongodbError, marker)
		if position <= previous {
			t.Fatalf("MongoDB rename warning does not preserve cutover order at %q: %s", marker, mongodbError)
		}
		previous = position
	}
	for _, adapter := range []projectfile.DatabaseAdapter{projectfile.DatabasePostgres, projectfile.DatabaseSQLite} {
		if warning := developmentSchemaWarningError(adapter).Error(); !strings.Contains(warning, "ridu migrate up") {
			t.Errorf("%s rename warning changed: %s", adapter, warning)
		}
	}
}

func TestManagedProcessStopPreservesTheServerDrainWindow(t *testing.T) {
	for _, name := range []string{"RIDU_READINESS_DRAIN_DELAY", "RIDU_SHUTDOWN_TIMEOUT", "RIDU_WORKER_DRAIN_TIMEOUT"} {
		t.Setenv(name, "")
	}
	for _, test := range []struct {
		name        string
		environment []string
		want        time.Duration
	}{
		{name: "defaults", want: 52 * time.Second},
		{name: "custom shutdown", environment: []string{"RIDU_SHUTDOWN_TIMEOUT=30s"}, want: 82 * time.Second},
		{name: "all custom", environment: []string{"RIDU_READINESS_DRAIN_DELAY=7s", "RIDU_SHUTDOWN_TIMEOUT=20s", "RIDU_WORKER_DRAIN_TIMEOUT=25s"}, want: 77 * time.Second},
		{name: "disabled readiness drain", environment: []string{"RIDU_READINESS_DRAIN_DELAY=-1s"}, want: 50 * time.Second},
		{name: "zero values use runtime defaults", environment: []string{"RIDU_READINESS_DRAIN_DELAY=0s", "RIDU_SHUTDOWN_TIMEOUT=0s", "RIDU_WORKER_DRAIN_TIMEOUT=0s"}, want: 52 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := managedProcessStopTimeout("server", test.environment); got != test.want {
				t.Fatalf("server stop timeout = %s, want %s", got, test.want)
			}
		})
	}
	for _, label := range []string{"admin", "generator"} {
		if got := managedProcessStopTimeout(label, []string{"RIDU_SHUTDOWN_TIMEOUT=2m"}); got != defaultManagedProcessStopTimeout {
			t.Errorf("%s stop timeout = %s, want %s", label, got, defaultManagedProcessStopTimeout)
		}
	}
	t.Setenv("RIDU_SHUTDOWN_TIMEOUT", "45s")
	if got := managedProcessStopTimeout("server", nil); got != 112*time.Second {
		t.Errorf("inherited server stop timeout = %s, want 1m52s", got)
	}
	if got := managedProcessStopTimeout("server", []string{"RIDU_SHUTDOWN_TIMEOUT=20s"}); got != 62*time.Second {
		t.Errorf("overridden server stop timeout = %s, want 1m2s", got)
	}

	done := make(chan struct{})
	cancelled := make(chan struct{})
	process := &managedProcess{
		cancel:      func() { close(cancelled) },
		done:        done,
		stopTimeout: time.Second,
	}
	stopped := make(chan struct{})
	go func() {
		process.stop()
		close(stopped)
	}()
	deadline := time.Now().Add(time.Second)
	for !process.stopping.Load() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if !process.stopping.Load() {
		t.Fatal("managed process did not begin stopping")
	}
	select {
	case <-cancelled:
		t.Fatal("managed process cancelled its command context before graceful completion")
	default:
	}
	close(done)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("managed process did not finish after graceful completion")
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("managed process did not release its command context after completion")
	}
}

func TestDrainDevelopmentChangesReturnsTheNewestRevision(t *testing.T) {
	changes := make(chan uint64, 3)
	changes <- 2
	changes <- 4
	changes <- 3
	if got := drainDevelopmentChanges(changes, 1); got != 4 {
		t.Fatalf("newest revision = %d, want 4", got)
	}
	if got := drainDevelopmentChanges(changes, 4); got != 4 {
		t.Fatalf("draining an empty channel changed revision to %d", got)
	}
}

func TestNewestDevelopmentRevisionCatchesUpWhenTheNotificationBufferDropsAnEdit(t *testing.T) {
	changes := make(chan uint64, 1)
	changes <- 4
	select {
	case changes <- 5:
		t.Fatal("full watcher notification buffer unexpectedly accepted the newer token")
	default:
	}
	notified := <-changes
	if got := newestDevelopmentRevision(notified, 5); got != 5 {
		t.Fatalf("catch-up revision = %d, want current watcher revision 5", got)
	}
}

func TestResolveDevelopmentDatabaseHonorsProjectSelection(t *testing.T) {
	root := t.TempDir()
	sqliteDefinition := projectfile.File{Root: root, Database: projectfile.DatabaseSQLite}
	database, err := resolveDevelopmentDatabase(sqliteDefinition, "", "", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if database.databasePath != filepath.Join(root, filepath.FromSlash(defaultDevelopmentSQLitePath)) || database.databaseURL != "" || database.startPostgres || database.startMongoDB {
		t.Fatalf("SQLite development database = %+v", database)
	}
	if _, err := resolveDevelopmentDatabase(sqliteDefinition, "postgres://explicit", "", true, false, false); err == nil || !strings.Contains(err.Error(), "do not accept --database-url") {
		t.Fatalf("SQLite PostgreSQL flag error = %v", err)
	}
	if _, err := resolveDevelopmentDatabase(sqliteDefinition, "", ":memory:", false, true, false); err == nil || !strings.Contains(err.Error(), "separate processes") {
		t.Fatalf("SQLite memory development error = %v", err)
	}
	if _, err := resolveDevelopmentDatabase(sqliteDefinition, "", "relative.sqlite", false, false, false); err == nil || !strings.Contains(err.Error(), "must be an absolute") {
		t.Fatalf("SQLite relative environment path error = %v", err)
	}
	database, err = resolveDevelopmentDatabase(sqliteDefinition, "", "relative.sqlite", false, true, false)
	if err != nil || database.databasePath != filepath.Join(root, "relative.sqlite") {
		t.Fatalf("SQLite relative flag path = %+v, %v", database, err)
	}

	postgresDefinition := projectfile.File{Root: root, Database: projectfile.DatabasePostgres}
	database, err = resolveDevelopmentDatabase(postgresDefinition, "", "", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if database.databaseURL != defaultDevelopmentDatabaseURL || !database.startPostgres || database.startMongoDB || database.databasePath != "" {
		t.Fatalf("PostgreSQL development database = %+v", database)
	}
	if _, err := resolveDevelopmentDatabase(postgresDefinition, "", "custom.sqlite", false, true, false); err == nil || !strings.Contains(err.Error(), "do not accept --database-path") {
		t.Fatalf("PostgreSQL SQLite flag error = %v", err)
	}

	mongodbDefinition := projectfile.File{Root: root, Database: projectfile.DatabaseMongoDB}
	database, err = resolveDevelopmentDatabase(mongodbDefinition, " mongodb://127.0.0.1:27029/ridu ", "", true, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if database.databaseURL != "mongodb://127.0.0.1:27029/ridu" || database.databasePath != "" || database.startPostgres || database.startMongoDB {
		t.Fatalf("MongoDB development database = %+v", database)
	}
	database, err = resolveDevelopmentDatabase(mongodbDefinition, "", "", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if database.databaseURL != defaultDevelopmentMongoDBURL || !database.startMongoDB || database.startPostgres || database.databasePath != "" {
		t.Fatalf("MongoDB scaffolded development database = %+v", database)
	}
	if _, err := resolveDevelopmentDatabase(mongodbDefinition, "", "", false, false, true); err == nil || !strings.Contains(err.Error(), "when --no-docker is set") {
		t.Fatalf("MongoDB no-Docker missing URL error = %v", err)
	}
	if _, err := resolveDevelopmentDatabase(mongodbDefinition, "mongodb://127.0.0.1:27029/ridu", "custom.sqlite", false, true, false); err == nil || !strings.Contains(err.Error(), "do not accept --database-path") {
		t.Fatalf("MongoDB SQLite flag error = %v", err)
	}
}

func TestSynchronizeDevelopmentSchemaUsesSQLiteMigrateAndReadiness(t *testing.T) {
	ctx := context.Background()
	manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion})
	databasePath := filepath.Join(t.TempDir(), "development.sqlite")
	var output bytes.Buffer
	reporter := newCLIOutput(&output, io.Discard, cliOutputOptions{})
	result, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: manifest}, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if result.schemaSyncDuration <= 0 || !strings.Contains(output.String(), "Synchronized SQLite development schema") {
		t.Fatalf("SQLite synchronization result = %+v, output = %q", result, output.String())
	}
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("synchronized SQLite readiness: %v", err)
	}
}

func TestSynchronizeDevelopmentSchemaHonorsMongoDBNoSync(t *testing.T) {
	ctx := context.Background()
	manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion})
	var output bytes.Buffer
	reporter := newCLIOutput(&output, io.Discard, cliOutputOptions{})
	result, err := synchronizeDevelopmentSchema(
		ctx,
		projectfile.DatabaseMongoDB,
		"mongodb://127.0.0.1:1/never-opened",
		"",
		false,
		true,
		developmentPreparation{manifest: manifest},
		reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.schemaSyncDuration != 0 || output.Len() != 0 {
		t.Fatalf("MongoDB synchronization result = %+v, output = %q", result, output.String())
	}

}

func TestOpenDevelopmentMongoDBHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	const databaseURL = "mongodb://development-user:development-secret@127.0.0.1:1/ridu?directConnection=true&replicaSet=ridu-rs0"
	_, err := openDevelopmentMongoDB(ctx, databaseURL)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("openDevelopmentMongoDB error = %v, want context deadline", err)
	}
}

func TestSynchronizeDevelopmentSchemaMongoDBLivePreparesASeparateServingStore(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("RIDU_MONGODB_URL"))
	if databaseURL == "" {
		t.Skip("set RIDU_MONGODB_URL to run the MongoDB development lifecycle test")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("RIDU_MONGODB_URL is not a valid MongoDB URL")
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	databaseName := "ridu_cli_test_" + hex.EncodeToString(suffix[:])
	parsed.Path = "/" + databaseName
	parsed.RawPath = ""
	selectedURL := parsed.String()

	client, err := mongo.Connect(options.Client().ApplyURI(selectedURL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if !strings.HasPrefix(databaseName, "ridu_cli_test_") {
			t.Errorf("refusing to drop unexpected MongoDB database %q", databaseName)
		} else if err := client.Database(databaseName).Drop(cleanupContext); err != nil {
			t.Errorf("drop isolated MongoDB development database: %v", err)
		}
		if err := client.Disconnect(cleanupContext); err != nil {
			t.Errorf("close MongoDB development cleanup client: %v", err)
		}
	})

	titlePath, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "MongoDB CLI development lifecycle"},
		Collections: []schema.Collection{{
			ID: "cli-development-posts", Slug: "posts",
			Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
			Fields: []schema.Field{{
				ID: "cli-development-posts-title", Name: "title", Path: titlePath,
				Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Index: true,
				Text: &schema.TextField{},
			}},
		}},
		Plugins: []schema.Plugin{},
	})
	preparation := developmentPreparation{manifest: manifest, schemaChanged: true}

	var output bytes.Buffer
	reporter := newCLIOutput(&output, io.Discard, cliOutputOptions{})
	withoutSync, err := synchronizeDevelopmentSchema(
		t.Context(), projectfile.DatabaseMongoDB, selectedURL, "", false, true, preparation, reporter,
	)
	if err != nil || withoutSync.schemaSyncDuration != 0 || output.Len() != 0 {
		t.Fatalf("MongoDB --no-sync preparation = %+v, output %q, error %v", withoutSync, output.String(), err)
	}
	unprepared, err := mongodb.OpenWithConfig(t.Context(), mongodb.Config{
		DatabaseURL: selectedURL, AllowInsecureTransport: true, ApplicationName: "ridu-development-no-sync-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := unprepared.VerifyIndexes(t.Context(), manifest); err == nil {
		_ = unprepared.Close()
		t.Fatal("MongoDB --no-sync unexpectedly prepared the physical index plan")
	}
	if err := unprepared.Close(); err != nil {
		t.Fatal(err)
	}

	output.Reset()
	synchronized, err := synchronizeDevelopmentSchema(
		t.Context(), projectfile.DatabaseMongoDB, selectedURL, "", true, true, preparation, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if synchronized.schemaSyncDuration <= 0 || !strings.Contains(output.String(), "Synchronized MongoDB development indexes") {
		t.Fatalf("MongoDB synchronized preparation = %+v, output %q", synchronized, output.String())
	}
	serving, err := mongodb.OpenWithConfig(t.Context(), mongodb.Config{
		DatabaseURL: selectedURL, AllowInsecureTransport: true, ApplicationName: "ridu-development-serving-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer serving.Close()
	if err := serving.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("separate MongoDB serving Store did not verify the synchronized manifest: %v", err)
	}
}

func TestDoctorDoesNotRequireDockerForSQLite(t *testing.T) {
	root := t.TempDir()
	project := `version = 1
database = "sqlite"
entry = "./cmd/server"
schema = "./generated/ridu.schema.json"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
`
	if err := os.WriteFile(filepath.Join(root, "ridu.toml"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	for _, name := range []string{"go", "bun"} {
		if err := os.WriteFile(filepath.Join(tools, name), []byte("#!/bin/sh\necho test-version\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("RIDU_SQLITE_PATH", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runDoctor(context.Background(), nil, &stdout, &stderr, Options{WorkingDirectory: root}); code != 0 {
		t.Fatalf("doctor exit = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "SQLite") || !strings.Contains(stdout.String(), "Docker is not required") || strings.Contains(stderr.String(), "docker") {
		t.Fatalf("SQLite doctor output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	t.Setenv("RIDU_SQLITE_PATH", ":memory:")
	if code := runDoctor(context.Background(), nil, &stdout, &stderr, Options{WorkingDirectory: root}); code != 1 {
		t.Fatalf("doctor memory exit = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "separate processes") {
		t.Fatalf("SQLite memory doctor output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestDoctorRequiresMongoDBURLWithoutFallingThroughToPostgreSQLDocker(t *testing.T) {
	root := t.TempDir()
	project := `version = 1
database = "mongodb"
entry = "./cmd/server"
schema = "./generated/ridu.schema.json"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
`
	if err := os.WriteFile(filepath.Join(root, "ridu.toml"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	for _, name := range []string{"go", "bun", "docker"} {
		if err := os.WriteFile(filepath.Join(tools, name), []byte("#!/bin/sh\necho test-version\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools)
	t.Setenv("DATABASE_URL", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runDoctor(context.Background(), nil, &stdout, &stderr, Options{WorkingDirectory: root}); code != 1 {
		t.Fatalf("MongoDB doctor without URL exit = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "set DATABASE_URL to a MongoDB replica-set database") {
		t.Fatalf("MongoDB doctor without URL output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	assertNoPostgreSQLDockerDiagnostic(t, stdout.String(), stderr.String())

	stdout.Reset()
	stderr.Reset()
	if err := os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("services:\n  mongodb: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runDoctor(context.Background(), nil, &stdout, &stderr, Options{WorkingDirectory: root}); code != 0 {
		t.Fatalf("MongoDB doctor with scaffolded Compose exit = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "scaffolded development MongoDB replica set") || stderr.Len() != 0 {
		t.Fatalf("MongoDB doctor with scaffolded Compose output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	assertNoPostgreSQLDockerDiagnostic(t, stdout.String(), stderr.String())

	stdout.Reset()
	stderr.Reset()
	t.Setenv("DATABASE_URL", "mongodb://127.0.0.1:27029/ridu")
	if code := runDoctor(context.Background(), nil, &stdout, &stderr, Options{WorkingDirectory: root}); code != 0 {
		t.Fatalf("MongoDB doctor with URL exit = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "MongoDB DATABASE_URL is set") || stderr.Len() != 0 {
		t.Fatalf("MongoDB doctor with URL output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	assertNoPostgreSQLDockerDiagnostic(t, stdout.String(), stderr.String())
}

func TestMongoDBMigrateCreateIsOfflineAndCredentialFree(t *testing.T) {
	root := t.TempDir()
	projectFile := `version = 1
database = "mongodb"
entry = "./cmd/server"
schema = "./generated/ridu.schema.json"
migrations = "./migrations"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
`
	if err := os.WriteFile(filepath.Join(root, "ridu.toml"), []byte(projectFile), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "MongoDB CLI fixture"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{},
	})
	manifestJSON, err := manifest.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	response, err := project.EncodeResponse(project.Response{
		ProtocolVersion: project.ProtocolVersion, FrameworkVersion: "test",
		ManifestVersion: uint32(schema.CurrentVersion), Manifest: manifestJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	argumentsPath := filepath.Join(t.TempDir(), "project-arguments")
	const fakeGo = `#!/bin/sh
printf '%s\n' "$@" > "$RIDU_FAKE_PROJECT_ARGUMENTS"
printf '%s' "$RIDU_FAKE_PROJECT_RESPONSE"
`
	if err := os.WriteFile(filepath.Join(tools, "go"), []byte(fakeGo), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	t.Setenv("RIDU_FAKE_PROJECT_ARGUMENTS", argumentsPath)
	t.Setenv("RIDU_FAKE_PROJECT_RESPONSE", string(response))
	const databaseURL = "mongodb://offline-user:offline-secret@127.0.0.1:1/ridu"
	t.Setenv("DATABASE_URL", databaseURL)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run(context.Background(), []string{"migrate", "status", "--allow-insecure-database"}, &stdout, &stderr, Options{WorkingDirectory: root, FrameworkVersion: "test"}); code != 1 {
		t.Fatalf("MongoDB migrate status without history exit = %d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "migration artifact history is empty") ||
		strings.Contains(stdout.String(), "offline-user") || strings.Contains(stdout.String(), "offline-secret") ||
		strings.Contains(stderr.String(), "offline-user") || strings.Contains(stderr.String(), "offline-secret") {
		t.Fatalf("MongoDB migrate status did not fail before connection or redaction: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"migrate", "create", "--name", "initial"}, &stdout, &stderr, Options{WorkingDirectory: root, FrameworkVersion: "test"}); code != 0 {
		t.Fatalf("MongoDB migrate create exit = %d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	arguments, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(filepath.Join(root, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Artifact.Planner.Name != "mongodb" || files[0].Artifact.Planner.Version != "2.0.0" {
		t.Fatalf("MongoDB CLI artifacts = %#v", files)
	}
	artifact, err := os.ReadFile(files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	for label, value := range map[string]string{
		"project argv": string(arguments), "stdout": stdout.String(), "stderr": stderr.String(), "artifact": string(artifact),
	} {
		if strings.Contains(value, databaseURL) || strings.Contains(value, "offline-user") || strings.Contains(value, "offline-secret") {
			t.Fatalf("MongoDB %s leaked database credentials: %q", label, value)
		}
	}
	if strings.Contains(string(arguments), "--database-url") || !strings.Contains(string(arguments), "__ridu\nmanifest\n") {
		t.Fatalf("MongoDB manifest handshake arguments = %q", arguments)
	}
	if stderr.Len() != 0 || !strings.Contains(stdout.String(), "Created migration migrations/") || !strings.Contains(stdout.String(), "review and commit it before running ridu migrate up") {
		t.Fatalf("MongoDB migrate create output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"migrate", "status", "--database-url", "mongodb://", "--allow-insecure-database"}, &stdout, &stderr, Options{WorkingDirectory: root, FrameworkVersion: "test"}); code != 1 {
		t.Fatalf("MongoDB migrate status with current history exit = %d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "open MongoDB: invalid MongoDB connection configuration") ||
		strings.Contains(stderr.String(), "offline migrate create") || strings.Contains(stderr.String(), "offline-secret") {
		t.Fatalf("MongoDB migrate status did not reach live dispatch safely: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestMongoDBMigrateCreateOmitsDatabaseEnvironmentFromFailingProject(t *testing.T) {
	root := t.TempDir()
	project := `version = 1
database = "mongodb"
entry = "./cmd/server"
schema = "./generated/ridu.schema.json"
migrations = "./migrations"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
`
	if err := os.WriteFile(filepath.Join(root, "ridu.toml"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	const fakeGo = `#!/bin/sh
printf 'DATABASE_URL=%s\nRIDU_SQLITE_PATH=%s\n' "$DATABASE_URL" "$RIDU_SQLITE_PATH" > "$RIDU_TEST_FAKE_GO_MARKER"
exit 23
`
	if err := os.WriteFile(filepath.Join(tools, "go"), []byte(fakeGo), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	marker := filepath.Join(t.TempDir(), "fake-go-environment")
	t.Setenv("RIDU_TEST_FAKE_GO_MARKER", marker)
	const databaseURL = "mongodb://failure-user:failure-secret@127.0.0.1:1/ridu"
	const databasePath = "/private/failure-secret.sqlite"
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv("RIDU_SQLITE_PATH", databasePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run(context.Background(), []string{"migrate", "create", "--name", "initial"}, &stdout, &stderr, Options{WorkingDirectory: root, FrameworkVersion: "test"}); code != 1 {
		t.Fatalf("failing MongoDB project exit = %d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "project manifest command failed") ||
		strings.Contains(stderr.String(), databaseURL) || strings.Contains(stderr.String(), "failure-user") ||
		strings.Contains(stderr.String(), "failure-secret") || strings.Contains(stderr.String(), databasePath) {
		t.Fatalf("failing MongoDB project diagnostic = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("failing MongoDB project wrote stdout: %q", stdout.String())
	}
	environment, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("fake go command did not run: %v", err)
	}
	if got, want := string(environment), "DATABASE_URL=\nRIDU_SQLITE_PATH=\n"; got != want {
		t.Fatalf("database environment reached the project command: got %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "migrations")); !os.IsNotExist(err) {
		t.Fatalf("failing MongoDB project changed the migration directory: %v", err)
	}
}

func TestMongoDBMigrateCreateRejectsLiveDatabaseSelectorsBeforeProjectResolution(t *testing.T) {
	root := t.TempDir()
	project := `version = 1
database = "mongodb"
entry = "./cmd/server"
schema = "./generated/ridu.schema.json"
migrations = "./migrations"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
`
	if err := os.WriteFile(filepath.Join(root, "ridu.toml"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "database URL", args: []string{"migrate", "create", "--name", "initial", "--database-url", "mongodb://explicit-user:explicit-secret@127.0.0.1:1/ridu"}, want: "offline"},
		{name: "database path", args: []string{"migrate", "create", "--name", "initial", "--database-path", "/tmp/private.sqlite"}, want: "offline"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if code := Run(context.Background(), test.args, &stdout, &stderr, Options{WorkingDirectory: root, FrameworkVersion: "test"}); code != 2 {
				t.Fatalf("MongoDB rejected create exit = %d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) || strings.Contains(stderr.String(), "explicit-user") || strings.Contains(stderr.String(), "explicit-secret") {
				t.Fatalf("MongoDB rejected create output: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("MongoDB rejected create wrote stdout: %q", stdout.String())
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, "migrations")); !os.IsNotExist(err) {
		t.Fatalf("rejected MongoDB create changed the migration directory: %v", err)
	}
}

func TestMongoDBLiveMigrationCommandsRequireURLBeforeProjectResolution(t *testing.T) {
	root := t.TempDir()
	project := `version = 1
database = "mongodb"
entry = "./cmd/server"
schema = "./generated/ridu.schema.json"
migrations = "./migrations"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
`
	if err := os.WriteFile(filepath.Join(root, "ridu.toml"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "")
	for _, command := range []string{"plan", "status", "up", "verify"} {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if code := Run(context.Background(), []string{"migrate", command}, &stdout, &stderr, Options{WorkingDirectory: root}); code != 2 {
				t.Fatalf("MongoDB migrate %s exit = %d; stdout=%q stderr=%q", command, code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "MongoDB URL is required through --database-url or DATABASE_URL") || stdout.Len() != 0 {
				t.Fatalf("MongoDB migrate %s missing-URL output: stdout=%q stderr=%q", command, stdout.String(), stderr.String())
			}
		})
	}
}

func TestMongoDBDestructiveMigrationLifecycleFailsClosedBeforeDispatch(t *testing.T) {
	root := t.TempDir()
	project := `version = 1
database = "mongodb"
entry = "./cmd/server"
schema = "./generated/ridu.schema.json"
migrations = "./migrations"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
`
	if err := os.WriteFile(filepath.Join(root, "ridu.toml"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "mongodb://offline-user:offline-secret@127.0.0.1:1/ridu")
	for _, command := range []string{"down", "reset", "refresh", "fresh"} {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if code := Run(context.Background(), []string{"migrate", command}, &stdout, &stderr, Options{WorkingDirectory: root}); code != 2 {
				t.Fatalf("MongoDB migrate exit = %d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "not implemented for MongoDB immutable artifacts") || strings.Contains(stderr.String(), "PostgreSQL URL") || strings.Contains(stderr.String(), "offline-secret") {
				t.Fatalf("MongoDB migrate output: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("MongoDB rejected migration wrote stdout: %q", stdout.String())
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, "migrations")); !os.IsNotExist(err) {
		t.Fatalf("rejected MongoDB lifecycle changed the migration directory: %v", err)
	}
}

func assertNoPostgreSQLDockerDiagnostic(t *testing.T, stdout, stderr string) {
	t.Helper()
	combined := stdout + stderr
	for _, forbidden := range []string{"development PostgreSQL service", "install Docker/OrbStack"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("MongoDB doctor emitted PostgreSQL Docker diagnostic %q: stdout=%q stderr=%q", forbidden, stdout, stderr)
		}
	}
}
