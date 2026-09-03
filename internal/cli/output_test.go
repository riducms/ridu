package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
)

func TestCLIRawWritersPreserveTerminalFileCapability(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = read.Close()
		_ = write.Close()
	})

	output := newCLIOutput(write, io.Discard, cliOutputOptions{})
	stdout, _ := output.rawWriters()
	terminal, ok := stdout.(terminalFile)
	if !ok {
		t.Fatalf("serialized *os.File writer has type %T; terminal capability was lost", stdout)
	}
	if terminal.Fd() != write.Fd() {
		t.Fatalf("serialized writer fd = %d, want %d", terminal.Fd(), write.Fd())
	}

	plain, _ := newCLIOutput(&bytes.Buffer{}, io.Discard, cliOutputOptions{}).rawWriters()
	if _, ok := plain.(terminalFile); ok {
		t.Fatalf("non-file writer %T unexpectedly advertises terminal capability", plain)
	}
}

func TestCLIOutputFormatsLevelsAndRoutesStreams(t *testing.T) {
	withoutEnvironment(t, "NO_COLOR")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	profile := colorprofile.NoTTY
	fixed := time.Date(2026, time.September, 1, 8, 31, 9, 0, time.FixedZone("BST", 60*60))
	output := newCLIOutput(&stdout, &stderr, cliOutputOptions{
		timeFunction:  func(time.Time) time.Time { return fixed },
		stdoutProfile: &profile,
		stderrProfile: &profile,
	})

	output.Info("Ridu is ready")
	output.Warn("watcher warning", errors.New("temporary failure"))
	output.Error("change rejected", errors.New("schema validation failed\ncollections[0].fields[1]: missing choices"))

	if got, want := stdout.String(), "08:31:09 [ridu] Ridu is ready\n"; got != want {
		t.Fatalf("stdout log = %q, want %q", got, want)
	}
	wantStderr := "08:31:09 [WARN] [ridu] watcher warning: temporary failure\n" +
		"08:31:09 [ERROR] [ridu] change rejected\n" +
		"  schema validation failed\n" +
		"  collections[0].fields[1]: missing choices\n"
	if got := stderr.String(); got != wantStderr {
		t.Fatalf("stderr log = %q, want %q", got, wantStderr)
	}
}

func TestDevelopmentReadyUsesAStandaloneReadableBlock(t *testing.T) {
	var stdout bytes.Buffer
	profile := colorprofile.NoTTY
	output := newCLIOutput(&stdout, io.Discard, cliOutputOptions{stdoutProfile: &profile})
	output.DevelopmentReady("v1.2.3", "947ms", "http://127.0.0.1:5173/admin/", "http://127.0.0.1:8080")
	want := "\n ridu  v1.2.3 ready in 947ms\n" +
		"┃ Admin  http://127.0.0.1:5173/admin/\n" +
		"┃ API    http://127.0.0.1:8080\n\n"
	if got := stdout.String(); got != want {
		t.Fatalf("development ready block = %q, want %q", got, want)
	}
}

func TestCLIOutputDisablesANSIForAccessibilityAndNoColorPresence(t *testing.T) {
	for _, test := range []struct {
		name       string
		accessible bool
		noColor    bool
	}{
		{name: "accessible", accessible: true},
		{name: "empty NO_COLOR", noColor: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			withoutEnvironment(t, "NO_COLOR")
			if test.noColor {
				t.Setenv("NO_COLOR", "")
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			forced := colorprofile.TrueColor
			output := newCLIOutput(&stdout, &stderr, cliOutputOptions{
				accessible:    test.accessible,
				stdoutProfile: &forced,
				stderrProfile: &forced,
			})
			output.Info("plain")
			output.Error("plain", errors.New("failure"))
			if strings.Contains(stdout.String(), "\x1b[") || strings.Contains(stderr.String(), "\x1b[") {
				t.Fatalf("disabled color output contains ANSI: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestCLIOutputUsesColorProfileForOwnedStyles(t *testing.T) {
	withoutEnvironment(t, "NO_COLOR")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	profile := colorprofile.TrueColor
	output := newCLIOutput(&stdout, &stderr, cliOutputOptions{stdoutProfile: &profile, stderrProfile: &profile})
	output.Info("colored")
	output.DevelopmentReady("v1.2.3", "947ms", "http://127.0.0.1:5173/admin/", "http://127.0.0.1:8080")
	output.Error("colored", errors.New("failure"))
	if !strings.Contains(stdout.String(), "\x1b[") || !strings.Contains(stderr.String(), "\x1b[") {
		t.Fatalf("forced color output lacks ANSI: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "\x1b[1") {
		t.Fatalf("normal output is unexpectedly bold: %q", stdout.String())
	}
	if strings.Count(stderr.String(), "\x1b[1") != 1 {
		t.Fatalf("error output does not bold only its timestamp: %q", stderr.String())
	}
}

func TestSourceWriterBuffersPhysicalLinesAndPreservesChildBytes(t *testing.T) {
	withoutEnvironment(t, "NO_COLOR")
	var stdout bytes.Buffer
	profile := colorprofile.NoTTY
	output := newCLIOutput(&stdout, &bytes.Buffer{}, cliOutputOptions{stdoutProfile: &profile})
	writer := output.sourceWriter(&stdout, "admin", profile)

	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("partial child line was emitted early: %q", stdout.String())
	}
	if _, err := writer.Write([]byte(" line\n\n\x1b[31mchild ansi\x1b[0m\nlast")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	want := "partial line\n\n\x1b[31mchild ansi\x1b[0m\nlast"
	if got := stdout.String(); got != want {
		t.Fatalf("child output = %q, want %q", got, want)
	}
}

func TestSourceWriterLetsViteOwnItsFormatting(t *testing.T) {
	withoutEnvironment(t, "NO_COLOR")
	var stdout bytes.Buffer
	profile := colorprofile.TrueColor
	output := newCLIOutput(&stdout, &bytes.Buffer{}, cliOutputOptions{stdoutProfile: &profile})
	writer := output.sourceWriter(&stdout, "admin", profile)
	if _, err := writer.Write([]byte("\x1b[31mchild\x1b[0m\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	got := stdout.String()
	if got != "\x1b[31mchild\x1b[0m\n" {
		t.Fatalf("child ANSI was altered: %q", got)
	}
}

func TestStructuredCLIErrorBreaksContextChainIntoPhysicalLines(t *testing.T) {
	encoded := "resolve executable config: project manifest command failed: 21:38:14 resolve project manifest: schema validation failed with 1 issue(s):\n  - collections[0].fields[1]: missing choices: exit status 1"
	want := "resolve executable config\n" +
		"project manifest command failed\n" +
		"resolve project manifest\n" +
		"schema validation failed with 1 issue(s):\n" +
		"  - collections[0].fields[1]: missing choices\n" +
		"exit status 1"
	if got := formatStructuredCLIError(encoded); got != want {
		t.Fatalf("structured error = %q, want %q", got, want)
	}
}

func TestForegroundCommandsReceiveOriginalWriters(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	output := newCLIOutput(&stdout, &stderr, cliOutputOptions{})
	wrapperOut, wrapperErr := output.rawWriters()
	if got := unwrapCLIWriter(wrapperOut); got != &stdout {
		t.Fatalf("foreground stdout = %T, want original *bytes.Buffer", got)
	}
	if got := unwrapCLIWriter(wrapperErr); got != &stderr {
		t.Fatalf("foreground stderr = %T, want original *bytes.Buffer", got)
	}
}

func TestSourceWritersSerializeCompleteLines(t *testing.T) {
	var stdout bytes.Buffer
	profile := colorprofile.NoTTY
	output := newCLIOutput(&stdout, &bytes.Buffer{}, cliOutputOptions{stdoutProfile: &profile})
	admin := output.sourceWriter(&stdout, "admin", profile)
	server := output.sourceWriter(&stdout, "server", profile)

	var group sync.WaitGroup
	for index := 0; index < 50; index++ {
		group.Add(2)
		go func() {
			defer group.Done()
			_, _ = admin.Write([]byte("admin-line\n"))
		}()
		go func() {
			defer group.Done()
			_, _ = server.Write([]byte("server-line\n"))
		}()
	}
	group.Wait()
	_ = admin.Close()
	_ = server.Close()

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 100 {
		t.Fatalf("serialized line count = %d, want 100", len(lines))
	}
	for _, line := range lines {
		if line != "admin-line" && line != "[server] server-line" {
			t.Fatalf("interleaved child line %q", line)
		}
	}
}

func BenchmarkCLIOutputConstruction(b *testing.B) {
	for b.Loop() {
		_ = newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})
	}
}

func BenchmarkCLIOutputFirstInfo(b *testing.B) {
	for b.Loop() {
		output := newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})
		output.Info("development server is ready")
	}
}

func BenchmarkCLIOutputInfo(b *testing.B) {
	output := newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})
	b.ResetTimer()
	for b.Loop() {
		output.Info("development server is ready")
	}
}

func BenchmarkCLIOutputMultilineError(b *testing.B) {
	output := newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})
	err := errors.New("schema validation failed\ncollections[0].fields[1]: missing choices")
	b.ResetTimer()
	for b.Loop() {
		output.Error("change rejected", err)
	}
}

func withoutEnvironment(t *testing.T, name string) {
	t.Helper()
	value, present := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(name, value)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}
