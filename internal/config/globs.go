package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/sys/unix"
)

// UsesGlobs reports whether any glob patterns are configured.
func (c *Config) UsesGlobs() bool { return len(c.Globs) > 0 }

// validGlob checks one remote glob pattern: it must be anchored at / or ~/
// and contain no whitespace or control characters.
func validGlob(p string) error {
	if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~/") {
		return fmt.Errorf("glob %q must start with / or ~/", p)
	}
	for _, r := range p {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("glob %q contains whitespace or a control character", p)
		}
	}
	return nil
}

// ParseGlobsFile reads a globs file: one pattern per line, with blank lines
// and # comment lines skipped. Every pattern is validated.
func ParseGlobsFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("globs file %s: %w", path, err)
	}
	var out []string
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := validGlob(line); err != nil {
			return nil, fmt.Errorf("globs file %s line %d: %w", path, i+1, err)
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("globs file %s lists no patterns", path)
	}
	return out, nil
}

// loadGlobs fills c.Globs and c.GlobsFile from the two globs keys.
func (c *Config) loadGlobs(env map[string]string) error {
	var all []string
	if v := env["EXTRACT_FILES_GLOBS"]; v != "" {
		pats := strings.Fields(v)
		if len(pats) == 0 {
			return fmt.Errorf("EXTRACT_FILES_GLOBS is set but lists no patterns")
		}
		for _, p := range pats {
			if err := validGlob(p); err != nil {
				return fmt.Errorf("EXTRACT_FILES_GLOBS: %w", err)
			}
		}
		all = append(all, pats...)
	}
	if v := env["EXTRACT_FILES_GLOBS_FILE"]; v != "" {
		p := v
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(c.Path), p)
		}
		p = filepath.Clean(p)
		fi, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("EXTRACT_FILES_GLOBS_FILE %s: %w", p, err)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("EXTRACT_FILES_GLOBS_FILE %s: not a regular file", p)
		}
		if err := unix.Access(p, unix.R_OK); err != nil {
			return fmt.Errorf("EXTRACT_FILES_GLOBS_FILE %s: not readable: %w", p, err)
		}
		pats, err := ParseGlobsFile(p)
		if err != nil {
			return err
		}
		c.GlobsFile = p
		all = append(all, pats...)
	}
	seen := make(map[string]bool, len(all))
	for _, p := range all {
		if !seen[p] {
			seen[p] = true
			c.Globs = append(c.Globs, p)
		}
	}
	return nil
}
