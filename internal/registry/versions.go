package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"

	"github.com/ISaiDPower/devrig/internal/version"
)

// Release is one version of a package.
type Release struct {
	Version version.Version
	// Assets maps a platform such as "windows/amd64" to its archive.
	Assets map[string]Asset
}

// Asset is a downloadable archive for one platform.
type Asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (a Asset) validate() error {
	if u, err := url.Parse(a.URL); err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("url %q is not an absolute https URL", a.URL)
	}
	if !sha256Pattern.MatchString(a.SHA256) {
		return fmt.Errorf("invalid sha256 %q", a.SHA256)
	}
	if a.Size <= 0 {
		return fmt.Errorf("invalid size %d", a.Size)
	}
	return nil
}

// ParseVersions decodes a versions.json document. Releases come back newest
// first.
func ParseVersions(data []byte) ([]Release, error) {
	var raw map[string]map[string]Asset
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}

	releases := make([]Release, 0, len(raw))
	for v, assets := range raw {
		ver, err := version.Parse(v)
		if err != nil {
			return nil, err
		}
		for platform, asset := range assets {
			if err := asset.validate(); err != nil {
				return nil, fmt.Errorf("%s %s: %w", v, platform, err)
			}
		}
		releases = append(releases, Release{Version: ver, Assets: assets})
	}

	sortNewestFirst(releases)
	return releases, nil
}

// MarshalVersions encodes releases as a versions.json document, newest
// first so that diffs stay readable.
func MarshalVersions(releases []Release) ([]byte, error) {
	sorted := slices.Clone(releases)
	sortNewestFirst(sorted)

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, rel := range sorted {
		var assets bytes.Buffer
		enc := json.NewEncoder(&assets)
		enc.SetEscapeHTML(false)
		enc.SetIndent("  ", "  ")
		if err := enc.Encode(rel.Assets); err != nil {
			return nil, err
		}

		fmt.Fprintf(&buf, "  %q: %s", rel.Version, bytes.TrimRight(assets.Bytes(), "\n"))
		if i < len(sorted)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")

	return buf.Bytes(), nil
}

func sortNewestFirst(releases []Release) {
	slices.SortFunc(releases, func(a, b Release) int {
		return version.Compare(b.Version, a.Version)
	})
}
