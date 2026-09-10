package cli_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/cli"
)

func TestFreshProjectBlocksReferenceWorkflow(t *testing.T) {
	ctx := context.Background()
	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, "blocks-reference")
	setFrameworkProxy(t, frameworkRoot)
	options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	run := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := cli.Run(ctx, args, &stdout, &stderr, options); code != 0 {
			t.Fatalf("ridu %v: %s\n%s", args, stdout.String(), stderr.String())
		}
	}
	run("new", "--database", "sqlite", "--module", "example.com/blocks/reference", target)
	copySource := func(source, destination string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(frameworkRoot, "examples", "blocks", source))
		if err != nil {
			t.Fatal(err)
		}
		value := strings.ReplaceAll(string(data), "github.com/riducms/ridu/examples/blocks/", "example.com/blocks/reference/")
		path := filepath.Join(target, destination)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
		return value
	}
	config := copySource("content/config.go", "content/config.go")
	copySource("render/blocks.go", "render/blocks.go")
	workflow := copySource("workflow_test.go", "workflow_test.go")
	// Generation does not compile render/ yet: the project entry imports only config.
	run("generate")
	run("generate", "--check")
	run("migrate", "create", "--name", "blocks-initial")
	database := filepath.Join(target, ".ridu", "blocks.sqlite")
	if err := os.MkdirAll(filepath.Dir(database), 0o755); err != nil {
		t.Fatal(err)
	}
	run("migrate", "up", "--database-path", database)
	check := func(evolved bool) {
		t.Helper()
		command := exec.Command("go", "test", ".", "-run", "TestReferenceWorkflow", "-count=1")
		command.Dir = target
		command.Env = append(os.Environ(), "RIDU_BLOCKS_REFERENCE_DB="+database)
		if evolved {
			command.Env = append(command.Env, "RIDU_BLOCKS_REFERENCE_EVOLVED=1")
		}
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("fresh blocks workflow: %v\n%s", err, output)
		}
	}
	check(false)
	config = strings.Replace(config, `field.Text("heading").Required().Localized(),`, `field.Text("heading").Required().Localized(),`+"\n\tfield.Text(\"eyebrow\"),", 1)
	if err := os.WriteFile(filepath.Join(target, "content", "config.go"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	run("generate")
	run("migrate", "create", "--name", "hero-eyebrow")
	run("migrate", "up", "--database-path", database)
	run("generate", "--check")
	// The evolved consumer uses the newly generated field without changing generated code.
	workflow = strings.Replace(workflow, `generated.HeroInput{Heading: "Hello <reader>",`, `generated.HeroInput{Heading: "Hello <reader>", Eyebrow: core.Set("New schema"),`, 1)
	if err := os.WriteFile(filepath.Join(target, "workflow_test.go"), []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	check(true)
}
