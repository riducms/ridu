//go:build windows

package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
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

func TestManagedProcessStopEndsDescendantsThatIgnoreCtrlBreak(t *testing.T) {
	const pattern = "^TestManagedProcessStopEndsDescendantsThatIgnoreCtrlBreak$"
	if role := os.Getenv(managedWindowsProcessHelper); role != "" {
		runStubbornWindowsProcessHelper(t, role, pattern)
		return
	}
	directory := t.TempDir()
	rootPIDPath := filepath.Join(directory, "root-pid")
	childPIDPath := filepath.Join(directory, "child-pid")
	process, err := startManagedProcess(
		t.Context(),
		"admin",
		directory,
		[]string{
			managedWindowsProcessHelper + "=root",
			"RIDU_WINDOWS_ROOT_PID=" + rootPIDPath,
			"RIDU_WINDOWS_CHILD_PID=" + childPIDPath,
		},
		newCLIOutput(io.Discard, io.Discard, cliOutputOptions{}),
		os.Args[0],
		"-test.run="+pattern,
	)
	if err != nil {
		t.Fatal(err)
	}
	process.stopTimeout = 5 * time.Second
	t.Cleanup(process.stop)
	root := openWindowsProcessFromMarker(t, rootPIDPath)
	child := openWindowsProcessFromMarker(t, childPIDPath)

	// The root drains on CTRL_BREAK; its child ignores CTRL_BREAK and would
	// otherwise outlive both the root and ridu.
	process.stop()
	assertWindowsProcessExits(t, root, "root that drains on CTRL_BREAK")
	assertWindowsProcessExits(t, child, "descendant that ignores CTRL_BREAK")
}

func TestManagedProcessTreeEndsWhenRiduIsTerminated(t *testing.T) {
	const pattern = "^TestManagedProcessTreeEndsWhenRiduIsTerminated$"
	switch role := os.Getenv(managedWindowsProcessHelper); role {
	case "":
	case "ridu":
		// Stands in for ridu dev: it starts a managed tree and is then killed
		// without any chance to stop it.
		process, err := startManagedProcess(
			context.Background(),
			"admin",
			os.Getenv("RIDU_WINDOWS_DIRECTORY"),
			[]string{managedWindowsProcessHelper + "=root"},
			newCLIOutput(io.Discard, io.Discard, cliOutputOptions{}),
			os.Args[0],
			"-test.run="+pattern,
		)
		if err != nil {
			t.Fatal(err)
		}
		<-process.done
		return
	default:
		runStubbornWindowsProcessHelper(t, role, pattern)
		return
	}
	directory := t.TempDir()
	rootPIDPath := filepath.Join(directory, "root-pid")
	childPIDPath := filepath.Join(directory, "child-pid")
	ridu := exec.Command(os.Args[0], "-test.run="+pattern)
	ridu.Env = append(os.Environ(),
		managedWindowsProcessHelper+"=ridu",
		"RIDU_WINDOWS_DIRECTORY="+directory,
		"RIDU_WINDOWS_ROOT_PID="+rootPIDPath,
		"RIDU_WINDOWS_CHILD_PID="+childPIDPath,
	)
	if err := ridu.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ridu.Process.Kill()
		_ = ridu.Wait()
	})
	root := openWindowsProcessFromMarker(t, rootPIDPath)
	child := openWindowsProcessFromMarker(t, childPIDPath)

	// Process.Kill calls TerminateProcess, as the npm launcher used to.
	if err := ridu.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = ridu.Wait()
	assertWindowsProcessExits(t, root, "managed root of a terminated ridu")
	assertWindowsProcessExits(t, child, "descendant of a terminated ridu")
}

// runStubbornWindowsProcessHelper runs a root that drains on CTRL_BREAK and a
// child that ignores it. Each records its process ID for the test.
func runStubbornWindowsProcessHelper(t *testing.T, role, pattern string) {
	t.Helper()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	switch role {
	case "root":
		child := exec.Command(os.Args[0], "-test.run="+pattern)
		child.Env = append(os.Environ(), managedWindowsProcessHelper+"=child")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		writeWindowsProcessID(t, os.Getenv("RIDU_WINDOWS_ROOT_PID"))
		select {
		case <-signals:
		case <-time.After(time.Minute):
		}
	case "child":
		// Receiving os.Interrupt without acting on it ignores CTRL_BREAK.
		// signal.Ignore would restore the default handler, which exits.
		writeWindowsProcessID(t, os.Getenv("RIDU_WINDOWS_CHILD_PID"))
		time.Sleep(time.Minute)
	default:
		t.Fatalf("unknown Windows process helper role %q", role)
	}
}

func writeWindowsProcessID(t *testing.T, path string) {
	t.Helper()
	temporary := path + ".tmp"
	writeWindowsProcessMarker(t, temporary, strconv.Itoa(os.Getpid()))
	if err := os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
}

func openWindowsProcessFromMarker(t *testing.T, path string) windows.Handle {
	t.Helper()
	waitForWindowsProcessMarker(t, path)
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	processID, err := strconv.ParseUint(string(encoded), 10, 32)
	if err != nil {
		t.Fatalf("parse Windows process ID marker %q: %v", encoded, err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(processID))
	if err != nil {
		t.Fatalf("open Windows process %d: %v", processID, err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(handle) })
	return handle
}

func assertWindowsProcessExits(t *testing.T, process windows.Handle, label string) {
	t.Helper()
	event, err := windows.WaitForSingleObject(process, 10_000)
	if err != nil || event != windows.WAIT_OBJECT_0 {
		t.Fatalf("Windows %s is still running: wait=%#x error=%v", label, event, err)
	}
}
