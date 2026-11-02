package extract

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gavincarr/extract/internal/config"
	"github.com/gavincarr/extract/internal/proc"
)

// Status is the outcome of one host.
type Status int

const (
	StatusOK Status = iota
	StatusFailed
	StatusTimedOut
)

// Hook stage names.
const (
	stageSetup    = "pre_extract_setup"
	stagePreHost  = "pre_extract_host"
	stagePostHost = "post_extract_host"
	stageTeardown = "post_extract_teardown"
)

// HookResult records one hook invocation. Step is nil when the hook was not
// run (skipped after an earlier failure, or under noop).
type HookResult struct {
	Stage   string
	Path    string
	Args    []string
	Step    *proc.StepResult
	Timeout time.Duration // the configured hook timeout (0 = none)
}

// Failed reports whether the hook ran and did not succeed.
func (h HookResult) Failed() bool { return h.Step != nil && h.Step.Failed() }

// HostResult is the outcome of extracting one host.
type HostResult struct {
	Host      Host
	Status    Status
	Error     string // "" when OK
	Warnings  []string
	Rsync     *proc.StepResult // nil if rsync was not run
	PreHooks  []HookResult
	PostHooks []HookResult
	Duration  time.Duration
}

// Result is the outcome of a whole run.
type Result struct {
	Start, End    time.Time
	Noop          bool
	Aborted       bool // a setup hook failed; no hosts were attempted
	Interrupted   bool // the context was cancelled; later steps were not run
	HostsPlanned  int  // the number of hosts passed to Run
	SetupHooks    []HookResult
	Hosts         []HostResult
	TeardownHooks []HookResult
}

// OK reports whether the run succeeded completely.
func (r *Result) OK() bool {
	if r.Aborted || r.Interrupted {
		return false
	}
	for _, h := range r.Hosts {
		if h.Status != StatusOK {
			return false
		}
	}
	return !anyFailed(r.SetupHooks) && !anyFailed(r.TeardownHooks)
}

func anyFailed(hs []HookResult) bool {
	for _, h := range hs {
		if h.Failed() {
			return true
		}
	}
	return false
}

// Options controls a run. Timeout is the per-host rsync timeout (0 = none).
// When Ctx (nil means context.Background()) is cancelled, the running step
// is killed and no further steps, hosts or teardown hooks are started.
type Options struct {
	Ctx       context.Context
	Noop      bool
	Verbosity int
	Timeout   time.Duration
}

// Run performs the extraction. It never prints.
func Run(cfg *config.Config, hosts []Host, opts Options) *Result {
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	ctx := opts.Ctx
	r := &Result{Start: time.Now(), Noop: opts.Noop, HostsPlanned: len(hosts)}
	defer func() { r.End = time.Now() }()

	setupArgs := []string{cfg.Root, cfg.Path}
	var errMsg string
	r.SetupHooks, errMsg = runHooks(ctx, cfg, stageSetup, cfg.PreSetup, setupArgs, opts.Noop)
	if ctx.Err() != nil {
		r.Interrupted = true
		return r
	}
	if errMsg != "" {
		r.Aborted = true
		return r
	}
	for _, h := range hosts {
		if ctx.Err() != nil {
			break
		}
		r.Hosts = append(r.Hosts, runHost(ctx, cfg, h, opts))
	}
	if ctx.Err() != nil {
		r.Interrupted = true
		return r
	}
	r.TeardownHooks, _ = runHooks(ctx, cfg, stageTeardown, cfg.PostTeardown, setupArgs, opts.Noop)
	return r
}

func runHost(ctx context.Context, cfg *config.Config, h Host, opts Options) (hr HostResult) {
	start := time.Now()
	hr.Host = h
	defer func() { hr.Duration = time.Since(start) }()

	hookArgs := []string{cfg.Root, cfg.Path, h.Name, cfg.FilesArg()}
	skip := func() {
		hr.PreHooks = skipHooks(stagePreHost, cfg.PreHost, hookArgs, cfg.HookTimeout)
		hr.PostHooks = skipHooks(stagePostHost, cfg.PostHost, hookArgs, cfg.HookTimeout)
	}
	fail := func(status Status, msg string) {
		hr.Status = status
		hr.Error = msg
	}

	if !opts.Noop {
		dir := filepath.Join(cfg.Root, h.Name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			if st, serr := os.Stat(dir); serr != nil || !st.IsDir() {
				skip()
				fail(StatusFailed, fmt.Sprintf("cannot create host directory: %v", err))
				return hr
			}
		}
	}

	var msg string
	hr.PreHooks, msg = runHooks(ctx, cfg, stagePreHost, cfg.PreHost, hookArgs, opts.Noop)
	if msg != "" {
		hr.PostHooks = skipHooks(stagePostHost, cfg.PostHost, hookArgs, cfg.HookTimeout)
		fail(StatusFailed, msg)
		return hr
	}

	step := proc.RunContext(ctx, RsyncArgs(cfg, h, opts.Noop, opts.Verbosity), cfg.Root, opts.Timeout)
	hr.Rsync = &step
	switch {
	case step.Err != nil:
		fail(StatusFailed, fmt.Sprintf("rsync failed to start: %v", step.Err))
	case step.TimedOut:
		fail(StatusTimedOut, fmt.Sprintf("rsync timed out after %s", opts.Timeout))
		hr.PostHooks = skipHooks(stagePostHost, cfg.PostHost, hookArgs, cfg.HookTimeout)
		return hr
	case step.Interrupted:
		fail(StatusFailed, "rsync interrupted")
		hr.PostHooks = skipHooks(stagePostHost, cfg.PostHost, hookArgs, cfg.HookTimeout)
		return hr
	case step.ExitCode == 24:
		hr.Warnings = append(hr.Warnings, "some files vanished during transfer (rsync exit 24)")
	case step.ExitCode != 0:
		fail(StatusFailed, fmt.Sprintf("rsync exited %d", step.ExitCode))
	}
	rsyncOK := hr.Status == StatusOK

	hr.PostHooks, msg = runHooks(ctx, cfg, stagePostHost, cfg.PostHost, hookArgs, opts.Noop)
	if msg != "" && rsyncOK {
		fail(StatusFailed, msg)
	}
	return hr
}

// skipHooks lists hooks that were not run.
func skipHooks(stage string, paths, args []string, timeout time.Duration) []HookResult {
	var out []HookResult
	for _, p := range paths {
		out = append(out, HookResult{Stage: stage, Path: p, Args: args, Timeout: timeout})
	}
	return out
}

// runHooks runs a hook list, stopping at the first failure; later hooks are
// listed with a nil Step. Under noop nothing is run. The returned string is
// the error description of the first failure, or "". Once ctx is done no
// further hook is started; the hook that was running, or would have run
// next, is recorded as interrupted.
func runHooks(ctx context.Context, cfg *config.Config, stage string, paths, args []string, noop bool) ([]HookResult, string) {
	out := skipHooks(stage, paths, args, cfg.HookTimeout)
	if noop {
		return out, ""
	}
	for i, p := range paths {
		step := proc.RunContext(ctx, append([]string{p}, args...), cfg.Root, cfg.HookTimeout)
		out[i].Step = &step
		base := filepath.Base(p)
		switch {
		case step.Err != nil:
			return out, fmt.Sprintf("%s hook %s failed to start: %v", stage, base, step.Err)
		case step.TimedOut:
			return out, fmt.Sprintf("%s hook %s timed out after %s", stage, base, cfg.HookTimeout)
		case step.Interrupted:
			return out, fmt.Sprintf("%s hook %s interrupted", stage, base)
		case step.ExitCode != 0:
			return out, fmt.Sprintf("%s hook %s exited %d", stage, base, step.ExitCode)
		}
	}
	return out, ""
}
