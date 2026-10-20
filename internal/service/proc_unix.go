//go:build !windows

package service

import (
	"os/exec"
	"syscall"
)

func hideWindow(*exec.Cmd) {}

// groupTree runs the process in its own process group, so signals reach
// everything it starts.
type groupTree struct {
	pgid int
}

func startTree(cmd *exec.Cmd) (processTree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return groupTree{pgid: cmd.Process.Pid}, nil
}

func (t groupTree) terminate() { syscall.Kill(-t.pgid, syscall.SIGTERM) }
func (t groupTree) kill()      { syscall.Kill(-t.pgid, syscall.SIGKILL) }
func (t groupTree) close()     { syscall.Kill(-t.pgid, syscall.SIGKILL) }
