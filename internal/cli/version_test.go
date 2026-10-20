package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	root := newRootCmd(loadApp)
	root.SetOut(&out)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("rig version: %v", err)
	}

	if !strings.HasPrefix(out.String(), "rig ") {
		t.Errorf("output should start with %q, got %q", "rig ", out.String())
	}
}

func TestVersionCommandRejectsArgs(t *testing.T) {
	root := newRootCmd(loadApp)
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"version", "extra"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected an error for unexpected arguments")
	}
}
