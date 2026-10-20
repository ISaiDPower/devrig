// Package buildinfo describes the running DevRig binary.
package buildinfo

import (
	"runtime"
	"runtime/debug"
)

// Version is the release version. Release builds set it with
// -ldflags "-X github.com/ISaiDPower/devrig/internal/buildinfo.Version=v1.2.3".
// Otherwise it falls back to the module version the Go toolchain stamped in.
var Version = "dev"

// Info describes how the running binary was built.
type Info struct {
	Version   string
	Commit    string
	Modified  bool
	GoVersion string
	Platform  string
}

// Read returns build information for the running binary.
func Read() Info {
	info := Info{
		Version:   Version,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}

	if info.Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		info.Version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Commit = s.Value
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}

	return info
}
