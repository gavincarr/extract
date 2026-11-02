// Package proc is the single place that executes child processes.
package proc

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// StepResult describes the outcome of one child process.
type StepResult struct {
	Argv     []string
	ExitCode int // -1 if the process did not exit normally or never started
	Stdout   string
	Stderr   string
	Duration time.Duration
	TimedOut bool
	// Interrupted is set when the context was cancelled before or while
	// the step ran; the process group was then killed (or never started).
	Interrupted bool
	Err         error // non-nil only when the process could not be started
}

// Failed reports whether the step did not complete successfully.
func (s StepResult) Failed() bool {
	return s.Err != nil || s.TimedOut || s.Interrupted || s.ExitCode != 0
}

var (
	killGrace     = 2 * time.Second // SIGTERM to SIGKILL grace; tests shorten it
	pipeWaitDelay = 5 * time.Second // assigned to cmd.WaitDelay; tests shorten it
)

// Run is RunContext with a context that is never cancelled.
func Run(argv []string, dir string, timeout time.Duration) StepResult {
	return RunContext(context.Background(), argv, dir, timeout)
}

// RunContext executes argv in dir (empty means the current directory) with
// stdin from /dev/null. If timeout > 0 and it elapses, or ctx is cancelled,
// the whole process group is sent SIGTERM, then SIGKILL after killGrace.
// A timeout of 0 means none. If ctx is already done, nothing is started.
func RunContext(ctx context.Context, argv []string, dir string, timeout time.Duration) StepResult {
	res := StepResult{Argv: argv, ExitCode: -1}
	if ctx.Err() != nil {
		res.Interrupted = true
		return res
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = pipeWaitDelay

	start := time.Now()
	if err := cmd.Start(); err != nil {
		res.Err = err
		res.Duration = time.Since(start)
		return res
	}

	pid := cmd.Process.Pid
	var timedOut, interrupted atomic.Bool
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		var timeoutC <-chan time.Time
		if timeout > 0 {
			t := time.NewTimer(timeout)
			defer t.Stop()
			timeoutC = t.C
		}
		select {
		case <-done:
			return
		case <-timeoutC:
			timedOut.Store(true)
		case <-ctx.Done():
			interrupted.Store(true)
		}
		_ = unix.Kill(-pid, unix.SIGTERM)
		g := time.NewTimer(killGrace)
		defer g.Stop()
		select {
		case <-done:
		case <-g.C:
			_ = unix.Kill(-pid, unix.SIGKILL)
		}
	}()

	err := cmd.Wait()
	close(done)
	<-watcherDone
	if timedOut.Load() || interrupted.Load() {
		// The leader is gone but group members may have survived SIGTERM.
		// The pgid cannot be recycled while any member lives, so this is safe.
		_ = unix.Kill(-pid, unix.SIGKILL)
	}

	res.Duration = time.Since(start)
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	res.TimedOut = timedOut.Load()
	res.Interrupted = interrupted.Load()
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	} else if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		res.Err = err
	}
	return res
}
