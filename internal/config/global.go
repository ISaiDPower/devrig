// Package config reads DevRig configuration files.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// Global is the machine wide configuration kept in config.toml in the DevRig
// home directory, for example:
//
//	[services.nginx.ports]
//	http = 8080
type Global struct {
	Services map[string]ServiceSettings `toml:"services"`
}

// ServiceSettings overrides the defaults of one service.
type ServiceSettings struct {
	// Ports replaces the recipe's default ports, by port name.
	Ports map[string]int `toml:"ports"`
}

// LoadGlobal reads file. A missing file is the same as an empty one.
func LoadGlobal(file string) (Global, error) {
	var g Global

	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return g, nil
	}
	if err != nil {
		return g, err
	}

	md, err := toml.Decode(string(data), &g)
	if err != nil {
		return g, fmt.Errorf("%s: %w", file, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return g, fmt.Errorf("%s: unknown key %q", file, undecoded[0].String())
	}
	for name, s := range g.Services {
		for port, n := range s.Ports {
			if n < 1 || n > 65535 {
				return g, fmt.Errorf("%s: services.%s.ports.%s: invalid port %d", file, name, port, n)
			}
		}
	}
	return g, nil
}
