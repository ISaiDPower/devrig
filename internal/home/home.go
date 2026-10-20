// Package home locates the directories DevRig owns on this machine.
package home

import (
	"os"
	"path/filepath"
	"runtime"
)

// EnvVar names the environment variable that overrides the default root.
const EnvVar = "DEVRIG_HOME"

// Dir is the root directory DevRig keeps its packages and caches in.
type Dir struct {
	Root string
}

// Default returns the root named by DEVRIG_HOME, or the platform default:
// %LOCALAPPDATA%\DevRig on Windows, ~/Library/Application Support/DevRig on
// macOS and $XDG_DATA_HOME/devrig (~/.local/share/devrig) elsewhere.
func Default() (Dir, error) {
	if root := os.Getenv(EnvVar); root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return Dir{}, err
		}
		return Dir{Root: abs}, nil
	}

	switch runtime.GOOS {
	case "windows":
		// UserCacheDir is %LOCALAPPDATA% on Windows.
		base, err := os.UserCacheDir()
		if err != nil {
			return Dir{}, err
		}
		return Dir{Root: filepath.Join(base, "DevRig")}, nil
	case "darwin":
		// UserConfigDir is ~/Library/Application Support on macOS.
		base, err := os.UserConfigDir()
		if err != nil {
			return Dir{}, err
		}
		return Dir{Root: filepath.Join(base, "DevRig")}, nil
	default:
		if base := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(base) {
			return Dir{Root: filepath.Join(base, "devrig")}, nil
		}
		base, err := os.UserHomeDir()
		if err != nil {
			return Dir{}, err
		}
		return Dir{Root: filepath.Join(base, ".local", "share", "devrig")}, nil
	}
}

// Packages holds one directory per package.
func (d Dir) Packages() string {
	return filepath.Join(d.Root, "packages")
}

// Package holds the installed releases of one package.
func (d Dir) Package(name string) string {
	return filepath.Join(d.Packages(), name)
}

// Release is the directory one release of a package is unpacked into.
func (d Dir) Release(name, version string) string {
	return filepath.Join(d.Package(name), version)
}

// Downloads caches downloaded archives, named after their sha256.
func (d Dir) Downloads() string {
	return filepath.Join(d.Root, "downloads")
}

// Service holds the generated config, logs and temporary files of one
// service release.
func (d Dir) Service(name, version string) string {
	return filepath.Join(d.Root, "services", name, version)
}

// Data holds the data of a service, such as a database, for one major
// version. It is kept when releases are upgraded or removed.
func (d Dir) Data(name, major string) string {
	return filepath.Join(d.Root, "data", name, major)
}

// Config is the machine wide DevRig configuration file.
func (d Dir) Config() string {
	return filepath.Join(d.Root, "config.toml")
}

// AgentLog is where the background agent writes its own log.
func (d Dir) AgentLog() string {
	return filepath.Join(d.Root, "agent.log")
}

// AgentSocket is the Unix socket the agent listens on outside Windows.
func (d Dir) AgentSocket() string {
	return filepath.Join(d.Root, "agent.sock")
}
