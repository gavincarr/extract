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
func (f *FakeRsync) Calls() []RsyncCall {
	data, err := os.ReadFile(filepath.Join(f.CtlDir, "calls"))
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
