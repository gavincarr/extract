package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadGlobsInline(t *testing.T) {
	tmp := t.TempDir()
	p := writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\nEXTRACT_FILES_GLOBS=\"~/.ssh/* /var/spool/cron/*\"\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != ModeFilesRemote || c.Source != "" {
		t.Errorf("mode/source = %v %q", c.Mode, c.Source)
	}
	if want := []string{"~/.ssh/*", "/var/spool/cron/*"}; !reflect.DeepEqual(c.Globs, want) {
		t.Errorf("Globs = %v, want %v", c.Globs, want)
	}
	if !c.UsesGlobs() {
		t.Error("UsesGlobs = false")
	}
}

func TestLoadGlobsFileRelativeToConfig(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "extract.globs"),
		[]byte("# comment\r\n\r\n~/.ssh/*\r\n  /etc/cron.d/*  \r\n# end\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\nEXTRACT_FILES_GLOBS_FILE=extract.globs\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(c.GlobsFile) || c.GlobsFile != filepath.Join(tmp, "extract.globs") {
		t.Errorf("GlobsFile = %q", c.GlobsFile)
	}
	if want := []string{"~/.ssh/*", "/etc/cron.d/*"}; !reflect.DeepEqual(c.Globs, want) {
		t.Errorf("Globs = %v, want %v", c.Globs, want)
	}
	if c.Mode != ModeFilesRemote {
		t.Errorf("Mode = %v", c.Mode)
	}
}

func TestLoadGlobsUnion(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "g"), []byte("~/.b\n~/.c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\nEXTRACT_FILES_GLOBS=\"~/.a ~/.b\"\nEXTRACT_FILES_GLOBS_FILE="+filepath.Join(tmp, "g")+"\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"~/.a", "~/.b", "~/.c"}; !reflect.DeepEqual(c.Globs, want) {
		t.Errorf("Globs = %v, want %v", c.Globs, want)
	}
}

func TestLoadGlobsWithFilesRemote(t *testing.T) {
	tmp := t.TempDir()
	p := writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\nEXTRACT_FILES_REMOTE=/etc/files.list\nEXTRACT_FILES_GLOBS=\"~/.ssh/*\"\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/etc/files.list"}; !reflect.DeepEqual(c.FilesRemote(), want) {
		t.Errorf("FilesRemote = %v", c.FilesRemote())
	}
	if want := []string{"~/.ssh/*"}; !reflect.DeepEqual(c.Globs, want) {
		t.Errorf("Globs = %v", c.Globs)
	}
}

func TestLoadGlobsErrors(t *testing.T) {
	tmp := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(tmp, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tabFile := write("tab", "~/.a\n\n~/.b\t~/.c\n")
	emptyFile := write("empty", "# nothing\n\n")
	list := write("list", "/etc/hosts\n")
	base := "EXTRACT_ROOT=" + tmp + "\n"
	tests := []struct {
		name, lines, want string
	}{
		{"relative", "EXTRACT_FILES_GLOBS=relative/x\n", "must start with / or ~/"},
		{"tilde user", "EXTRACT_FILES_GLOBS=~bob/.ssh\n", "must start with / or ~/"},
		{"tab in file", "EXTRACT_FILES_GLOBS_FILE=" + tabFile + "\n", "line 3:"},
		{"missing file", "EXTRACT_FILES_GLOBS_FILE=" + filepath.Join(tmp, "nope") + "\n", filepath.Join(tmp, "nope")},
		{"empty file", "EXTRACT_FILES_GLOBS_FILE=" + emptyFile + "\n", "lists no patterns"},
		{"blank inline", "EXTRACT_FILES_GLOBS=\"  \"\n", "EXTRACT_FILES_GLOBS is set but lists no patterns"},
		{"tree", "EXTRACT_TREE=/etc\nEXTRACT_FILES_GLOBS=~/.a\n", "can't be combined with EXTRACT_TREE"},
		{"local", "EXTRACT_FILES_LOCAL=" + list + "\nEXTRACT_FILES_GLOBS=~/.a\n", "can't be combined with EXTRACT_FILES_LOCAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConf(t, tmp, base+tt.lines))
			wantErr(t, err, tt.want)
		})
	}
}

func TestLoadShippedGlobsDist(t *testing.T) {
	t.Skip("conf/extract.globs.dist is added in Task 6")
	pats, err := ParseGlobsFile("../../conf/extract.globs.dist")
	if err != nil {
		t.Fatal(err)
	}
	if len(pats) < 50 {
		t.Errorf("only %d patterns", len(pats))
	}
	for _, want := range []string{"~/.ssh/*", "/var/spool/cron/crontabs/*"} {
		found := false
		for _, p := range pats {
			found = found || p == want
		}
		if !found {
			t.Errorf("missing %q", want)
		}
	}
}

func TestParseGlobsFileErrorFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "g")
	if err := os.WriteFile(p, []byte("~/.a\nbad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ParseGlobsFile(p)
	wantErr(t, err, "globs file "+p+" line 2:")
	if err != nil && !strings.Contains(err.Error(), "must start with / or ~/") {
		t.Error(err)
	}
}
