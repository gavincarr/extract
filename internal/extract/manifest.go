package extract

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// stateDir is the directory under EXTRACT_ROOT holding per-host manifests.
// Host names may not start with '.', so it never collides with a host.
const stateDir = ".extract-state"

// ManifestPath returns the manifest file for host under root: the paths
// extract has extracted for that host that still existed, one per line.
func ManifestPath(root, host string) string {
	return filepath.Join(root, stateDir, host+".files")
}

// ReadManifest reads a manifest. A missing file gives nil, nil.
func ReadManifest(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(string(data), "\n") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// WriteManifest atomically replaces the manifest at path with paths. It
// creates the directory with mode 0700 if needed and writes a temp file
// with mode 0600 in it, then renames that over path.
func WriteManifest(path string, paths []string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op once renamed
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
