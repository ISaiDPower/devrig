package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ISaiDPower/devrig/internal/version"
)

// githubAsset names a release asset and the platform it is for. Name may
// contain {version}.
type githubAsset struct {
	Platform string
	Name     string
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

// githubSource returns a source that reads a repository's recent releases.
// Tags are the version with tagPrefix in front. The sha256 comes from the
// digest GitHub records for every uploaded asset.
func githubSource(repo, tagPrefix string, assets ...githubAsset) func(context.Context) ([]candidate, error) {
	return func(ctx context.Context) ([]candidate, error) {
		var releases []githubRelease
		url := "https://api.github.com/repos/" + repo + "/releases?per_page=30"
		if err := getJSON(ctx, url, &releases); err != nil {
			return nil, err
		}
		return githubCandidates(releases, tagPrefix, assets)
	}
}

// githubCandidates keeps the newest patch release of every minor version.
// Older patch releases stay in versions.json once they have been added.
func githubCandidates(releases []githubRelease, tagPrefix string, assets []githubAsset) ([]candidate, error) {
	newest := make(map[string]githubRelease)
	versions := make(map[string]version.Version)
	for _, rel := range releases {
		if rel.Draft || rel.Prerelease || !strings.HasPrefix(rel.TagName, tagPrefix) {
			continue
		}
		v, err := version.Parse(strings.TrimPrefix(rel.TagName, tagPrefix))
		if err != nil {
			continue // tags such as "nightly" are not releases
		}
		minor := v.Truncate(2).String()
		if old, ok := versions[minor]; !ok || version.Compare(v, old) > 0 {
			newest[minor], versions[minor] = rel, v
		}
	}

	var out []candidate
	for minor, rel := range newest {
		v := versions[minor]
		for _, want := range assets {
			name := strings.ReplaceAll(want.Name, "{version}", v.String())
			c, err := findAsset(rel, name)
			if errors.Is(err, errNoDigest) {
				// GitHub only records digests for assets uploaded since 2025.
				// Without one there is nothing to verify against.
				fmt.Fprintf(os.Stderr, "skipping %s %s: %v\n", rel.TagName, name, err)
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("%s: %w", rel.TagName, err)
			}
			c.Version, c.Platform = v, want.Platform
			out = append(out, c)
		}
	}
	return out, nil
}

var errNoDigest = errors.New("no sha256 digest")

func findAsset(rel githubRelease, name string) (candidate, error) {
	for _, a := range rel.Assets {
		if a.Name != name {
			continue
		}
		sum, ok := strings.CutPrefix(a.Digest, "sha256:")
		if !ok {
			return candidate{}, errNoDigest
		}
		return candidate{URL: a.URL, SHA256: sum}, nil
	}
	return candidate{}, fmt.Errorf("no asset named %s", name)
}

// githubToken authenticates API requests when GITHUB_TOKEN is set, which
// raises GitHub's rate limit.
func githubToken() string {
	return os.Getenv("GITHUB_TOKEN")
}
