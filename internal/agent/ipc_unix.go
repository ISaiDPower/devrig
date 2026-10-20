//go:build !windows

package agent

import (
	"context"
	"net"
	"os"
	"os/exec"
	"syscall"

	"github.com/ISaiDPower/devrig/internal/home"
)

func listen(h home.Dir) (net.Listener, error) {
	// Serve has checked that no agent answers, so a socket file left behind
	// is stale.
	os.Remove(h.AgentSocket())
	l, err := net.Listen("unix", h.AgentSocket())
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(h.AgentSocket(), 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

func dial(ctx context.Context, h home.Dir) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", h.AgentSocket())
}

// spawn starts "rig agent run" in the background in a session of its own, so
// that closing the terminal does not end it.
func spawn(h home.Dir, exe string) error {
	log, err := os.OpenFile(h.AgentLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()

	cmd := exec.Command(exe, "agent", "run")
	cmd.Dir = h.Root
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
