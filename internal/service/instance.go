// Package service runs service releases. It renders their configuration,
// starts their processes, waits until they accept connections and restarts
// them when they crash.
package service

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"text/template"

	"github.com/ISaiDPower/devrig/internal/home"
	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/internal/version"
)

// Instance is one service release, ready to run on this machine.
type Instance struct {
	Name    string
	Version string
	// Exe is the executable to run.
	Exe string
	// Release is the installed release, State holds generated config, logs
	// and temporary files, and Data holds data kept across upgrades.
	Release, State, Data string
	// Ports are the ports to use, by name.
	Ports map[string]int
	Spec  *registry.Service
}

// NewInstance describes how to run release v of pkg. Ports override the
// recipe's defaults by name.
func NewInstance(h home.Dir, pkg *registry.Package, v version.Version, ports map[string]int) (*Instance, error) {
	if pkg.Service == nil {
		return nil, fmt.Errorf("%s is not a service", pkg.Name)
	}

	merged := maps.Clone(pkg.Service.Ports)
	for name, port := range ports {
		if _, ok := merged[name]; !ok {
			return nil, fmt.Errorf("%s has no port named %q", pkg.Name, name)
		}
		merged[name] = port
	}

	release := h.Release(pkg.Name, v.String())
	exe := filepath.Join(release, filepath.FromSlash(pkg.Service.Command))
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}

	return &Instance{
		Name:    pkg.Name,
		Version: v.String(),
		Exe:     exe,
		Release: release,
		State:   h.Service(pkg.Name, v.String()),
		Data:    h.Data(pkg.Name, v.Truncate(1).String()),
		Ports:   merged,
		Spec:    pkg.Service,
	}, nil
}

// LogFile is where the service's output goes.
func (i *Instance) LogFile() string {
	return filepath.Join(i.State, i.Name+".log")
}

// templateData is what recipe templates can use. Paths use forward slashes,
// which every service DevRig runs accepts on Windows too.
type templateData struct {
	Release, State, Data string
	Ports                map[string]int
}

func (i *Instance) templateData() templateData {
	return templateData{
		Release: filepath.ToSlash(i.Release),
		State:   filepath.ToSlash(i.State),
		Data:    filepath.ToSlash(i.Data),
		Ports:   i.Ports,
	}
}

func (i *Instance) render(text string) (string, error) {
	t, err := template.New(i.Name).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, i.templateData()); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// args renders a list of argument templates.
func (i *Instance) args(templates []string) ([]string, error) {
	out := make([]string, len(templates))
	for n, text := range templates {
		arg, err := i.render(text)
		if err != nil {
			return nil, fmt.Errorf("%s: argument %d: %w", i.Name, n+1, err)
		}
		out[n] = arg
	}
	return out, nil
}

// Prepare creates the instance's directories and writes its generated
// configuration files, replacing earlier ones.
func (i *Instance) Prepare() error {
	dirs := []string{i.State, i.Data}
	for _, d := range i.Spec.Dirs {
		dirs = append(dirs, filepath.Join(i.State, filepath.FromSlash(d)))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	for dest, text := range i.Spec.Templates {
		content, err := i.render(text)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", i.Name, dest, err)
		}
		file := filepath.Join(i.State, filepath.FromSlash(dest))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
