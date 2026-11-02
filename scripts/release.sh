#!/usr/bin/env bash
# Cut a release: validate state, sync internal/version/VERSION with the
# top CHANGELOG.md entry, commit, tag. Does NOT push; that is left to the
# operator.
#
# Usage: scripts/release.sh <version>     # e.g. scripts/release.sh 0.10.0 (or v0.10.0)
#
# Workflow:
#   1. vim CHANGELOG.md                # add a "## [X.Y.Z] - YYYY-MM-DD" entry at the top
#   2. scripts/release.sh X.Y.Z        # validates, commits, tags (vX.Y.Z also accepted)
#   3. git push origin <branch> vX.Y.Z # when ready
#
# The changelog is the structural gate: you cannot tag a version that has
# no corresponding entry at the top of CHANGELOG.md.

set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "usage: scripts/release.sh <version>   (e.g. 0.10.0 or v0.10.0)" >&2
    exit 64
fi

# Accept "vX.Y.Z" as well as "X.Y.Z"; the tag always gets the "v".
VERSION="${1#v}"
TAG="v${VERSION}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." >/dev/null && pwd)"
cd "$ROOT"

fail() { echo "error: $*" >&2; exit 1; }

# The version of the first "## [X.Y.Z]" heading in CHANGELOG.md.
changelog_version() {
    sed -n 's/^## \[\([^]]*\)\].*/\1/p' CHANGELOG.md | head -n 1
}

# The body of the top CHANGELOG.md entry: everything after its heading up
# to the next "## " heading, with leading and trailing blank lines removed.
changelog_body() {
    awk '/^## \[/ { n++; next } n == 1 && /^## / { exit } n == 1 { print }' CHANGELOG.md |
        sed -e '/./,$!d' | sed -e ':a' -e '/^\n*$/{$d;N;ba' -e '}'
}

# Gate 1: semver (cheapest check first, fails fast on typos)
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    fail "<version> must be plain semver X.Y.Z (got: $VERSION)"
fi

# Gate 2: on the upstream's branch and not behind it. The branch is derived
# from the configured upstream, never hardcoded, and a missing upstream is
# an error rather than a skipped check.
UPSTREAM="$(git rev-parse --abbrev-ref '@{u}' 2>/dev/null)" ||
    fail "current branch has no upstream; set one with git branch -u <remote>/<branch>"
REMOTE="${UPSTREAM%%/*}"
UPSTREAM_BRANCH="${UPSTREAM#*/}"
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$BRANCH" != "$UPSTREAM_BRANCH" ]]; then
    fail "must release from $UPSTREAM_BRANCH (the upstream's branch); currently on $BRANCH"
fi
echo "+ fetching $UPSTREAM..."
git fetch "$REMOTE" "$UPSTREAM_BRANCH" --quiet
if [[ "$(git rev-list --count "HEAD..$UPSTREAM")" != "0" ]]; then
    fail "$BRANCH is behind $UPSTREAM; pull first"
fi

# Gate 3: working tree clean except the release-managed files.
ALLOWED='^(CHANGELOG\.md|internal/version/VERSION)$'
DIRTY="$(git status --porcelain | sed 's/^...//' | { grep -vE "$ALLOWED" || true; })"
if [[ -n "$DIRTY" ]]; then
    echo "error: working tree has uncommitted/untracked changes outside the release-managed files:" >&2
    echo "$DIRTY" | sed 's/^/  /' >&2
    exit 1
fi

# Gate 4: tag must not already exist.
if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
    fail "tag $TAG already exists"
fi

# Gate 5: the top CHANGELOG.md entry is this version.
[[ -f CHANGELOG.md ]] || fail "CHANGELOG.md not found"
CHANGELOG_VERSION="$(changelog_version)"
if [[ "$CHANGELOG_VERSION" != "$VERSION" ]]; then
    echo "error: top CHANGELOG.md entry is '${CHANGELOG_VERSION:-none}', requested release '$VERSION'." >&2
    echo "  -> add a '## [$VERSION] - $(date +%F)' entry at the top of CHANGELOG.md and try again." >&2
    exit 1
fi
BODY="$(changelog_body)"
[[ -n "$BODY" ]] || fail "CHANGELOG.md entry for $VERSION is empty"

# Gate 6: formatting, vet and tests (slowest gate, last).
echo "+ checking gofmt..."
UNFORMATTED="$(gofmt -l .)"
if [[ -n "$UNFORMATTED" ]]; then
    echo "error: gofmt -l reports unformatted files:" >&2
    echo "$UNFORMATTED" | sed 's/^/  /' >&2
    exit 1
fi
echo "+ running go vet..."
go vet ./... || fail "go vet failed"
echo "+ running tests..."
go test ./... || fail "tests failed"

# All gates passed. Preview what we're about to release.
echo
echo "About to release $TAG. Changelog entry:"
echo "----------------------------------------"
echo "$BODY"
echo "----------------------------------------"
echo

# Sync VERSION (idempotent: writing the same value is a no-op).
echo "+ writing internal/version/VERSION = $VERSION"
echo "$VERSION" > internal/version/VERSION

# Commit if there's anything to commit; re-running after a partial failure
# (e.g. at the tag step) goes straight to tagging.
git add internal/version/VERSION CHANGELOG.md
if ! git diff --cached --quiet; then
    echo "+ committing chore: release $TAG"
    git commit -m "chore: release $TAG"
else
    echo "+ nothing to commit (VERSION and CHANGELOG.md already up to date)"
fi

echo "+ tagging $TAG"
# --cleanup=whitespace: the default would strip the changelog's "### ..."
# subheadings as comment lines.
git tag -a --cleanup=whitespace "$TAG" -F - <<EOF
Release $TAG

$BODY
EOF

echo
echo "Released $TAG locally. Push with:"
echo "    git push $REMOTE $BRANCH $TAG"
