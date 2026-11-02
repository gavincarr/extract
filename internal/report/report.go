package report

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/gavincarr/extract/internal/extract"
	"github.com/gavincarr/extract/internal/proc"
)

// Report logs the outcome of a run at levels appropriate to its content.
func Report(log *slog.Logger, r *extract.Result) {
	ctx := context.Background()
	say := func(l slog.Level, format string, a ...any) { log.Log(ctx, l, fmt.Sprintf(format, a...)) }
	out := func(l slog.Level, s string) {
		for _, line := range indent(s) {
			say(l, "%s", line)
		}
	}

	// Trace: every step that ran, in execution order.
	trace := func(s *proc.StepResult) {
		if s == nil {
			return
		}
		say(LevelTrace, "+ %s", shellJoin(s.Argv))
		out(LevelTrace, s.Stdout)
		out(LevelTrace, s.Stderr)
	}
	for _, h := range r.SetupHooks {
		trace(h.Step)
	}
	for _, h := range r.Hosts {
		for _, p := range h.PreHooks {
			trace(p.Step)
		}
		trace(h.Rsync)
		for _, p := range h.PostHooks {
			trace(p.Step)
		}
	}
	for _, h := range r.TeardownHooks {
		trace(h.Step)
	}

	// Errors.
	hookFailure := func(hs []extract.HookResult) {
		for _, h := range hs {
			if h.Failed() {
				say(slog.LevelError, "%s hook %s %s", h.Stage, h.Path, stepProblem(h.Step, h.Timeout))
				out(slog.LevelError, h.Step.Stdout)
				out(slog.LevelError, h.Step.Stderr)
			}
		}
	}
	hookFailure(r.SetupHooks)
	if r.Aborted {
		say(slog.LevelError, "run aborted: no hosts extracted")
	}
	var ok, failed, timedOut int
	for _, h := range r.Hosts {
		switch h.Status {
		case extract.StatusOK:
			ok++
			continue
		case extract.StatusTimedOut:
			timedOut++
		default:
			failed++
		}
		say(slog.LevelError, "%s: %s", h.Host, h.Error)
		if h.Rsync != nil && h.Rsync.Failed() && h.Rsync.ExitCode != 24 {
			out(slog.LevelError, h.Rsync.Stderr)
			// The rsync failure is the root cause, but a hook that also
			// failed on this host must not go unreported.
			for _, p := range append(append([]extract.HookResult{}, h.PreHooks...), h.PostHooks...) {
				if p.Failed() {
					say(slog.LevelError, "  then %s hook %s %s", p.Stage, filepath.Base(p.Path), stepProblem(p.Step, p.Timeout))
					out(slog.LevelError, indentText(p.Step.Stdout))
					out(slog.LevelError, indentText(p.Step.Stderr))
				}
			}
			continue
		}
		for _, p := range append(append([]extract.HookResult{}, h.PreHooks...), h.PostHooks...) {
			if p.Failed() {
				out(slog.LevelError, p.Step.Stdout)
				out(slog.LevelError, p.Step.Stderr)
				break
			}
		}
	}
	hookFailure(r.TeardownHooks)

	// Warnings.
	for _, h := range r.Hosts {
		for _, w := range h.Warnings {
			say(slog.LevelInfo, "%s: warning: %s", h.Host, w)
		}
	}

	// Per-host success lines.
	for _, h := range r.Hosts {
		if h.Status == extract.StatusOK {
			say(slog.LevelDebug, "%s ok (%s)", h.Host, h.Duration.Round(100*time.Millisecond))
		}
	}

	// Summary.
	switch {
	case r.Aborted:
		// The "run aborted" error line covers it.
	case r.OK():
		noun := "hosts"
		if ok == 1 {
			noun = "host"
		}
		say(slog.LevelInfo, "%d %s ok", ok, noun)
	default:
		say(slog.LevelInfo, "%d ok, %d failed, %d timed out", ok, failed, timedOut)
	}
}

// stepProblem describes why a failed hook step failed.
func stepProblem(s *proc.StepResult, timeout time.Duration) string {
	switch {
	case s.Err != nil:
		return fmt.Sprintf("failed to start: %v", s.Err)
	case s.TimedOut:
		return fmt.Sprintf("timed out after %s", timeout)
	}
	return fmt.Sprintf("exited %d", s.ExitCode)
}

// indentText prefixes each line of s with two spaces, so that a further
// pass through indent yields four.
func indentText(s string) string {
	return strings.Join(indent(s), "\n")
}

// indent splits captured output into lines prefixed by two spaces.
func indent(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return lines
}

// PrintPlan writes the noop plan to w, in execution order.
func PrintPlan(w io.Writer, r *extract.Result) {
	hooks := func(hs []extract.HookResult) {
		for _, h := range hs {
			fmt.Fprintf(w, "would run %s: %s\n", h.Stage, shellJoin(append([]string{h.Path}, h.Args...)))
		}
	}
	hooks(r.SetupHooks)
	for _, h := range r.Hosts {
		hooks(h.PreHooks)
		if h.Rsync != nil {
			fmt.Fprintf(w, "would rsync: %s\n", shellJoin(h.Rsync.Argv))
		}
		hooks(h.PostHooks)
	}
	hooks(r.TeardownHooks)
}

// ExitCode is 0 for a fully successful run, else 1.
func ExitCode(r *extract.Result) int {
	if r.OK() {
		return 0
	}
	return 1
}

// shellJoin quotes elements containing spaces or shell metacharacters.
func shellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\$`!*?[]{}()<>|&;#~") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		parts[i] = a
	}
	return strings.Join(parts, " ")
}
