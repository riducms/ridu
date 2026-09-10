package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/internal/generate"
	"github.com/riducms/ridu/internal/projectfile"
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
		context.Background(), definition, core.FrameworkVersion, candidate, buildDuration, false, nil, io.Discard, &stderr,
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
}
