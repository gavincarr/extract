# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`extract` rsyncs a set of files off one or more remote hosts into per-host
trees under a local `EXTRACT_ROOT`, running optional hook scripts before and
after. Typical use is a nightly cron job whose `post_extract_git` hook commits
remote config changes to git. It is a Go port (branch `go-port`) of an older
Perl implementation. The Perl (`bin/extract`, `lib/`, `t/`) is still in the
tree but is pending removal (Task 11); don't extend it. User docs are
`README.md`; the design authority is
`docs/superpowers/specs/2026-11-02-go-port-design.md`.

## Commands

```shell
go build ./cmd/extract
go test ./...
go test ./internal/extract/ -run TestRunNoop     # single test
go vet ./... && gofmt -l .                       # gofmt -l should print nothing
CGO_ENABLED=1 go test -race ./...                # -race needs cgo, which is off by default here
./extract -c conf/extract.conf -n -vvv [host ...]   # dry run, shows the plan
```

## Architecture

- `cmd/extract`: kong CLI (`cli.go`) and `run` in `main.go`, which wires
  config, hosts, runner and reporter together and maps results to exit codes.
  Every kong field has an explicit `name:` tag; `-v` is an `int` counter.
- `internal/config`: loads the shell-style config with its own literal
  parser (`parse.go`: quotes stripped, no `$` expansion, like Perl's
  Config::Tiny) and validates it, including resolving and checking hook
  paths at load time.
- `internal/extract`: `ResolveHosts`, `RsyncArgs` and `Run`.
- `internal/proc`: runs one command with a timeout in its own process group
  (kill `-pgid`: SIGTERM, then SIGKILL after a grace period). `RunContext`
  kills the same way when its context is cancelled; `run` cancels on
  SIGINT/SIGTERM/SIGHUP, because children in their own groups would
  otherwise survive extract being killed.
- `internal/report`: renders a result as plain lines through `slog`.
- `internal/testutil`: fake rsync on `PATH` for tests.

### The Result seam

`extract.Run` never prints. It returns a `*extract.Result` (setup hooks, one
`HostResult` per host, teardown hooks, `Aborted`). `report.Report` turns that
into output and `main` turns it into the exit code. Any future notifier should
consume `Result` rather than add output to the runner.

### Hook contract (`scripts/`)

Four hook keys, each a comma-separated list: `PRE_EXTRACT_SETUP` and
`POST_EXTRACT_TEARDOWN` (once per run), `PRE_EXTRACT_HOST` and
`POST_EXTRACT_HOST` (per host). Args are `$1=EXTRACT_ROOT $2=config path`
(absolute), plus `$3=host $4=files list path or ""` for per-host hooks.
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

Output levels: default prints only failures (stderr); `-v` adds a summary line,
`-vv` a line per OK host, `-vvv` rsync commands and step output (and rsync
`--verbose`). `-n` always prints the plan and runs no hooks, but does run
`EXTRACT_HOST_CMD`. Exit codes: 0 all OK; 1 a host or hook failed; 2 usage or
config error (kong's 80 is mapped to 2). Errors go to stderr, info to stdout.

## Conventions

Log only through `slog`; never import stdlib `log` or call `slog.SetDefault`.
Linux only. Tests use stdlib `testing`.
