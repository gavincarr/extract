Extract
=======

extract is a utility for copying a set of files off one or more remote
hosts into a set of local host trees. It's implemented using rsync.

extract requires the list of files to extract to be available either
locally or on the remote machine. I typically use
[rpm-find-changes](http://ofn.me/rfc) to generate a list of files to
extract on remote RedHat/CentOS machines. Files that belong to no
package, such as every user's `~/.ssh/authorized_keys`, can be added
with glob patterns (see "File globs").

extract copies secrets on purpose; read "Sensitive data" before pointing
it at a fleet.


Build and install
-----------------

extract is written in Go and needs a Go toolchain and rsync. File globs
(`EXTRACT_FILES_GLOBS`) need rsync 3.1.0 or later on both the extract
host and every remote host, for `--delete-missing-args`.

```shell
git clone https://github.com/gavincarr/extract/
cd extract
go build ./cmd/extract
```

Installed layout (the config and globs lines are for a first install
only and never overwrite an existing copy; `install -b` keeps any
existing hook it replaces as `<name>~`):

```shell
install -m 755 extract /usr/bin/extract
install -d /etc/extract/scripts
[ -e /etc/extract/extract.conf ] || install -m 644 conf/extract.conf.dist /etc/extract/extract.conf
[ -e /etc/extract/extract.globs ] || install -m 644 conf/extract.globs.dist /etc/extract/extract.globs
install -b -m 755 scripts/pre_extract_* scripts/post_extract_* /etc/extract/scripts/
$EDITOR /etc/extract/extract.conf
```

The globs list is only used if the config names it
(`EXTRACT_FILES_GLOBS_FILE=extract.globs`). When upgrading, compare the
shipped list with your copy and merge any new entries by hand, rather
than overwriting local edits:

```shell
diff -u /etc/extract/extract.globs conf/extract.globs.dist
```

Create `EXTRACT_ROOT` owned by root with mode `0700` (see "Sensitive
data"), for example `install -d -m 700 /data/extract`.

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
| `-t`, `--timeout` | per-host rsync timeout, in seconds (0 = none); also applies to the files-list probe and the globs list step | 60 |
| `-n`, `--noop` | dry run | off |

A host argument that has the same name as a command is read as that
command; name the command explicitly to sync such a host
(`extract sync sync`).

Hosts given on the command line override the hosts in the config file.
A host may be `user@host`; the user is passed to rsync and the local
directory is named after the host part only. A host is rejected if its
host part is empty, starts with `-` or `.`, or contains `/`,
`@`, `:`, whitespace or control characters; or if its user part is
empty, starts with `-`, or contains `/`, `:`, whitespace or control
characters.

rsync is run from `EXTRACT_ROOT` with `--archive --copy-links --delete`
and `--rsh=ssh -o BatchMode=yes`, so a host-key or password prompt fails
immediately instead of hanging a cron job. Each host is extracted into
`<EXTRACT_ROOT>/<host>/`. With file globs, rsync also gets
`--delete-missing-args` and a local `--files-from` list built for that
host (see "File globs").

With `-n`, extract prints what it would do (the hooks it would run and
the rsync commands) whatever the `-v` level. rsync runs with `--dry-run`
and hooks are not run. `EXTRACT_HOST_CMD` is still run, since it is
needed to get the host list, and so is the read-only files-list probe
(see `EXTRACT_FILES_REMOTE`), so the plan shows each host's chosen list
in a `checked files lists: ... -> <path>` line and in its rsync
`--files-from`. With file globs the read-only list step runs instead,
and the plan shows it as `built files list: <ssh command> -> N listed,
N globbed, N tracked, N missing, N skipped` (or `(failed)`), any
`would delete local copy: <path>` lines, then the `would rsync` line
with `--delete-missing-args`. Nothing is deleted and no manifest is
written under `-n`.


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
| `EXTRACT_FILES_REMOTE` | Absolute path, on the remote host, of a file listing the files to extract. May be a quoted, whitespace-separated list of paths, e.g. `"/var/cache/rpm-find-changes/etc.txt /var/cache/deb-find-changes/etc.txt"` for a mix of RPM- and DEB-based hosts: for each host, the first path that exists and is non-empty there is used. extract finds it with one `ssh -o BatchMode=yes [user@]host` call (a `test -s` loop in POSIX sh) before anything else for that host; a single path runs no probe. Paths can't contain whitespace. |
| `EXTRACT_FILES_LOCAL` | Path, on the local host, of a file listing the files to extract (a relative path is taken relative to `EXTRACT_ROOT`). Must be a readable regular file. |
| `EXTRACT_FILES_GLOBS` | Extra files to extract from each host, as a quoted, whitespace-separated list of glob patterns, each starting with `/` or `~/`, e.g. `"~/.ssh/* /var/spool/cron/*"`. See "File globs". |
| `EXTRACT_FILES_GLOBS_FILE` | Path, on the local host, of a file of glob patterns, one per line; blank lines and `#` comment lines are ignored, and surrounding whitespace is trimmed. A relative path is taken relative to the config file's directory, so `EXTRACT_FILES_GLOBS_FILE=extract.globs` finds `/etc/extract/extract.globs`. Must be a readable regular file; it is read at startup. See "File globs". |
| `PRE_EXTRACT_SETUP` | Hook(s) run once, before any extracts. |
| `PRE_EXTRACT_HOST` | Hook(s) run before each host's extract. |
| `POST_EXTRACT_HOST` | Hook(s) run after each host's extract. |
| `POST_EXTRACT_TEARDOWN` | Hook(s) run once, after all extracts. |
| `EXTRACT_SCRIPTS_DIR` | Where bare hook names are looked up (a relative path is taken relative to the current directory). Default `/etc/extract/scripts`. |
| `EXTRACT_HOOK_TIMEOUT` | Timeout in seconds for each hook and for `EXTRACT_HOST_CMD`. Default 600; 0 means none. |

Hosts come from the command line if any are given, otherwise from
`EXTRACT_HOSTS`, otherwise from `EXTRACT_HOST_CMD`.

Exactly one source must be set: `EXTRACT_TREE`, `EXTRACT_FILES_LOCAL`,
or remote files. Remote files means `EXTRACT_FILES_REMOTE`, globs (from
either globs key), or both. Globs with `EXTRACT_TREE` or
`EXTRACT_FILES_LOCAL` is a config error. Both globs keys may be set; the
patterns are the union of the two. Without globs, the files-list
variants are passed to rsync as `--files-from` (`--files-from=:<path>`
for the remote one, with the path chosen for that host).


File globs
----------

`EXTRACT_FILES_GLOBS` and `EXTRACT_FILES_GLOBS_FILE` add files to every
host's extract by glob pattern, alongside `EXTRACT_FILES_REMOTE` or on
their own. They catch security-relevant files that belong to no package,
and so never appear in a find-changes list, such as every user's
`~/.ssh/authorized_keys`. With globs, files that disappear from a host
are deleted from the local tree too, so `post_extract_git` commits the
removal.

### Patterns

Patterns use POSIX `sh` glob syntax: `*`, `?` and `[...]`, with no `**`
and no braces. Each pattern starts with either:

- `/`: an absolute pattern, expanded as it is
  (`/var/spool/cron/*`); or
- `~/`: a home pattern, expanded once for every home directory on the
  host (`~/.ssh/*`). One pattern therefore covers root, every user in
  `/home`, and service accounts with homes such as `/var/lib/git` or
  `/srv/deploy`, where a backdoor key is easiest to overlook.

A pattern only ever matches regular files at the level it names. Nothing
recurses, and directories, devices and the like are ignored, as is a
pattern that matches nothing. Patterns can't contain whitespace or
control characters. A bad pattern is a config error (exit 2), and one in
the globs file is reported with the file name and line number.

The home directories are the distinct home fields from
`getent passwd`, or from `/etc/passwd` when `getent` is missing (as on
some busybox systems). A home is skipped if it is `/` or resolves to
`/` (such as `//`, or a symlink to `/`), is not an absolute path, doesn't exist or isn't a directory (`/nonexistent`), or
contains whitespace or glob characters. The home part is used literally,
never glob-expanded. Homes shared by many system accounts, such as
`/usr/sbin` or `/bin`, are expanded once and normally match nothing.

### Symlinks

A glob match is a symlink match if the match itself, or any directory
between the home (or `/`) and the match, is a symlink. Symlink matches
are not extracted, with these rules:

- For a `~/` pattern, a symlink match is accepted when its fully
  resolved path (`readlink -f`) lies inside that home's own resolved
  path, so dotfile managers such as stow, chezmoi and yadm
  (`~/.bashrc -> ~/dotfiles/bashrc`) work. A home that is itself a
  symlink is fine. Links that resolve outside the home are skipped,
  which blocks both `~/.ssh/config -> /etc/shadow` and
  `~/.ssh -> /root/.ssh`.
- For an absolute pattern, every symlink match is skipped. The check is
  strict: any symlink anywhere in the path counts, so on a merged-`/usr`
  system, where `/bin` links to `/usr/bin`, every match of `/bin/*` is a
  skipped symlink. Write absolute patterns against the resolved path
  (`/usr/bin/*`).
- A skipped match is reported as a `-v` warning,
  `<host>: warning: skipped symlink <path>`. If extract had extracted
  that file on an earlier run, before it became a link, its local copy
  is deleted so the tree doesn't keep a frozen copy that looks current,
  and the warning ends `(deleted local copy)`.
- The rule applies to glob matches only. Paths from the
  `EXTRACT_FILES_REMOTE` list keep `--copy-links` behaviour, so a path
  that is both in that list and a symlinked glob match is still
  extracted.
- A match whose name contains a newline or carriage return can't be
  listed, and is skipped with a warning,
  `<host>: warning: skipped name containing a newline or carriage
  return <path>`, with `?` shown for those characters. rsync's
  `--files-from` ends a line at either, so such a name would otherwise
  reach rsync as two other paths, and `--delete-missing-args` could
  delete their local namesakes. The same applies to such a line in the
  `EXTRACT_FILES_REMOTE` list (after one trailing CR is stripped).

### What happens per host

With globs set, one ssh call per host builds that host's files list. It
replaces the `EXTRACT_FILES_REMOTE` probe, runs at the same point (from
`EXTRACT_ROOT`, with the `-t` timeout, and under `-n` too), and runs the
script as `sh -c '<script>'`, so the host's login shell may be csh or
anything else that can pass it on. The script:

1. picks the first non-empty `EXTRACT_FILES_REMOTE` candidate, as the
   probe does, and lists its paths. Finding none is not a failure when
   globs are set: the host is extracted from its globs and tracked files
   alone, with a note at `-vv`. So globs-only hosts (Arch, say) can share
   a config with deb and rpm hosts, and an RPM host whose list generator
   breaks keeps syncing the files already tracked.
2. expands the glob patterns, applying the symlink rule.
3. checks every tracked path (see below) for existence, noting which
   are missing.
4. ends its output with an end marker. Output without it, as from a
   login shell that couldn't run the script, fails the host rather than
   being read as an empty list.

rsync then gets the union of the remote list, the glob matches and the
tracked paths, minus skipped symlinks, through a temporary local
`--files-from` file, plus `--delete-missing-args`. Paths that no longer
exist on the host are in the list on purpose, and rsync deletes their
local copies. That covers tracked paths and also `EXTRACT_FILES_REMOTE`
list entries that don't exist on the host: without globs, such an entry
makes rsync exit 23 and fails the host; with globs, the host succeeds
and the local copy (if any) is deleted, so `post_extract_git` commits
the deletion. An empty list is fine; rsync runs and transfers nothing.

**Tracked files.** extract keeps, per host, a manifest of the paths it
has extracted: `$EXTRACT_ROOT/.extract-state/<host>.files`. It is
rewritten only after rsync exits 0 or 24, never under `-n`, and kept as
it was on any failure, so the next run retries the same deletions. It
lives outside the host directory, so `post_extract_git` never commits it.
Deletion only ever reaches paths named in this run's
`EXTRACT_FILES_REMOTE` list or in the manifest, never anything else in
the host directory. That is what keeps files that hooks create there
(such as `post_extract_tarball`'s tarballs) safe. Some consequences:

- A file that drops out of the find-changes list but still exists on
  the host stays tracked and keeps being extracted, so a revert to the
  package default shows up as a diff instead of being frozen.
- Files extracted before globs were enabled are not tracked, and are not
  cleaned up.
- A tracked path stays tracked while it exists, even when no current
  glob matches it any more (a pattern was removed from the list, or a
  home left passwd). It is only checked for existence, not against the
  symlink rule, so it keeps being extracted (see the known gap under
  "Sensitive data"). To stop tracking a path, remove its line from
  `$EXTRACT_ROOT/.extract-state/<host>.files`, or delete the file to
  stop tracking everything for that host.
- Without globs, nothing changes: a file deleted on the host stays in
  the local tree.

Remote hosts need rsync 3.1.0 or later, a POSIX `sh`, and
`readlink -f` (GNU coreutils or busybox) for glob matching.

### The shipped globs list

`conf/extract.globs.dist` is a recommended starting list, installed as
`/etc/extract/extract.globs` (see "Build and install"). It is used only
if the config names it. It covers ssh access and persistence files
(`~/.ssh/*`, `~/.k5login`, `~/.forward`, user crontabs and so on), shell
startup files, small credential files (`~/.netrc`, `~/.pgpass`,
`~/.aws/credentials` and so on), enough of `~/.gnupg` to restore a
keyring, and a few tool configs. Each group has a comment saying why.

Every entry has to pass two tests:

- **Small and hand-maintained**, and worth having for a restore.
- **Changes rarely.** Review is the point, and a file that changes every
  night drowns the changes that matter.

So it leaves out, on purpose, history files (`.bash_history`,
`.viminfo` and so on: noisy, and full of secrets typed at the prompt),
`~/.config/*` wholesale (mostly application state), and caches, data and
toolchains (`~/.cache`, `~/.local/share`, `.cargo` and so on). Keep to
the same tests when adding entries. Expect tens of KB per user.


Sensitive data
--------------

extract copies secrets on purpose: private keys, `/etc/shadow`,
passwords in config files, and anything else the files lists and globs
reach. These are exactly the files a restore needs. Filtering them out
was considered and rejected: the extract host already has root ssh
access to the whole fleet, so its copies add little risk, and any filter
would only ever catch some of the sensitive files. Security comes from
protecting the store instead:

- **git keeps everything forever.** With `post_extract_git`, every
  version of every extracted secret, rotated ones included, stays in the
  repo's history. Deleting a file from the tree doesn't remove it from
  history.
- **`EXTRACT_ROOT` should be mode `0700` and owned by root.** rsync keeps
  each file's original mode in the working tree, but git's object store
  doesn't: `.git/objects` is written with umask permissions, so only
  `EXTRACT_ROOT`'s own mode keeps other local users out. extract warns on
  stderr at startup, at every verbosity and under `-n`, when
  `EXTRACT_ROOT` grants any group or other permission:
  `warning: EXTRACT_ROOT /data/extract is accessible by group or others
  (mode 0755); ...`. The warning doesn't stop the run or change the
  exit code; `chmod 700` the directory to silence it.
- **Use full-disk encryption** on the extract host.
- **Reports must stay file-name only.** `bin/extract_git_report` uses
  `git log --stat`, and its output is mailed; never add `-p` or anything
  else that shows file content.
- **Encrypt any copy before it leaves the host.** That includes a pushed
  copy of the repo (use an encrypted remote such as `git-remote-gcrypt`)
  and backups of the extract host (use client-side encryption).
- **Known gap: a symlink swapped in after the list is built is
  followed.** The symlink rule is checked when the files list is built,
  and rsync runs a moment later with `--copy-links`. A user who replaces
  a checked regular file in their home with a symlink in that window
  (`~/.ssh/config -> /etc/shadow`, say) gets the link's target copied
  under the file's name, into the store and the review. The file lands
  only in the root-only store, which already holds copies of such files
  from the files lists, but it can mislead a review. Closing the gap
  would need a second rsync without `--copy-links` for glob matches; that
  was considered and declined as not worth the extra complexity. The
  same window applies to a directory: one swapped for a symlink after
  the check (`~/.ssh -> /root/.ssh`, say) is followed too. Separately,
  tracked paths that no current glob matches any more (a pattern removed
  from the list, or a home that left passwd) are only checked for
  existence, never against the symlink rule, so they keep being
  extracted, and any link in their path followed, indefinitely. To stop
  that, remove the path from `$EXTRACT_ROOT/.extract-state/<host>.files`,
  or delete that manifest.


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
  and `$4` = the files list given to rsync: the `EXTRACT_FILES_REMOTE`
  path chosen for that host (the value itself when it lists one path),
  the `EXTRACT_FILES_LOCAL` value, or an empty string when
  `EXTRACT_TREE` is used. With globs, `$4` is the chosen
  `EXTRACT_FILES_REMOTE` path, or an empty string when globs are used
  alone or the host had no list; it is never the temporary list file
  rsync is given.

Hooks are executed directly (not through a shell), inherit extract's
environment, run with `EXTRACT_ROOT` as the working directory, and have
stdin set to `/dev/null`.

Order: setup hooks run once; then for each host, the files-list probe
(only when `EXTRACT_FILES_REMOTE` lists several paths) or, with globs,
the list step, pre-host hooks, rsync, post-host hooks; then teardown
hooks run once.

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
| Files-list probe fails or finds no non-empty list (several `EXTRACT_FILES_REMOTE` paths) | Host marked failed (timed out, if it exceeded `-t`); its pre-host hooks, rsync and post-host hooks are skipped, and the ssh error output is shown. |
| Globs list step fails (the manifest can't be read; ssh fails, times out or is interrupted; or the output has no end marker) | Host marked failed (timed out, if it exceeded `-t`); its pre-host hooks, rsync and post-host hooks are skipped, and the ssh error output is shown. Finding no `EXTRACT_FILES_REMOTE` list is not a failure. |
| Pre-host hook fails | Host marked failed; its rsync and post-host hooks are skipped. |
| rsync exits 24 (files vanished) | Host is OK, with a warning shown at `-v` and above. |
| rsync exits non-zero otherwise (including 23) | Host marked failed. Post-host hooks still run. With globs, the manifest is not updated. |
| Globs files list can't be written (`cannot write files list`) | Host marked failed and rsync is skipped. Its pre-host hooks have already run, and its post-host hooks still run. The manifest is kept as it was. |
| Globs manifest can't be written after rsync (`cannot write manifest`) | Host marked failed. Post-host hooks still run. The old manifest is kept, so the next run retries the same deletions. |
| rsync times out (`-t`) | Host marked timed out. Post-host hooks are skipped, because with `--delete` a half-finished rsync could otherwise be committed by `post_extract_git`. |
| Post-host hook fails | Host marked failed. |
| Teardown hook fails | Run reported as failed. Exit 1. |

One host failing never stops the remaining hosts. A rsync timeout of 0
(`-t 0`) means no timeout.


Output
------

Output is plain lines without timestamps, suitable for cron mail.
Errors go to stderr. Host warnings (such as rsync exit 24, or a skipped
symlink glob match), the summary, per-host lines, `-vvv` detail and the
`-n` plan go to stdout, so they appear only at `-v` and above. The one
exception is the `EXTRACT_ROOT` permission warning (see "Sensitive
data"), which goes to stderr at every level.

| Level | All OK | Some failures |
|---|---|---|
| default | nothing | each failed or timed-out host or failed hook, with its error and rsync, probe or list step stderr, or hook output |
| `-v` | one summary line, e.g. `42 hosts ok`, plus host warnings such as `h1: warning: skipped symlink /home/bob/.ssh/config` | summary line (`40 ok, 1 failed, 1 timed out`) plus each failure itemised, plus host warnings as for all OK |
| `-vv` | adds one line per successful host, with duration, and `<host>: no files list found; using globs and tracked files only` for a globs host with no `EXTRACT_FILES_REMOTE` list | same |
| `-vvv` | adds rsync, files-list probe and list step commands, their output, hook output for successful steps, and per globs host `<host>: files list: N listed, N globbed, N tracked, N missing, N skipped` | same |

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
  `EXTRACT_ROOT`, bad source keys (none set, more than one, globs with
  `EXTRACT_TREE` or `EXTRACT_FILES_LOCAL`, or an `EXTRACT_FILES_REMOTE`
  or globs key with no paths or patterns), a bad glob pattern or an
  unreadable globs file, an
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
