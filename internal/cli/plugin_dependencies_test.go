package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/generate"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/schema"
)

func TestMissingGeneratedTypeScriptDependenciesUsesRootManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"@acme/installed":"1.0.0","plain-package":"1.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := schema.NewManifest(schema.Snapshot{Plugins: []schema.Plugin{{FieldTypes: []schema.PluginFieldType{
		{TypeScriptPackage: "@acme/missing-b"},
		{TypeScriptPackage: "@acme/installed"},
		{TypeScriptPackage: "@acme/installed/document"},
		{TypeScriptPackage: "plain-package/document"},
		{TypeScriptPackage: "@acme/missing-a"},
		{TypeScriptPackage: "@acme/missing-b"},
		{TypeScriptPackage: "@acme/missing-b/document"},
	}}}})
	definition := projectfile.File{Root: root, Client: "generated/ridu.generated.ts"}

	want := []string{"@acme/missing-a", "@acme/missing-b"}
	if got := missingGeneratedTypeScriptDependencies(definition, manifest); !reflect.DeepEqual(got, want) {
		t.Fatalf("missing dependencies = %v, want %v", got, want)
	}
}

func TestManifestPackageRequirementsKeepSharedDependencies(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{Plugins: []schema.Plugin{
		{Admin: &schema.PluginAdmin{Package: "@acme/shared-admin"}},
		{FieldTypes: []schema.PluginFieldType{{TypeScriptPackage: "@acme/shared-types/document"}}},
	}})

	if !manifestRequiresAdminPackage(manifest, "@acme/shared-admin") {
		t.Fatal("shared admin package was not retained")
	}
	if !manifestRequiresTypeScriptPackage(manifest, "@acme/shared-types") {
		t.Fatal("shared generated-type package was not retained")
	}
	if manifestRequiresAdminPackage(manifest, "@acme/removed") || manifestRequiresTypeScriptPackage(manifest, "@acme/removed") {
		t.Fatal("unreferenced package was retained")
	}
}

func TestMissingAdminPluginPackagesChecksAdminManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "admin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"@acme/root-admin":"1.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "admin", "package.json"), []byte(`{"devDependencies":{"@acme/admin-only":"1.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := schema.NewManifest(schema.Snapshot{Plugins: []schema.Plugin{
		{Admin: &schema.PluginAdmin{Package: "@riducms/plugin-graphql"}},
		{Admin: &schema.PluginAdmin{Package: "@acme/root-admin"}},
		{Admin: &schema.PluginAdmin{Package: "@acme/admin-only"}},
		{Admin: &schema.PluginAdmin{Package: "@riducms/plugin-graphql"}},
		{FieldTypes: []schema.PluginFieldType{{TypeScriptPackage: "@acme/types-only"}}},
	}})

	want := []string{"@acme/root-admin", "@riducms/plugin-graphql"}
	if got := missingAdminPluginPackages(projectfile.File{Root: root, Admin: "admin"}, manifest); !reflect.DeepEqual(got, want) {
		t.Fatalf("missing admin packages = %v, want %v", got, want)
	}
	if got := missingAdminPluginPackages(projectfile.File{Root: root}, manifest); got != nil {
		t.Fatalf("a project without an admin reported %v", got)
	}
}

func TestRegisteredAdminPackageRequirementUsesCoordinatedOfficialVersion(t *testing.T) {
	root := t.TempDir()
	registry := `{"version":1,"plugins":[{"key":"graphql","goPackage":"example.com/graphql","goVersion":"latest","constructor":"New","adminPackage":"@riducms/plugin-graphql","adminVersion":"latest"},{"key":"custom","goPackage":"example.com/custom","goVersion":"latest","constructor":"New","adminPackage":"@acme/custom","adminVersion":"^2.0.0"}]}`
	if err := os.WriteFile(filepath.Join(root, "ridu.plugins.json"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	definition := projectfile.File{Root: root, Plugins: "ridu.plugins.json"}
	if got := registeredAdminPackageRequirement(definition, "@riducms/plugin-graphql", "0.5.0"); got != "@riducms/plugin-graphql@0.5.0" {
		t.Fatalf("official requirement = %q", got)
	}
	if got := registeredAdminPackageRequirement(definition, "@acme/custom", "0.5.0"); got != "@acme/custom@^2.0.0" {
		t.Fatalf("custom requirement = %q", got)
	}
}

func TestGenerationWarningUsesAdminDirectoryAndCoordinatedVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "admin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "admin", "package.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := `{"version":1,"plugins":[{"key":"graphql","goPackage":"example.com/graphql","goVersion":"latest","constructor":"New","adminPackage":"@riducms/plugin-graphql","adminVersion":"latest"}]}`
	if err := os.WriteFile(filepath.Join(root, "ridu.plugins.json"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := schema.NewManifest(schema.Snapshot{Plugins: []schema.Plugin{{
		Admin: &schema.PluginAdmin{Package: "@riducms/plugin-graphql"},
	}}})
	definition := projectfile.File{
		Root: root, Admin: "admin", Plugins: "ridu.plugins.json", PackageManager: projectfile.PackageManagerBun,
	}
	var stdout, stderr bytes.Buffer
	printGenerationWarnings(outputFor(&stdout, &stderr, Options{}), definition, generate.Result{Manifest: manifest}, "0.5.0")
	if want := "bun add --cwd admin --exact @riducms/plugin-graphql@0.5.0"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("warning = %q, want command %q", stderr.String(), want)
	}
}
