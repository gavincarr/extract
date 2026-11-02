Extract
=======

extract is a utility for copying a set of files off one or more remote
hosts into a set of local host trees. It's implemented using rsync.

extract requires the list of files to extract to be available either
locally or on the remote machine. I typically use
[rpm-find-changes](http://ofn.me/rfc) to generate a list of files to
extract on remote RedHat/CentOS machines.


Build and install
-----------------

extract is written in Go and needs a Go toolchain and rsync.

```shell
git clone https://github.com/gavincarr/extract/
cd extract
go build ./cmd/extract
```

Installed layout (the config line is for a first install only and never
overwrites an existing config; `install -b` keeps any existing hook it
replaces as `<name>~`):

```shell
install -m 755 extract /usr/bin/extract
install -d /etc/extract/scripts
[ -e /etc/extract/extract.conf ] || install -m 644 conf/extract.conf.dist /etc/extract/extract.conf
install -b -m 755 scripts/pre_extract_* scripts/post_extract_* /etc/extract/scripts/
$EDITOR /etc/extract/extract.conf
```

To try it from a checkout instead, copy `conf/extract.conf.dist` to
`conf/extract.conf`, edit it, and run a dry run:

```shell
./extract -c conf/extract.conf -n
```

Relative config paths are made absolute, so hooks always receive an
absolute config path.


Usage
-----

    extract [global flags] [sync] [-n] [-t <timeout_secs>] [<host> ...]

`sync` copies files from the remote hosts. It is the default command, so
plain `extract` (with or without flags and hosts) runs it, as earlier
versions did: `extract -n h1` and `extract sync -n h1` are the same.
Global flags work before or after the command.

Global flags:

| Flag | Meaning | Default |
|---|---|---|
| `-c`, `--config` | config file | `/etc/extract/extract.conf` |
| `-v`, `--verbose` | verbosity, repeatable | 0 |
| `--version` | print version and exit | |
| `-h`, `--help` | usage (`extract sync --help` for sync's flags) | |

`sync` flags:

| Flag | Meaning | Default |
|---|---|---|
| `-t`, `--timeout` | per-host rsync timeout, in seconds (0 = none) | 60 |
| `-n`, `--noop` | dry run | off |

A host argument that has the same name as a command is read as that
command; name the command explicitly to sync such a host
(`extract sync sync`).

Hosts given on the command line override the hosts in the config file.
A host may be `user@host`; the user is passed to rsync and the local
directory is named after the host part only. A host is rejected if its
host part is empty, is `.` or `..`, starts with `-`, or contains `/`,
`@`, `:`, whitespace or control characters; or if its user part is
empty, starts with `-`, or contains `/`, `:`, whitespace or control
characters.

rsync is run from `EXTRACT_ROOT` with `--archive --copy-links --delete`
and `--rsh=ssh -o BatchMode=yes`, so a host-key or password prompt fails
immediately instead of hanging a cron job. Each host is extracted into
`<EXTRACT_ROOT>/<host>/`.

With `-n`, extract prints what it would do (the hooks it would run and
the rsync commands) whatever the `-v` level. rsync runs with `--dry-run`
and hooks are not run. `EXTRACT_HOST_CMD` is still run, since it is
needed to get the host list.


Configuration
-------------

extract is configured with a shell-style file of `KEY=value` lines,
optionally quoted, with `#` comments. The hook scripts `source` the same
file, so it must stay valid shell: no spaces around `=`, and quote
values containing spaces. extract takes values literally, as the 0.x
(Perl) releases did: quotes are stripped but `$` is not expanded. Keys extract
doesn't know are ignored; the hooks use their own, such as
`EXTRACT_GIT_REPO_MODE` (`ALL` for one git repo at `EXTRACT_ROOT`,
`HOST` for one repo per host directory). See
`conf/extract.conf.dist` for an annotated example.

| Key | Meaning |
|---|---|
| `EXTRACT_ROOT` | Required. Directory in which per-host trees are stored. Must exist and be writable. |
| `EXTRACT_HOSTS` | Hosts to extract from (quoted, whitespace separated). |
| `EXTRACT_HOST_CMD` | Alternative to `EXTRACT_HOSTS`: a command (run via `sh -c`) whose output is a whitespace-separated host list. A non-zero exit or empty output is fatal. |
| `EXTRACT_TREE` | A tree on the remote host to extract in its entirety, e.g. `/etc`. Files land in `<root>/<host>/etc/...`. |
| `EXTRACT_FILES_REMOTE` | Absolute path, on the remote host, of a file listing the files to extract. |
| `EXTRACT_FILES_LOCAL` | Path, on the local host, of a file listing the files to extract (a relative path is taken relative to `EXTRACT_ROOT`). Must be a readable regular file. |
| `PRE_EXTRACT_SETUP` | Hook(s) run once, before any extracts. |
| `PRE_EXTRACT_HOST` | Hook(s) run before each host's extract. |
| `POST_EXTRACT_HOST` | Hook(s) run after each host's extract. |
| `POST_EXTRACT_TEARDOWN` | Hook(s) run once, after all extracts. |
| `EXTRACT_SCRIPTS_DIR` | Where bare hook names are looked up (a relative path is taken relative to the current directory). Default `/etc/extract/scripts`. |
| `EXTRACT_HOOK_TIMEOUT` | Timeout in seconds for each hook and for `EXTRACT_HOST_CMD`. Default 600; 0 means none. |

Hosts come from the command line if any are given, otherwise from
`EXTRACT_HOSTS`, otherwise from `EXTRACT_HOST_CMD`.

Exactly one of `EXTRACT_TREE`, `EXTRACT_FILES_REMOTE` and
`EXTRACT_FILES_LOCAL` must be set. The files-list variants are passed to
rsync as `--files-from`.


Hook scripts
------------

Hooks are arbitrary executables, in any language. extract ships example
hooks in `scripts/` to load ssh keys before extraction and to commit the
extracted files to git.

The four hook keys take a comma-separated list; whitespace around commas
is ignored. A bare name is looked up in `EXTRACT_SCRIPTS_DIR`; an
absolute path is used as it is. Every hook must exist and be executable,
or extract exits with status 2 at startup, before any rsync.

Arguments:

- `PRE_EXTRACT_SETUP`, `POST_EXTRACT_TEARDOWN`:
  `$1` = `EXTRACT_ROOT`, `$2` = absolute path of the config file.
- `PRE_EXTRACT_HOST`, `POST_EXTRACT_HOST`: the same, plus `$3` = host
  and `$4` = the `EXTRACT_FILES_REMOTE` or `EXTRACT_FILES_LOCAL` value,
  or an empty string when `EXTRACT_TREE` is used.

Hooks are executed directly (not through a shell), inherit extract's
environment, run with `EXTRACT_ROOT` as the working directory, and have
stdin set to `/dev/null`.

Order: setup hooks run once; then for each host, pre-host hooks, rsync,
post-host hooks; then teardown hooks run once.

Exit status matters. A non-zero exit means the hook failed, and the
first failing hook in a list stops the rest of that list. For example,
with `POST_EXTRACT_HOST=post_extract_git,post_extract_touch`, the touch
doesn't run if git failed. Each hook, and `EXTRACT_HOST_CMD`, is killed
(with its process group) if it exceeds `EXTRACT_HOOK_TIMEOUT`, and
counts as failed.

What a failure does:

| Event | Effect |
|---|---|
| Setup hook fails | Run aborts: no hosts, no teardown. Exit 1. |
| Pre-host hook fails | Host marked failed; its rsync and post-host hooks are skipped. |
| rsync exits 24 (files vanished) | Host is OK, with a warning shown at `-v` and above. |
| rsync exits non-zero otherwise (including 23) | Host marked failed. Post-host hooks still run. |
| rsync times out (`-t`) | Host marked timed out. Post-host hooks are skipped, because with `--delete` a half-finished rsync could otherwise be committed by `post_extract_git`. |
| Post-host hook fails | Host marked failed. |
| Teardown hook fails | Run reported as failed. Exit 1. |

One host failing never stops the remaining hosts. A rsync timeout of 0
(`-t 0`) means no timeout.


Output
------

Output is plain lines without timestamps, suitable for cron mail.
Errors go to stderr. Warnings (such as rsync exit 24), the summary,
per-host lines, `-vvv` detail and the `-n` plan go to stdout, so
warnings appear only at `-v` and above.

| Level | All OK | Some failures |
|---|---|---|
| default | nothing | each failed or timed-out host or failed hook, with its error and rsync stderr or hook output |
| `-v` | one summary line, e.g. `42 hosts ok` | summary line (`40 ok, 1 failed, 1 timed out`) plus each failure itemised |
| `-vv` | adds one line per successful host, with duration | same |
| `-vvv` | adds rsync commands, rsync output, and hook output for successful steps | same |

At `-vvv` rsync runs with `--verbose` instead of `--quiet`.

Exit codes:

- `0`: every host and every hook succeeded.
- `1`: the run happened, but a host failed or timed out, or a hook
  failed, or the run was interrupted. On Ctrl-C (SIGINT), SIGTERM or
  SIGHUP extract kills the running rsync or hook (its whole process
  group), starts nothing further (no more hosts, no teardown hooks),
  reports `run interrupted: N of M hosts attempted` and exits 1. An
  interrupt while `EXTRACT_HOST_CMD` is still running kills that command
  and exits 1 with `extract: host command interrupted: context canceled`.
- `2`: usage or configuration error; nothing was extracted. This covers
  bad flags, an unreadable config, a missing or unwritable
  `EXTRACT_ROOT`, bad source keys (none set, or more than one), an
  invalid host name, a missing or non-executable hook, and a failed or
  empty `EXTRACT_HOST_CMD`.


Upgrading from 0.x (Perl)
-------------------------

The Go version (1.0) keeps the Perl 0.x releases' command line, config format,
hook arguments and rsync behaviour, but it treats failures as failures:
hook exit codes were ignored before and are not now; a missing hook, an
unreadable `-c` file, or both `EXTRACT_FILES_REMOTE` and
`EXTRACT_FILES_LOCAL` being set are now fatal rather than silently
skipped or worked around; and the exit status is no longer 0 regardless.
`-v` now prints a summary rather than progress chatter.

Also worth knowing:

- Hooks are executed directly rather than through `sh`, so each one
  must start with a `#!` line.
- extract passes `--rsh=ssh -o BatchMode=yes`, which overrides any
  `RSYNC_RSH` in the environment; put ssh options (an identity file, say)
  in `~/.ssh/config` instead.
- Timeouts now really kill a slow rsync (Perl's `alarm` left it finishing
  in the background), so a host that routinely needs longer than `-t`
  will report "timed out"; raise `-t` for slow links.
- Config values are literal, as with Perl: no `$` expansion.

Three shipped hooks were fixed, and the installed copies in
`/etc/extract/scripts` are separate from the repo, so **redeploy them**
— check first whether you've changed any of them locally, since
redeploying replaces them:

```shell
diff -ru /etc/extract/scripts scripts | less
install -b -m 755 scripts/pre_extract_* scripts/post_extract_* /etc/extract/scripts/
```

`install -b` keeps each replaced hook as `<name>~`. The fixes:

- `post_extract_git` exited 1 whenever there was nothing to commit; it
  now commits only if something is staged.
- `pre_extract_load_ssh_keys` exited 1 when keys were already loaded.
- `post_extract_tarball` called an undefined `die`.

If you forget, the old hooks make every run exit 1, so the mistake shows
up straight away. Locally modified or custom hooks need the same check:
they must exit 0 on their normal success path.


Author and Licence
------------------

Copyright 2007-2012 Gavin Carr <gavin@openfusion.com.au>.

extract is licensed under the terms of the GNU General Public Licence
Version 3. See the COPYING file for details.
