package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/riducms/ridu"
)

func TestVersion(t *testing.T) {
	previousVersion := version
	version = "v1.2.3"
	t.Cleanup(func() { version = previousVersion })

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if exitCode := run([]string{"version"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run(version) exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	if got, want := stdout.String(), "ridu v1.2.3\n"; got != want {
		t.Fatalf("run(version) stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("run(version) stderr = %q, want empty", stderr.String())
	}
}

func TestCLIVersionHasADevelopmentFallback(t *testing.T) {
	previousVersion := version
	version = ""
	t.Cleanup(func() { version = previousVersion })

	got := cliVersion()
	if got == "" || got == "(devel)" {
		t.Fatalf("cliVersion() = %q, want an actionable version", got)
	}
	if information, ok := debug.ReadBuildInfo(); !ok || information.Main.Version == "" || information.Main.Version == "(devel)" {
		if got != ridu.FrameworkVersion {
			t.Fatalf("cliVersion() = %q, want development fallback %q", got, ridu.FrameworkVersion)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if exitCode := run([]string{"serve"}, &stdout, &stderr); exitCode != 2 {
		t.Fatalf("run(unknown) exit code = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("run(unknown) stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, `unknown command "serve"`) {
		t.Fatalf("run(unknown) stderr = %q, want actionable diagnostic", got)
	}
}
