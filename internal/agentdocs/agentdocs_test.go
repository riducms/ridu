package agentdocs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInstallNewProjectWritesReleaseMatchedSkillsForAllLayouts(t *testing.T) {
	root := t.TempDir()
	result, err := InstallNewProject(root, SelectionAll, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		"AGENTS.md",
		"CLAUDE.md",
		manifestName,
		".agents/skills/ridu-project/SKILL.md",
		".agents/skills/ridu-project/reference/quickstart.md",
		".agents/skills/ridu-project/reference/fields.md",
		".agents/skills/ridu-project/reference/postgres.md",
		".agents/skills/ridu-project/reference/sqlite.md",
		".agents/skills/ridu-project/reference/bulk-and-trash.md",
		".agents/skills/ridu-project/reference/rich-text.md",
		".agents/skills/ridu-project/reference/live-preview.md",
		".agents/skills/payload-to-ridu/SKILL.md",
		".agents/skills/payload-to-ridu/references/migration-guide.md",
		".claude/skills/ridu-project/SKILL.md",
		".claude/skills/payload-to-ridu/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Errorf("installed %s: %v", relative, err)
		}
	}
	if result.FrameworkVersion != "v1.2.3" {
		t.Fatalf("framework version = %q", result.FrameworkVersion)
	}
	content, err := os.ReadFile(filepath.Join(root, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	var installed manifest
	if err := json.Unmarshal(content, &installed); err != nil {
		t.Fatal(err)
	}
	if installed.FrameworkVersion != "v1.2.3" || installed.SchemaVersion != manifestSchemaVersion {
		t.Fatalf("manifest = %#v", installed)
	}
}

func TestInstallNewProjectCanOmitAgentDocs(t *testing.T) {
	root := t.TempDir()
	if _, err := InstallNewProject(root, SelectionNone, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("none selection wrote project files: entries=%v err=%v", entries, err)
	}
}

func TestInstallPreservesExistingProjectInstructions(t *testing.T) {
	root := t.TempDir()
	const instructions = "# My project instructions\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(instructions), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Install(root, SelectionCodex, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PreservedEntrypoint) != 1 || result.PreservedEntrypoint[0] != "AGENTS.md" {
		t.Fatalf("preserved entrypoints = %v", result.PreservedEntrypoint)
	}
	content, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != instructions {
		t.Fatalf("AGENTS.md was overwritten: %q", content)
	}
}

func TestSyncRefusesModifiedManagedDocumentation(t *testing.T) {
	root := t.TempDir()
	if _, err := InstallNewProject(root, SelectionCodex, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	fieldReference := filepath.Join(root, ".agents", "skills", "ridu-project", "reference", "fields.md")
	handle, err := os.OpenFile(fieldReference, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.WriteString("\nuser note\n"); err != nil {
		handle.Close()
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(root, "v2.0.0"); err == nil || !strings.Contains(err.Error(), "was modified") {
		t.Fatalf("Sync error = %v", err)
	}
	installed, err := readManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if installed.FrameworkVersion != "v1.2.3" {
		t.Fatalf("failed sync changed manifest version to %q", installed.FrameworkVersion)
	}
}

func TestSyncRejectsManifestPathsOutsideManagedSkillRoots(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "do-not-touch.md")
	content := []byte("outside\n")
	if err := os.WriteFile(outside, content, 0o644); err != nil {
		t.Fatal(err)
	}
	malicious := manifest{
		SchemaVersion:    manifestSchemaVersion,
		FrameworkVersion: "v1.2.3",
		Selections:       []Selection{SelectionCodex},
		Files:            map[string]string{"../do-not-touch.md": digest(content)},
	}
	encoded, err := json.Marshal(malicious)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, manifestName), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(root, "v2.0.0"); err == nil || !strings.Contains(err.Error(), "unmanaged path") {
		t.Fatalf("Sync error = %v", err)
	}
	after, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(content) {
		t.Fatalf("outside file changed to %q", after)
	}
}

func TestInstalledMarkdownLinksResolveInsideProjectOrUseHTTPS(t *testing.T) {
	root := t.TempDir()
	if _, err := InstallNewProject(root, SelectionAll, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	linkPattern := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, skillRoot := range []string{".agents/skills", ".claude/skills"} {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(skillRoot)), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(content)
			if strings.Contains(text, "docs/roadmap/") || strings.Contains(text, "/Users/") {
				t.Errorf("%s contains a private checkout reference", path)
			}
			for _, match := range linkPattern.FindAllStringSubmatch(text, -1) {
				target := strings.Trim(strings.Fields(match[1])[0], "<>")
				if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
					continue
				}
				target = strings.SplitN(target, "#", 2)[0]
				resolved := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(target)))
				if relative, err := filepath.Rel(root, resolved); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
					t.Errorf("%s link %q escapes the generated project", path, match[1])
					continue
				}
				if _, err := os.Stat(resolved); err != nil {
					t.Errorf("%s link %q does not resolve: %v", path, match[1], err)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
