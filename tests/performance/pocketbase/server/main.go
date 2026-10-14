// Command pocketbase-server is the PocketBase side of the Ridu/PocketBase SQLite benchmark in
// ../run.ts. It is an ordinary PocketBase Go application with PocketBase's default settings.
// Its one migration creates the posts collection, the benchmark user and a superuser, so a fresh
// data directory needs no dashboard setup and serve never launches the browser installer.
//
//	pocketbase-server migrate up --dir <data>   apply the migration outside the timed startup
//	pocketbase-server serve --dir <data> --http <address>
//
// PB_BENCH_SYNCHRONOUS=FULL replaces only data.db's synchronous(NORMAL) pragma, matching the
// commit durability Ridu's SQLite adapter always uses. Every other pragma stays PocketBase's.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/types"
	"modernc.org/sqlite"
)

const (
	benchmarkEmail    = "admin@riducms.test"
	benchmarkPassword = "ridu-admin"
	superuserEmail    = "superuser@riducms.test"
	superuserPassword = "ridu-superuser"
)

func main() {
	switch mode := os.Getenv("PB_BENCH_SYNCHRONOUS"); mode {
	case "", "NORMAL":
	case "FULL":
		// Connection hooks run after the DSN pragmas PocketBase's DefaultDBConnect applies.
		sqlite.RegisterConnectionHook(func(connection sqlite.ExecQuerierContext, dsn string) error {
			if !strings.Contains(dsn, "data.db") {
				return nil
			}
			_, err := connection.ExecContext(context.Background(), "PRAGMA synchronous = FULL", nil)
			return err
		})
	default:
		log.Fatalf("PB_BENCH_SYNCHRONOUS must be NORMAL or FULL, not %q", mode)
	}

	app := pocketbase.NewWithConfig(pocketbase.Config{HideStartBanner: true})
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{})
	app.OnServe().BindFunc(func(event *core.ServeEvent) error {
		// Benchmark-only diagnostic: the pragmas each pool's connections actually use.
		event.Router.GET("/__bench/sqlite", func(request *core.RequestEvent) error {
			concurrent, err := pragmas(request.App.ConcurrentDB())
			if err != nil {
				return err
			}
			nonconcurrent, err := pragmas(request.App.NonconcurrentDB())
			if err != nil {
				return err
			}
			return request.JSON(http.StatusOK, map[string]any{
				"concurrent": concurrent, "nonconcurrent": nonconcurrent,
			})
		})
		return event.Next()
	})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

func pragmas(db dbx.Builder) (map[string]string, error) {
	result := map[string]string{}
	for _, name := range []string{"synchronous", "journal_mode", "cache_size", "temp_store", "busy_timeout", "foreign_keys"} {
		var value string
		if err := db.NewQuery("PRAGMA " + name).Row(&value); err != nil {
			return nil, fmt.Errorf("read PRAGMA %s: %w", name, err)
		}
		result[name] = value
	}
	return result, nil
}

func init() {
	core.AppMigrations.Register(func(app core.App) error {
		posts := core.NewBaseCollection("posts")
		published := `status = "published" || @request.auth.id != ""`
		authenticated := `@request.auth.id != ""`
		posts.ListRule = types.Pointer(published)
		posts.ViewRule = types.Pointer(published)
		posts.CreateRule = types.Pointer(authenticated)
		posts.UpdateRule = types.Pointer(authenticated)
		posts.DeleteRule = types.Pointer(authenticated)
		posts.Fields.Add(
			&core.TextField{Name: "title", Required: true},
			&core.TextField{Name: "summary"},
			&core.SelectField{Name: "status", Values: []string{"draft", "published"}, MaxSelect: 1, Required: true},
			&core.NumberField{Name: "readingMinutes"},
			&core.BoolField{Name: "featured"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		// Mirrors the Ridu fixture's indexed status field: the anonymous filter and id order.
		posts.AddIndex("idx_posts_status", false, "status, id", "")
		if err := app.Save(posts); err != nil {
			return fmt.Errorf("create posts: %w", err)
		}
		if err := createAuthRecord(app, core.CollectionNameSuperusers, superuserEmail, superuserPassword); err != nil {
			return err
		}
		return createAuthRecord(app, "users", benchmarkEmail, benchmarkPassword)
	}, nil, "1760400000_benchmark.go")
}

func createAuthRecord(app core.App, collectionName, email, password string) error {
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return err
	}
	record := core.NewRecord(collection)
	record.SetEmail(email)
	record.SetPassword(password)
	record.SetVerified(true)
	if err := app.Save(record); err != nil {
		return fmt.Errorf("create %s record: %w", collectionName, err)
	}
	return nil
}
