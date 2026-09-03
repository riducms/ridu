// Package agentdocs installs the release-owned Ridu skill bundle into generated
// and existing projects without overwriting user-authored instructions.
package agentdocs

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	manifestName          = ".ridu-agent-docs.json"
	manifestSchemaVersion = 1
)

//go:embed skills
var bundle embed.FS

// Selection identifies the coding-agent layout installed into a project.
type Selection string

const (
	SelectionCodex  Selection = "codex"
	SelectionClaude Selection = "claude"
	SelectionCursor Selection = "cursor"
	SelectionAll    Selection = "all"
	SelectionNone   Selection = "none"
)

// Definition is stable CLI-facing metadata for one agent selection.
type Definition struct {
	Selection   Selection
	Description string
}

var definitions = []Definition{
	{Selection: SelectionCodex, Description: "AGENTS.md and .agents/skills"},
	{Selection: SelectionClaude, Description: "CLAUDE.md and .claude/skills"},
	{Selection: SelectionCursor, Description: "AGENTS.md and .agents/skills"},
	{Selection: SelectionAll, Description: "Codex, Claude Code, and Cursor"},
	{Selection: SelectionNone, Description: "Do not install coding-agent guidance"},
}

// Definitions returns selections in their user-facing display order.
func Definitions() []Definition {
	return append([]Definition(nil), definitions...)
}

// ParseSelection validates a CLI-facing selection. Empty selects the default
// Codex project layout.
func ParseSelection(value string) (Selection, error) {
	selection := Selection(strings.TrimSpace(value))
	if selection == "" {
		return SelectionCodex, nil
	}
	for _, definition := range definitions {
		if definition.Selection == selection {
			return selection, nil
		}
	}
	return "", fmt.Errorf("unknown coding agent %q; expected codex, claude, cursor, all, or none", value)
}

type layout struct {
	Name       string
	SkillRoot  string
	ConfigFile string
}

var agentLayouts = map[Selection]layout{
	SelectionCodex:  {Name: "Codex", SkillRoot: ".agents/skills", ConfigFile: "AGENTS.md"},
	SelectionClaude: {Name: "Claude Code", SkillRoot: ".claude/skills", ConfigFile: "CLAUDE.md"},
	SelectionCursor: {Name: "Cursor", SkillRoot: ".agents/skills", ConfigFile: "AGENTS.md"},
}

func layoutsFor(selection Selection) ([]layout, error) {
	selection, err := ParseSelection(string(selection))
	if err != nil {
		return nil, err
	}
	if selection == SelectionNone {
		return nil, nil
	}
	requested := []Selection{selection}
	if selection == SelectionAll {
		requested = []Selection{SelectionCodex, SelectionClaude, SelectionCursor}
	}
	seen := map[string]bool{}
	layouts := make([]layout, 0, len(requested))
	for _, agent := range requested {
		candidate := agentLayouts[agent]
		key := candidate.SkillRoot + "\x00" + candidate.ConfigFile
		if seen[key] {
			continue
		}
		seen[key] = true
		layouts = append(layouts, candidate)
	}
	return layouts, nil
}

type manifest struct {
	SchemaVersion    int               `json:"schemaVersion"`
	FrameworkVersion string            `json:"frameworkVersion"`
	Selections       []Selection       `json:"selections"`
	Files            map[string]string `json:"files"`
}

// Result describes an installation or synchronization without exposing
// internal ownership metadata.
type Result struct {
	Written             []string
	PreservedEntrypoint []string
	FrameworkVersion    string
	ManagedFiles        int
}

// InstallNewProject installs agent docs into a new scaffold staging directory.
// The caller guarantees that the directory is not user-owned yet.
func InstallNewProject(root string, selection Selection, frameworkVersion string) (Result, error) {
	layouts, err := layoutsFor(selection)
	if err != nil {
		return Result{}, err
	}
	if len(layouts) == 0 {
		return Result{FrameworkVersion: frameworkVersion}, nil
	}
	desired, err := desiredFiles(layouts)
	if err != nil {
		return Result{}, err
	}
	result := Result{FrameworkVersion: frameworkVersion, ManagedFiles: len(desired)}
	for path, content := range desired {
		if err := writeNewFile(root, path, content); err != nil {
			return Result{}, err
		}
		result.Written = append(result.Written, path)
	}
	for _, item := range layouts {
		if err := writeNewFile(root, item.ConfigFile, entrypoint(item)); err != nil {
			return Result{}, err
		}
		result.Written = append(result.Written, item.ConfigFile)
	}
	if err := writeManifest(root, manifestFor(selection, frameworkVersion, desired)); err != nil {
		return Result{}, err
	}
	result.Written = append(result.Written, manifestName)
	sort.Strings(result.Written)
	return result, nil
}

// Install adds an agent layout to an existing project. Existing root
// instructions are preserved, and unmanaged skill files are never replaced.
func Install(root string, selection Selection, frameworkVersion string) (Result, error) {
	layouts, err := layoutsFor(selection)
	if err != nil {
		return Result{}, err
	}
	if len(layouts) == 0 {
		return Result{}, fmt.Errorf("agent install requires codex, claude, cursor, or all")
	}
	current, err := readManifest(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Result{}, err
	}
	if err == nil && current.SchemaVersion != manifestSchemaVersion {
		return Result{}, fmt.Errorf("unsupported agent documentation manifest schema %d", current.SchemaVersion)
	}
	if current.Files == nil {
		current = manifest{SchemaVersion: manifestSchemaVersion, Files: map[string]string{}}
	}
	for path := range current.Files {
		if _, err := managedPath(root, path); err != nil {
			return Result{}, err
		}
	}
	desired, err := desiredFiles(layouts)
	if err != nil {
		return Result{}, err
	}
	for path := range desired {
		absolute, err := managedPath(root, path)
		if err != nil {
			return Result{}, err
		}
		if _, tracked := current.Files[path]; tracked {
			continue
		}
		if _, err := os.Lstat(absolute); err == nil {
			return Result{}, fmt.Errorf("refuse to replace unmanaged agent documentation %s", path)
		} else if !os.IsNotExist(err) {
			return Result{}, fmt.Errorf("inspect agent documentation %s: %w", path, err)
		}
	}
	result := Result{FrameworkVersion: frameworkVersion}
	created := []string{}
	committed := false
	defer func() {
		if !committed {
			removeCreatedFiles(root, created)
		}
	}()
	for path, content := range desired {
		if _, tracked := current.Files[path]; tracked {
			continue
		}
		if err := writeNewFile(root, path, content); err != nil {
			return Result{}, err
		}
		current.Files[path] = digest(content)
		created = append(created, path)
		result.Written = append(result.Written, path)
	}
	for _, item := range layouts {
		absolute := filepath.Join(root, item.ConfigFile)
		if _, err := os.Lstat(absolute); err == nil {
			result.PreservedEntrypoint = append(result.PreservedEntrypoint, item.ConfigFile)
			continue
		} else if !os.IsNotExist(err) {
			return Result{}, fmt.Errorf("inspect %s: %w", item.ConfigFile, err)
		}
		if err := writeNewFile(root, item.ConfigFile, entrypoint(item)); err != nil {
			return Result{}, err
		}
		created = append(created, item.ConfigFile)
		result.Written = append(result.Written, item.ConfigFile)
	}
	current.SchemaVersion = manifestSchemaVersion
	current.FrameworkVersion = frameworkVersion
	current.Selections = mergeSelections(current.Selections, selection)
	result.ManagedFiles = len(current.Files)
	if err := writeManifest(root, current); err != nil {
		return Result{}, err
	}
	committed = true
	result.Written = append(result.Written, manifestName)
	sort.Strings(result.Written)
	sort.Strings(result.PreservedEntrypoint)
	return result, nil
}

// Sync updates every framework-owned skill file recorded by the project. It
// refuses the complete operation if any managed file was edited by the user.
func Sync(root, frameworkVersion string) (Result, error) {
	manifestContent, err := os.ReadFile(filepath.Join(root, manifestName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{}, fmt.Errorf("%s is missing; run ridu agent install first", manifestName)
		}
		return Result{}, err
	}
	current, err := readManifest(root)
	if err != nil {
		return Result{}, err
	}
	if current.SchemaVersion != manifestSchemaVersion {
		return Result{}, fmt.Errorf("unsupported agent documentation manifest schema %d", current.SchemaVersion)
	}
	original := make(map[string][]byte, len(current.Files))
	for path, expected := range current.Files {
		absolute, err := managedPath(root, path)
		if err != nil {
			return Result{}, err
		}
		if info, err := os.Lstat(absolute); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return Result{}, fmt.Errorf("managed agent documentation %s must not be a symbolic link", path)
		}
		content, err := os.ReadFile(absolute)
		if err != nil {
			return Result{}, fmt.Errorf("read managed agent documentation %s: %w", path, err)
		}
		if digest(content) != expected {
			return Result{}, fmt.Errorf("managed agent documentation %s was modified; preserve the edit elsewhere before syncing", path)
		}
		original[path] = content
	}
	layouts, err := layoutsForSelections(current.Selections)
	if err != nil {
		return Result{}, err
	}
	desired, err := desiredFiles(layouts)
	if err != nil {
		return Result{}, err
	}
	for path := range desired {
		if _, tracked := current.Files[path]; tracked {
			continue
		}
		absolute, err := managedPath(root, path)
		if err != nil {
			return Result{}, err
		}
		if _, err := os.Lstat(absolute); err == nil {
			return Result{}, fmt.Errorf("refuse to replace unmanaged agent documentation %s", path)
		} else if !os.IsNotExist(err) {
			return Result{}, fmt.Errorf("inspect agent documentation %s: %w", path, err)
		}
	}
	result := Result{FrameworkVersion: frameworkVersion, ManagedFiles: len(desired)}
	newPaths := []string{}
	fail := func(cause error) (Result, error) {
		rollbackErr := rollbackSync(root, original, newPaths, manifestContent)
		if rollbackErr != nil {
			cause = errors.Join(cause, fmt.Errorf("roll back agent documentation: %w", rollbackErr))
		}
		return Result{}, cause
	}
	for path, content := range desired {
		if previous, existed := original[path]; existed && bytes.Equal(previous, content) {
			continue
		}
		absolute, err := managedPath(root, path)
		if err != nil {
			return fail(err)
		}
		if _, existed := current.Files[path]; !existed {
			newPaths = append(newPaths, path)
		}
		if err := writeAtomicFile(absolute, content, 0o644); err != nil {
			return fail(err)
		}
		result.Written = append(result.Written, path)
	}
	for path := range current.Files {
		if _, retained := desired[path]; retained {
			continue
		}
		absolute, err := managedPath(root, path)
		if err != nil {
			return fail(err)
		}
		if err := os.Remove(absolute); err != nil && !os.IsNotExist(err) {
			return fail(fmt.Errorf("remove stale agent documentation %s: %w", path, err))
		}
	}
	current.FrameworkVersion = frameworkVersion
	current.Files = fileDigests(desired)
	if err := writeManifest(root, current); err != nil {
		return fail(err)
	}
	result.Written = append(result.Written, manifestName)
	sort.Strings(result.Written)
	return result, nil
}

func layoutsForSelections(selections []Selection) ([]layout, error) {
	seen := map[string]bool{}
	var combined []layout
	for _, selection := range selections {
		layouts, err := layoutsFor(selection)
		if err != nil {
			return nil, err
		}
		for _, candidate := range layouts {
			key := candidate.SkillRoot + "\x00" + candidate.ConfigFile
			if !seen[key] {
				seen[key] = true
				combined = append(combined, candidate)
			}
		}
	}
	return combined, nil
}

func desiredFiles(layouts []layout) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(bundle, "skills", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := bundle.ReadFile(path)
		if err != nil {
			return err
		}
		relative := strings.TrimPrefix(path, "skills/")
		for _, item := range layouts {
			target := filepath.ToSlash(filepath.Join(item.SkillRoot, filepath.FromSlash(relative)))
			files[target] = content
		}
		return nil
	})
	return files, err
}

func entrypoint(item layout) []byte {
	return []byte(fmt.Sprintf(`# Ridu project guide

This project includes release-matched Ridu guidance for %s under %s.

- Use the ridu-project skill for ordinary application work.
- Use the payload-to-ridu skill for Payload assessment or migration.
- Start with PROJECT.md for this project's file ownership and lifecycle commands.

Do not hand-edit generated contracts. The root .ridu-agent-docs.json records the installed
documentation version; run ridu agent sync after upgrading the Ridu CLI.
`, item.Name, item.SkillRoot))
}

func manifestFor(selection Selection, frameworkVersion string, files map[string][]byte) manifest {
	return manifest{
		SchemaVersion:    manifestSchemaVersion,
		FrameworkVersion: frameworkVersion,
		Selections:       []Selection{selection},
		Files:            fileDigests(files),
	}
}

func mergeSelections(existing []Selection, added Selection) []Selection {
	seen := map[Selection]bool{}
	merged := make([]Selection, 0, len(existing)+1)
	for _, selection := range append(existing, added) {
		if selection == SelectionNone || seen[selection] {
			continue
		}
		seen[selection] = true
		merged = append(merged, selection)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i] < merged[j] })
	return merged
}

func fileDigests(files map[string][]byte) map[string]string {
	digests := make(map[string]string, len(files))
	for path, content := range files {
		digests[path] = digest(content)
	}
	return digests
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func readManifest(root string) (manifest, error) {
	content, err := os.ReadFile(filepath.Join(root, manifestName))
	if err != nil {
		return manifest{}, err
	}
	var decoded manifest
	if err := json.Unmarshal(content, &decoded); err != nil {
		return manifest{}, fmt.Errorf("decode %s: %w", manifestName, err)
	}
	return decoded, nil
}

func writeManifest(root string, value manifest) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return writeAtomicFile(filepath.Join(root, manifestName), encoded, 0o644)
}

func writeNewFile(root, relative string, content []byte) error {
	target, err := projectPath(root, relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create agent documentation directory for %s: %w", relative, err)
	}
	handle, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create agent documentation %s: %w", relative, err)
	}
	if _, err := handle.Write(content); err != nil {
		handle.Close()
		return fmt.Errorf("write agent documentation %s: %w", relative, err)
	}
	if err := handle.Close(); err != nil {
		return fmt.Errorf("close agent documentation %s: %w", relative, err)
	}
	return nil
}

func projectPath(root, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid project-relative path %q", relative)
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(absoluteRoot, clean)
	relativeTarget, err := filepath.Rel(absoluteRoot, target)
	if err != nil || relativeTarget == ".." || strings.HasPrefix(relativeTarget, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("project-relative path %q escapes the project", relative)
	}
	return target, nil
}

func managedPath(root, relative string) (string, error) {
	normalized := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if !strings.HasPrefix(normalized, ".agents/skills/") && !strings.HasPrefix(normalized, ".claude/skills/") {
		return "", fmt.Errorf("agent documentation manifest contains unmanaged path %q", relative)
	}
	target, err := projectPath(root, relative)
	if err != nil {
		return "", err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(target)
	relativeParent, err := filepath.Rel(absoluteRoot, parent)
	if err != nil {
		return "", err
	}
	cursor := absoluteRoot
	for _, component := range strings.Split(relativeParent, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		cursor = filepath.Join(cursor, component)
		info, err := os.Lstat(cursor)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("inspect agent documentation directory %s: %w", cursor, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("agent documentation parent %s must be a directory, not a symbolic link or file", cursor)
		}
	}
	return target, nil
}

func removeCreatedFiles(root string, paths []string) {
	for index := len(paths) - 1; index >= 0; index-- {
		if target, err := projectPath(root, paths[index]); err == nil {
			_ = os.Remove(target)
		}
	}
}

func rollbackSync(root string, original map[string][]byte, newPaths []string, manifestContent []byte) error {
	var rollbackErrors []error
	for path, content := range original {
		target, err := managedPath(root, path)
		if err == nil {
			err = writeAtomicFile(target, content, 0o644)
		}
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", path, err))
		}
	}
	removeCreatedFiles(root, newPaths)
	if err := writeAtomicFile(filepath.Join(root, manifestName), manifestContent, 0o644); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	return errors.Join(rollbackErrors...)
}

func writeAtomicFile(target string, content []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".ridu-agent-docs-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, target)
}
