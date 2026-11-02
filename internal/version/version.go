// Package version exposes the current extract release version, embedded
// from VERSION at compile time. The release flow (scripts/release.sh)
// rewrites that file in lockstep with debian/changelog and the git tag,
// so binaries always agree with the packaging.
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var raw string

// Version is the trimmed contents of internal/version/VERSION — a plain
// semver string like "0.1.0".
var Version = strings.TrimSpace(raw)
