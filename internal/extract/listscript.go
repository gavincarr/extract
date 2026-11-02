package extract

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gavincarr/extract/internal/proc"
)

// ListResult is the parsed output of the list-building script for one host.
type ListResult struct {
	Remote   string   // chosen EXTRACT_FILES_REMOTE path, or ""
	NoList   bool     // remote lists were configured but none was non-empty
	Listed   []string // F: remote-list entries that exist
	Globbed  []string // G: accepted glob matches (regular files)
	Tracked  []string // T: manifest paths that exist
	Missing  []string // M: remote-list or manifest paths that don't exist
	Symlinks []string // L: glob matches skipped as symlinks
	Skipped  []string // X: glob matches with a newline, shown with '?' for it
}

// errListIncomplete is returned when the output lacks the end sentinel,
// which is how a login shell that could not run the script shows up.
var errListIncomplete = errors.New("files list output incomplete (no end marker)")

// ListArgs builds the ssh argv that runs the list-building script on host
// h, with the host's manifest to be supplied on stdin. The script is run as
// "sh -c <script>" so that it is POSIX sh whatever the login shell is; it
// is one line with no '!', so csh-style login shells can pass it through.
func ListArgs(h Host, remoteLists, globs []string) []string {
	return []string{"ssh", "-o", "BatchMode=yes", h.Remote(), "sh", "-c", shQuoteAlways(listScript(remoteLists, globs))}
}

// The script, in fragments. Records go to stdout, one per line: a type
// letter, a space and the path verbatim (see ListResult), then "E" last.
// All fragments are single-line and free of '!'; '#' only appears inside
// ${...} or quoted, never where it would start a comment.
const (
	// nl, cr and tab hold those characters without writing them literally;
	// b holds '!' so that words containing it can be spelt without it. IFS
	// is newline only, so unquoted expansions of patterns (which contain no
	// whitespace) and of the newline-separated home list never word-split
	// on spaces.
	lsPrelude = `nl=$(printf "\nx"); nl=${nl%x}; cr=$(printf "\r"); tab=$(printf "\t"); b=$(printf "\041"); IFS=$nl; set +f; `

	// Remote list: the first candidate that is non-empty (test -s, as the
	// 0.9.0 probe). Lines follow rsync --files-from: a trailing CR is
	// stripped, blank and #/; comment lines are skipped; the last line may
	// lack a newline. Entries are relative to /, as rsync's source is /.
	lsRemoteHead = `f=; for c in `
	lsRemoteTail = `; do if test -s "$c"; then f=$c; break; fi; done; ` +
		`if test -n "$f"; then printf "R %s\n" "$f"; ` +
		`while IFS= read -r p || test -n "$p"; do p=${p%"$cr"}; case $p in ""|"#"*|";"*) continue;; esac; ` +
		`if test -e "/${p#/}"; then printf "F %s\n" "$p"; else printf "M %s\n" "$p"; fi; done < "$f"; ` +
		`else printf "N\n"; fi; `

	// chk MATCH HOME RHOME classifies one glob match. HOME is the literal
	// home and RHOME its resolved path, both empty for an absolute pattern.
	// A word that is neither a symlink nor existing is an unmatched literal
	// pattern and is dropped. The match is a symlink match if it, or the
	// directory holding it, resolves somewhere other than its literal
	// spelling under the (resolved) base; that covers any symlinked
	// directory between the base and the match. A symlink match is listed
	// only for a ~/ pattern and only if it resolves inside RHOME. Of the
	// rest, only regular files are listed. ld/ds cache the directory check
	// for consecutive matches in one directory; callers reset ld per base.
	lsChk = `chk() { ` +
		`case $1 in *"$nl"*) printf "X %s\n" "$(printf %s "$1" | tr "\n" "?")"; return;; esac; ` +
		`if test -L "$1" || test -e "$1"; then :; else return; fi; ` +
		`d=${1%/*}; test -n "$d" || d=/; ` +
		`if test "$d" = "$ld"; then :; else ld=$d; e=${d#"$2"}; e=$3$e; ` +
		`if test "$(readlink -f -- "$d")" = "$e"; then ds=; else ds=1; fi; fi; ` +
		`if test -L "$1" || test -n "$ds"; then ` +
		`if test -z "$2"; then printf "L %s\n" "$1"; return; fi; ` +
		`case $(readlink -f -- "$1") in "$3"/*) ;; *) printf "L %s\n" "$1"; return;; esac; fi; ` +
		`if test -f "$1"; then printf "G %s\n" "$1"; fi; }; `

	// Absolute patterns: each expanded unquoted, once.
	lsAbsHead = `ld=; for p in `
	lsAbsTail = `; do for m in $p; do chk "$m" "" ""; done; done; `

	// Home directories: field 6 of getent passwd (or /etc/passwd), split
	// on newlines with globbing off. A home is skipped unless absolute and
	// a directory, if it is / (after dropping one trailing slash), if it
	// has whitespace or *?[, or if seen before. Each ~/ pattern's remainder
	// is expanded after the quoted, never-globbed home.
	lsHomeHead = `hs=$( { getent passwd 2>/dev/null || cat /etc/passwd; } | cut -s -d: -f6 ); s=$nl; set -f; ` +
		`for h in $hs; do set +f; h=${h%/}; case $h in /*) ;; *) continue;; esac; ` +
		`case $h in *" "*|*"$tab"*|*"$cr"*|*\**|*\?*|*\[*) continue;; esac; ` +
		`case $s in *"$nl$h$nl"*) continue;; esac; s=$s$h$nl; ` +
		`test -d "$h" || continue; rh=$(readlink -f -- "$h"); test -n "$rh" || continue; ` +
		`ld=; for p in `
	lsHomeTail = `; do for m in "$h"/$p; do chk "$m" "$h" "$rh"; done; done; done; set +f; `

	// Manifest on stdin: T if the path exists, else M. Then the sentinel.
	lsManifest = `while IFS= read -r p || test -n "$p"; do case $p in "") continue;; esac; ` +
		`if test -e "$p"; then printf "T %s\n" "$p"; else printf "M %s\n" "$p"; fi; done; ` +
		`printf "E\n"; exit 0`
)

// listScript builds the one-line remote script for the given remote list
// candidates and glob patterns (each starting with / or ~/).
func listScript(remoteLists, globs []string) string {
	var abs, home []string
	for _, g := range globs {
		if rest, ok := strings.CutPrefix(g, "~/"); ok {
			home = append(home, rest)
		} else {
			abs = append(abs, g)
		}
	}
	var b strings.Builder
	b.WriteString(lsPrelude)
	if len(remoteLists) > 0 {
		b.WriteString(lsRemoteHead + scriptWords(remoteLists) + lsRemoteTail)
	}
	if len(globs) > 0 {
		b.WriteString(lsChk)
	}
	if len(abs) > 0 {
		b.WriteString(lsAbsHead + scriptWords(abs) + lsAbsTail)
	}
	if len(home) > 0 {
		b.WriteString(lsHomeHead + scriptWords(home) + lsHomeTail)
	}
	b.WriteString(lsManifest)
	return b.String()
}

// scriptWords quotes each of ws as one shell word for the list script.
// Any '!' is spelt as "$b" (set to '!' by the prelude) so that the script
// text never contains '!'.
func scriptWords(ws []string) string {
	q := make([]string, len(ws))
	for i, w := range ws {
		parts := strings.Split(w, "!")
		for j, p := range parts {
			parts[j] = shQuote(p)
		}
		q[i] = strings.Join(parts, `"$b"`)
	}
	return strings.Join(q, " ")
}

// shQuoteAlways single-quotes s for a POSIX shell, even when it would be
// safe bare.
func shQuoteAlways(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// parseList parses the list script's output. The last line must be the
// "E" sentinel; any line that isn't a known record is an error.
func parseList(stdout string) (ListResult, error) {
	var r ListResult
	body, ok := strings.CutSuffix(stdout, "\nE\n")
	if !ok {
		if stdout != "E\n" {
			return r, errListIncomplete
		}
		body = ""
	}
	if body == "" {
		return r, nil
	}
	for _, line := range strings.Split(body, "\n") {
		if line == "N" {
			r.NoList = true
			continue
		}
		if len(line) < 3 || line[1] != ' ' {
			return r, fmt.Errorf("files list output has an unrecognised line: %q", line)
		}
		p := line[2:]
		switch line[0] {
		case 'R':
			r.Remote = p
		case 'F':
			r.Listed = append(r.Listed, p)
		case 'G':
			r.Globbed = append(r.Globbed, p)
		case 'T':
			r.Tracked = append(r.Tracked, p)
		case 'M':
			r.Missing = append(r.Missing, p)
		case 'L':
			r.Symlinks = append(r.Symlinks, p)
		case 'X':
			r.Skipped = append(r.Skipped, p)
		default:
			return r, fmt.Errorf("files list output has an unrecognised line: %q", line)
		}
	}
	return r, nil
}

// listOutcome interprets a finished list step. It returns the parsed
// result, or a failure status and message.
func listOutcome(s proc.StepResult, timeout time.Duration) (ListResult, Status, string) {
	switch {
	case s.Err != nil:
		return ListResult{}, StatusFailed, fmt.Sprintf("files list step failed to start: %v", s.Err)
	case s.TimedOut:
		return ListResult{}, StatusTimedOut, fmt.Sprintf("files list step timed out after %s", timeout)
	case s.Interrupted:
		return ListResult{}, StatusFailed, "files list step interrupted"
	case s.ExitCode != 0:
		return ListResult{}, StatusFailed, fmt.Sprintf("files list step exited %d", s.ExitCode)
	}
	r, err := parseList(s.Stdout)
	if err != nil {
		return ListResult{}, StatusFailed, err.Error()
	}
	return r, StatusOK, ""
}
