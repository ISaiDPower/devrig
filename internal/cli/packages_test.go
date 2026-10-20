package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ISaiDPower/devrig/internal/home"
	"github.com/ISaiDPower/devrig/internal/install"
	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/internal/version"
)

// testApp serves a demo package with releases 1.0.0, 1.1.0 and 2.0.0 for
// the current platform over HTTPS.
func testApp(t *testing.T) *app {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("demo.exe")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("demo"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := buf.Bytes()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	t.Cleanup(srv.Close)

	sum := sha256.Sum256(archive)
	var versions []string
	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		versions = append(versions, fmt.Sprintf(`%q: {%q: {"url": "%s/demo-%s.zip", "sha256": %q, "size": %d}}`,
			v, registry.Platform(), srv.URL, v, hex.EncodeToString(sum[:]), len(archive)))
	}

	reg, err := registry.Load(fstest.MapFS{
		"demo/recipe.toml":   {Data: []byte(`name = "demo"` + "\n" + `kind = "tool"`)},
		"demo/versions.json": {Data: []byte("{" + strings.Join(versions, ",") + "}")},
	})
	if err != nil {
		t.Fatal(err)
	}

	return &app{
		registry:  reg,
		installer: &install.Installer{Home: home.Dir{Root: t.TempDir()}, Client: srv.Client()},
		platform:  registry.Platform(),
	}
}

// run executes rig with args against a and returns its standard output.
func run(t *testing.T, a *app, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd(func() (*app, error) { return a, nil })
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func mustVersion(t *testing.T, s string) version.Version {
	t.Helper()
	v, err := version.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func mustRun(t *testing.T, a *app, args ...string) string {
	t.Helper()
	out, err := run(t, a, args...)
	if err != nil {
		t.Fatalf("rig %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// hasRow reports whether a table printed by rig has a row whose columns are
// the space separated fields of want.
func hasRow(table, want string) bool {
	for _, line := range strings.Split(table, "\n") {
		if strings.Join(strings.Fields(line), " ") == want {
			return true
		}
	}
	return false
}

func TestInstallListUninstall(t *testing.T) {
	a := testApp(t)

	if out := mustRun(t, a, "list"); !strings.Contains(out, "Nothing installed") {
		t.Errorf("list before installing:\n%s", out)
	}

	out := mustRun(t, a, "install", "demo@1")
	if !strings.Contains(out, "Installed demo 1.1.0") {
		t.Errorf("install demo@1 should pick 1.1.0:\n%s", out)
	}
	exe := filepath.Join(a.installer.Home.Release("demo", "1.1.0"), "demo.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Errorf("demo.exe not unpacked: %v", err)
	}

	if out := mustRun(t, a, "install", "demo@1.1"); !strings.Contains(out, "already installed") {
		t.Errorf("second install should be a no-op:\n%s", out)
	}

	mustRun(t, a, "install", "demo")
	out = mustRun(t, a, "list")
	if !strings.Contains(out, "2.0.0") || !strings.Contains(out, "1.1.0") || strings.Contains(out, "1.0.0") {
		t.Errorf("list should show 2.0.0 and 1.1.0 only:\n%s", out)
	}

	out = mustRun(t, a, "list", "--available")
	for _, want := range []string{"demo 2.0.0 installed", "demo 1.1.0 installed", "demo 1.0.0"} {
		if !hasRow(out, want) {
			t.Errorf("list --available is missing the row %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimRight(line, " ") != line {
			t.Errorf("trailing spaces in %q", line)
		}
	}

	if out := mustRun(t, a, "uninstall", "demo@2"); !strings.Contains(out, "Removed demo 2.0.0") {
		t.Errorf("uninstall demo@2:\n%s", out)
	}
	if a.installer.IsInstalled("demo", mustVersion(t, "2.0.0")) {
		t.Error("2.0.0 still installed")
	}
}

func TestUninstallNeedsAnUnambiguousVersion(t *testing.T) {
	a := testApp(t)
	mustRun(t, a, "install", "demo@1.0", "demo@1.1")

	for _, args := range [][]string{
		{"uninstall", "demo"},
		{"uninstall", "demo@1"},
		{"uninstall", "demo@3"},
	} {
		if _, err := run(t, a, args...); err == nil {
			t.Errorf("rig %s succeeded, want an error", strings.Join(args, " "))
		}
	}
}

func TestInstallErrors(t *testing.T) {
	a := testApp(t)
	for _, spec := range []string{"nope", "demo@3", "demo@x"} {
		if _, err := run(t, a, "install", spec); err == nil {
			t.Errorf("rig install %s succeeded, want an error", spec)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := map[int64]string{
		999:      "999 B",
		1000:     "1.0 kB",
		33873909: "33.9 MB",
		2e9:      "2.0 GB",
	}
	for n, want := range tests {
		if got := formatSize(n); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", n, got, want)
		}
	}
}
