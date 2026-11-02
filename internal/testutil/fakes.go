// Package testutil provides fakes shared by the extract tests.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// FakeRsync is a stand-in rsync binary placed first on PATH.
type FakeRsync struct{ BinDir, CtlDir string }

// RsyncCall is one recorded invocation of the fake rsync.
type RsyncCall struct {
	Dir  string
	Argv []string
}

const fakeRsyncScript = `#!/bin/sh
for a; do host="$a"; done
{
  printf '%s' "$PWD"
  for a; do printf '\037%s' "$a"; done
  printf '\n'
} >> "$FAKE_RSYNC_CTL/calls"
for a; do
  case "$a" in
    --files-from=:*) ;;
    --files-from=*) f="${a#--files-from=}"; [ -f "$f" ] && cp "$f" "$FAKE_RSYNC_CTL/$host.list" ;;
  esac
done
[ -f "$FAKE_RSYNC_CTL/$host.stderr" ] && cat "$FAKE_RSYNC_CTL/$host.stderr" >&2
if [ -f "$FAKE_RSYNC_CTL/$host.sleep" ]; then sleep "$(cat "$FAKE_RSYNC_CTL/$host.sleep")"; fi
if [ -f "$FAKE_RSYNC_CTL/$host.exit" ]; then exit "$(cat "$FAKE_RSYNC_CTL/$host.exit")"; fi
exit 0
`

// NewFakeRsync installs a fake rsync on PATH for the duration of the test.
// The fake treats its last argument as the host.
func NewFakeRsync(t *testing.T) *FakeRsync {
	t.Helper()
	f := &FakeRsync{BinDir: t.TempDir(), CtlDir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(f.BinDir, "rsync"), []byte(fakeRsyncScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", f.BinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_RSYNC_CTL", f.CtlDir)
	return f
}

// SetHost configures the fake's behaviour for one host.
func (f *FakeRsync) SetHost(host string, exit int, stderr string, sleep time.Duration) {
	write := func(ext, s string) {
		if err := os.WriteFile(filepath.Join(f.CtlDir, host+"."+ext), []byte(s), 0o644); err != nil {
			panic(err)
		}
	}
	write("exit", strconv.Itoa(exit))
	if stderr != "" {
		write("stderr", stderr)
	}
	if sleep > 0 {
		write("sleep", fmt.Sprintf("%.3f", sleep.Seconds()))
	}
}

// Calls returns the recorded invocations in order.
func (f *FakeRsync) Calls() []RsyncCall { return readCalls(filepath.Join(f.CtlDir, "calls")) }

// List returns the lines of the local --files-from file given to the fake
// for host on its last call, or nil if it was given none. The fake copies
// the file when it runs, so this works after the caller has removed it.
func (f *FakeRsync) List(host string) []string {
	data, err := os.ReadFile(filepath.Join(f.CtlDir, host+".list"))
	if err != nil || len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func readCalls(path string) []RsyncCall {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var calls []RsyncCall
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x1f")
		calls = append(calls, RsyncCall{Dir: parts[0], Argv: parts[1:]})
	}
	return calls
}

// FakeSSH is a stand-in ssh binary placed first on PATH. By default it runs
// the remote command locally with sh -c, as sshd would hand it to the login
// shell, so a probe sees real local files. SetHost overrides that per host.
//
// The remote command runs with $CtlDir/bin first on PATH (see SetPasswd).
// If FAKE_SSH_SHELL is set to a shell's path, a symlink named sh to it is
// put before that, so both the login shell and any "sh" the command runs
// are that shell, invoked as sh.
type FakeSSH struct{ BinDir, CtlDir string }

// SSHCall is one recorded invocation of the fake ssh.
type SSHCall = RsyncCall

const fakeSSHScript = `#!/bin/sh
{
  printf '%s' "$PWD"
  for a; do printf '\037%s' "$a"; done
  printf '\n'
} >> "$FAKE_SSH_CTL/calls"
while [ $# -gt 0 ]; do
  case "$1" in
    -[BbcDEeFIiJLlmOoPpQRSWw]) shift 2 ;;
    -*) shift ;;
    *) break ;;
  esac
done
host="${1#*@}"
shift
ctl="$FAKE_SSH_CTL/$host"
if [ -f "$ctl.exit" ]; then
  [ -f "$ctl.stderr" ] && cat "$ctl.stderr" >&2
  if [ -f "$ctl.sleep" ]; then sleep "$(cat "$ctl.sleep")"; fi
  [ -f "$ctl.stdout" ] && cat "$ctl.stdout"
  exit "$(cat "$ctl.exit")"
fi
PATH="$FAKE_SSH_CTL/bin:$PATH"
if [ -n "$FAKE_SSH_SHELL" ]; then
  d="$FAKE_SSH_CTL/shell-${FAKE_SSH_SHELL##*/}"
  [ -e "$d/sh" ] || { mkdir -p "$d" && ln -sf "$FAKE_SSH_SHELL" "$d/sh"; }
  PATH="$d:$PATH"
fi
export PATH
exec sh -c "$*"
`

// NewFakeSSH installs a fake ssh on PATH for the duration of the test. The
// fake skips options, takes the next argument as [user@]host and joins the
// rest into the remote command, as ssh does.
func NewFakeSSH(t *testing.T) *FakeSSH {
	t.Helper()
	f := &FakeSSH{BinDir: t.TempDir(), CtlDir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(f.BinDir, "ssh"), []byte(fakeSSHScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", f.BinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_SSH_CTL", f.CtlDir)
	return f
}

// SetHost makes the fake, for one host name (without user@), print stderr,
// sleep, print stdout and exit with exit instead of running the command.
func (f *FakeSSH) SetHost(host string, exit int, stdout, stderr string, sleep time.Duration) {
	write := func(ext, s string) {
		if err := os.WriteFile(filepath.Join(f.CtlDir, host+"."+ext), []byte(s), 0o644); err != nil {
			panic(err)
		}
	}
	write("exit", strconv.Itoa(exit))
	if stdout != "" {
		write("stdout", stdout)
	}
	if stderr != "" {
		write("stderr", stderr)
	}
	if sleep > 0 {
		write("sleep", fmt.Sprintf("%.3f", sleep.Seconds()))
	}
}

// SetPasswd installs a fake getent in $CtlDir/bin, seen by the remote
// command, that prints passwd for "getent passwd" and exits 2 otherwise.
func (f *FakeSSH) SetPasswd(t *testing.T, passwd string) {
	t.Helper()
	WriteFile(t, f.CtlDir, "passwd", passwd)
	f.writeGetent(t, "[ \"$1\" = passwd ] || exit 2\ncat \"$FAKE_SSH_CTL/passwd\"\n")
}

// SetPasswdFails installs a fake getent that always exits 2, so the remote
// command falls back to /etc/passwd.
func (f *FakeSSH) SetPasswdFails(t *testing.T) {
	t.Helper()
	f.writeGetent(t, "exit 2\n")
}

func (f *FakeSSH) writeGetent(t *testing.T, body string) {
	t.Helper()
	bin := filepath.Join(f.CtlDir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "getent"), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Calls returns the recorded invocations in order.
func (f *FakeSSH) Calls() []SSHCall { return readCalls(filepath.Join(f.CtlDir, "calls")) }

// WriteFile writes content to dir/name and returns its path.
func WriteFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// WriteHook writes an executable script dir/name. It first appends
// "<name> <args...> cwd=<PWD>" to dir/calls.log, then runs body.
func WriteHook(t *testing.T, dir, name, body string) string {
	t.Helper()
	script := "#!/bin/sh\necho \"" + name + " $* cwd=$PWD\" >> \"" +
		filepath.Join(dir, "calls.log") + "\"\n" + body + "\n"
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// HookLog returns the lines of dir/calls.log.
func HookLog(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "calls.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}
