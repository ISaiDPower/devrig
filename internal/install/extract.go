package install

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// extractZip unpacks the zip archive src into the directory dst, dropping the
// first strip path components of every entry. Entries that would land
// outside dst are rejected, and writes go through an os.Root so that nothing
// can escape it.
func extractZip(src, dst string, strip int) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()

	for _, f := range r.File {
		name, ok := stripComponents(f.Name, strip)
		if !ok {
			continue
		}
		if !filepath.IsLocal(name) {
			return fmt.Errorf("unsafe path in archive: %q", f.Name)
		}

		switch mode := f.Mode(); {
		case mode.IsDir():
			err = root.MkdirAll(name, 0o755)
		case mode.IsRegular():
			err = writeFile(root, name, f)
		default:
			err = fmt.Errorf("unsupported entry type %s", mode.Type())
		}
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	return nil
}

// stripComponents removes the first n components of a zip entry name and
// returns it as an OS path. It reports false for entries that are removed
// entirely, such as the top level directory itself.
func stripComponents(name string, n int) (string, bool) {
	// Some Windows tools write backslashes although zip mandates slashes.
	name = strings.ReplaceAll(name, `\`, "/")
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	if len(parts) <= n {
		return "", false
	}
	return filepath.FromSlash(strings.Join(parts[n:], "/")), true
}

func writeFile(root *os.Root, name string, f *zip.File) error {
	if dir := filepath.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	perm := fs.FileMode(0o644)
	if f.Mode()&0o111 != 0 {
		perm = 0o755
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
