package registry

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

const demoRecipe = `
name     = "demo"
kind     = "runtime"
homepage = "https://example.com"
bin      = ["demo"]

[install]
strip = 1
`

func asset(name string) string {
	return `{"url": "https://example.com/` + name + `.zip", "sha256": "` + strings.Repeat("a", 64) + `", "size": 10}`
}

var demoVersions = `{
  "8.3.14": {"windows/amd64": ` + asset("a") + `},
  "8.3.9":  {"windows/amd64": ` + asset("b") + `, "linux/amd64": ` + asset("c") + `},
  "8.10.0": {"windows/amd64": ` + asset("d") + `},
  "7.4.33": {"windows/amd64": ` + asset("e") + `}
}`

func load(t *testing.T, recipe, versions string) (*Registry, error) {
	t.Helper()
	return Load(fstest.MapFS{
		"demo/recipe.toml":   {Data: []byte(recipe)},
		"demo/versions.json": {Data: []byte(versions)},
	})
}

func demo(t *testing.T) *Package {
	t.Helper()
	r, err := load(t, demoRecipe, demoVersions)
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Package("demo")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	p := demo(t)
	if p.Kind != KindRuntime || p.Strip != 1 || len(p.Bin) != 1 {
		t.Errorf("unexpected package %+v", p)
	}
}

func TestResolve(t *testing.T) {
	p := demo(t)
	tests := []struct {
		constraint, platform, want string
	}{
		{"", "windows/amd64", "8.10.0"},
		{"latest", "windows/amd64", "8.10.0"},
		{"8", "windows/amd64", "8.10.0"},
		{"8.3", "windows/amd64", "8.3.14"},
		{"8.3.9", "windows/amd64", "8.3.9"},
		{"7", "windows/amd64", "7.4.33"},
		{"8", "linux/amd64", "8.3.9"},
	}
	for _, tt := range tests {
		rel, asset, err := p.Resolve(tt.constraint, tt.platform)
		if err != nil {
			t.Errorf("Resolve(%q, %q): %v", tt.constraint, tt.platform, err)
			continue
		}
		if rel.Version.String() != tt.want {
			t.Errorf("Resolve(%q, %q) = %s, want %s", tt.constraint, tt.platform, rel.Version, tt.want)
		}
		if asset != rel.Assets[tt.platform] {
			t.Errorf("Resolve(%q, %q) returned the asset of another platform", tt.constraint, tt.platform)
		}
	}
}

func TestResolveNoMatch(t *testing.T) {
	p := demo(t)
	for _, c := range []string{"9", "8.4", "8.3.1"} {
		if _, _, err := p.Resolve(c, "windows/amd64"); !errors.Is(err, ErrNoRelease) {
			t.Errorf("Resolve(%q) error = %v, want ErrNoRelease", c, err)
		}
	}
	if _, _, err := p.Resolve("", "darwin/arm64"); !errors.Is(err, ErrNoRelease) {
		t.Errorf("Resolve on a platform without assets: error = %v, want ErrNoRelease", err)
	}
	if _, _, err := p.Resolve("eight", "windows/amd64"); err == nil {
		t.Error("Resolve with an invalid constraint should fail")
	}
}

func TestReleasesFiltersByPlatform(t *testing.T) {
	p := demo(t)
	if got := len(p.Releases("windows/amd64")); got != 4 {
		t.Errorf("windows releases = %d, want 4", got)
	}
	linux := p.Releases("linux/amd64")
	if len(linux) != 1 || linux[0].Version.String() != "8.3.9" {
		t.Errorf("linux releases = %v, want only 8.3.9", linux)
	}
}

func TestUnknownPackage(t *testing.T) {
	r, err := load(t, demoRecipe, demoVersions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Package("nope"); !errors.Is(err, ErrUnknownPackage) {
		t.Errorf("error = %v, want ErrUnknownPackage", err)
	}
}

func TestLoadRejectsBadRecipes(t *testing.T) {
	tests := map[string]string{
		"unknown key":    demoRecipe + "\nextra = true\n",
		"wrong name":     strings.Replace(demoRecipe, `"demo"`, `"other"`, 1),
		"invalid kind":   strings.Replace(demoRecipe, `"runtime"`, `"daemon"`, 1),
		"negative strip": strings.Replace(demoRecipe, "strip = 1", "strip = -1", 1),
	}
	for name, recipe := range tests {
		if _, err := load(t, recipe, demoVersions); err == nil {
			t.Errorf("%s: Load succeeded, want an error", name)
		}
	}
}

func TestLoadRejectsBadVersions(t *testing.T) {
	sum := strings.Repeat("a", 64)
	tests := map[string]string{
		"bad version": `{"8.3.x": {"windows/amd64": ` + asset("a") + `}}`,
		"plain http":  `{"8.3.1": {"windows/amd64": {"url": "http://example.com/a.zip", "sha256": "` + sum + `", "size": 1}}}`,
		"bad sha256":  `{"8.3.1": {"windows/amd64": {"url": "https://example.com/a.zip", "sha256": "abc", "size": 1}}}`,
		"zero size":   `{"8.3.1": {"windows/amd64": {"url": "https://example.com/a.zip", "sha256": "` + sum + `", "size": 0}}}`,
		"extra field": `{"8.3.1": {"windows/amd64": {"url": "https://example.com/a.zip", "sha256": "` + sum + `", "size": 1, "md5": ""}}}`,
	}
	for name, versions := range tests {
		if _, err := load(t, demoRecipe, versions); err == nil {
			t.Errorf("%s: Load succeeded, want an error", name)
		}
	}
}

const serviceRecipe = `
name = "demo"
kind = "service"

[service]
command   = "bin/demo"
args      = ["--port", "{{.Ports.http}}"]
ready     = "http"
dirs      = ["logs"]
templates = { "conf/demo.conf" = "demo.conf.tmpl" }

[service.ports]
http = 8080
`

func loadServiceRecipe(t *testing.T, recipe string, extra fstest.MapFS) (*Registry, error) {
	t.Helper()
	fsys := fstest.MapFS{
		"demo/recipe.toml":    {Data: []byte(recipe)},
		"demo/versions.json":  {Data: []byte(demoVersions)},
		"demo/demo.conf.tmpl": {Data: []byte("listen {{.Ports.http}}\n")},
	}
	for name, file := range extra {
		fsys[name] = file
	}
	return Load(fsys)
}

func TestLoadService(t *testing.T) {
	r, err := loadServiceRecipe(t, serviceRecipe, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := r.Package("demo")
	s := p.Service
	if s == nil {
		t.Fatal("Service is nil")
	}
	if s.Command != "bin/demo" || s.Ports["http"] != 8080 || s.Ready != "http" {
		t.Errorf("unexpected service %+v", s)
	}
	if got := s.Templates["conf/demo.conf"]; got != "listen {{.Ports.http}}\n" {
		t.Errorf("template = %q", got)
	}
}

func TestLoadServiceRejects(t *testing.T) {
	tests := map[string]struct {
		recipe string
		extra  fstest.MapFS
	}{
		"service without table": {recipe: "name = \"demo\"\nkind = \"service\"\n"},
		"runtime with table":    {recipe: strings.Replace(serviceRecipe, `"service"`, `"runtime"`, 1)},
		"no command":            {recipe: strings.Replace(serviceRecipe, `"bin/demo"`, `""`, 1)},
		"command outside":       {recipe: strings.Replace(serviceRecipe, `"bin/demo"`, `"../demo"`, 1)},
		"ready not a port":      {recipe: strings.Replace(serviceRecipe, `ready     = "http"`, `ready = "smtp"`, 1)},
		"invalid port":          {recipe: strings.Replace(serviceRecipe, "http = 8080", "http = 0", 1)},
		"dir outside":           {recipe: strings.Replace(serviceRecipe, `["logs"]`, `["../logs"]`, 1)},
		"missing template":      {recipe: strings.Replace(serviceRecipe, "demo.conf.tmpl", "nope.tmpl", 1)},
		"bad template":          {recipe: serviceRecipe, extra: fstest.MapFS{"demo/demo.conf.tmpl": {Data: []byte("{{.Ports")}}},
		"bad arg template":      {recipe: strings.Replace(serviceRecipe, "{{.Ports.http}}", "{{.Ports.http", 1)},
	}
	for name, tt := range tests {
		if _, err := loadServiceRecipe(t, tt.recipe, tt.extra); err == nil {
			t.Errorf("%s: Load succeeded, want an error", name)
		}
	}
}

func TestMarshalVersionsRoundTrip(t *testing.T) {
	releases, err := ParseVersions([]byte(demoVersions))
	if err != nil {
		t.Fatal(err)
	}

	data, err := MarshalVersions(releases)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseVersions(data)
	if err != nil {
		t.Fatalf("ParseVersions(MarshalVersions(...)): %v\n%s", err, data)
	}
	if len(again) != len(releases) {
		t.Fatalf("round trip kept %d releases, want %d", len(again), len(releases))
	}

	// Newest first, compared as numbers: 8.10.0 before 8.3.14.
	text := string(data)
	if strings.Index(text, `"8.10.0"`) > strings.Index(text, `"8.3.14"`) {
		t.Errorf("releases are not ordered newest first:\n%s", text)
	}
}
