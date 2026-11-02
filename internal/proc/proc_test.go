package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRunCapturesSeparateStreams(t *testing.T) {
	r := Run([]string{"sh", "-c", "echo out; echo err >&2; exit 3"}, "", 0)
	if r.ExitCode != 3 || r.Stdout != "out\n" || r.Stderr != "err\n" || r.TimedOut || r.Err != nil || !r.Failed() {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestRunUsesDir(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := Run([]string{"pwd"}, tmp, 0)
	if r.Stdout != tmp+"\n" {
		t.Fatalf("pwd = %q, want %q", r.Stdout, tmp+"\n")
	}
}

func TestRunStdinIsEmpty(t *testing.T) {
	r := Run([]string{"cat"}, "", 2*time.Second)
	if r.ExitCode != 0 || r.Stdout != "" || r.TimedOut {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestRunStartFailure(t *testing.T) {
	r := Run([]string{"/nonexistent/x"}, "", 0)
	if r.Err == nil || r.ExitCode != -1 || !r.Failed() {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestRunTimeoutKillsProcessGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	start := time.Now()
	r := Run([]string{"sh", "-c", "sleep 30 & echo $! > " + pidfile + "; wait"}, "", 200*time.Millisecond)
	if !r.TimedOut {
		t.Fatalf("expected timeout: %+v", r)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took too long: %v", d)
	}
	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if unix.Kill(pid, 0) == unix.ESRCH {
			return
		}
		if time.Now().After(deadline) {
			unix.Kill(pid, unix.SIGKILL)
			t.Fatalf("grandchild %d still alive", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunTimeoutEscalatesToSIGKILL(t *testing.T) {
	old := killGrace
	killGrace = 300 * time.Millisecond
	defer func() { killGrace = old }()
	start := time.Now()
	r := Run([]string{"sh", "-c", `trap "" TERM; sleep 30`}, "", 200*time.Millisecond)
	if !r.TimedOut {
		t.Fatalf("expected timeout: %+v", r)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took too long: %v", d)
	}
}

func TestRunBackgroundChildHoldingPipe(t *testing.T) {
	old := pipeWaitDelay
	pipeWaitDelay = 300 * time.Millisecond
	defer func() { pipeWaitDelay = old }()
	start := time.Now()
	r := Run([]string{"sh", "-c", "sleep 30 & exit 0"}, "", 0)
	if r.ExitCode != 0 || r.Failed() {
		t.Fatalf("unexpected result: %+v", r)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took too long: %v", d)
	}
}
