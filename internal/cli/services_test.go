package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ISaiDPower/devrig/internal/agent"
	"github.com/ISaiDPower/devrig/internal/home"
	"github.com/ISaiDPower/devrig/internal/install"
	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/internal/service/servicetest"
)

func TestMain(m *testing.M) {
	servicetest.Main()
	os.Exit(m.Run())
}

// serviceApp returns an app whose agent runs in the test, with release 1.0.0
// of a fake service installed, and the port the service will listen on.
func serviceApp(t *testing.T) (*app, int) {
	t.Helper()

	h := home.Dir{Root: t.TempDir()}
	asset := `{"` + registry.Platform() + `": {"url": "https://example.com/x.zip", "sha256": "` + strings.Repeat("a", 64) + `", "size": 1}}`
	reg, err := registry.Load(fstest.MapFS{
		"fake/recipe.toml": {Data: []byte(`
name = "fake"
kind = "service"

[service]
command = "fake"
args    = ["listen", "{{.Ports.http}}", "{{.State}}"]
stop    = ["stop", "{{.State}}"]
ready   = "http"

[service.ports]
http = 1
`)},
		"fake/versions.json": {Data: []byte(`{"1.0.0": ` + asset + `}`)},
		"tool/recipe.toml":   {Data: []byte("name = \"tool\"\nkind = \"tool\"\n")},
		"tool/versions.json": {Data: []byte(`{"1.0.0": ` + asset + `}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	servicetest.Install(t, h.Release("fake", "1.0.0"), "fake")

	port := servicetest.FreePort(t)
	if err := os.WriteFile(h.Config(), fmt.Appendf(nil, "[services.fake.ports]\nhttp = %d\n", port), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- agent.NewServer(h, reg).Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-served
	})

	client := agent.NewClient(h)
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := client.Status(context.Background()); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("agent did not come up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	return &app{
		home:      h,
		registry:  reg,
		installer: &install.Installer{Home: h},
		platform:  registry.Platform(),
		agent:     func() *agent.Client { return client },
		connect:   func(context.Context) (*agent.Client, error) { return client, nil },
	}, port
}

func TestServiceCommands(t *testing.T) {
	a, port := serviceApp(t)
	url := "http://127.0.0.1:" + strconv.Itoa(port)

	if out := mustRun(t, a, "start", "fake"); !strings.Contains(out, "fake 1.0.0 is running: "+url) {
		t.Errorf("start:\n%s", out)
	}

	out := mustRun(t, a, "status")
	if !strings.Contains(out, "running for") || !strings.Contains(out, url) {
		t.Errorf("status should show fake running on %s:\n%s", url, out)
	}

	if out := mustRun(t, a, "logs", "fake"); !strings.Contains(out, "fake service listening") {
		t.Errorf("logs:\n%s", out)
	}

	if out := mustRun(t, a, "restart", "fake"); !strings.Contains(out, "fake 1.0.0 is running") {
		t.Errorf("restart:\n%s", out)
	}
	if out := mustRun(t, a, "status"); !strings.Contains(out, "restarted") && !strings.Contains(out, "running") {
		t.Errorf("status after restart:\n%s", out)
	}

	if out := mustRun(t, a, "stop", "fake"); !strings.Contains(out, "Stopped fake 1.0.0") {
		t.Errorf("stop:\n%s", out)
	}
	if _, err := run(t, a, "stop", "fake"); err == nil {
		t.Error("stopping a stopped service should fail")
	}
	if out := mustRun(t, a, "stop", "--all"); !strings.Contains(out, "Nothing is running") {
		t.Errorf("stop --all with nothing running:\n%s", out)
	}

	// The log is still there once the service has stopped.
	if out := mustRun(t, a, "logs", "fake"); !strings.Contains(out, "stopping cleanly") {
		t.Errorf("logs after stop:\n%s", out)
	}
}

func TestStopAll(t *testing.T) {
	a, _ := serviceApp(t)
	mustRun(t, a, "start", "fake")

	if out := mustRun(t, a, "stop", "--all"); !strings.Contains(out, "Stopped fake 1.0.0") {
		t.Errorf("stop --all:\n%s", out)
	}
}

func TestServiceCommandErrors(t *testing.T) {
	a, _ := serviceApp(t)

	for _, args := range [][]string{
		{"start", "tool"},
		{"start", "nope"},
		{"stop"},
		{"stop", "--all", "fake"},
		{"restart", "fake"},
		{"logs", "nope"},
	} {
		if _, err := run(t, a, args...); err == nil {
			t.Errorf("rig %s succeeded, want an error", strings.Join(args, " "))
		}
	}
}

func TestStatusWithoutAgent(t *testing.T) {
	h := home.Dir{Root: t.TempDir()}
	a := &app{home: h, agent: func() *agent.Client { return agent.NewClient(h) }}

	if out := mustRun(t, a, "status"); !strings.Contains(out, "No services are running") {
		t.Errorf("status:\n%s", out)
	}
	if out := mustRun(t, a, "agent", "stop"); !strings.Contains(out, "not running") {
		t.Errorf("agent stop:\n%s", out)
	}
}

func TestFormatPorts(t *testing.T) {
	got := formatPorts(map[string]int{"smtp": 1025, "http": 8025})
	if want := "http://127.0.0.1:8025, smtp 127.0.0.1:1025"; got != want {
		t.Errorf("formatPorts = %q, want %q", got, want)
	}
}
