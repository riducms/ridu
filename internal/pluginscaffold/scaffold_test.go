package pluginscaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/pluginscaffold"
)

func TestCreateRendersPairedConformanceStarter(t *testing.T) {
	target := filepath.Join(t.TempDir(), "color-plugin")
	created, err := pluginscaffold.Create(pluginscaffold.Options{Target: target, Key: "color", ModulePath: "example.com/ridu-color", AdminPackage: "@example/ridu-color-admin", FrameworkVersion: "v0.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if created != target {
		t.Fatalf("created = %q, want %q", created, target)
	}
	for _, path := range []string{"plugin.go", "plugin_test.go", "admin/src/index.ts", "admin/src/field.svelte", "admin/tests/plugin.test.ts"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(path))); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	plugin, err := os.ReadFile(filepath.Join(target, "plugin.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plugin), `Package: "@example/ridu-color-admin"`) || !strings.Contains(string(plugin), "FieldTypes:") {
		t.Fatalf("backend template lacks paired metadata:\n%s", plugin)
	}
	if !strings.Contains(string(plugin), `Minimum: "0.2.0", MaximumExclusive: "0.3.0"`) {
		t.Fatalf("backend template has stale framework compatibility:\n%s", plugin)
	}
	pluginTest, err := os.ReadFile(filepath.Join(target, "plugin_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pluginTest), "ValidData:") || !strings.Contains(string(pluginTest), "InvalidData:") {
		t.Fatalf("backend conformance template skips runtime validation:\n%s", pluginTest)
	}
	goModule, err := os.ReadFile(filepath.Join(target, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goModule), "go 1.25.13") {
		t.Fatalf("plugin scaffold does not require the security-patched Go toolchain:\n%s", goModule)
	}
	adminPackage, err := os.ReadFile(filepath.Join(target, "admin", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(adminPackage), `"@riducms/plugin": "0.2.0"`) ||
		strings.Contains(string(adminPackage), `"@riducms/plugin": "^0.2.0"`) {
		t.Fatalf("plugin scaffold does not exact-pin its Ridu package dependency:\n%s", adminPackage)
	}
}
