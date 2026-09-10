// Package scaffold renders a generated Ridu application without overwriting
// existing project files.
package scaffold

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"unicode"

	"github.com/riducms/ridu/internal/agentdocs"
	"github.com/riducms/ridu/internal/projectfile"
)

var (
	projectNamePattern          = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	projectNameSeparatorPattern = regexp.MustCompile(`[^a-z0-9]+`)
	modulePartPattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~+-]*$`)
	npmScopePattern             = regexp.MustCompile(`^@[a-z0-9][a-z0-9._-]*$`)
	versionPattern              = regexp.MustCompile(`^(?:v)?(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(?:\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`)
)

//go:embed templates/*
var templateFiles embed.FS

// Options describes one generated project.
type Options struct {
	Target string
	// InPlace explicitly permits installing into an existing directory.
	InPlace          bool
	ModulePath       string
	NPMScope         string
	FrameworkVersion string
	Template         Template
	Agent            agentdocs.Selection
	Database         projectfile.DatabaseAdapter
	PackageManager   projectfile.PackageManager
}

// PackageManagerDefinition is stable CLI-facing metadata for one supported
// frontend dependency manager.
type PackageManagerDefinition struct {
	Name        projectfile.PackageManager
	Label       string
	Description string
}

var packageManagerDefinitions = []PackageManagerDefinition{
	{Name: projectfile.PackageManagerNPM, Label: "npm", Description: "familiar Node.js default"},
	{Name: projectfile.PackageManagerBun, Label: "Bun", Description: "fast all-in-one JavaScript toolkit"},
	{Name: projectfile.PackageManagerPNPM, Label: "pnpm", Description: "strict, disk-efficient installs"},
	{Name: projectfile.PackageManagerYarn, Label: "Yarn", Description: "classic or modern Yarn workflows"},
}

// PackageManagers returns supported managers in user-facing display order.
func PackageManagers() []PackageManagerDefinition {
	return append([]PackageManagerDefinition(nil), packageManagerDefinitions...)
}

// ParsePackageManager validates a package-manager name. Empty selects npm for
// new projects; legacy projects without the ridu.toml key are handled by
// projectfile.File.FrontendPackageManager instead.
func ParsePackageManager(value string) (projectfile.PackageManager, error) {
	manager := projectfile.PackageManager(strings.ToLower(strings.TrimSpace(value)))
	if manager == "" {
		return projectfile.PackageManagerNPM, nil
	}
	if manager.Valid() {
		return manager, nil
	}
	return "", fmt.Errorf("unknown package manager %q; expected npm, bun, pnpm, or yarn", value)
}

// Template identifies one embedded project starting point. Templates share
// the same runtime and generated-contract boundaries; they differ only in the
// application-owned example content installed at time zero.
type Template string

const (
	// TemplateStarter includes authenticated Users and a small Posts example.
	TemplateStarter Template = "starter"
	// TemplateBlank includes only the auth collection required to enter admin.
	TemplateBlank Template = "blank"
)

// TemplateDefinition is stable CLI-facing metadata for an embedded template.
type TemplateDefinition struct {
	Name        Template
	Description string
}

var templateDefinitions = []TemplateDefinition{
	{Name: TemplateStarter, Description: "Users, Posts, and rich text"},
	{Name: TemplateBlank, Description: "Users auth and admin shell only"},
}

// Templates returns the embedded templates in their user-facing display order.
func Templates() []TemplateDefinition {
	return append([]TemplateDefinition(nil), templateDefinitions...)
}

// ParseTemplate validates a template name. Empty selects the backward-
// compatible starter so existing non-interactive invocations remain stable.
func ParseTemplate(value string) (Template, error) {
	selected := Template(strings.TrimSpace(value))
	if selected == "" {
		return TemplateStarter, nil
	}
	for _, definition := range templateDefinitions {
		if selected == definition.Name {
			return selected, nil
		}
	}
	return "", fmt.Errorf("unknown project template %q; expected starter or blank", value)
}

type templateData struct {
	ProjectName         string
	DisplayName         string
	ModulePath          string
	NPMScope            string
	GoVersion           string
	NPMVersion          string
	Template            Template
	Starter             bool
	Database            projectfile.DatabaseAdapter
	PackageManager      projectfile.PackageManager
	PackageManagerLabel string
	InstallCommand      string
	RunCommand          string
	AddCommand          string
	Lockfile            string
	RiduCommand         string
	AdminDevCommand     string
	Postgres            bool
	SQLite              bool
	MongoDB             bool
	PNPM                bool
	Yarn                bool
}

type templateFile struct {
	Source string
	Target string
	Mode   fs.FileMode
}

var commonFiles = []templateFile{
	{Source: "templates/go.mod.tmpl", Target: "go.mod", Mode: 0o644},
	{Source: "templates/package.json.tmpl", Target: "package.json", Mode: 0o644},
	{Source: "templates/pnpm-workspace.yaml.tmpl", Target: "pnpm-workspace.yaml", Mode: 0o644},
	{Source: "templates/yarnrc.yml.tmpl", Target: ".yarnrc.yml", Mode: 0o644},
	{Source: "templates/compose.yaml.tmpl", Target: "compose.yaml", Mode: 0o644},
	{Source: "templates/ridu.toml.tmpl", Target: "ridu.toml", Mode: 0o644},
	{Source: "templates/README.md.tmpl", Target: "README.md", Mode: 0o644},
	{Source: "templates/PROJECT.md.tmpl", Target: "PROJECT.md", Mode: 0o644},
	{Source: "templates/gitignore.tmpl", Target: ".gitignore", Mode: 0o644},
	{Source: "templates/cmd-server-main.go.tmpl", Target: "cmd/server/main.go", Mode: 0o644},
	{Source: "templates/content-config.go.tmpl", Target: "content/config.go", Mode: 0o644},
	{Source: "templates/content-plugins.go.tmpl", Target: "content/ridu_plugins.generated.go", Mode: 0o644},
	{Source: "templates/content-users.go.tmpl", Target: "content/users.go", Mode: 0o644},
	{Source: "templates/adminassets.go.tmpl", Target: "internal/adminassets/assets.go", Mode: 0o644},
	{Source: "templates/adminassets-placeholder.txt.tmpl", Target: "internal/adminassets/dist/placeholder.txt", Mode: 0o644},
	{Source: "templates/admin-package.json.tmpl", Target: "admin/package.json", Mode: 0o644},
	{Source: "templates/admin-index.html.tmpl", Target: "admin/index.html", Mode: 0o644},
	{Source: "templates/admin-favicon.svg.tmpl", Target: "admin/public/favicon.svg", Mode: 0o644},
	{Source: "templates/admin-tsconfig.json.tmpl", Target: "admin/tsconfig.json", Mode: 0o644},
	{Source: "templates/admin-vite.config.ts.tmpl", Target: "admin/vite.config.ts", Mode: 0o644},
	{Source: "templates/admin-text-editor.svelte.tmpl", Target: "admin/src/fields/text-editor.svelte", Mode: 0o644},
	{Source: "templates/admin-main.ts.tmpl", Target: "admin/src/main.ts", Mode: 0o644},
	{Source: "templates/admin-config.ts.tmpl", Target: "admin/src/admin.config.ts", Mode: 0o644},
	{Source: "templates/plugins.json.tmpl", Target: "ridu.plugins.json", Mode: 0o644},
}

var starterFiles = []templateFile{
	{Source: "templates/content-posts.go.tmpl", Target: "content/posts.go", Mode: 0o644},
}

// Create validates and renders a project before installing it. New directories
// are installed atomically; in-place creation checks all destinations first.
func Create(options Options) (string, error) {
	target, data, err := validate(options)
	if err != nil {
		return "", err
	}
	if err := validateTargetAvailability(target, options.InPlace); err != nil {
		return "", err
	}

	parent := filepath.Dir(target)
	if options.InPlace {
		parent = target
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("create target parent %s: %w", parent, err)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(target)+".ridu-new-")
	if err != nil {
		return "", fmt.Errorf("create scaffold staging directory: %w", err)
	}
	defer func() {
		if staging != "" {
			_ = os.RemoveAll(staging)
		}
	}()

	files := make([]templateFile, 0, len(commonFiles)+len(starterFiles))
	for _, file := range commonFiles {
		if data.SQLite && file.Target == "compose.yaml" {
			continue
		}
		if !data.PNPM && file.Target == "pnpm-workspace.yaml" {
			continue
		}
		if !data.Yarn && file.Target == ".yarnrc.yml" {
			continue
		}
		files = append(files, file)
	}
	if data.Starter {
		files = append(files, starterFiles...)
	}
	for _, file := range files {
		if err := render(staging, file, data); err != nil {
			return "", err
		}
	}
	if _, err := agentdocs.InstallNewProject(staging, options.Agent, data.GoVersion); err != nil {
		return "", fmt.Errorf("install agent documentation: %w", err)
	}
	if options.InPlace {
		if err := installInPlace(staging, target); err != nil {
			return "", err
		}
		return target, nil
	}
	if err := os.Rename(staging, target); err != nil {
		return "", fmt.Errorf("install generated project at %s: %w", target, err)
	}
	staging = ""
	return target, nil
}

func validate(options Options) (string, templateData, error) {
	target, projectName, err := ResolveTarget(options.Target, options.InPlace)
	if err != nil {
		return "", templateData{}, err
	}
	if err := validateModulePath(options.ModulePath); err != nil {
		return "", templateData{}, err
	}
	if !npmScopePattern.MatchString(options.NPMScope) {
		return "", templateData{}, fmt.Errorf("npm scope %q must start with @ and contain lowercase letters, numbers, dots, underscores, or hyphens", options.NPMScope)
	}
	goVersion, npmVersion, err := normalizeVersion(options.FrameworkVersion)
	if err != nil {
		return "", templateData{}, err
	}
	selectedTemplate, err := ParseTemplate(string(options.Template))
	if err != nil {
		return "", templateData{}, err
	}
	database := options.Database
	if database == "" {
		database = projectfile.DatabasePostgres
	}
	if database != projectfile.DatabasePostgres && database != projectfile.DatabaseSQLite && database != projectfile.DatabaseMongoDB {
		return "", templateData{}, fmt.Errorf("unknown database adapter %q; expected postgres, sqlite, or mongodb", options.Database)
	}
	packageManager, err := ParsePackageManager(string(options.PackageManager))
	if err != nil {
		return "", templateData{}, err
	}
	installCommand, runCommand, addCommand, lockfile, riduCommand, adminDevCommand := packageManagerCommands(packageManager, options.NPMScope)
	return target, templateData{
		ProjectName:         projectName,
		DisplayName:         humanize(projectName),
		ModulePath:          options.ModulePath,
		NPMScope:            options.NPMScope,
		GoVersion:           goVersion,
		NPMVersion:          npmVersion,
		Template:            selectedTemplate,
		Starter:             selectedTemplate == TemplateStarter,
		Database:            database,
		PackageManager:      packageManager,
		PackageManagerLabel: packageManagerDisplayLabel(packageManager),
		InstallCommand:      installCommand,
		RunCommand:          runCommand,
		AddCommand:          addCommand,
		Lockfile:            lockfile,
		RiduCommand:         riduCommand,
		AdminDevCommand:     adminDevCommand,
		Postgres:            database == projectfile.DatabasePostgres,
		SQLite:              database == projectfile.DatabaseSQLite,
		MongoDB:             database == projectfile.DatabaseMongoDB,
		PNPM:                packageManager == projectfile.PackageManagerPNPM,
		Yarn:                packageManager == projectfile.PackageManagerYarn,
	}, nil
}

func packageManagerCommands(manager projectfile.PackageManager, npmScope string) (install, run, add, lockfile, ridu, adminDev string) {
	workspace := npmScope + "/admin"
	switch manager {
	case projectfile.PackageManagerBun:
		return "bun install", "bun run", "bun add", "bun.lock", "bun run ridu --", "bun run --cwd admin dev"
	case projectfile.PackageManagerPNPM:
		return "pnpm install", "pnpm run", "pnpm add", "pnpm-lock.yaml", "pnpm run ridu", "pnpm --dir admin run dev"
	case projectfile.PackageManagerYarn:
		return "yarn install", "yarn run", "yarn add", "yarn.lock", "yarn run ridu", "yarn --cwd admin run dev"
	default:
		return "npm install", "npm run", "npm install", "package-lock.json", "npm run ridu --", "npm run dev --workspace " + workspace
	}
}

func packageManagerDisplayLabel(manager projectfile.PackageManager) string {
	for _, definition := range packageManagerDefinitions {
		if definition.Name == manager {
			return definition.Label
		}
	}
	return string(manager)
}

// ValidateTarget checks the path rules and availability used by project
// creation without writing a staging directory. Interactive clients use it to
// report mistakes before the user confirms the scaffold.
func ValidateTarget(value string, inPlace bool) error {
	target, _, err := ResolveTarget(value, inPlace)
	if err != nil {
		return err
	}
	return validateTargetAvailability(target, inPlace)
}

// ResolveTarget returns the absolute path and package-safe project name.
// Existing directory names are normalized only for explicit in-place creation.
func ResolveTarget(value string, inPlace bool) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		return "", "", fmt.Errorf("project target must not be empty")
	}
	target, err := filepath.Abs(value)
	if err != nil {
		return "", "", fmt.Errorf("resolve project target: %w", err)
	}
	projectName := filepath.Base(filepath.Clean(target))
	if inPlace {
		projectName = strings.Trim(projectNameSeparatorPattern.ReplaceAllString(strings.ToLower(projectName), "-"), "-")
		if projectName == "" {
			projectName = "ridu-project"
		} else if projectName[0] < 'a' || projectName[0] > 'z' {
			projectName = "ridu-" + projectName
		}
	}
	if !projectNamePattern.MatchString(projectName) {
		return "", "", fmt.Errorf("project name %q must be lowercase kebab-case", projectName)
	}
	return target, projectName, nil
}

func validateTargetAvailability(target string, inPlace bool) error {
	if inPlace {
		info, err := os.Stat(target)
		if err != nil {
			return fmt.Errorf("inspect current directory %s: %w", target, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("target %s must be a directory", target)
		}
		return nil
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("target %s already exists; choose a new directory", target)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect target %s: %w", target, err)
	}
	return nil
}

func normalizeVersion(version string) (string, string, error) {
	version = strings.TrimSpace(version)
	if !versionPattern.MatchString(version) {
		return "", "", fmt.Errorf("framework version %q must be a semantic release such as v1.2.3 or 1.2.3-beta.1", version)
	}
	npmVersion := strings.TrimPrefix(version, "v")
	return "v" + npmVersion, npmVersion, nil
}

func validateModulePath(modulePath string) error {
	if modulePath == "" {
		return fmt.Errorf("Go module path must not be empty")
	}
	if strings.ContainsAny(modulePath, "\\ \t\r\n") || strings.HasPrefix(modulePath, "/") || strings.HasSuffix(modulePath, "/") {
		return fmt.Errorf("Go module path %q is invalid", modulePath)
	}
	parts := strings.Split(modulePath, "/")
	if len(parts) < 2 || !strings.Contains(parts[0], ".") {
		return fmt.Errorf("Go module path %q must begin with a domain and contain a project segment", modulePath)
	}
	for _, part := range parts {
		if !modulePartPattern.MatchString(part) {
			return fmt.Errorf("Go module path %q contains invalid segment %q", modulePath, part)
		}
	}
	return nil
}

func render(staging string, file templateFile, data templateData) error {
	source, err := templateFiles.ReadFile(file.Source)
	if err != nil {
		return fmt.Errorf("read embedded scaffold template %s: %w", file.Source, err)
	}
	parsed, err := template.New(file.Source).Option("missingkey=error").Parse(string(source))
	if err != nil {
		return fmt.Errorf("parse scaffold template %s: %w", file.Source, err)
	}

	target := filepath.Join(staging, filepath.FromSlash(file.Target))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create scaffold directory for %s: %w", file.Target, err)
	}
	handle, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.Mode)
	if err != nil {
		return fmt.Errorf("create scaffold file %s: %w", file.Target, err)
	}
	if err := parsed.Execute(handle, data); err != nil {
		handle.Close()
		return fmt.Errorf("render scaffold file %s: %w", file.Target, err)
	}
	if err := handle.Close(); err != nil {
		return fmt.Errorf("close scaffold file %s: %w", file.Target, err)
	}
	return nil
}

func humanize(value string) string {
	words := strings.Split(value, "-")
	for index, word := range words {
		runes := []rune(word)
		if len(runes) != 0 {
			runes[0] = unicode.ToUpper(runes[0])
		}
		words[index] = string(runes)
	}
	return strings.Join(words, " ")
}
