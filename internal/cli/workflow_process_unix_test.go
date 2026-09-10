//go:build !windows

package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestManagedProcessStopDeliversGracefulSignalAfterParentCancellation(t *testing.T) {
	directory := t.TempDir()
	readyPath := filepath.Join(directory, "ready")
	drainedPath := filepath.Join(directory, "drained")
	parent, cancelParent := context.WithCancel(t.Context())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	output := newCLIOutput(&stdout, &stderr, cliOutputOptions{})
	process, err := startManagedProcess(
		parent,
		"server",
		directory,
		[]string{"RIDU_TEST_READY=" + readyPath, "RIDU_TEST_DRAINED=" + drainedPath},
		output,
		"sh",
		"-c",
		`trap 'printf drained > "$RIDU_TEST_DRAINED"; exit 0' TERM
printf ready > "$RIDU_TEST_READY"
while :; do sleep 1; done`,
	)
	if err != nil {
		t.Fatal(err)
	}
	process.stopTimeout = 2 * time.Second
	t.Cleanup(process.stop)

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("managed test process did not become ready; stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// CommandContext used to inherit this cancellation and SIGKILL the child
	// before stop could deliver the server's graceful SIGTERM.
	cancelParent()
	select {
	case <-process.done:
		t.Fatalf("parent cancellation killed the managed process before graceful stop: %v", process.waitError())
	case <-time.After(50 * time.Millisecond):
	}

	process.stop()
	if encoded, err := os.ReadFile(drainedPath); err != nil || string(encoded) != "drained" {
		t.Fatalf("managed process did not handle SIGTERM gracefully: marker=%q error=%v stdout=%q stderr=%q", encoded, err, stdout.String(), stderr.String())
	}
	if err := process.waitError(); err != nil {
		t.Fatalf("gracefully stopped process error = %v", err)
	}
}

func TestManagedProcessStopRemovesDescendantAfterGracefulRootExit(t *testing.T) {
	directory := t.TempDir()
	readyPath := filepath.Join(directory, "ready")
	drainedPath := filepath.Join(directory, "drained")
	childPIDPath := filepath.Join(directory, "child-pid")
	childSurvivedPath := filepath.Join(directory, "child-survived")
	stoppedPath := filepath.Join(directory, "stopped")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	output := newCLIOutput(&stdout, &stderr, cliOutputOptions{})
	process, err := startManagedProcess(
		t.Context(),
		"server",
		directory,
		[]string{
			"RIDU_TEST_READY=" + readyPath,
			"RIDU_TEST_DRAINED=" + drainedPath,
			"RIDU_TEST_CHILD_PID=" + childPIDPath,
			"RIDU_TEST_CHILD_SURVIVED=" + childSurvivedPath,
			"RIDU_TEST_STOPPED=" + stoppedPath,
		},
		output,
		"sh",
		"-c",
		`trap 'printf drained > "$RIDU_TEST_DRAINED"; exit 0' TERM
sh -c '
trap "" TERM
printf "%s" "$$" > "$RIDU_TEST_CHILD_PID.tmp"
mv "$RIDU_TEST_CHILD_PID.tmp" "$RIDU_TEST_CHILD_PID"
while [ ! -f "$RIDU_TEST_STOPPED" ]; do sleep 0.01; done
sleep 1
printf survived > "$RIDU_TEST_CHILD_SURVIVED"
while :; do sleep 1; done
' </dev/null >/dev/null 2>&1 &
printf ready > "$RIDU_TEST_READY"
while :; do sleep 1; done`,
	)
	if err != nil {
		t.Fatal(err)
	}
	process.stopTimeout = 5 * time.Second
	processGroupID := process.command.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-processGroupID, syscall.SIGKILL)
		process.stop()
	})
	waitForUnixProcessMarker(t, readyPath, 2*time.Second, stdout.String, stderr.String)
	waitForUnixProcessMarker(t, childPIDPath, 2*time.Second, stdout.String, stderr.String)
	encodedChildPID, err := os.ReadFile(childPIDPath)
	if err != nil {
		t.Fatal(err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(encodedChildPID)))
	if err != nil {
		t.Fatalf("parse stubborn descendant PID %q: %v", encodedChildPID, err)
	}

	started := time.Now()
	process.stop()
	// Probe survival after cleanup returns, not after a startup-relative delay
	// that could expire while the root is still draining under scheduler load.
	if err := os.WriteFile(stoppedPath, []byte("stopped"), 0o600); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= process.stopTimeout {
		t.Fatalf("managed stop waited for the force timeout instead of cleaning the post-drain process group: %s", elapsed)
	}
	if encoded, err := os.ReadFile(drainedPath); err != nil || string(encoded) != "drained" {
		t.Fatalf("managed root did not drain gracefully: marker=%q error=%v stdout=%q stderr=%q", encoded, err, stdout.String(), stderr.String())
	}
	if err := process.waitError(); err != nil {
		t.Fatalf("gracefully stopped root process error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err := syscall.Getpgid(childPID)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if err != nil {
			t.Fatalf("inspect stubborn descendant %d: %v", childPID, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("stubborn descendant %d survived process-group shutdown", childPID)
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(1100 * time.Millisecond)
	if encoded, err := os.ReadFile(childSurvivedPath); err == nil {
		t.Fatalf("stubborn managed descendant survived group cleanup: %q", encoded)
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect stubborn descendant marker: %v", err)
	}
}

func waitForUnixProcessMarker(t *testing.T, path string, timeout time.Duration, stdout, stderr func() string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("managed test process did not write %s; stdout=%q stderr=%q", path, stdout(), stderr())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
