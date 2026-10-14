// Command ridu is the Ridu side of the Ridu/PocketBase SQLite benchmark in ../run.ts. Its posts
// collection and access rules mirror ../server: anonymous readers see published posts, any
// signed-in user sees and writes every post. It links only the SQLite adapter with its defaults.
//
//	ridu migrations <directory>  write the initial migration artifact and print its history digest
//	ridu migrate <directory>     apply the artifact history to RIDU_PB_BENCH_SQLITE_PATH
//	ridu                         serve; RIDU_PB_BENCH_HISTORY_DIGEST selects production startup
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/query"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return serve(ctx)
	}
	if len(args) != 2 {
		return errors.New("usage: ridu [migrations|migrate <directory>]")
	}
	switch args[0] {
	case "migrations":
		digest, err := writeMigrations(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Println(digest)
		return nil
	case "migrate":
		backend, err := openStore(ctx)
		if err != nil {
			return err
		}
		defer backend.Close()
		return backend.ApplyArtifacts(ctx, args[1])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func config() ridu.Config {
	return ridu.Config{
		Name:  "Ridu PocketBase benchmark",
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{
				Slug:   "users",
				Auth:   true,
				Admin:  ridu.CollectionAdmin{UseAsTitle: "email"},
				Fields: field.Fields{field.Email("email").Required().Unique()},
				Access: ridu.CollectionAccess{
					Admin:  allowAuthenticated,
					Read:   allowAuthenticated,
					Update: allowAuthenticated,
					Delete: allowAuthenticated,
				},
			},
			{
				Slug:  "posts",
				Admin: ridu.CollectionAdmin{UseAsTitle: "title"},
				Fields: field.Fields{
					field.Text("title").Required(),
					field.Text("summary"),
					// Mirrors PocketBase's (status, id) index: the anonymous filter and id order.
					field.Select("status", "draft", "published").Required().Index(),
					field.Number("readingMinutes"),
					field.Checkbox("featured"),
				},
				Access: ridu.CollectionAccess{
					Create: allowAuthenticated,
					Read:   readPublishedOrAuthenticated,
					Update: allowAuthenticated,
					Delete: allowAuthenticated,
				},
			},
		},
	}
}

func readPublishedOrAuthenticated(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor == nil {
		return ridu.Where(query.Equal("status", "published")), nil
	}
	return ridu.Allow(), nil
}

func allowAuthenticated(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor == nil {
		return ridu.Deny(), nil
	}
	return ridu.Allow(), nil
}

func openStore(ctx context.Context) (*sqlite.Store, error) {
	path := os.Getenv("RIDU_PB_BENCH_SQLITE_PATH")
	if path == "" {
		return nil, errors.New("RIDU_PB_BENCH_SQLITE_PATH is required")
	}
	return sqlite.OpenWithConfig(ctx, sqlite.Config{Path: path})
}

func writeMigrations(ctx context.Context, directory string) (string, error) {
	manifest, err := ridu.Resolve(config())
	if err != nil {
		return "", err
	}
	if _, err := sqlite.CreateArtifact(ctx, directory, "pocketbase-initial", manifest, time.Unix(1, 0), sqlite.ArtifactOptions{}); err != nil {
		return "", err
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return "", err
	}
	return migrationartifact.HistoryDigest(files)
}

func serve(parent context.Context) error {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	address := os.Getenv("RIDU_PB_BENCH_ADDRESS")
	if address == "" {
		address = "127.0.0.1:18281"
	}
	backend, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer backend.Close()
	application, err := ridu.New(config(), backend)
	if err != nil {
		return err
	}
	// Like ridu.Execute's production preflight: verify the separately migrated database
	// against the executable migration history before serving.
	if digest := os.Getenv("RIDU_PB_BENCH_HISTORY_DIGEST"); digest != "" {
		if err := backend.ReadyWithMigrationHistory(ctx, application.Manifest(), digest); err != nil {
			return fmt.Errorf("production readiness preflight: %w", err)
		}
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: application.Handler(ridu.HandlerOptions{}), ReadHeaderTimeout: 10 * time.Second}
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
	case err := <-serveError:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}
