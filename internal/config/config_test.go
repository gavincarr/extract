package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeConf(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "extract.conf")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func wantErr(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Fatalf("error %q does not contain %q", err, sub)
	}
}

func TestLoadMinimalTree(t *testing.T) {
	tmp := t.TempDir()
	p := writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\nEXTRACT_HOSTS=\"h1 h2\"\nEXTRACT_TREE=/etc\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != ModeTree || c.Source != "/etc" {
		t.Errorf("mode/source = %v %q", c.Mode, c.Source)
	}
	if !reflect.DeepEqual(c.Hosts, []string{"h1", "h2"}) {
		t.Errorf("hosts = %v", c.Hosts)
	}
	if c.ScriptsDir != "/etc/extract/scripts" {
		t.Errorf("ScriptsDir = %q", c.ScriptsDir)
	}
	if c.HookTimeout != 600*time.Second {
		t.Errorf("HookTimeout = %v", c.HookTimeout)
	}
	if !filepath.IsAbs(c.Path) {
		t.Errorf("Path not absolute: %q", c.Path)
	}
	if c.FilesArg() != "" {
		t.Errorf("FilesArg = %q", c.FilesArg())
	}
}

func TestLoadShellSyntax(t *testing.T) {
	tmp := t.TempDir()
	p := writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\r\nexport EXTRACT_HOSTS='h1 h2'\r\n"+
		"EXTRACT_HOST_CMD=\"hosttag -A\"\r\nEXTRACT_TREE=/etc # trailing comment\r\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Hosts, []string{"h1", "h2"}) || c.HostCmd != "hosttag -A" || c.Source != "/etc" {
		t.Errorf("got hosts=%v cmd=%q source=%q", c.Hosts, c.HostCmd, c.Source)
	}
}

func TestLoadSourceModes(t *testing.T) {
	tmp := t.TempDir()
	list := filepath.Join(tmp, "list.txt")
	if err := os.WriteFile(list, []byte("/etc/hosts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := "EXTRACT_ROOT=" + tmp + "\n"
	tests := []struct {
		name    string
		lines   string
		mode    SourceMode
		wantErr string
	}{
		{"remote", "EXTRACT_FILES_REMOTE=/remote/list\n", ModeFilesRemote, ""},
		{"local", "EXTRACT_FILES_LOCAL=" + list + "\n", ModeFilesLocal, ""},
		{"none", "", 0, "one of EXTRACT_TREE, EXTRACT_FILES_REMOTE, EXTRACT_FILES_LOCAL"},
		{"tree+remote", "EXTRACT_TREE=/etc\nEXTRACT_FILES_REMOTE=/r\n", 0, "only one of"},
		{"remote+local", "EXTRACT_FILES_REMOTE=/r\nEXTRACT_FILES_LOCAL=" + list + "\n", 0, "only one of"},
		{"all", "EXTRACT_TREE=/etc\nEXTRACT_FILES_REMOTE=/r\nEXTRACT_FILES_LOCAL=" + list + "\n", 0, "only one of"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Load(writeConf(t, t.TempDir(), base+tc.lines))
			if tc.wantErr != "" {
				wantErr(t, err, tc.wantErr)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Mode != tc.mode {
				t.Errorf("mode = %v, want %v", c.Mode, tc.mode)
			}
			if c.FilesArg() != c.Source || c.Source == "" {
				t.Errorf("FilesArg = %q, Source = %q", c.FilesArg(), c.Source)
			}
		})
	}
}

func TestLoadFilesLocalMustBeReadable(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "nope.txt")
	_, err := Load(writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\nEXTRACT_FILES_LOCAL="+missing+"\n"))
	wantErr(t, err, missing)
}

func TestLoadRootErrors(t *testing.T) {
	tmp := t.TempDir()
	_, err := Load(writeConf(t, tmp, "EXTRACT_TREE=/etc\n"))
	wantErr(t, err, "EXTRACT_ROOT is required")

	missing := filepath.Join(tmp, "missing")
	_, err = Load(writeConf(t, tmp, "EXTRACT_ROOT="+missing+"\nEXTRACT_TREE=/etc\n"))
	wantErr(t, err, missing)

	file := filepath.Join(tmp, "afile")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Load(writeConf(t, tmp, "EXTRACT_ROOT="+file+"\nEXTRACT_TREE=/etc\n"))
	wantErr(t, err, "not a directory")

	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	ro := filepath.Join(tmp, "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	_, err = Load(writeConf(t, tmp, "EXTRACT_ROOT="+ro+"\nEXTRACT_TREE=/etc\n"))
	wantErr(t, err, "not writable")
}

func TestLoadRelativePaths(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeConf(t, filepath.Join(tmp, "conf"), "EXTRACT_ROOT=data\nEXTRACT_TREE=/etc\n")
	if err := os.Rename(filepath.Join(tmp, "conf", "extract.conf"), filepath.Join(tmp, "conf", "x.conf")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmp)
	c, err := Load("conf/x.conf")
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != tmp+"/conf/x.conf" || c.Root != tmp+"/data" {
		t.Errorf("Path=%q Root=%q", c.Path, c.Root)
	}
}

func TestLoadMissingConfigFile(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "none.conf")
	_, err := Load(missing)
	wantErr(t, err, missing)
}

func mkExec(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadHooks(t *testing.T) {
	tmp := t.TempDir()
	s := filepath.Join(tmp, "s")
	if err := os.Mkdir(s, 0o755); err != nil {
		t.Fatal(err)
	}
	mkExec(t, s, "a", 0o755)
	mkExec(t, s, "b", 0o755)
	c := mkExec(t, tmp, "c", 0o755)
	base := "EXTRACT_ROOT=" + tmp + "\nEXTRACT_TREE=/etc\nEXTRACT_SCRIPTS_DIR=" + s + "\n"

	cfg, err := Load(writeConf(t, tmp, base+"POST_EXTRACT_HOST=\" a , b \"\nPRE_EXTRACT_SETUP="+c+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.PostHost, []string{s + "/a", s + "/b"}) {
		t.Errorf("PostHost = %v", cfg.PostHost)
	}
	if !reflect.DeepEqual(cfg.PreSetup, []string{c}) {
		t.Errorf("PreSetup = %v", cfg.PreSetup)
	}

	_, err = Load(writeConf(t, tmp, base+"PRE_EXTRACT_HOST=ghost\n"))
	wantErr(t, err, s+"/ghost")

	mkExec(t, s, "plain", 0o644)
	_, err = Load(writeConf(t, tmp, base+"POST_EXTRACT_TEARDOWN=plain\n"))
	wantErr(t, err, "not executable")

	if err := os.Mkdir(filepath.Join(s, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = Load(writeConf(t, tmp, base+"POST_EXTRACT_HOST=dir\n"))
	wantErr(t, err, "not a regular file")
}

func TestLoadHookTimeout(t *testing.T) {
	tmp := t.TempDir()
	base := "EXTRACT_ROOT=" + tmp + "\nEXTRACT_TREE=/etc\n"
	for in, want := range map[string]time.Duration{"30": 30 * time.Second, "0": 0} {
		c, err := Load(writeConf(t, tmp, base+"EXTRACT_HOOK_TIMEOUT="+in+"\n"))
		if err != nil {
			t.Fatal(err)
		}
		if c.HookTimeout != want {
			t.Errorf("%s: got %v", in, c.HookTimeout)
		}
	}
	for _, in := range []string{"-1", "abc"} {
		_, err := Load(writeConf(t, tmp, base+"EXTRACT_HOOK_TIMEOUT="+in+"\n"))
		wantErr(t, err, "EXTRACT_HOOK_TIMEOUT")
	}
}

func TestLoadShippedDist(t *testing.T) {
	dist := "../../conf/extract.conf.dist"
	_, err := Load(dist)
	wantErr(t, err, "EXTRACT_ROOT is required")

	b, err := os.ReadFile(dist)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		for _, k := range []string{"EXTRACT_HOSTS=", "EXTRACT_HOST_CMD=", "EXTRACT_TREE="} {
			if strings.HasPrefix(l, "#"+k) {
				l = l[1:]
			}
		}
		out = append(out, l)
	}
	out = append(out, "EXTRACT_ROOT="+tmp)
	c, err := Load(writeConf(t, tmp, strings.Join(out, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Hosts, []string{"host1", "host2", "host3"}) || c.HostCmd != "hosttag -A" {
		t.Errorf("hosts=%v cmd=%q", c.Hosts, c.HostCmd)
	}
}

func TestLoadValuesAreLiteral(t *testing.T) {
	tmp := t.TempDir()
	base := "EXTRACT_ROOT=" + tmp + "\nEXTRACT_TREE=/etc\n"
	tests := []struct{ line, want string }{
		{`EXTRACT_HOST_CMD="awk '{print $1}' /etc/hosts.list"`, `awk '{print $1}' /etc/hosts.list`},
		{`EXTRACT_HOST_CMD=echo $HOME`, `echo $HOME`},
		{`EXTRACT_HOST_CMD=echo ${HOME}/x # comment`, `echo ${HOME}/x`},
		{`EXTRACT_HOST_CMD='echo "$HOME"'`, `echo "$HOME"`},
		{`EXTRACT_HOST_CMD="a \"b\" c"`, `a "b" c`},
		{`EXTRACT_HOST_CMD="a\\b \n"`, `a\b \n`},
		{`EXTRACT_HOST_CMD="x" # comment`, `x`},
		{`export   EXTRACT_HOST_CMD=a#b`, `a#b`},
		{"EXTRACT_HOST_CMD=first\nEXTRACT_HOST_CMD=second", `second`},
		{"  # indented comment\n\nEXTRACT_HOST_CMD = spaced", `spaced`},
		{"EXTRACT_HOST_CMD=a=b\t# tab comment", `a=b`},
	}
	for _, tc := range tests {
		c, err := Load(writeConf(t, tmp, base+tc.line+"\n"))
		if err != nil {
			t.Errorf("%s: %v", tc.line, err)
			continue
		}
		if c.HostCmd != tc.want {
			t.Errorf("%s: got %q want %q", tc.line, c.HostCmd, tc.want)
		}
	}
}

func TestLoadSyntaxErrors(t *testing.T) {
	tmp := t.TempDir()
	base := "EXTRACT_ROOT=" + tmp + "\nEXTRACT_TREE=/etc\n"
	for _, line := range []string{
		`EXTRACT_HOST_CMD="unterminated`,
		`EXTRACT_HOST_CMD='unterminated`,
		`EXTRACT_HOST_CMD="a \"`,
		`EXTRACT_HOST_CMD="a"b`,
		`1BAD=x`,
		`BAD-KEY=x`,
		`NOEQUALS`,
	} {
		_, err := Load(writeConf(t, tmp, base+line+"\n"))
		wantErr(t, err, "line 3")
	}
}

func TestLoadRelativeScriptsDirIsMadeAbsolute(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "s"), 0o755); err != nil {
		t.Fatal(err)
	}
	mkExec(t, filepath.Join(tmp, "s"), "a", 0o755)
	t.Chdir(tmp)
	c, err := Load(writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\nEXTRACT_TREE=/etc\nEXTRACT_SCRIPTS_DIR=s\nPRE_EXTRACT_SETUP=a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ScriptsDir != tmp+"/s" {
		t.Errorf("ScriptsDir = %q", c.ScriptsDir)
	}
	if !reflect.DeepEqual(c.PreSetup, []string{tmp + "/s/a"}) {
		t.Errorf("PreSetup = %v", c.PreSetup)
	}
}

func TestLoadRelativeFilesLocalResolvesAgainstRoot(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "list.txt"), []byte("/etc/hosts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	c, err := Load(writeConf(t, cwd, "EXTRACT_ROOT="+root+"\nEXTRACT_FILES_LOCAL=list.txt\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != "list.txt" || c.FilesArg() != "list.txt" {
		t.Errorf("Source = %q FilesArg = %q", c.Source, c.FilesArg())
	}
	_, err = Load(writeConf(t, cwd, "EXTRACT_ROOT="+root+"\nEXTRACT_FILES_LOCAL=nope.txt\n"))
	wantErr(t, err, filepath.Join(root, "nope.txt"))
}

func TestLoadFilesRemoteList(t *testing.T) {
	tmp := t.TempDir()
	base := "EXTRACT_ROOT=" + tmp + "\n"
	tests := []struct {
		name, line string
		want       []string
		filesArg   string
	}{
		{"one", "EXTRACT_FILES_REMOTE=/var/cache/etc.txt\n", []string{"/var/cache/etc.txt"}, "/var/cache/etc.txt"},
		{"one-padded", "EXTRACT_FILES_REMOTE=\" /var/cache/etc.txt \"\n", []string{"/var/cache/etc.txt"}, "/var/cache/etc.txt"},
		{"several", "EXTRACT_FILES_REMOTE=\"/var/cache/rpm.txt  /var/cache/deb.txt\t/x\"\n",
			[]string{"/var/cache/rpm.txt", "/var/cache/deb.txt", "/x"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Load(writeConf(t, t.TempDir(), base+tc.line))
			if err != nil {
				t.Fatal(err)
			}
			if c.Mode != ModeFilesRemote {
				t.Errorf("mode = %v", c.Mode)
			}
			if got := c.FilesRemote(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("FilesRemote = %q, want %q", got, tc.want)
			}
			if c.FilesArg() != tc.filesArg {
				t.Errorf("FilesArg = %q, want %q", c.FilesArg(), tc.filesArg)
			}
		})
	}
}

func TestLoadFilesRemoteWhitespaceOnly(t *testing.T) {
	tmp := t.TempDir()
	base := "EXTRACT_ROOT=" + tmp + "\n"
	_, err := Load(writeConf(t, tmp, base+"EXTRACT_FILES_REMOTE=\"  \t \"\n"))
	wantErr(t, err, "EXTRACT_FILES_REMOTE is set but lists no paths")
	// Set, though blank, still counts towards "only one of".
	_, err = Load(writeConf(t, tmp, base+"EXTRACT_TREE=/etc\nEXTRACT_FILES_REMOTE=\" \"\n"))
	wantErr(t, err, "only one of")
}

func TestShippedDistFilesRemoteListExample(t *testing.T) {
	b, err := os.ReadFile("../../conf/extract.conf.dist")
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, `#EXTRACT_FILES_REMOTE="`) {
			line = l[1:]
		}
	}
	if line == "" {
		t.Fatal("no quoted EXTRACT_FILES_REMOTE example in the dist file")
	}
	tmp := t.TempDir()
	c, err := Load(writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\n"+line+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(c.FilesRemote()); n < 2 {
		t.Errorf("FilesRemote = %q, want several paths", c.FilesRemote())
	}
}
