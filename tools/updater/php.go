package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/ISaiDPower/devrig/internal/version"
)

const (
	// phpReleasesURL lists the newest Windows build of every PHP minor
	// version, with checksums.
	phpReleasesURL = "https://downloads.php.net/~windows/releases/releases.json"
	// phpArchiveURL keeps every Windows build, the newest ones included, so
	// its URLs stay valid after newer patch releases come out.
	phpArchiveURL = "https://downloads.php.net/~windows/releases/archives/"
)

// phpBuild is one build entry in releases.json.
type phpBuild struct {
	Zip struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"zip"`
}

// phpReleases returns the non thread safe x64 builds from releases.json.
// Upstream only publishes checksums for the newest patch release of each
// minor version, so older patch releases are only known once they have been
// seen here.
func phpReleases(ctx context.Context) ([]candidate, error) {
	var index phpIndex
	if err := getJSON(ctx, phpReleasesURL, &index); err != nil {
		return nil, err
	}
	return index.candidates()
}

// phpIndex is releases.json: build entries per minor version, next to a
// "version" key with the full version.
type phpIndex map[string]map[string]json.RawMessage

func (index phpIndex) candidates() ([]candidate, error) {
	var out []candidate
	for minor, entry := range index {
		var raw string
		if err := json.Unmarshal(entry["version"], &raw); err != nil {
			return nil, fmt.Errorf("php %s: version: %w", minor, err)
		}
		v, err := version.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("php %s: %w", minor, err)
		}

		for key, data := range entry {
			// Build keys look like "nts-vs17-x64" or "ts-vs17-x86".
			if !strings.HasPrefix(key, "nts-") || !strings.HasSuffix(key, "-x64") {
				continue
			}

			var b phpBuild
			if err := json.Unmarshal(data, &b); err != nil {
				return nil, fmt.Errorf("php %s %s: %w", v, key, err)
			}
			if b.Zip.Path == "" || path.Base(b.Zip.Path) != b.Zip.Path {
				return nil, fmt.Errorf("php %s %s: unexpected zip path %q", v, key, b.Zip.Path)
			}

			out = append(out, candidate{
				Version:  v,
				Platform: "windows/amd64",
				URL:      phpArchiveURL + b.Zip.Path,
				SHA256:   strings.ToLower(b.Zip.SHA256),
			})
		}
	}
	return out, nil
}
