package extract

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestManifestPath(t *testing.T) {
	if got, want := ManifestPath("/srv/extract", "h1"), "/srv/extract/.extract-state/h1.files"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	p := ManifestPath(t.TempDir(), "h1")
	paths := []string{"/etc/passwd", "/home/a/.ssh/config", "/path with spaces/x"}
	if err := WriteManifest(p, paths); err != nil {
		t.Fatal(err)
	}
	got, err := ReadManifest(p)
	if err != nil || !reflect.DeepEqual(got, paths) {
		t.Errorf("ReadManifest = %q, %v; want %q", got, err, paths)
	}
	// Overwriting replaces the content.
	if err := WriteManifest(p, paths[:1]); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadManifest(p); err != nil || !reflect.DeepEqual(got, paths[:1]) {
		t.Errorf("after rewrite = %q, %v", got, err)
	}
	// An empty manifest reads back as empty.
	if err := WriteManifest(p, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadManifest(p); err != nil || len(got) != 0 {
		t.Errorf("empty manifest = %q, %v", got, err)
	}
}

func TestManifestMissingIsNil(t *testing.T) {
	got, err := ReadManifest(ManifestPath(t.TempDir(), "h1"))
	if got != nil || err != nil {
		t.Errorf("got %q, %v; want nil, nil", got, err)
	}
}

func TestManifestReadError(t *testing.T) {
	p := ManifestPath(t.TempDir(), "h1")
	if err := os.MkdirAll(p, 0o700); err != nil { // a directory where the file should be
		t.Fatal(err)
	}
	if _, err := ReadManifest(p); err == nil {
		t.Error("want an error reading a directory")
	}
}

func TestManifestModes(t *testing.T) {
	root := t.TempDir()
	p := ManifestPath(root, "h1")
	if err := WriteManifest(p, []string{"/x"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if m := st.Mode().Perm(); m != 0o600 {
		t.Errorf("file mode %#o, want 0600", m)
	}
	dst, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if m := dst.Mode().Perm(); m != 0o700 {
		t.Errorf("dir mode %#o, want 0700", m)
	}
}

func TestManifestNoTempLeftBehind(t *testing.T) {
	p := ManifestPath(t.TempDir(), "h1")
	for range 2 {
		if err := WriteManifest(p, []string{"/a", "/b"}); err != nil {
			t.Fatal(err)
		}
	}
	ents, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if !reflect.DeepEqual(names, []string{"h1.files"}) {
		t.Errorf("dir holds %q, want only h1.files", names)
	}
}
