package generate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/project"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestInstallWritesChecksAndPreservesUnchangedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated", "ridu.schema.json")
	content := []byte("first\n")
	changed, err := install(path, content, false)
	if err != nil || !changed {
		t.Fatalf("initial install = %v, %v; want changed", changed, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	changed, err = install(path, content, false)
	if err != nil || changed {
		t.Fatalf("unchanged install = %v, %v; want unchanged", changed, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged generation replaced the artifact")
	}
	if _, err := install(path, []byte("second\n"), true); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("check drift error = %v", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != "first\n" {
		t.Fatalf("check mode changed artifact to %q", actual)
	}
}

func TestInstallFailureDoesNotOverwriteExistingArtifact(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "schema.json")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	renameFailure := func(_, _ string) error { return errors.New("simulated rename failure") }
	if _, err := installWithRename(path, []byte("replacement\n"), false, renameFailure); err == nil {
		t.Fatal("install with rename failure succeeded, want error")
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != "original\n" {
		t.Fatalf("unrelated existing artifact changed to %q", actual)
	}
}

func TestInstallArtifactsRollsBackEarlierWrites(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "schema.json")
	second := filepath.Join(root, "client.ts")
	if err := os.WriteFile(first, []byte("original schema\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("original client\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	renames := 0
	renameFailure := func(source, target string) error {
		renames++
		if renames == 2 {
			return errors.New("simulated second rename failure")
		}
		return os.Rename(source, target)
	}
	_, err := installArtifacts([]artifact{
		{path: first, content: []byte("replacement schema\n")},
		{path: second, content: []byte("replacement client\n")},
	}, false, renameFailure)
	if err == nil || !strings.Contains(err.Error(), "simulated second rename failure") {
		t.Fatalf("installArtifacts error = %v", err)
	}

	for path, want := range map[string]string{
		first:  "original schema\n",
		second: "original client\n",
	} {
		actual, readError := os.ReadFile(path)
		if readError != nil {
			t.Fatalf("read %s: %v", path, readError)
		}
		if string(actual) != want {
			t.Fatalf("artifact %s = %q, want %q", path, actual, want)
		}
	}
}

func TestDevelopmentInstallArtifactsRollsBackEarlierWrites(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "schema.json")
	second := filepath.Join(root, "client.ts")
	if err := os.WriteFile(first, []byte("original schema\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("original client\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	renames := 0
	_, err := installArtifactsWithDurability([]artifact{
		{path: first, content: []byte("replacement schema\n")},
		{path: second, content: []byte("replacement client\n")},
	}, false, false, func(source, target string) error {
		renames++
		if renames == 2 {
			return errors.New("simulated development rename failure")
		}
		return os.Rename(source, target)
	})
	if err == nil || !strings.Contains(err.Error(), "simulated development rename failure") {
		t.Fatalf("development install error = %v", err)
	}
	for path, want := range map[string]string{first: "original schema\n", second: "original client\n"} {
		actual, readError := os.ReadFile(path)
		if readError != nil {
			t.Fatal(readError)
		}
		if string(actual) != want {
			t.Fatalf("development artifact %s = %q, want %q", path, actual, want)
		}
	}
}

func TestInstallArtifactsRejectsDuplicateDestinationsBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated", "contract.txt")
	_, err := installArtifacts([]artifact{
		{path: path, content: []byte("first\n")},
		{path: path, content: []byte("second\n")},
	}, false, os.Rename)
	if err == nil || !strings.Contains(err.Error(), "configured more than once") {
		t.Fatalf("duplicate destination error = %v", err)
	}
	if _, statError := os.Stat(path); !os.IsNotExist(statError) {
		t.Fatalf("duplicate destination wrote a file: %v", statError)
	}
}

func TestDiscoverManifestValidatesResponseVersions(t *testing.T) {
	definition := projectfile.File{Root: t.TempDir(), Entry: "cmd/server"}
	runner := func(_ context.Context, _ string, arguments []string) ([]byte, []byte, error) {
		if len(arguments) == 0 || arguments[0] != project.Command {
			t.Fatalf("project arguments = %v, want protocol command first", arguments)
		}
		manifest := schema.NewManifest(schema.Snapshot{
			Version:     schema.CurrentVersion,
			Application: schema.Application{Name: "Fixture"},
			Collections: []schema.Collection{},
			Plugins:     []schema.Plugin{},
		})
		manifestJSON, err := manifest.MarshalJSON()
		if err != nil {
			return nil, nil, err
		}
		response, err := project.EncodeResponse(project.Response{
			ProtocolVersion:  project.ProtocolVersion,
			FrameworkVersion: "old",
			ManifestVersion:  uint32(schema.CurrentVersion),
			Manifest:         manifestJSON,
		})
		return response, nil, err
	}
	_, err := discoverProjectMetadata(context.Background(), definition, "new", runner)
	if err == nil || !strings.Contains(err.Error(), "install matching Ridu versions") {
		t.Fatalf("discover mismatch error = %v", err)
	}
}

func TestRunResolvedUsesTheSuppliedExecutableManifest(t *testing.T) {
	root := t.TempDir()
	definition := projectfile.File{Root: root, Schema: "generated/ridu.schema.json"}
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Precompiled development project"},
		Collections: []schema.Collection{},
		Plugins:     []schema.Plugin{},
	})

	result, err := RunResolved(definition, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 2 || !result.Artifacts[0].Changed || result.Manifest.Snapshot().Application.Name != "Precompiled development project" {
		t.Fatalf("resolved generation result = %+v", result)
	}
	encoded, err := os.ReadFile(filepath.Join(root, "generated", "ridu.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := schema.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Snapshot().Application.Name != "Precompiled development project" {
		t.Fatalf("generated application name = %q", decoded.Snapshot().Application.Name)
	}
}

func TestRunResolvedWritesAFileTargetClientBesideGeneratedContracts(t *testing.T) {
	root := t.TempDir()
	definition := projectfile.File{
		Root:   root,
		Schema: "generated/ridu.schema.json",
		Client: "generated/ridu.generated.ts",
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Direct generated client"},
		Collections: []schema.Collection{},
		Plugins:     []schema.Plugin{},
	})

	result, err := RunResolved(definition, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 3 {
		t.Fatalf("artifact count = %d, want schema, Go, and TypeScript", len(result.Artifacts))
	}
	client, err := os.ReadFile(filepath.Join(root, "generated", "ridu.generated.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(client), `from "@riducms/sdk"`) || !strings.Contains(string(client), "export function createClient") {
		t.Fatalf("generated direct client does not expose the typed SDK factory:\n%s", client)
	}
}

func TestRunResolvedProjectWritesAndChecksConfiguredPluginArtifact(t *testing.T) {
	root := t.TempDir()
	definition := projectfile.File{
		Root: root, Schema: "generated/ridu.schema.json",
		GeneratedArtifacts: []projectfile.GeneratedArtifact{{Plugin: "contracts", Name: "schema", Path: "apps/web/generated/ridu.graphql"}},
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Plugin project generation"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{{Key: "contracts"}},
	})
	resolved := ResolvedProject{
		Manifest: manifest,
		PluginArtifacts: []project.Artifact{{
			Plugin: "contracts", Name: "schema", Content: []byte("type Query { status: Boolean! }\n"),
		}},
	}
	result, err := RunResolvedProject(definition, resolved, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 3 {
		t.Fatalf("artifact count = %d, want schema, Go, and GraphQL", len(result.Artifacts))
	}
	path := filepath.Join(root, "apps", "web", "generated", "ridu.graphql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "type Query { status: Boolean! }\n" {
		t.Fatalf("GraphQL schema = %q", content)
	}
	if _, err := RunResolvedProject(definition, resolved, true); err != nil {
		t.Fatalf("unchanged GraphQL drift check: %v", err)
	}
	resolved.PluginArtifacts[0].Content = []byte("type Query { changed: Boolean! }\n")
	if _, err := RunResolvedProject(definition, resolved, true); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("changed GraphQL drift check = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "type Query { status: Boolean! }\n" {
		t.Fatalf("check mode changed GraphQL schema to %q", after)
	}
}

func TestRunResolvedProjectRequiresProviderWhenArtifactPathIsConfigured(t *testing.T) {
	root := t.TempDir()
	definition := projectfile.File{Root: root, Schema: "generated/ridu.schema.json", GeneratedArtifacts: []projectfile.GeneratedArtifact{{Plugin: "contracts", Name: "schema", Path: "generated/ridu.graphql"}}}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Missing GraphQL plugin"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{},
	})
	_, err := RunResolvedProject(definition, ResolvedProject{Manifest: manifest}, false)
	if err == nil || !strings.Contains(err.Error(), "generated.contracts.schema") {
		t.Fatalf("missing plugin provider error = %v", err)
	}
	if _, statError := os.Stat(filepath.Join(root, "generated", "ridu.schema.json")); !os.IsNotExist(statError) {
		t.Fatalf("failed generation installed an earlier artifact: %v", statError)
	}
}

func TestRunResolvedProjectRejectsSymlinkedDestinationOutsideRoot(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "generated")); err != nil {
		t.Fatal(err)
	}
	definition := projectfile.File{
		Root: root, Schema: "contracts/ridu.schema.json",
		GeneratedArtifacts: []projectfile.GeneratedArtifact{{Plugin: "contracts", Name: "schema", Path: "generated/ridu.graphql"}},
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Symlink containment"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{{Key: "contracts"}},
	})
	_, err := RunResolvedProject(definition, ResolvedProject{
		Manifest:        manifest,
		PluginArtifacts: []project.Artifact{{Plugin: "contracts", Name: "schema", Content: []byte("content\n")}},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "outside the project root through a symlink") {
		t.Fatalf("symlink destination error = %v", err)
	}
	if _, statError := os.Stat(filepath.Join(external, "ridu.graphql")); !os.IsNotExist(statError) {
		t.Fatalf("generation wrote outside the project: %v", statError)
	}
}

func TestRunResolvedProjectRejectsOverlappingFileDestinationsBeforeCreatingDirectories(t *testing.T) {
	root := t.TempDir()
	definition := projectfile.File{
		Root: root, Schema: "contracts/ridu.schema.json",
		GeneratedArtifacts: []projectfile.GeneratedArtifact{
			{Plugin: "contracts", Name: "bundle", Path: "generated/contracts"},
			{Plugin: "contracts", Name: "schema", Path: "generated/contracts/schema.graphql"},
		},
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Overlapping outputs"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{{Key: "contracts"}},
	})
	_, err := RunResolvedProject(definition, ResolvedProject{
		Manifest: manifest,
		PluginArtifacts: []project.Artifact{
			{Plugin: "contracts", Name: "bundle", Content: []byte("bundle\n")},
			{Plugin: "contracts", Name: "schema", Content: []byte("schema\n")},
		},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("overlapping destination error = %v", err)
	}
	if _, statError := os.Stat(filepath.Join(root, "generated")); !os.IsNotExist(statError) {
		t.Fatalf("overlap validation created directories: %v", statError)
	}
}

func TestDiscoverProjectCarriesOnlyArtifactsFromManifestPlugins(t *testing.T) {
	definition := projectfile.File{Root: t.TempDir(), Entry: "cmd/server", GeneratedArtifacts: []projectfile.GeneratedArtifact{{Plugin: "graphql", Name: "schema", Path: "generated/schema.graphql"}}}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Executable artifacts"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{{Key: "graphql"}},
	})
	manifestJSON, err := manifest.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	runner := func(_ context.Context, _ string, arguments []string) ([]byte, []byte, error) {
		if len(arguments) < 8 || arguments[1] != "generate" || arguments[len(arguments)-2] != "--artifact" || arguments[len(arguments)-1] != "graphql/schema" {
			t.Fatalf("project arguments = %v", arguments)
		}
		encoded, encodeError := project.EncodeResponse(project.Response{
			ProtocolVersion: project.ProtocolVersion, FrameworkVersion: "test", ManifestVersion: uint32(schema.CurrentVersion), Manifest: manifestJSON,
			Artifacts: []project.Artifact{{Plugin: "graphql", Name: "schema", Content: []byte("type Query { ok: Boolean! }\n")}},
		})
		return encoded, nil, encodeError
	}
	resolved, err := discoverProject(context.Background(), definition, "test", runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.PluginArtifacts) != 1 || resolved.PluginArtifacts[0].Plugin != "graphql" {
		t.Fatalf("resolved artifacts = %#v", resolved.PluginArtifacts)
	}

	runner = func(_ context.Context, _ string, _ []string) ([]byte, []byte, error) {
		encoded, encodeError := project.EncodeResponse(project.Response{
			ProtocolVersion: project.ProtocolVersion, FrameworkVersion: "test", ManifestVersion: uint32(schema.CurrentVersion), Manifest: manifestJSON,
			Artifacts: []project.Artifact{{Plugin: "undeclared", Name: "schema", Content: []byte("content")}},
		})
		return encoded, nil, encodeError
	}
	if _, err := discoverProject(context.Background(), definition, "test", runner); err == nil || !strings.Contains(err.Error(), "absent from the manifest") {
		t.Fatalf("undeclared artifact error = %v", err)
	}
}

func TestDiscoverProjectUsesManifestOperationWithoutConfiguredArtifacts(t *testing.T) {
	definition := projectfile.File{Root: t.TempDir(), Entry: "cmd/server"}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "No generated plugin artifacts"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{{Key: "graphql"}},
	})
	manifestJSON, err := manifest.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	runner := func(_ context.Context, _ string, arguments []string) ([]byte, []byte, error) {
		if len(arguments) < 2 || arguments[1] != "manifest" || slices.Contains(arguments, "--artifact") {
			t.Fatalf("unconfigured project arguments = %v", arguments)
		}
		encoded, encodeError := project.EncodeResponse(project.Response{
			ProtocolVersion: project.ProtocolVersion, FrameworkVersion: "test", ManifestVersion: uint32(schema.CurrentVersion), Manifest: manifestJSON,
		})
		return encoded, nil, encodeError
	}
	resolved, err := discoverProject(context.Background(), definition, "test", runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.PluginArtifacts) != 0 || resolved.Manifest.Snapshot().Application.Name != "No generated plugin artifacts" {
		t.Fatalf("unconfigured resolved project = %#v", resolved)
	}
}

func TestDiscoverProjectMetadataDoesNotGenerateConfiguredPluginArtifacts(t *testing.T) {
	definition := projectfile.File{
		Root: t.TempDir(), Entry: "cmd/server",
		GeneratedArtifacts: []projectfile.GeneratedArtifact{{Plugin: "graphql", Name: "schema", Path: "generated/schema.graphql"}},
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Migration metadata"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{{Key: "graphql"}},
	})
	manifestJSON, err := manifest.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	descriptor := migration.DataTransformDescriptor{Name: "normalize-posts", Checksum: migration.DataTransformChecksum([]byte("normalize-posts"))}
	runner := func(_ context.Context, _ string, arguments []string) ([]byte, []byte, error) {
		if len(arguments) < 2 || arguments[1] != "manifest" || slices.Contains(arguments, "--artifact") {
			t.Fatalf("metadata project arguments = %v", arguments)
		}
		encoded, encodeError := project.EncodeResponse(project.Response{
			ProtocolVersion: project.ProtocolVersion, FrameworkVersion: "test", ManifestVersion: uint32(schema.CurrentVersion),
			Manifest: manifestJSON, DataTransforms: []migration.DataTransformDescriptor{descriptor},
		})
		return encoded, nil, encodeError
	}
	resolved, err := discoverProjectMetadata(context.Background(), definition, "test", runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.PluginArtifacts) != 0 || len(resolved.DataTransforms) != 1 || resolved.DataTransforms[0] != descriptor {
		t.Fatalf("resolved migration metadata = %#v", resolved)
	}
}

func TestDiscoverProjectMetadataReportsProjectFailure(t *testing.T) {
	definition := projectfile.File{Root: t.TempDir(), Entry: "cmd/server"}
	runner := func(_ context.Context, _ string, _ []string) ([]byte, []byte, error) {
		return nil, []byte("compile failed"), errors.New("exit 1")
	}
	_, err := discoverProjectMetadata(context.Background(), definition, "test", runner)
	if err == nil || !strings.Contains(err.Error(), "compile failed") {
		t.Fatalf("discover failure error = %v", err)
	}
}

func TestRunProjectMigrationKeepsDatabaseURLOutOfArgumentsAndRedactsDiagnostics(t *testing.T) {
	databaseURL := "mongodb://project-user:project-password@example.invalid:27017/ridu?replicaSet=ridu-rs0&proxyPassword=query%2Dsecret"
	t.Setenv("DATABASE_URL", "mongodb://ambient-user:ambient-password@ambient.invalid/ridu")
	t.Setenv("RIDU_SQLITE_PATH", "/tmp/ambient-secret.sqlite")
	t.Setenv(project.MigrationDatabaseURLEnvironment, "mongodb://stale-user:stale-password@stale.invalid/ridu")
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Project migration"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{},
	})
	manifestJSON, err := manifest.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	response, err := project.EncodeResponse(project.Response{
		ProtocolVersion: project.ProtocolVersion, FrameworkVersion: "test", ManifestVersion: uint32(schema.CurrentVersion), Manifest: manifestJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := migration.ProjectRequest{
		Action: migration.ProjectVerify, DatabaseURL: databaseURL, Directory: "/workspace/migrations",
		AllowInsecureDatabase: true, AllowMaintenance: true, AllowUnbounded: true,
		LockWait: 7 * time.Second, OperationTimeout: 2 * time.Minute,
		LockTimeout: 3 * time.Second, StatementTimeout: 4 * time.Minute, BatchTimeout: 5 * time.Second,
		IdleTransactionTimeout: 6 * time.Second, StopAfterPhase: "phase-002",
	}
	definition := projectfile.File{Root: "/workspace", Entry: "cmd/server"}
	var executable string
	runner := func(_ context.Context, directory string, environment []string, name string, arguments []string) ([]byte, []byte, error) {
		if directory != definition.Root {
			t.Fatalf("project migration directory = %q", directory)
		}
		if name == "go" {
			if len(arguments) != 4 || arguments[0] != "build" || arguments[1] != "-o" || arguments[3] != "./cmd/server" {
				t.Fatalf("project migration build arguments = %v", arguments)
			}
			executable = arguments[2]
			for _, variable := range environment {
				name, _, _ := strings.Cut(variable, "=")
				if name == "DATABASE_URL" || name == "RIDU_SQLITE_PATH" || name == project.MigrationDatabaseURLEnvironment {
					t.Fatalf("project migration build inherited database configuration through %s", name)
				}
			}
			return nil, nil, nil
		}
		if executable == "" || name != executable {
			t.Fatalf("project migration executed %q, want compiled executable %q", name, executable)
		}
		joinedArguments := strings.Join(arguments, " ")
		for _, forbidden := range []string{databaseURL, "project-user", "project-password", "example.invalid", "--database-url"} {
			if strings.Contains(joinedArguments, forbidden) {
				t.Fatalf("project migration arguments exposed %q: %v", forbidden, arguments)
			}
		}
		for _, expected := range []string{"--allow-insecure-database", "--allow-maintenance", "--allow-unbounded"} {
			if !slices.Contains(arguments, expected) {
				t.Fatalf("project migration arguments omitted %s: %v", expected, arguments)
			}
		}
		if !strings.Contains(joinedArguments, "--lock-wait 7s") || !strings.Contains(joinedArguments, "--operation-timeout 2m0s") ||
			!strings.Contains(joinedArguments, "--lock-timeout 3s") || !strings.Contains(joinedArguments, "--statement-timeout 4m0s") ||
			!strings.Contains(joinedArguments, "--batch-timeout 5s") || !strings.Contains(joinedArguments, "--idle-transaction-timeout 6s") ||
			!strings.Contains(joinedArguments, "--stop-after-phase phase-002") {
			t.Fatalf("project migration arguments omitted runtime bounds: %v", arguments)
		}
		selected := ""
		for _, variable := range environment {
			name, value, _ := strings.Cut(variable, "=")
			switch name {
			case "DATABASE_URL", "RIDU_SQLITE_PATH":
				t.Fatalf("project migration inherited ambient database configuration through %s", name)
			case project.MigrationDatabaseURLEnvironment:
				if selected != "" {
					t.Fatal("project migration received its private database selection more than once")
				}
				selected = value
			}
		}
		if selected != databaseURL {
			t.Fatalf("private project migration database selection = %q", selected)
		}
		return response, nil, nil
	}
	if err := runProjectMigration(context.Background(), definition, "test", request, runner); err != nil {
		t.Fatal(err)
	}

	failure := func(_ context.Context, _ string, _ []string, name string, _ []string) ([]byte, []byte, error) {
		if name == "go" {
			return nil, nil, nil
		}
		return nil,
			[]byte("failed " + databaseURL + " project-user project-password example.invalid:27017 query-secret query%2Dsecret"),
			fmt.Errorf("connect %s as project-user with project-password through example.invalid:27017 and query-secret/query%%2Dsecret: %w", databaseURL, errors.New("exit status 1"))
	}
	err = runProjectMigration(context.Background(), definition, "test", request, failure)
	if err == nil || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("project migration failure = %v, want a redacted diagnostic", err)
	}
	for _, forbidden := range []string{databaseURL, "project-user", "project-password", "example.invalid:27017", "query-secret", "query%2Dsecret"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("project migration failure exposed %q: %v", forbidden, err)
		}
	}
}

func TestRunProjectMigrationValidatesManifestVersion(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Project migration"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{},
	})
	manifestJSON, err := manifest.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	response, err := project.EncodeResponse(project.Response{
		ProtocolVersion: project.ProtocolVersion, FrameworkVersion: "test", ManifestVersion: uint32(schema.CurrentVersion) + 1, Manifest: manifestJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := func(_ context.Context, _ string, _ []string, name string, _ []string) ([]byte, []byte, error) {
		if name == "go" {
			return nil, nil, nil
		}
		return response, nil, nil
	}
	err = runProjectMigration(context.Background(), projectfile.File{Root: t.TempDir(), Entry: "."}, "test", migration.ProjectRequest{
		Action: migration.ProjectVerify, Directory: "migrations",
	}, runner)
	if err == nil || !strings.Contains(err.Error(), "manifest version") {
		t.Fatalf("project migration manifest mismatch = %v", err)
	}
}

func TestRunProjectMigrationCancellationStopsCompiledProject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/project-migration-cancellation\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "project-started")
	source := `package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	file, err := os.OpenFile(os.Getenv("RIDU_TEST_PROJECT_STARTED"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		os.Exit(2)
	}
	defer file.Close()
	for index := 0; index < 500; index++ {
		_, _ = fmt.Fprintln(file, index)
		_ = file.Sync()
		time.Sleep(20 * time.Millisecond)
	}
}
`
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RIDU_TEST_PROJECT_STARTED", marker)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelled := make(chan time.Time, 1)
	go func() {
		deadline := time.NewTimer(20 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-deadline.C:
				cancelled <- time.Time{}
				cancel()
				return
			case <-ticker.C:
				if info, err := os.Stat(marker); err == nil && info.Size() > 0 {
					cancelledAt := time.Now()
					cancel()
					cancelled <- cancelledAt
					return
				}
			}
		}
	}()
	err := RunProjectMigration(ctx, projectfile.File{Root: root, Entry: "."}, "test", migration.ProjectRequest{
		Action: migration.ProjectVerify, Directory: filepath.Join(root, "migrations"),
	})
	cancelledAt := <-cancelled
	if cancelledAt.IsZero() {
		t.Fatalf("compiled project did not start before timeout: %v", err)
	}
	if err == nil {
		t.Fatal("project migration succeeded after cancellation")
	}
	if elapsed := time.Since(cancelledAt); elapsed > 3*time.Second {
		t.Fatalf("project migration returned %s after cancellation; compiled child may still be running", elapsed)
	}
	before, statErr := os.Stat(marker)
	if statErr != nil {
		t.Fatal(statErr)
	}
	time.Sleep(150 * time.Millisecond)
	after, statErr := os.Stat(marker)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if after.Size() != before.Size() {
		t.Fatalf("compiled project continued writing after cancellation: size %d -> %d", before.Size(), after.Size())
	}
}

func TestResolveProjectMetadataExecutableRunsThePrecompiledProjectProtocol(t *testing.T) {
	root := t.TempDir()
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Compiled project protocol"},
		Collections: []schema.Collection{},
		Plugins:     []schema.Plugin{},
	})
	manifestJSON, err := manifest.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	response, err := project.EncodeResponse(project.Response{
		ProtocolVersion:  project.ProtocolVersion,
		FrameworkVersion: "test-framework",
		ManifestVersion:  uint32(schema.CurrentVersion),
		Manifest:         manifestJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/ridu-project-executable-test\n\ngo 1.25.13\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
)

func main() {
	if os.Getenv("DATABASE_URL") != "" || os.Getenv("RIDU_SQLITE_PATH") != "" || os.Getenv(%q) != "" {
		fmt.Fprint(os.Stderr, "project command received database configuration")
		os.Exit(1)
	}
	fmt.Print(%q)
}
`, project.MigrationDatabaseURLEnvironment, response)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "project")
	command := exec.Command("go", "build", "-o", executable, ".")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build project protocol fixture: %v\n%s", err, output)
	}
	t.Setenv("DATABASE_URL", "postgres://database-user:database-secret@example.invalid/ridu")
	t.Setenv("RIDU_SQLITE_PATH", "/database/secret/ridu.sqlite")
	t.Setenv(project.MigrationDatabaseURLEnvironment, "mongodb://private-user:private-secret@example.invalid/ridu")
	resolved, err := ResolveProjectMetadataExecutable(
		context.Background(),
		projectfile.File{Root: root},
		"test-framework",
		executable,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Manifest.Snapshot().Application.Name != "Compiled project protocol" {
		t.Fatalf("resolved application name = %q", resolved.Manifest.Snapshot().Application.Name)
	}
}

func TestAdminPluginRegistryUsesValidatedManifestMetadata(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Plugins"},
		Plugins: []schema.Plugin{
			{Key: "backend-only"},
			{Key: "color", FieldTypes: []schema.PluginFieldType{{Key: "swatch", TypeScriptPackage: "@example/color-admin/value", TypeScriptOutput: "Color", TypeScriptInput: "ColorInput"}}, Admin: &schema.PluginAdmin{Package: "@example/color-admin", Export: "colorAdminPlugin", APIVersion: schema.CurrentAdminPluginAPIVersion, PairingVersion: 7, Assets: []string{"styles.css"}}},
		},
	})
	registry := string(adminPluginRegistry(manifest))
	for _, expected := range []string{
		`import { resolveAdminPluginPairs, type PluginFieldRegistration } from "@riducms/plugin";`,
		`import { colorAdminPlugin as riduAdminPlugin0 } from "@example/color-admin";`,
		`key: "color"`,
		`apiVersion: 1`,
		`riduAdminPlugin0.fields["swatch"] satisfies PluginFieldRegistration<import("@example/color-admin/value").Color, import("@example/color-admin/value").ColorInput>;`,
		`fieldTypes: ["swatch"]`,
		`pairingVersion: 7`,
		`import "@example/color-admin/styles.css";`,
		`generatedAdminPlugins = resolvedAdminPluginPairs.plugins`,
	} {
		if !strings.Contains(registry, expected) {
			t.Fatalf("generated registry missing %q:\n%s", expected, registry)
		}
	}
	if strings.Contains(registry, "backend-only") || strings.Contains(registry, "richTextFieldPlugin") {
		t.Fatalf("generated registry contains a hard-coded or backend-only plugin:\n%s", registry)
	}
}

func TestAdminPluginRegistryWithoutPairsHasNoRuntimeImports(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "No pairs"}, Plugins: []schema.Plugin{{Key: "backend-only"}}})
	registry := string(adminPluginRegistry(manifest))
	if registry != "// Code generated by Ridu. DO NOT EDIT.\nexport const generatedAdminPlugins = [] as const;\n" {
		t.Fatalf("empty generated registry = %q", registry)
	}
}

func TestOpenAPIIsDeterministicAndContainsCRUDPaths(t *testing.T) {
	pluginPath, err := query.NewPath("accent")
	if err != nil {
		t.Fatal(err)
	}
	joinPath, err := query.NewPath("category")
	if err != nil {
		t.Fatal(err)
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{Name: "OpenAPI fixture", Localization: &schema.LocalizationSettings{
			Locales:       []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}},
			DefaultLocale: "en", Fallback: true,
		}, Endpoints: []schema.Endpoint{
			{Method: "POST", Path: "/maintenance/:region", Summary: "Run regional maintenance"},
			{Method: "GET", Path: "/preferences/:name", Summary: "Custom preference read"},
			{Method: "GET", Path: "/foo/:id"},
			{Method: "POST", Path: "/foo/id"},
			{Method: "CONNECT", Path: "/tunnel", Summary: "Open a tunnel"},
		}},
		Collections: []schema.Collection{{
			ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
			Capabilities: schema.Capabilities{Auth: true, Upload: true, Versions: true}, Auth: &schema.AuthSettings{}, Upload: &schema.UploadSettings{MaxFileSize: 1024, MimeTypes: []string{"image/png"}}, Versions: &schema.VersionSettings{Drafts: true},
			Fields: []schema.Field{
				{ID: "post-title", Name: "title", Type: schema.FieldTypeText, Required: true, Localized: true},
				{ID: "post-accent", Name: "accent", Path: pluginPath, Type: schema.FieldTypePlugin, Required: true, Plugin: &schema.PluginField{Key: "color"}},
				{ID: "post-related", Name: "related", Type: schema.FieldTypeJoin, Category: schema.FieldCategoryPresentation, Join: &schema.JoinField{CollectionID: "posts", CollectionSlug: "posts", On: joinPath, Limit: 10}},
			},
			Endpoints: []schema.Endpoint{{Method: "GET", Path: "/:id/tracking", Summary: "Track a post"}, {Method: "GET", Path: "/:slug", Summary: "Custom document read"}},
		}, {
			ID: "articles", Slug: "articles", Labels: schema.CollectionLabels{Singular: "Article", Plural: "Articles"},
			Capabilities: schema.Capabilities{Versions: true}, Versions: &schema.VersionSettings{Drafts: true},
			Fields: []schema.Field{{ID: "article-title", Name: "title", Type: schema.FieldTypeText, Required: true}},
		}},
		Globals: []schema.Global{{
			ID: "global-site-settings", Slug: "site-settings", Labels: schema.CollectionLabels{Singular: "Site settings", Plural: "Site settings"},
			Capabilities: schema.Capabilities{Global: true, Versions: true}, Versions: &schema.VersionSettings{Drafts: true},
			Fields:    []schema.Field{{ID: "site-name", Name: "siteName", Type: schema.FieldTypeText, Required: true, Localized: true}},
			Endpoints: []schema.Endpoint{{Method: "PUT", Path: "/refresh/:locale"}},
		}},
		Plugins: []schema.Plugin{{Key: "color", FieldTypes: []schema.PluginFieldType{{Key: "color", JSONSchema: []byte(`{"type":"string","pattern":"^#[0-9A-F]{6}$"}`)}}, Endpoints: []schema.PluginEndpoint{{Method: "GET", Path: "palette", Summary: "List colors"}, {Method: "POST", Path: "palette", Summary: "Create color"}}}},
	})
	first, err := openAPI(manifest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := openAPI(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("OpenAPI generation is not deterministic")
	}
	for _, expected := range []string{`"openapi": "3.1.0"`, `"/api/collections/posts"`, `"createposts"`, `"/api/access/collections/posts/selection"`, `"resolveSelectionposts"`, `"/api/collections/posts/{id}/joins/{field}"`, `"mutateJoinposts"`, `"/api/collections/posts/remote-upload"`, `"remoteUploadposts"`, `"/api/collections/posts/{id}/image"`, `"updateUploadImageposts"`, `"/api/collections/posts/{id}/versions/{revision}"`, `"/api/collections/posts/{id}/schedule/{jobId}"`, `"/api/auth/posts/bootstrap"`, `"authBootstrapposts"`, `"/api/globals/site-settings"`, `"/api/globals/site-settings/versions/{revision}"`, `"updateglobal-site-settings"`, `"/api/preferences"`, `"/api/plugins/color/palette"`, `"color-get-palette"`, `"color-post-palette"`, `"/api/maintenance/{region}"`, `"/api/collections/posts/{id}/tracking"`, `"/api/globals/site-settings/refresh/{locale}"`, `"Run regional maintenance"`, `"Custom PUT endpoint"`, `"x-ridu-connect"`, `"Endpoint-defined successful response"`, `"pattern": "^#[0-9A-F]{6}$"`} {
		if !strings.Contains(string(first), expected) {
			t.Fatalf("OpenAPI output missing %s:\n%s", expected, first)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	bootstrapOperation := requiredOpenAPIMap(t,
		requiredOpenAPIMap(t, requiredOpenAPIMap(t, decoded, "paths"), "/api/auth/posts/bootstrap"),
		"get",
	)
	bootstrapResponseSchema := requiredOpenAPIMap(t,
		requiredOpenAPIMap(t,
			requiredOpenAPIMap(t,
				requiredOpenAPIMap(t, requiredOpenAPIMap(t, bootstrapOperation, "responses"), "200"),
				"content",
			),
			"application/json",
		),
		"schema",
	)
	if bootstrapResponseSchema["$ref"] != "#/components/schemas/AuthBootstrapEnvelope" {
		t.Fatalf("auth bootstrap response schema = %#v", bootstrapResponseSchema)
	}
	joinOperation := requiredOpenAPIMap(t,
		requiredOpenAPIMap(t, requiredOpenAPIMap(t, decoded, "paths"), "/api/collections/posts/{id}/joins/{field}"),
		"patch",
	)
	requestSchema := requiredOpenAPIMap(t,
		requiredOpenAPIMap(t,
			requiredOpenAPIMap(t, requiredOpenAPIMap(t, joinOperation, "requestBody"), "content"),
			"application/json",
		),
		"schema",
	)
	if requestSchema["$ref"] != "#/components/schemas/JoinMutationInput" {
		t.Fatalf("join request schema = %#v", requestSchema)
	}
	responseSchema := requiredOpenAPIMap(t,
		requiredOpenAPIMap(t,
			requiredOpenAPIMap(t,
				requiredOpenAPIMap(t, requiredOpenAPIMap(t, joinOperation, "responses"), "200"),
				"content",
			),
			"application/json",
		),
		"schema",
	)
	if responseSchema["$ref"] != "#/components/schemas/JoinMutationposts" {
		t.Fatalf("join response schema = %#v", responseSchema)
	}
	schemas := requiredOpenAPIMap(t, requiredOpenAPIMap(t, decoded, "components"), "schemas")
	inputProperties := requiredOpenAPIMap(t, requiredOpenAPIMap(t, schemas, "JoinMutationInput"), "properties")
	additionItems := requiredOpenAPIMap(t, requiredOpenAPIMap(t, inputProperties, "additions"), "items")
	joinProperties := requiredOpenAPIMap(t, requiredOpenAPIMap(t, schemas, "JoinMutationposts"), "properties")
	if additionItems["type"] != "string" || requiredOpenAPIMap(t, joinProperties, "doc")["$ref"] != "#/components/schemas/posts" || requiredOpenAPIMap(t, joinProperties, "added")["type"] != "integer" {
		t.Fatalf("join component schemas = input %#v, response %#v", inputProperties, joinProperties)
	}
	selectionOperation := requiredOpenAPIMap(t,
		requiredOpenAPIMap(t, requiredOpenAPIMap(t, decoded, "paths"), "/api/access/collections/posts/selection"),
		"post",
	)
	selectionRequest := requiredOpenAPIMap(t,
		requiredOpenAPIMap(t,
			requiredOpenAPIMap(t, requiredOpenAPIMap(t, selectionOperation, "requestBody"), "content"),
			"application/json",
		),
		"schema",
	)
	selectionResponse := requiredOpenAPIMap(t,
		requiredOpenAPIMap(t,
			requiredOpenAPIMap(t,
				requiredOpenAPIMap(t, requiredOpenAPIMap(t, selectionOperation, "responses"), "200"),
				"content",
			),
			"application/json",
		),
		"schema",
	)
	selectionProperties := requiredOpenAPIMap(t, requiredOpenAPIMap(t, schemas, "CollectionSelectionEnvelope"), "properties")
	selectionItems := requiredOpenAPIMap(t, requiredOpenAPIMap(t, selectionProperties, "items"), "items")
	if selectionRequest["$ref"] != "#/components/schemas/CollectionSelectionInput" || selectionResponse["$ref"] != "#/components/schemas/CollectionSelectionEnvelope" || selectionItems["$ref"] != "#/components/schemas/CollectionSelectionItem" {
		t.Fatalf("selection schemas = request %#v, response %#v, items %#v", selectionRequest, selectionResponse, selectionItems)
	}
	paths := requiredOpenAPIMap(t, decoded, "paths")
	maintenancePath := requiredOpenAPIMap(t, paths, "/api/maintenance/{region}")
	maintenanceOperation := requiredOpenAPIMap(t, maintenancePath, "post")
	maintenanceParameters, ok := maintenanceOperation["parameters"].([]any)
	if !ok || len(maintenanceParameters) != 1 || requiredOpenAPIMap(t, maintenanceParameters[0].(map[string]any), "schema")["type"] != "string" {
		t.Fatalf("custom endpoint parameters = %#v", maintenanceOperation["parameters"])
	}
	if _, duplicate := paths["/api/collections/posts/{slug}"]; duplicate {
		t.Fatal("custom collection template was not merged with the equivalent built-in path")
	}
	if _, duplicate := paths["/api/preferences/{name}"]; duplicate {
		t.Fatal("custom root template was not merged with the equivalent built-in path")
	}
	if requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/collections/posts/{id}"), "get")["summary"] != "Custom document read" {
		t.Fatal("custom endpoint did not override the equivalent built-in operation")
	}
	if _, invalid := requiredOpenAPIMap(t, paths, "/api/tunnel")["connect"]; invalid {
		t.Fatal("CONNECT was emitted as an invalid OpenAPI Path Item operation")
	}
	if _, exists := requiredOpenAPIMap(t, paths, "/api/tunnel")["x-ridu-connect"]; !exists {
		t.Fatal("CONNECT vendor extension is missing")
	}
	customResponses := requiredOpenAPIMap(t, requiredOpenAPIMap(t, maintenancePath, "post"), "responses")
	if _, exists := customResponses["2XX"]; !exists {
		t.Fatalf("custom endpoint responses = %#v", customResponses)
	}
	operationIDs := make(map[string]string)
	pathShapes := make(map[string]string)
	for path, rawPathItem := range paths {
		shape := openAPIPathShape(path)
		if previous, duplicate := pathShapes[shape]; duplicate {
			t.Fatalf("equivalent OpenAPI paths %s and %s", previous, path)
		}
		pathShapes[shape] = path
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			t.Fatalf("OpenAPI path item %s = %T", path, rawPathItem)
		}
		for method, rawOperation := range pathItem {
			if method == "parameters" {
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				continue
			}
			operationID, _ := operation["operationId"].(string)
			if operationID == "" {
				continue
			}
			if previous, duplicate := operationIDs[operationID]; duplicate {
				t.Fatalf("duplicate OpenAPI operationId %q at %s and %s", operationID, previous, path)
			}
			operationIDs[operationID] = path
		}
	}
	assertOpenAPIIntegerParameter(t, requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/collections/posts"), "get"), "depth", 0, 5)
	assertOpenAPINoParameter(t, requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/collections/posts"), "post"), "draft")
	assertOpenAPIBooleanParameter(t, requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/collections/articles"), "post"), "draft")
	assertOpenAPIIntegerParameter(t, requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/globals/site-settings"), "get"), "depth", 0, 5)
	assertOpenAPIStringEnumParameter(t, requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/collections/posts"), "get"), "locale", []string{"en", "fr", "all"})
	assertOpenAPIStringEnumParameter(t, requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/collections/posts/{id}/duplicate"), "post"), "locale", []string{"en", "fr", "all"})
	assertOpenAPIStringEnumParameter(t, requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/globals/site-settings"), "patch"), "locale", []string{"en", "fr", "all"})
}

func assertOpenAPIBooleanParameter(t *testing.T, operation map[string]any, name string) {
	t.Helper()
	parameters, ok := operation["parameters"].([]any)
	if !ok {
		t.Fatalf("OpenAPI parameters = %#v, want array", operation["parameters"])
	}
	for _, value := range parameters {
		parameter, ok := value.(map[string]any)
		if !ok || parameter["name"] != name || parameter["in"] != "query" {
			continue
		}
		if requiredOpenAPIMap(t, parameter, "schema")["type"] != "boolean" {
			t.Fatalf("OpenAPI %q parameter schema = %#v", name, parameter["schema"])
		}
		return
	}
	t.Fatalf("OpenAPI query parameter %q is missing", name)
}

func assertOpenAPINoParameter(t *testing.T, operation map[string]any, name string) {
	t.Helper()
	parameters, ok := operation["parameters"].([]any)
	if !ok {
		return
	}
	for _, rawParameter := range parameters {
		parameter, ok := rawParameter.(map[string]any)
		if ok && parameter["name"] == name {
			t.Fatalf("OpenAPI query parameter %q must not be advertised", name)
		}
	}
}

func requiredOpenAPIMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI %q = %#v, want object", key, parent[key])
	}
	return value
}

func assertOpenAPIIntegerParameter(t *testing.T, operation map[string]any, name string, minimum, maximum float64) {
	t.Helper()
	parameters, ok := operation["parameters"].([]any)
	if !ok {
		t.Fatalf("OpenAPI parameters = %#v, want array", operation["parameters"])
	}
	for _, value := range parameters {
		parameter, ok := value.(map[string]any)
		if !ok || parameter["name"] != name || parameter["in"] != "query" {
			continue
		}
		schema := requiredOpenAPIMap(t, parameter, "schema")
		if schema["type"] != "integer" || schema["minimum"] != minimum || schema["maximum"] != maximum {
			t.Fatalf("OpenAPI %q parameter schema = %#v", name, schema)
		}
		return
	}
	t.Fatalf("OpenAPI query parameter %q is missing", name)
}

func assertOpenAPIStringEnumParameter(t *testing.T, operation map[string]any, name string, expected []string) {
	t.Helper()
	parameters, ok := operation["parameters"].([]any)
	if !ok {
		t.Fatalf("OpenAPI parameters = %#v, want array", operation["parameters"])
	}
	for _, value := range parameters {
		parameter, ok := value.(map[string]any)
		if !ok || parameter["name"] != name || parameter["in"] != "query" {
			continue
		}
		parameterSchema := requiredOpenAPIMap(t, parameter, "schema")
		encoded, _ := json.Marshal(parameterSchema["enum"])
		want, _ := json.Marshal(expected)
		if parameterSchema["type"] != "string" || string(encoded) != string(want) {
			t.Fatalf("OpenAPI %q parameter schema = %#v", name, parameterSchema)
		}
		return
	}
	t.Fatalf("OpenAPI query parameter %q is missing", name)
}

func TestGeneratedGoModelsIncludeTypedGlobals(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Global Go types"},
		Globals: []schema.Global{{
			ID: "global-site-settings", Slug: "site-settings", Labels: schema.CollectionLabels{Singular: "Site settings", Plural: "Site settings"},
			Capabilities: schema.Capabilities{Global: true},
			Fields:       []schema.Field{{ID: "site-name", Name: "siteName", Type: schema.FieldTypeText, Required: true}},
		}},
	})
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"type SiteSettings struct", "type SiteSettingsUpdate struct", `var SiteSettingsGlobal = core.NewTypedGlobal[SiteSettings, SiteSettingsUpdate]("site-settings")`} {
		if !strings.Contains(string(generated), expected) {
			t.Fatalf("generated Go global missing %q:\n%s", expected, generated)
		}
	}
}

func TestGeneratedGoModelsKeepJoinAndVirtualFieldsOutputOnly(t *testing.T) {
	joinPath, _ := query.NewPath("category")
	virtualPath, _ := query.NewPath("label")
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Computed Go types"},
		Collections: []schema.Collection{
			{ID: "categories", Slug: "categories", Fields: []schema.Field{
				{ID: "categories-posts", Name: "posts", Type: schema.FieldTypeJoin, Category: schema.FieldCategoryPresentation, Join: &schema.JoinField{CollectionID: "posts", CollectionSlug: "posts", On: joinPath, Limit: 10}},
				{ID: "categories-label", Name: "label", Path: virtualPath, Type: schema.FieldTypeVirtual, Category: schema.FieldCategoryPresentation, Virtual: &schema.VirtualField{ValueType: schema.ValueTypeString}},
			}},
			{ID: "posts", Slug: "posts"},
		},
	})
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(goSourceWithoutLineComments(t, generated))
	for _, expected := range []string{"[]Posts `json:\"posts,omitempty\"`", "*string `json:\"label,omitempty\"`", "type CategoriesCreate struct {\n}", "type CategoriesUpdate struct {\n}"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated Go client missing %q:\n%s", expected, text)
		}
	}
}

func TestGeneratedGoModelsTypeMultiSelectAsOptionSlice(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Multi-select Go types"},
		Collections: []schema.Collection{{
			ID: "users", Slug: "users", Labels: schema.CollectionLabels{Singular: "User", Plural: "Users"},
			Fields: []schema.Field{{
				ID: "users-roles", Name: "roles", Type: schema.FieldTypeSelect, Required: true,
				Select: &schema.SelectField{HasMany: true, DefaultValues: []string{"admin"}, Options: []schema.SelectOption{{Value: "admin"}, {Value: "editor"}}},
			}},
		}},
	})
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, expected := range []string{
		`[]UserRoles ` + "`json:\"roles,omitempty\"`",
		`*core.NonNullInput[[]UserRoles] ` + "`json:\"roles,omitempty\"`",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated Go multi-select missing %q:\n%s", expected, text)
		}
	}
}

func TestGeneratedGoNonNullNilableInputsCannotEncodeExplicitNull(t *testing.T) {
	defaultValues := []string{"published"}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Non-null Go inputs"},
		Collections: []schema.Collection{{
			ID: "articles", Slug: "articles", Labels: schema.CollectionLabels{Singular: "Article", Plural: "Articles"},
			Fields: []schema.Field{
				{ID: "required-tags", Name: "requiredTags", Type: schema.FieldTypeSelect, Required: true, Select: &schema.SelectField{HasMany: true}},
				{ID: "default-tags", Name: "defaultTags", Type: schema.FieldTypeSelect, Required: true, Select: &schema.SelectField{HasMany: true, DefaultValues: defaultValues}},
				{ID: "optional-tags", Name: "optionalTags", Type: schema.FieldTypeSelect, Select: &schema.SelectField{HasMany: true}},
				{ID: "required-target", Name: "requiredTarget", Type: schema.FieldTypeRelationship, Required: true, Relationship: &schema.RelationshipField{Polymorphic: true}},
				{ID: "required-payload", Name: "requiredPayload", Type: schema.FieldTypePlugin, Required: true, Plugin: &schema.PluginField{Key: "untyped"}},
				{ID: "optional-payload", Name: "optionalPayload", Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{Key: "untyped"}},
				{ID: "details", Name: "details", Type: schema.FieldTypeGroup, Required: true, Nested: &schema.NestedField{Fields: []schema.Field{
					{ID: "nested-tags", Name: "nestedTags", Type: schema.FieldTypeSelect, Required: true, Select: &schema.SelectField{HasMany: true}},
				}}},
			},
		}},
	})
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	declarations := goDeclarations(t, generated)
	text := declarations["Article"] + "\n" + declarations["ArticleCreate"] + "\n" + declarations["ArticleUpdate"]
	for _, expected := range []string{
		`(?m)^\s*RequiredTags\s+core\.NonNullInput\[\[\]ArticleRequiredTags\]\s+` + "`json:\"requiredTags\"`$",
		`(?m)^\s*DefaultTags\s+\*core\.NonNullInput\[\[\]ArticleDefaultTags\]\s+` + "`json:\"defaultTags,omitempty\"`$",
		`(?m)^\s*OptionalTags\s+\*core\.Input\[\[\]ArticleOptionalTags\]\s+` + "`json:\"optionalTags,omitempty\"`$",
		`(?m)^\s*RequiredTarget\s+ArticleRequiredTargetReferenceInput\s+` + "`json:\"requiredTarget\"`$",
		`(?m)^\s*RequiredPayload\s+core\.NonNullInput\[json\.RawMessage\]\s+` + "`json:\"requiredPayload\"`$",
		`(?m)^\s*OptionalPayload\s+\*core\.Input\[json\.RawMessage\]\s+` + "`json:\"optionalPayload,omitempty\"`$",
	} {
		if !regexp.MustCompile(expected).MatchString(text) {
			t.Fatalf("generated Go non-null nil-able input missing %q:\n%s", expected, text)
		}
	}
	if count := len(regexp.MustCompile(`(?m)^\s*DefaultTags\s+\*core\.NonNullInput\[\[\]ArticleDefaultTags\]`).FindAllString(text, -1)); count != 2 {
		t.Fatalf("defaulted required list wrapper count = %d, want create and update:\n%s", count, text)
	}
	if count := len(regexp.MustCompile(`(?m)^\s*RequiredTags\s+\*core\.NonNullInput\[\[\]ArticleRequiredTags\]`).FindAllString(text, -1)); count != 1 {
		t.Fatalf("required list optional wrapper count = %d, want update only:\n%s", count, text)
	}
	if count := len(regexp.MustCompile(`(?m)^\s*RequiredTarget\s+\*ArticleRequiredTargetReferenceUpdate`).FindAllString(text, -1)); count != 1 {
		t.Fatalf("required polymorphic update pointer count = %d, want update only:\n%s", count, text)
	}
	if count := len(regexp.MustCompile(`(?m)^\s*RequiredPayload\s+\*core\.NonNullInput\[json\.RawMessage\]`).FindAllString(text, -1)); count != 1 {
		t.Fatalf("required raw JSON optional wrapper count = %d, want update only:\n%s", count, text)
	}
	if count := len(regexp.MustCompile(`(?m)^\s*NestedTags\s+core\.NonNullInput\[\[\]ArticleDetailsNestedTags\]`).FindAllString(text, -1)); count != 1 {
		t.Fatalf("nested required list wrapper count = %d, want create only:\n%s", count, text)
	}
	if count := len(regexp.MustCompile(`(?m)^\s*NestedTags\s+\*core\.NonNullInput\[\[\]ArticleDetailsNestedTags\]`).FindAllString(text, -1)); count != 1 {
		t.Fatalf("nested required list optional wrapper count = %d, want update only:\n%s", count, text)
	}
}

func TestGeneratedGoInputsPreserveOmittedAndExplicitNull(t *testing.T) {
	defaultSummary := "Draft"
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Tri-state Go inputs"},
		Collections: []schema.Collection{{
			ID: "articles", Slug: "articles", Labels: schema.CollectionLabels{Singular: "Article", Plural: "Articles"},
			Fields: []schema.Field{
				{ID: "title", Name: "title", Type: schema.FieldTypeText, Required: true},
				{ID: "subtitle", Name: "subtitle", Type: schema.FieldTypeText},
				{ID: "summary", Name: "summary", Type: schema.FieldTypeText, Required: true, Default: &defaultSummary},
				{ID: "details", Name: "details", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{{ID: "note", Name: "note", Type: schema.FieldTypeText}}}},
			},
		}},
	})
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(goSourceWithoutLineComments(t, generated))
	for _, expected := range []string{
		`(?m)^\s*Title\s+string\s+` + "`json:\"title\"`$",
		`(?m)^\s*Subtitle\s+\*core\.Input\[string\]\s+` + "`json:\"subtitle,omitempty\"`$",
		`(?m)^\s*Summary\s+\*string\s+` + "`json:\"summary,omitempty\"`$",
		`(?s)Details\s+\*core\.Input\[struct\s*\{\s*Note\s+\*core\.Input\[string\]\s+` + "`json:\"note,omitempty\"`" + `\s*\}\]\s*` + "`json:\"details,omitempty\"`",
	} {
		if !regexp.MustCompile(expected).MatchString(text) {
			t.Fatalf("generated Go tri-state input missing %q:\n%s", expected, text)
		}
	}
	for _, forbidden := range []string{"Title *core.Input", "Summary *core.Input"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated non-nullable Go input admits explicit null through %q:\n%s", forbidden, text)
		}
	}
}

func TestGeneratedGoOutputModelsPreserveProjectedAndRedactedFieldPresence(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Partial Go output"},
		Collections: []schema.Collection{{
			ID: "articles", Slug: "articles", Labels: schema.CollectionLabels{Singular: "Article", Plural: "Articles"},
			Fields: []schema.Field{
				{ID: "title", Name: "title", Type: schema.FieldTypeText, Required: true},
				{ID: "location", Name: "location", Type: schema.FieldTypePoint, Required: true},
				{ID: "details", Name: "details", Type: schema.FieldTypeGroup, Required: true, Nested: &schema.NestedField{Fields: []schema.Field{
					{ID: "details-summary", Name: "summary", Type: schema.FieldTypeText, Required: true},
				}}},
			},
		}},
	})
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(goSourceWithoutLineComments(t, generated))
	for _, expected := range []string{
		`(?m)^\s*Title\s+\*string\s+` + "`json:\"title,omitempty\"`$",
		`(?m)^\s*Location\s+\*\[2\]float64\s+` + "`json:\"location,omitempty\"`$",
		`(?s)Details\s+\*struct\s*\{\s*Summary\s+\*string\s+` + "`json:\"summary,omitempty\"`" + `\s*\}\s*` + "`json:\"details,omitempty\"`",
	} {
		if !regexp.MustCompile(expected).MatchString(text) {
			t.Fatalf("generated Go output does not preserve optional presence for %q:\n%s", expected, text)
		}
	}
}

func TestGeneratedGoInputsTreatReadOnlyAsPresentationAndExcludeUploadMetadata(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Go input ownership"},
		Collections: []schema.Collection{{
			ID: "media", Slug: "media", Labels: schema.CollectionLabels{Singular: "Medium", Plural: "Media"},
			Fields: []schema.Field{
				{ID: "worker-status", Name: "workerStatus", Type: schema.FieldTypeText, Admin: schema.FieldAdmin{ReadOnly: true}},
				{ID: "filename", Name: "filename", Type: schema.FieldTypeText, Category: schema.FieldCategoryUpload, Admin: schema.FieldAdmin{ReadOnly: true}},
			},
		}},
	})
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	if count := strings.Count(text, "`json:\"workerStatus,omitempty\"`"); count != 3 {
		t.Fatalf("authored read-only field appears %d times, want output/create/update:\n%s", count, text)
	}
	if count := strings.Count(text, "`json:\"filename,omitempty\"`"); count != 1 {
		t.Fatalf("framework upload metadata appears %d times, want output only:\n%s", count, text)
	}
}

func TestGeneratedGoModelsUsePluginOwnedExactTypes(t *testing.T) {
	path, err := query.NewPath("accent")
	if err != nil {
		t.Fatal(err)
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Plugin Go types"},
		Plugins:     []schema.Plugin{{Key: "color", FieldTypes: []schema.PluginFieldType{{Key: "color", GoPackage: "example.com/color", GoType: "Value"}}}},
		Collections: []schema.Collection{{ID: "brands", Slug: "brands", Labels: schema.CollectionLabels{Singular: "Brand", Plural: "Brands"}, Fields: []schema.Field{{ID: "accent", Name: "accent", Path: path, Type: schema.FieldTypePlugin, Required: true, Plugin: &schema.PluginField{Key: "color"}}}}},
	})
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, expected := range []string{
		`color "example.com/color"`,
		`(?m)^\s*Accent\s+\*color\.Value\s+` + "`json:\"accent,omitempty\"`$",
		`(?m)^\s*Accent\s+core\.NonNullInput\[color\.Value\]\s+` + "`json:\"accent\"`$",
		`(?m)^\s*Accent\s+\*core\.NonNullInput\[color\.Value\]\s+` + "`json:\"accent,omitempty\"`$",
	} {
		if !regexp.MustCompile(expected).MatchString(text) {
			t.Fatalf("generated Go client missing %s:\n%s", expected, generated)
		}
	}
}

func TestGeneratedGoModelsDoNotImportUnusedPluginFieldTypes(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Unused plugin types"},
		Plugins:     []schema.Plugin{{Key: "color", FieldTypes: []schema.PluginFieldType{{Key: "color", GoPackage: "example.com/color", GoType: "Value"}}}},
		Collections: []schema.Collection{{
			ID:     "users",
			Slug:   "users",
			Labels: schema.CollectionLabels{Singular: "User", Plural: "Users"},
			Fields: []schema.Field{{ID: "email", Name: "email", Type: schema.FieldTypeEmail, Required: true}},
		}},
	})
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(generated), `"example.com/color"`) {
		t.Fatalf("generated Go imported an unused plugin field type:\n%s", generated)
	}
}

func TestIdentityChangeWarningsSendFieldRenamesToMigrationPlanning(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "schema.json")
	titlePath, _ := query.NewPath("title")
	headlinePath, _ := query.NewPath("headline")
	previous := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Rename"},
		Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: []schema.Field{{ID: "posts-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText}}}},
	})
	encoded, err := previous.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	current := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Rename"},
		Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: []schema.Field{{ID: "posts-headline", Name: "headline", Path: headlinePath, Type: schema.FieldTypeText}}}},
	})
	warnings := identityChangeWarnings(path, current)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `run ridu migrate create`) || !strings.Contains(warnings[0], `posts.title -> posts.headline`) {
		t.Fatalf("identity warnings = %#v", warnings)
	}
}

func TestIdentityChangeWarningsSendCollectionRenamesToMigrationPlanning(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "schema.json")
	titlePath, _ := query.NewPath("title")
	previous := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Rename"},
		Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: []schema.Field{{ID: "posts-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText}}}},
	})
	encoded, err := previous.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	current := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Rename"},
		Collections: []schema.Collection{{ID: "stories", Slug: "stories", Fields: []schema.Field{{ID: "stories-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText}}}},
	})
	warnings := identityChangeWarnings(path, current)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `run ridu migrate create`) || !strings.Contains(warnings[0], `posts -> stories`) {
		t.Fatalf("identity warnings = %#v", warnings)
	}
}
