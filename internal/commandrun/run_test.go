package commandrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestRunDoesNotTagCompletedFailureAsCancellation(t *testing.T) {
	if os.Getenv("RIDU_COMMANDRUN_FAILURE_CHILD") == "true" {
		os.Exit(7)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunDoesNotTagCompletedFailureAsCancellation$")
	command.Env = append(os.Environ(), "RIDU_COMMANDRUN_FAILURE_CHILD=true")
	err := Run(ctx, command)
	cancel()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 7 || errors.Is(err, context.Canceled) {
		t.Fatalf("command error=%v", err)
	}
}
