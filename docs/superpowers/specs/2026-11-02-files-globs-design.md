# EXTRACT_FILES_GLOBS: glob-selected files and deletion tracking

Status: design, for planning. Builds on 0.9.0 (multi-path
`EXTRACT_FILES_REMOTE`). Read `CLAUDE.md` and
`2026-11-02-go-port-design.md` first; this document only describes what
changes.

## Intent

The `{deb,rpm}-find-changes` lists only catch files that differ from
what a package shipped. Some security-relevant files belong to no
package, notably every user's `.ssh/authorized_keys*` and `.ssh/config`.
The operator wants these extracted from every host and committed by
`post_extract_git`, for two reasons:

- **Review.** The daily git report can be read for anything odd: a key
  nobody recognises, a new `Host` block or `ProxyCommand`, a removed
  entry. Odd findings are followed up by hand. This is a review aid, not
  an alarm system.
- **Restore.** The extract tree doubles as a backup of host configuration.
  It should be thorough, including the files that hold secrets.

Two things make this work:

1. **Globs.** A central list of absolute glob patterns, expanded on each
   host, adds files to that host's extract.
2. **Deletion tracking.** With `--files-from`, rsync's `--delete` does
   nothing, so a file deleted on the host stays in the local tree, and in
   git, looking current. This was verified on rsync 3.2.7; see "rsync
   findings" below. For `.ssh` files a removal matters as much as an
   addition, so removals must show up as deletions.

## Sensitive data stance (operator decision)

extract deliberately copies secrets: private keys, `/etc/shadow`,
passwords in configs, and anything else the lists and globs reach.
Filtering them out was considered and rejected, for three reasons:

- The extract host already has root ssh access to the whole fleet, so its
  copies add almost no marginal risk.
- Any filter would only ever catch a subset of the sensitive files.
- These are exactly the files a restore needs.

Security comes from protecting the store, not from leaving files out:

- **In this change:** a startup warning when `EXTRACT_ROOT` is group- or
  world-accessible, and a README section on handling the data (see
  "Other changes").
- **Separate ops work, not part of this change:** client-side encrypted
  backups of the extract host to replace its unencrypted Linode backups,
  and an encrypted git remote (e.g. `git-remote-gcrypt`).

## Goals

- `EXTRACT_FILES_GLOBS` selects extra files per host, either alongside
  `EXTRACT_FILES_REMOTE` (today's setup: always both) or on its own
  (future hosts with no deb/rpm tooling, such as Arch). Both kinds of host
  can share one config.
- When globs are in use, every file extract previously extracted for a
  host is deleted locally once it no longer exists on the host, so
  `post_extract_git` commits the deletion. (`git add .` stages removals.)
- An operator whose `EXTRACT_ROOT` is readable by other local users finds
  out from cron mail.

## Non-goals

- Filtering out private keys or other secrets (see the stance above).
- Encrypting anything inside the repo, pushing to a remote, or changing
  host backups.
- Changing anything else for configs without `EXTRACT_FILES_GLOBS`. They
  behave exactly as in 0.9.0, including the "stale file stays forever"
  behaviour. The `EXTRACT_ROOT` permission warning is the one exception.
- Globs with `EXTRACT_TREE` (rsync already recurses and deletes there) or
  with `EXTRACT_FILES_LOCAL` (possible later; not needed now).
- Cleaning up stale files left by runs from before the upgrade.
- Supporting patterns that contain whitespace, or filenames that contain
  a newline.
- Recursive patterns (`**`) or whole-directory extraction through
  globs.
- History files, caches and application state (see the shipped list's
  exclusions).

## Anti-goals (failure even if it "works")

- extract deleting files it didn't put there, such as the
  `post_extract_tarball` hook's `<host>-YYYYMMDD.tar.gz` files in the
  host directory.
- A file that is gone from the host, or is no longer extracted, staying
  in the tree looking current.
- A login shell that can't run the remote script being reported as
  "nothing found" instead of as a failure.
- A glob match that is a user-controlled symlink pulling an arbitrary
  file into another file's place in the review.

## rsync findings (rsync 3.2.7, extract's flags)

These were run with `--archive --copy-links --delete --files-from=<list>`,
local to local:

| Case | Result |
|---|---|
| File deleted on the source, still in the list | exit 23 (host fails); stale copy kept |
| Same, plus `--delete-missing-args` | exit 0; local copy deleted |
| File deleted and dropped from the list | exit 0; stale copy kept silently |
| File still on the source, dropped from the list | exit 0; copy kept, frozen |

So deletion tracking = list everything previously extracted, and add
`--delete-missing-args`. That option needs rsync >= 3.1.0 (2013) on both
ends; document the requirement.

## Config

### Patterns

Patterns use POSIX `sh` pathname-expansion syntax (`*`, `?`, `[...]`;
no `**`, no braces). Each one is either:

- **absolute** (`/var/spool/cron/*`), expanded as is; or
- **home-relative** (`~/.ssh/*`), expanded once for every home directory
  on the host (see "Home directories" below). *(Operator decision.)* One
  pattern therefore covers root, every user in `/home`, and service
  accounts with homes such as `/var/lib/git` or `/srv/deploy`, which is
  where a backdoor key is easiest to overlook.

A pattern only ever matches regular files at the level it names; nothing
recurses. That is what keeps a broad list small.

### Keys

- **`EXTRACT_FILES_GLOBS`**: a quoted, whitespace-separated list of
  patterns, for short lists. Example:
  `EXTRACT_FILES_GLOBS="~/.ssh/* /var/spool/cron/*"`. This is safe in the
  dual-format config: shell assignments don't glob-expand, and a quoted
  `~` isn't expanded.
- **`EXTRACT_FILES_GLOBS_FILE`**: a path, on the local host, to a file
  with one pattern per line. Blank lines and `#` comment lines are
  ignored, and leading and trailing whitespace is trimmed. *(Operator
  decision: the config parser reads one line per key, which doesn't suit
  a list of 50 patterns.)*
  - A relative path is resolved against the config file's directory,
    so `/etc/extract/extract.conf` can say
    `EXTRACT_FILES_GLOBS_FILE=extract.globs`. *(My call.)*
  - The file must be a readable regular file. It is read at load time,
    not passed to the remote host.
- Both keys may be set; the patterns are the union of the two.

### Validation at load (exit 2 on error)

- Each pattern must start with `/` or `~/`. *(My call.)*
- Patterns must not contain whitespace, or the characters the remote
  script uses for quoting. Pick the exact rule to fit the quoting scheme.
  An error from the globs file names the file and line number.
- A set but whitespace-only `EXTRACT_FILES_GLOBS`, or a globs file with
  no patterns, is an error, as for `EXTRACT_FILES_REMOTE`.

### Source rules

They become: exactly one of `EXTRACT_TREE`, `EXTRACT_FILES_LOCAL`, or
"remote files". Remote files means `EXTRACT_FILES_REMOTE` and/or globs
(from either key). Globs with `EXTRACT_TREE` or `EXTRACT_FILES_LOCAL` is
a config error naming the conflict.

## Per-host flow when EXTRACT_FILES_GLOBS is set

This replaces 0.9.0's probe for that host. There is still exactly one
ssh call and one rsync per host. Without globs, 0.9.0 behaviour is
unchanged.

### 1. One ssh call builds the list

`ssh -o BatchMode=yes [user@]host <command>`, run like the 0.9.0 probe:
from `EXTRACT_ROOT`, with the `-t` timeout, the same ctx and interrupt
handling, and also under `-n` (it only reads).

- **The script must run under POSIX `sh` whatever the login shell is.**
  Run it as `sh -c '<script>'`, quoted for the login shell, or an
  equivalent. *(My call. The 0.9.0 probe relied on the login shell being
  POSIX, and that is the known csh gap. This script is bigger, so close
  the gap here.)*
- **Its stdin is the host's manifest** (see step 4), one path per line,
  or empty.
- **The remote script does this, in order:**
  1. **Remote list.** If `EXTRACT_FILES_REMOTE` is set, choose its first
     non-empty candidate, using the same `test -s` rule as 0.9.0, and
     emit every path in the chosen list. **Finding no non-empty
     candidate is not a failure when globs are set** *(operator
     decision)*. The host just uses its globs and its manifest, and the
     result records that no list was found. That way globs-only hosts
     can share a config with deb/rpm hosts, and if an RPM host's list
     generator breaks, its already-tracked files keep syncing through the
     manifest.
  2. **Globs.** Expand each absolute pattern once, and each `~/`
     pattern once per home directory (see "Home directories" below).
     For each match:
     - **Symlink check.** It is a symlink match if the match itself, or
       any directory between the home (or `/`) and the match, is a
       symlink.
       - For a `~/` pattern, a symlink match is still accepted when its
         fully resolved path (`readlink -f`) lies inside that home's own
         resolved path. Dotfile managers such as stow, chezmoi and yadm
         make `~/.bashrc -> ~/dotfiles/bashrc`, and those links must
         work. Links that resolve outside the home are emitted as
         "symlink" and not listed, which blocks both
         `~/.ssh/config -> /etc/shadow` and `~/.ssh -> /root/.ssh`.
         *(Operator decision.)*
       - For an absolute pattern, every symlink match is emitted as
         "symlink" and not listed. *(My call: there is no home to
         compare against.)*
     - **Regular files** (`test -f`) are emitted as files.
     - **Anything else** is ignored. A pattern that matches nothing
       contributes nothing.

     Paths from the remote list keep today's `--copy-links` behaviour;
     the symlink rule applies only to glob matches.
  3. **Manifest.** For each manifest path read from stdin, emit it as a
     file if it exists, or as "missing" if it doesn't.
  4. **Sentinel.** Print an end-of-output sentinel last. Output without
     it is a step failure, never "empty list". This is what catches a
     shell that couldn't run the script.
- **Home directories.** These are the distinct home fields from
  `getent passwd`, falling back to `/etc/passwd` when `getent` is
  missing (as on some busybox systems). A home is skipped if it:
  - is `/`;
  - doesn't exist or isn't a directory (`/nonexistent`); or
  - contains whitespace or glob metacharacters.

  The home part is literal and never glob-expanded. Many system
  accounts share homes such as `/usr/sbin` or `/bin`; after deduping,
  those simply match nothing.
- **The output format is the builder's call**, but it must be
  unambiguous. For example, one record per line with a type prefix: `F`
  file, `M` missing, `L` symlinked glob match, `N` no remote list found,
  `E` end sentinel.
- **Paths containing a newline** can't be represented. If one is
  encountered (from a glob), don't list it, and emit it as a skipped
  path. *(My call.)* The same goes for a **carriage return**, which
  rsync's `--files-from` also treats as a line end: such a name would
  reach rsync as two other paths, and `--delete-missing-args` would
  delete their local namesakes (found in review: `~/.ssh/x\r.git`
  deleted the host directory's `.git`).

### 2. Interpreting the result

The step fails the host, exactly like a 0.9.0 probe failure, if:
- ssh fails, times out or is interrupted;
- the sentinel is missing.

A failed step skips pre-host hooks, rsync and post-host hooks. On
success:

- **The rsync list** is the deduplicated union of the `F` and `M` paths,
  minus every `L` path. A symlinked glob match stays out even if the
  manifest also lists it, since the manifest would otherwise follow the
  link. `M` paths are listed on purpose, so that `--delete-missing-args`
  deletes their local copies.
- **`L` and newline paths** become `-v` warnings on the host, alongside
  rsync's exit-24 warning, e.g. `skipped symlink /home/bob/.ssh/config`.
- **An `L` path with a local copy** in the host directory (extracted on
  an earlier run, before it became a link) has that copy deleted, and the
  warning says so. This keeps the tree from holding a frozen copy that
  looks current. The delete must stay inside the host directory.
  *(My call.)*
- **An empty union is fine.** rsync runs and transfers nothing.

### 3. rsync

The base flags stay as they are, plus `--delete-missing-args` and
`--files-from=<local temp file>` (written by extract, outside the host
directory, e.g. `os.CreateTemp`, and removed after rsync). The source is
`[user@]host:/` and the destination `host`, as now. Exit code handling is
unchanged.

### 4. Manifest

- **What it is.** Per host, the set of paths extract has extracted and
  that still exist. It lets later runs list, and so delete, files that
  have disappeared. It is what limits deletion to files extract itself
  put there, which keeps the tarball hook's outputs and any other
  hook-created files safe. So it is not a walk of the host tree.
- **Location.** `$EXTRACT_ROOT/.extract-state/<host>.files`, one path
  per line. *(My call.)* It sits outside the host directory, so
  `post_extract_git`'s `git add .` (run in the host directory) never
  commits it. `extract_git_report`'s `for i in *` skips dot-directories.
  A host literally named `.extract-state` must not collide; host names
  starting with `.` can simply be rejected.
- **When it is written.** Only after rsync exits 0 or 24, and never under
  `-n`. Its content is the `F` paths in the rsync list. Write it
  atomically (temp file plus rename). On any failure leave the old
  manifest alone, so the next run retries the same deletions.
- **No manifest** (first run after upgrade, or a new host): treat it as
  empty. Pre-upgrade stale files are not cleaned up (non-goal).
- **Consequence the operator accepted.** A file that drops out of the
  find-changes list but still exists on the host stays in the manifest,
  so it keeps being extracted. A revert to the package default therefore
  shows up as a diff instead of being frozen.

### 5. Hook `$4`

Hook `$4` is the chosen `EXTRACT_FILES_REMOTE` path, or `""` when globs
are used alone or when that host had no list. It is not the temp file.
The hook contract is otherwise unchanged.

## Shipped globs list: `conf/extract.globs.dist`

This is a recommended starting list for `EXTRACT_FILES_GLOBS_FILE`, not
a built-in default. Without a globs key, behaviour is unchanged. Every
entry has to pass two tests:

- **Small and hand-maintained**, worth having for a restore.
- **Changes rarely.** Review is the point, and a file that changes every
  night drowns the changes that matter.

Ship it with these groups, each under a comment header explaining why
it's there:

```
# Access and persistence: the highest review value. ~/.ssh/* also
# catches keys, known_hosts, ~/.ssh/rc and ~/.ssh/environment.
~/.ssh/*
~/.k5login
~/.rhosts
~/.shosts
~/.forward
~/.config/systemd/user/*
# User crontabs: RHEL keeps them in /var/spool/cron, Debian in
# /var/spool/cron/crontabs. Directories are skipped, so both are safe.
/var/spool/cron/*
/var/spool/cron/crontabs/*

# Shell startup: persistence vectors, and needed for a restore. Listed
# one by one, because ~/.bash* would also match .bash_history.
~/.profile
~/.bashrc
~/.bash_profile
~/.bash_login
~/.bash_logout
~/.bash_aliases
~/.zshrc
~/.zshenv
~/.zprofile
~/.zlogin
~/.cshrc
~/.tcshrc
~/.login

# Credentials: tiny, and painful to rebuild.
~/.netrc
~/.pgpass
~/.my.cnf
~/.git-credentials
~/.docker/config.json
~/.kube/config
~/.aws/config
~/.aws/credentials
~/.s3cfg
~/.npmrc
~/.pypirc
~/.vault-token
~/.config/rclone/rclone.conf

# GnuPG: enough to restore a keyring. random_seed is left out because
# it changes on every use.
~/.gnupg/*.conf
~/.gnupg/pubring.kbx
~/.gnupg/pubring.gpg
~/.gnupg/secring.gpg
~/.gnupg/trustdb.gpg
~/.gnupg/sshcontrol
~/.gnupg/private-keys-v1.d/*
~/.gnupg/openpgp-revocs.d/*

# Tool config: optional, but rarely noisy.
~/.gitconfig
~/.config/git/config
~/.vimrc
~/.tmux.conf
~/.screenrc
~/.inputrc
```

The file ends with a comment listing what is left out on purpose, and
why:

- **History files** (`.bash_history`, `.zsh_history`, `.mysql_history`,
  `.psql_history`, `.lesshst`, `.viminfo`): noisy daily, full of secrets
  typed at the prompt, and little use for a restore.
- **`~/.config/*` wholesale:** mostly application state, so name specific
  files instead.
- **Caches and data:** `~/.cache`, `~/.local/share`, browser and IDE
  profiles, `~/.kube/cache`, and toolchains (`.cargo`, `.rustup`, `.npm`,
  `go/`).

Expected size is tens of KB per user. That's unmeasured, but no entry is
normally large; `pubring.kbx` can reach a few MB with big keyrings.

## EXTRACT_ROOT permission warning

This applies to every config, with or without globs. At startup, after
the config loads, if `EXTRACT_ROOT`'s mode grants any group or other
permission (`mode & 0o077 != 0`), print a warning at every verbosity:
`slog.LevelWarn`, which the handler sends to stderr at the default
level. Point to the README section. It does not change the exit code and
does not stop the run. It is printed under `-n` too. *(My call: warn,
not refuse, so an existing cron setup isn't broken by an upgrade.)*

The reason is that rsync keeps each file's original mode in the working
tree, but git's object store does not. `.git/objects` gets umask
permissions, so only `EXTRACT_ROOT`'s own mode keeps other local users
out.

## Output

- **`-v`** shows the new symlink and newline warnings with the existing
  host warnings, on stdout.
- **`-vv`** adds a per-host note when no `EXTRACT_FILES_REMOTE` list was
  found.
- **`-vvv`** traces the ssh step like the 0.9.0 probe, plus a one-line
  summary of the list (counts of listed, globbed, missing and skipped
  paths).
- **`-n` plan** shows the ssh step and that summary instead of 0.9.0's
  `checked files lists:` line, then the `would rsync` line with
  `--delete-missing-args`. The exact wording is the builder's call.

## Other changes this implies

- **`internal/proc`** needs a way to pass stdin to a step. `RunContext`
  currently hard-wires `/dev/null`.
- **The fake ssh in `internal/testutil`** must pass stdin through and run
  the real script under `sh`, so the tests exercise the actual remote
  script against local files. Also test the script under both `dash` and
  `bash` where they're available. For `~/` patterns the tests must
  control the home list. For example, the fake ssh can put a fake
  `getent` first on `PATH` for the script, printing a passwd made of
  temp-dir homes. The real script must not grow a test-only hook.
- **README:**
  - the config table, the sources rule, the failure table and the output
    levels;
  - the rsync >= 3.1 requirement;
  - a new **"Sensitive data"** section saying that:
    - extract copies secrets on purpose, and git history keeps them,
      rotated ones included, forever;
    - `EXTRACT_ROOT` should be `0700` and owned by root, and why;
    - full-disk encryption on the extract host is recommended;
    - reports must stay file-name only;
    - any copy of the repo or host backups must be encrypted before it
      leaves the host;
    - one known gap: a symlink swapped in between the list-building step
      and rsync is still followed.
- **`bin/extract_git_report`:** a comment at the `git log --stat` calls
  saying the report must never include content (`-p`), because the repo
  holds secrets.
- **`conf/extract.conf.dist`:** commented examples of
  `EXTRACT_FILES_GLOBS_FILE=extract.globs` (pointing at the installed
  copy of `conf/extract.globs.dist`) and of a short
  `EXTRACT_FILES_GLOBS`. The README's install steps gain the globs file,
  with the same "don't clobber local edits" care as the config. A test
  (like `TestLoadShippedDist`) parses `conf/extract.globs.dist` and
  checks every pattern passes validation.
- **README, globs:** document both keys, `~/` patterns and how homes are
  found, the symlink rule, and the shipped list with its two tests (small
  and rarely changing).
- **`2026-11-02-go-port-design.md`:** the sources, rsync invocation and
  hook `$4` sections.
- **`CLAUDE.md`:** the architecture notes for the manifest and the
  permission warning.
- **Version:** bump to `0.10.0`. *(My call.)*

## Left to the builder

- The remote script's exact text and quoting scheme, its output format
  and its exit codes.
- Type and field names on `Config` and `HostResult`, and whether the
  0.9.0 probe code is generalised or a sibling is added.
- Where the permission check lives (`sync.go` is the natural place, since
  `extract.Run` never prints).
- Warning, plan and trace wording.
- Test structure, as long as it covers:
  - every source combination, and the config errors;
  - no remote list found while globs are set;
  - `~/` expansion: several homes, a service-account home outside
    `/home`, shared and nonexistent homes, a home that is itself a
    symlink, and the `/etc/passwd` fallback;
  - the globs file: comments, blank lines, a relative path, errors with
    line numbers, and the union with `EXTRACT_FILES_GLOBS`;
  - symlinks:
    - an in-home dotfile-manager link is followed;
    - links that resolve outside the home are skipped, both for the file
      and for a symlinked `.ssh` directory;
    - an absolute-pattern symlink is skipped;
    - a manifest path that has become a skipped symlink has its local
      copy deleted;
  - deletion of a manifest path that has disappeared, with a
    hook-created file in the host directory surviving;
  - the manifest not being written on failure or under `-n`;
  - the sentinel catching a non-POSIX login shell;
  - the permission warning, at the default verbosity and on stderr;
  - an end-to-end run.
