// Package frameworkpackages publishes the framework's unpublished frontend
// workspaces into a generated project for local dogfood testing.
package frameworkpackages

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type packageDefinition struct {
	name               string
	source             string
	target             string
	notices            string
	build              bool
	omitSourceTSConfig bool
}

var packages = []packageDefinition{
	{name: "@riducms/cli", source: "packages/cli", target: "ridu-framework-cli"},
	{name: "create-ridu", source: "packages/create-ridu", target: "ridu-create"},
	{name: "@riducms/protocol", source: "packages/protocol", target: "ridu-framework-protocol", build: true},
	{name: "@riducms/translations", source: "packages/translations", target: "ridu-framework-translations"},
	{name: "@riducms/sdk", source: "packages/sdk", target: "ridu-framework-sdk", build: true},
	{name: "@riducms/build", source: "packages/build", target: "ridu-framework-build", build: true},
	{name: "@riducms/plugin", source: "packages/plugin", target: "ridu-framework-plugin"},
	{name: "@riducms/ui", source: "packages/ui", target: "ridu-framework-ui"},
	{name: "@riducms/admin", source: "admin", target: "ridu-framework-admin", notices: "admin/THIRD_PARTY_NOTICES.md", omitSourceTSConfig: true},
	{name: "@riducms/plugin-richtext", source: "packages/plugin-richtext", target: "ridu-framework-plugin-richtext"},
	{name: "@riducms/plugin-seo", source: "packages/plugin-seo", target: "ridu-framework-plugin-seo"},
	{name: "@riducms/plugin-form-builder", source: "packages/plugin-form-builder", target: "ridu-framework-plugin-form-builder"},
}

// Publish copies source packages into ignored directories covered by the
// generated project's .ridu/packages/* workspace. This gives Bun one physical
// instance of each package, matching the identity guarantees of a published
// dependency graph while keeping dependency declarations release-shaped and
// repository-only framework plumbing out of the user's project tree.
func Publish(frameworkRoot, projectRoot, version string) error {
	npmVersion := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if npmVersion == "" {
		return fmt.Errorf("frontend package version must not be empty")
	}
	if err := buildRuntimePackages(frameworkRoot); err != nil {
		return err
	}
	packageRoot := filepath.Join(projectRoot, ".ridu", "packages")
	// This ignored directory is entirely framework-owned. Replace it as a unit
	// so package and directory renames cannot leave stale workspaces that Bun
	// may continue to resolve alongside the current package graph.
	if err := os.RemoveAll(packageRoot); err != nil {
		return fmt.Errorf("replace framework package workspace: %w", err)
	}
	for _, definition := range packages {
		source := filepath.Join(frameworkRoot, filepath.FromSlash(definition.source))
		target := filepath.Join(packageRoot, definition.target)
		if err := copyPackage(source, target, definition.build, definition.omitSourceTSConfig); err != nil {
			return fmt.Errorf("publish %s: %w", definition.name, err)
		}
		if err := copyFile(filepath.Join(frameworkRoot, "LICENSE"), filepath.Join(target, "LICENSE"), 0o644); err != nil {
			return fmt.Errorf("publish %s license: %w", definition.name, err)
		}
		if definition.notices != "" {
			if err := copyFile(filepath.Join(frameworkRoot, definition.notices), filepath.Join(target, "THIRD_PARTY_NOTICES.md"), 0o644); err != nil {
				return fmt.Errorf("publish %s third-party notices: %w", definition.name, err)
			}
		}
		if err := rewritePackageManifest(filepath.Join(target, "package.json"), npmVersion, definition.notices != ""); err != nil {
			return fmt.Errorf("publish %s: %w", definition.name, err)
		}
	}
	return nil
}

func buildRuntimePackages(frameworkRoot string) error {
	for _, definition := range packages {
		if !definition.build {
			continue
		}
		command := exec.Command("bun", "run", "--cwd", filepath.Join(frameworkRoot, filepath.FromSlash(definition.source)), "build")
		command.Dir = frameworkRoot
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("build %s release output: %w", definition.name, err)
		}
	}
	return nil
}

func copyPackage(source, target string, includeBuild, omitSourceTSConfig bool) error {
	if _, err := os.Stat(filepath.Join(source, "package.json")); err != nil {
		return fmt.Errorf("read source package: %w", err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	packageManifest := filepath.Join(source, "package.json")
	if err := copyFile(packageManifest, filepath.Join(target, "package.json"), 0o644); err != nil {
		return err
	}
	packageReadme := filepath.Join(source, "README.md")
	if _, err := os.Stat(packageReadme); err == nil {
		if err := copyFile(packageReadme, filepath.Join(target, "README.md"), 0o644); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := copyDirectory(filepath.Join(source, "src"), filepath.Join(target, "src")); err != nil {
		return err
	}
	if omitSourceTSConfig {
		if err := os.Remove(filepath.Join(target, "src", "tsconfig.json")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if includeBuild {
		return copyDirectory(filepath.Join(source, "dist"), filepath.Join(target, "dist"))
	}
	return nil
}

func copyDirectory(sourceRoot, targetRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(targetRoot, relative), 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(targetRoot, relative), info.Mode().Perm())
	})
}

func copyFile(source, target string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func rewritePackageManifest(path, version string, includeNotices bool) error {
	manifest, err := readManifest(path)
	if err != nil {
		return err
	}
	manifest["version"] = version
	delete(manifest, "private")
	files, _ := manifest["files"].([]any)
	requiredFiles := []string{"LICENSE"}
	if includeNotices {
		requiredFiles = append(requiredFiles, "THIRD_PARTY_NOTICES.md")
	}
	for _, required := range requiredFiles {
		present := false
		for _, file := range files {
			if file == required {
				present = true
				break
			}
		}
		if !present {
			files = append(files, required)
		}
	}
	manifest["files"] = files
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		dependencies, ok := manifest[key].(map[string]any)
		if !ok {
			continue
		}
		for name, rawVersion := range dependencies {
			dependencyVersion, ok := rawVersion.(string)
			if ok && strings.HasPrefix(dependencyVersion, "workspace:") {
				dependencies[name] = version
			}
		}
	}
	return writeManifest(path, manifest)
}

func readManifest(path string) (map[string]any, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	manifest := make(map[string]any)
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func writeManifest(path string, manifest map[string]any) error {
	encoded, err := json.MarshalIndent(manifest, "", "\t")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".package-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
