// Package pluginregistry owns the explicit, versioned plugin installation
// registry used by generated Ridu applications.
package pluginregistry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const CurrentVersion = 1

var (
	keyPattern          = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	goPackagePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~+/\-]*$`)
	identifierPattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	adminPackagePattern = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
)

// Registry is the canonical input for the generated compiled-plugin registry.
type Registry struct {
	Version int     `json:"version"`
	Plugins []Entry `json:"plugins"`
}

// Entry describes dependencies and the zero-argument constructor used to
// register one compiled plugin. Versions are dependency-manager requirements,
// not the runtime descriptor version reported by the plugin itself.
type Entry struct {
	Key          string `json:"key"`
	GoPackage    string `json:"goPackage"`
	GoVersion    string `json:"goVersion"`
	Constructor  string `json:"constructor"`
	AdminPackage string `json:"adminPackage,omitempty"`
	AdminVersion string `json:"adminVersion,omitempty"`
}

// Load reads and validates a registry. A missing file is treated as an empty
// current-version registry so older hand-written projects can adopt the tool.
func Load(path string) (Registry, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Registry{Version: CurrentVersion}, nil
	}
	if err != nil {
		return Registry{}, fmt.Errorf("read plugin registry: %w", err)
	}
	var registry Registry
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return Registry{}, fmt.Errorf("parse plugin registry: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return Registry{}, fmt.Errorf("parse plugin registry: unexpected trailing JSON value")
		}
		return Registry{}, fmt.Errorf("parse plugin registry trailing data: %w", err)
	}
	if err := registry.Validate(); err != nil {
		return Registry{}, err
	}
	return registry, nil
}

// Validate rejects ambiguous dependency or constructor declarations.
func (registry Registry) Validate() error {
	if registry.Version != CurrentVersion {
		return fmt.Errorf("plugin registry version %d is incompatible with CLI version %d", registry.Version, CurrentVersion)
	}
	keys := make(map[string]struct{}, len(registry.Plugins))
	for index, plugin := range registry.Plugins {
		if !keyPattern.MatchString(plugin.Key) {
			return fmt.Errorf("plugin %d key %q must be lowercase kebab-case", index, plugin.Key)
		}
		if _, duplicate := keys[plugin.Key]; duplicate {
			return fmt.Errorf("plugin key %q is registered more than once", plugin.Key)
		}
		keys[plugin.Key] = struct{}{}
		if !goPackagePattern.MatchString(plugin.GoPackage) || !strings.Contains(plugin.GoPackage, "/") {
			return fmt.Errorf("plugin %q Go package %q is invalid", plugin.Key, plugin.GoPackage)
		}
		if strings.TrimSpace(plugin.GoVersion) == "" {
			return fmt.Errorf("plugin %q Go version is required", plugin.Key)
		}
		if !identifierPattern.MatchString(plugin.Constructor) {
			return fmt.Errorf("plugin %q constructor %q is not a Go identifier", plugin.Key, plugin.Constructor)
		}
		if plugin.AdminPackage == "" && plugin.AdminVersion != "" {
			return fmt.Errorf("plugin %q has an admin version without an admin package", plugin.Key)
		}
		if plugin.AdminPackage != "" {
			if !adminPackagePattern.MatchString(plugin.AdminPackage) {
				return fmt.Errorf("plugin %q admin package %q is invalid", plugin.Key, plugin.AdminPackage)
			}
			if strings.TrimSpace(plugin.AdminVersion) == "" {
				return fmt.Errorf("plugin %q admin version is required", plugin.Key)
			}
		}
	}
	return nil
}

// Add inserts one entry while preserving deterministic key order.
func (registry *Registry) Add(entry Entry) error {
	registry.Plugins = append(registry.Plugins, entry)
	sort.Slice(registry.Plugins, func(left, right int) bool {
		return registry.Plugins[left].Key < registry.Plugins[right].Key
	})
	return registry.Validate()
}

// Remove deletes one installed plugin and returns its dependency declaration.
func (registry *Registry) Remove(key string) (Entry, error) {
	for index, plugin := range registry.Plugins {
		if plugin.Key != key {
			continue
		}
		registry.Plugins = append(registry.Plugins[:index], registry.Plugins[index+1:]...)
		return plugin, nil
	}
	return Entry{}, fmt.Errorf("plugin %q is not installed", key)
}

// Write atomically writes the registry and its generated Go registration file.
func (registry Registry) Write(registryPath, goPath, packageName string) error {
	if err := registry.Validate(); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode plugin registry: %w", err)
	}
	contents = append(contents, '\n')
	goContents, err := registry.GoSource(packageName)
	if err != nil {
		return err
	}
	previous, previousMode, previousExists, err := readExisting(registryPath)
	if err != nil {
		return err
	}
	if err := atomicWrite(registryPath, contents, 0o644); err != nil {
		return err
	}
	if err := atomicWrite(goPath, goContents, 0o644); err != nil {
		if restoreError := restore(registryPath, previous, previousMode, previousExists); restoreError != nil {
			return fmt.Errorf("%w; rollback plugin registry: %v", err, restoreError)
		}
		return err
	}
	return nil
}

func readExisting(path string) ([]byte, os.FileMode, bool, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, fmt.Errorf("snapshot plugin registry %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("inspect plugin registry %s: %w", path, err)
	}
	return contents, info.Mode().Perm(), true, nil
}

func restore(path string, contents []byte, mode os.FileMode, existed bool) error {
	if existed {
		return atomicWrite(path, contents, mode)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// GoSource renders the only Go source file managed by plugin installation.
func (registry Registry) GoSource(packageName string) ([]byte, error) {
	if !identifierPattern.MatchString(packageName) {
		return nil, fmt.Errorf("generated plugin package name %q is invalid", packageName)
	}
	var source strings.Builder
	source.WriteString("// Code generated by ridu plugin add; DO NOT EDIT.\n\npackage ")
	source.WriteString(packageName)
	source.WriteString("\n\nimport (\n\t\"github.com/riducms/ridu\"\n")
	for index, plugin := range registry.Plugins {
		fmt.Fprintf(&source, "\tplugin%d %q\n", index, plugin.GoPackage)
	}
	source.WriteString(")\n\nfunc installedPlugins() []ridu.Plugin {\n\treturn []ridu.Plugin{\n")
	for index, plugin := range registry.Plugins {
		fmt.Fprintf(&source, "\t\tplugin%d.%s(),\n", index, plugin.Constructor)
	}
	source.WriteString("\t}\n}\n")
	formatted, err := format.Source([]byte(source.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated plugin registry: %w", err)
	}
	return formatted, nil
}

func atomicWrite(path string, contents []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create plugin registry directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".ridu-plugin-*")
	if err != nil {
		return fmt.Errorf("create temporary plugin registry: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("set temporary plugin registry mode: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary plugin registry: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary plugin registry: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary plugin registry: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install plugin registry %s: %w", path, err)
	}
	return nil
}
