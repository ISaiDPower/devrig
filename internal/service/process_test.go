package service

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/internal/service/servicetest"
)

func TestMain(m *testing.M) {
	servicetest.Main()

	readyTimeout = 5 * time.Second
	stopTimeout = 3 * time.Second
	firstBackoff = 50 * time.Millisecond
	os.Exit(m.Run())
}

// newInstance runs the test binary as a service with the given arguments.
func newInstance(t *testing.T, args ...string) *Instance {
	t.Helper()
	servicetest.Enable(t)

	root := t.TempDir()
	return &Instance{
		Name:    "helper",
		Version: "1.0.0",
		Exe:     os.Args[0],
		Release: filepath.Join(root, "release"),
		State:   filepath.Join(root, "state"),
		Data:    filepath.Join(root, "data"),
		Ports:   map[string]int{"http": servicetest.FreePort(t)},
		Spec: &registry.Service{
			Command: "helper",
			Args:    args,
			Ready:   "http",
		},
	}
}

func dial(port int) error {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
	if err == nil {
		conn.Close()
	}
	return err
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStartAndStop(t *testing.T) {
	inst := newInstance(t, "listen", "{{.Ports.http}}", "{{.State}}")
	p, err := Start(inst)
	if err != nil {
		t.Fatal(err)
	}

	st := p.Status()
	if st.State != StateRunning || st.PID == 0 || st.Ports["http"] != inst.Ports["http"] {
		t.Errorf("status after Start = %+v", st)
	}
	if err := dial(inst.Ports["http"]); err != nil {
		t.Errorf("service does not accept connections: %v", err)
	}

	p.Stop()
	if st := p.Status(); st.State != StateStopped || st.PID != 0 {
		t.Errorf("status after Stop = %+v", st)
	}
	if err := dial(inst.Ports["http"]); err == nil {
		t.Error("service still accepts connections after Stop")
	}
}

func TestStopCommand(t *testing.T) {
	inst := newInstance(t, "listen", "{{.Ports.http}}", "{{.State}}")
	inst.Spec.Stop = []string{"stop", "{{.State}}"}

	p, err := Start(inst)
	if err != nil {
		t.Fatal(err)
	}
	p.Stop()

	lines, err := Tail(inst.LogFile(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if log := strings.Join(lines, "\n"); !strings.Contains(log, "fake service stopping cleanly") || strings.Contains(log, "killing it") {
		t.Errorf("the stop command should have stopped the service cleanly:\n%s", log)
	}
}

func TestStartReportsEarlyExit(t *testing.T) {
	inst := newInstance(t, "crash")

	_, err := Start(inst)
	if err == nil {
		t.Fatal("Start succeeded for a service that exits at once")
	}
	if msg := err.Error(); !strings.Contains(msg, "exit code 3") || !strings.Contains(msg, "fake service crashing") {
		t.Errorf("error should give the exit code and the end of the log:\n%s", msg)
	}
}

func TestStartTimesOut(t *testing.T) {
	old := readyTimeout
	readyTimeout = 500 * time.Millisecond
	t.Cleanup(func() { readyTimeout = old })

	inst := newInstance(t, "sleep")
	start := time.Now()
	_, err := Start(inst)
	if err == nil || !strings.Contains(err.Error(), "within") {
		t.Fatalf("error = %v, want a timeout", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Start took %s, the helper should have been killed after the timeout", d)
	}
}

func TestStartRejectsPortInUse(t *testing.T) {
	inst := newInstance(t, "listen", "{{.Ports.http}}", "{{.State}}")
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(inst.Ports["http"]))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	if _, err := Start(inst); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Errorf("error = %v, want port already in use", err)
	}
}

func TestRestartAfterCrash(t *testing.T) {
	inst := newInstance(t, "listen", "{{.Ports.http}}", "{{.State}}")
	p, err := Start(inst)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	first := p.Status().PID
	proc, err := os.FindProcess(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "a restart", func() bool {
		st := p.Status()
		return st.State == StateRunning && st.Restarts == 1 && st.PID != first
	})
	if err := dial(inst.Ports["http"]); err != nil {
		t.Errorf("restarted service does not accept connections: %v", err)
	}
}

func TestPrepareRendersTemplates(t *testing.T) {
	inst := newInstance(t)
	inst.Spec.Dirs = []string{"logs", "temp/a"}
	inst.Spec.Templates = map[string]string{"conf/app.conf": "listen {{.Ports.http}}; root {{.Release}}/html;\n"}

	if err := inst.Prepare(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"logs", "temp/a"} {
		if info, err := os.Stat(filepath.Join(inst.State, d)); err != nil || !info.IsDir() {
			t.Errorf("directory %s not created: %v", d, err)
		}
	}
	if _, err := os.Stat(inst.Data); err != nil {
		t.Errorf("data directory not created: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(inst.State, "conf", "app.conf"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("listen %d; root %s/html;\n", inst.Ports["http"], filepath.ToSlash(inst.Release))
	if string(got) != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

func TestPrepareRejectsUnknownPort(t *testing.T) {
	inst := newInstance(t)
	inst.Spec.Templates = map[string]string{"app.conf": "{{.Ports.smtp}}"}

	if err := inst.Prepare(); err == nil {
		t.Error("Prepare succeeded with a template using an unknown port")
	}
}

func TestTail(t *testing.T) {
	file := filepath.Join(t.TempDir(), "log")
	var lines []string
	for i := range 5000 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\r\n")+"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Tail(file, 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := "line 4997|line 4998|line 4999"; strings.Join(got, "|") != want {
		t.Errorf("Tail = %q, want %s", got, want)
	}

	if got, _ := Tail(file, 10000); len(got) != 5000 {
		t.Errorf("Tail of more lines than the file has returned %d lines, want 5000", len(got))
	}

	empty := filepath.Join(t.TempDir(), "empty")
	os.WriteFile(empty, nil, 0o644)
	if got, err := Tail(empty, 3); err != nil || len(got) != 0 {
		t.Errorf("Tail of an empty file = %q, %v", got, err)
	}
	if _, err := Tail(filepath.Join(t.TempDir(), "missing"), 3); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Tail of a missing file: error = %v, want ErrNotExist", err)
	}
}
