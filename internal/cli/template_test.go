package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/riducms/ridu/internal/agentdocs"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/scaffold"
)

func TestSelectNewProjectAccessibleWizardSelectsBlank(t *testing.T) {
	var output bytes.Buffer
	target := filepath.Join(t.TempDir(), "blank-project")
	selectedTarget, selected, database, manager, agent, cancelled, err := selectNewProject(context.Background(), target, "", "", "", "", &output, Options{
		Stdin:       strings.NewReader("2\n2\n3\n1\ny\n"),
		Interactive: true,
		Accessible:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled || selectedTarget != target || selected != scaffold.TemplateBlank || database != projectfile.DatabaseSQLite || manager != projectfile.PackageManagerPNPM || agent != agentdocs.SelectionCodex {
		t.Fatalf("target = %q, selected = %q, database = %q, manager = %q, agent = %q, cancelled = %v", selectedTarget, selected, database, manager, agent, cancelled)
	}
	if !strings.Contains(output.String(), "starter") || !strings.Contains(output.String(), "blank") || !strings.Contains(output.String(), "Create this Ridu project?") {
		t.Fatalf("accessible wizard output = %q", output.String())
	}
	if !strings.Contains(output.String(), "Which coding agent should Ridu equip?") {
		t.Fatalf("accessible wizard omitted agent selection: %q", output.String())
	}
	if !strings.Contains(output.String(), "Select a database") || !strings.Contains(output.String(), "PostgreSQL") || !strings.Contains(output.String(), "SQLite") || !strings.Contains(output.String(), "MongoDB") {
		t.Fatalf("accessible wizard omitted database selection: %q", output.String())
	}
	if !strings.Contains(output.String(), "Select a package manager") || !strings.Contains(output.String(), "npm") || !strings.Contains(output.String(), "pnpm") || !strings.Contains(output.String(), "Yarn") {
		t.Fatalf("accessible wizard omitted package-manager selection: %q", output.String())
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("accessible wizard emitted terminal escape codes: %q", output.String())
	}
}

func TestSelectNewProjectAcceptsExplicitNoAgent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "no-agent-project")
	selectedTarget, selected, database, manager, agent, cancelled, err := selectNewProject(context.Background(), target, "starter", "sqlite", "yarn", "none", &bytes.Buffer{}, Options{Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled || selectedTarget != target || selected != scaffold.TemplateStarter || database != projectfile.DatabaseSQLite || manager != projectfile.PackageManagerYarn || agent != agentdocs.SelectionNone {
		t.Fatalf("target = %q, selected = %q, database = %q, manager = %q, agent = %q, cancelled = %v", selectedTarget, selected, database, manager, agent, cancelled)
	}
}

func TestNewRejectsUnknownDatabaseBeforeCreatingProject(t *testing.T) {
	target := filepath.Join(t.TempDir(), "unknown-database")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"new", "--database", "mysql", target}, &stdout, &stderr, Options{})
	if code != 2 || !strings.Contains(stderr.String(), "expected postgres, sqlite, or mongodb") {
		t.Fatalf("ridu new unknown database = %d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("unknown database created a project: %v", err)
	}
}

func TestNewRejectsUnknownPackageManagerBeforeCreatingProject(t *testing.T) {
	target := filepath.Join(t.TempDir(), "unknown-package-manager")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"new", "--package-manager", "deno", target}, &stdout, &stderr, Options{})
	if code != 2 || !strings.Contains(stderr.String(), "expected npm, bun, pnpm, or yarn") {
		t.Fatalf("ridu new unknown package manager = %d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("unknown package manager created a project: %v", err)
	}
}

func TestAgentCommandAddsClaudeAndSynchronizesInstalledDocs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent-command-project")
	if _, err := scaffold.Create(scaffold.Options{
		Target:           root,
		ModulePath:       "example.com/agent-command-project",
		NPMScope:         "@agent-command-project",
		FrameworkVersion: "v1.2.3",
		Agent:            agentdocs.SelectionCodex,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := Options{WorkingDirectory: root, Version: "v1.2.4"}
	if exitCode := runAgent([]string{"install", "--agent", "claude"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("agent install exit = %d; stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "payload-to-ridu", "SKILL.md")); err != nil {
		t.Fatalf("Claude migration skill: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := runAgent([]string{"sync"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("agent sync exit = %d; stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if !regexp.MustCompile(`^Synchronized [1-9][0-9]* managed agent-documentation files\.\nRidu agent documentation matches v1\.2\.4\.\n$`).MatchString(stdout.String()) {
		t.Fatalf("agent sync should be concise: %q", stdout.String())
	}
}

func TestSelectNewProjectAccessibleWizardCollectsTarget(t *testing.T) {
	var output bytes.Buffer
	target := filepath.Join(t.TempDir(), "new-ridu-project")
	selectedTarget, selected, database, manager, agent, cancelled, err := selectNewProject(context.Background(), "", "", "", "", "", &output, Options{
		Stdin:       strings.NewReader(target + "\n1\n3\n2\n1\ny\n"),
		Interactive: true,
		Accessible:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled || selectedTarget != target || selected != scaffold.TemplateStarter || database != projectfile.DatabaseMongoDB || manager != projectfile.PackageManagerBun || agent != agentdocs.SelectionCodex {
		t.Fatalf("target = %q, selected = %q, database = %q, manager = %q, agent = %q, cancelled = %v", selectedTarget, selected, database, manager, agent, cancelled)
	}
}

func TestSelectNewProjectAccessibleWizardCanCancelAtReview(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cancelled-project")
	_, _, _, _, _, cancelled, err := selectNewProject(context.Background(), target, "", "", "", "", &bytes.Buffer{}, Options{
		Stdin:       strings.NewReader("2\n1\n1\n1\nn\n"),
		Interactive: true,
		Accessible:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("wizard did not cancel after negative confirmation")
	}
}

func TestSelectNewProjectDefaultsWithoutATerminal(t *testing.T) {
	target := filepath.Join(t.TempDir(), "non-interactive-project")
	selectedTarget, selected, database, manager, agent, cancelled, err := selectNewProject(context.Background(), target, "", "", "", "", &bytes.Buffer{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled || selectedTarget != target || selected != scaffold.TemplateStarter || database != projectfile.DatabasePostgres || manager != projectfile.PackageManagerNPM || agent != agentdocs.SelectionCodex {
		t.Fatalf("target = %q, selected = %q, database = %q, manager = %q, agent = %q, cancelled = %v", selectedTarget, selected, database, manager, agent, cancelled)
	}
}

func TestNewProjectLayoutRevealsOnlyTheCurrentAndCompletedSteps(t *testing.T) {
	target := "progressive-project"
	selected := scaffold.TemplateStarter
	confirmed := true
	targetField := huh.NewInput().Title("project-location-step").Value(&target)
	templateField := huh.NewSelect[scaffold.Template]().
		Title("project-template-step").
		Options(huh.NewOption("starter", scaffold.TemplateStarter)).
		Value(&selected)
	confirmField := huh.NewConfirm().Title("project-confirm-step").Value(&confirmed)
	fields := []huh.Field{targetField, templateField, confirmField}
	steps := make([]newProjectStep, 0, len(fields))
	groups := make([]*huh.Group, 0, len(fields))
	for index, field := range fields {
		group := huh.NewGroup(field)
		stepIndex := index
		steps = append(steps, newProjectStep{
			field: field,
			group: group,
			summary: func() string {
				return "completed-step-" + string(rune('1'+stepIndex))
			},
		})
		groups = append(groups, group)
	}
	layout := newProjectLayout{steps: steps}
	form := huh.NewForm(groups...).WithLayout(layout).WithWidth(88)
	form.Init()

	first := layout.View(form)
	if !strings.Contains(first, "project-location-step") || strings.Contains(first, "project-template-step") || strings.Contains(first, "project-confirm-step") {
		t.Fatalf("first progressive frame = %q", first)
	}
	form.NextGroup()
	second := layout.View(form)
	if !strings.Contains(second, "completed-step-1") || strings.Contains(second, "project-location-step") || !strings.Contains(second, "project-template-step") || strings.Contains(second, "project-confirm-step") {
		t.Fatalf("second progressive frame = %q", second)
	}
	form.NextGroup()
	third := layout.View(form)
	if !strings.Contains(third, "completed-step-1") || !strings.Contains(third, "completed-step-2") || strings.Contains(third, "project-location-step") || strings.Contains(third, "project-template-step") || !strings.Contains(third, "project-confirm-step") {
		t.Fatalf("third progressive frame = %q", third)
	}
}
