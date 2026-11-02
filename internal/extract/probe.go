package extract

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gavincarr/extract/internal/proc"
)

// probeNoneFound is the probe script's exit status when no candidate is a
// non-empty file. It is not 1, so that a login shell that can't run the
// script (csh: "for: Command not found.") is reported as a probe failure
// rather than as "nothing found".
const probeNoneFound = 3

// ProbeArgs builds the ssh argv that prints the first of paths that exists
// and is non-empty on host h, exiting 0, or exits probeNoneFound with no
// output when none is. It uses the same ssh options as rsync's --rsh. The remote
// command is one line of POSIX sh with no '!', so that csh-style login
// shells and history expansion have nothing to trip on.
func ProbeArgs(h Host, paths []string) []string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = shQuote(p)
	}
	script := "for f in " + strings.Join(quoted, " ") +
		`; do if test -s "$f"; then printf "%s\n" "$f"; exit 0; fi; done; exit ` + strconv.Itoa(probeNoneFound)
	return []string{"ssh", "-o", "BatchMode=yes", h.Remote(), script}
}

// shQuote single-quotes s for a POSIX shell unless it consists only of
// characters that need no quoting.
func shQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./+,:=@%-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// probeOutcome interprets a finished probe step. It returns the chosen
// path, or a failure status and message.
func probeOutcome(s proc.StepResult, paths []string, timeout time.Duration) (string, Status, string) {
	switch {
	case s.Err != nil:
		return "", StatusFailed, fmt.Sprintf("files list probe failed to start: %v", s.Err)
	case s.TimedOut:
		return "", StatusTimedOut, fmt.Sprintf("files list probe timed out after %s", timeout)
	case s.Interrupted:
		return "", StatusFailed, "files list probe interrupted"
	}
	out := strings.TrimSpace(s.Stdout)
	switch {
	case s.ExitCode == 0 && slices.Contains(paths, out):
		return out, StatusOK, ""
	case s.ExitCode == 0:
		return "", StatusFailed, "files list probe exited 0 without printing a candidate path"
	case s.ExitCode == probeNoneFound && out == "":
		return "", StatusFailed, "no non-empty files list found among: " + strings.Join(paths, " ")
	}
	return "", StatusFailed, fmt.Sprintf("files list probe exited %d", s.ExitCode)
}
