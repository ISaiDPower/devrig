package cli

import (
	"fmt"
	"strings"

	"github.com/ISaiDPower/devrig/internal/buildinfo"
	"github.com/ISaiDPower/devrig/internal/home"
	"github.com/ISaiDPower/devrig/internal/install"
	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/recipes"
)

// app is what the package commands work with.
type app struct {
	registry  *registry.Registry
	installer *install.Installer
	platform  string
}

// loader builds the app lazily, so commands such as version never touch the
// disk.
type loader func() (*app, error)

// loadApp builds the app for this machine from the built-in recipes.
func loadApp() (*app, error) {
	h, err := home.Default()
	if err != nil {
		return nil, fmt.Errorf("locate the DevRig home directory: %w", err)
	}
	reg, err := registry.Load(recipes.FS)
	if err != nil {
		return nil, fmt.Errorf("load recipes: %w", err)
	}

	return &app{
		registry: reg,
		installer: &install.Installer{
			Home:      h,
			UserAgent: "devrig/" + buildinfo.Read().Version + " (+https://github.com/ISaiDPower/devrig)",
		},
		platform: registry.Platform(),
	}, nil
}

// parseSpec splits "php@8.3" into a package name and a version constraint.
func parseSpec(spec string) (name, constraint string) {
	name, constraint, _ = strings.Cut(spec, "@")
	return name, constraint
}

// formatSize formats a byte count with decimal units, such as "33.9 MB".
func formatSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}
