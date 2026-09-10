// Package cli implements the portable Ridu command-line interface behind the
// thin cmd/ridu entry point.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/internal/agentdocs"
	"github.com/riducms/ridu/internal/generate"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/pluginregistry"
	"github.com/riducms/ridu/internal/pluginscaffold"
	"github.com/riducms/ridu/internal/postgresmigration"
	"github.com/riducms/ridu/internal/project"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/scaffold"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// Options supplies process-owned state to a CLI run.
type Options struct {
	WorkingDirectory     string
	Version              string
	FrameworkVersion     string
	Stdin                io.Reader
	Interactive          bool
	Accessible           bool
	NewProjectResultFile string
}

// Run executes one CLI invocation and returns its process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	output := newCLIOutput(stdout, stderr, cliOutputOptions{accessible: options.Accessible})
	stdout, stderr = output.rawWriters()
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}

	switch args[0] {
	case "version", "--version", "-v":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "ridu version does not accept arguments")
			return 2
		}
		fmt.Fprintf(stdout, "ridu %s\n", options.Version)
		return 0
	case "help", "--help", "-h":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "ridu help does not accept arguments")
			return 2
		}
		printUsage(stdout)
		return 0
	case "new":
		return runNew(ctx, args[1:], stdout, stderr, options)
	case "agent":
		return runAgent(args[1:], stdout, stderr, options)
	case "generate":
		return runGenerate(ctx, args[1:], stdout, stderr, options)
	case "migrate":
		return runMigrate(ctx, args[1:], stdout, stderr, options)
	case "dev":
		return runDev(ctx, args[1:], stdout, stderr, options)
	case "check":
		return runCheck(ctx, args[1:], stdout, stderr, options)
	case "build":
		return runBuild(ctx, args[1:], stdout, stderr, options)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr, options)
	case "plugin":
		return runPlugin(ctx, args[1:], stdout, stderr, options)
	case "add":
		return runPluginAdd(ctx, args[1:], stdout, stderr, options)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func outputFor(stdout, stderr io.Writer, options Options) *cliOutput {
	if writer, ok := stdout.(cliOutputCarrier); ok {
		return writer.cliOutput()
	}
	if writer, ok := stderr.(cliOutputCarrier); ok {
		return writer.cliOutput()
	}
	return newCLIOutput(stdout, stderr, cliOutputOptions{accessible: options.Accessible})
}

func runAgent(args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ridu agent <install|sync> [options]")
		return 2
	}
	definition, err := projectfile.Discover(options.WorkingDirectory)
	if err != nil {
		output.Error("discover project", err)
		return 1
	}
	version := options.Version
	if version == "" {
		version = options.FrameworkVersion
	}
	var result agentdocs.Result
	switch args[0] {
	case "install":
		flags := flag.NewFlagSet("ridu agent install", flag.ContinueOnError)
		flags.SetOutput(stderr)
		agent := flags.String("agent", "codex", "coding agent: codex, claude, cursor, or all")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if flags.NArg() != 0 {
			fmt.Fprintln(stderr, "usage: ridu agent install [--agent codex|claude|cursor|all]")
			return 2
		}
		selection, err := agentdocs.ParseSelection(*agent)
		if err != nil || selection == agentdocs.SelectionNone {
			if err == nil {
				err = fmt.Errorf("agent install does not accept none")
			}
			output.Error("configure agent documentation", err)
			return 2
		}
		result, err = agentdocs.Install(definition.Root, selection, version)
	case "sync":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "ridu agent sync does not accept arguments")
			return 2
		}
		result, err = agentdocs.Sync(definition.Root, version)
	default:
		fmt.Fprintf(stderr, "unknown agent command %q; expected install or sync\n", args[0])
		return 2
	}
	if err != nil {
		output.Error("update agent documentation", err)
		return 1
	}
	verb := "Installed"
	if args[0] == "sync" {
		verb = "Synchronized"
	}
	fmt.Fprintf(stdout, "%s %d managed agent-documentation files.\n", verb, result.ManagedFiles)
	for _, path := range result.PreservedEntrypoint {
		fmt.Fprintf(stdout, "Preserved existing project instructions: %s\n", path)
	}
	fmt.Fprintf(stdout, "Ridu agent documentation matches %s.\n", result.FrameworkVersion)
	return 0
}

func runPlugin(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ridu plugin <add|remove|new> [options]")
		return 2
	}
	switch args[0] {
	case "add":
		return runPluginAdd(ctx, args[1:], stdout, stderr, options)
	case "remove":
		return runPluginRemove(ctx, args[1:], stdout, stderr, options)
	case "new":
		return runPluginNew(args[1:], stdout, stderr, options)
	default:
		fmt.Fprintf(stderr, "unknown plugin command %q; expected add, remove, or new\n", args[0])
		return 2
	}
}

func runPluginNew(args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	args = pluginPositionalLast(args)
	flags := flag.NewFlagSet("ridu plugin new", flag.ContinueOnError)
	flags.SetOutput(stderr)
	key := flags.String("key", "", "stable lowercase plugin key")
	module := flags.String("module", "", "Go module path for the plugin")
	adminPackage := flags.String("admin-package", "", "published admin package name")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 || *key == "" || *module == "" || *adminPackage == "" {
		fmt.Fprintln(stderr, "usage: ridu plugin new <directory> --key <key> --module <go-module> --admin-package <npm-package>")
		return 2
	}
	target := flags.Arg(0)
	if !filepath.IsAbs(target) {
		target = filepath.Join(options.WorkingDirectory, target)
	}
	created, err := pluginscaffold.Create(pluginscaffold.Options{Target: target, Key: *key, ModulePath: *module, AdminPackage: *adminPackage, FrameworkVersion: frameworkVersion(options)})
	if err != nil {
		output.Error("create plugin", err)
		return 1
	}
	fmt.Fprintf(stdout, "Created paired Ridu plugin at %s\n", created)
	fmt.Fprintln(stdout, "Run its Go and admin conformance suites before publishing both packages with matching versions.")
	return 0
}

func runPluginAdd(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	args = pluginPositionalLast(args)
	flags := flag.NewFlagSet("ridu plugin add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	goPackage := flags.String("go-package", "", "compiled Go plugin package")
	goVersion := flags.String("go-version", "latest", "Go module version requirement")
	constructor := flags.String("constructor", "New", "exported zero-argument Go constructor")
	adminPackage := flags.String("admin-package", "", "paired admin package, when present")
	adminVersion := flags.String("admin-version", "latest", "admin package version requirement")
	noInstall := flags.Bool("no-install", false, "update registration without invoking dependency managers")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 || *goPackage == "" {
		fmt.Fprintln(stderr, "usage: ridu plugin add <key> --go-package <package> [--go-version <version>] [--constructor New] [--admin-package <package>] [--admin-version <version>] [--no-install]")
		return 2
	}
	definition, registry, exitCode := loadPluginRegistry(options.WorkingDirectory, output)
	if exitCode != 0 {
		return exitCode
	}
	entry := pluginregistry.Entry{Key: flags.Arg(0), GoPackage: *goPackage, GoVersion: *goVersion, Constructor: *constructor, AdminPackage: *adminPackage}
	if *adminPackage != "" {
		entry.AdminVersion = *adminVersion
	}
	if err := registry.Add(entry); err != nil {
		output.Error("register plugin", err)
		return 1
	}
	rollback, err := snapshotPluginMutation(definition)
	if err != nil {
		output.Error("snapshot project before plugin installation", err)
		return 1
	}
	failed := true
	defer func() {
		if failed {
			if rollbackError := rollback(); rollbackError != nil {
				output.Warn("plugin installation rollback was incomplete", rollbackError)
			}
		}
	}()
	if !*noInstall {
		output.Info("Installing Go dependency", "package", entry.GoPackage, "version", entry.GoVersion)
		if err := runForeground(ctx, definition.Root, nil, stdout, stderr, "go", "get", entry.GoPackage+"@"+entry.GoVersion); err != nil {
			output.Error("install Go plugin dependency", err)
			return 1
		}
		if entry.AdminPackage != "" {
			if definition.Admin == "" {
				output.Error("plugin declares an admin package but this project has no admin directory", nil)
				return 1
			}
			if contractRoot := generatedContractPackageRoot(definition); contractRoot != "" {
				output.Info("Installing generated-contract dependency", "package", entry.AdminPackage, "version", entry.AdminVersion)
				command, arguments := packageManagerAddCommand(definition.FrontendPackageManager(), entry.AdminPackage+"@"+entry.AdminVersion)
				if err := runForeground(ctx, contractRoot, nil, stdout, stderr, command, arguments...); err != nil {
					output.Error("install generated-contract plugin dependency", err)
					return 1
				}
			}
			output.Info("Installing admin dependency", "package", entry.AdminPackage, "version", entry.AdminVersion)
			command, arguments := packageManagerAddCommand(definition.FrontendPackageManager(), entry.AdminPackage+"@"+entry.AdminVersion)
			if err := runForeground(ctx, definition.Absolute(definition.Admin), nil, stdout, stderr, command, arguments...); err != nil {
				output.Error("install admin plugin dependency", err)
				return 1
			}
		}
	}
	if err := writePluginRegistry(definition, registry); err != nil {
		output.Error("write plugin registration", err)
		return 1
	}
	resolved, err := generate.ResolveProject(ctx, definition, frameworkVersion(options))
	if err != nil {
		output.Error("plugin compatibility or contract generation failed; installation was rolled back", err)
		return 1
	}
	manifest := resolved.Manifest
	if missing := missingGeneratedTypeScriptDependencies(definition, manifest); len(missing) != 0 {
		output.Error(fmt.Sprintf("plugin compatibility or contract generation failed; installation was rolled back: generated TypeScript requires %s in %s; add the matching package version there and rerun the command", strings.Join(missing, ", "), generatedContractPackageManifest(definition)), nil)
		return 1
	}
	if _, err := generate.RunResolvedProject(definition, resolved, false); err != nil {
		output.Error("plugin compatibility or contract generation failed; installation was rolled back", err)
		return 1
	}
	failed = false
	fmt.Fprintf(stdout, "Registered plugin %s. Review its descriptor, then create and apply any resulting migration.\n", entry.Key)
	return 0
}

func runPluginRemove(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	args = pluginPositionalLast(args)
	flags := flag.NewFlagSet("ridu plugin remove", flag.ContinueOnError)
	flags.SetOutput(stderr)
	noInstall := flags.Bool("no-install", false, "update registration without invoking dependency managers")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: ridu plugin remove <key> [--no-install]")
		return 2
	}
	definition, registry, exitCode := loadPluginRegistry(options.WorkingDirectory, output)
	if exitCode != 0 {
		return exitCode
	}
	pluginKey := flags.Arg(0)
	var configuredArtifacts []string
	for _, artifact := range definition.GeneratedArtifacts {
		if artifact.Plugin == pluginKey {
			configuredArtifacts = append(configuredArtifacts, "generated."+artifact.Plugin+"."+artifact.Name)
		}
	}
	if len(configuredArtifacts) != 0 {
		output.Error(fmt.Sprintf("cannot remove plugin %s while ridu.toml still configures %s; remove those mappings and their generated files first", pluginKey, strings.Join(configuredArtifacts, ", ")), nil)
		return 1
	}
	removed, err := registry.Remove(pluginKey)
	if err != nil {
		output.Error("remove plugin", err)
		return 1
	}
	rollback, err := snapshotPluginMutation(definition)
	if err != nil {
		output.Error("snapshot project before plugin removal", err)
		return 1
	}
	failed := true
	defer func() {
		if failed {
			if rollbackError := rollback(); rollbackError != nil {
				output.Warn("plugin removal rollback was incomplete", rollbackError)
			}
		}
	}()
	if err := writePluginRegistry(definition, registry); err != nil {
		output.Error("write plugin registration", err)
		return 1
	}
	resolved, err := generate.ResolveProject(ctx, definition, frameworkVersion(options))
	if err != nil {
		output.Error("contract generation failed; plugin removal was rolled back", err)
		return 1
	}
	manifest := resolved.Manifest
	if !*noInstall {
		if removed.AdminPackage != "" {
			if definition.Admin != "" && !manifestRequiresAdminPackage(manifest, removed.AdminPackage) && packageDeclaresDependency(filepath.Join(definition.Absolute(definition.Admin), "package.json"), removed.AdminPackage) {
				command, arguments := packageManagerRemoveCommand(definition.FrontendPackageManager(), removed.AdminPackage)
				if err := runForeground(ctx, definition.Absolute(definition.Admin), nil, stdout, stderr, command, arguments...); err != nil {
					output.Error("plugin registration was removed, but removing the admin dependency failed", err)
					return 1
				}
			}
			contractRoot := generatedContractPackageRoot(definition)
			contractManifest := generatedContractPackageManifest(definition)
			if contractRoot != "" && !manifestRequiresTypeScriptPackage(manifest, removed.AdminPackage) && packageDeclaresDependency(contractManifest, removed.AdminPackage) {
				command, arguments := packageManagerRemoveCommand(definition.FrontendPackageManager(), removed.AdminPackage)
				if err := runForeground(ctx, contractRoot, nil, stdout, stderr, command, arguments...); err != nil {
					output.Error("plugin registration was removed, but removing the generated-contract dependency failed", err)
					return 1
				}
			}
		}
		if err := runForeground(ctx, definition.Root, nil, stdout, stderr, "go", "mod", "tidy"); err != nil {
			output.Error("plugin registration was removed, but Go dependency cleanup failed", err)
			return 1
		}
	}
	if _, err := generate.RunResolvedProject(definition, resolved, false); err != nil {
		output.Error("contract generation failed; plugin removal was rolled back", err)
		return 1
	}
	failed = false
	fmt.Fprintf(stdout, "Removed plugin %s. Create a migration to review any plugin down steps before applying them.\n", removed.Key)
	return 0
}

func pluginPositionalLast(args []string) []string {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return args
	}
	reordered := append([]string(nil), args[1:]...)
	return append(reordered, args[0])
}

func loadPluginRegistry(workingDirectory string, output *cliOutput) (projectfile.File, pluginregistry.Registry, int) {
	definition, err := projectfile.Discover(workingDirectory)
	if err != nil {
		output.Error("discover project", err)
		return projectfile.File{}, pluginregistry.Registry{}, 1
	}
	registry, err := pluginregistry.Load(definition.Absolute(definition.Plugins))
	if err != nil {
		output.Error("load plugin registry", err)
		return projectfile.File{}, pluginregistry.Registry{}, 1
	}
	return definition, registry, 0
}

func writePluginRegistry(definition projectfile.File, registry pluginregistry.Registry) error {
	return registry.Write(definition.Absolute(definition.Plugins), definition.Absolute(definition.PluginGo), "content")
}

type pluginMutationFile struct {
	path    string
	content []byte
	mode    os.FileMode
	exists  bool
}

func snapshotPluginMutation(definition projectfile.File) (func() error, error) {
	paths := []string{
		filepath.Join(definition.Root, "go.mod"),
		filepath.Join(definition.Root, "go.sum"),
		filepath.Join(definition.Root, "package.json"),
		definition.Absolute(definition.Plugins),
		definition.Absolute(definition.PluginGo),
	}
	paths = append(paths, packageManagerLockfiles(definition)...)
	if definition.Admin != "" {
		paths = append(paths, filepath.Join(definition.Absolute(definition.Admin), "package.json"))
	}
	files := make([]pluginMutationFile, 0, len(paths))
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			files = append(files, pluginMutationFile{path: path})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", path, err)
		}
		files = append(files, pluginMutationFile{path: path, content: contents, mode: info.Mode().Perm(), exists: true})
	}
	return func() error {
		var failures []string
		for _, file := range files {
			if file.exists {
				if err := os.WriteFile(file.path, file.content, file.mode); err != nil {
					failures = append(failures, fmt.Sprintf("restore %s: %v", file.path, err))
				}
				continue
			}
			if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
				failures = append(failures, fmt.Sprintf("remove %s: %v", file.path, err))
			}
		}
		if len(failures) != 0 {
			return fmt.Errorf("%s", strings.Join(failures, "; "))
		}
		return nil
	}, nil
}

func packageDeclaresDependency(path, name string) bool {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return false
	}
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies"} {
		var dependencies map[string]string
		if raw, ok := manifest[key]; !ok || json.Unmarshal(raw, &dependencies) != nil {
			continue
		}
		if _, ok := dependencies[name]; ok {
			return true
		}
	}
	return false
}

func generatedContractPackageRoot(definition projectfile.File) string {
	if definition.Client == "" {
		return ""
	}
	return definition.Root
}

func generatedContractPackageManifest(definition projectfile.File) string {
	root := generatedContractPackageRoot(definition)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "package.json")
}

func missingGeneratedTypeScriptDependencies(definition projectfile.File, manifest schema.Manifest) []string {
	packageManifest := generatedContractPackageManifest(definition)
	if packageManifest == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var missing []string
	for _, plugin := range manifest.Snapshot().Plugins {
		for _, fieldType := range plugin.FieldTypes {
			name := npmPackageRoot(fieldType.TypeScriptPackage)
			if name == "" || packageDeclaresDependency(packageManifest, name) {
				continue
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func manifestRequiresAdminPackage(manifest schema.Manifest, name string) bool {
	for _, plugin := range manifest.Snapshot().Plugins {
		if plugin.Admin != nil && npmPackageRoot(plugin.Admin.Package) == npmPackageRoot(name) {
			return true
		}
	}
	return false
}

func npmPackageRoot(specifier string) string {
	parts := strings.Split(strings.TrimSpace(specifier), "/")
	if len(parts) == 0 {
		return ""
	}
	if strings.HasPrefix(parts[0], "@") && len(parts) >= 2 {
		return strings.Join(parts[:2], "/")
	}
	return parts[0]
}

func manifestRequiresTypeScriptPackage(manifest schema.Manifest, name string) bool {
	for _, plugin := range manifest.Snapshot().Plugins {
		for _, fieldType := range plugin.FieldTypes {
			if npmPackageRoot(fieldType.TypeScriptPackage) == npmPackageRoot(name) {
				return true
			}
		}
	}
	return false
}

func runMigrate(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ridu migrate <create|plan|status|up|down|reset|refresh|fresh|verify> [options]")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet("ridu migrate "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	databaseURL := flags.String("database-url", "", "PostgreSQL or MongoDB URL (defaults to DATABASE_URL)")
	databasePath := flags.String("database-path", "", "SQLite path (defaults to RIDU_SQLITE_PATH)")
	allowInsecureDatabase := flags.Bool("allow-insecure-database", false, "explicitly allow plaintext PostgreSQL or MongoDB transport for local operations")
	name := flags.String("name", "", "lowercase kebab-case migration name")
	transformName := flags.String("transform", "", "compiled data-transform name to bind to the immutable migration artifact")
	acceptRenames := flags.Bool("accept-renames", false, "accept every unambiguous detected rename without prompting")
	allowDestructive := flags.Bool("allow-destructive", false, "approve reviewed destructive planning or lifecycle work")
	allowMaintenance := flags.Bool("allow-maintenance", false, "admit traffic-sensitive steps after stopping every application process and worker through completion and retries")
	jsonOutput := flags.Bool("json", false, "emit machine-readable JSON for plan or status")
	allowUnbounded := flags.Bool("allow-unbounded", false, "explicitly admit zero migration timeouts")
	advisoryLockWait := flags.Duration("advisory-lock-wait", 0, "maximum wait for the adapter migration lock")
	lockTimeout := flags.Duration("lock-timeout", 0, "maximum PostgreSQL lock wait per phase")
	statementTimeout := flags.Duration("statement-timeout", 0, "maximum transactional statement duration")
	batchTimeout := flags.Duration("batch-timeout", 0, "maximum duration per checkpoint batch")
	concurrentIndexTimeout := flags.Duration("concurrent-index-timeout", 0, "maximum PostgreSQL concurrent-index operation or complete MongoDB migration duration")
	idleTransactionTimeout := flags.Duration("idle-in-transaction-timeout", 0, "maximum idle time in a transaction phase")
	stopAfterPhase := flags.String("stop-after-phase", "", "stop after a committed phase boundary")
	stopAfterStep := flags.String("stop-after-step", "", "stop after a committed step boundary")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "ridu migrate %s does not accept positional arguments\n", command)
		return 2
	}
	if command != "create" && command != "plan" && command != "status" && command != "up" && command != "down" && command != "reset" && command != "refresh" && command != "fresh" && command != "verify" {
		fmt.Fprintf(stderr, "unknown migrate command %q; expected create, plan, status, up, down, reset, refresh, fresh, or verify\n", command)
		return 2
	}
	lifecycleCommand := command == "down" || command == "reset" || command == "refresh" || command == "fresh"
	if command != "create" && (*name != "" || *transformName != "" || *acceptRenames) || command != "create" && !lifecycleCommand && *allowDestructive {
		fmt.Fprintf(stderr, "ridu migrate %s does not accept create-only migration options\n", command)
		return 2
	}
	if command == "create" && *allowInsecureDatabase {
		fmt.Fprintln(stderr, "ridu migrate create does not accept --allow-insecure-database")
		return 2
	}
	if command != "up" && command != "verify" && *allowMaintenance {
		fmt.Fprintf(stderr, "ridu migrate %s does not accept --allow-maintenance\n", command)
		return 2
	}
	if command != "plan" && command != "status" && *jsonOutput {
		fmt.Fprintf(stderr, "ridu migrate %s does not accept --json\n", command)
		return 2
	}
	operationalFlag := false
	postgresOnlyRunnerFlag := false
	databaseURLFlag := false
	databasePathFlag := false
	flags.Visit(func(candidate *flag.Flag) {
		switch candidate.Name {
		case "database-url":
			databaseURLFlag = true
		case "database-path":
			databasePathFlag = true
		case "allow-unbounded", "advisory-lock-wait", "concurrent-index-timeout":
			operationalFlag = true
		case "lock-timeout", "statement-timeout", "batch-timeout", "idle-in-transaction-timeout", "stop-after-phase", "stop-after-step":
			operationalFlag = true
			postgresOnlyRunnerFlag = true
		}
	})
	if !databaseURLFlag {
		*databaseURL = os.Getenv("DATABASE_URL")
	}
	if !databasePathFlag {
		*databasePath = os.Getenv("RIDU_SQLITE_PATH")
	}
	if command != "up" && command != "verify" && operationalFlag {
		fmt.Fprintf(stderr, "ridu migrate %s does not accept runner timeout or stop-boundary options\n", command)
		return 2
	}
	if command != "up" && (*stopAfterPhase != "" || *stopAfterStep != "") {
		fmt.Fprintf(stderr, "ridu migrate %s does not accept stop boundaries\n", command)
		return 2
	}
	definition, err := projectfile.Discover(options.WorkingDirectory)
	if err != nil {
		output.Error("discover project", err)
		return 1
	}
	if definition.Migrations == "" {
		output.Error("project ridu.toml does not configure a migrations directory", nil)
		return 1
	}
	if definition.Database == projectfile.DatabasePostgres && databasePathFlag {
		fmt.Fprintln(stderr, "PostgreSQL projects do not accept --database-path or RIDU_SQLITE_PATH")
		return 2
	}
	if definition.Database == projectfile.DatabaseSQLite && databaseURLFlag {
		fmt.Fprintln(stderr, "SQLite projects do not accept --database-url or DATABASE_URL")
		return 2
	}
	if definition.Database == projectfile.DatabasePostgres && lifecycleCommand {
		fmt.Fprintf(stderr, "ridu migrate %s is not implemented for PostgreSQL immutable artifacts; select SQLite or use the existing PostgreSQL recovery procedure\n", command)
		return 2
	}
	if definition.Database == projectfile.DatabaseMongoDB && lifecycleCommand {
		fmt.Fprintf(stderr, "ridu migrate %s is not implemented for MongoDB immutable artifacts; use a reviewed forward migration or restore the complete recovery point\n", command)
		return 2
	}
	if definition.Database == projectfile.DatabaseMongoDB && command != "create" && databasePathFlag {
		fmt.Fprintln(stderr, "MongoDB projects do not accept --database-path or RIDU_SQLITE_PATH")
		return 2
	}
	if definition.Database == projectfile.DatabaseMongoDB && postgresOnlyRunnerFlag {
		fmt.Fprintf(stderr, "ridu migrate %s does not accept PostgreSQL-only runner timeout or stop-boundary options\n", command)
		return 2
	}
	if definition.Database == projectfile.DatabaseMongoDB && command != "create" && *databaseURL == "" {
		fmt.Fprintln(stderr, "MongoDB URL is required through --database-url or DATABASE_URL")
		return 2
	}
	if definition.Database == projectfile.DatabasePostgres && command != "create" && *databaseURL == "" {
		fmt.Fprintln(stderr, "PostgreSQL URL is required through --database-url or DATABASE_URL")
		return 2
	}
	if definition.Database == projectfile.DatabaseSQLite && command != "create" && command != "verify" && *databasePath == "" {
		fmt.Fprintln(stderr, "SQLite path is required through --database-path or RIDU_SQLITE_PATH")
		return 2
	}
	if definition.Database == projectfile.DatabaseSQLite && lifecycleCommand && !*allowDestructive {
		fmt.Fprintf(stderr, "ridu migrate %s can remove schema or data; rerun with --allow-destructive after reviewing the selected SQLite database\n", command)
		return 2
	}
	if definition.Database == projectfile.DatabaseSQLite && (*allowInsecureDatabase || *allowMaintenance || operationalFlag) {
		fmt.Fprintf(stderr, "ridu migrate %s does not accept PostgreSQL transport, maintenance, timeout, or stop-boundary options for SQLite\n", command)
		return 2
	}
	directory := definition.Absolute(definition.Migrations)
	if definition.Database == projectfile.DatabaseMongoDB && command == "create" {
		if databaseURLFlag || databasePathFlag {
			fmt.Fprintln(stderr, "ridu migrate create for MongoDB is offline and does not accept --database-url or --database-path")
			return 2
		}
		return runMongoDBMigrateCreate(ctx, *name, *transformName, *acceptRenames, *allowDestructive, directory, definition, stdout, stderr, options)
	}
	var executableManifest schema.Manifest
	var sqliteDataTransforms []migration.DataTransformDescriptor
	mongoDBProjectMigration := false
	postgresProjectMigration := false
	if command != "create" {
		resolved, err := generate.ResolveProjectMetadata(ctx, definition, frameworkVersion(options))
		if err != nil {
			output.Error("resolve project schema", err)
			return 1
		}
		executableManifest = resolved.Manifest
		if definition.Database == projectfile.DatabaseSQLite {
			sqliteDataTransforms = append([]migration.DataTransformDescriptor(nil), resolved.DataTransforms...)
		} else {
			files, historyErr := migrationartifact.RequireCurrentHistory(directory, resolved.Manifest)
			if historyErr != nil {
				output.Error("validate migration artifact history", historyErr)
				return 1
			}
			if definition.Database == projectfile.DatabaseMongoDB {
				mongoDBProjectMigration = mongoDBHistoryRequiresProjectDriver(files)
			} else if definition.Database == projectfile.DatabasePostgres {
				postgresProjectMigration = mongoDBHistoryRequiresProjectDriver(files)
			}
		}
	}
	if definition.Database == projectfile.DatabaseMongoDB {
		if mongoDBProjectMigration && (command == "up" || command == "verify") {
			projectRequest := migration.ProjectRequest{
				Action: migration.ProjectAction(command), DatabaseURL: *databaseURL, Directory: directory,
				AllowInsecureDatabase: *allowInsecureDatabase, AllowMaintenance: *allowMaintenance, AllowUnbounded: *allowUnbounded,
				LockWait: *advisoryLockWait, OperationTimeout: *concurrentIndexTimeout,
			}
			if err := generate.RunProjectMigration(ctx, definition, frameworkVersion(options), projectRequest); err != nil {
				output.Error(command+" migrations", err)
				return 1
			}
			if command == "verify" {
				fmt.Fprintln(stdout, "Migration history and compiled data transforms replay cleanly in an isolated MongoDB database.")
			} else {
				fmt.Fprintln(stdout, "Migrations are current.")
			}
			return 0
		}
		return runMongoDBMigrate(ctx, mongoDBMigrationCLIOptions{
			command: command, databaseURL: *databaseURL, allowInsecureDatabase: *allowInsecureDatabase,
			jsonOutput: *jsonOutput, directory: directory, executableManifest: executableManifest,
			runnerOptions: mongodb.RunnerOptions{
				AllowMaintenance: *allowMaintenance, AllowUnbounded: *allowUnbounded,
				LeaseWait:        *advisoryLockWait,
				OperationTimeout: *concurrentIndexTimeout,
			},
		}, stdout, stderr, output)
	}
	runnerOptions := postgres.RunnerOptions{
		AllowInsecureDatabase: *allowInsecureDatabase,
		AllowMaintenance:      *allowMaintenance, AllowUnbounded: *allowUnbounded,
		AdvisoryLockWait: *advisoryLockWait, LockTimeout: *lockTimeout, StatementTimeout: *statementTimeout,
		BatchTimeout: *batchTimeout, ConcurrentIndexTimeout: *concurrentIndexTimeout,
		IdleInTransactionTimeout: *idleTransactionTimeout,
		StopAfterPhase:           *stopAfterPhase, StopAfterStep: *stopAfterStep,
		Notice: func(notice postgres.MigrationNotice) {
			fmt.Fprintf(stdout, "NOTICE\t%s\t%s\t%s\n", notice.Code, notice.Artifact, notice.Message)
		},
	}
	if definition.Database == projectfile.DatabaseSQLite {
		return runSQLiteMigrate(ctx, sqliteMigrationCLIOptions{
			command: command, databasePath: *databasePath, name: *name, transformName: *transformName,
			databasePathFlag: databasePathFlag,
			acceptRenames:    *acceptRenames, allowDestructive: *allowDestructive, jsonOutput: *jsonOutput,
			directory: directory, definition: definition, dataTransforms: sqliteDataTransforms, executableManifest: executableManifest,
		}, stdout, stderr, options)
	}
	if postgresProjectMigration && (command == "up" || command == "verify") {
		projectRequest := migration.ProjectRequest{
			Action: migration.ProjectAction(command), DatabaseURL: *databaseURL, Directory: directory,
			AllowInsecureDatabase: *allowInsecureDatabase, AllowMaintenance: *allowMaintenance, AllowUnbounded: *allowUnbounded,
			LockWait: *advisoryLockWait, OperationTimeout: *concurrentIndexTimeout,
			LockTimeout: *lockTimeout, StatementTimeout: *statementTimeout, BatchTimeout: *batchTimeout,
			IdleTransactionTimeout: *idleTransactionTimeout, StopAfterPhase: *stopAfterPhase, StopAfterStep: *stopAfterStep,
		}
		if err := generate.RunProjectMigration(ctx, definition, frameworkVersion(options), projectRequest); err != nil {
			output.Error(command+" migrations", err)
			return 1
		}
		if command == "verify" {
			fmt.Fprintln(stdout, "Migration history and compiled data transforms replay cleanly in an isolated PostgreSQL schema.")
		} else if *stopAfterPhase != "" || *stopAfterStep != "" {
			fmt.Fprintln(stdout, "Migration progress is committed at the requested boundary; history is not yet current.")
		} else {
			fmt.Fprintln(stdout, "Migrations are current.")
		}
		return 0
	}
	switch command {
	case "create":
		if *name == "" {
			fmt.Fprintln(stderr, "ridu migrate create requires --name")
			return 2
		}
		resolved, err := generate.ResolveProjectMetadata(ctx, definition, frameworkVersion(options))
		if err != nil {
			output.Error("resolve project schema", err)
			return 1
		}
		manifest := resolved.Manifest
		var transforms []migration.DataTransformDescriptor
		if *transformName != "" {
			for _, descriptor := range resolved.DataTransforms {
				if descriptor.Name == *transformName {
					transforms = append(transforms, descriptor)
					break
				}
			}
			if len(transforms) == 0 {
				output.Error(fmt.Sprintf("compiled project did not register data transform %q with ridu.WithProjectMigrations", *transformName), nil)
				return 1
			}
			if err := validatePostgresTransformCreationHistory(directory, transforms[0]); err != nil {
				output.Error("validate PostgreSQL data transform history", err)
				return 1
			}
		}
		previous, previousExists, err := migrationRenameBase(directory)
		if err != nil {
			output.Error("read migration artifact history", err)
			return 1
		}
		var candidates []schemadiff.RenameCandidate
		if previousExists {
			candidates = schemadiff.RenameCandidates(previous, manifest)
		}
		accepted, err := confirmRenameCandidates(candidates, options.Stdin, stdout, *acceptRenames)
		if err != nil {
			output.Error("confirm schema renames", err)
			return 1
		}
		var previousPointer *schema.Manifest
		previousPlannerVersion := ""
		if previousExists {
			previousPointer = &previous
			previousPlannerVersion, err = migrationHeadPlannerVersion(directory)
			if err != nil {
				output.Error("read migration planner history", err)
				return 1
			}
		}
		artifact, err := buildPostgresArtifactWithDataTransforms(ctx, *name, previousPointer, manifest, postgresRenames(accepted), *allowDestructive, previousPlannerVersion, transforms)
		if err != nil {
			output.Error("plan migration", err)
			return 1
		}
		created, err := migrationartifact.Create(directory, *name, artifact, time.Now())
		if err != nil {
			output.Error("create migration", err)
			return 1
		}
		for _, risk := range artifact.Risks {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", risk.Level, risk.Code, risk.Message)
		}
		relative, _ := filepath.Rel(definition.Root, created.Path)
		fmt.Fprintf(stdout, "Created migration %s; review it before running ridu migrate up.\n", filepath.ToSlash(relative))
	case "plan", "status":
		backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{
			DatabaseURL: *databaseURL, AllowInsecureTransport: *allowInsecureDatabase, ApplicationName: "ridu-migration-inspect",
		})
		if err != nil {
			output.Error("open PostgreSQL", err)
			return 1
		}
		defer backend.Close()
		var statuses []postgres.MigrationStatus
		if command == "plan" {
			statuses, err = backend.ArtifactPlan(ctx, directory)
		} else {
			statuses, err = backend.ArtifactStatus(ctx, directory)
		}
		if err != nil {
			output.Error("migration status", err)
			return 1
		}
		if *jsonOutput {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(statuses); err != nil {
				output.Error("encode migration "+command, err)
				return 1
			}
			break
		}
		if len(statuses) == 0 {
			fmt.Fprintln(stdout, "No migration files found.")
		}
		for _, status := range statuses {
			state := "pending"
			if status.Applied {
				state = "applied"
			}
			fmt.Fprintf(stdout, "%s\t%s", state, status.Name)
			fmt.Fprintln(stdout)
			if command == "plan" {
				for _, phase := range status.Phases {
					fmt.Fprintf(stdout, "  %s\t%s\t%s\n", phase.State, phase.Mode, phase.ID)
					for _, step := range phase.Steps {
						fmt.Fprintf(stdout, "    %s\t%s\t%s\n", step.State, step.Kind, step.ID)
					}
				}
			}
		}
	case "up":
		backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{
			DatabaseURL: *databaseURL, AllowInsecureTransport: *allowInsecureDatabase, ApplicationName: "ridu-migration-up",
		})
		if err != nil {
			output.Error("open PostgreSQL", err)
			return 1
		}
		defer backend.Close()
		if err := backend.ApplyArtifactsWithOptions(ctx, directory, runnerOptions); err != nil {
			output.Error("apply migrations", err)
			return 1
		}
		statuses, err := backend.ArtifactStatus(ctx, directory)
		if err != nil {
			output.Error("confirm migration status", err)
			return 1
		}
		for _, status := range statuses {
			if !status.Applied {
				fmt.Fprintln(stdout, "Migration progress is committed at the requested boundary; history is not yet current.")
				return 0
			}
		}
		fmt.Fprintln(stdout, "Migrations are current.")
	case "verify":
		if err := postgres.VerifyArtifactsWithOptions(ctx, *databaseURL, directory, runnerOptions); err != nil {
			output.Error("verify migrations", err)
			return 1
		}
		fmt.Fprintln(stdout, "Migration history replays cleanly in an isolated shadow schema.")
	}
	return 0
}

func runMongoDBMigrateCreate(
	ctx context.Context,
	name string,
	transformName string,
	acceptRenames bool,
	allowDestructive bool,
	directory string,
	definition projectfile.File,
	stdout io.Writer,
	stderr io.Writer,
	options Options,
) int {
	output := outputFor(stdout, stderr, options)
	if name == "" {
		fmt.Fprintln(stderr, "ridu migrate create requires --name")
		return 2
	}
	resolved, err := generate.ResolveProjectMetadataOffline(ctx, definition, frameworkVersion(options))
	if err != nil {
		output.Error("resolve project schema", err)
		return 1
	}
	previous, previousExists, err := migrationRenameBase(directory)
	if err != nil {
		output.Error("read migration artifact history", err)
		return 1
	}
	var candidates []schemadiff.RenameCandidate
	if previousExists {
		candidates = schemadiff.RenameCandidates(previous, resolved.Manifest)
	}
	accepted, err := confirmRenameCandidates(candidates, options.Stdin, stdout, acceptRenames)
	if err != nil {
		output.Error("confirm schema renames", err)
		return 1
	}
	var transforms []migration.DataTransformDescriptor
	if transformName != "" {
		for _, descriptor := range resolved.DataTransforms {
			if descriptor.Name == transformName {
				transforms = append(transforms, descriptor)
				break
			}
		}
		if len(transforms) == 0 {
			output.Error(fmt.Sprintf("compiled project did not register data transform %q with ridu.WithProjectMigrations", transformName), nil)
			return 1
		}
	}
	created, err := mongodb.CreateArtifactWithOptions(ctx, directory, name, resolved.Manifest, time.Now(), mongodb.ArtifactOptions{
		AllowDestructive: allowDestructive,
		Renames:          mongoDBRenames(accepted),
		DataTransforms:   transforms,
	})
	if err != nil {
		output.Error("plan migration", err)
		return 1
	}
	if files, readError := migrationartifact.ReadAll(directory); readError == nil {
		for _, file := range files {
			if file.Name != created.Name || file.Digest != created.Checksum {
				continue
			}
			for _, risk := range file.Artifact.Risks {
				fmt.Fprintf(stdout, "%s\t%s\t%s\n", risk.Level, risk.Code, risk.Message)
			}
			break
		}
	}
	relative, _ := filepath.Rel(definition.Root, created.Path)
	fmt.Fprintf(stdout, "Created migration %s; review and commit it before running ridu migrate up.\n", filepath.ToSlash(relative))
	return 0
}

type mongoDBMigrationCLIOptions struct {
	command               string
	databaseURL           string
	allowInsecureDatabase bool
	jsonOutput            bool
	directory             string
	executableManifest    schema.Manifest
	runnerOptions         mongodb.RunnerOptions
}

func runMongoDBMigrate(ctx context.Context, request mongoDBMigrationCLIOptions, stdout, stderr io.Writer, output *cliOutput) int {
	config := mongodb.Config{
		DatabaseURL:            request.databaseURL,
		AllowInsecureTransport: request.allowInsecureDatabase,
		ApplicationName:        "ridu-migration-" + request.command,
	}
	switch request.command {
	case "plan", "status":
		statuses, err := mongodb.InspectArtifacts(ctx, config, request.directory, request.executableManifest)
		if err != nil {
			output.Error("migration status", err)
			return 1
		}
		if request.jsonOutput {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(statuses); err != nil {
				output.Error("encode migration "+request.command, err)
				return 1
			}
			return 0
		}
		if len(statuses) == 0 {
			fmt.Fprintln(stdout, "No migration files found.")
		}
		for _, status := range statuses {
			state := "pending"
			if status.Applied {
				state = "applied"
			}
			fmt.Fprintf(stdout, "%s\t%s\n", state, status.Name)
			if request.command != "plan" {
				continue
			}
			for _, phase := range status.Phases {
				fmt.Fprintf(stdout, "  %s\t%s\t%s\n", phase.State, phase.Mode, phase.ID)
				for _, step := range phase.Steps {
					fmt.Fprintf(stdout, "    %s\t%s\t%s\n", step.State, step.Kind, step.ID)
				}
			}
		}
	case "up":
		projectRequest := migration.ProjectRequest{
			Action: migration.ProjectApply, DatabaseURL: request.databaseURL, Directory: request.directory,
			AllowInsecureDatabase: request.allowInsecureDatabase,
			AllowMaintenance:      request.runnerOptions.AllowMaintenance,
			AllowUnbounded:        request.runnerOptions.AllowUnbounded,
			LockWait:              request.runnerOptions.LeaseWait,
			OperationTimeout:      request.runnerOptions.OperationTimeout,
		}
		if err := mongodb.ProjectMigrations().RunProjectMigration(ctx, projectRequest, request.executableManifest); err != nil {
			output.Error("apply migrations", err)
			return 1
		}
		fmt.Fprintln(stdout, "Migrations are current.")
	case "verify":
		projectRequest := migration.ProjectRequest{
			Action: migration.ProjectVerify, DatabaseURL: request.databaseURL, Directory: request.directory,
			AllowInsecureDatabase: request.allowInsecureDatabase,
			AllowMaintenance:      request.runnerOptions.AllowMaintenance,
			AllowUnbounded:        request.runnerOptions.AllowUnbounded,
			LockWait:              request.runnerOptions.LeaseWait,
			OperationTimeout:      request.runnerOptions.OperationTimeout,
		}
		if err := mongodb.ProjectMigrations().RunProjectMigration(ctx, projectRequest, request.executableManifest); err != nil {
			output.Error("verify migrations", err)
			return 1
		}
		fmt.Fprintln(stdout, "Migration history replays cleanly in an isolated shadow database.")
	default:
		fmt.Fprintf(stderr, "ridu migrate %s is not implemented for MongoDB\n", request.command)
		return 2
	}
	return 0
}

type sqliteMigrationCLIOptions struct {
	command            string
	databasePath       string
	databasePathFlag   bool
	name               string
	transformName      string
	acceptRenames      bool
	allowDestructive   bool
	jsonOutput         bool
	directory          string
	definition         projectfile.File
	dataTransforms     []migration.DataTransformDescriptor
	executableManifest schema.Manifest
}

func runSQLiteMigrate(ctx context.Context, request sqliteMigrationCLIOptions, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	if request.command == "create" {
		if request.name == "" {
			fmt.Fprintln(stderr, "ridu migrate create requires --name")
			return 2
		}
		if request.acceptRenames {
			fmt.Fprintln(stderr, "SQLite rename intent requires a registered transaction-bound data transform; --accept-renames alone cannot rewrite stored canonical JSON")
			return 2
		}
		resolved, err := generate.ResolveProjectMetadata(ctx, request.definition, frameworkVersion(options))
		if err != nil {
			output.Error("resolve project schema", err)
			return 1
		}
		var descriptors []migration.DataTransformDescriptor
		if request.transformName != "" {
			for _, descriptor := range resolved.DataTransforms {
				if descriptor.Name == request.transformName {
					descriptors = append(descriptors, descriptor)
					break
				}
			}
			if len(descriptors) == 0 {
				output.Error(fmt.Sprintf("compiled project did not register data transform %q with ridu.WithProjectMigrations", request.transformName), nil)
				return 1
			}
		}
		created, err := sqlite.CreateArtifact(ctx, request.directory, request.name, resolved.Manifest, time.Now(), request.allowDestructive, descriptors...)
		if err != nil {
			output.Error("plan migration", err)
			return 1
		}
		files, err := migrationartifact.ReadAll(request.directory)
		if err == nil && len(files) != 0 {
			for _, risk := range files[len(files)-1].Artifact.Risks {
				fmt.Fprintf(stdout, "%s\t%s\t%s\n", risk.Level, risk.Code, risk.Message)
			}
		}
		relative, _ := filepath.Rel(request.definition.Root, created.Path)
		fmt.Fprintf(stdout, "Created migration %s; review it before running ridu migrate up.\n", filepath.ToSlash(relative))
		return 0
	}

	hasDataTransforms := len(request.dataTransforms) != 0
	projectAction := request.command == "verify" || request.command == "up" || request.command == "down" || request.command == "reset" || request.command == "refresh" || request.command == "fresh"
	if projectAction {
		databasePath := ""
		if request.command != "verify" {
			var pathError error
			databasePath, pathError = resolveSQLiteMigrationDatabasePathForSource(request.definition.Root, request.databasePath, request.databasePathFlag)
			if pathError != nil {
				output.Error("resolve SQLite path", pathError)
				return 1
			}
			if databasePath == ":memory:" {
				fmt.Fprintf(stderr, "ridu migrate %s requires a persistent SQLite file; :memory: cannot retain applied migration history\n", request.command)
				return 1
			}
		}
		projectRequest := migration.ProjectRequest{
			Action: migration.ProjectAction(request.command), DatabasePath: databasePath, Directory: request.directory,
		}
		var actionError error
		if hasDataTransforms {
			actionError = generate.RunProjectMigration(ctx, request.definition, frameworkVersion(options), projectRequest)
		} else {
			actionError = sqlite.ProjectMigrations().RunProjectMigration(ctx, projectRequest, request.executableManifest)
		}
		if actionError != nil {
			if hasDataTransforms {
				output.Error(request.command+" migrations", actionError)
			} else {
				writeSQLiteMigrationError(output, request.command, actionError)
			}
			return 1
		}
		switch request.command {
		case "verify":
			if hasDataTransforms {
				fmt.Fprintln(stdout, "Migration history and compiled data transforms replay cleanly in an isolated SQLite database.")
			} else {
				fmt.Fprintln(stdout, "Migration history replays cleanly in an isolated SQLite database.")
			}
		case "up":
			fmt.Fprintln(stdout, "Migrations are current.")
		case "down":
			fmt.Fprintln(stdout, "Rolled back the latest applied migration.")
		case "reset":
			fmt.Fprintln(stdout, "Rolled back every applied migration.")
		case "refresh":
			fmt.Fprintln(stdout, "Refreshed committed migration history.")
		case "fresh":
			fmt.Fprintln(stdout, "Dropped the SQLite database schema and replayed committed migration history.")
		}
		return 0
	}

	databasePath, pathError := resolveSQLiteMigrationDatabasePathForSource(request.definition.Root, request.databasePath, request.databasePathFlag)
	if pathError != nil {
		output.Error("resolve SQLite path", pathError)
		return 1
	}

	switch request.command {
	case "plan", "status":
		statuses, err := sqlite.InspectArtifacts(ctx, databasePath, request.directory, request.executableManifest)
		if err != nil {
			output.Error("migration status", err)
			return 1
		}
		if request.jsonOutput {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(statuses); err != nil {
				output.Error("encode migration "+request.command, err)
				return 1
			}
			return 0
		}
		if len(statuses) == 0 {
			fmt.Fprintln(stdout, "No migration files found.")
		}
		for _, status := range statuses {
			state := "pending"
			if status.Applied {
				state = "applied"
			}
			fmt.Fprintf(stdout, "%s\t%s\n", state, status.Name)
			if request.command == "plan" {
				for _, phase := range status.Phases {
					fmt.Fprintf(stdout, "  %s\t%s\t%s\n", phase.State, phase.Mode, phase.ID)
					for _, step := range phase.Steps {
						fmt.Fprintf(stdout, "    %s\t%s\t%s\n", step.State, step.Kind, step.ID)
					}
				}
			}
		}
	}
	return 0
}

func writeSQLiteMigrationError(output *cliOutput, command string, err error) {
	switch command {
	case "verify":
		output.Error("verify migrations", err)
	case "up":
		output.Error("apply migrations", err)
	case "down":
		output.Error("roll back migration", err)
	case "reset":
		output.Error("reset migrations", err)
	case "refresh":
		output.Error("refresh migrations", err)
	case "fresh":
		output.Error("rebuild SQLite database", err)
	}
}

func resolveSQLiteMigrationDatabasePath(projectRoot, input string) (string, error) {
	databasePath := strings.TrimSpace(input)
	if databasePath == "" || databasePath == ":memory:" || filepath.IsAbs(databasePath) {
		return databasePath, nil
	}
	if !strings.HasPrefix(databasePath, "file:") {
		return filepath.Join(projectRoot, databasePath), nil
	}
	parsed, err := url.Parse(databasePath)
	if err != nil {
		return "", fmt.Errorf("parse file URI: %w", err)
	}
	target := parsed.Path
	if target == "" {
		target, err = url.PathUnescape(parsed.Opaque)
		if err != nil {
			return "", fmt.Errorf("decode file URI path: %w", err)
		}
	}
	if target == "" {
		return "", fmt.Errorf("file URI path is required")
	}
	parameters := parsed.Query()
	mode, err := singleSQLiteURIParameter(parameters, "mode")
	if err != nil {
		return "", err
	}
	if target == ":memory:" || strings.EqualFold(mode, "memory") || strings.EqualFold(parameters.Get("vfs"), "memdb") {
		return "", fmt.Errorf("shared SQLite memory URIs are unsupported; use :memory: for a private pooled store")
	}
	if filepath.IsAbs(target) {
		return databasePath, nil
	}
	absolute, err := filepath.Abs(filepath.Join(projectRoot, target))
	if err != nil {
		return "", err
	}
	parsed.Host = ""
	parsed.Opaque = ""
	parsed.Path = absolute
	parsed.RawPath = ""
	return parsed.String(), nil
}

func resolveSQLiteMigrationDatabasePathForSource(projectRoot, input string, explicitFlag bool) (string, error) {
	databasePath := strings.TrimSpace(input)
	resolved, err := resolveSQLiteMigrationDatabasePath(projectRoot, databasePath)
	if err != nil {
		return "", err
	}
	if !explicitFlag && resolved != databasePath {
		return "", fmt.Errorf("RIDU_SQLITE_PATH must be an absolute file path or URI; use --database-path for a project-relative migration path")
	}
	return resolved, nil
}

func singleSQLiteURIParameter(parameters url.Values, name string) (string, error) {
	values := parameters[name]
	if len(values) > 1 {
		return "", fmt.Errorf("SQLite URI parameter %q must not be specified more than once", name)
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], nil
}

func migrationRenameBase(directory string) (schema.Manifest, bool, error) {
	previous, exists, err := migrationartifact.LatestManifest(directory)
	if err != nil {
		return schema.Manifest{}, false, err
	}
	return previous, exists, nil
}

func migrationHeadPlannerVersion(directory string) (string, error) {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}
	return files[len(files)-1].Artifact.Planner.Version, nil
}

func confirmRenameCandidates(candidates []schemadiff.RenameCandidate, input io.Reader, output io.Writer, acceptAll bool) ([]schemadiff.RenameCandidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if acceptAll {
		for _, candidate := range candidates {
			fmt.Fprintf(output, "Accepted %s.\n", renameDescription(candidate))
		}
		return candidates, nil
	}
	if input == nil {
		return nil, fmt.Errorf("rename confirmation requires interactive input or --accept-renames")
	}
	reader := bufio.NewReader(input)
	accepted := make([]schemadiff.RenameCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		fmt.Fprintf(output, "Detected %s. Preserve its existing data as a rename? [Y/n] ", renameDescription(candidate))
		answer, err := reader.ReadString('\n')
		if err != nil && len(answer) == 0 {
			return nil, fmt.Errorf("read rename confirmation: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			accepted = append(accepted, candidate)
		case "n", "no":
		default:
			return nil, fmt.Errorf("answer %q is not yes or no", strings.TrimSpace(answer))
		}
	}
	return accepted, nil
}

func renameDescription(candidate schemadiff.RenameCandidate) string {
	if candidate.Kind == schemadiff.RenameCollection {
		return fmt.Sprintf("collection rename %q -> %q", candidate.BeforeCollection.Slug, candidate.AfterCollection.Slug)
	}
	return fmt.Sprintf("field rename %q.%s -> %q.%s", candidate.BeforeCollection.Slug, candidate.BeforeField.Path, candidate.AfterCollection.Slug, candidate.AfterField.Path)
}

func postgresRenames(candidates []schemadiff.RenameCandidate) []postgres.Rename {
	renamed := make([]postgres.Rename, len(candidates))
	for index, candidate := range candidates {
		renamed[index] = postgres.Rename{
			Kind:             postgres.RenameKind(candidate.Kind),
			BeforeCollection: candidate.BeforeCollection,
			AfterCollection:  candidate.AfterCollection,
			BeforeField:      candidate.BeforeField,
			AfterField:       candidate.AfterField,
		}
		for _, pair := range candidate.Fields {
			renamed[index].Fields = append(renamed[index].Fields, postgres.FieldRename{Before: pair.Before, After: pair.After})
		}
	}
	return renamed
}

func buildPostgresArtifactWithDataTransforms(
	ctx context.Context,
	name string,
	before *schema.Manifest,
	after schema.Manifest,
	renames []postgres.Rename,
	allowDestructive bool,
	previousPlannerVersion string,
	transforms []migration.DataTransformDescriptor,
) (migration.Artifact, error) {
	if before != nil {
		if err := postgresmigration.ValidateVersionedTransition(before.Snapshot(), after.Snapshot(), transforms); err != nil {
			return migration.Artifact{}, err
		}
	}
	artifact, err := postgres.BuildArtifactWithPreviousPlanner(ctx, name, before, after, renames, allowDestructive, previousPlannerVersion)
	if err != nil {
		if len(transforms) == 0 || before == nil || !strings.Contains(err.Error(), "schema is current; no migration steps were planned") {
			return migration.Artifact{}, err
		}
		fromDigest, digestError := migration.DigestManifest(*before)
		if digestError != nil {
			return migration.Artifact{}, digestError
		}
		toDigest, digestError := migration.DigestManifest(after)
		if digestError != nil {
			return migration.Artifact{}, digestError
		}
		if fromDigest != toDigest {
			return migration.Artifact{}, err
		}
		artifact, err = postgresmigration.DataOnlyArtifact(name, migration.Planner{Name: "atlas", Version: postgres.AtlasVersion}, *before, after)
		if err != nil {
			return migration.Artifact{}, err
		}
	}
	return postgresmigration.BindDataTransforms(artifact, transforms)
}

func mongoDBRenames(candidates []schemadiff.RenameCandidate) []migration.Rename {
	renamed := make([]migration.Rename, len(candidates))
	for index, candidate := range candidates {
		renamed[index] = migration.Rename{
			CollectionBefore: candidate.BeforeCollection.Slug,
			CollectionAfter:  candidate.AfterCollection.Slug,
		}
		if candidate.Kind == schemadiff.RenameField {
			renamed[index].FieldBefore = candidate.BeforeField.Path.String()
			renamed[index].FieldAfter = candidate.AfterField.Path.String()
			continue
		}
		for _, pair := range candidate.Fields {
			renamed[index].Fields = append(renamed[index].Fields, migration.FieldRename{
				Before: pair.Before.Path.String(),
				After:  pair.After.Path.String(),
			})
		}
	}
	return renamed
}

func mongoDBHistoryRequiresProjectDriver(files []migrationartifact.File) bool {
	for _, file := range files {
		for _, phase := range file.Artifact.Phases {
			for _, step := range phase.Steps {
				if step.Kind == migration.StepDataTransform {
					return true
				}
			}
		}
	}
	return false
}

func validatePostgresTransformCreationHistory(directory string, selected migration.DataTransformDescriptor) error {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return err
	}
	for _, file := range files {
		for _, phase := range file.Artifact.Phases {
			for _, step := range phase.Steps {
				if step.Kind != migration.StepDataTransform {
					continue
				}
				var payload migration.DataTransformPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("decode migration %s data transform: %w", file.Name, err)
				}
				if payload.Transform.Name == selected.Name && payload.Transform.Checksum != selected.Checksum {
					return fmt.Errorf("data transform %q changed checksum after immutable migration %s; register the changed callback under a new name", selected.Name, file.Name)
				}
			}
		}
	}
	return nil
}

func runNew(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	flags := flag.NewFlagSet("ridu new", flag.ContinueOnError)
	flags.SetOutput(stderr)
	modulePath := flags.String("module", "", "Go module path (defaults to example.com/<project>)")
	npmScope := flags.String("scope", "", "npm scope for the generated admin workspace")
	templateName := flags.String("template", "", "project template: starter or blank")
	databaseName := flags.String("database", "", "database adapter: postgres, sqlite, or mongodb")
	packageManagerName := flags.String("package-manager", "", "frontend package manager: npm, bun, pnpm, or yarn")
	agentName := flags.String("agent", "", "coding agent: codex, claude, cursor, all, or none")
	noAgent := flags.Bool("no-agent", false, "do not install coding-agent guidance")
	releaseVersionOverride := flags.String("release-version", "", "framework dependency release override (development only)")
	if err := parseNewProjectFlags(flags, args); err != nil {
		return 2
	}
	if flags.NArg() > 1 || (flags.NArg() == 0 && !options.Interactive) {
		fmt.Fprintln(stderr, "usage: ridu new [--template starter|blank] [--database postgres|sqlite|mongodb] [--package-manager npm|bun|pnpm|yarn] [--agent codex|claude|cursor|all|none] [--no-agent] [--module path] [--scope @scope] [--release-version version] [directory]")
		return 2
	}
	if *noAgent && strings.TrimSpace(*agentName) != "" {
		fmt.Fprintln(stderr, "ridu new accepts either --agent or --no-agent, not both")
		return 2
	}
	requestedAgent := strings.TrimSpace(*agentName)
	if *noAgent {
		requestedAgent = string(agentdocs.SelectionNone)
	}
	target := ""
	if flags.NArg() == 1 {
		target = flags.Arg(0)
	}
	target, selectedTemplate, database, packageManager, selectedAgent, cancelled, err := selectNewProject(ctx, target, *templateName, *databaseName, *packageManagerName, requestedAgent, stdout, options)
	if err != nil {
		output.Error("configure new project", err)
		return 2
	}
	if cancelled {
		fmt.Fprintln(stdout, "Project creation cancelled.")
		return 0
	}
	projectName := filepath.Base(filepath.Clean(target))
	if *modulePath == "" {
		*modulePath = "example.com/" + projectName
	}
	if *npmScope == "" {
		*npmScope = "@" + projectName
	}
	releaseVersion := options.Version
	if releaseVersion == "" {
		releaseVersion = options.FrameworkVersion
	}
	if *releaseVersionOverride != "" {
		releaseVersion = *releaseVersionOverride
	}
	created, err := scaffold.Create(scaffold.Options{
		Target:           target,
		ModulePath:       *modulePath,
		NPMScope:         *npmScope,
		FrameworkVersion: releaseVersion,
		Template:         selectedTemplate,
		Agent:            selectedAgent,
		Database:         database,
		PackageManager:   packageManager,
	})
	if err != nil {
		output.Error("create project", err)
		return 1
	}
	if *releaseVersionOverride != "" {
		output.Warn("--release-version is a framework-development override; scaffold dependencies use "+releaseVersion, nil)
	}
	fmt.Fprintf(stdout, "Created Ridu %s project at %s\n", selectedTemplate, created)
	output.Info("Resolving Go dependencies")
	if err := runForeground(ctx, created, nil, stdout, stderr, "go", "mod", "tidy"); err != nil {
		printNewRecovery(created, "Go dependency setup", err, output, stderr)
		return 1
	}
	definition, err := projectfile.Discover(created)
	if err == nil {
		var result generate.Result
		result, err = generate.Run(ctx, definition, frameworkVersion(options), false)
		if err == nil {
			for _, warning := range result.Warnings {
				output.Warn(warning, nil)
			}
		}
	}
	if err != nil {
		printNewRecovery(created, "initial contract generation", err, output, stderr)
		return 1
	}
	if err := writeNewProjectResult(options.NewProjectResultFile, created); err != nil {
		output.Error("record created project", err)
		return 1
	}
	fmt.Fprintln(stdout, "Generated initial schema, clients, and admin plugin registry.")
	installCommand, runCommand := packageManagerUserCommands(packageManager)
	fmt.Fprintf(stdout, "\nNext:\n  cd %s\n  %s\n  %s dev\n", filepath.Base(created), installCommand, runCommand)
	return 0
}

func writeNewProjectResult(path, created string) (resultError error) {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); resultError == nil && err != nil {
			resultError = err
		}
	}()
	_, err = fmt.Fprintln(file, created)
	return err
}

func selectNewProject(ctx context.Context, requestedTarget, requestedTemplate, requestedDatabase, requestedPackageManager, requestedAgent string, output io.Writer, options Options) (string, scaffold.Template, projectfile.DatabaseAdapter, projectfile.PackageManager, agentdocs.Selection, bool, error) {
	selected, err := scaffold.ParseTemplate(requestedTemplate)
	if err != nil {
		return "", "", "", "", "", false, err
	}
	database, err := parseNewProjectDatabase(requestedDatabase)
	if err != nil {
		return "", "", "", "", "", false, err
	}
	packageManager, err := scaffold.ParsePackageManager(requestedPackageManager)
	if err != nil {
		return "", "", "", "", "", false, err
	}
	selectedAgent, err := agentdocs.ParseSelection(requestedAgent)
	if err != nil {
		return "", "", "", "", "", false, err
	}
	if !options.Interactive {
		return requestedTarget, selected, database, packageManager, selectedAgent, false, nil
	}
	needsTarget := strings.TrimSpace(requestedTarget) == ""
	needsTemplate := strings.TrimSpace(requestedTemplate) == ""
	needsDatabase := strings.TrimSpace(requestedDatabase) == ""
	needsPackageManager := strings.TrimSpace(requestedPackageManager) == ""
	needsAgent := strings.TrimSpace(requestedAgent) == ""
	if !needsTarget && !needsTemplate && !needsDatabase && !needsPackageManager && !needsAgent {
		return requestedTarget, selected, database, packageManager, selectedAgent, false, nil
	}
	if options.Stdin == nil {
		return "", "", "", "", "", false, fmt.Errorf("interactive input is unavailable")
	}

	target := requestedTarget
	confirmed := true
	fields := make([]huh.Field, 0, 5)
	summaries := make([]func() string, 0, 5)
	if needsTarget {
		fields = append(fields, huh.NewInput().
			Title("Where should Ridu live?").
			Description("Choose a new directory with a lowercase kebab-case name.").
			Placeholder("my-ridu-project").
			Value(&target).
			Validate(scaffold.ValidateTarget))
		summaries = append(summaries, func() string {
			return completedNewProjectStep("Where should Ridu live?", target)
		})
	}
	if needsTemplate {
		options := make([]huh.Option[scaffold.Template], 0, len(scaffold.Templates()))
		for _, definition := range scaffold.Templates() {
			label := fmt.Sprintf("%-7s  %s", definition.Name, definition.Description)
			options = append(options, huh.NewOption(label, definition.Name))
		}
		fields = append(fields, huh.NewSelect[scaffold.Template]().
			Title("How should the project begin?").
			Description("You can add collections and plugins at any time.").
			Options(options...).
			Value(&selected))
		summaries = append(summaries, func() string {
			return completedNewProjectStep("How should the project begin?", string(selected))
		})
	}
	if needsDatabase {
		databaseOptions := make([]huh.Option[projectfile.DatabaseAdapter], 0, len(newProjectDatabases))
		for _, definition := range newProjectDatabases {
			label := fmt.Sprintf("%-10s  %s", definition.Label, definition.Description)
			databaseOptions = append(databaseOptions, huh.NewOption(label, definition.Adapter))
		}
		fields = append(fields, huh.NewSelect[projectfile.DatabaseAdapter]().
			Title("Select a database").
			Description("Connection details stay in runtime environment variables, outside generated config.").
			Options(databaseOptions...).
			Value(&database))
		summaries = append(summaries, func() string {
			return completedNewProjectStep("Select a database", newProjectDatabaseLabel(database))
		})
	}
	if needsPackageManager {
		managerOptions := make([]huh.Option[projectfile.PackageManager], 0, len(scaffold.PackageManagers()))
		for _, definition := range scaffold.PackageManagers() {
			label := fmt.Sprintf("%-5s  %s", definition.Label, definition.Description)
			managerOptions = append(managerOptions, huh.NewOption(label, definition.Name))
		}
		fields = append(fields, huh.NewSelect[projectfile.PackageManager]().
			Title("Select a package manager").
			Description("Ridu uses it for installs, admin development, checks, builds, and plugin packages.").
			Options(managerOptions...).
			Value(&packageManager))
		summaries = append(summaries, func() string {
			return completedNewProjectStep("Select a package manager", packageManagerLabel(packageManager))
		})
	}
	if needsAgent {
		agentOptions := make([]huh.Option[agentdocs.Selection], 0, len(agentdocs.Definitions()))
		for _, definition := range agentdocs.Definitions() {
			label := fmt.Sprintf("%-7s  %s", definition.Selection, definition.Description)
			agentOptions = append(agentOptions, huh.NewOption(label, definition.Selection))
		}
		fields = append(fields, huh.NewSelect[agentdocs.Selection]().
			Title("Which coding agent should Ridu equip?").
			Description("The release-matched skills work offline inside the generated project.").
			Options(agentOptions...).
			Value(&selectedAgent))
		summaries = append(summaries, func() string {
			return completedNewProjectStep("Which coding agent should Ridu equip?", string(selectedAgent))
		})
	}
	fields = append(fields, huh.NewConfirm().
		Title("Create this Ridu project?").
		DescriptionFunc(func() string {
			return fmt.Sprintf("%s template in %s using %s and %s", selected, target, newProjectDatabaseLabel(database), packageManagerLabel(packageManager))
		}, []any{&selected, &target, &database, &packageManager}).
		Affirmative("Create project").
		Negative("Cancel").
		Value(&confirmed))
	summaries = append(summaries, func() string {
		answer := "Cancel"
		if confirmed {
			answer = "Create project"
		}
		return completedNewProjectStep("Create this Ridu project?", answer)
	})

	input := options.Stdin
	if options.Accessible {
		// Huh creates a scanner for each accessible field. Limit reads so one
		// scanner cannot buffer answers intended for the following fields.
		input = singleByteReader{Reader: input}
	}
	steps := make([]newProjectStep, 0, len(fields))
	groups := make([]*huh.Group, 0, len(fields))
	for index, field := range fields {
		group := huh.NewGroup(field)
		steps = append(steps, newProjectStep{field: field, group: group, summary: summaries[index]})
		groups = append(groups, group)
	}
	groups[0].
		Title("Create a Ridu project").
		Description("Remember the content. Forget the runtime.")

	form := huh.NewForm(groups...).
		WithInput(input).
		WithOutput(output).
		WithAccessible(options.Accessible).
		WithLayout(newProjectLayout{steps: steps})
	if options.Accessible {
		// Accessible prompts should be stable plain text for screen readers and
		// redirected output rather than carrying terminal colour escapes.
		form.WithTheme(huh.ThemeFunc(huh.ThemeBase))
	}
	if err := form.RunWithContext(ctx); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", "", "", "", "", true, nil
		}
		return "", "", "", "", "", false, err
	}
	if !confirmed {
		return "", "", "", "", "", true, nil
	}
	return target, selected, database, packageManager, selectedAgent, false, nil
}

func packageManagerLabel(manager projectfile.PackageManager) string {
	for _, definition := range scaffold.PackageManagers() {
		if definition.Name == manager {
			return definition.Label
		}
	}
	return string(manager)
}

type newProjectDatabaseDefinition struct {
	Adapter     projectfile.DatabaseAdapter
	Label       string
	Description string
}

var newProjectDatabases = []newProjectDatabaseDefinition{
	{Adapter: projectfile.DatabasePostgres, Label: "PostgreSQL", Description: "networked production workloads"},
	{Adapter: projectfile.DatabaseSQLite, Label: "SQLite", Description: "local file on one host"},
	{Adapter: projectfile.DatabaseMongoDB, Label: "MongoDB", Description: "transactional replica-set deployments"},
}

func parseNewProjectDatabase(value string) (projectfile.DatabaseAdapter, error) {
	database := projectfile.DatabaseAdapter(strings.TrimSpace(value))
	if database == "" {
		return projectfile.DatabasePostgres, nil
	}
	for _, definition := range newProjectDatabases {
		if definition.Adapter == database {
			return database, nil
		}
	}
	return "", fmt.Errorf("unknown database adapter %q; expected postgres, sqlite, or mongodb", value)
}

func newProjectDatabaseLabel(database projectfile.DatabaseAdapter) string {
	for _, definition := range newProjectDatabases {
		if definition.Adapter == database {
			return definition.Label
		}
	}
	return string(database)
}

type singleByteReader struct {
	io.Reader
}

func (reader singleByteReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 1 {
		buffer = buffer[:1]
	}
	return reader.Reader.Read(buffer)
}

type newProjectStep struct {
	field   huh.Field
	group   *huh.Group
	summary func() string
}

// newProjectLayout keeps completed answers visible, renders only the active
// prompt, and withholds later prompts until the author reaches them. This
// mirrors the progressive history of create-payload-app without introducing a
// second prompt implementation beside Huh.
type newProjectLayout struct {
	steps []newProjectStep
}

func (layout newProjectLayout) View(form *huh.Form) string {
	if len(layout.steps) == 0 {
		return ""
	}
	current := layout.current(form.GetFocusedField())
	parts := make([]string, 0, current+3)
	if header := layout.steps[0].group.Header(); header != "" {
		parts = append(parts, header)
	}
	for index := 0; index <= current; index++ {
		content := layout.steps[index].group.Content()
		if index < current && layout.steps[index].summary != nil {
			content = layout.steps[index].summary()
		}
		if content != "" {
			parts = append(parts, content)
		}
	}
	if footer := layout.steps[current].group.Footer(); footer != "" {
		parts = append(parts, footer)
	}
	return strings.Join(parts, "\n\n")
}

func (layout newProjectLayout) GroupWidth(_ *huh.Form, _ *huh.Group, width int) int {
	return width
}

func (layout newProjectLayout) current(focused huh.Field) int {
	for index, step := range layout.steps {
		if step.field == focused {
			return index
		}
	}
	return 0
}

func completedNewProjectStep(question, answer string) string {
	return fmt.Sprintf("◇ %s\n│ %s", question, answer)
}

func printNewRecovery(created, stage string, err error, output *cliOutput, stderr io.Writer) {
	output.Error(stage+" failed", err)
	fmt.Fprintf(stderr, "\nRidu created the project at %s.\n", created)
	fmt.Fprintln(stderr, "The project was kept so the problem can be fixed without starting over.")
	fmt.Fprintf(stderr, "\nAfter fixing the error:\n  cd %q\n  go mod tidy\n  ridu generate\n", created)
}

func runGenerate(ctx context.Context, args []string, stdout, stderr io.Writer, options Options) int {
	output := outputFor(stdout, stderr, options)
	flags := flag.NewFlagSet("ridu generate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.Bool("check", false, "check generated contract drift without writing")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "ridu generate does not accept positional arguments")
		return 2
	}
	definition, err := projectfile.Discover(options.WorkingDirectory)
	if err != nil {
		output.Error("discover project", err)
		return 1
	}
	frameworkVersion := options.FrameworkVersion
	if frameworkVersion == "" {
		frameworkVersion = options.Version
	}
	result, err := generate.Run(ctx, definition, frameworkVersion, *check)
	if err != nil {
		output.Error("generate project", err)
		return 1
	}
	for _, warning := range result.Warnings {
		output.Warn(warning, nil)
	}
	for _, artifact := range result.Artifacts {
		relative, relativeError := filepath.Rel(definition.Root, artifact.Path)
		if relativeError != nil {
			relative = artifact.Path
		}
		relative = filepath.ToSlash(relative)
		if *check {
			fmt.Fprintf(stdout, "Generated artifact is current: %s\n", relative)
		} else if artifact.Changed {
			fmt.Fprintf(stdout, "Generated %s\n", relative)
		} else {
			fmt.Fprintf(stdout, "Unchanged %s\n", relative)
		}
	}
	return 0
}

func printUsage(writer io.Writer) {
	fmt.Fprintf(writer, `Ridu content-management framework

Usage:
  ridu <command>

Commands:
  new       Create a generated Ridu application
  agent     Install or synchronize release-matched coding-agent guidance
  dev       Run the selected database, generation, API, and admin together
  generate  Resolve executable config and write generated contracts
  migrate   Create artifacts and run the selected adapter's qualified lifecycle
  check     Verify generated drift, Go, TypeScript, and Svelte
  build     Generate the admin and build one application binary
  doctor    Diagnose local project prerequisites
  plugin    Add or remove compiled backend/admin plugins
  add       Install a plugin (alias for plugin add)
  version   Print the Ridu CLI version
  help      Show this help

Contract versions:
  project protocol %d
  schema manifest  %d
`, project.ProtocolVersion, schema.CurrentVersion)
}
