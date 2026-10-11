package core

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
)

// migrateCommand is the argument that makes a built binary apply its
// migrations instead of serving: `<binary> migrate up`.
const migrateCommand = "migrate"

// sqlitePlanner is the planner the SQLite adapter records in every artifact.
// The other adapters select a network database by URL.
const sqlitePlanner = "ridu-sqlite"

// runMigrateCommand applies the committed migration history from a binary made
// by ridu build, so a production host needs neither the Ridu CLI, Go nor a
// JavaScript runtime to migrate. It applies only the history the binary was
// built with: the migrations directory must match the build's history digest,
// which the server's readiness check also requires.
func runMigrateCommand(ctx context.Context, args []string, stdout, stderr io.Writer, applicationConfig Config, driver migration.ProjectDriver, historyDigest string) error {
	if len(args) == 0 || args[0] != "up" {
		return errors.New("usage: migrate up [flags]; run migrate up -h for the flags")
	}
	flags := flag.NewFlagSet("migrate up", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("directory", "migrations", "migration artifacts from the commit this binary was built from")
	databaseURL := flags.String("database-url", "", "PostgreSQL or MongoDB URL (defaults to DATABASE_URL)")
	databasePath := flags.String("database-path", "", "absolute SQLite path or file URI (defaults to RIDU_SQLITE_PATH)")
	allowInsecureDatabase := flags.Bool("allow-insecure-database", false, "allow plaintext PostgreSQL or MongoDB transport to a remote host (defaults to RIDU_ALLOW_INSECURE_DATABASE); loopback and Unix sockets need no TLS")
	allowMaintenance := flags.Bool("allow-maintenance", false, "admit traffic-sensitive steps; stop every application process and worker first")
	allowUnbounded := flags.Bool("allow-unbounded", false, "explicitly admit zero migration timeouts")
	advisoryLockWait := flags.Duration("advisory-lock-wait", 0, "maximum wait for the adapter migration lock")
	lockTimeout := flags.Duration("lock-timeout", 0, "maximum PostgreSQL lock wait per phase")
	statementTimeout := flags.Duration("statement-timeout", 0, "maximum transactional statement duration")
	batchTimeout := flags.Duration("batch-timeout", 0, "maximum duration per checkpoint batch")
	concurrentIndexTimeout := flags.Duration("concurrent-index-timeout", 0, "maximum PostgreSQL concurrent-index operation or complete MongoDB migration duration")
	idleTransactionTimeout := flags.Duration("idle-in-transaction-timeout", 0, "maximum idle time in a transaction phase")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("migrate up does not accept positional arguments")
	}
	explicit := map[string]bool{}
	flags.Visit(func(set *flag.Flag) { explicit[set.Name] = true })
	if historyDigest == "" {
		return errors.New("migrate up runs only from a binary built with ridu build, which records the migration history it applies; in development, run ridu migrate up")
	}
	if driver == nil {
		return errors.New("this binary has no migration driver; register one in cmd/server/main.go with ridu.WithProjectMigrations, such as postgres.ProjectMigrations()")
	}
	if err := driver.Validate(); err != nil {
		return fmt.Errorf("validate project migration driver: %w", err)
	}
	if info, err := os.Stat(*directory); err != nil || !info.IsDir() {
		return fmt.Errorf("no migrations directory at %s; deploy the project's committed migrations with the binary, or pass --directory", *directory)
	}
	manifest, err := Resolve(applicationConfig)
	if err != nil {
		return err
	}
	files, err := migrationartifact.RequireCurrentHistory(*directory, manifest)
	if err != nil {
		return fmt.Errorf("migrations in %s: %w", *directory, err)
	}
	if digest, err := migrationartifact.HistoryDigest(files); err != nil {
		return fmt.Errorf("migrations in %s: %w", *directory, err)
	} else if digest != historyDigest {
		return fmt.Errorf("the migrations in %s are not the history this binary was built with; deploy them from the same commit as the binary", *directory)
	}

	request := migration.ProjectRequest{Action: migration.ProjectApply, Directory: *directory}
	// Select the database the way ridu migrate up and the generated server do.
	if files[len(files)-1].Artifact.Planner.Name == sqlitePlanner {
		for _, name := range []string{"database-url", "allow-insecure-database", "allow-maintenance", "allow-unbounded", "advisory-lock-wait", "lock-timeout", "statement-timeout", "batch-timeout", "concurrent-index-timeout", "idle-in-transaction-timeout"} {
			if explicit[name] {
				return fmt.Errorf("SQLite migrations do not accept --%s", name)
			}
		}
		request.DatabasePath = strings.TrimSpace(*databasePath)
		if !explicit["database-path"] {
			request.DatabasePath = strings.TrimSpace(os.Getenv("RIDU_SQLITE_PATH"))
		}
		if !sqlitePathAbsolute(request.DatabasePath) {
			return errors.New("the SQLite database path must be an absolute file path or file URI, as the server requires")
		}
	} else {
		if explicit["database-path"] {
			return errors.New("PostgreSQL and MongoDB migrations do not accept --database-path")
		}
		request.DatabaseURL = *databaseURL
		if !explicit["database-url"] {
			request.DatabaseURL = os.Getenv("DATABASE_URL")
		}
		request.AllowInsecureDatabase = *allowInsecureDatabase
		if !explicit["allow-insecure-database"] {
			enabled, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("RIDU_ALLOW_INSECURE_DATABASE")))
			request.AllowInsecureDatabase = err == nil && enabled
		}
		request.AllowMaintenance, request.AllowUnbounded = *allowMaintenance, *allowUnbounded
		request.LockWait, request.OperationTimeout = *advisoryLockWait, *concurrentIndexTimeout
		request.LockTimeout, request.StatementTimeout = *lockTimeout, *statementTimeout
		request.BatchTimeout, request.IdleTransactionTimeout = *batchTimeout, *idleTransactionTimeout
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if err := driver.RunProjectMigration(ctx, request, manifest); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	fmt.Fprintln(stdout, "Migrations are current.")
	return nil
}

// sqlitePathAbsolute applies the generated server's rule for RIDU_SQLITE_PATH,
// so migrate up opens the file the server will serve.
func sqlitePathAbsolute(path string) bool {
	if !strings.HasPrefix(path, "file:") {
		return filepath.IsAbs(path)
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return false
	}
	target := parsed.Path
	if target == "" {
		if target, err = url.PathUnescape(parsed.Opaque); err != nil {
			return false
		}
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		// A Windows UNC share names its server as the host.
		return true
	}
	if strings.HasPrefix(target, "/") && filepath.VolumeName(target[1:]) != "" {
		// A Windows drive path is written /C:/….
		target = target[1:]
	}
	return filepath.IsAbs(filepath.FromSlash(target))
}
