package frameworkpackages_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/frameworkpackages"
)

func TestPublishVendorsUnpublishedPackagesAsOneLocalWorkspaceGraph(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	projectRoot := t.TempDir()
	stalePackage := filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-admin-build", "package.json")
	if err := os.MkdirAll(filepath.Dir(stalePackage), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePackage, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := frameworkpackages.Publish(frameworkRoot, projectRoot, "v0.0.0-dogfood.1"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := os.Stat(stalePackage); !os.IsNotExist(err) {
		t.Fatalf("stale framework package survived workspace replacement: %v", err)
	}

	for _, target := range []string{"ridu-create", "ridu-framework-admin", "ridu-framework-build", "ridu-framework-cli", "ridu-framework-plugin", "ridu-framework-ui", "ridu-framework-plugin-richtext", "ridu-framework-plugin-seo", "ridu-framework-plugin-form-builder", "ridu-framework-protocol", "ridu-framework-sdk", "ridu-framework-translations"} {
		if _, err := os.Stat(filepath.Join(projectRoot, ".ridu", "packages", target)); err != nil {
			t.Errorf("hidden dogfood package %s: %v", target, err)
		}
	}
	adminManifestPath := filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-admin", "package.json")
	adminEncoded, err := os.ReadFile(adminManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(adminEncoded), "workspace:") || !strings.Contains(string(adminEncoded), `"version": "0.0.0-dogfood.1"`) {
		t.Fatalf("published admin manifest was not release-shaped:\n%s", adminEncoded)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-admin", "src", "index.ts")); err != nil {
		t.Fatalf("published admin source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-admin", "src", "tsconfig.json")); !os.IsNotExist(err) {
		t.Fatalf("published admin package contains its manifest-excluded source tsconfig: %v", err)
	}
	for _, legalFile := range []string{"LICENSE", "THIRD_PARTY_NOTICES.md"} {
		if _, err := os.Stat(filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-admin", legalFile)); err != nil {
			t.Fatalf("published admin %s: %v", legalFile, err)
		}
	}
	for _, target := range []string{"ridu-create", "ridu-framework-build", "ridu-framework-cli", "ridu-framework-plugin", "ridu-framework-ui", "ridu-framework-plugin-richtext", "ridu-framework-plugin-seo", "ridu-framework-plugin-form-builder", "ridu-framework-protocol", "ridu-framework-sdk", "ridu-framework-translations"} {
		packageRoot := filepath.Join(projectRoot, ".ridu", "packages", target)
		if _, err := os.Stat(filepath.Join(packageRoot, "LICENSE")); err != nil {
			t.Fatalf("published package %s omits LICENSE: %v", target, err)
		}
		if _, err := os.Stat(filepath.Join(packageRoot, "THIRD_PARTY_NOTICES.md")); !os.IsNotExist(err) {
			t.Fatalf("published package %s has unrelated third-party notices: %v", target, err)
		}
	}
	for _, target := range []string{"ridu-create", "ridu-framework-cli"} {
		if _, err := os.Stat(filepath.Join(projectRoot, ".ridu", "packages", target, "README.md")); err != nil {
			t.Errorf("published package %s omits README.md: %v", target, err)
		}
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-admin", "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("published admin package contains node_modules: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-admin", "svelte.config.js")); !os.IsNotExist(err) {
		t.Fatalf("published admin package contains development Svelte config: %v", err)
	}

	adminBuildEncoded, err := os.ReadFile(filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-build", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(adminBuildEncoded), `"@iconify/json"`) {
		t.Fatalf("published admin build package does not own its icon data dependency:\n%s", adminBuildEncoded)
	}
	for _, target := range []string{"ridu-framework-protocol", "ridu-framework-sdk"} {
		packageRoot := filepath.Join(projectRoot, ".ridu", "packages", target)
		for _, compiledFile := range []string{"dist/index.js", "dist/index.d.ts"} {
			if _, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(compiledFile))); err != nil {
				t.Errorf("published package %s omits %s: %v", target, compiledFile, err)
			}
		}
		manifest, err := os.ReadFile(filepath.Join(packageRoot, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(manifest), `"./src/index.ts"`) ||
			!strings.Contains(string(manifest), `"types": "./dist/index.d.ts"`) ||
			!strings.Contains(string(manifest), `"import": "./dist/index.js"`) {
			t.Errorf("published package %s does not expose compiled output:\n%s", target, manifest)
		}
	}
}

func TestPublishPreservesAUserOwnedPackagesDirectory(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	projectRoot := t.TempDir()
	customPackage := filepath.Join(projectRoot, "packages", "storefront", "package.json")
	if err := os.MkdirAll(filepath.Dir(customPackage), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("{\"name\":\"storefront\"}\n")
	if err := os.WriteFile(customPackage, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := frameworkpackages.Publish(frameworkRoot, projectRoot, "v0.0.0-dogfood.2"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	actual, err := os.ReadFile(customPackage)
	if err != nil {
		t.Fatalf("read user-owned package after publish: %v", err)
	}
	if string(actual) != string(content) {
		t.Fatalf("user-owned package changed to %q", actual)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-sdk")); err != nil {
		t.Fatalf("hidden SDK package: %v", err)
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
