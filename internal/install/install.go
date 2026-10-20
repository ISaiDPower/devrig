// Package install downloads, verifies and unpacks package releases into a
// DevRig home directory.
package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ISaiDPower/devrig/internal/home"
	"github.com/ISaiDPower/devrig/internal/registry"
	"github.com/ISaiDPower/devrig/internal/version"
)

// ErrNotInstalled is returned when removing a release that is not installed.
var ErrNotInstalled = errors.New("not installed")

// defaultClient gives up on servers that never answer, but not on large
// downloads that take a while.
var defaultClient = &http.Client{Transport: func() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 30 * time.Second
	return t
}()}

// Installer installs releases into a DevRig home directory.
type Installer struct {
	Home home.Dir
	// Client downloads archives. Nil means a client with sensible timeouts.
	Client *http.Client
	// UserAgent is sent with every download.
	UserAgent string
	// Progress, if set, is called while an archive downloads with the bytes
	// received so far and the expected total.
	Progress func(done, total int64)
}

// IsInstalled reports whether a release is unpacked in the home directory.
func (in *Installer) IsInstalled(name string, v version.Version) bool {
	_, err := os.Stat(in.Home.Release(name, v.String()))
	return err == nil
}

// Install downloads, verifies and unpacks a release, and returns the
// directory it now lives in. Installing a release that is already there does
// nothing.
func (in *Installer) Install(ctx context.Context, pkg *registry.Package, rel registry.Release, asset registry.Asset) (string, error) {
	dest := in.Home.Release(pkg.Name, rel.Version.String())
	if in.IsInstalled(pkg.Name, rel.Version) {
		return dest, nil
	}

	ext, err := archiveExt(asset.URL)
	if err != nil {
		return "", err
	}
	archive, err := in.fetch(ctx, asset, ext)
	if err != nil {
		return "", err
	}

	parent := in.Home.Package(pkg.Name)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	sweep(parent)

	// Unpack next to the final location, then move it into place, so a
	// release directory only ever exists complete.
	tmp, err := os.MkdirTemp(parent, ".tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) // a no-op once moved into place

	if err := extractZip(archive, tmp, pkg.Strip); err != nil {
		return "", fmt.Errorf("unpack %s %s: %w", pkg.Name, rel.Version, err)
	}
	if err := moveIntoPlace(tmp, dest); err != nil {
		return "", fmt.Errorf("install %s %s: %w", pkg.Name, rel.Version, err)
	}
	return dest, nil
}

// Uninstall removes an installed release.
func (in *Installer) Uninstall(name string, v version.Version) error {
	dir := in.Home.Release(name, v.String())
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s %s is %w", name, v, ErrNotInstalled)
	}

	// Move the release out of the way before deleting it, so it is gone at
	// once and never seen half deleted. Programs still running from it keep
	// their files open on Windows. Those files cannot be deleted yet and are
	// swept up by a later install or uninstall.
	parent := in.Home.Package(name)
	trash := filepath.Join(parent, fmt.Sprintf(".trash-%s-%d", v, time.Now().UnixNano()))
	if err := os.Rename(dir, trash); err != nil {
		return fmt.Errorf("remove %s %s (is something still using it?): %w", name, v, err)
	}
	sweep(parent)
	return nil
}

// Installed returns the installed releases of a package, newest first.
func (in *Installer) Installed(name string) ([]version.Version, error) {
	entries, err := os.ReadDir(in.Home.Package(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var vs []version.Version
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if v, err := version.Parse(e.Name()); err == nil {
			vs = append(vs, v)
		}
	}
	slices.SortFunc(vs, func(a, b version.Version) int { return version.Compare(b, a) })
	return vs, nil
}

// Packages returns the names of the packages that have a directory in the
// home directory, sorted.
func (in *Installer) Packages() ([]string, error) {
	entries, err := os.ReadDir(in.Home.Packages())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// fetch returns the path of the verified archive for asset, downloading it
// unless the cache already holds it.
func (in *Installer) fetch(ctx context.Context, asset registry.Asset, ext string) (string, error) {
	dir := in.Home.Downloads()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	cached := filepath.Join(dir, asset.SHA256+ext)
	if verifyFile(cached, asset) == nil {
		return cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", err
	}
	if in.UserAgent != "" {
		req.Header.Set("User-Agent", in.UserAgent)
	}

	client := in.Client
	if client == nil {
		client = defaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", asset.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", asset.URL, resp.Status)
	}

	part, err := os.CreateTemp(dir, ".part-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(part.Name()) // a no-op once renamed

	h := sha256.New()
	w := io.MultiWriter(part, h, &progressWriter{total: asset.Size, report: in.Progress})
	n, err := io.Copy(w, io.LimitReader(resp.Body, asset.Size+1))
	if closeErr := part.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("download %s: %w", asset.URL, err)
	}

	if n != asset.Size {
		return "", fmt.Errorf("download %s: got %d bytes, expected %d", asset.URL, n, asset.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != asset.SHA256 {
		return "", fmt.Errorf("download %s: sha256 is %s, expected %s", asset.URL, got, asset.SHA256)
	}

	if err := os.Rename(part.Name(), cached); err != nil {
		return "", err
	}
	return cached, nil
}

// verifyFile checks that file exists with the size and sha256 of asset.
func verifyFile(file string, asset registry.Asset) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != asset.Size || hex.EncodeToString(h.Sum(nil)) != asset.SHA256 {
		return errors.New("checksum mismatch")
	}
	return nil
}

// archiveExt returns the archive extension of an asset URL. Only zip is
// supported for now, which covers every Windows build.
func archiveExt(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if ext := path.Ext(u.Path); strings.EqualFold(ext, ".zip") {
		return ".zip", nil
	}
	return "", fmt.Errorf("unsupported archive format: %s", path.Base(u.Path))
}

const (
	moveAttempts = 10
	moveDelay    = 200 * time.Millisecond
)

// moveIntoPlace renames a freshly unpacked release to its final directory.
// Antivirus scanners on Windows briefly lock new files, so failures are
// retried for a couple of seconds. If dst appears meanwhile, another rig
// process installed the same release, which is just as good.
func moveIntoPlace(src, dst string) error {
	for attempt := 1; ; attempt++ {
		err := os.Rename(src, dst)
		if err == nil {
			return nil
		}
		if _, statErr := os.Stat(dst); statErr == nil {
			return nil
		}
		if attempt == moveAttempts {
			return err
		}
		time.Sleep(moveDelay)
	}
}

// sweep deletes what earlier uninstalls could not, such as files that were
// still in use. Failures are left for the next sweep.
func sweep(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".trash-") {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

type progressWriter struct {
	done, total int64
	report      func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.report != nil {
		p.report(p.done, p.total)
	}
	return len(b), nil
}
