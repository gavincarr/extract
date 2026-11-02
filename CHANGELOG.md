# Changelog

All notable changes to extract are recorded here. Each release has an
entry headed `## [X.Y.Z] - YYYY-MM-DD`, newest first; `scripts/release.sh`
refuses to tag a version whose entry is not at the top.

## [0.10.0] - 2026-11-02

First release of the Go rewrite, replacing the Perl 0.7.2 release. The
command line, config format, hook arguments and rsync invocation are
kept; see "Upgrading from 0.7.x (Perl)" in the README for everything
that behaves differently. (0.8.0 and 0.9.0 were development versions
and were never tagged.)

### Breaking changes

- Failures are failures: hook exit codes are checked, a failing hook
  stops the rest of its list, and the exit status is 0 (all OK),
  1 (a host or hook failed, or the run was interrupted) or 2 (usage or
  config error).
- A missing or non-executable hook, an unreadable `-c` file, and both
  `EXTRACT_FILES_REMOTE` and `EXTRACT_FILES_LOCAL` being set are fatal
  at startup (exit 2) instead of being skipped or worked around.
- An unknown key in extract's namespaces (`EXTRACT_*`, `PRE_EXTRACT_*`,
  `POST_EXTRACT_*`) is a config error with a did-you-mean hint, except
  the shipped hooks' `EXTRACT_GIT_REPO_MODE`. Such keys were silently
  ignored before.
- `EXTRACT_HOSTS` and `EXTRACT_HOST_CMD` are merged (duplicates removed)
  when no hosts are given on the command line; previously
  `EXTRACT_HOSTS` won and the command was never run. A host command that
  fails or prints no hosts is fatal.
- Hooks are executed directly rather than through `sh`, so each needs a
  `#!` line.
- `-v` prints a summary rather than progress chatter; see the README's
  output levels.
- `--rsh=ssh -o BatchMode=yes` is always passed, overriding `RSYNC_RSH`.
- Timeouts (`-t`) really kill a slow rsync and its process group.
- Host names may not start with `-` or `.`.

### Added

- `EXTRACT_FILES_GLOBS` and `EXTRACT_FILES_GLOBS_FILE`: extra files
  selected per host by absolute or `~/` glob patterns (`~/` expands over
  every home directory in the host's passwd), with a curated starting
  list shipped as `conf/extract.globs.dist`. Symlinked matches that
  resolve outside their home are skipped.
- Deletion tracking for glob hosts: a per-host manifest under
  `$EXTRACT_ROOT/.extract-state/` plus rsync `--delete-missing-args`, so
  files removed from a host are deleted locally and committed as
  deletions. Needs rsync 3.1.0 or later on both ends.
- `EXTRACT_FILES_REMOTE` accepts several candidate paths; each host uses
  the first that exists and is non-empty there (e.g. for mixed RPM and
  DEB fleets).
- A startup warning when `EXTRACT_ROOT` is accessible by group or
  others, since the tree holds copies of remote secrets.
- `EXTRACT_SCRIPTS_DIR` (where bare hook names are looked up) and
  `EXTRACT_HOOK_TIMEOUT` (per-hook and host-command timeout).
- `-n` prints the full plan (hooks, rsync commands) at every verbosity.
- Ctrl-C, SIGTERM and SIGHUP stop the running rsync or hook (its whole
  process group) and report the run as interrupted.
- `--version`, a `sync` subcommand (still the default, so plain
  `extract [flags] [host ...]` works as before), `scripts/build.sh`, a
  `Rakefile` and `scripts/release.sh`.

### Fixed

- Shipped hooks exit 0 on their normal success paths (e.g.
  `post_extract_git` with nothing to commit, `pre_extract_load_ssh_keys`
  with keys already loaded); redeploy them, as the README describes.
- Config values are parsed literally (quotes stripped, no `$`
  expansion), as with the Perl release.
