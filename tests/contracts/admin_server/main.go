// Command admin_server is a deterministic browser-verification fixture for the embedded admin.
package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"html"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	sqliteadapter "github.com/riducms/ridu/adapters/sqlite"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

const resetTokenHeader = "X-Ridu-Test-Reset-Token"

var postgresFixtureSchemaPattern = regexp.MustCompile(`^ridu_admin_fixture_[a-z0-9_]+$`)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	address := os.Getenv("RIDU_BROWSER_ADDRESS")
	if address == "" {
		address = "127.0.0.1:18081"
	}
	previewAddress := os.Getenv("RIDU_BROWSER_PREVIEW_ADDRESS")
	if previewAddress == "" {
		previewAddress = "127.0.0.1:18082"
	}
	adminListener, previewListener, err := listenFixtureServers(address, previewAddress)
	if err != nil {
		return err
	}
	defer adminListener.Close()
	defer previewListener.Close()
	address, previewAddress = adminListener.Addr().String(), previewListener.Addr().String()
	// Worker fixtures bind port zero. Resolve the preview origin before building
	// the config so preview tokens and links use this worker's actual listener.
	if err := os.Setenv("RIDU_BROWSER_PREVIEW_ADDRESS", previewAddress); err != nil {
		return err
	}
	uploadRoot, err := os.MkdirTemp("", "ridu-admin-uploads-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(uploadRoot)
	uploadStorage, err := localstorage.New(uploadRoot)
	if err != nil {
		return err
	}
	bootstrapFixture := os.Getenv("RIDU_BROWSER_BOOTSTRAP") == "true"
	config := fixtureConfig(uploadStorage)
	if bootstrapFixture {
		config = bootstrapFixtureConfig()
	}
	resetToken := os.Getenv("RIDU_BROWSER_RESET_TOKEN")
	if !bootstrapFixture && resetToken != "" {
		if err := requireLoopbackListener(address); err != nil {
			return err
		}
	}
	var adminAssets fs.FS
	if directory := os.Getenv("RIDU_BROWSER_ADMIN_DIR"); directory != "" {
		adminAssets = os.DirFS(directory)
	}
	baseDatabaseURL := os.Getenv("RIDU_POSTGRES_URL")
	mongoDatabaseURL := os.Getenv("RIDU_MONGODB_URL")
	sqliteFixture := os.Getenv("RIDU_SQLITE_FIXTURE") == "true"
	if (baseDatabaseURL != "" && (sqliteFixture || mongoDatabaseURL != "")) || (sqliteFixture && mongoDatabaseURL != "") {
		return fmt.Errorf("select only one of RIDU_POSTGRES_URL, RIDU_SQLITE_FIXTURE and RIDU_MONGODB_URL")
	}
	if mongoDatabaseURL != "" {
		mongoDatabaseURL, err = mongoFixtureURL(mongoDatabaseURL)
		if err != nil {
			return err
		}
		defer func() {
			cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if cleanupErr := resetMongoFixture(cleanupContext, mongoDatabaseURL); cleanupErr != nil {
				log.Printf("clean up MongoDB fixture: %v", cleanupErr)
			}
		}()
	}
	sqlitePath := ""
	if sqliteFixture {
		sqliteDirectory, sqliteErr := os.MkdirTemp("", "ridu-admin-sqlite-")
		if sqliteErr != nil {
			return sqliteErr
		}
		defer os.RemoveAll(sqliteDirectory)
		sqlitePath = filepath.Join(sqliteDirectory, "admin.sqlite")
	}
	databaseURL := baseDatabaseURL
	fixtureSchema := ""
	if baseDatabaseURL != "" {
		var schemaErr error
		fixtureSchema, schemaErr = postgresFixtureSchema()
		if schemaErr != nil {
			return schemaErr
		}
		releaseLock, lockErr := acquirePostgresFixtureLock(ctx, baseDatabaseURL, fixtureSchema)
		if lockErr != nil {
			return lockErr
		}
		defer releaseLock()
		if resetErr := resetPostgresFixture(ctx, baseDatabaseURL, fixtureSchema); resetErr != nil {
			return resetErr
		}
		defer func() {
			cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if cleanupErr := dropPostgresFixture(cleanupContext, baseDatabaseURL, fixtureSchema); cleanupErr != nil {
				log.Printf("clean up PostgreSQL fixture: %v", cleanupErr)
			}
		}()
		databaseURL, err = postgresFixtureURL(baseDatabaseURL, fixtureSchema)
		if err != nil {
			return err
		}
	}
	applicationHandler, closeBackend, err := fixtureApplicationHandler(ctx, config, bootstrapFixture, adminAssets, databaseURL, sqlitePath, mongoDatabaseURL)
	if err != nil {
		return err
	}
	var activeMu sync.RWMutex
	activeHandler := applicationHandler
	activeClose := closeBackend
	defer func() {
		activeMu.Lock()
		defer activeMu.Unlock()
		activeClose()
	}()
	mux := http.NewServeMux()
	if !bootstrapFixture && resetToken != "" {
		mux.HandleFunc("POST /__ridu-test/reset", func(response http.ResponseWriter, request *http.Request) {
			providedToken := request.Header.Get(resetTokenHeader)
			if !resetTokenMatches(providedToken, resetToken) {
				http.Error(response, "fixture reset authentication failed", http.StatusUnauthorized)
				return
			}
			activeMu.Lock()
			defer activeMu.Unlock()
			activeClose()
			activeClose = func() {}
			if resetErr := resetFixtureUploads(uploadRoot); resetErr != nil {
				http.Error(response, resetErr.Error(), http.StatusInternalServerError)
				return
			}
			if baseDatabaseURL != "" {
				if resetErr := resetPostgresFixture(request.Context(), baseDatabaseURL, fixtureSchema); resetErr != nil {
					http.Error(response, resetErr.Error(), http.StatusInternalServerError)
					return
				}
			}
			if sqlitePath != "" {
				if resetErr := resetSQLiteFixture(sqlitePath); resetErr != nil {
					http.Error(response, resetErr.Error(), http.StatusInternalServerError)
					return
				}
			}
			if mongoDatabaseURL != "" {
				if resetErr := resetMongoFixture(request.Context(), mongoDatabaseURL); resetErr != nil {
					http.Error(response, resetErr.Error(), http.StatusInternalServerError)
					return
				}
			}
			// The replacement handler outlives this reset request. Build it with the
			// server context so pooled database work is not tied to a cancelled
			// request context after the 204 response is sent.
			replacement, replacementClose, resetErr := fixtureApplicationHandler(ctx, config, false, adminAssets, databaseURL, sqlitePath, mongoDatabaseURL)
			if resetErr != nil {
				http.Error(response, resetErr.Error(), http.StatusInternalServerError)
				return
			}
			activeHandler = replacement
			activeClose = replacementClose
			response.WriteHeader(http.StatusNoContent)
		})
	}
	mux.HandleFunc("/", func(response http.ResponseWriter, request *http.Request) {
		activeMu.RLock()
		defer activeMu.RUnlock()
		activeHandler.ServeHTTP(response, request)
	})
	previewMux := http.NewServeMux()
	previewMux.HandleFunc("GET /preview/posts/{id}", func(response http.ResponseWriter, request *http.Request) {
		livePreview(response, request, "http://"+address, "posts")
	})
	previewMux.HandleFunc("GET /preview/block-articles/{id}", func(response http.ResponseWriter, request *http.Request) {
		livePreview(response, request, "http://"+address, "block-articles")
	})
	adminServer := &http.Server{Handler: mux}
	previewServer := &http.Server{Handler: previewMux}
	serveErrors := make(chan error, 2)
	fmt.Printf("admin fixture listening at http://%s/admin/login\n", address)
	if bootstrapFixture {
		fmt.Println("fixture admin collection is empty; create the first account in the admin")
	} else {
		fmt.Println("fixture accounts: admin@riducms.test / ridu-admin; editor@riducms.test / ridu-browser; demo@riducms.local / ridu-demo; api@riducms.test / ridu-api (admin denied)")
	}
	fmt.Printf("preview fixture listening at http://%s\n", previewAddress)
	go func() {
		serveErrors <- serverError("preview fixture", previewServer.Serve(previewListener))
	}()
	go func() {
		serveErrors <- serverError("admin fixture", adminServer.Serve(adminListener))
	}()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serveErrors:
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	shutdownErr := errors.Join(adminServer.Shutdown(shutdownContext), previewServer.Shutdown(shutdownContext))
	if serveErr != nil || shutdownErr != nil {
		return errors.Join(serveErr, shutdownErr)
	}
	return nil
}

func serverError(name string, err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("%s stopped: %w", name, err)
}

func listenFixtureServers(adminAddress, previewAddress string) (net.Listener, net.Listener, error) {
	adminListener, err := net.Listen("tcp", adminAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("listen for admin fixture on %s: %w", adminAddress, err)
	}
	previewListener, err := net.Listen("tcp", previewAddress)
	if err != nil {
		_ = adminListener.Close()
		return nil, nil, fmt.Errorf("listen for preview fixture on %s: %w", previewAddress, err)
	}
	return adminListener, previewListener, nil
}

func resetTokenMatches(provided, expected string) bool {
	return expected != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func fixtureApplicationHandler(ctx context.Context, config ridu.Config, bootstrapFixture bool, adminAssets fs.FS, databaseURL, sqlitePath, mongoDatabaseURL string) (http.Handler, func(), error) {
	backend, closeBackend, err := fixtureBackend(ctx, config, databaseURL, sqlitePath, mongoDatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		closeBackend()
		return nil, nil, err
	}
	if !bootstrapFixture {
		if _, err := seedFixture(ctx, application); err != nil {
			closeBackend()
			return nil, nil, err
		}
	}
	options := ridu.HandlerOptions{
		AllowedOrigins: []string{"http://127.0.0.1:5173"},
		AdminAssets:    adminAssets,
		// Browser cases intentionally authenticate for each flow. Keep the fixture
		// above that suite traffic while core tests exercise the production boundary.
		AuthRateLimit: 100,
		RequestError: func(event ridu.RequestErrorEvent) {
			chain := make([]string, 0, 4)
			for current := event.Error; current != nil; current = errors.Unwrap(current) {
				chain = append(chain, current.Error())
			}
			log.Printf("fixture request failed: method=%s path=%s request_id=%s error_chain=%q", event.Method, event.Path, event.RequestID, chain)
		},
	}
	return blockSchemaRecoveryFixture(application.Handler(options), config, backend, options), closeBackend, nil
}

func fixtureBackend(ctx context.Context, config ridu.Config, databaseURL, sqlitePath, mongoDatabaseURL string) (store.Store, func(), error) {
	if mongoDatabaseURL != "" {
		backend, err := mongodb.OpenWithConfig(ctx, mongodb.Config{DatabaseURL: mongoDatabaseURL, AllowInsecureTransport: true})
		if err != nil {
			return nil, nil, err
		}
		manifest, err := ridu.Resolve(config)
		if err != nil {
			_ = backend.Close()
			return nil, nil, err
		}
		if err := backend.SyncIndexes(ctx, manifest); err != nil {
			_ = backend.Close()
			return nil, nil, err
		}
		return backend, func() { _ = backend.Close() }, nil
	}
	if databaseURL == "" && sqlitePath == "" {
		return teststore.New(), func() {}, nil
	}
	if sqlitePath != "" {
		backend, err := sqliteadapter.Open(ctx, sqlitePath)
		if err != nil {
			return nil, nil, err
		}
		manifest, err := ridu.Resolve(config)
		if err != nil {
			_ = backend.Close()
			return nil, nil, err
		}
		if err := backend.Migrate(ctx, manifest); err != nil {
			_ = backend.Close()
			return nil, nil, err
		}
		return backend, func() { _ = backend.Close() }, nil
	}
	backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: databaseURL, AllowInsecureTransport: true})
	if err != nil {
		return nil, nil, err
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		backend.Close()
		return nil, nil, err
	}
	artifact, err := postgres.BuildArtifact(ctx, "browser-fixture-initial", nil, manifest, nil, false)
	if err != nil {
		backend.Close()
		return nil, nil, err
	}
	directory, err := os.MkdirTemp("", "ridu-browser-migrations-")
	if err != nil {
		backend.Close()
		return nil, nil, err
	}
	defer os.RemoveAll(directory)
	if _, err := migrationartifact.Create(directory, "browser-fixture-initial", artifact, time.Unix(1, 0)); err != nil {
		backend.Close()
		return nil, nil, err
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		backend.Close()
		return nil, nil, err
	}
	return backend, backend.Close, nil
}

func resetSQLiteFixture(databasePath string) error {
	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove SQLite fixture file %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

func postgresFixtureSchema() (string, error) {
	schemaName := os.Getenv("RIDU_POSTGRES_FIXTURE_SCHEMA")
	if schemaName == "" {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return "", fmt.Errorf("generate PostgreSQL fixture schema: %w", err)
		}
		schemaName = fmt.Sprintf("ridu_admin_fixture_%x", suffix)
	}
	if !postgresFixtureSchemaPattern.MatchString(schemaName) {
		return "", fmt.Errorf("RIDU_POSTGRES_FIXTURE_SCHEMA %q must start with ridu_admin_fixture_ and contain only lowercase letters, digits, and underscores", schemaName)
	}
	return schemaName, nil
}

func requireLoopbackListener(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse RIDU_BROWSER_ADDRESS for reset route: %w", err)
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	addressIP := net.ParseIP(host)
	if addressIP == nil || !addressIP.IsLoopback() {
		return fmt.Errorf("RIDU_BROWSER_RESET_TOKEN requires RIDU_BROWSER_ADDRESS to use a loopback host")
	}
	return nil
}

func resetFixtureUploads(root string) error {
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("clear fixture upload storage: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("recreate fixture upload storage: %w", err)
	}
	return nil
}

func postgresFixtureURL(databaseURL, schemaName string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("parse RIDU_POSTGRES_URL: %w", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func resetPostgresFixture(ctx context.Context, databaseURL, schemaName string) error {
	if !postgresFixtureSchemaPattern.MatchString(schemaName) {
		return fmt.Errorf("refusing to reset non-fixture PostgreSQL schema %q", schemaName)
	}
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL fixture database: %w", err)
	}
	defer connection.Close(context.Background())
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := connection.Exec(ctx, "DROP SCHEMA IF EXISTS "+identifier+" CASCADE"); err != nil {
		return fmt.Errorf("drop PostgreSQL fixture schema: %w", err)
	}
	if _, err := connection.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		return fmt.Errorf("create PostgreSQL fixture schema: %w", err)
	}
	return nil
}

func dropPostgresFixture(ctx context.Context, databaseURL, schemaName string) error {
	if !postgresFixtureSchemaPattern.MatchString(schemaName) {
		return fmt.Errorf("refusing to drop non-fixture PostgreSQL schema %q", schemaName)
	}
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL fixture database for cleanup: %w", err)
	}
	defer connection.Close(context.Background())
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := connection.Exec(ctx, "DROP SCHEMA IF EXISTS "+identifier+" CASCADE"); err != nil {
		return fmt.Errorf("drop PostgreSQL fixture schema during cleanup: %w", err)
	}
	return nil
}

func acquirePostgresFixtureLock(ctx context.Context, databaseURL, schemaName string) (func(), error) {
	if !postgresFixtureSchemaPattern.MatchString(schemaName) {
		return nil, fmt.Errorf("refusing to lock non-fixture PostgreSQL schema %q", schemaName)
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("ridu-admin-fixture:" + schemaName))
	lockKey := int64(hasher.Sum64())
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL fixture database for ownership lock: %w", err)
	}
	var acquired bool
	if err := connection.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&acquired); err != nil {
		_ = connection.Close(context.Background())
		return nil, fmt.Errorf("acquire PostgreSQL fixture ownership lock: %w", err)
	}
	if !acquired {
		_ = connection.Close(context.Background())
		return nil, fmt.Errorf("PostgreSQL fixture schema %q is already owned by another process", schemaName)
	}
	return func() {
		releaseContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := connection.Exec(releaseContext, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			log.Printf("release PostgreSQL fixture ownership lock: %v", err)
		}
		if err := connection.Close(context.Background()); err != nil {
			log.Printf("close PostgreSQL fixture ownership connection: %v", err)
		}
	}, nil
}

func livePreview(response http.ResponseWriter, request *http.Request, adminOrigin, collection string) {
	title, summary, blocks := serverRenderedPreview(request, adminOrigin, collection)
	blocksHidden := ""
	if blocks == "" {
		blocksHidden = " hidden"
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(response, `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Ridu live preview</title><style>
*{box-sizing:border-box}body{margin:0;min-height:100vh;background:#f4f0e8;color:#181714;font-family:ui-sans-serif,system-ui,sans-serif}main{padding:clamp(2rem,8vw,8rem)}.kicker{font-size:12px;letter-spacing:.16em;text-transform:uppercase}h1{font-family:Georgia,serif;font-size:clamp(3rem,9vw,8rem);line-height:1;margin:1rem 0}p{font-size:clamp(1rem,2vw,1.5rem);line-height:1.6;max-width:760px}
</style></head><body><main><div class="kicker">Ridu comparison preview</div><h1 id="title">`+html.EscapeString(title)+`</h1><p id="summary">`+html.EscapeString(summary)+`</p><pre data-preview-blocks`+blocksHidden+`>`+html.EscapeString(blocks)+`</pre></main>
<script>const adminOrigin=`+strconv.Quote(adminOrigin)+`;
const channel=new URL(location.href).searchParams.get('__ridu_preview');
const target=window.opener||window.parent;
function ready(){if(channel&&target!==window)target.postMessage({type:'ridu-live-preview',ready:true,channel},adminOrigin)}
addEventListener('message',(event)=>{if(event.origin!==adminOrigin||event.source!==target||!event.data||event.data.type!=='ridu-live-preview'||event.data.channel!==channel||!Number.isSafeInteger(event.data.sequence)||typeof event.data.data!=='object')return;const data=event.data.data;document.querySelector('#title').textContent=typeof data.title==='string'&&data.title?data.title:'Live preview';document.querySelector('#summary').textContent=typeof data.summary==='string'&&data.summary?data.summary:'Edit the Ridu form to stream the current draft into this viewport.';const blocks=document.querySelector('[data-preview-blocks]');blocks.textContent=data.body?JSON.stringify(data.body,null,2):'';blocks.hidden=!data.body});
addEventListener('pageshow',ready);addEventListener('focus',ready);addEventListener('online',ready);ready();
</script></body></html>`)
}

func serverRenderedPreview(request *http.Request, adminOrigin, collection string) (string, string, string) {
	const fallbackTitle = "Live preview"
	const fallbackSummary = "Edit the Ridu form to stream the current draft into this viewport."
	token := request.URL.Query().Get("__ridu_preview_token")
	documentID := request.PathValue("id")
	if token == "" || documentID == "" {
		return fallbackTitle, fallbackSummary, ""
	}
	previewRequest, err := http.NewRequestWithContext(
		request.Context(),
		http.MethodGet,
		adminOrigin+"/api/preview/collections/"+url.PathEscape(collection)+"/"+url.PathEscape(documentID),
		nil,
	)
	if err != nil {
		return fallbackTitle, fallbackSummary, ""
	}
	previewRequest.Header.Set("Authorization", "Bearer "+token)
	previewResponse, err := http.DefaultClient.Do(previewRequest)
	if err != nil {
		return fallbackTitle, fallbackSummary, ""
	}
	defer previewResponse.Body.Close()
	if previewResponse.StatusCode != http.StatusOK {
		return fallbackTitle, fallbackSummary, ""
	}
	var envelope struct {
		Doc struct {
			Title   string          `json:"title"`
			Summary string          `json:"summary"`
			Body    json.RawMessage `json:"body"`
		} `json:"doc"`
	}
	if err := json.NewDecoder(previewResponse.Body).Decode(&envelope); err != nil {
		return fallbackTitle, fallbackSummary, ""
	}
	if envelope.Doc.Title == "" {
		envelope.Doc.Title = fallbackTitle
	}
	if envelope.Doc.Summary == "" {
		envelope.Doc.Summary = fallbackSummary
	}
	return envelope.Doc.Title, envelope.Doc.Summary, string(envelope.Doc.Body)
}
