//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processTree is a managed command's console process group, held in a job
// object that ends every process in it when the job's last handle closes.
// Windows closes ridu's handle however ridu exits, including a crash,
// TerminateProcess, or a closed console, so no descendant outlives ridu.
type processTree struct {
	command *exec.Cmd
	job     windows.Handle
}

func startProcessTree(command *exec.Cmd) (processTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return processTree{}, fmt.Errorf("create process job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return processTree{}, fmt.Errorf("configure process job: %w", err)
	}
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	// The process starts suspended so it joins the job before it can start a
	// descendant. Every descendant then joins the job when it is created.
	command.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED
	if err := command.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return processTree{}, err
	}
	if err := joinJobAndResume(job, uint32(command.Process.Pid)); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = windows.CloseHandle(job)
		return processTree{}, fmt.Errorf("track process tree: %w", err)
	}
	return processTree{command: command, job: job}, nil
}

func joinJobAndResume(job windows.Handle, processID uint32) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, processID)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return err
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	resumed := false
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != processID {
			continue
		}
		thread, openError := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if openError != nil {
			return openError
		}
		_, resumeError := windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		if resumeError != nil {
			return resumeError
		}
		resumed = true
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return err
	}
	if !resumed {
		return fmt.Errorf("process %d has no thread to resume", processID)
	}
	return nil
}

func (tree processTree) terminate(force bool) {
	if tree.command == nil || tree.command.Process == nil {
		return
	}
	if !force {
		// A targeted CTRL_BREAK lets cooperative console descendants drain.
		_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(tree.command.Process.Pid))
		return
	}
	// The job also reaches descendants whose parent has already exited, which
	// taskkill /T cannot find from the root process.
	if tree.job != 0 && windows.TerminateJobObject(tree.job, 1) == nil {
		return
	}
	killTree := exec.Command("taskkill.exe", "/PID", strconv.Itoa(tree.command.Process.Pid), "/T", "/F")
	killTree.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := killTree.Run(); err != nil {
		_ = tree.command.Process.Kill()
	}
}

func (tree processTree) release() {
	if tree.job != 0 {
		_ = windows.CloseHandle(tree.job)
	}
}
