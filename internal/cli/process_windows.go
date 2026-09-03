//go:build windows

package cli

import (
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureProcess(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}

func terminateProcess(command *exec.Cmd, force bool) {
	if command == nil || command.Process == nil {
		return
	}
	if !force {
		// A targeted CTRL_BREAK lets cooperative console descendants drain.
		_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(command.Process.Pid))
		return
	}
	killTree := exec.Command("taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
	killTree.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := killTree.Run(); err != nil {
		_ = command.Process.Kill()
	}
}
