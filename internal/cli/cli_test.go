package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/cli"
	"github.com/riducms/ridu/internal/frameworkpackages"
	"github.com/riducms/ridu/internal/frameworkproxy"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

var testReleaseVersion = "v0.0.0-test.3"

var sharedFrameworkTestEnvironment struct {
	once          sync.Once
	root          string
	clean         bool
	frameworkRoot string
	version       string
	proxyURL      string
	moduleCache   string
	buildCache    string
	err           error
}

var sharedFrontendTestSnapshot struct {
	once     sync.Once
	snapshot *frameworkpackages.Snapshot
	err      error
}

func publishFrontendPackages(t *testing.T, target string) {
	t.Helper()
	sharedFrontendTestSnapshot.once.Do(func() {
		sharedFrontendTestSnapshot.snapshot, sharedFrontendTestSnapshot.err = frameworkpackages.Prepare(moduleRoot(t))
	})
	if sharedFrontendTestSnapshot.err != nil {
		t.Fatal(sharedFrontendTestSnapshot.err)
	}
	if err := sharedFrontendTestSnapshot.snapshot.Publish(target, testReleaseVersion); err != nil {
		t.Fatal(err)
	}
}

func TestMain(m *testing.M) {
	exitCode := m.Run()
	if snapshot := sharedFrontendTestSnapshot.snapshot; snapshot != nil {
		if err := snapshot.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "remove frontend publication snapshot: %v\n", err)
			exitCode = 1
		}
	}
	if root := sharedFrameworkTestEnvironment.root; root != "" && sharedFrameworkTestEnvironment.clean {
		if err := removeSharedFrameworkTestEnvironment(root); err != nil {
			fmt.Fprintf(os.Stderr, "remove shared CLI release fixture: %v\n", err)
			if exitCode == 0 {
				exitCode = 1
			}
		}
	}
	os.Exit(exitCode)
}

func TestDatabaseCredentialsNeverAppearInHelpDefaults(t *testing.T) {
	const databaseURL = "postgres://help-sentinel-user:help-sentinel-password@database.example/ridu?sslmode=require"
	const databasePath = "/private/help-sentinel-database.sqlite"
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv("RIDU_SQLITE_PATH", databasePath)

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "dev", args: []string{"dev", "--help"}},
		{name: "migrate", args: []string{"migrate", "status", "--help"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if code := cli.Run(t.Context(), test.args, &stdout, &stderr, cli.Options{}); code != 2 {
				t.Fatalf("help exit = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			output := stdout.String() + stderr.String()
			if !strings.Contains(output, "-database-url") || !strings.Contains(output, "DATABASE_URL") {
				t.Fatalf("help omitted safe database option guidance: %s", output)
			}
			for _, secret := range []string{databaseURL, databasePath, "help-sentinel-user", "help-sentinel-password"} {
				if strings.Contains(output, secret) {
					t.Fatalf("help leaked %q: %s", secret, output)
				}
			}
		})
	}
}

func TestNewReleaseOverrideWorksThroughRealBinary(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, "binary-dogfood-content")
	setFrameworkProxy(t, frameworkRoot)
	binary := filepath.Join(t.TempDir(), "ridu")
	resultFile := filepath.Join(t.TempDir(), "new-project-result")

	build := exec.Command("go", "build", "-o", binary, "./cmd/ridu")
	build.Dir = frameworkRoot
	build.Env = os.Environ()
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real CLI: %v\n%s", err, output)
	}
	command := exec.Command(binary,
		"new",
		target,
		"--release-version", testReleaseVersion,
		"--module", "example.com/fixture/binary-dogfood",
		"--scope", "@fixture",
	)
	command.Dir = frameworkRoot
	command.Env = append(os.Environ(), "RIDU_NEW_PROJECT_RESULT_FILE="+resultFile)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run real CLI: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("framework-development override")) {
		t.Fatalf("real CLI did not disclose release override:\n%s", output)
	}
	goModule, err := os.ReadFile(filepath.Join(target, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goModule), "github.com/riducms/ridu "+testReleaseVersion) || strings.Contains(string(goModule), "replace github.com/riducms/ridu") {
		t.Fatalf("real CLI ignored release override:\n%s", goModule)
	}
	adminPackage, err := os.ReadFile(filepath.Join(target, "admin", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(adminPackage), `"@riducms/admin": "`+strings.TrimPrefix(testReleaseVersion, "v")+`"`) ||
		!strings.Contains(string(adminPackage), `"@riducms/plugin": "`+strings.TrimPrefix(testReleaseVersion, "v")+`"`) ||
		!strings.Contains(string(adminPackage), `"@riducms/ui": "`+strings.TrimPrefix(testReleaseVersion, "v")+`"`) ||
		!strings.Contains(string(adminPackage), `"@riducms/build": "`+strings.TrimPrefix(testReleaseVersion, "v")+`"`) ||
		!strings.Contains(string(adminPackage), `"@iconify/json": "^2.2.509"`) {
		t.Fatalf("real CLI did not apply the npm release override:\n%s", adminPackage)
	}
	rootPackage, err := os.ReadFile(filepath.Join(target, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rootPackage), `"@riducms/sdk": "`+strings.TrimPrefix(testReleaseVersion, "v")+`"`) ||
		!strings.Contains(string(rootPackage), `"@riducms/plugin-richtext": "`+strings.TrimPrefix(testReleaseVersion, "v")+`"`) ||
		!strings.Contains(string(rootPackage), `"@riducms/plugin-form-builder": "`+strings.TrimPrefix(testReleaseVersion, "v")+`"`) {
		t.Fatalf("real CLI did not make the generated contract dependency root-resolvable:\n%s", rootPackage)
	}
	if _, err := os.Stat(filepath.Join(target, "generated", "ridu.schema.json")); err != nil {
		t.Fatalf("real CLI did not generate initial contracts: %v", err)
	}
	created, err := os.ReadFile(resultFile)
	if err != nil {
		t.Fatalf("real CLI did not record its created project: %v", err)
	}
	if got, want := strings.TrimSpace(string(created)), target; got != want {
		t.Fatalf("created project result = %q, want %q", got, want)
	}
}

func TestGeneratedProjectCompilesAndGeneratesOutsideRepository(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, "fixture-content")
	setFrameworkProxy(t, frameworkRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := cli.Run(context.Background(), []string{
		"new",
		"--module", "example.com/fixture/content",
		"--scope", "@fixture",
		target,
	}, &stdout, &stderr, cli.Options{WorkingDirectory: frameworkRoot, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion})
	if exitCode != 0 {
		t.Fatalf("ridu new exit = %d; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}
	goModule, err := os.ReadFile(filepath.Join(target, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goModule), "github.com/riducms/ridu "+testReleaseVersion) || strings.Contains(string(goModule), "replace github.com/riducms/ridu") {
		t.Fatalf("generated go.mod does not consume the released framework version:\n%s", goModule)
	}
	if _, err := os.Stat(filepath.Join(target, "go.sum")); err != nil {
		t.Fatalf("ridu new did not initialize go.sum: %v", err)
	}

	manifestPath := filepath.Join(target, "generated", "ridu.schema.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read generated manifest: %v", err)
	}
	manifest, err := schema.Parse(manifestBytes)
	if err != nil {
		t.Fatalf("parse generated manifest: %v", err)
	}
	if got := len(manifest.Snapshot().Collections); got != 2 {
		t.Fatalf("generated collection count = %d, want 2", got)
	}
	clientPath := filepath.Join(target, "generated", "ridu.generated.ts")
	clientBytes, err := os.ReadFile(clientPath)
	if err != nil {
		t.Fatalf("read generated client: %v", err)
	}
	if strings.Contains(string(clientBytes), "any") {
		t.Fatalf("generated client contains forbidden any: %s", clientBytes)
	}
	if !strings.Contains(string(clientBytes), "export interface RiduConfig") ||
		!strings.Contains(string(clientBytes), "export function createClient") {
		t.Fatalf("generated client is missing its config or binding: %s", clientBytes)
	}
	if _, err := os.Stat(filepath.Join(target, "packages")); !os.IsNotExist(err) {
		t.Fatalf("default generated project exposes a packages directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "generated", "ridu.openapi.json")); !os.IsNotExist(err) {
		t.Fatalf("default generated project should leave OpenAPI generation disabled: %v", err)
	}
	goClientBytes, err := os.ReadFile(filepath.Join(target, "generated", "ridu.generated.go"))
	if err != nil {
		t.Fatalf("read generated Go client: %v", err)
	}
	if !strings.Contains(string(goClientBytes), "var PostsCollection = core.NewTypedCollection") || !strings.Contains(string(goClientBytes), "type PostCreate struct") {
		t.Fatalf("generated Go client is missing typed collection contracts: %s", goClientBytes)
	}
	adminPlugins, err := os.ReadFile(filepath.Join(target, "admin", "src", "ridu.plugins.generated.ts"))
	if err != nil {
		t.Fatalf("read generated admin plugins: %v", err)
	}
	if !strings.Contains(string(adminPlugins), "richTextAdminPlugin") || !strings.Contains(string(adminPlugins), "resolveAdminPluginPairs") {
		t.Fatalf("generated admin plugins did not follow Go config: %s", adminPlugins)
	}
	fixturePluginDirectory := filepath.Join(target, "plugins", "fixture")
	if err := os.MkdirAll(fixturePluginDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	fixturePlugin := `package fixture

import "github.com/riducms/ridu"

type plugin struct{}
func New() ridu.Plugin { return plugin{} }
func (plugin) Key() string { return "fixture" }
func (plugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{Version: "1.0.0", GoPackage: "example.com/fixture/content/plugins/fixture", APIVersion: ridu.PluginAPIVersion, Ridu: ridu.RiduCompatibility{Minimum: "0.0.0-dev", MaximumExclusive: "1.0.0"}}
}
`
	if err := os.WriteFile(filepath.Join(fixturePluginDirectory, "plugin.go"), []byte(fixturePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = cli.Run(context.Background(), []string{"plugin", "add", "fixture", "--go-package", "example.com/fixture/content/plugins/fixture", "--go-version", "v0.0.0", "--no-install"}, &stdout, &stderr, cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion})
	if exitCode != 0 {
		t.Fatalf("ridu plugin add exit = %d; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}
	pluginRegistry, err := os.ReadFile(filepath.Join(target, "ridu.plugins.json"))
	if err != nil {
		t.Fatal(err)
	}
	compiledRegistry, err := os.ReadFile(filepath.Join(target, "content", "ridu_plugins.generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pluginRegistry), `"key": "fixture"`) || !strings.Contains(string(compiledRegistry), `"example.com/fixture/content/plugins/fixture"`) {
		t.Fatalf("plugin add did not update both registries:\n%s\n%s", pluginRegistry, compiledRegistry)
	}
	projectFilePath := filepath.Join(target, "ridu.toml")
	projectFile, err := os.ReadFile(projectFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectFilePath, append(append([]byte(nil), projectFile...), []byte("generated.fixture.contract = \"./generated/fixture.txt\"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = cli.Run(context.Background(), []string{"plugin", "remove", "fixture", "--no-install"}, &stdout, &stderr, cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion})
	if exitCode != 1 || !strings.Contains(stderr.String(), "remove those mappings") {
		t.Fatalf("configured-artifact plugin remove exit = %d; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}
	pluginRegistry, err = os.ReadFile(filepath.Join(target, "ridu.plugins.json"))
	if err != nil || !strings.Contains(string(pluginRegistry), `"key": "fixture"`) {
		t.Fatalf("refused plugin remove changed the registry: %v\n%s", err, pluginRegistry)
	}
	if err := os.WriteFile(projectFilePath, projectFile, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = cli.Run(context.Background(), []string{"plugin", "remove", "fixture", "--no-install"}, &stdout, &stderr, cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion})
	if exitCode != 0 {
		t.Fatalf("ridu plugin remove exit = %d; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}
	incompatibleDirectory := filepath.Join(target, "plugins", "incompatible")
	if err := os.MkdirAll(incompatibleDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	incompatiblePlugin := strings.Replace(fixturePlugin, `package fixture`, `package incompatible`, 1)
	incompatiblePlugin = strings.Replace(incompatiblePlugin, `return "fixture"`, `return "incompatible"`, 1)
	incompatiblePlugin = strings.Replace(incompatiblePlugin, `example.com/fixture/content/plugins/fixture`, `example.com/fixture/content/plugins/incompatible`, 1)
	incompatiblePlugin = strings.Replace(incompatiblePlugin, `Minimum: "0.0.0-dev"`, `Minimum: "9.0.0"`, 1)
	if err := os.WriteFile(filepath.Join(incompatibleDirectory, "plugin.go"), []byte(incompatiblePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = cli.Run(context.Background(), []string{"add", "incompatible", "--go-package", "example.com/fixture/content/plugins/incompatible", "--go-version", "v0.0.0", "--no-install"}, &stdout, &stderr, cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion})
	if exitCode != 1 || !strings.Contains(stderr.String(), "rolled back") {
		t.Fatalf("incompatible ridu add exit = %d; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}
	pluginRegistry, err = os.ReadFile(filepath.Join(target, "ridu.plugins.json"))
	if err != nil {
		t.Fatal(err)
	}
	compiledRegistry, err = os.ReadFile(filepath.Join(target, "content", "ridu_plugins.generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pluginRegistry), `"key": "incompatible"`) || strings.Contains(string(compiledRegistry), `plugins/incompatible`) {
		t.Fatalf("failed plugin installation was not rolled back:\n%s\n%s", pluginRegistry, compiledRegistry)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = cli.Run(context.Background(), []string{"generate", "--check"}, &stdout, &stderr, cli.Options{
		WorkingDirectory: target,
		Version:          testReleaseVersion,
		FrameworkVersion: ridu.FrameworkVersion,
	})
	if exitCode != 0 {
		t.Fatalf("ridu generate --check exit = %d; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}

	command := exec.Command("go", "test", "./...")
	command.Dir = target
	command.Env = os.Environ()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated go test: %v\n%s", err, output)
	}
	formatCommand := exec.Command("gofmt", "-l", "cmd", "content", "internal", "generated")
	formatCommand.Dir = target
	if output, err := formatCommand.CombinedOutput(); err != nil {
		t.Fatalf("generated gofmt check: %v\n%s", err, output)
	} else if len(bytes.TrimSpace(output)) != 0 {
		t.Fatalf("generated Go files need formatting:\n%s", output)
	}
}

func TestGeneratedProjectIgnoresAnUnlistedEnclosingGoWorkspace(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	setFrameworkProxy(t, frameworkRoot)
	workspaceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceRoot, "go.mod"), []byte("module example.com/outer-workspace\n\ngo 1.25.13\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workFile := filepath.Join(workspaceRoot, "go.work")
	if err := os.WriteFile(workFile, []byte("go 1.25.13\n\nuse .\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", workFile)
	target := filepath.Join(workspaceRoot, "nested-content")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := cli.Run(context.Background(), []string{
		"new",
		"--module", "example.com/fixture/nested-content",
		"--scope", "@fixture",
		target,
	}, &stdout, &stderr, cli.Options{WorkingDirectory: frameworkRoot, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion})
	if exitCode != 0 {
		t.Fatalf("nested ridu new exit = %d; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(target, "generated", "ridu.schema.json")); err != nil {
		t.Fatalf("nested project did not generate initial contracts: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = cli.Run(context.Background(), []string{"generate", "--check"}, &stdout, &stderr, cli.Options{
		WorkingDirectory: target,
		Version:          testReleaseVersion,
		FrameworkVersion: ridu.FrameworkVersion,
	})
	if exitCode != 0 {
		t.Fatalf("nested ridu generate --check exit = %d; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}
}

func TestNewKeepsProjectWhenDependencySetupFails(t *testing.T) {
	target := newProjectTarget(t, "incomplete-content")
	emptyProxy := t.TempDir()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", (&url.URL{Scheme: "file", Path: emptyProxy}).String())
	t.Setenv("GOSUMDB", "off")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := cli.Run(context.Background(), []string{"new", target}, &stdout, &stderr, cli.Options{Version: "v9.9.9-test", FrameworkVersion: ridu.FrameworkVersion})
	if exitCode != 1 {
		t.Fatalf("ridu new exit = %d, want 1; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(target, "go.mod")); err != nil {
		t.Fatalf("recoverable project was not kept: %v", err)
	}
	if !strings.Contains(stderr.String(), "project was kept") || !strings.Contains(stderr.String(), "go mod tidy") || !strings.Contains(stderr.String(), "ridu generate") {
		t.Fatalf("ridu new stderr = %q", stderr.String())
	}
}

func TestNewKeepsProjectWhenInitialGenerationFails(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, "invalid-config-content")
	setFrameworkProxy(t, frameworkRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := cli.Run(context.Background(), []string{"new", target}, &stdout, &stderr, cli.Options{
		Version:          testReleaseVersion,
		FrameworkVersion: "9.9.9",
	})
	if exitCode != 1 {
		t.Fatalf("ridu new exit = %d, want 1; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(target, "go.sum")); err != nil {
		t.Fatalf("initialized project was not kept: %v", err)
	}
	if !strings.Contains(stderr.String(), "initial contract generation failed") || !strings.Contains(stderr.String(), "project was kept") {
		t.Fatalf("ridu new stderr = %q", stderr.String())
	}
}

func TestNewRejectsAnUnversionedCLIWithoutCreatingTarget(t *testing.T) {
	target := newProjectTarget(t, "unversioned-content")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := cli.Run(context.Background(), []string{"new", target}, &stdout, &stderr, cli.Options{Version: "development"})
	if exitCode != 1 {
		t.Fatalf("ridu new exit = %d, want 1", exitCode)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("invalid CLI version created a target: %v", err)
	}
	if !strings.Contains(stderr.String(), "semantic release") {
		t.Fatalf("ridu new stderr = %q", stderr.String())
	}
}

func TestNewRejectsUnknownTemplateBeforeCreatingTarget(t *testing.T) {
	target := newProjectTarget(t, "unknown-template-content")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := cli.Run(context.Background(), []string{"new", "--template", "website", target}, &stdout, &stderr, cli.Options{Version: testReleaseVersion})
	if exitCode != 2 {
		t.Fatalf("ridu new exit = %d, want 2", exitCode)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("invalid template created a target: %v", err)
	}
	if !strings.Contains(stderr.String(), "unknown project template") {
		t.Fatalf("ridu new stderr = %q", stderr.String())
	}
}

func TestInteractiveNewCanCancelBeforeCreatingTarget(t *testing.T) {
	target := newProjectTarget(t, "cancelled-content")
	resultFile := filepath.Join(t.TempDir(), "new-project-result")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := cli.Run(context.Background(), []string{"new", target}, &stdout, &stderr, cli.Options{
		Version:              testReleaseVersion,
		Stdin:                strings.NewReader("1\n1\n1\n1\nn\n"),
		Interactive:          true,
		Accessible:           true,
		NewProjectResultFile: resultFile,
	})
	if exitCode != 0 {
		t.Fatalf("ridu new exit = %d; stdout: %s; stderr: %s", exitCode, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("cancelled template selection created a target: %v", err)
	}
	if _, err := os.Stat(resultFile); !os.IsNotExist(err) {
		t.Fatalf("cancelled template selection recorded a created project: %v", err)
	}
	if !strings.Contains(stdout.String(), "starter") || !strings.Contains(stdout.String(), "blank") || !strings.Contains(stdout.String(), "cancelled") {
		t.Fatalf("interactive template output = %q", stdout.String())
	}
}

func TestGenerateCheckReportsDriftWithoutOverwriting(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, "drift-content")
	setFrameworkProxy(t, frameworkRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	if exitCode := cli.Run(context.Background(), []string{"new", target}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu new: %s", stderr.String())
	}
	manifestPath := filepath.Join(target, "generated", "ridu.schema.json")
	if err := os.WriteFile(manifestPath, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"generate", "--check"}, &stdout, &stderr, options); exitCode != 1 {
		t.Fatalf("generate --check exit = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "out of date") {
		t.Fatalf("generate --check stderr = %q", stderr.String())
	}
	actual, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != "stale\n" {
		t.Fatalf("check mode overwrote drifted manifest with %q", actual)
	}
}

func TestNewCurrentDirectoryGeneratesCompleteProject(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	setFrameworkProxy(t, frameworkRoot)
	for _, argument := range []string{".", "./"} {
		t.Run(argument, func(t *testing.T) {
			target := newProjectTarget(t, "My CMS")
			if err := os.Mkdir(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "notes.txt"), []byte("keep me"), 0o644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
			if exitCode := cli.Run(context.Background(), []string{"new", "--package-manager", "npm", argument, "--template", "blank", "--database", "sqlite", "--no-agent"}, &stdout, &stderr, options); exitCode != 0 {
				t.Fatalf("ridu new %s: %s\n%s", argument, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "current directory") || strings.Contains(stdout.String(), "  cd ") || !strings.Contains(stdout.String(), "npm install") {
				t.Fatalf("current directory instructions: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			for _, path := range []string{"go.sum", "generated/ridu.schema.json", "generated/ridu.generated.ts"} {
				if _, err := os.Stat(filepath.Join(target, path)); err != nil {
					t.Fatal(err)
				}
			}
			module, err := os.ReadFile(filepath.Join(target, "go.mod"))
			if err != nil || !strings.Contains(string(module), "module example.com/my-cms") {
				t.Fatalf("default module: %s, %v", module, err)
			}
			files, err := migrationartifact.ReadAll(filepath.Join(target, "migrations"))
			if err != nil || len(files) != 1 {
				t.Fatalf("initial migration = %#v, %v", files, err)
			}
			notes, err := os.ReadFile(filepath.Join(target, "notes.txt"))
			if err != nil || string(notes) != "keep me" {
				t.Fatalf("existing notes changed: %s, %v", notes, err)
			}
		})
	}
}

func TestNewProjectIncludesTheMigrationRequiredByFirstBuild(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, "first-build")
	setFrameworkProxy(t, frameworkRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	if exitCode := cli.Run(context.Background(), []string{"new", "--database", "sqlite", "--module", "example.com/first/build", target}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu new: %s", stderr.String())
	}
	files, err := migrationartifact.ReadAll(filepath.Join(target, "migrations"))
	if err != nil || len(files) != 1 || !strings.HasSuffix(files[0].Name, "_initial.ridu.json") {
		t.Fatalf("initial migration = %#v, %v", files, err)
	}

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

	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"build"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("first ridu build exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Built dist/first-build") {
		t.Fatalf("first ridu build output = %q", stdout.String())
	}
}

func TestNewMongoDBStarterAndBlankGenerateWithoutOpeningTheDatabase(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	setFrameworkProxy(t, frameworkRoot)
	const offlineSecret = "mongodb-scaffold-must-not-connect"
	t.Setenv("DATABASE_URL", "mongodb://offline-user:"+offlineSecret+"@127.0.0.1:1/ridu?directConnection=true&replicaSet=ridu-rs0")

	for _, selectedTemplate := range []string{"starter", "blank"} {
		t.Run(selectedTemplate, func(t *testing.T) {
			target := newProjectTarget(t, "mongodb-"+selectedTemplate)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
			if exitCode := cli.Run(context.Background(), []string{
				"new",
				"--database", "mongodb",
				"--template", selectedTemplate,
				"--module", "example.com/mongodb/" + selectedTemplate,
				target,
			}, &stdout, &stderr, options); exitCode != 0 {
				t.Fatalf("ridu new MongoDB %s exit = %d\nstdout: %s\nstderr: %s", selectedTemplate, exitCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), "Created Ridu "+selectedTemplate+" project") {
				t.Fatalf("ridu new MongoDB %s output = %q", selectedTemplate, stdout.String())
			}

			stdout.Reset()
			stderr.Reset()
			if exitCode := cli.Run(context.Background(), []string{"generate", "--check"}, &stdout, &stderr, options); exitCode != 0 {
				t.Fatalf("ridu generate --check MongoDB %s exit = %d\nstdout: %s\nstderr: %s", selectedTemplate, exitCode, stdout.String(), stderr.String())
			}
			files, err := migrationartifact.ReadAll(filepath.Join(target, "migrations"))
			if err != nil || len(files) != 1 || !strings.HasSuffix(files[0].Name, "_initial.ridu.json") {
				t.Fatalf("ridu new MongoDB %s initial migrations = %#v, %v", selectedTemplate, files, err)
			}

			if err := filepath.WalkDir(target, func(path string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				contents, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if strings.Contains(string(contents), offlineSecret) {
					t.Errorf("generated MongoDB file %s persisted DATABASE_URL credentials", path)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			_, postsError := os.Stat(filepath.Join(target, "content", "posts.go"))
			if selectedTemplate == "starter" && postsError != nil {
				t.Fatalf("MongoDB starter omitted posts: %v", postsError)
			}
			if selectedTemplate == "blank" && !os.IsNotExist(postsError) {
				t.Fatalf("MongoDB blank installed starter posts: %v", postsError)
			}
		})
	}
}

func TestCheckAndBuildOwnTheGeneratedGoWorkflow(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, "workflow-content")
	setFrameworkProxy(t, frameworkRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	if exitCode := cli.Run(context.Background(), []string{"new", "--module", "example.com/workflow/content", target}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu new: %s", stderr.String())
	}
	projectPath := filepath.Join(target, "ridu.toml")
	projectBytes, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	projectText := strings.ReplaceAll(string(projectBytes), "admin = \"./admin\"\n", "")
	projectText = strings.ReplaceAll(projectText, "assets = \"./internal/adminassets/dist\"\n", "")
	if err := os.RemoveAll(filepath.Join(target, "migrations")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(projectText), 0o644); err != nil {
		t.Fatal(err)
	}
	if exitCode := cli.Run(context.Background(), []string{"generate"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu generate: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"check"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), "migration artifact history is empty") || !strings.Contains(stderr.String(), "ridu migrate create --name initial") {
		t.Fatalf("ridu check without history exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"build"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), "migration artifact history is empty") || !strings.Contains(stderr.String(), "ridu migrate create --name initial") {
		t.Fatalf("ridu build without history exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(target, "dist", "workflow-content")); !os.IsNotExist(err) {
		t.Fatalf("ridu build without history created output: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"migrate", "status", "--database-url", "postgres://invalid:invalid@127.0.0.1:1/invalid", "--allow-insecure-database"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), "migration artifact history is empty") {
		t.Fatalf("ridu migrate status without history exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"migrate", "create", "--name", "initial"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu migrate create: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"check"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu check exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	mismatchedProjectText := strings.Replace(projectText, `database = "postgres"`, `database = "mongodb"`, 1)
	if mismatchedProjectText == projectText {
		t.Fatal("generated project database selection was not found")
	}
	if err := os.WriteFile(projectPath, []byte(mismatchedProjectText), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"check"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), `uses planner "atlas" instead of selected database planner "mongodb"`) {
		t.Fatalf("ridu check with wrong-adapter history exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	builtPath := filepath.Join(target, "dist", "workflow-content")
	if err := os.MkdirAll(filepath.Dir(builtPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(builtPath, []byte("last-adapter-matched-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"build"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), `uses planner "atlas" instead of selected database planner "mongodb"`) {
		t.Fatalf("ridu build with wrong-adapter history exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	if encoded, err := os.ReadFile(builtPath); err != nil || string(encoded) != "last-adapter-matched-binary" {
		t.Fatalf("wrong-adapter history build replaced the last reviewed binary: contents=%q error=%v", encoded, err)
	}
	if err := os.WriteFile(projectPath, []byte(projectText), 0o644); err != nil {
		t.Fatal(err)
	}
	postsPath := filepath.Join(target, "content", "posts.go")
	posts, err := os.ReadFile(postsPath)
	if err != nil {
		t.Fatal(err)
	}
	changedPosts := strings.Replace(string(posts), `field.Text("title")`, `field.Text("headline")`, 1)
	if changedPosts == string(posts) {
		t.Fatal("generated posts field was not found")
	}
	if err := os.WriteFile(postsPath, []byte(changedPosts), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"generate"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu generate changed manifest: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"check"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), "does not match latest migration artifact") {
		t.Fatalf("ridu check with stale history exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	if err := os.MkdirAll(filepath.Dir(builtPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(builtPath, []byte("last-reviewed-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"build"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), "does not match latest migration artifact") {
		t.Fatalf("ridu build with stale history exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	if encoded, err := os.ReadFile(builtPath); err != nil || string(encoded) != "last-reviewed-binary" {
		t.Fatalf("stale-history build replaced the last reviewed binary: contents=%q error=%v", encoded, err)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"migrate", "create", "--name", "rename-title", "--accept-renames"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu migrate create rename: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"check"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu check after migration exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(context.Background(), []string{"build"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu build exit = %d\nstdout: %s\nstderr: %s", exitCode, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(builtPath); err != nil {
		t.Fatalf("built binary: %v", err)
	}
	files, err := migrationartifact.ReadAll(filepath.Join(target, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	identities := make([]ridumigration.ArtifactIdentity, len(files))
	for index, file := range files {
		identities[index] = ridumigration.ArtifactIdentity{Name: file.Name, Digest: file.Digest}
	}
	historyDigest, err := ridumigration.DigestArtifactHistory(identities)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(builtPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(historyDigest)) {
		t.Fatal("ridu build did not bind the exact ordered migration history into the executable")
	}
}

func TestGeneratedProjectMigratesAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run generated-project PostgreSQL integration")
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("ridu_generated_%d", time.Now().UnixNano())
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA "`+schemaName+`" CASCADE`)
		admin.Close()
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	parsed.RawQuery = parameters.Encode()

	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, "postgres-content")
	setFrameworkProxy(t, frameworkRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	if exitCode := cli.Run(ctx, []string{"new", "--module", "example.com/postgres/content", target}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu new: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(ctx, []string{"generate"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu generate: %s", stderr.String())
	}
	serverPath := filepath.Join(target, "cmd", "server", "main.go")
	serverBytes, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatal(err)
	}
	serverText := strings.Replace(string(serverBytes),
		`"github.com/riducms/ridu/adapters/postgres"`,
		"\"github.com/riducms/ridu/adapters/postgres\"\n\t\"github.com/riducms/ridu/migration\"", 1)
	serverText = strings.Replace(serverText,
		"\tvar options []ridu.ExecuteOption\n\tif len(os.Args) == 1 {\n\t\toptions = runtimeOptions(applicationConfig)\n\t}",
		"\toptions := []ridu.ExecuteOption{\n"+
			"\t\tridu.WithProjectMigrations(postgres.ProjectMigrations(migration.DataTransform{\n"+
			"\t\t\tDataTransformDescriptor: migration.DataTransformDescriptor{\n"+
			"\t\t\t\tName: \"seed-data\", Checksum: migration.DataTransformChecksum([]byte(\"seed-data-v1\")),\n"+
			"\t\t\t},\n"+
			"\t\t\tUp: func(context.Context, migration.DataTransaction) error { return nil },\n"+
			"\t\t\tDown: func(context.Context, migration.DataTransaction) error { return nil },\n"+
			"\t\t})),\n"+
			"\t}\n"+
			"\tif len(os.Args) == 1 {\n"+
			"\t\toptions = append(options, runtimeOptions(applicationConfig)...)\n"+
			"\t}", 1)
	if serverText == string(serverBytes) || !strings.Contains(serverText, "postgres.ProjectMigrations") {
		t.Fatal("could not register compiled PostgreSQL migration callback in generated entry")
	}
	if err := os.WriteFile(serverPath, []byte(serverText), 0o644); err != nil {
		t.Fatal(err)
	}
	format := exec.Command("gofmt", "-w", serverPath)
	format.Dir = target
	if output, err := format.CombinedOutput(); err != nil {
		t.Fatalf("format generated PostgreSQL project: %v\n%s", err, output)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = target
	tidy.Env = os.Environ()
	if output, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("tidy generated PostgreSQL project: %v\n%s", err, output)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(ctx, []string{"migrate", "create", "--name", "unknown", "--transform", "missing-transform"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), "did not register") {
		t.Fatalf("unknown PostgreSQL transform: exit=%d stderr=%q", exitCode, stderr.String())
	}
	for _, invocation := range [][]string{
		{"migrate", "plan", "--database-url", parsed.String(), "--allow-insecure-database", "--json"},
		{"migrate", "verify", "--database-url", parsed.String(), "--allow-insecure-database"},
		{"migrate", "up", "--database-url", parsed.String(), "--allow-insecure-database"},
		{"migrate", "create", "--name", "seed-data", "--transform", "seed-data"},
		{"migrate", "plan", "--database-url", parsed.String(), "--allow-insecure-database", "--json"},
		{"migrate", "verify", "--database-url", parsed.String(), "--allow-insecure-database", "--allow-maintenance"},
		{"migrate", "up", "--database-url", parsed.String(), "--allow-insecure-database", "--allow-maintenance"},
		{"migrate", "status", "--database-url", parsed.String(), "--allow-insecure-database", "--json"},
		{"migrate", "status", "--database-url", parsed.String(), "--allow-insecure-database"},
	} {
		stdout.Reset()
		stderr.Reset()
		if exitCode := cli.Run(ctx, invocation, &stdout, &stderr, options); exitCode != 0 {
			t.Fatalf("ridu %v: %s", invocation, stderr.String())
		}
	}
	if !strings.Contains(stdout.String(), "applied") {
		t.Fatalf("migration status = %q", stdout.String())
	}
	files, err := migrationartifact.ReadAll(filepath.Join(target, "migrations"))
	hasTransform := false
	if len(files) == 2 {
		for _, phase := range files[1].Artifact.Phases {
			for _, step := range phase.Steps {
				hasTransform = hasTransform || step.Kind == ridumigration.StepDataTransform
			}
		}
	}
	if err != nil || len(files) != 2 || !strings.HasSuffix(files[0].Name, ".ridu.json") || !hasTransform {
		t.Fatalf("generated migrations = %v, %v", files, err)
	}
}

func TestGeneratedProjectUsesSQLiteMigrationLifecycle(t *testing.T) {
	ctx := context.Background()
	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, "sqlite-content")
	setFrameworkProxy(t, frameworkRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	if exitCode := cli.Run(ctx, []string{"new", "--database", "sqlite", "--module", "example.com/sqlite/content", target}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu new: %s", stderr.String())
	}
	serverPath := filepath.Join(target, "cmd", "server", "main.go")
	serverBytes, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatal(err)
	}
	serverText := strings.Replace(string(serverBytes),
		`"github.com/riducms/ridu/adapters/sqlite"`,
		"\"github.com/riducms/ridu/adapters/sqlite\"\n\t\"github.com/riducms/ridu/migration\"", 1)
	serverText = strings.Replace(serverText,
		"\tvar options []ridu.ExecuteOption\n\tif len(os.Args) == 1 {\n\t\toptions = runtimeOptions(applicationConfig)\n\t}",
		"\toptions := []ridu.ExecuteOption{\n"+
			"\t\tridu.WithProjectMigrations(sqlite.ProjectMigrations(migration.DataTransform{\n"+
			"\t\t\tDataTransformDescriptor: migration.DataTransformDescriptor{\n"+
			"\t\t\t\tName: \"seed-data\", Checksum: migration.DataTransformChecksum([]byte(\"seed-data-v1\")),\n"+
			"\t\t\t},\n"+
			"\t\t\tUp: func(context.Context, migration.DataTransaction) error { return nil },\n"+
			"\t\t\tDown: func(context.Context, migration.DataTransaction) error { return nil },\n"+
			"\t\t})),\n"+
			"\t}\n"+
			"\tif len(os.Args) == 1 {\n"+
			"\t\toptions = append(options, runtimeOptions(applicationConfig)...)\n"+
			"\t}", 1)
	if serverText == string(serverBytes) || !strings.Contains(serverText, "WithProjectMigrations") {
		t.Fatal("could not register compiled SQLite migration callback in generated entry")
	}
	if err := os.WriteFile(serverPath, []byte(serverText), 0o644); err != nil {
		t.Fatal(err)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = target
	tidy.Env = os.Environ()
	if output, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("tidy generated SQLite project: %v\n%s", err, output)
	}
	if exitCode := cli.Run(ctx, []string{"generate"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu generate: %s", stderr.String())
	}
	databaseFile := filepath.Join(target, ".ridu", "lifecycle.sqlite")
	databasePath := "file:.ridu/lifecycle.sqlite?cache=private"
	if err := os.MkdirAll(filepath.Dir(databaseFile), 0o755); err != nil {
		t.Fatal(err)
	}
	missingDatabase := filepath.Join(target, ".ridu", "inspection-missing.sqlite")
	for _, invocation := range [][]string{
		{"migrate", "plan", "--database-path", missingDatabase, "--json"},
		{"migrate", "status", "--database-path", missingDatabase},
	} {
		stdout.Reset()
		stderr.Reset()
		if exitCode := cli.Run(ctx, invocation, &stdout, &stderr, options); exitCode != 0 {
			t.Fatalf("ridu %v: %s", invocation, stderr.String())
		}
		if !strings.Contains(stdout.String(), "pending") && !strings.Contains(stdout.String(), `"applied": false`) {
			t.Fatalf("missing database inspection output = %q", stdout.String())
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if _, err := os.Stat(missingDatabase + suffix); !os.IsNotExist(err) {
				t.Fatalf("ridu %v created missing SQLite target %q: %v", invocation, missingDatabase+suffix, err)
			}
		}
	}

	for _, invocation := range [][]string{
		{"migrate", "up", "--database-path", ":memory:"},
		{"migrate", "down", "--database-path", ":memory:", "--allow-destructive"},
		{"migrate", "reset", "--database-path", ":memory:", "--allow-destructive"},
		{"migrate", "refresh", "--database-path", ":memory:", "--allow-destructive"},
		{"migrate", "fresh", "--database-path", ":memory:", "--allow-destructive"},
	} {
		stdout.Reset()
		stderr.Reset()
		if exitCode := cli.Run(ctx, invocation, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), "requires a persistent SQLite file") {
			t.Fatalf("ridu %v memory target = exit %d, stderr %q", invocation, exitCode, stderr.String())
		}
	}

	t.Setenv("RIDU_SQLITE_PATH", "relative-from-environment.sqlite")
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(ctx, []string{"migrate", "status"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), "RIDU_SQLITE_PATH must be an absolute") {
		t.Fatalf("relative RIDU_SQLITE_PATH = exit %d, stderr %q", exitCode, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(target, "relative-from-environment.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("relative RIDU_SQLITE_PATH created a database: %v", err)
	}
	t.Setenv("RIDU_SQLITE_PATH", "")

	for _, invocation := range [][]string{
		{"migrate", "up", "--database-path", databasePath},
		{"migrate", "create", "--name", "data-only", "--transform", "seed-data"},
		{"migrate", "verify"},
		{"migrate", "plan", "--database-path", databasePath, "--json"},
		{"migrate", "up", "--database-path", databasePath},
		{"migrate", "status", "--database-path", databasePath},
		{"migrate", "down", "--database-path", databasePath, "--allow-destructive"},
		{"migrate", "up", "--database-path", databasePath},
		{"migrate", "refresh", "--database-path", databasePath, "--allow-destructive"},
		{"migrate", "reset", "--database-path", databasePath, "--allow-destructive"},
		{"migrate", "fresh", "--database-path", databasePath, "--allow-destructive"},
	} {
		stdout.Reset()
		stderr.Reset()
		if exitCode := cli.Run(ctx, invocation, &stdout, &stderr, options); exitCode != 0 {
			t.Fatalf("ridu %v: %s", invocation, stderr.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(ctx, []string{"migrate", "status", "--database-path", databasePath, "--json"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("final SQLite status: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), `"applied": true`) {
		t.Fatalf("final SQLite status = %s", stdout.String())
	}
	if _, err := os.Stat(databaseFile); err != nil {
		t.Fatalf("relative SQLite file URI did not stay rooted at the project across direct and delegated lifecycle paths: %v", err)
	}
}

func TestSQLiteCanonicalAuthUpgradeCLIRequiresDestructiveCreationApproval(t *testing.T) {
	ctx := context.Background()
	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, "sqlite-canonical-auth")
	setFrameworkProxy(t, frameworkRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	if exitCode := cli.Run(ctx, []string{"new", "--database", "sqlite", "--module", "example.com/sqlite/canonical-auth", target}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu new: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(ctx, []string{"generate"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu generate: %s", stderr.String())
	}

	directory := filepath.Join(target, "migrations")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("read ordinary SQLite initial migration = %d files, %v", len(files), err)
	}
	legacy := files[0].Artifact
	if err := os.Remove(files[0].Path); err != nil {
		t.Fatal(err)
	}
	legacy.Planner.Version = "1.0.0"
	if _, err := migrationartifact.Create(directory, legacy.Name, legacy, time.Unix(1, 0)); err != nil {
		t.Fatalf("publish frozen SQLite v1 fixture: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(ctx, []string{"migrate", "create", "--name", "canonical-auth"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), "explicit safety resolution") {
		t.Fatalf("unapproved canonical auth migration = exit %d, stdout %q, stderr %q", exitCode, stdout.String(), stderr.String())
	}
	files, err = migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("unapproved canonical auth migration published history = %d files, %v", len(files), err)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(ctx, []string{"migrate", "create", "--name", "canonical-auth", "--allow-destructive"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("approved canonical auth migration = exit %d, stderr %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "destructive\tRIDU_AUTH_IDENTITY_CANONICALIZATION") {
		t.Fatalf("approved canonical auth migration output = %q", stdout.String())
	}
	files, err = migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 2 {
		t.Fatalf("approved canonical auth migration history = %d files, %v", len(files), err)
	}
	foundDestructive := false
	for _, risk := range files[1].Artifact.Risks {
		foundDestructive = foundDestructive || risk.Code == "RIDU_AUTH_IDENTITY_CANONICALIZATION" && risk.Level == ridumigration.RiskDestructive
	}
	if !foundDestructive {
		t.Fatalf("approved canonical auth migration risks = %#v", files[1].Artifact.Risks)
	}

	databasePath := filepath.Join(target, ".ridu", "canonical-auth.sqlite")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(ctx, []string{"migrate", "up", "--database-path", databasePath}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("apply approved canonical auth migration without a second approval = exit %d, stderr %q", exitCode, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := cli.Run(ctx, []string{"migrate", "create", "--name", "unchanged"}, &stdout, &stderr, options); exitCode != 1 || !strings.Contains(stderr.String(), "schema is current") || strings.Contains(stderr.String(), "explicit safety resolution") {
		t.Fatalf("unchanged current-planner creation = exit %d, stdout %q, stderr %q", exitCode, stdout.String(), stderr.String())
	}
}

func TestMigrateMaintenanceAdmissionIsLimitedToUpAndVerify(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	root := t.TempDir()
	project := `version = 1
database = "postgres"
entry = "./cmd/server"
schema = "./generated/ridu.schema.json"
migrations = "./migrations"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
`
	if err := os.WriteFile(filepath.Join(root, "ridu.toml"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	options := cli.Options{WorkingDirectory: root, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	if code := cli.Run(context.Background(), []string{"migrate", "status", "--allow-maintenance"}, &stdout, &stderr, options); code != 2 || !strings.Contains(stderr.String(), "does not accept --allow-maintenance") {
		t.Fatalf("status maintenance flag = code %d stderr %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), []string{"migrate", "up", "--allow-maintenance"}, &stdout, &stderr, options); code != 2 || !strings.Contains(stderr.String(), "PostgreSQL URL is required") {
		t.Fatalf("up maintenance flag = code %d stderr %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), []string{"migrate", "status", "--lock-timeout", "1s"}, &stdout, &stderr, options); code != 2 || !strings.Contains(stderr.String(), "runner timeout") {
		t.Fatalf("status timeout flag = code %d stderr %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), []string{"migrate", "verify", "--stop-after-phase", "phase-001"}, &stdout, &stderr, options); code != 2 || !strings.Contains(stderr.String(), "does not accept stop boundaries") {
		t.Fatalf("verify stop flag = code %d stderr %q", code, stderr.String())
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("canonicalize %s: %v", path, err)
	}
	return canonical
}

func newProjectTarget(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(canonicalPath(t, t.TempDir()), name)
}

func setFrameworkProxy(t *testing.T, frameworkRoot string) {
	t.Helper()
	if testing.Short() {
		t.Skip("release-path fixture is disabled in short mode")
	}
	frameworkRoot = filepath.Clean(frameworkRoot)
	sharedFrameworkTestEnvironment.once.Do(func() {
		sharedFrameworkTestEnvironment.frameworkRoot = frameworkRoot
		sharedFrameworkTestEnvironment.clean = os.Getenv("RIDU_TEST_CLEAN_CACHE") == "true"
		sharedFrameworkTestEnvironment.root = filepath.Join(frameworkRoot, ".ridu", "test-cache", "cli")
		if sharedFrameworkTestEnvironment.clean {
			sharedFrameworkTestEnvironment.root, sharedFrameworkTestEnvironment.err = os.MkdirTemp("", "ridu-cli-release-fixture-")
		}
		if sharedFrameworkTestEnvironment.err != nil {
			return
		}
		sharedFrameworkTestEnvironment.moduleCache = filepath.Join(sharedFrameworkTestEnvironment.root, "go-module-cache")
		sharedFrameworkTestEnvironment.buildCache = filepath.Join(sharedFrameworkTestEnvironment.root, "go-build-cache")
		for _, path := range []string{sharedFrameworkTestEnvironment.moduleCache, sharedFrameworkTestEnvironment.buildCache} {
			if sharedFrameworkTestEnvironment.err = os.MkdirAll(path, 0o755); sharedFrameworkTestEnvironment.err != nil {
				return
			}
		}
		proxyRoot := filepath.Join(sharedFrameworkTestEnvironment.root, "proxy")
		sharedFrameworkTestEnvironment.proxyURL, sharedFrameworkTestEnvironment.version, sharedFrameworkTestEnvironment.err = frameworkproxy.PublishSnapshot(frameworkRoot, proxyRoot)
	})
	if sharedFrameworkTestEnvironment.err != nil {
		t.Fatalf("prepare shared CLI release fixture: %v", sharedFrameworkTestEnvironment.err)
	}
	if sharedFrameworkTestEnvironment.frameworkRoot != frameworkRoot {
		t.Fatalf(
			"shared CLI release fixture is already configured for %s@%s, not %s@%s",
			sharedFrameworkTestEnvironment.frameworkRoot,
			sharedFrameworkTestEnvironment.version,
			frameworkRoot,
			testReleaseVersion,
		)
	}

	t.Setenv("GOWORK", "off")
	// Captured source bytes determine the immutable version, so unchanged modules
	// can reuse this checkout's dedicated caches across runs. Clean-release checks
	// explicitly request empty disposable caches with RIDU_TEST_CLEAN_CACHE=true.
	testReleaseVersion = sharedFrameworkTestEnvironment.version
	t.Setenv("GOMODCACHE", sharedFrameworkTestEnvironment.moduleCache)
	t.Setenv("GOCACHE", sharedFrameworkTestEnvironment.buildCache)
	t.Setenv("GOPROXY", sharedFrameworkTestEnvironment.proxyURL+",https://proxy.golang.org,direct")
	t.Setenv("GONOSUMDB", "github.com/riducms/ridu")
}

func removeSharedFrameworkTestEnvironment(root string) error {
	if err := filepath.Walk(root, func(path string, info os.FileInfo, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		return os.Chmod(path, 0o700)
	}); err != nil {
		return err
	}
	return os.RemoveAll(root)
}
