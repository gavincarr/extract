package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/gavincarr/extract/internal/extract"
	"github.com/gavincarr/extract/internal/proc"
)

var rsyncArgv = []string{"rsync", "--archive", "--copy-links", "--delete", "--rsh=ssh -o BatchMode=yes", "--quiet", "h1:/etc", "h1"}

func host(n string) extract.Host { return extract.Host{Name: n} }

func okHost(n string) extract.HostResult {
	return extract.HostResult{Host: host(n), Duration: 1234 * time.Millisecond,
		Rsync: &proc.StepResult{Argv: rsyncArgv, Stdout: "sent 12 bytes"}}
}

func run(r *extract.Result, v int) (string, string) {
	var o, e bytes.Buffer
	Report(NewLogger(&o, &e, v), r)
	return o.String(), e.String()
}

func contains(t *testing.T, name, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s missing %q in %q", name, w, got)
		}
	}
}

func TestReportHealthy(t *testing.T) {
	r := &extract.Result{Hosts: []extract.HostResult{okHost("h1"), okHost("h2")}}
	o, e := run(r, 0)
	if o != "" || e != "" {
		t.Errorf("v0: %q %q", o, e)
	}
	o, e = run(r, 1)
	if o != "2 hosts ok\n" || e != "" {
		t.Errorf("v1: %q %q", o, e)
	}
	o, e = run(r, 2)
	contains(t, "v2 stdout", o, "h1 ok (1.2s)", "2 hosts ok")
	if e != "" {
		t.Errorf("v2 stderr: %q", e)
	}
	o, e = run(r, 3)
	contains(t, "v3 stdout", o, "+ rsync --archive --copy-links --delete '--rsh=ssh -o BatchMode=yes'", "  sent 12 bytes")
	if e != "" {
		t.Errorf("v3 stderr: %q", e)
	}
	o, _ = run(&extract.Result{Hosts: []extract.HostResult{okHost("h1")}}, 1)
	if o != "1 host ok\n" {
		t.Errorf("singular: %q", o)
	}
}

func failedRun() *extract.Result {
	return &extract.Result{Hosts: []extract.HostResult{
		okHost("h1"),
		{Host: host("h2"), Status: extract.StatusFailed, Error: "rsync exited 23",
			Rsync: &proc.StepResult{ExitCode: 23, Stderr: "rsync: link_stat x failed\n"}},
		{Host: host("h3"), Status: extract.StatusTimedOut, Error: "rsync timed out after 1m0s",
			Rsync: &proc.StepResult{ExitCode: -1, TimedOut: true}},
	}}
}

func failedPair(withArgv bool) *extract.Result {
	h1 := okHost("h1")
	h2 := extract.HostResult{Host: host("h2"), Status: extract.StatusFailed, Error: "rsync exited 23",
		Rsync: &proc.StepResult{ExitCode: 23, Stderr: "rsync: link_stat x failed"}}
	if withArgv {
		h2.Rsync.Argv = []string{"rsync", "--archive", "--copy-links", "--delete", "--rsh=ssh -o BatchMode=yes", "--quiet", "h2:/etc", "h2"}
	}
	return &extract.Result{Hosts: []extract.HostResult{h1, h2}}
}

func TestReportFailuresVV(t *testing.T) {
	o, e := run(failedPair(false), 2)
	contains(t, "stdout", o, "h1 ok (1.2s)")
	if strings.Contains(o, "h2 ok") {
		t.Errorf("stdout reports h2 ok: %q", o)
	}
	contains(t, "stderr", e, "h2: rsync exited 23")
}

func TestReportFailuresVVV(t *testing.T) {
	o, e := run(failedPair(true), 3)
	contains(t, "stdout", o, "+ rsync --archive --copy-links --delete '--rsh=ssh -o BatchMode=yes' --quiet h2:/etc h2")
	contains(t, "stderr", e, "h2: rsync exited 23\n  rsync: link_stat x failed\n")
}

func TestReportFailures(t *testing.T) {
	o, e := run(failedRun(), 0)
	if o != "" {
		t.Errorf("stdout: %q", o)
	}
	contains(t, "stderr", e, "h2: rsync exited 23\n  rsync: link_stat x failed\n", "h3: rsync timed out after 1m0s\n")
	o, e2 := run(failedRun(), 1)
	if o != "1 ok, 1 failed, 1 timed out\n" || e2 != e {
		t.Errorf("v1: %q %q", o, e2)
	}
}

func TestReportHookFailureOutput(t *testing.T) {
	r := &extract.Result{Hosts: []extract.HostResult{{
		Host: host("h1"), Status: extract.StatusFailed, Error: "post_extract_host hook git exited 1",
		Rsync: &proc.StepResult{},
		PostHooks: []extract.HookResult{{Stage: "post_extract_host", Path: "/s/git",
			Step: &proc.StepResult{ExitCode: 1, Stdout: "nothing to commit\n"}}},
	}}}
	o, e := run(r, 0)
	if o != "" {
		t.Errorf("stdout: %q", o)
	}
	contains(t, "stderr", e, "h1: post_extract_host hook git exited 1\n  nothing to commit\n")
}

func TestReportWarning(t *testing.T) {
	r := &extract.Result{Hosts: []extract.HostResult{{Host: host("h1"), Rsync: &proc.StepResult{ExitCode: 24},
		Warnings: []string{"some files vanished during transfer (rsync exit 24)"}}}}
	o, e := run(r, 0)
	if o != "" || e != "" {
		t.Errorf("v0: %q %q", o, e)
	}
	o, e = run(r, 1)
	contains(t, "stdout", o, "h1: warning: some files vanished during transfer (rsync exit 24)\n")
	if e != "" {
		t.Errorf("stderr: %q", e)
	}
}

func TestReportAborted(t *testing.T) {
	r := &extract.Result{Aborted: true, SetupHooks: []extract.HookResult{{Stage: "pre_extract_setup", Path: "/s/keys",
		Step: &proc.StepResult{ExitCode: 99, Stderr: "boom\n"}}}}
	o, e := run(r, 0)
	if o != "" {
		t.Errorf("stdout: %q", o)
	}
	contains(t, "stderr", e, "pre_extract_setup hook /s/keys exited 99\n  boom\n", "run aborted: no hosts extracted\n")
	o, e2 := run(r, 1)
	if o != "" || e2 != e {
		t.Errorf("v1: %q %q", o, e2)
	}
}

func TestReportTeardownFailure(t *testing.T) {
	r := &extract.Result{TeardownHooks: []extract.HookResult{{Stage: "post_extract_teardown", Path: "/s/t",
		Step: &proc.StepResult{ExitCode: 1}}}}
	o, e := run(r, 0)
	if o != "" {
		t.Errorf("stdout: %q", o)
	}
	contains(t, "stderr", e, "post_extract_teardown hook /s/t exited 1\n")
}

func TestReportTeardownFailureSummary(t *testing.T) {
	r := &extract.Result{Hosts: []extract.HostResult{okHost("h1"), okHost("h2")},
		TeardownHooks: []extract.HookResult{{Stage: "post_extract_teardown", Path: "/s/t",
			Step: &proc.StepResult{ExitCode: 1}}}}
	o, e := run(r, 1)
	if o != "2 ok, 0 failed, 0 timed out\n" {
		t.Errorf("stdout: %q", o)
	}
	contains(t, "stderr", e, "post_extract_teardown hook /s/t exited 1\n")
}

func TestReportTeardownTimeout(t *testing.T) {
	r := &extract.Result{TeardownHooks: []extract.HookResult{{Stage: "post_extract_teardown", Path: "/s/td",
		Timeout: 10 * time.Minute, Step: &proc.StepResult{ExitCode: -1, TimedOut: true}}}}
	o, e := run(r, 0)
	if o != "" {
		t.Errorf("stdout: %q", o)
	}
	contains(t, "stderr", e, "post_extract_teardown hook /s/td timed out after 10m0s\n")
}

func TestPrintPlan(t *testing.T) {
	args := []string{"/root", "/cfg"}
	r := &extract.Result{Noop: true,
		SetupHooks: []extract.HookResult{{Stage: "pre_extract_setup", Path: "/s/setup", Args: args}},
		Hosts:      []extract.HostResult{{Host: host("h1"), Rsync: &proc.StepResult{Argv: append(rsyncArgv[:5:5], "--dry-run", "h1:/etc", "h1")}}}}
	var b bytes.Buffer
	PrintPlan(&b, r)
	want := "would run pre_extract_setup: /s/setup /root /cfg\n" +
		"would rsync: rsync --archive --copy-links --delete '--rsh=ssh -o BatchMode=yes' --dry-run h1:/etc h1\n"
	if b.String() != want {
		t.Errorf("got %q want %q", b.String(), want)
	}
}

func TestExitCode(t *testing.T) {
	if ExitCode(&extract.Result{Hosts: []extract.HostResult{okHost("h1")}}) != 0 {
		t.Error("ok != 0")
	}
	bad := []*extract.Result{
		failedRun(),
		{Aborted: true},
		{SetupHooks: []extract.HookResult{{Step: &proc.StepResult{ExitCode: 1}}}},
		{TeardownHooks: []extract.HookResult{{Step: &proc.StepResult{ExitCode: 1}}}},
	}
	for i, r := range bad {
		if ExitCode(r) != 1 {
			t.Errorf("case %d != 1", i)
		}
	}
}

func TestReportPostHookFailureAfterRsyncFailure(t *testing.T) {
	r := &extract.Result{Hosts: []extract.HostResult{{
		Host: host("h1"), Status: extract.StatusFailed, Error: "rsync exited 23",
		Rsync: &proc.StepResult{ExitCode: 23, Stderr: "link_stat x\n"},
		PostHooks: []extract.HookResult{{Stage: "post_extract_host", Path: "/s/git",
			Step: &proc.StepResult{ExitCode: 1, Stdout: "fatal: bad object\n"}}},
	}}}
	o, e := run(r, 0)
	if o != "" {
		t.Errorf("stdout: %q", o)
	}
	contains(t, "stderr", e, "h1: rsync exited 23", "link_stat x",
		"then post_extract_host hook git exited 1", "fatal: bad object")
	contains(t, "stderr", e, "h1: rsync exited 23\n  link_stat x\n  then post_extract_host hook git exited 1\n    fatal: bad object\n")
}

func TestReportInterrupted(t *testing.T) {
	r := &extract.Result{Interrupted: true, HostsPlanned: 3, Hosts: []extract.HostResult{
		okHost("h1"),
		{Host: host("h2"), Status: extract.StatusFailed, Error: "rsync interrupted",
			Rsync: &proc.StepResult{ExitCode: -1, Interrupted: true}},
	}}
	o, e := run(r, 0)
	if o != "" {
		t.Errorf("v0 stdout: %q", o)
	}
	if e != "h2: rsync interrupted\nrun interrupted: 2 of 3 hosts attempted\n" {
		t.Errorf("v0 stderr: %q", e)
	}
	o, e2 := run(r, 1)
	if o != "" || e2 != e {
		t.Errorf("v1: %q %q", o, e2)
	}
	if ExitCode(r) != 1 {
		t.Error("exit code != 1")
	}
}

func TestReportInterruptedSetupHook(t *testing.T) {
	r := &extract.Result{Interrupted: true, HostsPlanned: 2, SetupHooks: []extract.HookResult{{
		Stage: "pre_extract_setup", Path: "/s/keys", Step: &proc.StepResult{ExitCode: -1, Interrupted: true}}}}
	_, e := run(r, 0)
	if e != "pre_extract_setup hook /s/keys interrupted\nrun interrupted: 0 of 2 hosts attempted\n" {
		t.Errorf("stderr: %q", e)
	}
}

func probeArgv(h string) []string {
	return extract.ProbeArgs(host(h), []string{"/a", "/b"})
}

const probeCmd = `ssh -o BatchMode=yes h1 'for f in /a /b; do if test -s "$f"; then printf "%s\n" "$f"; exit 0; fi; done; exit 3'`

func TestReportProbeFailure(t *testing.T) {
	r := &extract.Result{Hosts: []extract.HostResult{{
		Host: host("h1"), Status: extract.StatusFailed, Error: "files list probe exited 255",
		Probe: &proc.StepResult{Argv: probeArgv("h1"), ExitCode: 255,
			Stderr: "ssh: connect to host h1 port 22: Connection refused\n"},
		PreHooks: []extract.HookResult{{Stage: "pre_extract_host", Path: "/s/pre"}},
	}}}
	o, e := run(r, 0)
	if o != "" {
		t.Errorf("stdout: %q", o)
	}
	if e != "h1: files list probe exited 255\n  ssh: connect to host h1 port 22: Connection refused\n" {
		t.Errorf("stderr: %q", e)
	}

	r.Hosts[0].Error = "files list probe exited 0 without printing a candidate path"
	r.Hosts[0].Probe = &proc.StepResult{Argv: probeArgv("h1"), Stdout: "Welcome to h1\n"}
	_, e = run(r, 0)
	if e != "h1: files list probe exited 0 without printing a candidate path\n  Welcome to h1\n" {
		t.Errorf("stderr: %q", e)
	}
}

func TestReportProbeTraceVVV(t *testing.T) {
	h := okHost("h1")
	h.Probe = &proc.StepResult{Argv: probeArgv("h1"), Stdout: "/b\n"}
	h.FilesFrom = "/b"
	o, e := run(&extract.Result{Hosts: []extract.HostResult{h}}, 3)
	if e != "" {
		t.Errorf("stderr: %q", e)
	}
	want := "+ " + probeCmd + "\n  /b\n+ rsync "
	if !strings.Contains(o, want) {
		t.Errorf("stdout %q lacks %q", o, want)
	}
}

func TestPrintPlanProbe(t *testing.T) {
	args := []string{"/root", "/cfg", "h1", "/b"}
	ok := extract.HostResult{Host: host("h1"), FilesFrom: "/b",
		Probe:     &proc.StepResult{Argv: probeArgv("h1"), Stdout: "/b\n"},
		PreHooks:  []extract.HookResult{{Stage: "pre_extract_host", Path: "/s/pre", Args: args}},
		Rsync:     &proc.StepResult{Argv: []string{"rsync", "--files-from=:/b", "h1:/", "h1"}},
		PostHooks: []extract.HookResult{{Stage: "post_extract_host", Path: "/s/post", Args: args}}}
	failArgs := []string{"/root", "/cfg", "h2", ""}
	failed := extract.HostResult{Host: host("h2"), Status: extract.StatusFailed,
		Probe:     &proc.StepResult{Argv: probeArgv("h2"), ExitCode: 3},
		PreHooks:  []extract.HookResult{{Stage: "pre_extract_host", Path: "/s/pre", Args: failArgs}},
		PostHooks: []extract.HookResult{{Stage: "post_extract_host", Path: "/s/post", Args: failArgs}}}
	var b bytes.Buffer
	PrintPlan(&b, &extract.Result{Noop: true, Hosts: []extract.HostResult{ok, failed}})
	want := "checked files lists: " + probeCmd + " -> /b\n" +
		"would run pre_extract_host: /s/pre /root /cfg h1 /b\n" +
		"would rsync: rsync --files-from=:/b h1:/ h1\n" +
		"would run post_extract_host: /s/post /root /cfg h1 /b\n" +
		"checked files lists: " + strings.Replace(probeCmd, " h1 ", " h2 ", 1) + " (failed)\n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}

// globsHost is a glob host with user "u", so that lines using the host's
// user@name can be told from ones using its bare name.
func globsHost(n string, g *extract.GlobsResult) extract.HostResult {
	h := okHost(n)
	h.Host.User = "u"
	h.Probe = &proc.StepResult{Argv: []string{"ssh", "-o", "BatchMode=yes", "u@" + n, "sh", "-c", "'echo hi'"}, Stdout: "/etc/a\n"}
	h.Globs = g
	return h
}

// globsCmd is how the list step of globsHost("h1") is shown: the 9-byte
// script "'echo hi'" is replaced by a placeholder.
const globsCmd = "ssh -o BatchMode=yes u@h1 sh -c <list script, 9 bytes>"

func TestReportGlobsNoListAtVV(t *testing.T) {
	r := &extract.Result{Hosts: []extract.HostResult{globsHost("h1", &extract.GlobsResult{NoList: true})}}
	want := "u@h1: no files list found; using globs and tracked files only\n"
	o, _ := run(r, 2)
	contains(t, "v2", o, want)
	o, _ = run(r, 1)
	if strings.Contains(o, "no files list") {
		t.Errorf("v1 shows it: %q", o)
	}
	r.Hosts[0].Globs.NoList = false
	o, _ = run(r, 2)
	if strings.Contains(o, "no files list") {
		t.Errorf("shown without NoList: %q", o)
	}
}

func TestReportGlobsTraceSummary(t *testing.T) {
	g := &extract.GlobsResult{Listed: 3, Globbed: 4, Tracked: 5, Missing: 1, Skipped: []string{"a", "b"}}
	r := &extract.Result{Hosts: []extract.HostResult{globsHost("h1", g)}}
	want := "u@h1: files list: 3 listed, 4 globbed, 5 tracked, 1 missing, 2 skipped\n"
	o, _ := run(r, 3)
	contains(t, "v3", o, want)
	if i, j := strings.Index(o, "+ ssh"), strings.Index(o, want); i < 0 || j < i {
		t.Errorf("summary not after traced step: %q", o)
	}
	o, _ = run(r, 2)
	if strings.Contains(o, "files list:") {
		t.Errorf("v2 shows it: %q", o)
	}
}

func TestPlanGlobsHost(t *testing.T) {
	g := &extract.GlobsResult{Listed: 3, Globbed: 4, Tracked: 5, Missing: 1, Skipped: []string{"a"},
		Deleted: []string{"/root/h1/etc/old", "/root/h1/etc/older"}}
	h := globsHost("h1", g)
	h.PreHooks = []extract.HookResult{{Stage: "pre_extract_host", Path: "/s/pre", Args: []string{"/root", "/cfg", "h1", ""}}}
	var b bytes.Buffer
	PrintPlan(&b, &extract.Result{Noop: true, Hosts: []extract.HostResult{h}})
	want := "built files list: " + globsCmd + " -> 3 listed, 4 globbed, 5 tracked, 1 missing, 1 skipped\n" +
		"would run pre_extract_host: /s/pre /root /cfg h1 ''\n" +
		"would delete local copy: /root/h1/etc/old\n" +
		"would delete local copy: /root/h1/etc/older\n" +
		"would rsync: " + shellJoin(rsyncArgv) + "\n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}

func TestPlanGlobsFailed(t *testing.T) {
	h := globsHost("h1", &extract.GlobsResult{Failed: true})
	h.Status = extract.StatusFailed
	h.Probe.ExitCode = 255
	var b bytes.Buffer
	PrintPlan(&b, &extract.Result{Noop: true, Hosts: []extract.HostResult{h}})
	if want := "built files list: " + globsCmd + " (failed)\n"; b.String() != want {
		t.Errorf("got %q want %q", b.String(), want)
	}
}

func TestReportGlobsFailureShowsStderr(t *testing.T) {
	h := globsHost("h1", &extract.GlobsResult{Failed: true})
	h.Status = extract.StatusFailed
	h.Error = "files list script exited 255"
	h.Probe.Stdout = "out\n"
	h.Probe.Stderr = "ssh: refused\n"
	h.Probe.ExitCode = 255
	h.Rsync = nil
	if o, _ := run(&extract.Result{Hosts: []extract.HostResult{h}}, 3); strings.Contains(o, "files list:") {
		t.Errorf("counts shown for failed host: %q", o)
	}
	_, e := run(&extract.Result{Hosts: []extract.HostResult{h}}, 0)
	if want := "u@h1: files list script exited 255\n  out\n  ssh: refused\n"; e != want {
		t.Errorf("stderr %q want %q", e, want)
	}
}

func TestReportGlobsTraceScriptOnce(t *testing.T) {
	g := &extract.GlobsResult{Globbed: 1}
	plain := okHost("h3")
	plain.Probe = &proc.StepResult{Argv: probeArgv("h3"), Stdout: "/b\n"}
	plain.FilesFrom = "/b"
	r := &extract.Result{Hosts: []extract.HostResult{globsHost("h1", g), plain, globsHost("h2", g)}}
	o, _ := run(r, 3)
	const script = "files list script: 'echo hi'\n"
	if n := strings.Count(o, script); n != 1 {
		t.Fatalf("script shown %d times, want 1:\n%s", n, o)
	}
	if strings.Count(o, "echo hi") != 1 {
		t.Errorf("script shown in a traced command:\n%s", o)
	}
	first := "+ " + globsCmd + "\n  /etc/a\n"
	if i, j := strings.Index(o, script), strings.Index(o, first); i < 0 || j < i {
		t.Errorf("script not shown before the first list step:\n%s", o)
	}
	contains(t, "v3", o, "+ "+strings.Replace(globsCmd, "u@h1", "u@h2", 1)+"\n",
		"+ "+strings.Replace(probeCmd, " h1 ", " h3 ", 1)+"\n  /b\n") // the 0.9.0 probe keeps its full command
	o, _ = run(r, 2)
	if strings.Contains(o, "files list script") {
		t.Errorf("v2 shows the script: %q", o)
	}
}

// manifestFailedHost is a glob host whose manifest could not be read, so
// the list step never ran: Globs is failed and Probe is nil.
func manifestFailedHost() extract.HostResult {
	args := []string{"/root", "/cfg", "h1", ""}
	return extract.HostResult{Host: extract.Host{User: "u", Name: "h1"}, Status: extract.StatusFailed,
		Error:     "cannot read manifest: read /root/.extract-state/h1.files: is a directory",
		Globs:     &extract.GlobsResult{Failed: true},
		PreHooks:  []extract.HookResult{{Stage: "pre_extract_host", Path: "/s/pre", Args: args}},
		PostHooks: []extract.HookResult{{Stage: "post_extract_host", Path: "/s/post", Args: args}}}
}

func TestPlanGlobsManifestUnreadable(t *testing.T) {
	var b bytes.Buffer
	PrintPlan(&b, &extract.Result{Noop: true, Hosts: []extract.HostResult{manifestFailedHost(), globsHost("h2", &extract.GlobsResult{})}})
	want := "built files list: u@h1 (failed: cannot read manifest)\n" +
		"built files list: " + strings.Replace(globsCmd, "u@h1", "u@h2", 1) + " -> 0 listed, 0 globbed, 0 tracked, 0 missing, 0 skipped\n" +
		"would rsync: " + shellJoin(rsyncArgv) + "\n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}

func TestReportGlobsManifestUnreadable(t *testing.T) {
	r := &extract.Result{Hosts: []extract.HostResult{manifestFailedHost()}}
	for v := range 4 {
		o, e := run(r, v)
		if want := "u@h1: cannot read manifest: read /root/.extract-state/h1.files: is a directory\n"; e != want {
			t.Errorf("v%d: stderr %q want %q", v, e, want)
		}
		if strings.Contains(o, "files list") {
			t.Errorf("v%d: stdout %q", v, o)
		}
	}
}
