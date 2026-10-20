package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ISaiDPower/devrig/internal/home"
	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/internal/service"
	"github.com/ISaiDPower/devrig/internal/service/servicetest"
)

func TestMain(m *testing.M) {
	servicetest.Main()
	os.Exit(m.Run())
}

const fakeRecipe = `
name = "fake"
kind = "service"

[service]
command = "fake"
args    = ["listen", "{{.Ports.http}}", "{{.State}}"]
stop    = ["stop", "{{.State}}"]
ready   = "http"

[service.ports]
http = 1
`

type fixture struct {
	home   home.Dir
	client *Client
	port   int
	served chan error
	cancel context.CancelFunc
}

// newFixture runs an agent for a fresh home in which release 1.0.0 of the
// fake service is installed. Its port comes from config.toml.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	h := home.Dir{Root: t.TempDir()}
	versions := `{"1.0.0": {"` + registry.Platform() + `": {"url": "https://example.com/fake.zip", "sha256": "` + strings.Repeat("a", 64) + `", "size": 1}}}`
	reg, err := registry.Load(fstest.MapFS{
		"fake/recipe.toml":   {Data: []byte(fakeRecipe)},
		"fake/versions.json": {Data: []byte(versions)},
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
	f := &fixture{home: h, client: NewClient(h), port: port, served: make(chan error, 1), cancel: cancel}
	go func() { f.served <- NewServer(h, reg).Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-f.served
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := f.client.Status(context.Background()); err == nil {
			return f
		} else if time.Now().After(deadline) {
			t.Fatalf("agent did not come up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func accepts(port int) bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
	if err == nil {
		conn.Close()
	}
	return err == nil
}

func TestStatusOfIdleAgent(t *testing.T) {
	f := newFixture(t)

	st, err := f.client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.PID != os.Getpid() || st.Home != f.home.Root || len(st.Services) != 0 {
		t.Errorf("status = %+v", st)
	}
}

func TestStartStop(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	st, err := f.client.Start(ctx, "fake", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != service.StateRunning || st.Ports["http"] != f.port {
		t.Errorf("start returned %+v, want running on the port from config.toml", st)
	}
	if !accepts(f.port) {
		t.Error("the service does not accept connections")
	}

	again, err := f.client.Start(ctx, "fake", "1.0.0")
	if err != nil || again.PID != st.PID {
		t.Errorf("starting a running release again = %+v, %v; want the same process", again, err)
	}

	status, err := f.client.Status(ctx)
	if err != nil || len(status.Services) != 1 || status.Services[0].Name != "fake" {
		t.Errorf("status = %+v, %v; want the fake service", status, err)
	}

	st, err = f.client.Stop(ctx, "fake")
	if err != nil || st.State != service.StateStopped {
		t.Errorf("stop = %+v, %v", st, err)
	}
	if accepts(f.port) {
		t.Error("the service still accepts connections after stop")
	}
	if _, err := f.client.Stop(ctx, "fake"); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("stopping a stopped service: error = %v, want not running", err)
	}
}

func TestRestart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.client.Start(ctx, "fake", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.client.Restart(ctx, "fake")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != service.StateRunning || st.PID == first.PID {
		t.Errorf("restart = %+v, want a new running process", st)
	}
}

func TestStartErrors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for _, tt := range []struct{ name, version, want string }{
		{"fake", "2.0.0", "not installed"},
		{"nope", "1.0.0", "unknown package"},
		{"fake", "x", "invalid version"},
	} {
		if _, err := f.client.Start(ctx, tt.name, tt.version); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("start %s@%s: error = %v, want %q", tt.name, tt.version, err, tt.want)
		}
	}
}

func TestShutdown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.client.Start(ctx, "fake", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-f.served:
		f.served <- err // for the cleanup
		if err != nil {
			t.Errorf("Serve returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after shutdown")
	}
	if accepts(f.port) {
		t.Error("the service survived the agent's shutdown")
	}
	if _, err := f.client.Status(ctx); !errors.Is(err, ErrNotRunning) {
		t.Errorf("status after shutdown: error = %v, want ErrNotRunning", err)
	}
}

func TestOneAgentPerHome(t *testing.T) {
	f := newFixture(t)

	err := NewServer(f.home, nil).Serve(context.Background())
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second agent: error = %v, want ErrAlreadyRunning", err)
	}
}

func TestClientWithoutAgent(t *testing.T) {
	c := NewClient(home.Dir{Root: t.TempDir()})
	if _, err := c.Status(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("error = %v, want ErrNotRunning", err)
	}
}
