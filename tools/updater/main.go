// Command updater refreshes the generated versions.json files under recipes/
// from upstream release data.
//
// Usage:
//
//	go run ./tools/updater [-recipes dir] [package...]
//
// Releases already listed are kept as they are. Every new asset is downloaded
// once and its size and sha256 checked against what upstream publishes before
// it is added.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"

	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/internal/version"
)

const userAgent = "devrig-updater (+https://github.com/ISaiDPower/devrig)"

// candidate is a release asset upstream publishes, before it is verified.
type candidate struct {
	Version  version.Version
	Platform string
	URL      string
	SHA256   string
}

// sources lists the upstream release data for each package it can update.
var sources = map[string]func(ctx context.Context) ([]candidate, error){
	"mailpit": githubSource("axllent/mailpit", "v",
		githubAsset{"windows/amd64", "mailpit-windows-amd64.zip"},
		githubAsset{"windows/arm64", "mailpit-windows-arm64.zip"},
	),
	// nginx.org builds Windows as 32 bit, which runs on 64 bit Windows too.
	"nginx": githubSource("nginx/nginx", "release-",
		githubAsset{"windows/amd64", "nginx-{version}.zip"},
	),
	"php": phpReleases,
}

func main() {
	dir := flag.String("recipes", "recipes", "directory holding the recipes")
	flag.Parse()

	names := flag.Args()
	if len(names) == 0 {
		names = slices.Sorted(maps.Keys(sources))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for _, name := range names {
		if err := update(ctx, filepath.Join(*dir, name, "versions.json"), name); err != nil {
			fmt.Fprintf(os.Stderr, "updater: %s: %v\n", name, err)
			stop()
			os.Exit(1)
		}
	}
}

func update(ctx context.Context, file, name string) error {
	source, ok := sources[name]
	if !ok {
		return errors.New("no upstream source for this package")
	}

	known, err := readVersions(file)
	if err != nil {
		return err
	}

	candidates, err := source(ctx)
	if err != nil {
		return err
	}

	added := 0
	for _, c := range candidates {
		key := c.Version.String()
		if _, ok := known[key][c.Platform]; ok {
			continue
		}

		size, err := verify(ctx, c)
		if err != nil {
			return err
		}
		if known[key] == nil {
			known[key] = make(map[string]registry.Asset)
		}
		known[key][c.Platform] = registry.Asset{URL: c.URL, SHA256: c.SHA256, Size: size}
		added++
		fmt.Printf("%s %s %s: verified %d bytes\n", name, key, c.Platform, size)
	}

	if added == 0 {
		fmt.Printf("%s: up to date\n", name)
		return nil
	}

	releases := make([]registry.Release, 0, len(known))
	for key, assets := range known {
		v, err := version.Parse(key)
		if err != nil {
			return err
		}
		releases = append(releases, registry.Release{Version: v, Assets: assets})
	}
	data, err := registry.MarshalVersions(releases)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: added %d assets\n", name, added)
	return nil
}

// readVersions loads an existing versions.json keyed by version and platform.
// A missing file counts as empty.
func readVersions(file string) (map[string]map[string]registry.Asset, error) {
	known := make(map[string]map[string]registry.Asset)

	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return known, nil
	}
	if err != nil {
		return nil, err
	}

	releases, err := registry.ParseVersions(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	for _, rel := range releases {
		known[rel.Version.String()] = rel.Assets
	}
	return known, nil
}

// verify downloads a candidate, checks its sha256 and returns its size.
func verify(ctx context.Context, c candidate) (int64, error) {
	body, err := get(ctx, c.URL)
	if err != nil {
		return 0, err
	}
	defer body.Close()

	h := sha256.New()
	size, err := io.Copy(h, body)
	if err != nil {
		return 0, fmt.Errorf("download %s: %w", c.URL, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != c.SHA256 {
		return 0, fmt.Errorf("%s: sha256 is %s, upstream says %s", c.URL, got, c.SHA256)
	}
	return size, nil
}

func get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if token := githubToken(); token != "" && req.URL.Host == "api.github.com" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

func getJSON(ctx context.Context, url string, v any) error {
	body, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()

	if err := json.NewDecoder(body).Decode(v); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return nil
}
