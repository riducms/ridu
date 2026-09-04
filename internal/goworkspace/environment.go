// Package goworkspace keeps project Go commands from being captured by an
// unrelated enclosing workspace.
package goworkspace

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// IsolateUnlistedModule returns environment with GOWORK disabled when the
// active workspace does not list the module containing directory. Listed
// modules retain the caller's workspace so deliberate local replacements keep
// working.
func IsolateUnlistedModule(directory string, environment []string) []string {
	workFile := activeWorkFile(directory, environment)
	if workFile == "" {
		return environment
	}
	moduleFile := findUp(directory, "go.mod")
	if moduleFile == "" {
		return environment
	}
	content, err := os.ReadFile(workFile)
	if err != nil {
		return environment
	}
	workspace, err := modfile.ParseWork(workFile, content, nil)
	if err != nil {
		return environment
	}
	moduleRoot := canonicalDirectory(filepath.Dir(moduleFile))
	workRoot := filepath.Dir(workFile)
	for _, use := range workspace.Use {
		usedModule := use.Path
		if !filepath.IsAbs(usedModule) {
			usedModule = filepath.Join(workRoot, usedModule)
		}
		if canonicalDirectory(usedModule) == moduleRoot {
			return environment
		}
	}
	return replace(environment, "GOWORK", "off")
}

func activeWorkFile(directory string, environment []string) string {
	if configured, exists := environmentValue(environment, "GOWORK"); exists {
		switch configured {
		case "", "auto":
		case "off":
			return ""
		default:
			if filepath.IsAbs(configured) {
				return filepath.Clean(configured)
			}
			return ""
		}
	}
	return findUp(directory, "go.work")
}

func findUp(directory, name string) string {
	current, err := filepath.Abs(directory)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(current, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func canonicalDirectory(directory string) string {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return filepath.Clean(directory)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return filepath.Clean(absolute)
	}
	return filepath.Clean(canonical)
}

func environmentValue(environment []string, key string) (string, bool) {
	for index := len(environment) - 1; index >= 0; index-- {
		name, value, found := strings.Cut(environment[index], "=")
		if found && strings.EqualFold(name, key) {
			return value, true
		}
	}
	return "", false
}

func replace(environment []string, key, value string) []string {
	updated := make([]string, 0, len(environment)+1)
	for _, variable := range environment {
		name, _, found := strings.Cut(variable, "=")
		if found && strings.EqualFold(name, key) {
			continue
		}
		updated = append(updated, variable)
	}
	return append(updated, key+"="+value)
}
