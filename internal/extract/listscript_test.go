package extract

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gavincarr/extract/internal/proc"
	"github.com/gavincarr/extract/internal/testutil"
)

const listTimeout = 60 * time.Second

// eachShell runs fn once per POSIX shell available here, as the remote
// "sh": the default sh, then bash, dash and busybox sh when installed. The
// fake ssh puts a "sh" symlink to FAKE_SSH_SHELL first on PATH, so both the
// login shell and the script's own "sh -c" run under the shell under test
// (bash then runs in POSIX mode, as it does when it is /bin/sh).
func eachShell(t *testing.T, fn func(t *testing.T)) {
	t.Helper()
	for _, name := range []string{"sh", "bash", "dash", "busybox"} {
		t.Run(name, func(t *testing.T) {
			if name != "sh" {
				p, err := exec.LookPath(name)
				if err != nil {
					t.Skipf("%s not installed", name)
				}
				if name == "busybox" && exec.Command(p, "sh", "-c", ":").Run() != nil {
					t.Skip("busybox has no sh applet")
				}
				t.Setenv("FAKE_SSH_SHELL", p)
			}
			fn(t)
		})
	}
}

// runList runs the list script through the fake ssh with manifest on
// stdin and returns the parsed result, failing the test on any error.
func runList(t *testing.T, remoteLists, globs []string, manifest string) ListResult {
	t.Helper()
	var stdin io.Reader
	if manifest != "" {
		stdin = strings.NewReader(manifest)
	}
	argv := ListArgs(Host{User: "u", Name: "h1"}, remoteLists, globs)
	s := proc.RunContextStdin(context.Background(), argv, "", listTimeout, stdin)
	r, st, msg := listOutcome(s, listTimeout)
	if st != StatusOK {
		t.Fatalf("status %v: %s\nstdout:\n%s\nstderr:\n%s", st, msg, s.Stdout, s.Stderr)
	}
	if s.Stderr != "" {
		t.Errorf("unexpected stderr: %q", s.Stderr)
	}
	return r
}

func mkfile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func symlink(t *testing.T, target, link string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return link
}

// passwd builds passwd lines with the given home directories.
func passwd(homes ...string) string {
	var b strings.Builder
	for i, h := range homes {
		fmt.Fprintf(&b, "u%d:x:%d:%d:User %d:%s:/bin/sh\n", i, 1000+i, 1000+i, i, h)
	}
	return b.String()
}

// sameSet compares two path lists ignoring order.
func sameSet(t *testing.T, what string, got, want []string) {
	t.Helper()
	g, w := slices.Clone(got), slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)
	if len(g) == 0 && len(w) == 0 {
		return
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s:\n got  %q\n want %q", what, g, w)
	}
}

func TestListArgsShape(t *testing.T) {
	cases := []struct {
		name         string
		lists, globs []string
	}{
		{"empty", nil, nil},
		{"lists only", []string{"/var/a", "/var/b c"}, nil},
		{"both", []string{"/var/it's", "/var/x!y"}, []string{"/etc/cron.d/*", "~/.ssh/*", "/etc/[!a]*", "~/it's!"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ListArgs(Host{User: "u", Name: "h1"}, c.lists, c.globs)
			want := []string{"ssh", "-o", "BatchMode=yes", "u@h1", "sh", "-c"}
			if len(got) != 7 || !reflect.DeepEqual(got[:6], want) {
				t.Fatalf("argv = %q, want prefix %q plus the script", got, want)
			}
			script := got[6]
			if !strings.HasPrefix(script, "'") || !strings.HasSuffix(script, "'") {
				t.Errorf("script is not single-quoted: %s", script)
			}
			if strings.Contains(script, "\n") || strings.Contains(script, "!") {
				t.Errorf("script must be one line without '!': %s", script)
			}
		})
	}
}

func TestListRemoteListChosen(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		testutil.NewFakeSSH(t)
		root := t.TempDir()
		x := mkfile(t, filepath.Join(root, "etc", "x"))
		gone := filepath.Join(root, "gone")
		empty := testutil.WriteFile(t, root, "empty", "")
		// CRLF on the first entry, comments, a blank line, and no newline
		// after the last entry.
		good := testutil.WriteFile(t, root, "good", x+"\r\n\n# c\n; c\r\n"+gone)
		r := runList(t, []string{filepath.Join(root, "missing"), empty, good}, nil, "")
		if r.Remote != good || r.NoList {
			t.Errorf("Remote = %q, NoList = %v; want %q, false", r.Remote, r.NoList, good)
		}
		sameSet(t, "Listed", r.Listed, []string{x})
		sameSet(t, "Missing", r.Missing, []string{gone})
		sameSet(t, "Globbed", r.Globbed, nil)
	})
}

func TestListNoRemoteList(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		testutil.NewFakeSSH(t)
		root := t.TempDir()
		r := runList(t, []string{filepath.Join(root, "a"), filepath.Join(root, "b")}, nil, "")
		if !r.NoList || r.Remote != "" {
			t.Errorf("NoList = %v, Remote = %q; want true, \"\"", r.NoList, r.Remote)
		}
	})
}

func TestListHomeGlobs(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		f := testutil.NewFakeSSH(t)
		base := t.TempDir()
		a := filepath.Join(base, "home", "a")
		s := filepath.Join(base, "srv", "svc")
		spaced := filepath.Join(base, "home", "b c")
		starred := filepath.Join(base, "home", "st*r")
		want := []string{
			mkfile(t, filepath.Join(a, ".ssh", "authorized_keys")),
			mkfile(t, filepath.Join(a, ".ssh", "my key")),
			mkfile(t, filepath.Join(s, ".ssh", "id")),
		}
		mkfile(t, filepath.Join(spaced, ".ssh", "id"))
		mkfile(t, filepath.Join(starred, ".ssh", "id"))
		f.SetPasswd(t, passwd(a, s, a, a+"/", "/nonexistent", "/", "/bin", spaced, starred, "relative/home", ""))
		r := runList(t, nil, []string{"~/.ssh/*"}, "")
		sameSet(t, "Globbed", r.Globbed, want)
		if len(r.Globbed) != len(want) {
			t.Errorf("Globbed has duplicates: %q", r.Globbed)
		}
		sameSet(t, "Symlinks", r.Symlinks, nil)
	})
}

func TestListHomeIsSymlink(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		f := testutil.NewFakeSSH(t)
		base := t.TempDir()
		realH := filepath.Join(base, "real", "h")
		h := symlink(t, realH, filepath.Join(base, "home", "h"))
		mkfile(t, filepath.Join(realH, "dotfiles", "bashrc"))
		mkfile(t, filepath.Join(realH, "dotfiles", "ssh", "config"))
		symlink(t, "dotfiles/bashrc", filepath.Join(realH, ".bashrc"))
		symlink(t, "dotfiles/ssh", filepath.Join(realH, ".ssh"))
		f.SetPasswd(t, passwd(h))
		r := runList(t, nil, []string{"~/.bashrc", "~/.ssh/*"}, "")
		sameSet(t, "Globbed", r.Globbed, []string{h + "/.bashrc", h + "/.ssh/config"})
		sameSet(t, "Symlinks", r.Symlinks, nil)
	})
}

func TestListSymlinkOutsideHomeSkipped(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		f := testutil.NewFakeSSH(t)
		base := t.TempDir()
		a := filepath.Join(base, "a")
		b := filepath.Join(base, "b")
		kh := mkfile(t, filepath.Join(a, ".ssh", "known_hosts"))
		cfg := symlink(t, "/etc/hostname", filepath.Join(a, ".ssh", "config"))
		symlink(t, filepath.Join(a, ".ssh"), filepath.Join(b, ".ssh"))
		f.SetPasswd(t, passwd(a, b))
		r := runList(t, nil, []string{"~/.ssh/*"}, "")
		sameSet(t, "Globbed", r.Globbed, []string{kh})
		sameSet(t, "Symlinks", r.Symlinks, []string{cfg, b + "/.ssh/config", b + "/.ssh/known_hosts"})
	})
}

func TestListAbsolutePatternSymlinkSkipped(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		testutil.NewFakeSSH(t)
		base := t.TempDir()
		cron := filepath.Join(base, "cron")
		y := mkfile(t, filepath.Join(cron, "y"))
		x := symlink(t, "y", filepath.Join(cron, "x"))
		link := symlink(t, cron, filepath.Join(base, "link"))
		r := runList(t, nil, []string{cron + "/*", link + "/*"}, "")
		sameSet(t, "Globbed", r.Globbed, []string{y})
		sameSet(t, "Symlinks", r.Symlinks, []string{x, link + "/x", link + "/y"})
	})
}

func TestListDirsAndNoMatchIgnored(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		f := testutil.NewFakeSSH(t)
		base := t.TempDir()
		mkdir(t, filepath.Join(base, "d", "sub"))
		mkdir(t, filepath.Join(base, "h", ".config", "systemd", "user", "x.wants"))
		f.SetPasswd(t, passwd(filepath.Join(base, "h")))
		r := runList(t, nil, []string{base + "/d/*", base + "/none/*", base + "/lit", "~/.config/systemd/user/*", "~/.k5login"}, "")
		for name, l := range map[string][]string{"Listed": r.Listed, "Globbed": r.Globbed, "Tracked": r.Tracked,
			"Missing": r.Missing, "Symlinks": r.Symlinks, "Skipped": r.Skipped} {
			sameSet(t, name, l, nil)
		}
		if r.NoList || r.Remote != "" {
			t.Errorf("NoList = %v, Remote = %q with no remote lists configured", r.NoList, r.Remote)
		}
	})
}

func TestListManifestStdin(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		testutil.NewFakeSSH(t)
		base := t.TempDir()
		var b strings.Builder
		for i := range 10000 {
			p := filepath.Join(base, fmt.Sprintf("f%05d", i))
			if i%2 == 0 {
				if err := os.WriteFile(p, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			b.WriteString(p)
			if i < 9999 { // no newline after the last line
				b.WriteString("\n")
			}
		}
		r := runList(t, nil, nil, b.String())
		if len(r.Tracked) != 5000 || len(r.Missing) != 5000 {
			t.Errorf("Tracked %d, Missing %d; want 5000 each", len(r.Tracked), len(r.Missing))
		}
		if len(r.Missing) > 0 && r.Missing[len(r.Missing)-1] != filepath.Join(base, "f09999") {
			t.Errorf("last Missing = %q, want the unterminated last line", r.Missing[len(r.Missing)-1])
		}
	})
}

func TestListNewlineName(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		f := testutil.NewFakeSSH(t)
		a := t.TempDir()
		ok := mkfile(t, filepath.Join(a, ".ssh", "ok"))
		mkfile(t, filepath.Join(a, ".ssh", "a\nb"))
		f.SetPasswd(t, passwd(a))
		r := runList(t, nil, []string{"~/.ssh/*"}, "")
		sameSet(t, "Globbed", r.Globbed, []string{ok})
		sameSet(t, "Skipped", r.Skipped, []string{a + "/.ssh/a?b"})
	})
}

// rsync's --files-from ends a line at a CR as well as a newline, so a CR
// name must be skipped like a newline one, never emitted as a match.
func TestListCRName(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		f := testutil.NewFakeSSH(t)
		a := t.TempDir()
		ok := mkfile(t, filepath.Join(a, ".ssh", "ok"))
		mkfile(t, filepath.Join(a, ".ssh", "x\r.git"))
		mkfile(t, filepath.Join(a, ".ssh", "y\r"))
		mkfile(t, filepath.Join(a, ".ssh", "n\r\nz"))
		f.SetPasswd(t, passwd(a))
		r := runList(t, nil, []string{"~/.ssh/*", a + "/.ssh/x*"}, "")
		sameSet(t, "Globbed", r.Globbed, []string{ok})
		sameSet(t, "Skipped", r.Skipped, []string{
			a + "/.ssh/x?.git", a + "/.ssh/y?", a + "/.ssh/n??z", // ~/ pattern
			a + "/.ssh/x?.git", // absolute pattern
		})
	})
}

// Remote-list and manifest lines lose one trailing CR (remote list only);
// a line that still holds one is skipped, not tested or listed.
func TestListCRInRemoteListAndManifest(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		testutil.NewFakeSSH(t)
		base := t.TempDir()
		x := mkfile(t, filepath.Join(base, "x"))
		mkfile(t, filepath.Join(base, "a\rb"))
		list := testutil.WriteFile(t, base, "list", x+"\r\n"+base+"/a\rb\r\n"+base+"/c\rd\n")
		r := runList(t, []string{list}, nil, x+"\n"+base+"/a\rb\n"+x+"\r\n")
		sameSet(t, "Listed", r.Listed, []string{x})
		sameSet(t, "Tracked", r.Tracked, []string{x})
		sameSet(t, "Missing", r.Missing, nil)
		sameSet(t, "Skipped", r.Skipped, []string{base + "/a?b", base + "/c?d", base + "/a?b", x + "?"})
	})
}

// A home that is, or resolves to, / would put every user's ~/ pattern at
// the root of the filesystem. Such a home contributes nothing.
func TestListRootHomeSkipped(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		f := testutil.NewFakeSSH(t)
		base := t.TempDir()
		rootLink := symlink(t, "/", filepath.Join(base, "rooth"))
		a := filepath.Join(base, "a")
		kh := mkfile(t, filepath.Join(a, "etc", "passwd"))
		f.SetPasswd(t, passwd("//", "/", rootLink, a))
		r := runList(t, nil, []string{"~/etc/*"}, "")
		sameSet(t, "Globbed", r.Globbed, []string{kh})
		sameSet(t, "Symlinks", r.Symlinks, nil)
		sameSet(t, "Skipped", r.Skipped, nil)
	})
}

// Without a working readlink -f every ~/ match would quietly vanish, so the
// step fails instead, saying why.
func TestListReadlinkUnsupported(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		if strings.HasSuffix(os.Getenv("FAKE_SSH_SHELL"), "busybox") {
			t.Skip("busybox sh runs its own readlink applet, not the one on PATH")
		}
		f := testutil.NewFakeSSH(t)
		f.SetPasswd(t, passwd(t.TempDir()))
		f.WriteBin(t, "readlink", "echo \"readlink: illegal option -- f\" >&2\nexit 1\n")
		argv := ListArgs(Host{User: "u", Name: "h1"}, nil, []string{"~/.ssh/*"})
		s := proc.RunContextStdin(context.Background(), argv, "", listTimeout, nil)
		_, st, msg := listOutcome(s, listTimeout)
		if st != StatusFailed || msg != "files list step exited 3" {
			t.Errorf("got %v %q, want StatusFailed %q", st, msg, "files list step exited 3")
		}
		if want := "readlink -f is not supported on this host; EXTRACT_FILES_GLOBS needs it\n"; s.Stderr != want || s.Stdout != "" {
			t.Errorf("stdout %q stderr %q, want stderr %q", s.Stdout, s.Stderr, want)
		}

		// Without globs readlink isn't needed, so isn't checked.
		runList(t, []string{filepath.Join(t.TempDir(), "none")}, nil, "")
	})
}

func TestListQuotingAndBang(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		testutil.NewFakeSSH(t)
		base := t.TempDir()
		dir := filepath.Join(base, "it's!")
		keep := mkfile(t, filepath.Join(dir, "b"))
		mkfile(t, filepath.Join(dir, "a"))
		entry := mkfile(t, filepath.Join(base, "e $x `y` \\z"))
		list := testutil.WriteFile(t, base, "my list's!", entry+"\n")
		r := runList(t, []string{list}, []string{dir + "/[!a]*"}, "")
		if r.Remote != list {
			t.Errorf("Remote = %q, want %q", r.Remote, list)
		}
		sameSet(t, "Listed", r.Listed, []string{entry})
		sameSet(t, "Globbed", r.Globbed, []string{keep})
	})
}

func TestListPasswdFallback(t *testing.T) {
	eachShell(t, func(t *testing.T) {
		f := testutil.NewFakeSSH(t)
		f.SetPasswdFails(t)
		// runList fails the test unless the step exits 0 with the sentinel.
		runList(t, nil, []string{"~/.extract-test-none"}, "")
	})
}

func TestListCshLoginShell(t *testing.T) {
	f := testutil.NewFakeSSH(t)
	argv := ListArgs(Host{User: "u", Name: "h1"}, []string{"/a"}, []string{"~/.ssh/*"})

	f.SetHost("h1", 1, "", "for: Command not found.\n", 0)
	s := proc.RunContextStdin(context.Background(), argv, "", listTimeout, nil)
	_, st, msg := listOutcome(s, listTimeout)
	if st != StatusFailed || msg != "files list step exited 1" {
		t.Errorf("got %v %q, want StatusFailed %q", st, msg, "files list step exited 1")
	}

	f.SetHost("h1", 0, "R /a\nG /x\n", "", 0)
	s = proc.RunContextStdin(context.Background(), argv, "", listTimeout, nil)
	_, st, msg = listOutcome(s, listTimeout)
	if want := "files list output incomplete (no end marker)"; st != StatusFailed || msg != want {
		t.Errorf("got %v %q, want StatusFailed %q", st, msg, want)
	}
}

func TestListOutcome(t *testing.T) {
	cases := []struct {
		name   string
		s      proc.StepResult
		status Status
		msg    string
	}{
		{"start", proc.StepResult{ExitCode: -1, Err: fmt.Errorf("no ssh")}, StatusFailed, "files list step failed to start: no ssh"},
		{"timeout", proc.StepResult{ExitCode: -1, TimedOut: true}, StatusTimedOut, "files list step timed out after 1m0s"},
		{"interrupted", proc.StepResult{ExitCode: -1, Interrupted: true}, StatusFailed, "files list step interrupted"},
		{"exit", proc.StepResult{ExitCode: 255, Stdout: "E\n"}, StatusFailed, "files list step exited 255"},
		{"ok", proc.StepResult{Stdout: "N\nE\n"}, StatusOK, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, st, msg := listOutcome(c.s, time.Minute)
			if st != c.status || msg != c.msg {
				t.Errorf("got %v %q, want %v %q", st, msg, c.status, c.msg)
			}
		})
	}
}

func TestListParse(t *testing.T) {
	r, err := parseList("R /etc/l\nF /a b\nM /gone\nG /g\nT /t\nM /m2\nL /l\nX /x?y\nE\n")
	if err != nil {
		t.Fatal(err)
	}
	want := ListResult{Remote: "/etc/l", Listed: []string{"/a b"}, Globbed: []string{"/g"}, Tracked: []string{"/t"},
		Missing: []string{"/gone", "/m2"}, Symlinks: []string{"/l"}, Skipped: []string{"/x?y"}}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("got  %+v\nwant %+v", r, want)
	}
	if r, err := parseList("N\nE\n"); err != nil || !r.NoList {
		t.Errorf("N: got %+v, %v", r, err)
	}

	for _, bad := range []struct{ in, err string }{
		{"", "files list output incomplete (no end marker)"},
		{"F /a\n", "files list output incomplete (no end marker)"},
		{"E\nF /a\n", "files list output incomplete (no end marker)"},
		{"E", "files list output incomplete (no end marker)"},
		{"motd\nE\n", `files list output has an unrecognised line: "motd"`},
		{"F \nE\n", `files list output has an unrecognised line: "F "`},
		{"Q /a\nE\n", `files list output has an unrecognised line: "Q /a"`},
	} {
		if _, err := parseList(bad.in); err == nil || err.Error() != bad.err {
			t.Errorf("parseList(%q) error = %v, want %q", bad.in, err, bad.err)
		}
	}
}
