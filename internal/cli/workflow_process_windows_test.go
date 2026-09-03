//go:build windows

package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"testing"
	"time"
)

const managedWindowsProcessHelper = "RIDU_MANAGED_WINDOWS_PROCESS_HELPER"

func TestManagedProcessStopDeliversWindowsCtrlBreakToCooperativeProcessTree(t *testing.T) {
	if os.Getenv(managedWindowsProcessHelper) != "" {
		runManagedWindowsProcessHelper(t)
		return
	}
	directory := t.TempDir()
	rootReadyPath := filepath.Join(directory, "root-ready")
	rootDrainedPath := filepath.Join(directory, "root-drained")
	childReadyPath := filepath.Join(directory, "child-ready")
	childDrainedPath := filepath.Join(directory, "child-drained")
	parent, cancelParent := context.WithCancel(t.Context())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	output := newCLIOutput(&stdout, &stderr, cliOutputOptions{})
	process, err := startManagedProcess(
		parent,
		"server",
		directory,
		[]string{
			managedWindowsProcessHelper + "=root",
			"RIDU_WINDOWS_ROOT_READY=" + rootReadyPath,
			"RIDU_WINDOWS_ROOT_DRAINED=" + rootDrainedPath,
			"RIDU_WINDOWS_CHILD_READY=" + childReadyPath,
			"RIDU_WINDOWS_CHILD_DRAINED=" + childDrainedPath,
		},
		output,
		os.Args[0],
		"-test.run=^TestManagedProcessStopDeliversWindowsCtrlBreakToCooperativeProcessTree$",
	)
	if err != nil {
		t.Fatal(err)
	}
	process.stopTimeout = 5 * time.Second
	t.Cleanup(process.stop)
	waitForWindowsProcessMarker(t, rootReadyPath)

	cancelParent()
	select {
	case <-process.done:
		t.Fatalf("parent cancellation killed the Windows process before graceful stop: %v", process.waitError())
	case <-time.After(50 * time.Millisecond):
	}

	process.stop()
	assertWindowsProcessMarker(t, rootDrainedPath, "root", stdout.String(), stderr.String())
	assertWindowsProcessMarker(t, childDrainedPath, "child", stdout.String(), stderr.String())
	if err := process.waitError(); err != nil {
		t.Fatalf("gracefully stopped Windows process error = %v", err)
	}
}

func runManagedWindowsProcessHelper(t *testing.T) {
	t.Helper()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	switch os.Getenv(managedWindowsProcessHelper) {
	case "child":
		writeWindowsProcessMarker(t, os.Getenv("RIDU_WINDOWS_CHILD_READY"), "ready")
		waitForWindowsInterrupt(t, signals)
		writeWindowsProcessMarker(t, os.Getenv("RIDU_WINDOWS_CHILD_DRAINED"), "drained")
	case "root":
		child := exec.Command(os.Args[0], "-test.run=^TestManagedProcessStopDeliversWindowsCtrlBreakToCooperativeProcessTree$")
		child.Env = append(os.Environ(),
			managedWindowsProcessHelper+"=child",
			"RIDU_WINDOWS_CHILD_READY="+os.Getenv("RIDU_WINDOWS_CHILD_READY"),
			"RIDU_WINDOWS_CHILD_DRAINED="+os.Getenv("RIDU_WINDOWS_CHILD_DRAINED"),
		)
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		waitForWindowsProcessMarker(t, os.Getenv("RIDU_WINDOWS_CHILD_READY"))
		writeWindowsProcessMarker(t, os.Getenv("RIDU_WINDOWS_ROOT_READY"), "ready")
		waitForWindowsInterrupt(t, signals)
		waitForWindowsProcessMarker(t, os.Getenv("RIDU_WINDOWS_CHILD_DRAINED"))
		if err := child.Wait(); err != nil {
			t.Fatalf("wait for cooperative Windows child: %v", err)
		}
		writeWindowsProcessMarker(t, os.Getenv("RIDU_WINDOWS_ROOT_DRAINED"), "drained")
	default:
		t.Fatalf("unknown Windows process helper role %q", os.Getenv(managedWindowsProcessHelper))
	}
}

func waitForWindowsInterrupt(t *testing.T, signals <-chan os.Signal) {
	t.Helper()
	select {
	case <-signals:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for CTRL_BREAK")
	}
}

func writeWindowsProcessMarker(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertWindowsProcessMarker(t *testing.T, path, label, stdout, stderr string) {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil || string(encoded) != "drained" {
		t.Fatalf("Windows %s did not handle CTRL_BREAK gracefully: marker=%q error=%v stdout=%q stderr=%q", label, encoded, err, stdout, stderr)
	}
}

func waitForWindowsProcessMarker(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for Windows process marker %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
