package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gavincarr/extract/internal/testutil"
)

// writeConf writes a tree-mode config and returns its path.
func writeConf(t *testing.T, extra string) string {
	t.Helper()
	root := t.TempDir()
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
	testutil.NewFakeRsync(t)
	conf := writeConf(t, "EXTRACT_HOSTS=\"h1 h2\"\n")
	code, out, errs := runCLI("-c", conf)
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errs)
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
