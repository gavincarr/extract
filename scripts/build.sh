#!/usr/bin/env bash
# Build extract into bin/extract.
#
# Usage: scripts/build.sh
#
# The version comes from internal/version/VERSION (embedded at compile
# time), so no ldflags are needed.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." >/dev/null && pwd)"
cd "$ROOT"

go build -trimpath -o bin/extract ./cmd/extract
echo "built $(bin/extract --version) -> bin/extract"
