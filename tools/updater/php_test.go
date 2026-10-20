package main

import (
	"encoding/json"
	"testing"
)

// A trimmed down releases.json in the shape windows.php.net publishes.
const phpIndexSample = `{
  "8.4": {
    "version": "8.4.26",
    "nts-vs17-x64": {
      "mtime": "2026-09-23T10:00:00+00:00",
      "zip": {"path": "php-8.4.26-nts-Win32-vs17-x64.zip", "size": "33.48MB", "sha256": "DA68394F9193B7F6B89D0C76861A4034AE10EFEE7FD55A7255D8118C2ACF70D7"},
      "debug_pack": {"path": "php-debug-pack-8.4.26-nts-Win32-vs17-x64.zip", "size": "22MB", "sha256": "00"}
    },
    "ts-vs17-x64": {"zip": {"path": "php-8.4.26-Win32-vs17-x64.zip", "size": "33MB", "sha256": "11"}},
    "nts-vs17-x86": {"zip": {"path": "php-8.4.26-nts-Win32-vs17-x86.zip", "size": "30MB", "sha256": "22"}},
    "source": {"path": "php-8.4.26-src.zip", "size": "25MB", "sha256": "33"}
  }
}`

func TestPHPCandidates(t *testing.T) {
	var index phpIndex
	if err := json.Unmarshal([]byte(phpIndexSample), &index); err != nil {
		t.Fatal(err)
	}

	got, err := index.candidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want only the nts x64 build: %+v", len(got), got)
	}

	c := got[0]
	if c.Version.String() != "8.4.26" || c.Platform != "windows/amd64" {
		t.Errorf("candidate = %s %s", c.Version, c.Platform)
	}
	if want := phpArchiveURL + "php-8.4.26-nts-Win32-vs17-x64.zip"; c.URL != want {
		t.Errorf("URL = %s, want %s", c.URL, want)
	}
	if c.SHA256 != "da68394f9193b7f6b89d0c76861a4034ae10efee7fd55a7255d8118c2acf70d7" {
		t.Errorf("sha256 should be lower cased, got %s", c.SHA256)
	}
}

func TestPHPCandidatesRejectsOddPaths(t *testing.T) {
	var index phpIndex
	sample := `{"8.4": {"version": "8.4.26", "nts-vs17-x64": {"zip": {"path": "../php.zip", "sha256": "00"}}}}`
	if err := json.Unmarshal([]byte(sample), &index); err != nil {
		t.Fatal(err)
	}
	if _, err := index.candidates(); err == nil {
		t.Error("a zip path with directories should be rejected")
	}
}
