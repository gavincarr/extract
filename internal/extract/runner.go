package extract

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	Host     Host
	Status   Status
	Error    string // "" when OK
	Warnings []string
	// Probe is the ssh step that chose among several EXTRACT_FILES_REMOTE
	// candidates; nil when it was not run (one candidate, or another mode).
	Probe *proc.StepResult
	// FilesFrom is the files list given to rsync and passed to the per-host
	// hooks as $4: the chosen EXTRACT_FILES_REMOTE path, the
	// EXTRACT_FILES_LOCAL value, or "" in tree mode or if the probe failed.
	FilesFrom string
	Rsync     *proc.StepResult // nil if rsync was not run
	PreHooks  []HookResult
	PostHooks []HookResult
	// Globs is the outcome of building the files list from
	// EXTRACT_FILES_GLOBS, nil when globs are not used. Probe is then the
	// list step, or nil if it was never run because the manifest could not
	// be read (Globs.Failed is set).
	Globs    *GlobsResult
	Duration time.Duration
}

// GlobsResult describes the files list built for a host using
// EXTRACT_FILES_GLOBS.
type GlobsResult struct {
	Failed                            bool     // the list step failed; nothing else ran
	NoList                            bool     // remote lists configured, none non-empty
	Listed, Globbed, Tracked, Missing int      // counts for -vvv and the plan
	Skipped                           []string // symlinks, and names with a newline or CR
	Deleted                           []string // local copies removed for L paths (or, under noop, that would be)
}

// ProbeFailed reports whether the files list probe (or, with globs, the
// list step) ran without producing a files list, in which case nothing
// else was run for the host.
func (h HostResult) ProbeFailed() bool {
	if h.Globs != nil {
		return h.Globs.Failed
	}
	return h.Probe != nil && h.FilesFrom == ""
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
	if ctx.Err() != nil {
		r.Interrupted = true
	}
	return r
}

func runHost(ctx context.Context, cfg *config.Config, h Host, opts Options) (hr HostResult) {
	start := time.Now()
	hr.Host = h
	defer func() { hr.Duration = time.Since(start) }()

	var hookArgs []string
	skip := func() {
		hr.PreHooks = skipHooks(stagePreHost, cfg.PreHost, hookArgs, cfg.HookTimeout)
		hr.PostHooks = skipHooks(stagePostHost, cfg.PostHost, hookArgs, cfg.HookTimeout)
	}
	fail := func(status Status, msg string) {
		hr.Status = status
		hr.Error = msg
	}

	hr.FilesFrom = cfg.FilesArg()
	var lr *ListResult        // set when using globs
	var prevManifest []string // the manifest read before the list step
	if cfg.UsesGlobs() {
		hr.FilesFrom = ""
		var status Status
		var msg string
		lr, prevManifest, status, msg = buildList(ctx, cfg, h, opts, &hr)
		if msg != "" {
			hookArgs = []string{cfg.Root, cfg.Path, h.Name, ""}
			skip()
			fail(status, msg)
			return hr
		}
		hr.FilesFrom = lr.Remote
	} else if paths := cfg.FilesRemote(); cfg.Mode == config.ModeFilesRemote && len(paths) > 1 {
		// Read-only, so it runs under noop too, like EXTRACT_HOST_CMD.
		step := proc.RunContext(ctx, ProbeArgs(h, paths), cfg.Root, opts.Timeout)
		hr.Probe = &step
		chosen, status, msg := probeOutcome(step, paths, opts.Timeout)
		if msg != "" {
			hookArgs = []string{cfg.Root, cfg.Path, h.Name, ""}
			skip()
			fail(status, msg)
			return hr
		}
		hr.FilesFrom = chosen
	}
	hookArgs = []string{cfg.Root, cfg.Path, h.Name, hr.FilesFrom}

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

	var argv []string
	if lr != nil {
		deleteSymlinkCopies(cfg.Root, h.Name, trackedSymlinks(lr, prevManifest), opts.Noop, &hr)
		listFile, err := writeListFile(rsyncList(lr))
		if err != nil {
			fail(StatusFailed, fmt.Sprintf("cannot write files list: %v", err))
			return finishHost(ctx, cfg, hookArgs, opts.Noop, &hr, false)
		}
		defer os.Remove(listFile)
		argv = GlobsRsyncArgs(h, listFile, opts.Noop, opts.Verbosity)
	} else {
		argv = RsyncArgs(cfg, h, hr.FilesFrom, opts.Noop, opts.Verbosity)
	}
	step := proc.RunContext(ctx, argv, cfg.Root, opts.Timeout)
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

	// rsync exited 0 or 24: record what is now extracted. On any failure the
	// old manifest stays, so the next run retries the same deletions.
	if lr != nil && rsyncOK && !opts.Noop {
		if err := WriteManifest(ManifestPath(cfg.Root, h.Name), manifestList(lr)); err != nil {
			fail(StatusFailed, fmt.Sprintf("cannot write manifest: %v", err))
			rsyncOK = false
		}
	}
	return finishHost(ctx, cfg, hookArgs, opts.Noop, &hr, rsyncOK)
}

// finishHost runs the post-host hooks. A hook failure fails the host only
// if it has not already failed.
func finishHost(ctx context.Context, cfg *config.Config, hookArgs []string, noop bool, hr *HostResult, ok bool) HostResult {
	var msg string
	hr.PostHooks, msg = runHooks(ctx, cfg, stagePostHost, cfg.PostHost, hookArgs, noop)
	if msg != "" && ok {
		hr.Status = StatusFailed
		hr.Error = msg
	}
	return *hr
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

// buildList reads the host's manifest and runs the list step with it on
// stdin, recording the step and its outcome in hr and adding a warning per
// skipped glob match. It returns the list and the manifest it read. On
// failure it returns the status and message for the host; nothing else is
// then run for it.
func buildList(ctx context.Context, cfg *config.Config, h Host, opts Options, hr *HostResult) (*ListResult, []string, Status, string) {
	manifest, err := ReadManifest(ManifestPath(cfg.Root, h.Name))
	if err != nil {
		hr.Globs = &GlobsResult{Failed: true}
		return nil, nil, StatusFailed, fmt.Sprintf("cannot read manifest: %v", err)
	}
	var stdin strings.Builder
	for _, p := range manifest {
		stdin.WriteString(p)
		stdin.WriteByte('\n')
	}
	// Read-only, so it runs under noop too, like the probe.
	step := proc.RunContextStdin(ctx, ListArgs(h, cfg.FilesRemote(), cfg.Globs), cfg.Root, opts.Timeout, strings.NewReader(stdin.String()))
	hr.Probe = &step
	lr, status, msg := listOutcome(step, opts.Timeout)
	hr.Globs = &GlobsResult{Failed: msg != ""}
	if msg != "" {
		return nil, nil, status, msg
	}
	dropUnsafeNames(&lr)
	lr.Symlinks = dedup(lr.Symlinks)
	lr.Skipped = dedup(lr.Skipped)
	g := hr.Globs
	g.NoList = lr.NoList
	g.Listed, g.Globbed, g.Tracked, g.Missing = len(lr.Listed), len(lr.Globbed), len(lr.Tracked), len(lr.Missing)
	listed := pathSet(lr.Listed)
	for _, p := range lr.Symlinks {
		if listed[p] {
			continue // extracted anyway: remote-list paths follow links
		}
		hr.Warnings = append(hr.Warnings, "skipped symlink "+p)
		g.Skipped = append(g.Skipped, p)
	}
	for _, p := range lr.Skipped {
		hr.Warnings = append(hr.Warnings, "skipped name containing a newline or carriage return "+p)
		g.Skipped = append(g.Skipped, p)
	}
	return &lr, manifest, StatusOK, ""
}

// unsafeName reports whether p holds a newline or a carriage return. rsync's
// --files-from ends a line at either, so such a name would reach it as two
// other paths, both likely missing remotely, and --delete-missing-args would
// then delete their local namesakes (such as the host directory's .git).
func unsafeName(p string) bool { return strings.ContainsAny(p, "\n\r") }

// dropUnsafeNames moves every path in lr that unsafeName rejects into
// lr.Skipped, with '?' for each newline and CR. The list script already
// does this; this keeps such a name from reaching rsync or the manifest
// whatever the remote printed.
func dropUnsafeNames(lr *ListResult) {
	keep := func(paths []string) []string {
		var out []string
		for _, p := range paths {
			if unsafeName(p) {
				lr.Skipped = append(lr.Skipped, strings.NewReplacer("\n", "?", "\r", "?").Replace(p))
			} else {
				out = append(out, p)
			}
		}
		return out
	}
	lr.Listed = keep(lr.Listed)
	lr.Globbed = keep(lr.Globbed)
	lr.Tracked = keep(lr.Tracked)
	lr.Missing = keep(lr.Missing)
	lr.Symlinks = keep(lr.Symlinks)
}

// deleteSymlinkCopies removes the local copy, if any, of each glob match
// that was skipped as a symlink, so that the tree doesn't keep a frozen
// copy from before it became a link. It never deletes outside the host
// directory, nor a directory. Under noop it only records what it would
// delete.
func deleteSymlinkCopies(root, host string, symlinks []string, noop bool, hr *HostResult) {
	hostDir := filepath.Join(root, host)
	realHost, err := filepath.EvalSymlinks(hostDir)
	if err != nil {
		return // no host directory (wiped, or noop on a new host): no copies
	}
	for _, p := range symlinks {
		local := filepath.Join(hostDir, p) // Join cleans the result
		if !strings.HasPrefix(local, hostDir+string(os.PathSeparator)) {
			continue
		}
		// Guard against a symlinked directory inside the host tree (rsync
		// never makes one, but a hook might) leading the delete elsewhere.
		parent, err := filepath.EvalSymlinks(filepath.Dir(local))
		if err != nil || (parent != realHost && !strings.HasPrefix(parent, realHost+string(os.PathSeparator))) {
			continue
		}
		target := filepath.Join(parent, filepath.Base(local))
		st, err := os.Lstat(target)
		if err != nil || st.IsDir() {
			continue
		}
		note := "" // under noop the plan reports Deleted instead
		if !noop {
			if err := os.Remove(target); err != nil {
				note = fmt.Sprintf(" (cannot delete local copy: %v)", err)
			} else {
				note = " (deleted local copy)"
			}
		}
		if noop || note == " (deleted local copy)" {
			hr.Globs.Deleted = append(hr.Globs.Deleted, local)
		}
		if i := slices.Index(hr.Warnings, "skipped symlink "+p); i >= 0 {
			hr.Warnings[i] += note
		}
	}
}

// rsyncList is the files list given to rsync for a globs host: the
// remote-list paths, then the glob, tracked and missing paths minus every
// symlinked glob match, deduplicated. The symlink rule applies only to
// glob matches (and the manifest that records them); a remote-list path
// keeps --copy-links. Missing paths are listed so that
// --delete-missing-args deletes their local copies.
func rsyncList(lr *ListResult) []string {
	return dedup(lr.Listed, without(dedup(lr.Globbed, lr.Tracked, lr.Missing), lr.Symlinks))
}

// manifestList is the new manifest after a successful rsync: the paths in
// the rsync list that exist on the host.
func manifestList(lr *ListResult) []string {
	return dedup(lr.Listed, without(dedup(lr.Globbed, lr.Tracked), lr.Symlinks))
}

// trackedSymlinks returns the symlinked glob matches whose local copies
// may be deleted: those extract put there itself (in the previous
// manifest) and that the remote list doesn't still extract.
func trackedSymlinks(lr *ListResult, prevManifest []string) []string {
	tracked, listed := pathSet(prevManifest), pathSet(lr.Listed)
	var out []string
	for _, p := range lr.Symlinks {
		if tracked[absPath(p)] && !listed[absPath(p)] {
			out = append(out, p)
		}
	}
	return out
}

// pathSet returns the set of paths, made absolute.
func pathSet(paths []string) map[string]bool {
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[absPath(p)] = true
	}
	return set
}

// absPath makes a files-list path absolute. Remote-list entries are
// relative to / (rsync's source); writing them absolute keeps the manifest
// valid for the list script, which tests manifest paths as given.
func absPath(p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}

// dedup concatenates lists, making each path absolute and keeping only
// its first occurrence.
func dedup(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lists {
		for _, p := range l {
			p = absPath(p)
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// without returns paths minus any in drop (compared as absolute paths).
func without(paths, drop []string) []string {
	skip := pathSet(drop)
	var out []string
	for _, p := range paths {
		if !skip[p] {
			out = append(out, p)
		}
	}
	return out
}

// writeListFile writes paths, one per line, to a new temp file outside
// the host directory and returns its name. The caller removes it. A path
// that unsafeName rejects is an error (buildList has already dropped them).
func writeListFile(paths []string) (string, error) {
	for _, p := range paths {
		if unsafeName(p) {
			return "", fmt.Errorf("path contains a newline or carriage return: %q", p)
		}
	}
	f, err := os.CreateTemp("", "extract-files-*")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	_, werr := f.WriteString(b.String())
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(f.Name())
		return "", werr
	}
	return f.Name(), nil
}
