# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`extract` rsyncs a set of files off one or more remote hosts into per-host
trees under a local `EXTRACT_ROOT`, running optional hook scripts before and
after. Typical use is a nightly cron job whose `post_extract_git` hook commits
remote config changes to git. User docs are
`README.md`; the design authority is
`docs/superpowers/specs/2026-11-02-go-port-design.md`.

## Commands

```shell
scripts/build.sh                                 # builds bin/extract (also: rake build)
scripts/push [user@]host                         # installs bin/extract as host:/usr/bin/extract (also: rake push[host]); no default host
go test ./...
go test ./internal/extract/ -run TestRunNoop     # single test
go vet ./... && gofmt -l .                       # gofmt -l should print nothing
CGO_ENABLED=1 go test -race ./...                # -race needs cgo, which is off by default here
bin/extract -c conf/extract.conf -n -vvv [host ...]   # dry run, shows the plan
scripts/release.sh X.Y.Z                         # needs a top CHANGELOG.md entry for X.Y.Z; commits + tags, never pushes
```

The version lives only in `internal/version/VERSION` (embedded); every
release needs a `## [X.Y.Z] - YYYY-MM-DD` entry at the top of
`CHANGELOG.md`, which `scripts/release.sh` enforces.

## Architecture

- `cmd/extract`: kong CLI with subcommands. `cli.go` holds the grammar:
  global flags (`-c`, `-v`, `--version`) on `CLI`, and `sync` (`SyncCmd`)
  tagged `default:"withargs"` so plain `extract [flags] [host ...]` still
  runs it (cron jobs and scripts call it bare; keep that working). `run` in
  `main.go` parses, sets up the logger and the signal context, and
  dispatches on `kctx.Command()` to a method on `app` (`app.go`: shared
  state plus `loadConfig`); `sync.go` wires config, hosts, runner and
  reporter and maps results to exit codes. After loading the config it
  warns (`a.log.Warn`, so stderr at every verbosity, and under `-n`)
  when `EXTRACT_ROOT` has any group or other permission bit: the tree
  holds copies of remote secrets and `.git/objects` gets umask modes, so
  the root's own mode is the only fence. It never changes the exit code.
  E2e tests must therefore use a `0700` root (`rootDir` in
  `e2e_test.go`); `t.TempDir()` subdirectories follow the umask. A new
  subcommand is a struct field with `cmd:""` on `CLI`, an `app` method,
  and a case in the switch.
  Every kong field has an explicit `name:` tag; `-v` is an `int` counter.
- `internal/config`: loads the shell-style config with its own literal
  parser (`parse.go`: quotes stripped, no `$` expansion, like
  Config::Tiny) and validates it, including resolving and checking hook
  paths at load time. `globs.go` loads `EXTRACT_FILES_GLOBS` and
  `EXTRACT_FILES_GLOBS_FILE`; `TestLoadShippedGlobsDist` parses
  `conf/extract.globs.dist`. `keys.go` holds `knownKeys` (the single
  list of keys extract reads) and `hookKeys`; a key in `EXTRACT_*`,
  `PRE_EXTRACT_*` or `POST_EXTRACT_*` outside both is a config error
  with a did-you-mean hint. `TestKnownKeysMatchSource` fails if a
  key literal is read without being listed.
- `internal/extract`: `ResolveHosts`, `RsyncArgs` and `Run`. With globs,
  `listscript.go` builds the one-line POSIX sh script sent as
  `ssh ... sh -c '<script>'` (no newline, no `!`; must run under dash and
  bash). It replaces the 0.9.0 probe for that host, reads the manifest
  on stdin and ends its output with an `E` sentinel. No path holding a
  newline or CR may reach the rsync list or the manifest: rsync's
  `--files-from` ends a line at either, so the name would split into two
  paths that `--delete-missing-args` deletes locally (even the host
  directory's `.git`). The script emits such names as `X` (skipped) and
  `dropUnsafeNames` drops any that get through. `manifest.go` keeps
  `$EXTRACT_ROOT/.extract-state/<host>.files` (dir `0700`, file `0600`,
  written atomically): the paths extract has extracted, so later runs
  list them and `--delete-missing-args` deletes copies of files gone
  from the host. Missing `EXTRACT_FILES_REMOTE` entries are listed and
  deleted too (where 0.9.0 fails the host with rsync exit 23). Deletion
  only reaches paths named in this run's remote list or the manifest,
  never anything else in the host directory, which keeps hook-created
  files (tarballs) safe. Manifest paths are only checked for existence,
  not the symlink rule, once no glob matches them. The manifest is
  rewritten only after rsync exits 0 or 24, never under `-n`. Host names
  can't start with `.`, so no host collides with `.extract-state`. With
  several `EXTRACT_FILES_REMOTE` paths, `runHost` first runs an ssh probe
  (`probe.go`: `ProbeArgs`, a one-line `test -s` loop) to choose the
  host's list; a failed probe skips pre/post-host hooks and rsync.
- `internal/proc`: runs one command with a timeout in its own process group
  (kill `-pgid`: SIGTERM, then SIGKILL after a grace period). `RunContext`
  kills the same way when its context is cancelled; `run` cancels on
  SIGINT/SIGTERM/SIGHUP, because children in their own groups would
  otherwise survive extract being killed.
- `internal/report`: renders a result as plain lines through `slog`.
- `internal/testutil`: fake rsync and ssh on `PATH` for tests. The fake
  ssh runs the remote command with `sh -c` against local files unless
  `SetHost` overrides it.

### The Result seam

`extract.Run` never prints. It returns a `*extract.Result` (setup hooks, one
`HostResult` per host, teardown hooks, `Aborted`). `report.Report` turns that
into output and `main` turns it into the exit code. Any future notifier should
consume `Result` rather than add output to the runner.

### Hook contract (`scripts/`)

Four hook keys, each a comma-separated list: `PRE_EXTRACT_SETUP` and
`POST_EXTRACT_TEARDOWN` (once per run), `PRE_EXTRACT_HOST` and
`POST_EXTRACT_HOST` (per host). Args are `$1=EXTRACT_ROOT $2=config path`
(absolute), plus `$3=host $4=files list path or ""` for per-host hooks
(the per-host chosen path when `EXTRACT_FILES_REMOTE` lists several; with
globs, that chosen path or "", never the temp list file rsync is given).
Changing the arg order breaks user scripts. Hooks are exec'd directly with
`EXTRACT_ROOT` as cwd. Non-zero exit is a failure and stops the rest of that
list; each hook has `EXTRACT_HOOK_TIMEOUT`. Failure rules that are easy to
break: a setup hook failure aborts the run; an rsync timeout skips that host's
post-host hooks (a half-finished `--delete` must not be committed), but an
rsync error (other than 24, which is only a warning) still runs them. Shipped
hooks must therefore exit 0 on their normal success paths.

### Config file is dual-format

`conf/extract.conf.dist` is parsed by `internal/config` in Go **and** `source`d by the
bash hooks and `bin/extract_git_report`. New settings must stay valid as both
`KEY=value` and shell assignments: no spaces around `=`, quote multi-word
values. `TestLoadShippedDist` parses the dist file.

### Output and exit codes

Output levels: default prints only failures and the `EXTRACT_ROOT` permission
warning (stderr); `-v` adds a summary line and host warnings, `-vv` a line per
OK host, `-vvv` rsync commands and step output (and rsync `--verbose`). `-n`
always prints the plan and runs no hooks, but does run `EXTRACT_HOST_CMD` and
the files-list probe or globs list step. Exit codes: 0 all OK; 1 a host or
hook failed; 2 usage or config error (kong's 80 is mapped to 2). Errors go to
stderr, info to stdout.

## Conventions

Log only through `slog`; never import stdlib `log` or call `slog.SetDefault`.
Linux only. Tests use stdlib `testing`.
