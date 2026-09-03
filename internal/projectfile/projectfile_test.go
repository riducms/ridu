package projectfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/projectfile"
)

func TestDiscoverFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ridu.toml"), `version = 1
database = "postgres"
entry = "./cmd/server"
schema = "./generated/ridu.schema.json"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
`)
	nested := filepath.Join(root, "internal", "content")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	project, err := projectfile.Discover(nested)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if project.Root != root {
		t.Fatalf("project root = %q, want %q", project.Root, root)
	}
	if got := project.Absolute(project.Schema); got != filepath.Join(root, "generated", "ridu.schema.json") {
		t.Fatalf("absolute schema = %q", got)
	}
}

func TestLoadSelectsAnOfficialDatabaseAdapter(t *testing.T) {
	for _, test := range []struct {
		name     string
		line     string
		expected projectfile.DatabaseAdapter
	}{
		{name: "postgres", line: "database = \"postgres\"\n", expected: projectfile.DatabasePostgres},
		{name: "sqlite", line: "database = \"sqlite\"\n", expected: projectfile.DatabaseSQLite},
		{name: "mongodb", line: "database = \"mongodb\"\n", expected: projectfile.DatabaseMongoDB},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ridu.toml")
			writeFile(t, path, "version = 1\n"+test.line+"entry = \"./cmd/server\"\nschema = \"./schema.json\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n")
			project, err := projectfile.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if project.Database != test.expected {
				t.Fatalf("database = %q, want %q", project.Database, test.expected)
			}
		})
	}
}

func TestLoadSelectsAFrontendPackageManager(t *testing.T) {
	for _, manager := range []projectfile.PackageManager{
		projectfile.PackageManagerNPM,
		projectfile.PackageManagerBun,
		projectfile.PackageManagerPNPM,
		projectfile.PackageManagerYarn,
	} {
		t.Run(string(manager), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ridu.toml")
			writeFile(t, path, "version = 1\ndatabase = \"postgres\"\npackage_manager = \""+string(manager)+"\"\nentry = \"./cmd/server\"\nschema = \"./schema.json\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n")
			project, err := projectfile.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if project.FrontendPackageManager() != manager {
				t.Fatalf("package manager = %q, want %q", project.FrontendPackageManager(), manager)
			}
		})
	}

	path := filepath.Join(t.TempDir(), "ridu.toml")
	writeFile(t, path, "version = 1\ndatabase = \"postgres\"\nentry = \"./cmd/server\"\nschema = \"./schema.json\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n")
	project, err := projectfile.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if project.FrontendPackageManager() != projectfile.PackageManagerBun {
		t.Fatalf("legacy package manager = %q, want bun", project.FrontendPackageManager())
	}
}

func TestLoadRejectsVersionAndEscapingPaths(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"version", "version = 99\nentry = \"./cmd/server\"\nschema = \"./schema.json\"\n", "incompatible"},
		{"missing database", "version = 1\nentry = \"./cmd/server\"\nschema = \"./schema.json\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n", "database is required"},
		{"escape", "version = 1\ndatabase = \"postgres\"\nentry = \"../server\"\nschema = \"./schema.json\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n", "inside the project root"},
		{"unknown", "version = 1\ndatabase = \"postgres\"\nentry = \"./cmd/server\"\nschema = \"./schema.json\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\nsecret = \"no\"\n", "unknown project key"},
		{"database", "version = 1\ndatabase = \"mysql\"\nentry = \"./cmd/server\"\nschema = \"./schema.json\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n", "adapter \"mysql\" is unsupported; expected \"postgres\", \"sqlite\", or \"mongodb\""},
		{"package manager", "version = 1\ndatabase = \"postgres\"\npackage_manager = \"deno\"\nentry = \"./cmd/server\"\nschema = \"./schema.json\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n", "manager \"deno\" is unsupported"},
		{"client directory", "version = 1\ndatabase = \"postgres\"\nentry = \"./cmd/server\"\nschema = \"./schema.json\"\nclient = \"./packages/ridu-client\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n", "must name a TypeScript file"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ridu.toml")
			writeFile(t, path, test.content)
			_, err := projectfile.Load(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestLoadAllowsAProjectRootServerEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "ridu.toml")
	writeFile(t, path, "version = 1\ndatabase = \"postgres\"\nentry = \".\"\nschema = \"./generated/schema.json\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n")
	project, err := projectfile.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if project.Entry != "." || project.Absolute(project.Entry) != root {
		t.Fatalf("root entry = %q (%q)", project.Entry, project.Absolute(project.Entry))
	}
}

func TestLoadAcceptsAProjectRelativeGeneratedArtifact(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "ridu.toml")
	writeFile(t, path, "version = 1\ndatabase = \"postgres\"\nentry = \"./cmd/server\"\nschema = \"./generated/schema.json\"\ngenerated.graphql.schema = \"./apps/web/generated/ridu.graphql\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n")
	project, err := projectfile.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.GeneratedArtifacts) != 1 || project.GeneratedArtifacts[0].Plugin != "graphql" || project.GeneratedArtifacts[0].Name != "schema" || project.GeneratedArtifacts[0].Path != "apps/web/generated/ridu.graphql" {
		t.Fatalf("generated artifacts = %#v", project.GeneratedArtifacts)
	}
	if project.Absolute(project.GeneratedArtifacts[0].Path) != filepath.Join(root, "apps", "web", "generated", "ridu.graphql") {
		t.Fatalf("generated artifact absolute path = %q", project.Absolute(project.GeneratedArtifacts[0].Path))
	}
}

func TestLoadRejectsInvalidOrCollidingGeneratedArtifacts(t *testing.T) {
	for _, test := range []struct {
		name string
		key  string
		path string
		want string
	}{
		{name: "escape", key: "generated.graphql.schema", path: "../schema.graphql", want: "inside the project root"},
		{name: "invalid key", key: "generated.GraphQL.schema", path: "generated/schema.graphql", want: "lowercase kebab-case"},
		{name: "collision", key: "generated.graphql.schema", path: "GENERATED/schema.json", want: "already used by schema"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ridu.toml")
			writeFile(t, path, "version = 1\ndatabase = \"postgres\"\nentry = \"./cmd/server\"\nschema = \"./generated/schema.json\"\n"+test.key+" = \"./"+test.path+"\"\nplugins = \"./plugins.json\"\nplugin_go = \"./content/plugins.go\"\n")
			_, err := projectfile.Load(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
