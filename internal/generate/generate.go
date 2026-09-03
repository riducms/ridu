// Package generate obtains a resolved manifest from an executable project and
// installs generated artifacts atomically.
package generate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/riducms/ridu/internal/project"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"golang.org/x/sync/errgroup"
)

// Result describes one generation or drift-check run.
type Result struct {
	Artifacts []ArtifactResult
	Warnings  []string
	// Manifest is the executable schema used for every generated artifact in
	// this run. Release checks use the same resolved value to prove migration
	// history without invoking the project protocol a second time.
	Manifest schema.Manifest
}

// ResolvedProject is one executable project snapshot used by every artifact
// in a generation run. PluginArtifacts are produced inside the compiled
// project so executable plugin options never need to enter the manifest.
type ResolvedProject struct {
	Manifest        schema.Manifest
	PluginArtifacts []project.Artifact
	DataTransforms  []migration.DataTransformDescriptor
}

// ArtifactResult describes one checked or installed generated file.
type ArtifactResult struct {
	Path    string
	Changed bool
}

// Run resolves executable config through the project protocol and writes or
// checks the canonical schema artifact.
func Run(ctx context.Context, definition projectfile.File, frameworkVersion string, check bool) (Result, error) {
	resolved, err := ResolveProject(ctx, definition, frameworkVersion)
	if err != nil {
		return Result{}, err
	}
	return RunResolvedProject(definition, resolved, check)
}

// RunResolved generates project artifacts from a manifest already obtained
// through the versioned executable project protocol. Development uses this to
// build once, resolve the candidate binary, and start that same binary.
func RunResolved(definition projectfile.File, manifest schema.Manifest, check bool) (Result, error) {
	return RunResolvedProject(definition, ResolvedProject{Manifest: manifest}, check)
}

// RunResolvedProject installs artifacts from one already-resolved executable
// project response.
func RunResolvedProject(definition projectfile.File, resolved ResolvedProject, check bool) (Result, error) {
	return runResolved(definition, resolved, check, true)
}

// RunResolvedDevelopment atomically generates project artifacts without
// forcing each disposable intermediate file to stable storage. A rename still
// exposes only a complete old or new artifact; ordinary generation retains the
// stronger per-file durability boundary used by review and release workflows.
func RunResolvedDevelopment(definition projectfile.File, manifest schema.Manifest) (Result, error) {
	return RunResolvedProjectDevelopment(definition, ResolvedProject{Manifest: manifest})
}

// RunResolvedProjectDevelopment installs one executable project response with
// development durability while retaining atomic visibility.
func RunResolvedProjectDevelopment(definition projectfile.File, resolved ResolvedProject) (Result, error) {
	return runResolved(definition, resolved, false, false)
}

func runResolved(definition projectfile.File, resolved ResolvedProject, check, durable bool) (Result, error) {
	manifest := resolved.Manifest
	manifestContent, err := manifest.Bytes()
	if err != nil {
		return Result{}, fmt.Errorf("encode canonical manifest: %w", err)
	}
	warnings := identityChangeWarnings(definition.Absolute(definition.Schema), manifest)
	artifacts := []artifact{{
		path:    definition.Absolute(definition.Schema),
		content: manifestContent,
	}}
	var goContent []byte
	var openAPIContent []byte
	var clientContent []byte
	var group errgroup.Group
	group.Go(func() error {
		generated, err := goClient(manifest)
		if err != nil {
			return err
		}
		goContent = generated
		return nil
	})
	if definition.OpenAPI != "" {
		group.Go(func() error {
			generated, err := openAPI(manifest)
			if err != nil {
				return fmt.Errorf("generate OpenAPI contract: %w", err)
			}
			openAPIContent = generated
			return nil
		})
	}
	if definition.Client != "" {
		group.Go(func() error {
			generated, err := typescript.Client(manifest)
			if err != nil {
				return fmt.Errorf("generate application client: %w", err)
			}
			clientContent = generated
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return Result{}, err
	}
	artifacts = append(artifacts, artifact{path: filepath.Join(filepath.Dir(definition.Absolute(definition.Schema)), "ridu.generated.go"), content: goContent})
	if definition.OpenAPI != "" {
		artifacts = append(artifacts, artifact{path: definition.Absolute(definition.OpenAPI), content: openAPIContent})
	}
	if definition.Client != "" {
		artifacts = append(artifacts, artifact{path: definition.Absolute(definition.Client), content: clientContent})
	}
	for _, destination := range definition.GeneratedArtifacts {
		content, exists := generatedPluginArtifact(resolved.PluginArtifacts, destination.Plugin, destination.Name)
		if !exists {
			return Result{}, fmt.Errorf("ridu.toml configures generated.%s.%s but the compiled plugin did not provide that artifact", destination.Plugin, destination.Name)
		}
		artifacts = append(artifacts, artifact{path: definition.Absolute(destination.Path), content: content})
	}
	if definition.Admin != "" {
		artifacts = append(artifacts, artifact{
			path:    filepath.Join(definition.Absolute(definition.Admin), "src", "ridu.plugins.generated.ts"),
			content: adminPluginRegistry(manifest),
		})
	}
	if err := validateArtifactDestinations(definition.Root, artifacts); err != nil {
		return Result{}, err
	}
	installed, err := installArtifactsWithDurability(artifacts, check, durable, os.Rename)
	if err != nil {
		return Result{}, err
	}
	results := make([]ArtifactResult, len(installed))
	for index, installedArtifact := range installed {
		results[index] = ArtifactResult{Path: installedArtifact.path, Changed: installedArtifact.changed}
	}
	return Result{Artifacts: results, Warnings: warnings, Manifest: manifest}, nil
}

func validateArtifactDestinations(root string, artifacts []artifact) error {
	type destination struct{ canonical, configured string }
	seen := make([]destination, 0, len(artifacts))
	for _, item := range artifacts {
		canonical, err := canonicalArtifactDestination(root, item.path)
		if err != nil {
			return fmt.Errorf("validate generated artifact %s: %w", item.path, err)
		}
		for _, previous := range seen {
			if artifactDestinationsOverlap(previous.canonical, canonical) {
				return fmt.Errorf("generated artifact paths %s and %s overlap", previous.configured, item.path)
			}
		}
		seen = append(seen, destination{canonical: canonical, configured: item.path})
	}
	return nil
}

func artifactDestinationsOverlap(left, right string) bool {
	left = strings.ToLower(filepath.Clean(left))
	right = strings.ToLower(filepath.Clean(right))
	if left == right {
		return true
	}
	leftToRight, leftError := filepath.Rel(left, right)
	rightToLeft, rightError := filepath.Rel(right, left)
	return leftError == nil && pathDescends(leftToRight) || rightError == nil && pathDescends(rightToLeft)
}

func pathDescends(relative string) bool {
	return relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func canonicalArtifactDestination(root, target string) (string, error) {
	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	rootCanonical, err := filepath.EvalSymlinks(rootAbsolute)
	if err != nil {
		return "", fmt.Errorf("resolve project root symlinks: %w", err)
	}
	targetAbsolute, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve destination: %w", err)
	}
	if !pathWithin(rootAbsolute, targetAbsolute) {
		return "", fmt.Errorf("destination is outside the project root")
	}

	current := targetAbsolute
	var suffix []string
	for {
		resolved, resolveError := filepath.EvalSymlinks(current)
		if resolveError == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			if !pathWithin(rootCanonical, resolved) {
				return "", fmt.Errorf("destination resolves outside the project root through a symlink")
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(resolveError) {
			return "", fmt.Errorf("resolve destination symlinks: %w", resolveError)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("could not resolve an existing destination ancestor")
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func generatedPluginArtifact(artifacts []project.Artifact, plugin, name string) ([]byte, bool) {
	for _, artifact := range artifacts {
		if artifact.Plugin == plugin && artifact.Name == name {
			return append([]byte(nil), artifact.Content...), true
		}
	}
	return nil, false
}

func adminPluginRegistry(manifest schema.Manifest) []byte {
	var output strings.Builder
	output.WriteString("// Code generated by Ridu. DO NOT EDIT.\n")
	plugins := manifest.Snapshot().Plugins
	adminCount := 0
	for _, plugin := range plugins {
		if plugin.Admin != nil {
			adminCount++
		}
	}
	if adminCount == 0 {
		output.WriteString("export const generatedAdminPlugins = [] as const;\n")
		return []byte(output.String())
	}

	output.WriteString("import { resolveAdminPluginPairs } from \"@riducms/plugin\";\n")
	adminIndex := 0
	for _, plugin := range plugins {
		if plugin.Admin == nil {
			continue
		}
		fmt.Fprintf(&output, "import { %s as riduAdminPlugin%d } from %s;\n", plugin.Admin.Export, adminIndex, strconv.Quote(plugin.Admin.Package))
		adminIndex++
	}
	for _, plugin := range plugins {
		if plugin.Admin == nil {
			continue
		}
		for _, asset := range plugin.Admin.Assets {
			fmt.Fprintf(&output, "import %s;\n", strconv.Quote(strings.TrimSuffix(plugin.Admin.Package, "/")+"/"+asset))
		}
	}
	output.WriteString("\nconst resolvedAdminPluginPairs = resolveAdminPluginPairs([\n")
	adminIndex = 0
	for _, plugin := range plugins {
		if plugin.Admin == nil {
			continue
		}
		fmt.Fprintf(&output, "\t{\n\t\tbackend: { key: %s, package: %s, export: %s, apiVersion: %d, pairingVersion: %d, routes: %s, assets: %s },\n\t\tadmin: riduAdminPlugin%d,\n\t},\n",
			strconv.Quote(plugin.Key), strconv.Quote(plugin.Admin.Package), strconv.Quote(plugin.Admin.Export), plugin.Admin.APIVersion, plugin.Admin.PairingVersion, typescriptStringArray(plugin.Admin.Routes), typescriptStringArray(plugin.Admin.Assets), adminIndex)
		adminIndex++
	}
	output.WriteString("]);\n\nexport const generatedAdminPlugins = resolvedAdminPluginPairs.plugins;\n")
	return []byte(output.String())
}

// AdminPluginRegistry returns the generated static admin-plugin pairing module.
// It is exported from this internal package so maintained framework fixtures can
// byte-check the same artifact written for generated applications.
func AdminPluginRegistry(manifest schema.Manifest) []byte {
	return adminPluginRegistry(manifest)
}

func typescriptStringArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = strconv.Quote(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func identityChangeWarnings(existingPath string, current schema.Manifest) []string {
	previous, exists, err := schemadiff.ReadManifest(existingPath)
	if err != nil || !exists {
		return nil
	}
	var warnings []string
	for _, candidate := range schemadiff.RenameCandidates(previous, current) {
		switch candidate.Kind {
		case schemadiff.RenameCollection:
			warnings = append(warnings, fmt.Sprintf(
				"possible collection rename %s -> %s; run ridu migrate create to confirm it and preserve existing data",
				candidate.BeforeCollection.Slug, candidate.AfterCollection.Slug,
			))
		case schemadiff.RenameField:
			warnings = append(warnings, fmt.Sprintf(
				"possible field rename %s.%s -> %s.%s; run ridu migrate create to confirm it and preserve existing data",
				candidate.BeforeCollection.Slug, candidate.BeforeField.Path, candidate.AfterCollection.Slug, candidate.AfterField.Path,
			))
		}
	}
	return warnings
}

type commandRunner func(context.Context, string, []string) ([]byte, []byte, error)

// ResolveProjectMetadata obtains the current canonical manifest and migration
// metadata without asking plugins to generate unrelated artifacts.
func ResolveProjectMetadata(ctx context.Context, definition projectfile.File, frameworkVersion string) (ResolvedProject, error) {
	entry := "./" + strings.TrimPrefix(filepath.ToSlash(definition.Entry), "./")
	environment := projectEnvironmentWithoutDatabaseConfiguration()
	return discoverProjectMetadata(ctx, definition, frameworkVersion, func(ctx context.Context, directory string, arguments []string) ([]byte, []byte, error) {
		return runCommandWithEnvironment(ctx, directory, environment, "go", append([]string{"run", entry}, arguments...)...)
	})
}

// ResolveProjectMetadataOffline obtains migration-planning metadata without
// forwarding Ridu's supported database connection or path variables to the
// compiled child. Offline planning does not need them, and a failing child
// must not be able to copy their values into its diagnostics.
func ResolveProjectMetadataOffline(ctx context.Context, definition projectfile.File, frameworkVersion string) (ResolvedProject, error) {
	entry := "./" + strings.TrimPrefix(filepath.ToSlash(definition.Entry), "./")
	environment := projectEnvironmentWithoutDatabaseConfiguration()
	return discoverProjectMetadata(ctx, definition, frameworkVersion, func(ctx context.Context, directory string, arguments []string) ([]byte, []byte, error) {
		return runCommandWithEnvironment(ctx, directory, environment, "go", append([]string{"run", entry}, arguments...)...)
	})
}

// ResolveProject obtains the manifest and exact executable-plugin artifacts
// through the versioned project generation command.
func ResolveProject(ctx context.Context, definition projectfile.File, frameworkVersion string) (ResolvedProject, error) {
	entry := "./" + strings.TrimPrefix(filepath.ToSlash(definition.Entry), "./")
	environment := projectEnvironmentWithoutDatabaseConfiguration()
	return discoverProject(ctx, definition, frameworkVersion, func(ctx context.Context, directory string, arguments []string) ([]byte, []byte, error) {
		return runCommandWithEnvironment(ctx, directory, environment, "go", append([]string{"run", entry}, arguments...)...)
	})
}

// ResolveProjectMetadataExecutable obtains the canonical manifest and
// migration metadata from one already compiled project executable. The
// executable still enters the same private, versioned project protocol as
// ordinary generation.
func ResolveProjectMetadataExecutable(ctx context.Context, definition projectfile.File, frameworkVersion, executable string) (ResolvedProject, error) {
	environment := projectEnvironmentWithoutDatabaseConfiguration()
	return discoverProjectMetadata(ctx, definition, frameworkVersion, func(ctx context.Context, directory string, arguments []string) ([]byte, []byte, error) {
		return runCommandWithEnvironment(ctx, directory, environment, executable, arguments...)
	})
}

// ResolveProjectExecutable obtains one generation snapshot from an already
// compiled project executable.
func ResolveProjectExecutable(ctx context.Context, definition projectfile.File, frameworkVersion, executable string) (ResolvedProject, error) {
	environment := projectEnvironmentWithoutDatabaseConfiguration()
	return discoverProject(ctx, definition, frameworkVersion, func(ctx context.Context, directory string, arguments []string) ([]byte, []byte, error) {
		return runCommandWithEnvironment(ctx, directory, environment, executable, arguments...)
	})
}

func discoverProjectMetadata(ctx context.Context, definition projectfile.File, frameworkVersion string, runner commandRunner) (ResolvedProject, error) {
	return discoverProjectOperation(ctx, definition, frameworkVersion, "manifest", nil, runner)
}

func discoverProject(ctx context.Context, definition projectfile.File, frameworkVersion string, runner commandRunner) (ResolvedProject, error) {
	requests := make([]project.ArtifactRequest, len(definition.GeneratedArtifacts))
	for index, artifact := range definition.GeneratedArtifacts {
		requests[index] = project.ArtifactRequest{Plugin: artifact.Plugin, Name: artifact.Name}
	}
	if len(requests) == 0 {
		return discoverProjectOperation(ctx, definition, frameworkVersion, "manifest", nil, runner)
	}
	return discoverProjectOperation(ctx, definition, frameworkVersion, "generate", requests, runner)
}

func discoverProjectOperation(ctx context.Context, definition projectfile.File, frameworkVersion, operation string, requests []project.ArtifactRequest, runner commandRunner) (ResolvedProject, error) {
	arguments := []string{
		project.Command,
		operation,
		"--protocol-version",
		fmt.Sprint(project.ProtocolVersion),
		"--framework-version",
		frameworkVersion,
	}
	for _, request := range requests {
		arguments = append(arguments, "--artifact", request.Plugin+"/"+request.Name)
	}
	stdout, stderr, err := runner(ctx, definition.Root, arguments)
	if err != nil {
		diagnostic := strings.TrimSpace(string(stderr))
		if diagnostic != "" {
			diagnostic += ": " + err.Error()
		} else {
			diagnostic = err.Error()
		}
		return ResolvedProject{}, fmt.Errorf("project %s command failed: %s", operation, diagnostic)
	}
	response, err := project.DecodeResponse(stdout)
	if err != nil {
		return ResolvedProject{}, fmt.Errorf("read project %s response: %w", operation, err)
	}
	if response.FrameworkVersion != frameworkVersion {
		return ResolvedProject{}, fmt.Errorf("project framework version %q is incompatible with CLI framework version %q; install matching Ridu versions", response.FrameworkVersion, frameworkVersion)
	}
	if response.ManifestVersion != uint32(schema.CurrentVersion) {
		return ResolvedProject{}, fmt.Errorf("project manifest version %d is incompatible with CLI manifest version %d", response.ManifestVersion, schema.CurrentVersion)
	}
	manifest, err := schema.Parse(response.Manifest)
	if err != nil {
		return ResolvedProject{}, fmt.Errorf("validate project manifest: %w", err)
	}
	plugins := make(map[string]struct{}, len(manifest.Snapshot().Plugins))
	for _, plugin := range manifest.Snapshot().Plugins {
		plugins[plugin.Key] = struct{}{}
	}
	wantedArtifacts := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		wantedArtifacts[request.Plugin+"/"+request.Name] = struct{}{}
	}
	for _, artifact := range response.Artifacts {
		if _, exists := plugins[artifact.Plugin]; !exists {
			return ResolvedProject{}, fmt.Errorf("project generated artifact %s/%s for a plugin absent from the manifest", artifact.Plugin, artifact.Name)
		}
		if _, requested := wantedArtifacts[artifact.Plugin+"/"+artifact.Name]; !requested {
			return ResolvedProject{}, fmt.Errorf("project returned unrequested generated artifact %s/%s", artifact.Plugin, artifact.Name)
		}
		delete(wantedArtifacts, artifact.Plugin+"/"+artifact.Name)
	}
	for identity := range wantedArtifacts {
		return ResolvedProject{}, fmt.Errorf("project did not return requested generated artifact %s", identity)
	}
	return ResolvedProject{Manifest: manifest, PluginArtifacts: response.Artifacts, DataTransforms: response.DataTransforms}, nil
}

// RunProjectMigration delegates a callback-bearing lifecycle operation to the
// compiled project process so application functions and adapter schema work
// share one transaction.
func RunProjectMigration(ctx context.Context, definition projectfile.File, frameworkVersion string, request migration.ProjectRequest) error {
	return runProjectMigration(ctx, definition, frameworkVersion, request, func(ctx context.Context, directory string, environment []string, name string, arguments []string) ([]byte, []byte, error) {
		return runCommandWithEnvironment(ctx, directory, environment, name, arguments...)
	})
}

type projectMigrationCommandRunner func(context.Context, string, []string, string, []string) ([]byte, []byte, error)

func runProjectMigration(ctx context.Context, definition projectfile.File, frameworkVersion string, request migration.ProjectRequest, runner projectMigrationCommandRunner) (resultErr error) {
	if ctx == nil {
		return fmt.Errorf("project migration requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	temporaryDirectory, err := os.MkdirTemp("", "ridu-project-migration-")
	if err != nil {
		return fmt.Errorf("create temporary project migration directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(temporaryDirectory); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove temporary project migration directory: %w", err))
		}
	}()

	entry := "./" + strings.TrimPrefix(filepath.ToSlash(definition.Entry), "./")
	executableName := "ridu-project-migration"
	if runtime.GOOS == "windows" {
		executableName += ".exe"
	}
	executable := filepath.Join(temporaryDirectory, executableName)
	buildEnvironment := projectEnvironmentWithoutDatabaseConfiguration()
	_, buildStderr, err := runner(ctx, definition.Root, buildEnvironment, "go", []string{"build", "-o", executable, entry})
	if err != nil {
		return projectMigrationCommandError("build project migration executable", buildStderr, err, request.DatabaseURL)
	}

	arguments := []string{
		project.Command, "migrate",
		"--protocol-version", fmt.Sprint(project.ProtocolVersion),
		"--framework-version", frameworkVersion,
		"--action", string(request.Action),
		"--directory", request.Directory,
	}
	if request.DatabasePath != "" {
		arguments = append(arguments, "--database-path", request.DatabasePath)
	}
	if request.AllowInsecureDatabase {
		arguments = append(arguments, "--allow-insecure-database")
	}
	if request.AllowMaintenance {
		arguments = append(arguments, "--allow-maintenance")
	}
	if request.AllowUnbounded {
		arguments = append(arguments, "--allow-unbounded")
	}
	if request.LockWait != 0 {
		arguments = append(arguments, "--lock-wait", request.LockWait.String())
	}
	if request.OperationTimeout != 0 {
		arguments = append(arguments, "--operation-timeout", request.OperationTimeout.String())
	}
	if request.LockTimeout != 0 {
		arguments = append(arguments, "--lock-timeout", request.LockTimeout.String())
	}
	if request.StatementTimeout != 0 {
		arguments = append(arguments, "--statement-timeout", request.StatementTimeout.String())
	}
	if request.BatchTimeout != 0 {
		arguments = append(arguments, "--batch-timeout", request.BatchTimeout.String())
	}
	if request.IdleTransactionTimeout != 0 {
		arguments = append(arguments, "--idle-transaction-timeout", request.IdleTransactionTimeout.String())
	}
	if request.StopAfterPhase != "" {
		arguments = append(arguments, "--stop-after-phase", request.StopAfterPhase)
	}
	if request.StopAfterStep != "" {
		arguments = append(arguments, "--stop-after-step", request.StopAfterStep)
	}
	environment := append([]string(nil), buildEnvironment...)
	if request.DatabaseURL != "" {
		environment = append(environment, project.MigrationDatabaseURLEnvironment+"="+request.DatabaseURL)
	}
	stdout, stderr, err := runner(ctx, definition.Root, environment, executable, arguments)
	if err != nil {
		return projectMigrationCommandError("project migrate command failed", stderr, err, request.DatabaseURL)
	}
	response, err := project.DecodeResponse(stdout)
	if err != nil {
		return fmt.Errorf("read project migrate response: %w", err)
	}
	if response.FrameworkVersion != frameworkVersion {
		return fmt.Errorf("project framework version %q is incompatible with CLI framework version %q; install matching Ridu versions", response.FrameworkVersion, frameworkVersion)
	}
	if response.ManifestVersion != uint32(schema.CurrentVersion) {
		return fmt.Errorf("project manifest version %d is incompatible with CLI manifest version %d", response.ManifestVersion, schema.CurrentVersion)
	}
	if _, err := schema.Parse(response.Manifest); err != nil {
		return fmt.Errorf("validate project migrate manifest: %w", err)
	}
	return nil
}

func projectMigrationCommandError(prefix string, stderr []byte, commandErr error, databaseURL string) error {
	diagnostic := strings.TrimSpace(string(stderr))
	if diagnostic != "" {
		diagnostic += ": " + commandErr.Error()
	} else {
		diagnostic = commandErr.Error()
	}
	return fmt.Errorf("%s: %s", prefix, redactProjectMigrationDiagnostic(diagnostic, databaseURL))
}

func redactProjectMigrationDiagnostic(diagnostic, databaseURL string) string {
	if diagnostic == "" || databaseURL == "" {
		return diagnostic
	}
	secrets := []string{databaseURL}
	if parsed, err := url.Parse(databaseURL); err == nil {
		if parsed.User != nil {
			secrets = append(secrets, parsed.User.String(), parsed.User.Username())
			if password, exists := parsed.User.Password(); exists {
				secrets = append(secrets, password)
			}
		}
		secrets = append(secrets, parsed.Host)
		secrets = append(secrets, strings.Split(parsed.Host, ",")...)
		for _, parameter := range strings.Split(parsed.RawQuery, "&") {
			_, encodedValue, found := strings.Cut(parameter, "=")
			if !found || encodedValue == "" {
				continue
			}
			secrets = append(secrets, encodedValue)
			if decodedValue, err := url.QueryUnescape(encodedValue); err == nil {
				secrets = append(secrets, decodedValue, url.QueryEscape(decodedValue), url.PathEscape(decodedValue))
			}
		}
		for _, values := range parsed.Query() {
			for _, value := range values {
				secrets = append(secrets, value, url.QueryEscape(value), url.PathEscape(value))
			}
		}
	}
	candidates := secrets
	unique := make(map[string]struct{}, len(candidates))
	secrets = make([]string, 0, len(candidates))
	for _, secret := range candidates {
		if secret == "" {
			continue
		}
		if _, exists := unique[secret]; exists {
			continue
		}
		unique[secret] = struct{}{}
		secrets = append(secrets, secret)
	}
	sort.SliceStable(secrets, func(left, right int) bool { return len(secrets[left]) > len(secrets[right]) })
	for _, secret := range secrets {
		diagnostic = strings.ReplaceAll(diagnostic, secret, "[redacted]")
	}
	return diagnostic
}

func runCommandWithEnvironment(ctx context.Context, directory string, environment []string, name string, arguments ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Env = environment
	stdout := limitedBuffer{limit: project.MaxResponseBytes}
	stderr := limitedBuffer{limit: 1 << 20}
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if stdout.exceeded {
		err = errors.Join(err, fmt.Errorf("project stdout exceeds the %d-byte protocol limit", project.MaxResponseBytes))
	}
	if stderr.exceeded {
		err = errors.Join(err, fmt.Errorf("project stderr exceeds the %d-byte diagnostic limit", stderr.limit))
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func projectEnvironmentWithoutDatabaseConfiguration() []string {
	environment := os.Environ()
	filtered := make([]string, 0, len(environment))
	for _, variable := range environment {
		name, _, _ := strings.Cut(variable, "=")
		if strings.EqualFold(name, "DATABASE_URL") || strings.EqualFold(name, "RIDU_SQLITE_PATH") || strings.EqualFold(name, project.MigrationDatabaseURLEnvironment) {
			continue
		}
		filtered = append(filtered, variable)
	}
	return filtered
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *limitedBuffer) Write(content []byte) (int, error) {
	written := len(content)
	remaining := buffer.limit - buffer.Len()
	if remaining > 0 {
		if remaining > len(content) {
			remaining = len(content)
		}
		_, _ = buffer.Buffer.Write(content[:remaining])
	}
	if remaining < len(content) {
		buffer.exceeded = true
	}
	return written, nil
}

func install(path string, content []byte, check bool) (bool, error) {
	return installWithRename(path, content, check, os.Rename)
}

func installWithRename(path string, content []byte, check bool, rename func(string, string) error) (bool, error) {
	results, err := installArtifacts([]artifact{{path: path, content: content}}, check, rename)
	if err != nil {
		return false, err
	}
	return results[0].changed, nil
}

type artifact struct {
	path    string
	content []byte
}

type preparedArtifact struct {
	artifact
	changed   bool
	existed   bool
	temporary string
	backup    string
}

func installArtifacts(artifacts []artifact, check bool, rename func(string, string) error) ([]preparedArtifact, error) {
	return installArtifactsWithDurability(artifacts, check, true, rename)
}

func installArtifactsWithDurability(artifacts []artifact, check, durable bool, rename func(string, string) error) ([]preparedArtifact, error) {
	seenPaths := make(map[string]struct{}, len(artifacts))
	for _, item := range artifacts {
		path := filepath.Clean(item.path)
		if _, exists := seenPaths[path]; exists {
			return nil, fmt.Errorf("generated artifact path %s is configured more than once", path)
		}
		seenPaths[path] = struct{}{}
	}
	prepared := make([]preparedArtifact, len(artifacts))
	for index, item := range artifacts {
		prepared[index].artifact = item
		existing, err := os.ReadFile(item.path)
		if err == nil && bytes.Equal(existing, item.content) {
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			cleanupPrepared(prepared)
			return nil, fmt.Errorf("read generated artifact %s: %w", item.path, err)
		}
		prepared[index].changed = true
		prepared[index].existed = err == nil
		if check {
			cleanupPrepared(prepared)
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("generated artifact %s is missing; run ridu generate", item.path)
			}
			return nil, fmt.Errorf("generated artifact %s is out of date; run ridu generate", item.path)
		}

		directory := filepath.Dir(item.path)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			cleanupPrepared(prepared)
			return nil, fmt.Errorf("create generated artifact directory %s: %w", directory, err)
		}
		temporary, err := writeTemporary(directory, ".ridu-generated-*", item.content, durable)
		if err != nil {
			cleanupPrepared(prepared)
			return nil, err
		}
		prepared[index].temporary = temporary
		if prepared[index].existed {
			backup, err := writeTemporary(directory, ".ridu-backup-*", existing, durable)
			if err != nil {
				cleanupPrepared(prepared)
				return nil, err
			}
			prepared[index].backup = backup
		}
	}

	installed := make([]int, 0, len(prepared))
	for index := range prepared {
		if !prepared[index].changed {
			continue
		}
		if err := rename(prepared[index].temporary, prepared[index].path); err != nil {
			rollbackError := rollback(prepared, installed, rename)
			cleanupPrepared(prepared)
			return nil, errors.Join(fmt.Errorf("atomically install generated artifact %s: %w", prepared[index].path, err), rollbackError)
		}
		prepared[index].temporary = ""
		installed = append(installed, index)
	}
	cleanupPrepared(prepared)
	return prepared, nil
}

func writeTemporary(directory, pattern string, content []byte, durable bool) (string, error) {
	temporary, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", fmt.Errorf("create temporary generated artifact: %w", err)
	}
	path := temporary.Name()
	failed := true
	defer func() {
		if failed {
			temporary.Close()
			os.Remove(path)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return "", fmt.Errorf("set temporary artifact permissions: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		return "", fmt.Errorf("write temporary generated artifact: %w", err)
	}
	if durable {
		if err := temporary.Sync(); err != nil {
			return "", fmt.Errorf("sync temporary generated artifact: %w", err)
		}
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close temporary generated artifact: %w", err)
	}
	failed = false
	return path, nil
}

func rollback(prepared []preparedArtifact, installed []int, rename func(string, string) error) error {
	var rollbackErrors []error
	for position := len(installed) - 1; position >= 0; position-- {
		item := prepared[installed[position]]
		if item.existed {
			if err := rename(item.backup, item.path); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", item.path, err))
			}
		} else if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove new artifact %s: %w", item.path, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func cleanupPrepared(prepared []preparedArtifact) {
	for _, item := range prepared {
		if item.temporary != "" {
			_ = os.Remove(item.temporary)
		}
		if item.backup != "" {
			_ = os.Remove(item.backup)
		}
	}
}
