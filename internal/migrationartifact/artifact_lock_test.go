package migrationartifact

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const (
	artifactLockChildDirectory = "RIDU_TEST_ARTIFACT_LOCK_DIRECTORY"
	artifactLockChildBusy      = "RIDU_TEST_ARTIFACT_LOCK_EXPECT_BUSY"
)

func TestCreateLockExcludesSeparateProcess(t *testing.T) {
	if directory := os.Getenv(artifactLockChildDirectory); directory != "" {
		lock, err := acquireCreateLock(directory)
		if os.Getenv(artifactLockChildBusy) == "1" {
			if err == nil {
				_ = lock.Close()
				t.Fatal("separate process acquired an already-held artifact creation lock")
			}
			if !strings.Contains(err.Error(), "directory is busy") {
				t.Fatalf("separate-process lock error = %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("separate process did not acquire released artifact creation lock: %v", err)
		}
		if err := lock.Close(); err != nil {
			t.Fatalf("release separate-process artifact creation lock: %v", err)
		}
		return
	}

	directory := t.TempDir()
	lock, err := acquireCreateLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	runArtifactLockChild(t, directory, true)
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	runArtifactLockChild(t, directory, false)
}

func runArtifactLockChild(t *testing.T, directory string, expectBusy bool) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestCreateLockExcludesSeparateProcess$")
	busy := "0"
	if expectBusy {
		busy = "1"
	}
	command.Env = append(os.Environ(), artifactLockChildDirectory+"="+directory, artifactLockChildBusy+"="+busy)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("artifact creation-lock child failed: %v\n%s", err, output)
	}
}
