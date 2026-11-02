package config

import (
	"fmt"
	"sort"
	"strings"
)

// knownKeys is the single list of keys extract itself reads.
// TestKnownKeysMatchSource checks it against the string literals in this
// package, so adding a read without listing the key here fails the tests.
var knownKeys = map[string]bool{
	"EXTRACT_ROOT":             true,
	"EXTRACT_HOSTS":            true,
	"EXTRACT_HOST_CMD":         true,
	"EXTRACT_TREE":             true,
	"EXTRACT_FILES_REMOTE":     true,
	"EXTRACT_FILES_LOCAL":      true,
	"EXTRACT_FILES_GLOBS":      true,
	"EXTRACT_FILES_GLOBS_FILE": true,
	"EXTRACT_SCRIPTS_DIR":      true,
	"EXTRACT_HOOK_TIMEOUT":     true,
	"PRE_EXTRACT_SETUP":        true,
	"PRE_EXTRACT_HOST":         true,
	"POST_EXTRACT_HOST":        true,
	"POST_EXTRACT_TEARDOWN":    true,
}

// hookKeys are read by the shipped hook scripts, not by extract, but live in
// extract's namespace so are accepted.
var hookKeys = map[string]bool{
	"EXTRACT_GIT_REPO_MODE": true,
}

func isKnownOrAllowed(key string) bool { return knownKeys[key] || hookKeys[key] }

func inNamespace(key string) bool {
	return strings.HasPrefix(key, "EXTRACT_") ||
		strings.HasPrefix(key, "PRE_EXTRACT_") ||
		strings.HasPrefix(key, "POST_EXTRACT_")
}

// checkKeys returns an error naming every key in extract's namespaces that
// is neither known nor allowed. lines maps each key to the line it first
// appears on.
func checkKeys(path string, lines map[string]int) error {
	var bad []string
	for k := range lines {
		if inNamespace(k) && !isKnownOrAllowed(k) {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	msgs := make([]string, len(bad))
	for i, k := range bad {
		msgs[i] = fmt.Sprintf("line %d: unknown key %s", lines[k], k)
		if s := suggestKey(k); s != "" {
			msgs[i] += fmt.Sprintf(" (did you mean %s?)", s)
		}
	}
	return fmt.Errorf("config file %s %s", path, strings.Join(msgs, "; "))
}

// suggestKey returns the closest known or allowed key when it is within 3
// edits or a third of the key's length, else "". Ties go to the first
// alphabetically.
func suggestKey(key string) string {
	limit := max(3, len(key)/3)
	best, bestD := "", limit+1
	for _, m := range []map[string]bool{knownKeys, hookKeys} {
		for c := range m {
			d := editDistance(key, c)
			if d < bestD || (d == bestD && c < best) {
				best, bestD = c, d
			}
		}
	}
	return best
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
