# EXTRACT_FILES_GLOBS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add glob-selected remote files (absolute or `~/` patterns,
inline or from a globs file), deletion tracking through a per-host
manifest plus `--delete-missing-args`, a shipped curated globs list, and
an `EXTRACT_ROOT` permission warning.

**Architecture:**
- When globs are configured, one ssh call per host runs a POSIX `sh`
  script. The script picks the remote list (as 0.9.0's probe does),
  expands the globs, and checks the manifest paths it is fed on stdin. It
  prints typed records.
- extract turns those records into a local `--files-from` temp file and
  runs one rsync with `--delete-missing-args`. After a good rsync it
  rewrites the manifest.
- Configs without globs keep the 0.9.0 code path untouched.

**Tech Stack:** Go (stdlib `testing`, `slog`), POSIX `sh` on the remote,
rsync >= 3.1.0.

**Spec:** `docs/superpowers/specs/2026-11-02-files-globs-design.md`.
Read it first: it is the authority on behaviour, and this plan only fixes
names, files and tests. Also read `CLAUDE.md`.

## Global Constraints

**Branch and commit discipline:**
- Work on a topic branch: `git switch -c files-globs` from `master`. It
  is fast-forward merged later by the user.
- Never `git push`.
- Use Conventional Commits, ending with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

**After every task**, all of these must pass:
`go vet ./... && gofmt -l . && go test ./...`
`gofmt -l` must print nothing.

**Code conventions:**
- Log only through `slog`; never import `log` or call `slog.SetDefault`.
- `extract.Run` never prints. Output belongs in `internal/report` or
  `cmd/extract`.
- Configs without `EXTRACT_FILES_GLOBS`/`EXTRACT_FILES_GLOBS_FILE`
  behave exactly as in 0.9.0. Existing tests must pass unmodified,
  except where a task explicitly changes a signature. The one exception
  in behaviour is the permission warning.

**The remote script:**
- Its text contains no newline and no `!`, and runs under POSIX `sh`
  (test under `dash` and `bash`).
- It is sent as `ssh -o BatchMode=yes <user@host> "sh -c '<script>'"`,
  as separate argv elements `sh`, `-c`, `<single-quoted script>`. ssh
  joins them for the login shell.

**Fixed values:**

| Item | Value |
|---|---|
| Manifest path | `$EXTRACT_ROOT/.extract-state/<host>.files`; dir mode `0700`, file `0600` |
| Extra rsync flag in globs mode | `--delete-missing-args` |
| Version | `internal/version/VERSION` = `0.10.0` |
| Pattern prefixes | `/` or `~/` only |

## Review Focus

1. **Glob matches with spaces** in the file name (`~/.ssh/my key`) are
   extracted intact. Tested in Task 3.
2. **Remote lists with blank lines, `#`/`;` comment lines or CRLF
   endings** behave as rsync's `--files-from` does: those lines are
   ignored, never treated as paths, and never written to the manifest.
   Tested in Task 3.
3. **The same path from the remote list, a glob and the manifest**
   appears once in the rsync list and once in the manifest. Tested in
   Task 4.
4. **A manifest that exists for a host whose tree was wiped** re-fetches
   everything, with no error. Tested in Task 4.
5. **A large manifest on stdin** (10k+ lines) doesn't deadlock or
   truncate. Tested in Task 1 (1 MiB through `cat`) and Task 3 (10k
   manifest lines).

---

### Task 1: stdin for steps (`internal/proc`)

**Files:**
- Modify: `internal/proc/proc.go`
- Test: `internal/proc/proc_test.go`

**Interfaces:**
- Produces:
  - `func RunContextStdin(ctx context.Context, argv []string, dir string, timeout time.Duration, stdin io.Reader) StepResult`.
    A nil `stdin` means `/dev/null`.
  - `RunContext` becomes a call to `RunContextStdin(..., nil)`. Its
    behaviour is unchanged.

- [ ] **Step 1: Write failing tests.**
  - `TestRunContextStdinPassesInput`: run `cat` with
    `strings.NewReader(s)`, where `s` is 1 MiB of `"line\n"` repeated.
    Assert `Stdout == s` and `ExitCode == 0`.
  - `TestRunContextStdinNilIsDevNull`: run
    `sh -c 'test -c /dev/stdin && readlink /proc/self/fd/0'` with nil.
    Assert stdout is `/dev/null`.
  - `TestRunContextStdinTimeoutKills`: run `sleep 5` with a stdin reader
    and a 100 ms timeout. Assert `TimedOut`.
- [ ] **Step 2: Run them.** `go test ./internal/proc/ -run Stdin`.
  Expect FAIL (undefined).
- [ ] **Step 3: Implement.** Set `cmd.Stdin = stdin` when it isn't nil.
  `exec.Cmd` copies a non-file reader in a goroutine, and the existing
  `WaitDelay` already bounds it.
- [ ] **Step 4: Check.** `go test -race ./internal/proc/` (with
  `CGO_ENABLED=1`). Expect PASS.
- [ ] **Step 5: Commit.** `feat(proc): allow passing stdin to a step`.

---

### Task 2: globs config (`internal/config`)

**Files:**
- Create: `internal/config/globs.go`
- Modify: `internal/config/config.go` (`Config`, `loadSource`)
- Test: `internal/config/globs_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `Config.Globs []string`: validated patterns. Inline
    `EXTRACT_FILES_GLOBS` (split with `strings.Fields`) come first, then
    the globs file's patterns in file order. Duplicates are removed,
    keeping the first occurrence.
  - `Config.GlobsFile string`: the absolute path of the globs file, or
    `""`.
  - `func (c *Config) UsesGlobs() bool`, which is `len(c.Globs) > 0`.
  - `func ParseGlobsFile(path string) ([]string, error)` (exported, for
    the shipped-file test):
    - one pattern per line, trimmed;
    - blank and `#` lines skipped, plus a trailing `\r`;
    - each pattern validated;
    - errors are formatted `globs file <path> line <n>: <reason>`.
  - `func validGlob(p string) error`:
    - must start with `/` or `~/`;
    - must contain no whitespace and no control characters (`unicode.IsControl`).
- Source rules (spec "Config"):
  - Globs set (either key) puts `Mode` at `ModeFilesRemote`, even when
    `EXTRACT_FILES_REMOTE` is unset (then `Source == ""`).
  - Globs plus `EXTRACT_TREE` gives the error
    `EXTRACT_FILES_GLOBS/EXTRACT_FILES_GLOBS_FILE can't be combined with EXTRACT_TREE`;
    likewise for `EXTRACT_FILES_LOCAL`.
  - With no source key and no globs, the existing "one of …" error
    stays. Mention globs in its text.
- Path resolution: a relative `EXTRACT_FILES_GLOBS_FILE` resolves
  against `filepath.Dir(c.Path)`. The file must be a readable regular
  file.
- Empty values:
  - a set but whitespace-only `EXTRACT_FILES_GLOBS` gives
    `EXTRACT_FILES_GLOBS is set but lists no patterns`;
  - a globs file with no patterns gives
    `globs file <path> lists no patterns`.

- [ ] **Step 1: Write failing tests (table-driven).**
  - `TestLoadGlobsInline`: `EXTRACT_FILES_GLOBS="~/.ssh/* /var/spool/cron/*"`.
    Expect `Mode == ModeFilesRemote`, `Source == ""`, and
    `Globs == {"~/.ssh/*", "/var/spool/cron/*"}`.
  - `TestLoadGlobsFileRelativeToConfig`: the conf holds
    `EXTRACT_FILES_GLOBS_FILE=extract.globs`, and `extract.globs` sits
    next to it with comments, blank lines and CRLF. Expect `GlobsFile`
    absolute and the patterns parsed.
  - `TestLoadGlobsUnion`: inline `~/.a ~/.b` plus a file with
    `~/.b ~/.c` gives `{"~/.a", "~/.b", "~/.c"}`.
  - `TestLoadGlobsWithFilesRemote`: both keys set. Expect
    `FilesRemote()` and `Globs` both populated.
  - `TestLoadGlobsErrors`, with error substrings:
    - `relative/x` gives `must start with / or ~/`;
    - `~bob/.ssh` gives the same;
    - a tab in a file pattern gives `line 3:`;
    - a missing file gives the path;
    - an empty file gives `lists no patterns`;
    - whitespace-only inline gives `lists no patterns`;
    - plus `EXTRACT_TREE` gives `can't be combined with EXTRACT_TREE`;
    - plus `EXTRACT_FILES_LOCAL` gives `can't be combined with EXTRACT_FILES_LOCAL`.
  - `TestLoadShippedGlobsDist`: `ParseGlobsFile("../../conf/extract.globs.dist")`
    gives no error and at least 50 patterns, including `~/.ssh/*` and
    `/var/spool/cron/crontabs/*`. (The file is created in Task 6. Mark
    this test `t.Skip` until then, and remove the skip in Task 6.)
- [ ] **Step 2: Run them.** `go test ./internal/config/`. Expect FAIL.
- [ ] **Step 3: Implement** `globs.go` and the `loadSource` changes as
  specified.
- [ ] **Step 4: Check.** `go test ./internal/config/`. Expect PASS; all
  old tests unchanged.
- [ ] **Step 5: Commit.** `feat(config): add EXTRACT_FILES_GLOBS and EXTRACT_FILES_GLOBS_FILE`.

---

### Task 3: the list-building script and its parser (`internal/extract/listscript.go`)

**Files:**
- Create: `internal/extract/listscript.go`, `internal/extract/listscript_test.go`
- Modify: `internal/testutil/fakes.go` (fake `getent` support)

**Interfaces:**
- Consumes: `config.Config.Globs`, `Config.FilesRemote()`, and
  `shQuote` from `probe.go`.
- Produces:
  - `func ListArgs(h Host, remoteLists, globs []string) []string`, the
    full ssh argv:
    `ssh -o BatchMode=yes <h.Remote()> sh -c <shQuote(script)>`.
    `shQuote` must always quote the script.
  - `type ListResult struct { Remote string; NoList bool; Listed, Globbed, Tracked, Missing, Symlinks, Skipped []string }`.
    - `Remote` is the chosen remote list path, or `""`.
    - `NoList` means remote lists were configured but none was non-empty.
    - The slices hold, in order, the F, G, T, M and L records.
    - `Skipped` holds X records (newline-bearing names, shown with `?`
      in place of the newline).
  - `func parseList(stdout string) (ListResult, error)`. A missing
    sentinel gives the error `files list output incomplete (no end marker)`.
  - `func listOutcome(s proc.StepResult, timeout time.Duration) (ListResult, Status, string)`,
    with messages in the same style as `probeOutcome`:
    - `files list step failed to start: …`
    - `… timed out after …`
    - `… interrupted`
    - `files list step exited N`
    - a parse error
- **Record format** (one per line, the type letter then a space, then the
  path verbatim):
  - `R <path>`: the chosen remote list;
  - `N`: no non-empty remote list;
  - `F <path>`: an existing remote-list entry;
  - `G <path>`: an accepted glob match;
  - `T <path>`: an existing manifest path;
  - `M <path>`: a remote-list or manifest path that doesn't exist;
  - `L <path>`: a skipped symlinked glob match;
  - `X <path>`: a skipped newline-bearing name;
  - `E`: the sentinel, last line.
  - The script exits 0 whenever it reaches `E`.
- **Inputs:**
  - Remote list candidates and patterns are embedded in the script as
    shell-quoted words.
  - The manifest arrives on stdin.
- **Remote-list lines** follow rsync `--files-from` rules: strip `\r`;
  skip blank lines and lines starting with `#` or `;`. A kept line `p` is
  F if `test -e "/${p#/}"`, else M. The path is printed as written in the
  list.
- **Globs:**
  - Expand unquoted with `set +f` and `IFS` reset.
  - Skip a word that is still the literal pattern and doesn't exist.
  - For `~/` patterns, loop over homes and prefix the pattern with the
    literal home. Quote the home part so it is never globbed: build
    `"$home"/rest` with only `rest` unquoted.
  - In-home symlink test:
    `r=$(readlink -f -- "$m")`, `rh=$(readlink -f -- "$home")`, then
    `case $r in "$rh"/*)`.
  - Detect a symlinked intermediate directory by comparing
    `readlink -f "$(dirname "$m")"` with the literal dirname. That
    covers `~/.ssh -> /root/.ssh`.
- **Homes:** `getent passwd 2>/dev/null || cat /etc/passwd`, taking
  field 6. Dedupe, and skip any home that is `/`, isn't a directory, or
  contains whitespace or `*?[`.
- **Manifest lines** on stdin: T if `test -e`, else M.
- **Newline in a matched name:** detect it with a `nl` variable built
  without a literal newline, and emit X.

**Fake getent** (`internal/testutil/fakes.go`):
- `func (f *FakeSSH) SetPasswd(t *testing.T, passwd string)` writes
  `$CtlDir/bin/getent`, a script that prints `passwd` for `getent passwd`.
- The fake ssh script prepends `$FAKE_SSH_CTL/bin` to `PATH` before its
  `exec sh -c`.
- `SetPasswdFails(t)` makes the fake getent exit 2, to exercise the
  fallback.

- [ ] **Step 1: Write failing script tests.**
  - Run `ListArgs` through `testutil.NewFakeSSH` plus
    `proc.RunContextStdin`, against temp-dir homes, then `parseList`.
  - Run each test under `/bin/sh` and, where present, under `bash` and
    `dash` too. Use a helper that swaps the fake ssh's `exec sh` for the
    shell under test, through an env var the fake honours.
  - Tests:
    - `TestListRemoteListChosen`: candidates `[missing, empty, good]`,
      where `good` holds `/etc/x\n\n# c\n; c\r\n/gone\n`. Expect
      `Remote == good`, `Listed == {/etc/x}` (with `/etc/x` created
      under a temp root; use absolute temp paths throughout) and
      `Missing == {/gone}`.
    - `TestListNoRemoteList`: all candidates missing. Expect
      `NoList == true`, `Remote == ""` and no error.
    - `TestListHomeGlobs`: passwd with homes `A` (in `/home`-like dir),
      `S` (service account elsewhere), a duplicate of `A`,
      `/nonexistent`, `/` and `/bin`. Pattern `~/.ssh/*`. Expect `Globbed`
      to hold files from `A` and `S` exactly once each, including
      `A/.ssh/my key` (with a space).
    - `TestListHomeIsSymlink`: home `H -> realH`, holding
      `.bashrc -> dotfiles/bashrc` inside it. Expect it in `Globbed`.
    - `TestListSymlinkOutsideHomeSkipped`: `A/.ssh/config -> /etc/hostname`
      and `B/.ssh -> A/.ssh`. Expect both in `Symlinks`, not `Globbed`.
    - `TestListAbsolutePatternSymlinkSkipped`: `/tmpdir/cron/x -> y`
      with pattern `/tmpdir/cron/*`. Expect it in `Symlinks`.
    - `TestListDirsAndNoMatchIgnored`: a pattern matching only a
      directory, and a pattern matching nothing. Expect nothing.
    - `TestListManifestStdin`: 10 000 manifest lines, half existing.
      Expect the matching `Tracked` and `Missing` counts.
    - `TestListNewlineName`: a file named `a\nb` under `~/.ssh`. Expect
      it in `Skipped` and absent from `Globbed`.
    - `TestListPasswdFallback`: `SetPasswdFails`, with pattern
      `~/.extract-test-none`. Expect no error and the sentinel present.
    - `TestListCshLoginShell`: `FakeSSH.SetHost("h1", 1, "", "for: Command not found.\n", 0)`.
      `listOutcome` gives `StatusFailed` and `files list step exited 1`.
      With exit 0 and no `E`, it gives `files list output incomplete (no end marker)`.
  - `TestListArgsShape` checks the argv prefix
    `ssh -o BatchMode=yes u@h1 sh -c`, and checks the script has no
    newline and no `!`.
- [ ] **Step 2: Run them.** `go test ./internal/extract/ -run 'TestList'`.
  Expect FAIL.
- [ ] **Step 3: Implement** the script builder, `parseList`,
  `listOutcome` and the fake getent support.
- [ ] **Step 4: Check.** `go test ./internal/extract/ -run TestList -count=3`.
  Expect PASS.
- [ ] **Step 5: Commit.** `feat(extract): build per-host file lists from globs over ssh`.

---

### Task 4: manifest, runner integration and rsync args

**Files:**
- Create: `internal/extract/manifest.go`, `internal/extract/manifest_test.go`
- Modify: `internal/extract/runner.go`, `internal/extract/rsync.go`,
  `internal/extract/hosts.go`
- Test: `internal/extract/runner_test.go`, `internal/extract/rsync_test.go`,
  `internal/extract/hosts_test.go`

**Interfaces:**
- Consumes: Task 1 `proc.RunContextStdin`, Task 2 `Config.UsesGlobs`,
  and Task 3 `ListArgs`, `listOutcome` and `ListResult`.
- Produces:
  - `func ManifestPath(root, host string) string`.
  - `func ReadManifest(path string) ([]string, error)`: a missing file
    gives `nil, nil`.
  - `func WriteManifest(path string, paths []string) error`: creates the
    dir with mode `0700`, writes a temp file with mode `0600`, then
    renames it.
  - `func GlobsRsyncArgs(h Host, listFile string, noop bool, verbosity int) []string`.
    It is the same as `RsyncArgs` but with
    `--delete-missing-args --files-from=<listFile>`, a local path.
    Factor out the shared base flags; `RsyncArgs` output is unchanged.
  - `HostResult.Globs *GlobsResult`, which is nil when globs are not
    used:
    ```go
    type GlobsResult struct {
        Failed  bool     // the list step failed; nothing else ran
        NoList  bool     // remote lists configured, none non-empty
        Listed, Globbed, Tracked, Missing int // counts for -vvv and the plan
        Skipped []string // L and X paths
        Deleted []string // local copies removed for L paths (or, under noop, that would be)
    }
    ```
  - `HostResult.ProbeFailed()` returns `h.Globs.Failed` when
    `h.Globs != nil`, and otherwise behaves as now. The ssh step is
    stored in `HostResult.Probe`, so the existing trace and plan code
    sees it.
  - `ParseHost` rejects names starting with `.`, with
    `invalid host %q: name must not start with '.'`. The `.` and `..`
    case becomes redundant; keep the tests passing.
- **Runner order for a globs host** (in `runHost`, a separate branch
  from the 0.9.0 probe):
  1. Read the manifest. A read error fails the host
     (`cannot read manifest: …`), skipping hooks.
  2. Run the list step through `RunContextStdin`, feeding it the
     manifest. Use `opts.Timeout` and `cfg.Root`. It runs under noop
     too. On failure, set `Globs.Failed`, skip hooks, fail or time out
     the host, and return.
  3. Set `hr.FilesFrom = lr.Remote`. Hook `$4` is that value, possibly
     `""`. Add a `-v` warning per `Symlinks`/`Skipped` path:
     `skipped symlink <p>` or `skipped name containing a newline <p>`.
  4. Do the existing mkdir (not under noop), then the pre-host hooks
     (existing logic).
  5. For each L path, take its local copy at
     `filepath.Join(root, host, p)`. Verify the joined path stays under
     `root/host` after `filepath.Clean`; skip it otherwise. If the copy
     exists, remove it (not under noop), record it in `Deleted`, and
     append ` (deleted local copy)` to its warning.
  6. Build the deduplicated rsync list (Listed ∪ Globbed ∪ Tracked ∪
     Missing, minus Symlinks, in first-seen order). Write it to `os.CreateTemp("", "extract-files-*")` and
     remove the file when the host finishes. Run rsync with
     `GlobsRsyncArgs`. Exit code handling is unchanged.
  7. If rsync exited 0 or 24 and not under noop, write the manifest with
     Listed ∪ Globbed ∪ Tracked, deduplicated, minus Symlinks. A write failure fails the host
     (`cannot write manifest: …`), but post-host hooks still run, as for
     an rsync error.
  8. Run the post-host hooks (existing logic).

- [ ] **Step 1: Write failing tests.**
  - `manifest_test.go`: a round trip; missing gives nil; the file mode
    is `0600` and the dir `0700`; a rename leaves no temp file behind.
  - `rsync_test.go`: `TestGlobsRsyncArgs`. Expect
    `--delete-missing-args`, `--files-from=/tmp/l` (no colon), `u@h1:/`
    and `h1`, and the existing quiet/verbose/dry-run variants.
  - `hosts_test.go`: `.extract-state` and `.x` are rejected.
  - `runner_test.go`, using a globs config built by hand with
    `Mode: ModeFilesRemote`, `Globs`, and fake-getent homes under
    `t.TempDir()`:
    - `TestRunGlobsHealthy`: rsync is called once with
      `--delete-missing-args`. Its list file is read inside the fake
      rsync; extend `FakeRsync` to copy the `--files-from` file it was
      given to `$CtlDir/<host>.list`. Expect that list to be the
      expected union. The manifest is written. Hook `$4` is the chosen
      remote list.
    - `TestRunGlobsDedup`: the same path from the remote list, a glob
      and the manifest appears once in the list and once in the
      manifest.
    - `TestRunGlobsMissingFromManifestListed`: a manifest path deleted
      on the "remote" appears in the rsync list, and the new manifest
      lacks it.
    - `TestRunGlobsNoListIsOK`: remote candidates are configured but
      absent. Expect `Status == StatusOK`, `Globs.NoList`, and hook
      `$4 == ""`.
    - `TestRunGlobsSymlinkDeletesLocalCopy`: the manifest has
      `A/.ssh/config`, now a link outside home, and a local copy exists
      under the host dir. Expect the copy removed, `Deleted` set, the
      warning saying `(deleted local copy)`, and the path not in the
      list or the new manifest. A hook-created
      `root/h1/h1-20261102.tar.gz` survives.
    - `TestRunGlobsNoopWritesNothing`: under noop, the list step runs,
      nothing is deleted, and no manifest is written.
    - `TestRunGlobsRsyncFailureKeepsManifest`: rsync exits 23. Expect
      the manifest unchanged and the post-host hooks run.
    - `TestRunGlobsListStepFails`: ssh exits 255. Expect
      `Globs.Failed`, no rsync, hooks skipped, and other hosts still
      run.
    - `TestRunGlobsWipedTree`: a manifest exists but the host dir
      doesn't. Expect no error, and all manifest paths listed.
- [ ] **Step 2: Run them.** `go test ./internal/extract/`. Expect FAIL.
- [ ] **Step 3: Implement** as specified.
- [ ] **Step 4: Check.** `go test ./internal/extract/ -count=3` and
  `CGO_ENABLED=1 go test -race ./internal/extract/`. Expect PASS; the
  0.9.0 tests are unchanged.
- [ ] **Step 5: Commit.** `feat(extract): track deletions for glob hosts via a manifest`.

---

### Task 5: reporting (`internal/report`)

**Files:**
- Modify: `internal/report/report.go`
- Test: `internal/report/report_test.go`

**Interfaces:**
- Consumes: Task 4 `HostResult.Globs` and `GlobsResult`.
- Produces the output in spec "Output":
  - **`-v`:** the new warnings go through the existing `Warnings` path.
    No new code beyond checking that they print.
  - **`-vv`:** for a host with `Globs.NoList`, print
    `<host>: no files list found; using globs and tracked files only`
    at Debug.
  - **`-vvv`:** after the traced ssh step, print
    `<host>: files list: <Listed> listed, <Globbed> globbed, <Tracked> tracked, <Missing> missing, <len(Skipped)> skipped`
    at `LevelTrace`.
  - **`-n` plan:** for a globs host, print
    `built files list: <shellJoin(argv)> -> <counts line as above>`
    instead of `checked files lists:`. A failed step gives
    `built files list: <cmd> (failed)` and nothing else for that host.
    Each `Deleted` path gives `would delete local copy: <path>`.
  - **Failure:** a failed globs host prints its error, plus the step's
    stdout and stderr, exactly as `ProbeFailed` does now.

- [ ] **Step 1: Write failing tests.** `TestReportGlobsNoListAtVV`,
  `TestReportGlobsTraceSummary`, `TestPlanGlobsHost`,
  `TestPlanGlobsFailed` and `TestReportGlobsFailureShowsStderr`, built
  from hand-made `Result` values. Each asserts the exact lines above at
  the right verbosity, and their absence one level lower.
- [ ] **Step 2: Run them.** `go test ./internal/report/`. Expect FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Check.** `go test ./internal/report/`. Expect PASS.
- [ ] **Step 5: Commit.** `feat(report): show glob file lists in traces and the plan`.

---

### Task 6: CLI wiring, permission warning, shipped files, docs, e2e, version

**Files:**
- Modify:
  - `cmd/extract/sync.go`
  - `cmd/extract/e2e_test.go`
  - `bin/extract_git_report`
  - `README.md`
  - `conf/extract.conf.dist`
  - `docs/superpowers/specs/2026-11-02-go-port-design.md`
  - `CLAUDE.md`
  - `internal/version/VERSION`
  - `internal/config/globs_test.go` (remove the `t.Skip`)
- Create: `conf/extract.globs.dist`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - **Permission warning.** In `sync.go`, after `loadConfig`, stat
    `cfg.Root`. If `mode.Perm()&0o077 != 0`, log:
    `a.log.Warn(fmt.Sprintf("warning: EXTRACT_ROOT %s is accessible by group or others (mode %04o); it holds copies of remote secrets, so chmod 700 it (see README \"Sensitive data\")", cfg.Root, mode.Perm()))`.
    The run continues and the exit code is unaffected. This applies to
    every config.
  - **`conf/extract.globs.dist`:** exactly the list and comments in the
    spec's "Shipped globs list" section, including the closing
    "left out on purpose" comment.
  - **`conf/extract.conf.dist`:** add these, commented, under the
    files section:
    ```
    # Extra files selected by glob on each host (absolute or ~/ patterns;
    # ~/ means every home directory in the host's passwd)
    #EXTRACT_FILES_GLOBS_FILE=extract.globs
    #EXTRACT_FILES_GLOBS="~/.ssh/* /var/spool/cron/*"
    ```
  - **`bin/extract_git_report`:** above each `git log --stat`, add
    `# Never add -p here: the repo holds copies of remote secrets, and this output is mailed.`
  - **README**, per spec "Other changes":
    - the config table rows for both keys;
    - the sources rule;
    - the failure table row (the list step fails: host failed, hooks
      and rsync skipped);
    - the output levels;
    - the rsync >= 3.1.0 requirement;
    - install steps for `extract.globs` with no-clobber care;
    - the globs section (patterns, `~/` homes, the symlink rule, the
      shipped list and its two tests);
    - the "Sensitive data" section, with every bullet the spec lists.
  - **`2026-11-02-go-port-design.md` and `CLAUDE.md`:** update as the
    spec says.
  - **`VERSION`:** `0.10.0`.

- [ ] **Step 1: Write failing tests** in `e2e_test.go`.
  - `TestE2EPermWarning`: chmod the root to `0755`. Expect exit 0, and
    stderr containing `is accessible by group or others (mode 0755)`.
    At `0700` stderr is empty.
  - `TestE2EGlobsRealRsync`:
    - Skip unless a real `rsync` >= 3.1 is on `PATH`.
    - Uses `testutil.NewFakeSSH` and no fake rsync, so the fake ssh
      runs `rsync --server` locally.
    - First run: extract a file selected by a `~/` glob from a
      fake-getent home into `root/localhost/...`.
    - Then delete the file in the "remote" home and run again. Expect
      the local copy gone, and a hook-created `root/localhost/keep.tar.gz`
      untouched.
    - Also run with `-n`, and assert the plan contains
      `built files list:` and `--delete-missing-args`.
  - Remove the `t.Skip` from `TestLoadShippedGlobsDist`.
- [ ] **Step 2: Run them.** `go test ./cmd/extract/ ./internal/config/`.
  Expect FAIL.
- [ ] **Step 3: Implement** the code, files and docs as specified.
- [ ] **Step 4: Full verification.**
  - `go build ./... && go vet ./... && gofmt -l . && go test ./... && CGO_ENABLED=1 go test -race ./...`
    must pass with gofmt silent.
  - `go build ./cmd/extract && ./extract --version` must print
    `extract 0.10.0`; delete the binary afterwards.
- [ ] **Step 5: Commit.** `feat: wire up file globs, permission warning and shipped globs list`.
  Add a body summarising the feature and noting the rsync >= 3.1
  requirement.
