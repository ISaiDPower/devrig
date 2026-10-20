package home

import (
	"path/filepath"
	"testing"
)

func TestDefaultHonoursEnvVar(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvVar, root)

	d, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if d.Root != root {
		t.Errorf("Root = %q, want %q", d.Root, root)
	}
}

func TestDefaultMakesEnvVarAbsolute(t *testing.T) {
	t.Setenv(EnvVar, "relative")

	d, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(d.Root) {
		t.Errorf("Root = %q, want an absolute path", d.Root)
	}
}

func TestLayout(t *testing.T) {
	d := Dir{Root: filepath.FromSlash("/devrig")}

	want := filepath.FromSlash("/devrig/packages/php/8.3.14")
	if got := d.Release("php", "8.3.14"); got != want {
		t.Errorf("Release = %q, want %q", got, want)
	}
	want = filepath.FromSlash("/devrig/downloads")
	if got := d.Downloads(); got != want {
		t.Errorf("Downloads = %q, want %q", got, want)
	}
}
