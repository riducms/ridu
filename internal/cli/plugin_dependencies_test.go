package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

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
