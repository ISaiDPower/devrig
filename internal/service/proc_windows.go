package service

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// hideWindow stops console programs started by the agent, which has no
// console of its own, from opening a window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
	}
}

// jobTree keeps the process tree in a job object. Closing the job's last
// handle kills everything in it, so services also end if the agent dies.
type jobTree struct {
	mu     sync.Mutex
	job    windows.Handle
	closed bool
}

func startTree(cmd *exec.Cmd) (processTree, error) {
	hideWindow(cmd)

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}

	// The process runs for a moment before it joins the job. Programs that
	// start helpers right away could start one outside it, but nginx and
	// the like read their configuration first.
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, proc)
		windows.CloseHandle(proc)
	}
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		windows.CloseHandle(job)
		return nil, err
	}

	return &jobTree{job: job}, nil
}

func (t *jobTree) terminate() { t.kill() }

func (t *jobTree) kill() {
	t.mu.Lock()
	defer t.mu.Unlock()
	// A closed handle value may already belong to something else.
	if !t.closed {
		windows.TerminateJobObject(t.job, 1)
	}
}

func (t *jobTree) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		windows.CloseHandle(t.job)
		t.closed = true
	}
}
