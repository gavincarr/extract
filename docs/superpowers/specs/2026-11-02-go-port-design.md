# extract: Go port design

Date: 2026-11-02
Status: draft, awaiting approval (builder check done)

## Intent

`extract` is a safety-net backup. Each night it rsyncs important config
(mostly `/etc`, occasionally other paths) off 20–100 remote hosts into
per-host trees under a local root. Hook scripts then typically commit
those trees to git. The config is supposed to live in ansible, so the
backups are rarely needed. Occasionally they've been the only copy of
something that mattered.

The current implementation is ~200 lines of Perl (`bin/extract`,
`lib/Extract/*.pm`). The owner no longer works in Perl, so it's being
ported to Go. Go is a single binary with no CPAN dependencies and is the
owner's working language. The port is also a chance to make extract
**fail loudly**. The worst outcome for a safety net is backups that
silently stopped working months ago and are only noticed on the day
they're needed.

## Priorities

1. **Correctness.** Copy exactly what the Perl version copied, into the
   same place.
2. **Fail loudly.** Every host, rsync or hook failure must be visible in
   cron mail and reflected in the exit status.
3. **Quiet when healthy.** A fully successful run prints nothing, so
   cron mail only arrives when something's wrong.
4. Speed is nice to have, not a goal.

## Goals

- A drop-in replacement for `bin/extract` on existing installs. The
  existing `/etc/extract/extract.conf` keeps working unchanged. Two
  shipped hooks must be fixed and redeployed (see Hook fixes). The owner
  is fine with updating configs and hooks where there's clear value.
- Every failure is surfaced: rsync errors, timeouts, hook failures, and a
  host-list command that fails.
- A timeout actually kills the rsync (and its ssh child).
- A structured per-run result that a future notifier (Slack, chat,
  webhooks) can consume without reworking the runner.

## Non-goals (v1)

- Notifications beyond stdout/stderr. These are a planned follow-up; v1
  only shapes the result type so the follow-up is easy.
- Packaging. A `.deb` will come later; v1 is deployed by copying the
  binary.
- Parallel extraction. The runner should not preclude it, but v1 runs
  hosts one at a time.
- Porting the bash hooks or `bin/extract_git_report`. They stay bash.
- `EXTRACT_FILES_*` accepting a list of files (an old TODO item). Deferred.

## Anti-goals

- **Silent failure.** Any path where something went wrong and extract
  exits 0 with no output is a bug.
- **Noisy success.** Output on healthy runs trains people to ignore cron
  mail, which defeats the point.
- **Breaking existing hooks or configs** without a deliberate decision.

## Compatibility contract

These are fixed. Changing any of them is a breaking change for existing
installs.

### CLI

`extract [-v...] [-n] [-c <config>] [-t <timeout_secs>] [<host> ...]`

| Flag | Meaning | Default |
|---|---|---|
| `-c`, `--config` | config file | `/etc/extract/extract.conf` |
| `-t`, `--timeout` | per-host rsync timeout, seconds | 60 |
| `-n`, `--noop` | dry run (see below) | off |
| `-v`, `--verbose` | verbosity, repeatable (counter) | 0 |
| `-h`, `--help` | usage | |

Added: long-only `--version`, following the user's `release-flow`
conventions (`internal/version/VERSION`, embedded).

### Config file

- The format is shell-style `KEY=value` lines, optionally quoted, with
  `#` comments. Bash hooks `source` the same file, so it must remain
  valid shell. Unknown keys are ignored; hooks use their own keys, such
  as `EXTRACT_GIT_REPO_MODE`.
- Keys extract reads: `EXTRACT_ROOT` (required), `EXTRACT_HOSTS`,
  `EXTRACT_HOST_CMD`, exactly one source (`EXTRACT_TREE`,
  `EXTRACT_FILES_LOCAL`, or "remote files": `EXTRACT_FILES_REMOTE`
  and/or globs from `EXTRACT_FILES_GLOBS` / `EXTRACT_FILES_GLOBS_FILE`),
  `PRE_EXTRACT_SETUP`, `PRE_EXTRACT_HOST`, `POST_EXTRACT_HOST` and
  `POST_EXTRACT_TEARDOWN`.
  The four hook keys are comma-separated lists, with whitespace around
  commas ignored.
- `EXTRACT_FILES_REMOTE` is a whitespace-separated list of remote paths
  (quoted when there are several, since hooks `source` the file; paths
  therefore can't contain whitespace). A value that is set but blank
  still counts as set for the "exactly one of" rule, and is then a
  config error (no paths). Paths are not checked at load time; they are
  remote. See rsync invocation for how one is chosen per host.
  *(Added in 0.9.0: RPM- and DEB-based hosts generate their lists with
  different tools under different names.)*
- `EXTRACT_FILES_GLOBS` (a quoted, whitespace-separated list) and
  `EXTRACT_FILES_GLOBS_FILE` (a local file, one pattern per line, a
  relative path taken against the config file's directory) select extra
  files per host by `/` or `~/` glob. Either or both may be set, with or
  without `EXTRACT_FILES_REMOTE`; globs with `EXTRACT_TREE` or
  `EXTRACT_FILES_LOCAL` is a config error. *(Added in 0.10.0; see
  `2026-11-02-files-globs-design.md`.)*
- New optional key `EXTRACT_SCRIPTS_DIR`, defaulting to
  `/etc/extract/scripts`. *(My call. It replaces Perl's
  `$RealBin/../etc/scripts` fallback, which never matched the repo
  layout.)*

### Host resolution

The host list comes from CLI arguments if any are given (`EXTRACT_HOST_CMD`
is then not run). Otherwise it is `EXTRACT_HOSTS` (split on whitespace)
followed by the stdout of `EXTRACT_HOST_CMD` (run via `sh -c`, output split
on whitespace), de-duplicated keeping the first occurrence. A host command
that fails, times out or prints no hosts is an error even when
`EXTRACT_HOSTS` supplied hosts (it is not silently skipped). Hosts
may be `user@host`. The user is passed to rsync, and the local directory
is named after the host part only.

### rsync invocation

The rsync commands must match the Perl version's (via `File::Rsync`):

- base flags: `--archive --copy-links --delete`
- `EXTRACT_TREE=/etc` → source `[user@]host:/etc`, destination `host`
  (no trailing slash, so files land in `<root>/<host>/etc/...`)
- `EXTRACT_FILES_REMOTE=/p` → `--files-from=:/p`, source
  `[user@]host:/`, destination `host`
- `EXTRACT_FILES_REMOTE="/p1 /p2 ..."` (two or more paths) → first, for
  each host, a probe:
  `ssh -o BatchMode=yes [user@]host 'for f in /p1 /p2; do if test -s "$f"; then printf "%s\n" "$f"; exit 0; fi; done; exit 3'`
  (paths single-quoted when they need it; one line of POSIX sh with no
  `!`). It runs before the pre-host hooks and the host-directory mkdir,
  from `EXTRACT_ROOT`, with the `-t` timeout, and under `-n` too (it is
  read-only). Exit 0 printing one of the candidates chooses it, and rsync
  gets `--files-from=:<chosen>`. Exit 3 with no output means no
  candidate is a non-empty file on that host (not 1, which a login
  shell that cannot run the loop, such as csh, also returns). Anything else (ssh's 255,
  say, or exit 0 printing something else) is a probe failure. On any
  failure the host is failed (timed out on a timeout) and its pre-host
  hooks, rsync and post-host hooks are all skipped; other hosts carry
  on. A single path runs no probe. The probe is recorded on
  `HostResult.Probe`, the chosen path on `HostResult.FilesFrom`.
- `EXTRACT_FILES_LOCAL=/p` → `--files-from=/p`, source
  `[user@]host:/`, destination `host`
- globs set (with or without `EXTRACT_FILES_REMOTE`, any number of
  paths) → instead of the probe, one ssh call per host runs a POSIX sh
  list-building script (`sh -c '<script>'`) with the host's manifest
  (`$EXTRACT_ROOT/.extract-state/<host>.files`) on stdin, at the same
  point and under the same rules as the probe (`-t`, `-n`, failure skips
  the host's hooks and rsync). rsync then gets the base flags plus
  `--delete-missing-args --files-from=<local temp file>` (written outside
  the host directory and removed afterwards), source `[user@]host:/`,
  destination `host`. The manifest is rewritten only after rsync exits 0
  or 24, never under `-n`. Needs rsync >= 3.1.0 on both ends. *(Added in
  0.10.0; see `2026-11-02-files-globs-design.md`.)*
- run with the working directory set to `EXTRACT_ROOT`; the host
  directory is created if missing
- `-n` adds `--dry-run`
- `--quiet` at verbosity 0–2; `--verbose` instead at `-vvv`. *(My call.
  Perl used `--verbose` at `-v`, but `-v` now means "summary".)*

### Hook contract

- **Resolution.** A bare name resolves under `EXTRACT_SCRIPTS_DIR`;
  absolute paths are used as-is.
- **Arguments.**
  - `PRE_EXTRACT_SETUP` and `POST_EXTRACT_TEARDOWN`:
    `$1=EXTRACT_ROOT $2=<config path>`
  - `PRE_EXTRACT_HOST` and `POST_EXTRACT_HOST`: the same, plus
    `$3=<host> $4=<files path>`. `$4` is the files list given to rsync:
    the `EXTRACT_FILES_REMOTE` path chosen for that host (the value
    itself when it lists one path), the `EXTRACT_FILES_LOCAL` value, or
    an empty string in tree mode. With globs it is the chosen
    `EXTRACT_FILES_REMOTE` path, or an empty string when globs are used
    alone or that host had no list; never the temp file rsync is given.
  - `$2` must be the config path as an absolute path, because hooks
    `source` it after `cd`-ing.
- **Order.** Setup hooks run once. Then, for each host: the files-list
  probe (several `EXTRACT_FILES_REMOTE` paths only) or, with globs, the
  list-building step, pre-host hooks,
  rsync, post-host hooks. Teardown hooks run once at the end. A failed
  probe skips both pre- and post-host hooks.
- **Environment and working directory.** Hooks inherit extract's
  environment and run with the working directory set to `EXTRACT_ROOT`.
  stdin is `/dev/null`.
- **Lists stop at the first failure.** Within one hook list, the first
  failing hook stops the rest of that list. For example, in
  `post_extract_git,post_extract_touch`, the touch (a freshness marker)
  must not claim health after git failed. *(My call.)*
- **Hook timeout.** New optional key `EXTRACT_HOOK_TIMEOUT` (seconds,
  default 600) applies to each hook and to `EXTRACT_HOST_CMD`. A hook
  that times out is killed (process group, as with rsync) and counts as
  failed. A hung `git` or `ssh-add` must never stop extract from
  exiting. `0` means no timeout, and the same holds for `-t 0`, matching
  Perl. *(My call.)*

## Hook fixes

Two shipped hooks exit non-zero on their normal success path. Perl hid
this by ignoring exit codes:

- `scripts/post_extract_git` ends in `git commit`, which exits 1 when
  there is nothing to commit, i.e. most hosts on most nights. Fix: after
  `git add`, commit only if something is staged, and exit 0 otherwise.
- `scripts/pre_extract_load_ssh_keys` ends in
  `ssh-add -l | grep -q 'no identities' && ssh-add`, which exits 1 when
  keys are already loaded (the normal case). Fix: exit 0 in that case.

Also: `scripts/post_extract_tarball` calls an undefined `die`. Define it
like the other hooks do.

Installed copies under `/etc/extract/scripts` are separate from the
repo, so the cutover must redeploy them. If someone forgets, every run
fails loudly, so the mistake can't go unnoticed.

## Deliberate behaviour changes (all toward failing loudly)

| Area | Perl today | Go |
|---|---|---|
| Hook exit codes | ignored | non-zero = failure (after the Hook fixes above) |
| Hook not found / not executable | prints "skipping", continues | config error, checked at startup before any rsync (exit 2) |
| Hook invocation | `qx()` through a shell, args single-quoted | exec'd directly with an argument list, no shell |
| Timeout | `alarm` interrupts Perl; rsync likely keeps running | rsync runs in its own process group; on deadline the group is killed (SIGTERM, then SIGKILL after a short grace period) |
| `EXTRACT_HOST_CMD` fails | output used regardless | non-zero exit or empty output = fatal (exit 2) |
| `-c` file unreadable | silently falls back to `/etc/extract/extract.conf` | fatal (exit 2) |
| Both `EXTRACT_FILES_REMOTE` and `_LOCAL` set | local silently wins | fatal (exit 2) |
| Exit status | 0 unless something `die`d | see Exit codes |
| Output | progress chatter at `-v` | see Output levels |

Per-host failure semantics *(my calls except where noted)*:

- **Pre-host hook fails.** The host is marked failed, and its rsync and
  post-host hooks are skipped.
- **rsync exits 24** (files vanished during transfer). The host counts
  as OK with a warning. The warning is shown at `-v` and above, not by
  default. *(Owner's decision.)*
- **rsync exits non-zero, including 23** (partial transfer, usually a
  `--files-from` entry missing on the host). The host is marked failed.
  Post-host hooks **still run**, matching Perl (deliberate since 0.5.1).
  23 counts as failure because file lists are refreshed at the same
  cadence as extract (possibly as a pre-host hook), so a stale list is
  real drift. *(Owner's decision.)*
- **rsync times out.** The host is marked timed out, and post-host hooks
  are **skipped**, matching Perl. This matters: with `--delete`, a
  half-finished rsync can leave deletions applied without the new files,
  and `post_extract_git` would commit that bogus state.
- **Post-host hook fails.** The host is marked failed.
- **Setup hook fails.** The run aborts: no hosts, no teardown, exit 1.
- **Teardown hook fails.** The run is reported as failed, exit 1.
- One host failing never stops the remaining hosts.

Host names are validated before use, since each one becomes both a
directory name and part of an rsync argument. Reject empty names, names
containing `/`, names starting with `-`, and `.` / `..`. *(My call.)*

ssh under cron must never sit at a prompt until the timeout. Pass
`-e "ssh -o BatchMode=yes"` to rsync, so a host-key or password prompt
fails immediately with a clear error. *(My call. This is the one
addition to the rsync command, and it is pinned in the golden tests.)*

## Output levels

All diagnostic output goes through `slog` (the user's convention). The
handler produces plain, timestamp-free lines suitable for cron mail.
Verbosity maps to level. Errors and warnings go to **stderr**. Info and
debug lines (summary, per-host lines, `-vvv` detail, the `-n` plan) go to
**stdout**. *(My call.)*

| Level | All OK | Some failures |
|---|---|---|
| default | nothing | each failed / timed-out host or failed hook, with its error and rsync stderr / hook output |
| `-v` | one summary line, e.g. `42 hosts ok` | summary line (`40 ok, 1 failed, 1 timed out`) plus each failure itemised |
| `-vv` | adds one line per successful host (with duration) | same |
| `-vvv` | adds rsync commands, rsync stdout/stderr, and hook output for successful steps | same |

Under `-n`, extract prints what it would do (hooks it would run, rsync
commands) regardless of `-v`. rsync runs with `--dry-run`, hooks are
**not** run, and `EXTRACT_HOST_CMD` **is** run (it's needed to get the
host list). The files-list probe also runs (it is read-only), and the plan
shows it as `checked files lists: <ssh command> -> <chosen>` before the
host's hook lines, or `... (failed)` and nothing else for that host. *(My call: noop output ignores `-v` because asking for a dry
run is asking to see the plan.)*

## Exit codes

- `0`: every host succeeded and every hook succeeded.
- `1`: the run happened but at least one host failed or timed out, or
  a hook failed.
- `2`: usage or configuration error. Nothing was extracted. This
  covers bad flags, a missing or unreadable config, a missing or
  unwritable `EXTRACT_ROOT`, bad source keys, an invalid host, a hook
  that's missing or not executable, and a failed `EXTRACT_HOST_CMD`.

Usage errors follow the user's `go-stack` kong conventions: usage goes to
stderr, kong's exit 80 is mapped to 2, and `--help` exits 0.

## Run result (seam for future notifiers)

The runner returns a value, roughly:

```go
type Result struct {
    Start, End     time.Time
    SetupHooks     []HookResult
    Hosts          []HostResult
    TeardownHooks  []HookResult
}
type HostResult struct {
    Host, User     string
    Status         Status        // OK, Failed, TimedOut, Skipped
    Warnings       []string      // e.g. rsync exit 24
    Rsync          *StepResult   // argv, exit code, stdout, stderr, duration
    PreHooks       []HookResult
    PostHooks      []HookResult
}
```

Printing (the output-levels table) is a separate reporter that consumes
`Result`. Exit-code selection is derived from `Result`. A future
notifier is just another consumer. The exact shape is the builder's,
but the runner must not print directly.

## Structure

The Go code lives in this repo alongside the Perl until the port matches
it, then the Perl is removed.

- `go.mod`: module `github.com/gavincarr/extract`
- `cmd/extract/`: kong CLI, wiring, slog setup
- `internal/config/`: parse and validate the config file
- `internal/extract/`: runner (host loop, rsync, hooks) → `Result`
- `internal/report/`: `Result` → output lines + exit code
- `internal/version/`: `VERSION` + embed

Libraries: kong (+ kong-help-colours), `slog`. The shell-style config is
read by a small parser in `internal/config` that takes values literally,
like Perl's Config::Tiny did: surrounding quotes are stripped (`\"` and
`\\` unescape inside double quotes), `#` comments and a leading `export`
are allowed, and there is no `$` expansion, so existing configs keep
meaning what they meant to the Perl version.

Unchanged and kept: `scripts/*` (bash hooks), `bin/extract_git_report`,
`conf/extract.conf.dist`, `conf/cron.d/extract`.

## Testing

- **Config.** Table tests over quoting, comments, whitespace in hook
  lists, missing or conflicting keys. Also parse the shipped
  `conf/extract.conf.dist` with sample values uncommented.
- **rsync commands (golden).** For each source mode × `user@`/no user ×
  `-n` × verbosity, assert the exact rsync argument list. These pin the
  compatibility contract.
- **Runner integration.** Put a fake `rsync` and fake hooks (small shell
  scripts) on `PATH` / in a temp scripts dir. They record their
  arguments and can exit non-zero, write to stderr, or sleep past the
  timeout. Cover every row of the failure-semantics list, including that
  a timed-out fake rsync and its child are actually killed. No real ssh
  or rsync is needed.
- **CLI grammar.** A table test through `kong.New` + `Parse`, including
  `-vvv` as a counter. Per `go-stack`, a `[]bool` verbosity flag hangs;
  only a grammar test catches it.
- **Output.** Assert stdout and stderr separately for each verbosity
  level, healthy and failing.
- **One manual smoke run** against a real host before the Perl is
  removed.

## Cutover

1. Build the Go version to parity, with all the above tests passing.
2. Redeploy the fixed hooks to `/etc/extract/scripts`. Smoke-test against real hosts with the existing config and hooks,
   and compare the resulting trees with a Perl run.
3. Remove `bin/extract`, `lib/`, `t/`, and the Perl-specific bits of
   `.gitignore`. Rewrite `README.md` (config documentation moves there
   from the POD) and update `CLAUDE.md`.
4. Deploy by copying the binary over `/usr/bin/extract`.

## Left to the builder

- The exact `Result` field names, the reporter's line formats, and
  the slog handler implementation.
- The SIGTERM→SIGKILL grace period.
- Whether to dedupe a repeated host on the command line.
- Whether `-n` creates missing host directories (Perl does).
- Starting version number.
- How to handle orphaned children if extract itself is killed.
