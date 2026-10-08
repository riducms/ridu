package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/riducms/ridu/internal/frameworkpackages"
	"github.com/riducms/ridu/internal/frameworkproxy"
)

const defaultOut = ".ridu/local-release"

var frameworkVersionPattern = regexp.MustCompile(`const FrameworkVersion = "([^"]+)"`)

func runStage(arguments []string) int {
	flags := flag.NewFlagSet("stage", flag.ContinueOnError)
	version := flags.String("version", "", "release version (defaults to the next patch with a unique -local prerelease)")
	out := flags.String("out", defaultOut, "directory for the staged release")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	frameworkRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "determine framework root: %v\n", err)
		return 1
	}
	if err := stage(frameworkRoot, *out, strings.TrimPrefix(strings.TrimSpace(*version), "v")); err != nil {
		fmt.Fprintf(os.Stderr, "stage local release: %v\n", err)
		return 1
	}
	return 0
}

func stage(frameworkRoot, out, version string) error {
	current, err := frameworkVersion(filepath.Join(frameworkRoot, "core", "version.go"))
	if err != nil {
		return err
	}
	if version == "" {
		version = nextLocalVersion(current, time.Now())
	}
	if !semver.IsValid("v" + version) {
		return fmt.Errorf("version %q is not a semantic version", version)
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(frameworkRoot, out)
	}
	// A new stage replaces the last one; the caches stay so repeated installs are quick.
	for _, directory := range []string{"source", "releases", "goproxy", "npm"} {
		if err := os.RemoveAll(filepath.Join(out, directory)); err != nil {
			return err
		}
	}
	source := filepath.Join(out, "source")

	step("Copying the working tree")
	if err := copyWorkingTree(frameworkRoot, source); err != nil {
		return err
	}
	if err := setFrameworkVersion(filepath.Join(source, "core", "version.go"), version); err != nil {
		return err
	}

	step("Building the CLI archive")
	if err := writeCLIRelease(source, filepath.Join(out, "releases", "v"+version), version); err != nil {
		return err
	}

	step("Publishing the Go module proxy")
	if _, err := frameworkproxy.Publish(source, filepath.Join(out, "goproxy"), "v"+version); err != nil {
		return err
	}

	step("Packing the npm packages")
	if err := command(source, "bun", "install", "--frozen-lockfile").Run(); err != nil {
		return fmt.Errorf("install the staged workspace: %w", err)
	}
	if err := packNPMPackages(source, filepath.Join(out, "npm"), version); err != nil {
		return err
	}

	if err := writeEnvironment(out, version); err != nil {
		return err
	}
	fmt.Printf(`
Staged Ridu %[1]s in %[2]s.

Serve it, then create a project in another terminal:
  go run ./internal/localrelease serve
  source %[2]s/env.sh
  npm create ridu@%[1]s my-app
`, version, out)
	return nil
}

func step(message string) { fmt.Printf("== %s\n", message) }

func frameworkVersion(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	match := frameworkVersionPattern.FindSubmatch(content)
	if match == nil {
		return "", fmt.Errorf("%s does not declare FrameworkVersion", path)
	}
	return string(match[1]), nil
}

// nextLocalVersion is unique per stage, so package-manager caches never return an earlier stage's files.
func nextLocalVersion(current string, now time.Time) string {
	var major, minor, patch int
	fmt.Sscanf(current, "%d.%d.%d", &major, &minor, &patch)
	return fmt.Sprintf("%d.%d.%d-local.%s", major, minor, patch+1, now.UTC().Format("20060102150405"))
}

func setFrameworkVersion(path, version string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := frameworkVersionPattern.ReplaceAll(content, []byte(fmt.Sprintf("const FrameworkVersion = %q", version)))
	return os.WriteFile(path, updated, 0o644)
}

// copyWorkingTree copies tracked and unignored files, including uncommitted changes.
func copyWorkingTree(frameworkRoot, target string) error {
	listing := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	listing.Dir = frameworkRoot
	output, err := listing.Output()
	if err != nil {
		return fmt.Errorf("list the working tree: %w", err)
	}
	for _, name := range strings.Split(string(output), "\x00") {
		if name == "" {
			continue
		}
		origin := filepath.Join(frameworkRoot, filepath.FromSlash(name))
		info, err := os.Lstat(origin)
		if os.IsNotExist(err) {
			continue // Deleted but not yet staged.
		}
		if err != nil {
			return err
		}
		destination := filepath.Join(target, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(origin)
			if err != nil {
				return err
			}
			if err := os.Symlink(link, destination); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(origin, destination, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(origin, destination string, mode os.FileMode) error {
	source, err := os.Open(origin)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		return err
	}
	return target.Close()
}

// writeCLIRelease builds the host CLI as GoReleaser does and writes the archive and SHA256SUMS
// that @riducms/cli downloads from RIDU_CLI_RELEASE_BASE_URL.
func writeCLIRelease(source, releaseRoot, version string) error {
	if err := os.MkdirAll(releaseRoot, 0o755); err != nil {
		return err
	}
	binaryName := "ridu"
	if runtime.GOOS == "windows" {
		binaryName = "ridu.exe"
	}
	binary := filepath.Join(releaseRoot, binaryName)
	build := command(source, "go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-buildid= -X main.version=v"+version, "-o", binary, "./cmd/ridu")
	build.Env = append(build.Env, "CGO_ENABLED=0")
	if err := build.Run(); err != nil {
		return fmt.Errorf("build the CLI: %w", err)
	}
	defer os.Remove(binary)
	content, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("ridu_%s_%s_%s", version, runtime.GOOS, runtime.GOARCH)
	var archive []byte
	if runtime.GOOS == "windows" {
		name += ".zip"
		archive, err = zipArchive(binaryName, content)
	} else {
		name += ".tar.gz"
		archive, err = tarArchive(binaryName, content)
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(releaseRoot, name), archive, 0o644); err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	return os.WriteFile(filepath.Join(releaseRoot, "SHA256SUMS"), []byte(hex.EncodeToString(sum[:])+"  "+name+"\n"), 0o644)
}

// tarArchive writes plain USTAR, like GoReleaser. The launcher reads the first entry named ridu,
// so the archive must not carry extended-attribute entries such as those macOS tar adds.
func tarArchive(name string, content []byte) ([]byte, error) {
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
	if err := archive.WriteHeader(header); err != nil {
		return nil, err
	}
	if _, err := archive.Write(content); err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := compressed.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func zipArchive(name string, content []byte) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	writer, err := archive.Create(name)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(content); err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// packNPMPackages builds the packages as a release does and packs each with its release version.
func packNPMPackages(source, npmRoot, version string) error {
	snapshot, err := frameworkpackages.Prepare(source)
	if err != nil {
		return err
	}
	defer snapshot.Close()
	project, err := os.MkdirTemp("", "ridu-local-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(project)
	if err := snapshot.Publish(project, version); err != nil {
		return err
	}
	if err := os.MkdirAll(npmRoot, 0o755); err != nil {
		return err
	}
	packages, err := os.ReadDir(filepath.Join(project, ".ridu", "packages"))
	if err != nil {
		return err
	}
	for _, entry := range packages {
		directory := filepath.Join(project, ".ridu", "packages", entry.Name())
		tarball := filepath.Join(npmRoot, entry.Name()+".tgz")
		if err := command(directory, "bun", "pm", "pack", "--filename", tarball, "--ignore-scripts", "--quiet").Run(); err != nil {
			return fmt.Errorf("pack %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// writeEnvironment points every installer at the local release and keeps its caches apart, so
// no local version reaches the real npm, pnpm, Bun, Go or Ridu CLI caches.
func writeEnvironment(out, version string) error {
	goCache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return err
	}
	registry := "http://" + defaultAddress + "/"
	cache := filepath.Join(out, "cache")
	lines := []string{
		"# Install and run the local Ridu release like a published one: source this file first.",
		"export LOCAL_RIDU_VERSION=" + version,
		"export npm_config_registry=" + registry,
		"export pnpm_config_registry=" + registry,
		"export BUN_CONFIG_REGISTRY=" + registry,
		"export npm_config_cache=" + filepath.Join(cache, "npm"),
		"export pnpm_config_store_dir=" + filepath.Join(cache, "pnpm-store"),
		"export BUN_INSTALL_CACHE_DIR=" + filepath.Join(cache, "bun"),
		"export RIDU_CLI_RELEASE_BASE_URL=http://" + defaultAddress + "/releases/v" + version,
		"export RIDU_CLI_CACHE_DIR=" + filepath.Join(cache, "ridu-cli"),
		"export GOMODCACHE=" + filepath.Join(cache, "gomod"),
		// The real module download cache answers everything else without network access.
		"export GOPROXY=file://" + filepath.Join(out, "goproxy") + ",file://" + filepath.Join(strings.TrimSpace(string(goCache)), "cache", "download") + ",https://proxy.golang.org",
		"export GONOSUMDB=github.com/riducms/ridu",
		"export GOWORK=off",
	}
	return os.WriteFile(filepath.Join(out, "env.sh"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func command(directory, name string, arguments ...string) *exec.Cmd {
	cmd := exec.Command(name, arguments...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	return cmd
}
