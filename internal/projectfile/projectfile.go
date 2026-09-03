// Package projectfile discovers and validates the structural ridu.toml file
// owned by a generated application.
package projectfile

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const CurrentVersion = 1

// DatabaseAdapter is the closed set of official stores selectable by an
// ordinary project. Connection details remain runtime-owned and are not
// serialized into ridu.toml.
type DatabaseAdapter string

const (
	DatabasePostgres DatabaseAdapter = "postgres"
	DatabaseSQLite   DatabaseAdapter = "sqlite"
	DatabaseMongoDB  DatabaseAdapter = "mongodb"
)

// PackageManager is the frontend dependency manager selected by a project.
// Existing version-1 project files that omit the key retain Bun for backwards
// compatibility; newly scaffolded projects always write the selection.
type PackageManager string

const (
	PackageManagerBun  PackageManager = "bun"
	PackageManagerNPM  PackageManager = "npm"
	PackageManagerPNPM PackageManager = "pnpm"
	PackageManagerYarn PackageManager = "yarn"
)

// File is a validated project definition rooted at the directory containing
// ridu.toml. Structural paths are always relative and cannot escape Root.
type File struct {
	Path               string
	Root               string
	Version            int
	Database           DatabaseAdapter
	PackageManager     PackageManager
	Entry              string
	Admin              string
	Client             string
	Schema             string
	OpenAPI            string
	Migrations         string
	Assets             string
	Plugins            string
	PluginGo           string
	GeneratedArtifacts []GeneratedArtifact
}

// GeneratedArtifact maps one compiled plugin artifact to a project-relative
// destination. Plugin and Name form the protocol identity.
type GeneratedArtifact struct {
	Plugin string
	Name   string
	Path   string
}

// Discover walks from start toward the filesystem root until it finds ridu.toml.
func Discover(start string) (File, error) {
	absolute, err := filepath.Abs(start)
	if err != nil {
		return File{}, fmt.Errorf("resolve project search path: %w", err)
	}
	if info, statError := os.Stat(absolute); statError == nil && !info.IsDir() {
		absolute = filepath.Dir(absolute)
	}

	for {
		candidate := filepath.Join(absolute, "ridu.toml")
		if _, err := os.Stat(candidate); err == nil {
			return Load(candidate)
		} else if !os.IsNotExist(err) {
			return File{}, fmt.Errorf("inspect %s: %w", candidate, err)
		}

		parent := filepath.Dir(absolute)
		if parent == absolute {
			return File{}, fmt.Errorf("no ridu.toml found from %s or its parents", start)
		}
		absolute = parent
	}
}

// Load parses and validates one versioned project file.
func Load(path string) (File, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return File{}, fmt.Errorf("resolve project file path: %w", err)
	}
	handle, err := os.Open(absolute)
	if err != nil {
		return File{}, fmt.Errorf("open project file %s: %w", absolute, err)
	}
	defer handle.Close()

	project := File{Path: absolute, Root: filepath.Dir(absolute)}
	seen := make(map[string]int)
	scanner := bufio.NewScanner(handle)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rawValue, found := strings.Cut(line, "=")
		if !found {
			return File{}, fmt.Errorf("%s:%d: expected key = value", absolute, lineNumber)
		}
		key = strings.TrimSpace(key)
		rawValue = strings.TrimSpace(rawValue)
		if previousLine, exists := seen[key]; exists {
			return File{}, fmt.Errorf("%s:%d: duplicate key %q (first declared on line %d)", absolute, lineNumber, key, previousLine)
		}
		seen[key] = lineNumber

		switch key {
		case "version":
			project.Version, err = strconv.Atoi(rawValue)
			if err != nil {
				return File{}, fmt.Errorf("%s:%d: version must be an integer", absolute, lineNumber)
			}
		case "database":
			var value string
			value, err = parseString(rawValue)
			project.Database = DatabaseAdapter(value)
		case "package_manager":
			var value string
			value, err = parseString(rawValue)
			project.PackageManager = PackageManager(value)
		case "entry":
			project.Entry, err = parseString(rawValue)
		case "admin":
			project.Admin, err = parseString(rawValue)
		case "client":
			project.Client, err = parseString(rawValue)
		case "schema":
			project.Schema, err = parseString(rawValue)
		case "openapi":
			project.OpenAPI, err = parseString(rawValue)
		case "migrations":
			project.Migrations, err = parseString(rawValue)
		case "assets":
			project.Assets, err = parseString(rawValue)
		case "plugins":
			project.Plugins, err = parseString(rawValue)
		case "plugin_go":
			project.PluginGo, err = parseString(rawValue)
		default:
			if strings.HasPrefix(key, "generated.") {
				identity, identityError := parseGeneratedArtifactKey(key)
				if identityError != nil {
					return File{}, fmt.Errorf("%s:%d: %w", absolute, lineNumber, identityError)
				}
				identity.Path, err = parseString(rawValue)
				project.GeneratedArtifacts = append(project.GeneratedArtifacts, identity)
			} else {
				return File{}, fmt.Errorf("%s:%d: unknown project key %q for ridu.toml version %d", absolute, lineNumber, key, CurrentVersion)
			}
		}
		if err != nil {
			return File{}, fmt.Errorf("%s:%d: %s: %w", absolute, lineNumber, key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return File{}, fmt.Errorf("read project file %s: %w", absolute, err)
	}

	if project.Version != CurrentVersion {
		return File{}, fmt.Errorf("%s: project file version %d is incompatible with CLI version %d; regenerate or upgrade the project", absolute, project.Version, CurrentVersion)
	}
	if project.Database == "" {
		return File{}, fmt.Errorf("%s: database is required", absolute)
	}
	if project.Database != DatabasePostgres && project.Database != DatabaseSQLite && project.Database != DatabaseMongoDB {
		return File{}, fmt.Errorf("%s: database: adapter %q is unsupported; expected %q, %q, or %q", absolute, project.Database, DatabasePostgres, DatabaseSQLite, DatabaseMongoDB)
	}
	if project.PackageManager != "" && !project.PackageManager.Valid() {
		return File{}, fmt.Errorf("%s: package_manager: manager %q is unsupported; expected %q, %q, %q, or %q", absolute, project.PackageManager, PackageManagerNPM, PackageManagerBun, PackageManagerPNPM, PackageManagerYarn)
	}
	for _, required := range []struct {
		key   string
		value string
	}{{"entry", project.Entry}, {"schema", project.Schema}, {"plugins", project.Plugins}, {"plugin_go", project.PluginGo}} {
		if required.value == "" {
			return File{}, fmt.Errorf("%s: required key %q is missing", absolute, required.key)
		}
	}
	paths := []struct {
		key   string
		value *string
	}{
		{"entry", &project.Entry},
		{"admin", &project.Admin},
		{"client", &project.Client},
		{"schema", &project.Schema},
		{"openapi", &project.OpenAPI},
		{"migrations", &project.Migrations},
		{"assets", &project.Assets},
		{"plugins", &project.Plugins},
		{"plugin_go", &project.PluginGo},
	}
	for _, path := range paths {
		if *path.value == "" {
			continue
		}
		cleaned, err := cleanRelativePath(*path.value, path.key == "entry")
		if err != nil {
			return File{}, fmt.Errorf("%s: %s: %w", absolute, path.key, err)
		}
		*path.value = cleaned
	}
	for index := range project.GeneratedArtifacts {
		cleaned, err := cleanRelativePath(project.GeneratedArtifacts[index].Path, false)
		if err != nil {
			return File{}, fmt.Errorf("%s: generated.%s.%s: %w", absolute, project.GeneratedArtifacts[index].Plugin, project.GeneratedArtifacts[index].Name, err)
		}
		project.GeneratedArtifacts[index].Path = cleaned
	}
	if project.Client != "" && !strings.EqualFold(filepath.Ext(project.Client), ".ts") {
		return File{}, fmt.Errorf("%s: client: path %q must name a TypeScript file", absolute, project.Client)
	}
	filePaths := []struct {
		key   string
		value string
	}{
		{"schema", project.Schema},
		{"client", project.Client},
		{"openapi", project.OpenAPI},
		{"plugins", project.Plugins},
		{"plugin_go", project.PluginGo},
	}
	seenFiles := make(map[string]string, len(filePaths))
	for _, candidate := range filePaths {
		if candidate.value == "" {
			continue
		}
		identity := strings.ToLower(candidate.value)
		if previous, exists := seenFiles[identity]; exists {
			return File{}, fmt.Errorf("%s: %s: path %q is already used by %s", absolute, candidate.key, candidate.value, previous)
		}
		seenFiles[identity] = candidate.key
	}
	for _, generated := range project.GeneratedArtifacts {
		key := "generated." + generated.Plugin + "." + generated.Name
		identity := strings.ToLower(generated.Path)
		if previous, exists := seenFiles[identity]; exists {
			return File{}, fmt.Errorf("%s: %s: path %q is already used by %s", absolute, key, generated.Path, previous)
		}
		seenFiles[identity] = key
	}
	return project, nil
}

// Valid reports whether manager is one of the supported frontend package
// managers.
func (manager PackageManager) Valid() bool {
	switch manager {
	case PackageManagerNPM, PackageManagerBun, PackageManagerPNPM, PackageManagerYarn:
		return true
	default:
		return false
	}
}

// FrontendPackageManager returns the configured manager. Bun is the fallback
// only for projects created before package_manager was added to version 1.
func (project File) FrontendPackageManager() PackageManager {
	if project.PackageManager.Valid() {
		return project.PackageManager
	}
	return PackageManagerBun
}

func parseGeneratedArtifactKey(key string) (GeneratedArtifact, error) {
	parts := strings.Split(key, ".")
	if len(parts) != 3 || parts[0] != "generated" || !isArtifactIdentifier(parts[1]) || !isArtifactIdentifier(parts[2]) {
		return GeneratedArtifact{}, fmt.Errorf("generated artifact key %q must use generated.<plugin>.<artifact> with lowercase kebab-case identifiers", key)
	}
	return GeneratedArtifact{Plugin: parts[1], Name: parts[2]}, nil
}

func isArtifactIdentifier(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	previousHyphen := false
	for _, current := range value[1:] {
		if (current < 'a' || current > 'z') && (current < '0' || current > '9') && current != '-' {
			return false
		}
		if current == '-' && previousHyphen {
			return false
		}
		previousHyphen = current == '-'
	}
	return !previousHyphen
}

// Absolute resolves a validated project-relative path against Root.
func (project File) Absolute(relative string) string {
	return filepath.Join(project.Root, filepath.FromSlash(relative))
}

func parseString(raw string) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", fmt.Errorf("value must be a double-quoted TOML string without inline comments")
	}
	value, err := strconv.Unquote(raw)
	if err != nil {
		return "", fmt.Errorf("invalid quoted string: %w", err)
	}
	return value, nil
}

func cleanRelativePath(value string, allowRoot bool) (string, error) {
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("path %q must be relative to the project root", value)
	}
	if strings.Contains(value, `\`) {
		return "", fmt.Errorf("path %q must use forward slashes", value)
	}
	cleaned := filepath.Clean(filepath.FromSlash(value))
	if cleaned == "." && allowRoot {
		return ".", nil
	}
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q must name a location inside the project root", value)
	}
	return filepath.ToSlash(cleaned), nil
}
