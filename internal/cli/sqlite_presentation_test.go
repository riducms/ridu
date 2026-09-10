package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/cli"
)

func TestFreshSQLiteProjectPresentationMigrationWorkflow(t *testing.T) {
	ctx := context.Background()
	root := moduleRoot(t)
	target := newProjectTarget(t, "sqlite-presentation")
	setFrameworkProxy(t, root)
	options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	run := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := cli.Run(ctx, args, &stdout, &stderr, options); code != 0 {
			t.Fatalf("ridu %v: %s\n%s", args, stdout.String(), stderr.String())
		}
		return stdout.String()
	}
	run("new", "--template", "blank", "--database", "sqlite", "--module", "example.com/sqlite-presentation", target)
	// The migration regression needs the real generated Go production server.
	// Frontend bundling has its own release fixture; omit that independent build.
	projectPath := filepath.Join(target, "ridu.toml")
	project, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	projectText := strings.ReplaceAll(string(project), "admin = \"./admin\"\n", "")
	projectText = strings.ReplaceAll(projectText, "assets = \"./internal/adminassets/dist\"\n", "")
	if err := os.WriteFile(projectPath, []byte(projectText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(target, "migrations")); err != nil {
		t.Fatal(err)
	}
	config := `package content
import (
 "github.com/riducms/ridu"
 "github.com/riducms/ridu/field"
)
func Config() ridu.Config { return ridu.Config{Name: "Audit",
 Admin: ridu.AdminConfig{Localization: ridu.AdminLocalizationConfig{
  DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}},
  DefaultTimeZone: "UTC", TimeZones: []ridu.AdminTimeZone{{ID: "UTC", Label: "UTC"}, {ID: "Europe/London", Label: "London"}},
 }},
 Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "ar", Label: "Arabic", RTL: false}}},
 Collections: []ridu.Collection{{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{
  field.Text("title").Localized().Required(),
  field.Select("tone", "light", "dark"),
  field.Radio("layout", "compact", "large"),
 }}}} }
`
	writeConfig := func() {
		t.Helper()
		if err := os.WriteFile(filepath.Join(target, "content", "config.go"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig()
	run("generate")
	run("migrate", "create", "--name", "initial")
	database := filepath.Join(target, "content.sqlite")
	run("migrate", "up", "--database-path", database)
	// Exercise content through the generated application's real config before
	// metadata changes, then compare its public document/revision contracts.
	if err := os.WriteFile(filepath.Join(target, "journey_test.go"), []byte(sqlitePresentationContentFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	checkContent := func() {
		t.Helper()
		command := exec.Command("go", "test", ".", "-run", "TestContent", "-count=1")
		command.Dir = target
		command.Env = generatedEnvironment(map[string]string{"RIDU_SQLITE_PATH": database})
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("content journey: %v\n%s", err, output)
		}
	}
	checkContent()
	for _, name := range []string{"application-name", "field-label", "admin-language-default", "admin-timezone-default", "select-choice-order", "radio-choice-order", "admin-language-order", "admin-timezone-order", "content-locale-direction"} {
		switch name {
		case "application-name":
			config = strings.Replace(config, `Name: "Audit"`, `Name: "Publication"`, 1)
		case "field-label":
			config = strings.Replace(config, `.Required()`, `.Required().Label("Headline")`, 1)
		case "admin-language-default":
			config = strings.Replace(config, `DefaultLanguage: "en"`, `DefaultLanguage: "fr"`, 1)
		case "admin-timezone-default":
			config = strings.Replace(config, `DefaultTimeZone: "UTC"`, `DefaultTimeZone: "Europe/London"`, 1)
		case "select-choice-order":
			config = strings.Replace(config, `field.Select("tone", "light", "dark")`, `field.Select("tone", "dark", "light")`, 1)
		case "radio-choice-order":
			config = strings.Replace(config, `field.Radio("layout", "compact", "large")`, `field.Radio("layout", "large", "compact")`, 1)
		case "admin-language-order":
			config = strings.Replace(config, `Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}`, `Languages: []ridu.AdminLanguage{{Code: "fr", Label: "French"}, {Code: "en", Label: "English"}}`, 1)
		case "admin-timezone-order":
			config = strings.Replace(config, `TimeZones: []ridu.AdminTimeZone{{ID: "UTC", Label: "UTC"}, {ID: "Europe/London", Label: "London"}}`, `TimeZones: []ridu.AdminTimeZone{{ID: "Europe/London", Label: "London"}, {ID: "UTC", Label: "UTC"}}`, 1)
		case "content-locale-direction":
			config = strings.Replace(config, `RTL: false`, `RTL: true`, 1)
		}
		writeConfig()
		run("generate")
		run("generate", "--check")
		var stdout, stderr bytes.Buffer
		code := cli.Run(ctx, []string{"migrate", "status", "--database-path", database}, &stdout, &stderr, options)
		if code == 0 || !strings.Contains(stderr.String(), "create") {
			t.Fatalf("missing-artifact status = %d: %s\n%s", code, stdout.String(), stderr.String())
		}
		run("migrate", "create", "--name", name)
		run("migrate", "verify")
		run("migrate", "up", "--database-path", database)
		run("migrate", "status", "--database-path", database)
		checkContent()
	}
	run("build")
	binary := filepath.Join(target, "dist", "sqlite-presentation")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	assertSQLitePresentationProduction(t, binary, target, database)
	config = strings.Replace(config, `.Label("Headline")`, `.Label("Headline"), field.Text("summary").Index()`, 1)
	writeConfig()
	run("generate")
	run("migrate", "create", "--name", "add-summary")
	run("migrate", "verify")
	run("migrate", "up", "--database-path", database)
	run("migrate", "status", "--database-path", database)
	checkContent()
	run("build")
	assertSQLitePresentationProduction(t, binary, target, database)
}

func assertSQLitePresentationProduction(t *testing.T, binary, target, database string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	address := fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))
	command := exec.CommandContext(ctx, binary)
	command.Dir = target
	command.Env = generatedEnvironment(map[string]string{
		"RIDU_SQLITE_PATH": database, "RIDU_ADDRESS": address,
		"RIDU_SKIP_READINESS_PREFLIGHT": "false", "RIDU_ALLOW_UNVERIFIABLE_READINESS": "false",
	})
	log, err := os.CreateTemp(t.TempDir(), "production-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() { cancel(); <-done }()
	client := &http.Client{Timeout: time.Second}
	for {
		response, err := client.Get("http://" + address + "/readyz")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case err := <-done:
			done <- err
			output, _ := os.ReadFile(log.Name())
			t.Fatalf("production startup exited: %v\n%s", err, output)
		case <-ctx.Done():
			output, _ := os.ReadFile(log.Name())
			t.Fatalf("production readiness timed out: %v\n%s", ctx.Err(), output)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

const sqlitePresentationContentFixture = `package journey_test
import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "testing"
 "example.com/sqlite-presentation/content"
 "github.com/riducms/ridu"
 "github.com/riducms/ridu/adapters/sqlite"
 "github.com/riducms/ridu/store"
)
func TestContent(t *testing.T) {
 ctx := context.Background()
 backend, err := sqlite.Open(ctx, os.Getenv("RIDU_SQLITE_PATH")); if err != nil { t.Fatal(err) }; defer backend.Close()
 app, err := ridu.New(content.Config(), backend); if err != nil { t.Fatal(err) }
 baseline := filepath.Join(".ridu", "content-before.json")
 if err := os.MkdirAll(filepath.Dir(baseline), 0700); err != nil { t.Fatal(err) }
 before, err := os.ReadFile(baseline)
 if os.IsNotExist(err) {
  doc, err := app.Local().Create(ctx, "posts", store.Values{"title": store.String("Draft"), "tone": store.String("light"), "layout": store.String("compact")}, nil); if err != nil { t.Fatal(err) }
  _, err = app.Local().PublishChanges(ctx, "posts", doc.ID, store.Values{"title": store.String("Published")}, doc.Revision, nil); if err != nil { t.Fatal(err) }
 } else if err != nil { t.Fatal(err) }
 page, err := app.Local().List(ctx, "posts", ridu.ListOptions{}); if err != nil { t.Fatal(err) }
 if len(page.Documents) != 1 { t.Fatalf("documents: %#v", page) }
 versions, err := app.Local().Versions(ctx, "posts", page.Documents[0].ID, nil); if err != nil || len(versions) != 2 { t.Fatalf("versions: %#v, %v", versions, err) }
 after, err := json.Marshal([]any{page.Documents, versions}); if err != nil { t.Fatal(err) }
 if before == nil { if err := os.WriteFile(baseline, after, 0600); err != nil { t.Fatal(err) } } else if string(before) != string(after) { t.Fatalf("content/revisions changed: %s -> %s", before, after) }
}
`
