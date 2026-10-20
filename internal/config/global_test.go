package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestLoadGlobal(t *testing.T) {
	g, err := LoadGlobal(write(t, "[services.nginx.ports]\nhttp = 8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Services["nginx"].Ports["http"]; got != 8080 {
		t.Errorf("nginx http port = %d, want 8080", got)
	}
}

func TestLoadGlobalMissingFile(t *testing.T) {
	g, err := LoadGlobal(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Services) != 0 {
		t.Errorf("Services = %v, want none", g.Services)
	}
}

func TestLoadGlobalRejects(t *testing.T) {
	for name, content := range map[string]string{
		"unknown key":  "[services.nginx]\nport = 8080\n",
		"invalid port": "[services.nginx.ports]\nhttp = 70000\n",
		"bad syntax":   "[services.nginx.ports\n",
	} {
		if _, err := LoadGlobal(write(t, content)); err == nil {
			t.Errorf("%s: LoadGlobal succeeded, want an error", name)
		}
	}
}
