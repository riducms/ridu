package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/internal/generate"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/schema"
)

func TestBuildDevelopmentBinaryCreatesOneDisposableExecutable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/ridu-development-binary-test\n\ngo 1.25.13\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nimport (\"fmt\"; \"os\")\n\nfunc main() { if len(os.Args) > 1 { panic(\"development panic\") }; fmt.Print(\"development-binary\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	binary, duration, err := buildDevelopmentBinary(
		context.Background(),
		projectfile.File{Root: root, Entry: "."},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("build development binary: %v\n%s", err, stderr.String())
	}
	t.Cleanup(func() { _ = binary.remove() })
	if duration <= 0 {
		t.Fatalf("build duration = %v", duration)
	}
	wantDirectory := filepath.Join(root, filepath.FromSlash(developmentBinaryDirectory)) + string(filepath.Separator)
	if !strings.HasPrefix(binary.path, wantDirectory) {
		t.Fatalf("development binary path = %q, want prefix %q", binary.path, wantDirectory)
	}
	output, err := exec.Command(binary.path).CombinedOutput()
	if err != nil {
		t.Fatalf("run development binary: %v\n%s", err, output)
	}
	if string(output) != "development-binary" {
		t.Fatalf("development binary output = %q", output)
	}
	panicOutput, err := exec.Command(binary.path, "panic").CombinedOutput()
	if err == nil || !bytes.Contains(panicOutput, []byte("main.main()")) || !bytes.Contains(panicOutput, []byte("development panic")) {
		t.Fatalf("stripped development binary lost its actionable panic stack: %v\n%s", err, panicOutput)
	}
	if err := binary.remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(binary.path); !os.IsNotExist(err) {
		t.Fatalf("development binary still exists: %v", err)
	}
}

func TestBuildDevelopmentBinaryCleansUpARejectedCandidate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/ridu-development-binary-failure\n\ngo 1.25.13\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() { this does not compile }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if binary, _, err := buildDevelopmentBinary(
		context.Background(),
		projectfile.File{Root: root, Entry: "."},
		io.Discard,
		&stderr,
	); err == nil {
		_ = binary.remove()
		t.Fatal("invalid development project built successfully")
	}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(developmentBinaryDirectory)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected development build left %d candidate(s): %v", len(entries), entries)
	}
}

func TestRebuildForGeneratedGoReplacesTheStaleCandidate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/ridu-development-binary-regeneration\n\ngo 1.25.13\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "generated"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "generated", "contract.go"), []byte("package generated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nimport _ \"example.com/ridu-development-binary-regeneration/generated\"\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	definition := projectfile.File{Root: root, Entry: ".", Schema: "generated/ridu.schema.json"}
	candidate, firstDuration, err := buildDevelopmentBinary(context.Background(), definition, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, totalDuration, err := rebuildForGeneratedGo(
		context.Background(),
		definition,
		candidate,
		firstDuration,
		developmentPreparation{generatedGoChanged: true},
		io.Discard,
		io.Discard,
		newCLIOutput(io.Discard, io.Discard, cliOutputOptions{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rebuilt.remove() })
	if candidate.path == rebuilt.path {
		t.Fatalf("generated Go rebuild reused stale candidate path %q", candidate.path)
	}
	if totalDuration <= firstDuration {
		t.Fatalf("combined build duration = %v, first build = %v", totalDuration, firstDuration)
	}
	if _, err := os.Stat(candidate.path); !os.IsNotExist(err) {
		t.Fatalf("stale candidate still exists: %v", err)
	}
}

func TestRebuildForGeneratedGoKeepsCandidateOutsideDependencyGraph(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/ridu-development-binary-no-regeneration\n\ngo 1.25.13\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "generated"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "generated", "contract.go"), []byte("package generated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	definition := projectfile.File{Root: root, Entry: ".", Schema: "generated/ridu.schema.json"}
	candidate, firstDuration, err := buildDevelopmentBinary(context.Background(), definition, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	kept, totalDuration, err := rebuildForGeneratedGo(
		context.Background(),
		definition,
		candidate,
		firstDuration,
		developmentPreparation{generatedGoChanged: true},
		io.Discard,
		io.Discard,
		newCLIOutput(io.Discard, io.Discard, cliOutputOptions{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kept.remove() })
	if candidate.path != kept.path {
		t.Fatalf("unreachable generated package replaced candidate %q with %q", candidate.path, kept.path)
	}
	if totalDuration != firstDuration {
		t.Fatalf("build duration changed from %v to %v without a rebuild", firstDuration, totalDuration)
	}
}

func TestDevelopmentEntryImportsGeneratedGoOnlyWhenReachable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/ridu-development-dependencies\n\ngo 1.25.13\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "generated"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "generated", "contract.go"), []byte("package generated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	definition := projectfile.File{Root: root, Entry: ".", Schema: "generated/ridu.schema.json"}
	importsGenerated, err := developmentEntryImportsGeneratedGo(context.Background(), definition)
	if err != nil {
		t.Fatal(err)
	}
	if importsGenerated {
		t.Fatal("unreachable generated package was reported as a server dependency")
	}
	if err := os.WriteFile(mainPath, []byte("package main\n\nimport _ \"example.com/ridu-development-dependencies/generated\"\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	importsGenerated, err = developmentEntryImportsGeneratedGo(context.Background(), definition)
	if err != nil {
		t.Fatal(err)
	}
	if !importsGenerated {
		t.Fatal("reachable generated package was not reported as a server dependency")
	}
}

func TestStabilizeDevelopmentCandidateResolvesTheRebuiltExecutable(t *testing.T) {
	root := t.TempDir()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	frameworkRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	goMod := "module example.com/ridu-development-fixed-point\n\ngo 1.25.13\n\nrequire github.com/riducms/ridu v0.0.0\n\nreplace github.com/riducms/ridu => " + filepath.ToSlash(frameworkRoot) + "\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	frameworkGoSum, err := os.ReadFile(filepath.Join(frameworkRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.sum"), frameworkGoSum, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "generated"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "generated", "ridu.generated.go"), []byte("package generated\n\ntype Post struct { Title string }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `package main

import (
	"fmt"
	"log"
	"reflect"

	"example.com/ridu-development-fixed-point/generated"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

func main() {
	config := ridu.Config{
		Name: fmt.Sprintf("Ridu-%d", reflect.TypeOf(generated.Post{}).NumField()),
		Collections: []ridu.Collection{{
			Slug: "posts",
			Fields: field.Fields{field.Text("title"), field.Text("summary")},
		}},
	}
	if err := ridu.Execute(config); err != nil { log.Fatal(err) }
}
`
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = root
	tidy.Env = append(os.Environ(), "GOWORK=off")
	if output, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("prepare fixed-point fixture: %v\n%s", err, output)
	}
	definition := projectfile.File{Root: root, Entry: ".", Schema: "generated/ridu.schema.json"}
	var stderr bytes.Buffer
	candidate, buildDuration, err := buildDevelopmentBinary(context.Background(), definition, io.Discard, &stderr)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	stable, _, preparation, err := stabilizeDevelopmentCandidate(
		context.Background(), definition, core.FrameworkVersion, candidate, buildDuration, false, nil, nil, io.Discard, &stderr,
		newCLIOutput(io.Discard, &stderr, cliOutputOptions{}),
	)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	t.Cleanup(func() { _ = stable.remove() })
	if stable.path == candidate.path {
		t.Fatal("generated Go dependency did not rebuild the initial candidate")
	}
	resolved, err := generate.ResolveProjectMetadataExecutable(context.Background(), definition, core.FrameworkVersion, stable.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.Manifest.Snapshot().Application.Name; got != "Ridu-5" {
		t.Fatalf("stable executable application name = %q, want Ridu-5", got)
	}
	if !resolved.Manifest.Equal(preparation.manifest) {
		t.Fatal("served executable manifest differs from stabilized generated contracts")
	}

	// A renamed field is detected against the database's accepted schema.
	renamed := strings.Replace(source, `field.Text("title"), field.Text("summary")`, `field.Text("headline"), field.Text("summary")`, 1)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(renamed), 0o644); err != nil {
		t.Fatal(err)
	}
	renamedBinary, _, err := buildDevelopmentBinary(context.Background(), definition, io.Discard, &stderr)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	t.Cleanup(func() { _ = renamedBinary.remove() })
	renamedMetadata, err := generate.ResolveProjectMetadataExecutable(context.Background(), definition, core.FrameworkVersion, renamedBinary.path)
	if err != nil {
		t.Fatal(err)
	}
	schemaPath := definition.Absolute(definition.Schema)
	generatedSchema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	reporter := newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})
	current := true
	prepare := func(project projectfile.File, renames *developmentRenames) error {
		_, err := prepareDevelopment(context.Background(), project, core.FrameworkVersion, renamedBinary.path, true, func() bool { return current }, renames, reporter)
		return err
	}
	// Without a terminal, ridu dev rejects the reload before it generates
	// anything, so the next save meets the same rename instead of slipping
	// through against a rewritten schema file.
	waiting := func(answer bool) *developmentRenames {
		renames, _ := devRenameWithoutTerminal("", "")
		renames.readBaseline = func(context.Context, projectfile.File) (schema.Manifest, bool, error) {
			if answer {
				return preparation.manifest, true, nil
			}
			return renamedMetadata.Manifest, true, nil
		}
		renames.stillHas = func(context.Context, schema.Manifest, schema.Manifest) (bool, error) { return answer, nil }
		return renames
	}
	for _, save := range []string{"first", "second"} {
		if err := prepare(definition, waiting(true)); err == nil || !strings.Contains(err.Error(), "paused for a possible rename") {
			t.Fatalf("the %s save of a rename without a terminal = %v", save, err)
		}
		if unchanged, err := os.ReadFile(schemaPath); err != nil || !bytes.Equal(unchanged, generatedSchema) {
			t.Fatalf("the %s rejected save regenerated the schema file: %v", save, err)
		}
	}
	// Once the database no longer has the old schema, because the rename was
	// migrated by hand, the same save goes through and the stale schema file
	// does not raise the question again.
	if err := prepare(definition, waiting(false)); err != nil {
		t.Fatalf("a save after the rename was migrated by hand = %v", err)
	}
	if regenerated, err := os.ReadFile(schemaPath); err != nil || bytes.Equal(regenerated, generatedSchema) {
		t.Fatalf("the released save did not regenerate the schema file: %v", err)
	}
	if err := os.WriteFile(schemaPath, generatedSchema, 0o644); err != nil {
		t.Fatal(err)
	}
	// A caller that supplies no rename handling still never syncs past one.
	if err := prepare(definition, nil); err == nil || !strings.Contains(err.Error(), "paused for a possible rename") {
		t.Fatalf("rename without rename handling = %v", err)
	}
	if err := os.WriteFile(schemaPath, generatedSchema, 0o644); err != nil {
		t.Fatal(err)
	}
	// A prompt that cannot finish rejects the reload before generation replaces
	// the schema file, so the next save asks again. It reports its own reason
	// rather than the generic pause.
	unreadable := definition
	unreadable.Migrations = "main.go"
	accepted, _ := devRenamePrompt("y\n\n", "")
	if err := prepare(unreadable, accepted); err == nil || !strings.Contains(err.Error(), "rename not applied: read migration history") || strings.Contains(err.Error(), "paused for a possible rename") {
		t.Fatalf("rename the prompt could not apply = %v", err)
	}
	if unchanged, err := os.ReadFile(schemaPath); err != nil || !bytes.Equal(unchanged, generatedSchema) {
		t.Fatalf("a paused rename prompt regenerated the schema file: %v", err)
	}
	// An answer given after the config changed again is about a change that
	// may no longer exist. It is discarded and the newest source is built.
	current = false
	stale, _ := devRenamePrompt("y\n\n", "")
	if err := prepare(definition, stale); !errors.Is(err, errDevelopmentSourceChanged) {
		t.Fatalf("an answer to a stale config = %v", err)
	}
	if unchanged, err := os.ReadFile(schemaPath); err != nil || !bytes.Equal(unchanged, generatedSchema) {
		t.Fatalf("a stale answer regenerated the schema file: %v", err)
	}
	if files, err := migrationartifact.ReadAll(definition.Absolute(definition.Migrations)); err != nil || len(files) != 0 {
		t.Fatalf("a stale answer wrote %d migrations, %v", len(files), err)
	}
	current = true
	// Declining every rename lets the reload continue as an additive change.
	// The database keeps the old schema until schema sync runs, and a reload
	// prepares the config again after generated Go changes; that second pass
	// must not repeat a question already answered. A later save asks again.
	declined, _ := devRenamePrompt("n\n", "")
	previousManifest, exists, err := schemadiff.ReadManifest(schemaPath)
	if err != nil || !exists {
		t.Fatalf("read the generated schema: %t, %v", exists, err)
	}
	declined.readBaseline = func(context.Context, projectfile.File) (schema.Manifest, bool, error) {
		return previousManifest, true, nil
	}
	// Only a project with a migrations directory can record a rename answer.
	recording := definition
	recording.Migrations = "migrations"
	declined.beginReload()
	for _, pass := range []string{"first", "second"} {
		if err := prepare(recording, declined); err != nil {
			t.Fatalf("the %s preparation of declined renames = %v", pass, err)
		}
	}
	if regenerated, err := os.ReadFile(schemaPath); err != nil || bytes.Equal(regenerated, generatedSchema) {
		t.Fatalf("declined renames did not regenerate the schema file: %v", err)
	}
	declined.beginReload()
	if err := prepare(recording, declined); err == nil || !strings.Contains(err.Error(), "read rename answer") {
		t.Fatalf("a later save of the declined rename = %v", err)
	}
}
