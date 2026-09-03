package pluginregistry_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/pluginregistry"
)

func TestRegistryWritesDeterministicCompiledRegistration(t *testing.T) {
	registry := pluginregistry.Registry{Version: pluginregistry.CurrentVersion}
	if err := registry.Add(pluginregistry.Entry{Key: "seo", GoPackage: "example.com/plugins/seo", GoVersion: "v1.2.3", Constructor: "New"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(pluginregistry.Entry{Key: "color", GoPackage: "example.com/plugins/color", GoVersion: "v2.0.0", Constructor: "Plugin", AdminPackage: "@example/color-admin", AdminVersion: "^2.0.0"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	registryPath := filepath.Join(root, "ridu.plugins.json")
	goPath := filepath.Join(root, "content", "ridu_plugins.generated.go")
	if err := registry.Write(registryPath, goPath, "content"); err != nil {
		t.Fatal(err)
	}
	loaded, err := pluginregistry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Plugins[0].Key != "color" || loaded.Plugins[1].Key != "seo" {
		t.Fatalf("plugins are not key-sorted: %#v", loaded.Plugins)
	}
	goSource, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goSource), `plugin0 "example.com/plugins/color"`) || !strings.Contains(string(goSource), "plugin0.Plugin()") {
		t.Fatalf("generated registration is incomplete:\n%s", goSource)
	}
}

func TestRegistryRejectsAmbiguousEntries(t *testing.T) {
	registry := pluginregistry.Registry{Version: pluginregistry.CurrentVersion, Plugins: []pluginregistry.Entry{{Key: "bad key", GoPackage: "example.com/plugin", GoVersion: "latest", Constructor: "New"}}}
	if err := registry.Validate(); err == nil || !strings.Contains(err.Error(), "lowercase kebab-case") {
		t.Fatalf("Validate error = %v", err)
	}
}
