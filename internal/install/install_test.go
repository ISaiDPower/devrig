package install

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ISaiDPower/devrig/internal/home"
	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/internal/version"
)

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type fixture struct {
	installer *Installer
	pkg       *registry.Package
	rel       registry.Release
	asset     registry.Asset
	requests  *atomic.Int32
}

// newFixture serves archive over HTTPS and describes it as demo 1.2.3.
func newFixture(t *testing.T, archive []byte, strip int) *fixture {
	t.Helper()

	var requests atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write(archive)
	}))
	t.Cleanup(srv.Close)

	sum := sha256.Sum256(archive)
	v, err := version.Parse("1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	return &fixture{
		installer: &Installer{Home: home.Dir{Root: t.TempDir()}, Client: srv.Client()},
		pkg:       &registry.Package{Name: "demo", Strip: strip},
		rel:       registry.Release{Version: v},
		asset: registry.Asset{
			URL:    srv.URL + "/demo-1.2.3.zip",
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(archive)),
		},
		requests: &requests,
	}
}

func (f *fixture) install(t *testing.T) (string, error) {
	t.Helper()
	return f.installer.Install(context.Background(), f.pkg, f.rel, f.asset)
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInstall(t *testing.T) {
	f := newFixture(t, makeZip(t, map[string]string{
		"demo-1.2.3/bin/demo.exe": "binary",
		"demo-1.2.3/README":       "readme",
	}), 1)

	dir, err := f.install(t)
	if err != nil {
		t.Fatal(err)
	}
	if want := f.installer.Home.Release("demo", "1.2.3"); dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
	if got := readFile(t, filepath.Join(dir, "bin", "demo.exe")); got != "binary" {
		t.Errorf("bin/demo.exe = %q", got)
	}
	if !f.installer.IsInstalled("demo", f.rel.Version) {
		t.Error("IsInstalled = false after Install")
	}

	// Nothing but the release itself is left in the package directory.
	entries, err := os.ReadDir(f.installer.Home.Package("demo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("package directory holds %d entries, want 1", len(entries))
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	f := newFixture(t, makeZip(t, map[string]string{"demo": "x"}), 0)

	for range 2 {
		if _, err := f.install(t); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.requests.Load(); got != 1 {
		t.Errorf("downloaded %d times, want 1", got)
	}
}

func TestReinstallUsesCache(t *testing.T) {
	f := newFixture(t, makeZip(t, map[string]string{"demo": "x"}), 0)

	if _, err := f.install(t); err != nil {
		t.Fatal(err)
	}
	if err := f.installer.Uninstall("demo", f.rel.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := f.install(t); err != nil {
		t.Fatal(err)
	}
	if got := f.requests.Load(); got != 1 {
		t.Errorf("downloaded %d times, want 1", got)
	}
}

func TestInstallRejectsChecksumMismatch(t *testing.T) {
	f := newFixture(t, makeZip(t, map[string]string{"demo": "x"}), 0)
	f.asset.SHA256 = strings.Repeat("0", 64)

	_, err := f.install(t)
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("error = %v, want a sha256 mismatch", err)
	}
	if f.installer.IsInstalled("demo", f.rel.Version) {
		t.Error("release installed despite the checksum mismatch")
	}
	entries, _ := os.ReadDir(f.installer.Home.Downloads())
	if len(entries) != 0 {
		t.Errorf("download cache holds %d files after a failed download, want 0", len(entries))
	}
}

func TestInstallRejectsWrongSize(t *testing.T) {
	f := newFixture(t, makeZip(t, map[string]string{"demo": "x"}), 0)
	f.asset.Size--

	if _, err := f.install(t); err == nil {
		t.Fatal("Install succeeded although the download is larger than expected")
	}
}

func TestInstallRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"../evil.txt", "a/../../evil.txt", "/abs/evil.txt"} {
		f := newFixture(t, makeZip(t, map[string]string{name: "evil"}), 0)

		if _, err := f.install(t); err == nil {
			t.Errorf("%s: Install succeeded, want an error", name)
		}
		if f.installer.IsInstalled("demo", f.rel.Version) {
			t.Errorf("%s: release installed", name)
		}
		if _, err := os.Stat(filepath.Join(f.installer.Home.Root, "evil.txt")); err == nil {
			t.Errorf("%s: file written outside the release", name)
		}
	}
}

func TestInstallRejectsUnsupportedFormat(t *testing.T) {
	f := newFixture(t, makeZip(t, map[string]string{"demo": "x"}), 0)
	f.asset.URL = strings.TrimSuffix(f.asset.URL, ".zip") + ".tar.xz"

	if _, err := f.install(t); err == nil {
		t.Fatal("Install succeeded for a .tar.xz asset")
	}
	if got := f.requests.Load(); got != 0 {
		t.Errorf("downloaded %d times before rejecting the format, want 0", got)
	}
}

func TestProgress(t *testing.T) {
	f := newFixture(t, makeZip(t, map[string]string{"demo": "x"}), 0)
	var last, total int64
	f.installer.Progress = func(d, tot int64) { last, total = d, tot }

	if _, err := f.install(t); err != nil {
		t.Fatal(err)
	}
	if last != f.asset.Size || total != f.asset.Size {
		t.Errorf("last progress = %d/%d, want %d/%d", last, total, f.asset.Size, f.asset.Size)
	}
}

func TestUninstall(t *testing.T) {
	f := newFixture(t, makeZip(t, map[string]string{"demo": "x"}), 0)
	if _, err := f.install(t); err != nil {
		t.Fatal(err)
	}

	if err := f.installer.Uninstall("demo", f.rel.Version); err != nil {
		t.Fatal(err)
	}
	if f.installer.IsInstalled("demo", f.rel.Version) {
		t.Error("still installed after Uninstall")
	}
	if err := f.installer.Uninstall("demo", f.rel.Version); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("second Uninstall error = %v, want ErrNotInstalled", err)
	}
}

func TestInstalled(t *testing.T) {
	in := &Installer{Home: home.Dir{Root: t.TempDir()}}
	for _, dir := range []string{"8.3.9", "8.10.1", "8.3.14", ".tmp-123", ".trash-8.2.0-1", "notes"} {
		if err := os.MkdirAll(in.Home.Release("php", dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	vs, err := in.Installed("php")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.String())
	}
	if want := "8.10.1 8.3.14 8.3.9"; strings.Join(got, " ") != want {
		t.Errorf("Installed = %v, want %s", got, want)
	}

	names, err := in.Packages()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "php" {
		t.Errorf("Packages = %v, want [php]", names)
	}
}

func TestInstalledWithoutHome(t *testing.T) {
	in := &Installer{Home: home.Dir{Root: filepath.Join(t.TempDir(), "missing")}}

	if vs, err := in.Installed("php"); err != nil || len(vs) != 0 {
		t.Errorf("Installed = %v, %v; want nothing and no error", vs, err)
	}
	if names, err := in.Packages(); err != nil || len(names) != 0 {
		t.Errorf("Packages = %v, %v; want nothing and no error", names, err)
	}
}
