package extract

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gavincarr/extract/internal/config"
	"github.com/gavincarr/extract/internal/testutil"
)

type env struct {
	root, hooks string
	fake        *testutil.FakeRsync
	cfg         *config.Config
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{root: t.TempDir(), hooks: t.TempDir(), fake: testutil.NewFakeRsync(t)}
	e.cfg = &config.Config{
		Path: "/cfg/extract.conf", Root: e.root, Mode: config.ModeTree,
		Source: "/etc", HookTimeout: 10 * time.Second,
	}
	return e
}

func (e *env) hook(t *testing.T, name, body string) string {
	return testutil.WriteHook(t, e.hooks, name, body)
}

func hosts(names ...string) []Host {
	var hs []Host
	for _, n := range names {
		hs = append(hs, Host{Name: n})
	}
	return hs
}

func TestRunHealthy(t *testing.T) {
	e := newEnv(t)
	e.cfg.PreSetup = []string{e.hook(t, "setup", "")}
	e.cfg.PreHost = []string{e.hook(t, "pre", "")}
	e.cfg.PostHost = []string{e.hook(t, "post", "")}
	e.cfg.PostTeardown = []string{e.hook(t, "teardown", "")}
	r := Run(e.cfg, hosts("h1", "h2"), Options{})
	R, C := e.root, "/cfg/extract.conf"
	want := []string{
		"setup " + R + " " + C + " cwd=" + R,
		"pre " + R + " " + C + " h1  cwd=" + R,
		"post " + R + " " + C + " h1  cwd=" + R,
		"pre " + R + " " + C + " h2  cwd=" + R,
		"post " + R + " " + C + " h2  cwd=" + R,
		"teardown " + R + " " + C + " cwd=" + R,
	}
	if got := testutil.HookLog(t, e.hooks); !reflect.DeepEqual(got, want) {
		t.Fatalf("hook log:\n got %q\nwant %q", got, want)
	}
	for _, h := range r.Hosts {
		if h.Status != StatusOK {
			t.Errorf("%v: status %v err %q", h.Host, h.Status, h.Error)
		}
		if st, err := os.Stat(filepath.Join(R, h.Host.Name)); err != nil || !st.IsDir() {
			t.Errorf("dir for %v missing", h.Host)
		}
	}
	if !r.OK() {
		t.Error("want OK")
	}
	calls := e.fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d", len(calls))
	}
	for _, c := range calls {
		if c.Dir != R {
			t.Errorf("rsync dir %q", c.Dir)
		}
	}
}

func TestRunFilesModeHookArgs(t *testing.T) {
	e := newEnv(t)
	e.cfg.Mode = config.ModeFilesRemote
	e.cfg.Source = "/var/x.txt"
	e.cfg.PreHost = []string{e.hook(t, "pre", "")}
	Run(e.cfg, []Host{{User: "u", Name: "h1"}}, Options{})
	want := "pre " + e.root + " /cfg/extract.conf h1 /var/x.txt cwd=" + e.root
	if got := testutil.HookLog(t, e.hooks); !reflect.DeepEqual(got, []string{want}) {
		t.Fatalf("log %q want %q", got, want)
	}
	calls := e.fake.Calls()
	if len(calls) != 1 || !slices.Contains(calls[0].Argv, "u@h1:/") {
		t.Fatalf("calls %+v", calls)
	}
}

func TestRunPreHostHookFails(t *testing.T) {
	e := newEnv(t)
	e.cfg.PreHost = []string{e.hook(t, "pre", `[ "$3" = h1 ] && exit 99; exit 0`)}
	e.cfg.PostHost = []string{e.hook(t, "post", "")}
	r := Run(e.cfg, hosts("h1", "h2"), Options{})
	h1, h2 := r.Hosts[0], r.Hosts[1]
	if h1.Status != StatusFailed || h1.Error != "pre_extract_host hook pre exited 99" {
		t.Errorf("h1: %v %q", h1.Status, h1.Error)
	}
	if h1.Rsync != nil {
		t.Error("rsync ran for h1")
	}
	if len(h1.PostHooks) != 1 || h1.PostHooks[0].Step != nil {
		t.Errorf("h1 post hooks %+v", h1.PostHooks)
	}
	if h2.Status != StatusOK || r.OK() {
		t.Errorf("h2 %v, OK=%v", h2.Status, r.OK())
	}
	if n := len(e.fake.Calls()); n != 1 {
		t.Errorf("rsync calls = %d", n)
	}
}

func TestRunHookListStopsAtFirstFailure(t *testing.T) {
	e := newEnv(t)
	e.cfg.PostHost = []string{e.hook(t, "a", "exit 1"), e.hook(t, "b", "")}
	r := Run(e.cfg, hosts("h1"), Options{})
	h := r.Hosts[0]
	if h.Status != StatusFailed || h.PostHooks[0].Step == nil || h.PostHooks[1].Step != nil {
		t.Errorf("%+v", h)
	}
	for _, l := range testutil.HookLog(t, e.hooks) {
		if strings.HasPrefix(l, "b ") {
			t.Errorf("b ran: %q", l)
		}
	}
}

func TestRunRsyncFailureStillRunsPostHooks(t *testing.T) {
	e := newEnv(t)
	e.cfg.PostHost = []string{e.hook(t, "post", "")}
	e.fake.SetHost("h1", 23, "link_stat failed", 0)
	h := Run(e.cfg, hosts("h1"), Options{}).Hosts[0]
	if h.Status != StatusFailed || h.Error != "rsync exited 23" {
		t.Errorf("%v %q", h.Status, h.Error)
	}
	if !strings.Contains(h.Rsync.Stderr, "link_stat") {
		t.Errorf("stderr %q", h.Rsync.Stderr)
	}
	if len(testutil.HookLog(t, e.hooks)) != 1 {
		t.Error("post hook did not run")
	}
}

func TestRunRsyncExit24IsWarning(t *testing.T) {
	e := newEnv(t)
	e.fake.SetHost("h1", 24, "", 0)
	r := Run(e.cfg, hosts("h1"), Options{})
	h := r.Hosts[0]
	if h.Status != StatusOK || !r.OK() {
		t.Errorf("%v", h.Status)
	}
	want := []string{"some files vanished during transfer (rsync exit 24)"}
	if !reflect.DeepEqual(h.Warnings, want) {
		t.Errorf("warnings %q", h.Warnings)
	}
}

func TestRunRsyncTimeoutSkipsPostHooks(t *testing.T) {
	e := newEnv(t)
	e.cfg.PostHost = []string{e.hook(t, "post", "")}
	e.fake.SetHost("h1", 0, "", 5*time.Second)
	start := time.Now()
	h := Run(e.cfg, hosts("h1"), Options{Timeout: 200 * time.Millisecond}).Hosts[0]
	if time.Since(start) > 4*time.Second {
		t.Error("took too long")
	}
	if h.Status != StatusTimedOut || h.Error != "rsync timed out after 200ms" {
		t.Errorf("%v %q", h.Status, h.Error)
	}
	if h.Duration < 200*time.Millisecond {
		t.Errorf("Duration = %v, want >= 200ms", h.Duration)
	}
	if len(testutil.HookLog(t, e.hooks)) != 0 {
		t.Error("post hook ran")
	}
}

func TestRunPostHostHookFails(t *testing.T) {
	e := newEnv(t)
	e.cfg.PostHost = []string{e.hook(t, "post", "exit 3")}
	h := Run(e.cfg, hosts("h1"), Options{}).Hosts[0]
	if h.Status != StatusFailed || h.Error != "post_extract_host hook post exited 3" {
		t.Errorf("%v %q", h.Status, h.Error)
	}
}

func TestRunHookTimeout(t *testing.T) {
	e := newEnv(t)
	e.cfg.HookTimeout = 200 * time.Millisecond
	e.cfg.PreHost = []string{e.hook(t, "pre", "sleep 5")}
	start := time.Now()
	h := Run(e.cfg, hosts("h1"), Options{}).Hosts[0]
	if time.Since(start) > 4*time.Second {
		t.Error("took too long")
	}
	if h.Status != StatusFailed || h.Error != "pre_extract_host hook pre timed out after 200ms" {
		t.Errorf("%v %q", h.Status, h.Error)
	}
	if h.PreHooks[0].Timeout != 200*time.Millisecond {
		t.Errorf("Timeout = %v", h.PreHooks[0].Timeout)
	}
}

func TestRunSetupFails(t *testing.T) {
	e := newEnv(t)
	e.cfg.PreSetup = []string{e.hook(t, "setup", "exit 1")}
	e.cfg.PostTeardown = []string{e.hook(t, "teardown", "")}
	r := Run(e.cfg, hosts("h1"), Options{})
	if !r.Aborted || len(r.Hosts) != 0 || r.OK() || len(e.fake.Calls()) != 0 {
		t.Errorf("%+v", r)
	}
	for _, l := range testutil.HookLog(t, e.hooks) {
		if strings.HasPrefix(l, "teardown") {
			t.Error("teardown ran")
		}
	}
}

func TestRunTeardownFails(t *testing.T) {
	e := newEnv(t)
	e.cfg.PostTeardown = []string{e.hook(t, "teardown", "exit 1")}
	r := Run(e.cfg, hosts("h1", "h2"), Options{})
	for _, h := range r.Hosts {
		if h.Status != StatusOK {
			t.Errorf("%v", h.Status)
		}
	}
	if r.OK() {
		t.Error("want !OK")
	}
}

func TestRunNoop(t *testing.T) {
	e := newEnv(t)
	e.cfg.PreSetup = []string{e.hook(t, "setup", "")}
	e.cfg.PreHost = []string{e.hook(t, "pre", "")}
	e.cfg.PostHost = []string{e.hook(t, "post", "")}
	e.cfg.PostTeardown = []string{e.hook(t, "teardown", "")}
	r := Run(e.cfg, hosts("h1", "h2"), Options{Noop: true})
	if len(testutil.HookLog(t, e.hooks)) != 0 {
		t.Error("hooks ran")
	}
	for _, c := range e.fake.Calls() {
		if !slices.Contains(c.Argv, "--dry-run") {
			t.Errorf("no --dry-run: %q", c.Argv)
		}
	}
	if len(e.fake.Calls()) != 2 {
		t.Error("want 2 rsync calls")
	}
	all := append(append([]HookResult{}, r.SetupHooks...), r.TeardownHooks...)
	for _, h := range r.Hosts {
		all = append(all, h.PreHooks...)
		all = append(all, h.PostHooks...)
		if len(h.PreHooks) != 1 || len(h.PostHooks) != 1 {
			t.Errorf("hooks not listed: %+v", h)
		}
	}
	if len(all) != 6 {
		t.Errorf("hook results = %d", len(all))
	}
	for _, h := range all {
		if h.Step != nil || len(h.Args) == 0 {
			t.Errorf("%+v", h)
		}
	}
	if _, err := os.Stat(filepath.Join(e.root, "h1")); err == nil {
		t.Error("h1 dir created in noop")
	}
	if !r.OK() {
		t.Error("want OK")
	}
}

func TestRunMkdirFailure(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(filepath.Join(e.root, "h1"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := Run(e.cfg, hosts("h1", "h2"), Options{})
	h1 := r.Hosts[0]
	if h1.Status != StatusFailed || !strings.HasPrefix(h1.Error, "cannot create host directory") {
		t.Errorf("%v %q", h1.Status, h1.Error)
	}
	if r.Hosts[1].Status != StatusOK {
		t.Error("h2 not OK")
	}
	calls := e.fake.Calls()
	if len(calls) != 1 || calls[0].Argv[len(calls[0].Argv)-1] != "h2" {
		t.Errorf("calls %+v", calls)
	}
}

func cancelAfter(d time.Duration) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(d, cancel)
	return ctx
}

func TestRunInterruptedDuringRsync(t *testing.T) {
	e := newEnv(t)
	e.cfg.PostHost = []string{e.hook(t, "post", "")}
	e.cfg.PostTeardown = []string{e.hook(t, "teardown", "")}
	e.fake.SetHost("h1", 0, "", 5*time.Second)
	start := time.Now()
	r := Run(e.cfg, hosts("h1", "h2", "h3"), Options{Ctx: cancelAfter(200 * time.Millisecond)})
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("took too long: %v", d)
	}
	if !r.Interrupted || r.Aborted || r.OK() {
		t.Fatalf("Interrupted=%v Aborted=%v OK=%v", r.Interrupted, r.Aborted, r.OK())
	}
	if r.HostsPlanned != 3 {
		t.Errorf("HostsPlanned = %d", r.HostsPlanned)
	}
	if len(r.Hosts) != 1 {
		t.Fatalf("hosts = %d", len(r.Hosts))
	}
	h := r.Hosts[0]
	if h.Status != StatusFailed || h.Error != "rsync interrupted" {
		t.Errorf("status %v error %q", h.Status, h.Error)
	}
	if h.PostHooks[0].Step != nil {
		t.Error("post hook ran after interruption")
	}
	for _, l := range testutil.HookLog(t, e.hooks) {
		if strings.HasPrefix(l, "teardown") || strings.HasPrefix(l, "post") {
			t.Errorf("hook ran after interruption: %q", l)
		}
	}
	if n := len(e.fake.Calls()); n != 1 {
		t.Errorf("rsync calls = %d", n)
	}
}

func TestRunInterruptedDuringPostHook(t *testing.T) {
	e := newEnv(t)
	e.cfg.PostHost = []string{e.hook(t, "git", "sleep 5"), e.hook(t, "touch", "")}
	r := Run(e.cfg, hosts("h1", "h2"), Options{Ctx: cancelAfter(300 * time.Millisecond)})
	if !r.Interrupted || len(r.Hosts) != 1 {
		t.Fatalf("Interrupted=%v hosts=%d", r.Interrupted, len(r.Hosts))
	}
	h := r.Hosts[0]
	if h.Status != StatusFailed || h.Error != "post_extract_host hook git interrupted" {
		t.Errorf("status %v error %q", h.Status, h.Error)
	}
	if h.PostHooks[1].Step != nil {
		t.Error("second post hook ran after interruption")
	}
}

func TestRunInterruptedDuringSetup(t *testing.T) {
	e := newEnv(t)
	e.cfg.PreSetup = []string{e.hook(t, "setup", "sleep 5")}
	e.cfg.PostTeardown = []string{e.hook(t, "teardown", "")}
	r := Run(e.cfg, hosts("h1"), Options{Ctx: cancelAfter(200 * time.Millisecond)})
	if !r.Interrupted || r.Aborted || len(r.Hosts) != 0 || r.OK() {
		t.Fatalf("Interrupted=%v Aborted=%v hosts=%d", r.Interrupted, r.Aborted, len(r.Hosts))
	}
	if !r.SetupHooks[0].Step.Interrupted {
		t.Error("setup step not marked interrupted")
	}
	if got := testutil.HookLog(t, e.hooks); len(got) != 1 {
		t.Errorf("hook log: %q", got)
	}
}
