// Package proc is the single place that executes child processes.
package proc

import (
	"bytes"
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
	Err      error // non-nil only when the process could not be started
}

// Failed reports whether the step did not complete successfully.
func (s StepResult) Failed() bool {
	return s.Err != nil || s.TimedOut || s.ExitCode != 0
}

var (
	killGrace     = 2 * time.Second // SIGTERM to SIGKILL grace; tests shorten it
	pipeWaitDelay = 5 * time.Second // assigned to cmd.WaitDelay; tests shorten it
)

// Run executes argv in dir (empty means the current directory) with stdin
// from /dev/null. If timeout > 0 and it elapses, the whole process group is
// sent SIGTERM, then SIGKILL after killGrace. A timeout of 0 means none.
func Run(argv []string, dir string, timeout time.Duration) StepResult {
	res := StepResult{Argv: argv, ExitCode: -1}
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
	var timedOut atomic.Bool
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		if timeout <= 0 {
			<-done
			return
		}
		t := time.NewTimer(timeout)
		defer t.Stop()
		select {
		case <-done:
			return
		case <-t.C:
		}
		timedOut.Store(true)
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
	if timedOut.Load() {
		// The leader is gone but group members may have survived SIGTERM.
		// The pgid cannot be recycled while any member lives, so this is safe.
		_ = unix.Kill(-pid, unix.SIGKILL)
	}

	res.Duration = time.Since(start)
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	res.TimedOut = timedOut.Load()
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	} else if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		res.Err = err
	}
	return res
}
