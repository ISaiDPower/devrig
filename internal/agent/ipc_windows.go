package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"github.com/ISaiDPower/devrig/internal/home"
)

// pipeName derives the agent's pipe from the home directory, so agents for
// different homes do not meet.
func pipeName(h home.Dir) string {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(h.Root))))
	return `\\.\pipe\devrig-` + hex.EncodeToString(sum[:8])
}

func listen(h home.Dir) (net.Listener, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	// A protected DACL with one entry: full access for the current user and
	// nobody else.
	sddl := "D:P(A;;GA;;;" + user.User.Sid.String() + ")"
	return winio.ListenPipe(pipeName(h), &winio.PipeConfig{SecurityDescriptor: sddl})
}

func dial(ctx context.Context, h home.Dir) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipeName(h))
}

// spawn starts "rig agent run" in the background, detached from the console
// so that closing the terminal does not end it.
func spawn(h home.Dir, exe string) error {
	log, err := os.OpenFile(h.AgentLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()

	start := func(flags uint32) (*exec.Cmd, error) {
		cmd := exec.Command(exe, "agent", "run")
		// Run from the home directory: a process's working directory cannot
		// be deleted or renamed on Windows, and the agent lives long.
		cmd.Dir = h.Root
		cmd.Stdout, cmd.Stderr = log, log
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
		return cmd, cmd.Start()
	}

	flags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	// Terminals and IDEs often run their children in a job object that ends
	// them all on close. Leave it if the job allows that.
	cmd, err := start(flags | windows.CREATE_BREAKAWAY_FROM_JOB)
	if err != nil {
		cmd, err = start(flags)
	}
	if err != nil {
		return err
	}
	return cmd.Process.Release()
}
