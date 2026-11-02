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
