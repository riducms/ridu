package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/pluginregistry"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/schema"
)

const riduModulePath = "github.com/riducms/ridu"

// frameworkPackagePin matches one @riducms dependency and its requirement in a
// package.json file, so pins can be rewritten without reformatting the file.
var frameworkPackagePin = regexp.MustCompile(`("@riducms/[a-z0-9][a-z0-9._-]*"\s*:\s*")([^"]*)(")`)

// skippedPackageDirectories never contain the project's own package manifests.
// Hidden directories, such as .ridu, .svelte-kit, and .vercel, are skipped too:
// they hold caches and build output that copy package.json files.
var skippedPackageDirectories = map[string]bool{
	"node_modules": true, "dist": true, "build": true, "coverage": true, "vendor": true,
}

type upgradeResult struct {
	packageFiles []string
	pluginKeys   []string
	skipped      []string
}

func runUpgrade(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	flags := flag.NewFlagSet("ridu upgrade", flag.ContinueOnError)
	flags.SetOutput(stderr)
	noInstall := flags.Bool("no-install", false, "only rewrite version pins; skip installing, generating, and migration creation")
	// Accept flags before or after the version, as in `ridu upgrade 0.5.0 --no-install`.
	var positional []string
	for remaining := args; ; {
		if err := flags.Parse(remaining); err != nil {
			return 2
		}
		if flags.NArg() == 0 {
			break
		}
		positional = append(positional, flags.Arg(0))
		remaining = flags.Args()[1:]
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "usage: ridu upgrade <version> [--no-install]\nexample: ridu upgrade 0.5.0")
		return 2
	}
	version := strings.TrimPrefix(strings.TrimSpace(positional[0]), "v")
	if !schema.IsValidSemanticVersion(version) {
		fmt.Fprintf(stderr, "ridu upgrade needs an exact release version such as 0.5.0; got %q\n", positional[0])
		return 2
	}
	definition, err := projectfile.Discover(options.WorkingDirectory)
	if err != nil {
		output.Error("find the Ridu project", err)
		return 1
	}

	result, err := rewriteFrameworkPins(definition, version)
	if err != nil {
		output.Error("update Ridu version pins", err)
		return 1
	}
	if err := runForeground(ctx, definition.Root, nil, stdout, stderr, "go", "mod", "edit", "-require="+riduModulePath+"@v"+version); err != nil {
		output.Error("update the Go module requirement", err)
		return 1
	}
	fmt.Fprintf(stdout, "Set %s to v%s in go.mod.\n", riduModulePath, version)
	for _, path := range result.packageFiles {
		fmt.Fprintf(stdout, "Set @riducms packages to %s in %s.\n", version, relativePath(definition.Root, path))
	}
	if len(result.pluginKeys) != 0 {
		fmt.Fprintf(stdout, "Set official plugins %s to %s in %s.\n", strings.Join(result.pluginKeys, ", "), version, definition.Plugins)
	}
	for _, skipped := range result.skipped {
		output.Warn("left a linked package unchanged", errors.New(skipped))
	}
	if replaced, workFile := goWorkReplacesRidu(definition.Root); replaced {
		output.Warn("go.work still replaces the Ridu module", fmt.Errorf("%s points %s at a local checkout; delete it to build against v%s", relativePath(definition.Root, workFile), riduModulePath, version))
	}

	migrationName := "ridu-" + strings.NewReplacer(".", "-", "+", "-").Replace(version)
	if *noInstall {
		install, run := packageManagerUserCommands(definition.FrontendPackageManager())
		fmt.Fprintf(stdout, "\nNext:\n  go mod tidy\n  %s\n  %s ridu generate\n  %s ridu agent sync\n  %s ridu migrate create --name %s\n", install, run, run, run, migrationName)
		return 0
	}

	manager := definition.FrontendPackageManager()
	if err := runForeground(ctx, definition.Root, nil, stdout, stderr, string(manager), "install"); err != nil {
		output.Error("install the upgraded packages", err)
		return 1
	}
	if err := runForeground(ctx, definition.Root, nil, stdout, stderr, "go", "mod", "tidy"); err != nil {
		output.Error("tidy the Go module", err)
		return 1
	}
	// The remaining steps must run with the upgraded CLI, which generates
	// contracts and plans migrations for the new release.
	if err := runUpgradedCLI(ctx, definition, stdout, stderr, "generate"); err != nil {
		output.Error("regenerate contracts with the upgraded CLI", err)
		return 1
	}
	if fileExists(filepath.Join(definition.Root, ".ridu-agent-docs.json")) {
		if err := runUpgradedCLI(ctx, definition, stdout, stderr, "agent", "sync"); err != nil {
			output.Error("synchronize agent documentation", err)
			return 1
		}
	}
	created, err := createUpgradeMigration(ctx, definition, migrationName, stdout)
	if err != nil {
		output.Warn("could not create the upgrade migration", err)
		_, run := packageManagerUserCommands(manager)
		fmt.Fprintf(stdout, "Run `%s ridu migrate create --name %s` after resolving the error above.\n", run, migrationName)
		return 1
	}
	if created {
		fmt.Fprintln(stdout, "Review the new migration and commit it with the upgrade.")
	} else {
		fmt.Fprintln(stdout, "The schema did not change, so no migration is needed.")
	}
	fmt.Fprintf(stdout, "Upgraded to Ridu %s. Run your checks and tests before deploying.\n", version)
	return 0
}

// rewriteFrameworkPins sets every @riducms package and official plugin to one
// exact release. Linked specifiers such as workspace: or file: are reported and
// left alone.
func rewriteFrameworkPins(definition projectfile.File, version string) (upgradeResult, error) {
	var result upgradeResult
	manifests, err := projectPackageManifests(definition.Root)
	if err != nil {
		return result, err
	}
	for _, path := range manifests {
		contents, err := os.ReadFile(path)
		if err != nil {
			return result, err
		}
		changed := false
		rewritten := frameworkPackagePin.ReplaceAllFunc(contents, func(match []byte) []byte {
			parts := frameworkPackagePin.FindSubmatch(match)
			current := string(parts[2])
			if linkedPackageSpecifier(current) {
				result.skipped = append(result.skipped, fmt.Sprintf("%s: %s is %q", relativePath(definition.Root, path), strings.Split(string(parts[1]), `"`)[1], current))
				return match
			}
			if current != version {
				changed = true
			}
			return []byte(string(parts[1]) + version + string(parts[3]))
		})
		if !changed {
			continue
		}
		if err := atomicWriteFile(path, rewritten); err != nil {
			return result, err
		}
		result.packageFiles = append(result.packageFiles, path)
	}
	if definition.Plugins == "" {
		return result, nil
	}
	registryPath := definition.Absolute(definition.Plugins)
	if !fileExists(registryPath) {
		return result, nil
	}
	registry, err := pluginregistry.Load(registryPath)
	if err != nil {
		return result, err
	}
	changed := false
	for index, entry := range registry.Plugins {
		official := false
		if strings.HasPrefix(entry.GoPackage, riduModulePath+"/") {
			official = true
			if entry.GoVersion != "v"+version {
				registry.Plugins[index].GoVersion, changed = "v"+version, true
			}
		}
		if strings.HasPrefix(entry.AdminPackage, "@riducms/") {
			official = true
			if entry.AdminVersion != version {
				registry.Plugins[index].AdminVersion, changed = version, true
			}
		}
		if official {
			result.pluginKeys = append(result.pluginKeys, entry.Key)
		}
	}
	if !changed {
		result.pluginKeys = nil
		return result, nil
	}
	return result, registry.Write(registryPath, definition.Absolute(definition.PluginGo), "content")
}

// projectPackageManifests finds the project's own package.json files: the root
// and every workspace package beneath it.
func projectPackageManifests(root string) ([]string, error) {
	var manifests []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (skippedPackageDirectories[entry.Name()] || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "package.json" {
			manifests = append(manifests, path)
		}
		return nil
	})
	sort.Strings(manifests)
	return manifests, err
}

func linkedPackageSpecifier(requirement string) bool {
	for _, prefix := range []string{"workspace:", "link:", "file:", "portal:", "npm:", "git", "http:", "https:", "github:"} {
		if strings.HasPrefix(requirement, prefix) {
			return true
		}
	}
	return strings.HasPrefix(requirement, ".") || strings.HasPrefix(requirement, "/")
}

func goWorkReplacesRidu(root string) (bool, string) {
	path := filepath.Join(root, "go.work")
	contents, err := os.ReadFile(path)
	if err != nil {
		return false, ""
	}
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "replace"))
		if len(fields) != 0 && fields[0] == riduModulePath && strings.Contains(line, "=>") {
			return true, path
		}
	}
	return false, ""
}

// runUpgradedCLI runs a command with the CLI version now installed in the project.
func runUpgradedCLI(ctx context.Context, definition projectfile.File, stdout, stderr io.Writer, arguments ...string) error {
	name, commandArguments, err := upgradedCLICommand(definition, arguments...)
	if err != nil {
		return err
	}
	return runForeground(ctx, definition.Root, nil, stdout, stderr, name, commandArguments...)
}

func upgradedCLICommand(definition projectfile.File, arguments ...string) (string, []string, error) {
	binary := filepath.Join(definition.Root, "node_modules", ".bin", "ridu")
	if fileExists(binary) {
		return binary, arguments, nil
	}
	return "", nil, fmt.Errorf("the upgraded @riducms/cli is not installed at %s; install dependencies and rerun", relativePath(definition.Root, binary))
}

// createUpgradeMigration asks the upgraded CLI for a migration and reports
// whether the release changed the schema.
func createUpgradeMigration(ctx context.Context, definition projectfile.File, name string, stdout io.Writer) (bool, error) {
	command, arguments, err := upgradedCLICommand(definition, "migrate", "create", "--name", name)
	if err != nil {
		return false, err
	}
	printed, err := commandOutput(ctx, definition.Root, nil, command, arguments...)
	if err != nil {
		return false, fmt.Errorf("%w\n%s", err, strings.TrimSpace(printed))
	}
	if strings.Contains(printed, noMigrationNeeded) {
		return false, nil
	}
	fmt.Fprint(stdout, printed)
	return true, nil
}

func atomicWriteFile(path string, contents []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}
