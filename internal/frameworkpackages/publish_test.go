package frameworkpackages

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPublishVendorsUnpublishedPackagesAsOneLocalWorkspaceGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("fresh runtime builds are covered by the full publication integration")
	}
	frameworkRoot := moduleRoot(t)
	projectRoot := t.TempDir()
	stalePackage := filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-admin-build", "package.json")
	if err := os.MkdirAll(filepath.Dir(stalePackage), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePackage, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Publish(frameworkRoot, projectRoot, "v0.0.0-dogfood.1"); err != nil {
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
	buildPackageRoot := filepath.Join(projectRoot, ".ridu", "packages", "ridu-framework-build")
	for _, compiledFile := range []string{
		"dist/index.js",
		"dist/index.d.ts",
		"dist/uno/index.js",
		"dist/uno/index.d.ts",
		"dist/vite/index.js",
		"dist/vite/index.d.ts",
	} {
		if _, err := os.Stat(filepath.Join(buildPackageRoot, filepath.FromSlash(compiledFile))); err != nil {
			t.Errorf("published build package omits %s: %v", compiledFile, err)
		}
	}
}

func TestPublishPreservesAUserOwnedPackagesDirectory(t *testing.T) {
	frameworkRoot := packageFixture(t)
	snapshot, err := captureBuiltPackages(frameworkRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := snapshot.Close(); err != nil {
			t.Error(err)
		}
	})
	projectRoot := t.TempDir()
	customPackage := filepath.Join(projectRoot, "packages", "storefront", "package.json")
	if err := os.MkdirAll(filepath.Dir(customPackage), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("{\"name\":\"storefront\"}\n")
	if err := os.WriteFile(customPackage, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := snapshot.Publish(projectRoot, "v0.0.0-dogfood.2"); err != nil {
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

// Copy/rewrite contracts use tiny explicit package inputs. The integration above
// separately proves that actual workspace build scripts produce publishable files.
func packageFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("LICENSE", "fixture license")
	for _, definition := range packages {
		write(filepath.Join(definition.source, "package.json"), `{"name":"`+definition.name+`","private":true,"version":"0.0.0","dependencies":{"@riducms/sdk":"workspace:*"},"scripts":{"build":"exit 23"}}`)
		write(filepath.Join(definition.source, "src", "index.ts"), "original source")
		write(filepath.Join(definition.source, "src", "tsconfig.json"), "{}")
		write(filepath.Join(definition.source, "node_modules", "excluded"), "development-only")
		if definition.build {
			write(filepath.Join(definition.source, "dist", "index.js"), "fresh compiled source")
		}
		if definition.notices != "" {
			write(definition.notices, "fixture notices")
		}
	}
	return root
}

func TestPreparedSnapshotCopiesIndependentlyAndRemovesStalePackages(t *testing.T) {
	root := packageFixture(t)
	snapshot, err := captureBuiltPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := snapshot.Close(); err != nil {
			t.Error(err)
		}
	})
	// Mutating the original checkout cannot change a captured graph.
	if err := os.WriteFile(filepath.Join(root, "packages", "sdk", "dist", "index.js"), []byte("later build"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v0.0.0-first", "v0.0.0-second"} {
		project := t.TempDir()
		stale := filepath.Join(project, ".ridu", "packages", "obsolete", "index.js")
		if err := os.MkdirAll(filepath.Dir(stale), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stale, []byte("obsolete"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := snapshot.Publish(project, version); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatal("stale package survived replacement")
		}
		for _, definition := range packages {
			target := filepath.Join(project, ".ridu", "packages", definition.target)
			manifest, err := os.ReadFile(filepath.Join(target, "package.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(manifest), "workspace:") || strings.Contains(string(manifest), `"private"`) || !strings.Contains(string(manifest), strings.TrimPrefix(version, "v")) {
				t.Fatalf("invalid published manifest: %s", manifest)
			}
			if _, err := os.Stat(filepath.Join(target, "node_modules")); !os.IsNotExist(err) {
				t.Fatal("snapshot copied development dependencies")
			}
			if definition.omitSourceTSConfig {
				if _, err := os.Stat(filepath.Join(target, "src", "tsconfig.json")); !os.IsNotExist(err) {
					t.Fatal("snapshot copied excluded tsconfig")
				}
			}
		}
		sdk := filepath.Join(project, ".ridu", "packages", "ridu-framework-sdk", "dist", "index.js")
		content, err := os.ReadFile(sdk)
		if err != nil || string(content) != "fresh compiled source" {
			t.Fatalf("snapshot followed changed build output: %q, %v", content, err)
		}
		if err := os.WriteFile(sdk, []byte("application modification"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snapshot.root); !os.IsNotExist(err) {
		t.Fatal("snapshot cleanup left private files")
	}
	if err := snapshot.Publish(t.TempDir(), "v1.0.0"); err == nil {
		t.Fatal("closed snapshot published successfully")
	}
}

func TestPrepareDoesNotAcceptStaleOutputAfterABuildFailure(t *testing.T) {
	root := packageFixture(t)
	snapshot, err := Prepare(root)
	if err == nil {
		snapshot.Close()
		t.Fatal("existing dist masked a failed fresh build")
	}
	if !strings.Contains(err.Error(), "build @riducms/protocol release output") {
		t.Fatalf("unexpected build failure: %v", err)
	}
}
