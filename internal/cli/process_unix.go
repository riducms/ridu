//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// processTree is a managed command's Unix process group.
type processTree struct {
	command *exec.Cmd
}

func startProcessTree(command *exec.Cmd) (processTree, error) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return processTree{}, err
	}
	return processTree{command: command}, nil
}

func (tree processTree) terminate(force bool) {
	if tree.command == nil || tree.command.Process == nil {
		return
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	_ = syscall.Kill(-tree.command.Process.Pid, signal)
}

func (processTree) release() {}
