package main

import (
	"bytes"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gavincarr/extract/internal/testutil"
)

// rootDir returns a new EXTRACT_ROOT with mode 0700. t.TempDir's
// subdirectories follow the umask, which would trigger the permission
// warning.
func rootDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

// writeConf writes a tree-mode config and returns its path.
func writeConf(t *testing.T, extra string) string {
	t.Helper()
	root := rootDir(t)
	body := "EXTRACT_ROOT=" + root + "\nEXTRACT_TREE=/etc\n" + extra
	p := filepath.Join(t.TempDir(), "extract.conf")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runCLI(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func TestE2EHealthyIsSilent(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\n")
	code, out, errs := runCLI("-c", conf)
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if n := len(f.Calls()); n != 2 {
		t.Fatalf("rsync called %d times, want 2", n)
	}
}

func TestE2EVerboseSummary(t *testing.T) {
	testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\n")
	code, out, errs := runCLI("-c", conf, "-v")
	if code != 0 || out != "2 hosts ok\n" || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestE2EFailureExits1(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	f.SetHost("h2", 23, "boom", 0)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\n")
	code, out, errs := runCLI("-c", conf)
	if code != 1 || out != "" || !strings.Contains(errs, "h2: rsync exited 23") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestE2EConfigErrorExits2(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	code, out, errs := runCLI("-c", "/nonexistent/extract.conf")
	if code != 2 || out != "" || !strings.HasPrefix(errs, "extract: ") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("rsync called %d times", n)
	}
}

func TestE2ERsyncMissingExits2(t *testing.T) {
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1\"\n")
	t.Setenv("PATH", t.TempDir())
	code, out, errs := runCLI("-c", conf)
	if code != 2 || out != "" || !strings.Contains(errs, "rsync not found") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestE2ENegativeTimeoutExits2(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1\"\n")
	code, out, errs := runCLI("-c", conf, "--timeout=-1")
	if code != 2 || out != "" || !strings.HasPrefix(errs, "extract: ") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("rsync called %d times", n)
	}
}

func TestE2ENoopPrintsPlan(t *testing.T) {
	testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\n")
	code, out, errs := runCLI("-c", conf, "-n")
	if code != 0 || errs != "" ||
		!strings.Contains(out, "would rsync:") || !strings.Contains(out, "--dry-run") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestE2ENoopSkipsHooksAndDryRuns(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	hooks := t.TempDir()
	hook := testutil.WriteHook(t, hooks, "post", "exit 0")
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\nPOST_EXTRACT_HOST="+hook+"\n")
	code, out, errs := runCLI("-c", conf, "-n")
	if code != 0 || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	calls := f.Calls()
	if len(calls) == 0 {
		t.Fatal("no rsync calls")
	}
	for _, c := range calls {
		if !slices.Contains(c.Argv, "--dry-run") {
			t.Errorf("argv lacks --dry-run: %q", c.Argv)
		}
	}
	if l := testutil.HookLog(t, hooks); len(l) != 0 {
		t.Errorf("hook ran: %q", l)
	}
	if !strings.Contains(out, "would run post_extract_host:") {
		t.Errorf("stdout = %q", out)
	}
}

func TestE2ETimeoutFlagPassedThrough(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	f.SetHost("h1", 0, "", 3*time.Second)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1\"\n")
	start := time.Now()
	code, _, errs := runCLI("-c", conf, "-t", "1")
	if d := time.Since(start); d > 2800*time.Millisecond {
		t.Errorf("took %v", d)
	}
	if code != 1 || !strings.Contains(errs, "h1: rsync timed out after 1s") {
		t.Fatalf("code=%d stderr=%q", code, errs)
	}
}

func TestE2ETimeoutZeroMeansNone(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	f.SetHost("h1", 0, "", 1500*time.Millisecond)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1\"\n")
	code, out, errs := runCLI("-c", conf, "-t", "0")
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestE2EHostArgsOverrideConfig(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\n")
	code, out, errs := runCLI("-c", conf, "h9")
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d rsync calls, want 1: %v", len(calls), calls)
	}
	if last := calls[0].Argv[len(calls[0].Argv)-1]; last != "h9" {
		t.Errorf("dest = %q, want h9", last)
	}
}

func TestE2EHostCmdFailureExits2(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOST_CMD=\"exit 3\"\n")
	code, out, errs := runCLI("-c", conf)
	if code != 2 || out != "" || !strings.HasPrefix(errs, "extract: ") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("rsync called %d times", n)
	}
}

func TestE2EHostsMergedWithHostCmd(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\nEXTRACT_HOST_CMD=\"echo h2 h3\"\n")
	code, out, errs := runCLI("-c", conf)
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if n := len(f.Calls()); n != 3 {
		t.Fatalf("got %d rsync calls, want 3", n)
	}
}

func TestE2EEmptyHostCmdWithHostsExits2(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1\"\nEXTRACT_HOST_CMD=\"true\"\n")
	code, out, errs := runCLI("-c", conf)
	if code != 2 || out != "" || errs != "extract: host command returned no hosts\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("rsync called %d times", n)
	}
}

func TestE2EInvalidHostExits2(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1\"\n")
	code, out, errs := runCLI("-c", conf, "--", "-x")
	if code != 2 || out != "" || !strings.Contains(errs, "-x") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("rsync called %d times", n)
	}
}

func TestE2ESIGTERMInterruptsRun(t *testing.T) {
	// Keep the test process alive whether or not run() handles the signal.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	f := testutil.NewFakeRsync(t)
	f.SetHost("h1", 0, "", 10*time.Second)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\n")
	go func() {
		// Wait for the fake rsync to start, so run() is mid-step.
		for i := 0; i < 500 && len(f.Calls()) == 0; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	start := time.Now()
	code, out, errs := runCLI("-c", conf)
	if d := time.Since(start); d > 6*time.Second {
		t.Fatalf("took too long: %v", d)
	}
	if code != 1 || out != "" || errs != "h1: rsync interrupted\nrun interrupted: 1 of 2 hosts attempted\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestInterruptSignalsSkipsIgnored(t *testing.T) {
	all := []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
	got := interruptSignals(func(s os.Signal) bool { return false })
	if !reflect.DeepEqual(got, all) {
		t.Errorf("none ignored: got %v", got)
	}
	got = interruptSignals(func(s os.Signal) bool { return s == syscall.SIGHUP })
	if want := []os.Signal{os.Interrupt, syscall.SIGTERM}; !reflect.DeepEqual(got, want) {
		t.Errorf("SIGHUP ignored: got %v, want %v", got, want)
	}
	got = interruptSignals(func(s os.Signal) bool { return true })
	if len(got) != 0 {
		t.Errorf("all ignored: got %v", got)
	}
}

func TestSignalContextAllIgnoredIsNeverCancelled(t *testing.T) {
	// signal.NotifyContext with no signals would relay every signal; make
	// sure that case yields a plain, uncancellable context instead.
	ctx, stop := signalContext(func(os.Signal) bool { return true })
	defer stop()
	if ctx.Done() != nil {
		t.Error("context with no signals should never be cancelled")
	}
}

func TestE2EIgnoredSIGHUPDoesNotInterrupt(t *testing.T) {
	// Simulate `nohup extract`: SIGHUP is ignored before run() starts.
	wasIgnored := signal.Ignored(syscall.SIGHUP)
	signal.Ignore(syscall.SIGHUP)
	if !wasIgnored {
		defer signal.Reset(syscall.SIGHUP)
	}

	f := testutil.NewFakeRsync(t)
	f.SetHost("h1", 0, "", 1*time.Second)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\n")
	go func() {
		for i := 0; i < 500 && len(f.Calls()) == 0; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	}()
	code, out, errs := runCLI("-c", conf)
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if n := len(f.Calls()); n != 2 {
		t.Errorf("rsync calls = %d, want 2", n)
	}
}

func TestE2ESIGTERMDuringHostCmd(t *testing.T) {
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	f := testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOST_CMD=\"sleep 5; echo h1\"\n")
	time.AfterFunc(300*time.Millisecond, func() { _ = syscall.Kill(os.Getpid(), syscall.SIGTERM) })
	start := time.Now()
	code, out, errs := runCLI("-c", conf)
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("took too long: %v", d)
	}
	if code != 1 || out != "" || errs != "extract: host command interrupted: context canceled\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("rsync calls = %d, want 0", n)
	}
}

func TestE2EExplicitSyncSubcommand(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\n")
	code, out, errs := runCLI("sync", "-c", conf, "-v", "h2")
	if code != 0 || out != "1 host ok\n" || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Argv[len(calls[0].Argv)-1] != "h2" {
		t.Errorf("calls = %+v", calls)
	}
}

// writeRemoteListsConf writes a config whose EXTRACT_FILES_REMOTE lists a
// missing file, an empty file and a non-empty one, and returns its path and
// the non-empty file's path.
func writeRemoteListsConf(t *testing.T, extra string) (conf, full string) {
	t.Helper()
	d := t.TempDir()
	missing := filepath.Join(d, "missing.txt")
	empty := testutil.WriteFile(t, d, "empty.txt", "")
	full = testutil.WriteFile(t, d, "deb.txt", "/etc/hosts\n")
	body := "EXTRACT_ROOT=" + rootDir(t) + "\nEXTRACT_FILES_REMOTE=\"" + missing + " " + empty + " " + full + "\"\n" + extra
	conf = filepath.Join(t.TempDir(), "extract.conf")
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return conf, full
}

func TestE2EFilesRemoteProbe(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	s := testutil.NewFakeSSH(t)
	hooks := t.TempDir()
	hook := testutil.WriteHook(t, hooks, "post", "exit 0")
	conf, full := writeRemoteListsConf(t, "EXTRACT_HOSTS=\"h1 u@h2\"\nPOST_EXTRACT_HOST="+hook+"\n")

	code, out, errs := runCLI("-c", conf)
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if n := len(s.Calls()); n != 2 {
		t.Errorf("ssh calls = %d, want 2", n)
	}
	for _, c := range f.Calls() {
		if !slices.Contains(c.Argv, "--files-from=:"+full) {
			t.Errorf("rsync argv %q", c.Argv)
		}
	}
	for _, l := range testutil.HookLog(t, hooks) {
		if !strings.Contains(l, " "+full+" cwd=") {
			t.Errorf("hook $4 not the chosen list: %q", l)
		}
	}

	// -n still probes, and the plan shows the choice.
	code, out, errs = runCLI("-c", conf, "-n", "h1")
	if code != 0 || errs != "" ||
		!strings.Contains(out, "checked files lists: ssh -o BatchMode=yes h1 'for f in ") ||
		!strings.Contains(out, " -> "+full+"\n") ||
		!strings.Contains(out, "--files-from=:"+full+" h1:/ h1\n") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestE2EFilesRemoteProbeFailure(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	s := testutil.NewFakeSSH(t)
	s.SetHost("h1", 255, "", "Permission denied (publickey).\n", 0)
	conf, _ := writeRemoteListsConf(t, "EXTRACT_HOSTS=\"h1 h2\"\n")
	code, out, errs := runCLI("-c", conf, "-v")
	if code != 1 || out != "1 ok, 1 failed, 0 timed out\n" ||
		errs != "h1: files list probe exited 255\n  Permission denied (publickey).\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if calls := f.Calls(); len(calls) != 1 || calls[0].Argv[len(calls[0].Argv)-1] != "h2" {
		t.Errorf("rsync calls %+v", calls)
	}
}

func TestE2EPermWarning(t *testing.T) {
	testutil.NewFakeRsync(t)
	root := rootDir(t)
	conf := testutil.WriteFile(t, t.TempDir(), "extract.conf",
		"EXTRACT_ROOT="+root+"\nEXTRACT_TREE=/etc\nEXTRACT_HOSTS=h1\n")

	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runCLI("-c", conf)
	if code != 0 || out != "" ||
		!strings.Contains(errs, "warning: EXTRACT_ROOT "+root+" is accessible by group or others (mode 0755)") {
		t.Fatalf("0755: code=%d stdout=%q stderr=%q", code, out, errs)
	}
	// Printed under -n too, and the run still happens.
	code, out, errs = runCLI("-c", conf, "-n")
	if code != 0 || !strings.Contains(out, "would rsync:") ||
		!strings.Contains(errs, "is accessible by group or others (mode 0755)") {
		t.Fatalf("0755 -n: code=%d stdout=%q stderr=%q", code, out, errs)
	}

	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	code, out, errs = runCLI("-c", conf)
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("0700: code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

// requireRsync31 skips the test unless a real rsync >= 3.1 is on PATH.
func requireRsync31(t *testing.T) {
	t.Helper()
	b, err := exec.Command("rsync", "--version").Output()
	if err != nil {
		t.Skipf("no rsync: %v", err)
	}
	m := regexp.MustCompile(`version\s+(\d+)\.(\d+)`).FindSubmatch(b)
	if m == nil {
		t.Skipf("can't parse rsync version from %q", b)
	}
	major, _ := strconv.Atoi(string(m[1]))
	minor, _ := strconv.Atoi(string(m[2]))
	if major < 3 || major == 3 && minor < 1 {
		t.Skipf("rsync %d.%d < 3.1", major, minor)
	}
	t.Logf("using rsync %d.%d", major, minor)
}

func TestE2EGlobsRealRsync(t *testing.T) {
	requireRsync31(t)
	// No fake rsync: the real one runs the fake ssh, which runs
	// "rsync --server ..." locally.
	s := testutil.NewFakeSSH(t)
	home := filepath.Join(t.TempDir(), "home")
	for _, d := range []string{".ssh", ".d"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.SetPasswd(t, "bob:x:1000:1000:Bob:"+home+":/bin/sh\n")
	keys := testutil.WriteFile(t, filepath.Join(home, ".ssh"), "authorized_keys", "ssh-ed25519 AAAA bob\n")
	other := testutil.WriteFile(t, filepath.Join(home, ".d"), "f", "gone with its directory\n")

	root := rootDir(t)
	hooks := t.TempDir()
	// Appends, so a copy deleted and recreated would hold only one line.
	hook := testutil.WriteHook(t, hooks, "post", `echo tarball >> "$1/$3/keep.tar.gz"`)
	conf := testutil.WriteFile(t, t.TempDir(), "extract.conf",
		"EXTRACT_ROOT="+root+"\nEXTRACT_HOSTS=localhost\n"+
			"EXTRACT_FILES_GLOBS=\"~/.ssh/* ~/.d/*\"\nPOST_EXTRACT_HOST="+hook+"\n")
	local := func(p string) string { return filepath.Join(root, "localhost", p) }
	keep := filepath.Join(root, "localhost", "keep.tar.gz")

	code, out, errs := runCLI("-c", conf)
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("run 1: code=%d stdout=%q stderr=%q", code, out, errs)
	}
	for _, p := range []string{keys, other} {
		if b, err := os.ReadFile(local(p)); err != nil {
			t.Fatalf("run 1: %v", err)
		} else if want, _ := os.ReadFile(p); string(b) != string(want) {
			t.Errorf("run 1: %s = %q, want %q", local(p), b, want)
		}
	}

	// -n builds the list and shows the plan, but changes nothing. The plan
	// shows the list step without its script; -vvv shows that once.
	code, out, errs = runCLI("-c", conf, "-n")
	if code != 0 || errs != "" ||
		!regexp.MustCompile(`(?m)^built files list: ssh -o BatchMode=yes localhost sh -c <list script, \d+ bytes> -> 0 listed, 2 globbed, 2 tracked, 0 missing, 0 skipped$`).MatchString(out) ||
		strings.Contains(out, "chk() {") || !strings.Contains(out, "--delete-missing-args") {
		t.Fatalf("noop: code=%d stdout=%q stderr=%q", code, out, errs)
	}
	code, out, errs = runCLI("-c", conf, "-n", "-vvv")
	if code != 0 || errs != "" || strings.Count(out, "files list script: 'nl=") != 1 ||
		strings.Count(out, "chk() {") != 1 || !strings.Contains(out, "+ ssh -o BatchMode=yes localhost sh -c <list script, ") {
		t.Fatalf("noop -vvv: code=%d stdout=%q stderr=%q", code, out, errs)
	}

	// Remove the key, and a tracked file together with its directory.
	if err := os.Remove(keys); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(other)); err != nil {
		t.Fatal(err)
	}
	code, out, errs = runCLI("-c", conf)
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("run 2: code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if _, err := os.Lstat(local(keys)); !os.IsNotExist(err) {
		t.Errorf("local copy of deleted key kept: %v", err)
	}
	if _, err := os.Lstat(local(other)); !os.IsNotExist(err) {
		t.Errorf("local copy of file in deleted directory kept: %v", err)
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "tarball\ntarball\n" {
		t.Errorf("hook-created %s: %q, %v", keep, b, err)
	}
}

// A remote user can create a file whose name holds a carriage return.
// rsync's --files-from treats CR as a line end, so "x\r.git" would reach
// it as "/home/bob/.ssh/x" and ".git"; both are missing remotely, and
// --delete-missing-args would delete <root>/<host>/.git. Such names must
// never reach rsync.
func TestE2EGlobsCRNameRealRsync(t *testing.T) {
	requireRsync31(t)
	s := testutil.NewFakeSSH(t)
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.SetPasswd(t, "bob:x:1000:1000:Bob:"+home+":/bin/sh\n")
	keys := testutil.WriteFile(t, filepath.Join(home, ".ssh"), "authorized_keys", "ssh-ed25519 AAAA bob\n")
	testutil.WriteFile(t, filepath.Join(home, ".ssh"), "x\r.git", "evil\n")
	testutil.WriteFile(t, filepath.Join(home, ".ssh"), "y\rkeep.tar.gz", "evil\n")

	root := rootDir(t)
	hostDir := filepath.Join(root, "localhost")
	if err := os.MkdirAll(filepath.Join(hostDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	head := testutil.WriteFile(t, filepath.Join(hostDir, ".git"), "HEAD", "ref: refs/heads/master\n")
	hooks := t.TempDir()
	// Runs before rsync, so the file is there for rsync to delete.
	hook := testutil.WriteHook(t, hooks, "pre", `echo tarball >> "$1/$3/keep.tar.gz"`)
	conf := testutil.WriteFile(t, t.TempDir(), "extract.conf",
		"EXTRACT_ROOT="+root+"\nEXTRACT_HOSTS=localhost\n"+
			"EXTRACT_FILES_GLOBS=\"~/.ssh/*\"\nPRE_EXTRACT_HOST="+hook+"\n")

	code, out, errs := runCLI("-c", conf, "-v")
	if code != 0 || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	for _, w := range []string{
		"localhost: warning: skipped name containing a newline or carriage return " + home + "/.ssh/x?.git\n",
		"localhost: warning: skipped name containing a newline or carriage return " + home + "/.ssh/y?keep.tar.gz\n",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("stdout lacks %q:\n%s", w, out)
		}
	}
	if b, err := os.ReadFile(head); err != nil || string(b) != "ref: refs/heads/master\n" {
		t.Errorf("%s: %q, %v", head, b, err)
	}
	keep := filepath.Join(hostDir, "keep.tar.gz")
	if b, err := os.ReadFile(keep); err != nil || string(b) != "tarball\n" {
		t.Errorf("hook-created %s: %q, %v", keep, b, err)
	}
	if _, err := os.Stat(filepath.Join(hostDir, keys)); err != nil {
		t.Errorf("authorized_keys not extracted: %v", err)
	}
	m, err := os.ReadFile(filepath.Join(root, ".extract-state", "localhost.files"))
	if err != nil || strings.ContainsAny(string(m), "\r") {
		t.Errorf("manifest %q, %v", m, err)
	}
}

func TestE2EGlobsManifestUnreadable(t *testing.T) {
	f := testutil.NewFakeRsync(t)
	s := testutil.NewFakeSSH(t)
	s.SetPasswd(t, "")
	root := rootDir(t)
	if err := os.MkdirAll(filepath.Join(root, ".extract-state", "h1.files"), 0o700); err != nil {
		t.Fatal(err)
	}
	hooks := t.TempDir()
	hook := testutil.WriteHook(t, hooks, "post", "exit 0")
	conf := testutil.WriteFile(t, t.TempDir(), "extract.conf",
		"EXTRACT_ROOT="+root+"\nEXTRACT_HOSTS=\"h1 h2\"\nEXTRACT_FILES_GLOBS=\"~/.ssh/*\"\nPOST_EXTRACT_HOST="+hook+"\n")
	code, out, errs := runCLI("-c", conf, "-n")
	if code != 1 || !strings.HasPrefix(out, "built files list: h1 (failed: cannot read manifest)\nbuilt files list: ssh ") ||
		strings.Count(out, "would run post_extract_host:") != 1 ||
		!strings.HasPrefix(errs, "h1: cannot read manifest: ") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if calls := f.Calls(); len(calls) != 1 || calls[0].Argv[len(calls[0].Argv)-1] != "h2" {
		t.Errorf("rsync calls %+v", calls)
	}
}
