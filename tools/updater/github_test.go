package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func releasesSample(t *testing.T) []githubRelease {
	t.Helper()
	asset := func(tag, name string) string {
		return `{"name": "` + name + `", "browser_download_url": "https://github.com/x/y/releases/download/` + tag + `/` + name + `", "digest": "sha256:` + strings.Repeat("a", 64) + `"}`
	}
	release := func(tag string, extra string) string {
		v := strings.TrimPrefix(tag, "release-")
		return `{"tag_name": "` + tag + `"` + extra + `, "assets": [` + asset(tag, "nginx-"+v+".zip") + `, ` + asset(tag, "nginx-"+v+".tar.gz") + `]}`
	}

	sample := "[" + strings.Join([]string{
		release("release-1.31.7", ""),
		release("release-1.31.6", ""),
		release("release-1.30.6", ""),
		release("release-1.32.0", `, "prerelease": true`),
		release("release-1.33.0", `, "draft": true`),
		`{"tag_name": "nightly", "assets": []}`,
	}, ",") + "]"

	var releases []githubRelease
	if err := json.Unmarshal([]byte(sample), &releases); err != nil {
		t.Fatal(err)
	}
	return releases
}

func TestGitHubCandidates(t *testing.T) {
	got, err := githubCandidates(releasesSample(t), "release-", []githubAsset{{"windows/amd64", "nginx-{version}.zip"}})
	if err != nil {
		t.Fatal(err)
	}

	var versions []string
	for _, c := range got {
		versions = append(versions, c.Version.String())
		if c.Platform != "windows/amd64" || !strings.HasSuffix(c.URL, ".zip") || len(c.SHA256) != 64 {
			t.Errorf("unexpected candidate %+v", c)
		}
	}
	slices.Sort(versions)
	if want := []string{"1.30.6", "1.31.7"}; !slices.Equal(versions, want) {
		t.Errorf("versions = %v, want the newest patch of each minor %v", versions, want)
	}
}

func TestGitHubCandidatesSkipsAssetsWithoutDigest(t *testing.T) {
	releases := releasesSample(t)
	for i := range releases[0].Assets {
		releases[0].Assets[i].Digest = ""
	}

	got, err := githubCandidates(releases, "release-", []githubAsset{{"windows/amd64", "nginx-{version}.zip"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Version.String() == "1.31.7" {
			t.Error("1.31.7 has no digest and should be skipped")
		}
	}
}

func TestGitHubCandidatesMissingAsset(t *testing.T) {
	if _, err := githubCandidates(releasesSample(t), "release-", []githubAsset{{"windows/arm64", "nginx-{version}-arm64.zip"}}); err == nil {
		t.Error("a missing asset should be an error")
	}
}
