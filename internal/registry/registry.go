// Package registry loads package recipes and the releases they offer.
//
// Every package is a directory holding a hand written recipe.toml and a
// generated versions.json. See docs/design.md for the format.
package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"

	"github.com/ISaiDPower/devrig/internal/version"
)

// Kind says how a package is used.
type Kind string

const (
	KindRuntime Kind = "runtime" // language runtimes such as PHP or Node.js
	KindService Kind = "service" // long running processes such as Nginx
	KindTool    Kind = "tool"    // command line tools such as Composer
)

var (
	// ErrUnknownPackage is returned for a name without a recipe.
	ErrUnknownPackage = errors.New("unknown package")
	// ErrNoRelease is returned when no release matches a constraint.
	ErrNoRelease = errors.New("no matching release")
)

// namePattern keeps package names safe to use as directory names.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Platform returns the current operating system and architecture in the form
// assets are keyed by, such as "windows/amd64".
func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// Package is a recipe together with the releases it offers.
type Package struct {
	Name     string
	Kind     Kind
	Homepage string
	// Bin lists the executables the package provides, without extensions.
	Bin []string
	// Strip is how many leading path components to drop from archive entries.
	Strip int
	// Service describes how to run the package. It is set for services only.
	Service *Service

	releases []Release // newest first
}

// Service describes how to run a service package. Args, Stop and Templates
// are text/template templates. See the service package for the values they
// can use.
type Service struct {
	// Command is the executable to run, relative to the release directory and
	// without an extension.
	Command string
	// Args are the arguments to start the service with.
	Args []string
	// Stop, if set, are the arguments of a second invocation of Command that
	// asks the service to shut down cleanly.
	Stop []string
	// Ports are the default ports, by name.
	Ports map[string]int
	// Ready names the port to wait for before the service counts as running.
	Ready string
	// Dirs are directories to create in the service directory.
	Dirs []string
	// Templates maps files to generate in the service directory to their
	// template text.
	Templates map[string]string
}

// Releases returns the releases that have an asset for platform, newest
// first.
func (p *Package) Releases(platform string) []Release {
	var out []Release
	for _, rel := range p.releases {
		if _, ok := rel.Assets[platform]; ok {
			out = append(out, rel)
		}
	}
	return out
}

// Resolve returns the newest release for platform that matches constraint,
// together with its asset. The constraint is a version prefix such as "8" or
// "8.3". An empty constraint or "latest" matches every release.
func (p *Package) Resolve(constraint, platform string) (Release, Asset, error) {
	var prefix version.Version
	if constraint != "" && constraint != "latest" {
		v, err := version.Parse(constraint)
		if err != nil {
			return Release{}, Asset{}, err
		}
		prefix = v
	} else {
		constraint = "latest"
	}

	for _, rel := range p.releases {
		if asset, ok := rel.Assets[platform]; ok && rel.Version.HasPrefix(prefix) {
			return rel, asset, nil
		}
	}
	return Release{}, Asset{}, fmt.Errorf("%w for %s@%s on %s", ErrNoRelease, p.Name, constraint, platform)
}

// Registry is a set of packages.
type Registry struct {
	packages map[string]*Package
}

// Load reads every <name>/recipe.toml and its <name>/versions.json in fsys.
func Load(fsys fs.FS) (*Registry, error) {
	files, err := fs.Glob(fsys, "*/recipe.toml")
	if err != nil {
		return nil, err
	}

	r := &Registry{packages: make(map[string]*Package, len(files))}
	for _, file := range files {
		dir := path.Dir(file)
		p, err := loadPackage(fsys, dir)
		if err != nil {
			return nil, fmt.Errorf("recipe %s: %w", dir, err)
		}
		r.packages[p.Name] = p
	}
	return r, nil
}

// Package returns the package with the given name.
func (r *Registry) Package(name string) (*Package, error) {
	p, ok := r.packages[name]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownPackage, name)
	}
	return p, nil
}

// Packages returns every package, sorted by name.
func (r *Registry) Packages() []*Package {
	ps := slices.Collect(maps.Values(r.packages))
	slices.SortFunc(ps, func(a, b *Package) int { return strings.Compare(a.Name, b.Name) })
	return ps
}

type recipeFile struct {
	Name     string   `toml:"name"`
	Kind     Kind     `toml:"kind"`
	Homepage string   `toml:"homepage"`
	Bin      []string `toml:"bin"`
	Install  struct {
		Strip int `toml:"strip"`
	} `toml:"install"`
	Service *serviceFile `toml:"service"`
}

type serviceFile struct {
	Command   string            `toml:"command"`
	Args      []string          `toml:"args"`
	Stop      []string          `toml:"stop"`
	Ports     map[string]int    `toml:"ports"`
	Ready     string            `toml:"ready"`
	Dirs      []string          `toml:"dirs"`
	Templates map[string]string `toml:"templates"`
}

// loadService validates a [service] table and reads its template files from
// the recipe directory.
func loadService(fsys fs.FS, dir string, sf *serviceFile) (*Service, error) {
	if sf.Command == "" || !filepath.IsLocal(filepath.FromSlash(sf.Command)) {
		return nil, fmt.Errorf("service.command %q must be a path inside the release", sf.Command)
	}
	for name, port := range sf.Ports {
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("service.ports.%s: invalid port %d", name, port)
		}
	}
	if _, ok := sf.Ports[sf.Ready]; sf.Ready != "" && !ok {
		return nil, fmt.Errorf("service.ready %q is not one of the ports", sf.Ready)
	}
	for _, d := range sf.Dirs {
		if !filepath.IsLocal(filepath.FromSlash(d)) {
			return nil, fmt.Errorf("service.dirs: %q must be a relative path", d)
		}
	}

	s := &Service{
		Command:   sf.Command,
		Args:      sf.Args,
		Stop:      sf.Stop,
		Ports:     sf.Ports,
		Ready:     sf.Ready,
		Dirs:      sf.Dirs,
		Templates: make(map[string]string, len(sf.Templates)),
	}
	for dest, src := range sf.Templates {
		if !filepath.IsLocal(filepath.FromSlash(dest)) {
			return nil, fmt.Errorf("service.templates: %q must be a relative path", dest)
		}
		if path.Base(src) != src {
			return nil, fmt.Errorf("service.templates: %q must be a file in the recipe directory", src)
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, src))
		if err != nil {
			return nil, err
		}
		s.Templates[dest] = string(data)
	}

	// Parse every template now, so mistakes show up when recipes load rather
	// than when a service starts.
	texts := append(slices.Clone(s.Args), s.Stop...)
	for _, text := range s.Templates {
		texts = append(texts, text)
	}
	for _, text := range texts {
		if _, err := template.New("").Parse(text); err != nil {
			return nil, fmt.Errorf("service: %w", err)
		}
	}
	return s, nil
}

func loadPackage(fsys fs.FS, dir string) (*Package, error) {
	data, err := fs.ReadFile(fsys, path.Join(dir, "recipe.toml"))
	if err != nil {
		return nil, err
	}

	var rf recipeFile
	md, err := toml.Decode(string(data), &rf)
	if err != nil {
		return nil, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("unknown key %q", undecoded[0].String())
	}

	switch {
	case !namePattern.MatchString(rf.Name):
		return nil, fmt.Errorf("invalid name %q", rf.Name)
	case rf.Name != dir:
		return nil, fmt.Errorf("name %q does not match its directory", rf.Name)
	case rf.Kind != KindRuntime && rf.Kind != KindService && rf.Kind != KindTool:
		return nil, fmt.Errorf("invalid kind %q", rf.Kind)
	case rf.Install.Strip < 0:
		return nil, fmt.Errorf("invalid install.strip %d", rf.Install.Strip)
	case rf.Kind == KindService && rf.Service == nil:
		return nil, errors.New("a service needs a [service] table")
	case rf.Kind != KindService && rf.Service != nil:
		return nil, fmt.Errorf("a %s cannot have a [service] table", rf.Kind)
	}

	var svc *Service
	if rf.Service != nil {
		if svc, err = loadService(fsys, dir, rf.Service); err != nil {
			return nil, err
		}
	}

	data, err = fs.ReadFile(fsys, path.Join(dir, "versions.json"))
	if err != nil {
		return nil, err
	}
	releases, err := ParseVersions(data)
	if err != nil {
		return nil, fmt.Errorf("versions.json: %w", err)
	}

	return &Package{
		Name:     rf.Name,
		Kind:     rf.Kind,
		Homepage: rf.Homepage,
		Bin:      rf.Bin,
		Strip:    rf.Install.Strip,
		Service:  svc,
		releases: releases,
	}, nil
}
