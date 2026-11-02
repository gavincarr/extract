// Package config loads and validates the shell-style extract.conf file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// SourceMode says which kind of source the config selects.
type SourceMode int

const (
	ModeTree SourceMode = iota + 1
	ModeFilesRemote
	ModeFilesLocal
)

const (
	defaultScriptsDir  = "/etc/extract/scripts"
	defaultHookTimeout = 600
)

// Config is a validated extract configuration.
type Config struct {
	Path        string // absolute path of the config file
	Root        string // absolute EXTRACT_ROOT
	Hosts       []string
	HostCmd     string
	Mode        SourceMode
	Source      string // the EXTRACT_TREE / _FILES_REMOTE / _FILES_LOCAL value
	ScriptsDir  string
	HookTimeout time.Duration

	PreSetup, PreHost, PostHost, PostTeardown []string // resolved absolute hook paths
}

// FilesArg returns Source for the files modes and "" for ModeTree.
func (c *Config) FilesArg() string {
	if c.Mode == ModeTree {
		return ""
	}
	return c.Source
}

// Load reads, parses and validates the config file at path.
func Load(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	env, err := parseFile(abs)
	if err != nil {
		return nil, err
	}
	c := &Config{Path: abs}

	root := env["EXTRACT_ROOT"]
	if root == "" {
		return nil, fmt.Errorf("EXTRACT_ROOT is required (config file %s)", abs)
	}
	if c.Root, err = filepath.Abs(root); err != nil {
		return nil, fmt.Errorf("EXTRACT_ROOT %q: %w", root, err)
	}
	fi, err := os.Stat(c.Root)
	if err != nil {
		return nil, fmt.Errorf("EXTRACT_ROOT %s: %w", c.Root, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("EXTRACT_ROOT %s: not a directory", c.Root)
	}
	if err := unix.Access(c.Root, unix.W_OK); err != nil {
		return nil, fmt.Errorf("EXTRACT_ROOT %s: not writable: %w", c.Root, err)
	}

	c.Hosts = strings.Fields(env["EXTRACT_HOSTS"])
	c.HostCmd = env["EXTRACT_HOST_CMD"]

	if err := c.loadSource(env); err != nil {
		return nil, err
	}

	c.ScriptsDir = env["EXTRACT_SCRIPTS_DIR"]
	if c.ScriptsDir == "" {
		c.ScriptsDir = defaultScriptsDir
	}
	// Hooks are exec'd with Root as their working directory, so a relative
	// scripts dir must be fixed against the current directory now.
	if c.ScriptsDir, err = filepath.Abs(c.ScriptsDir); err != nil {
		return nil, fmt.Errorf("EXTRACT_SCRIPTS_DIR %q: %w", env["EXTRACT_SCRIPTS_DIR"], err)
	}

	secs := defaultHookTimeout
	if v := strings.TrimSpace(env["EXTRACT_HOOK_TIMEOUT"]); v != "" {
		secs, err = strconv.Atoi(v)
		if err != nil || secs < 0 {
			return nil, fmt.Errorf("EXTRACT_HOOK_TIMEOUT %q: must be a non-negative integer number of seconds", v)
		}
	}
	c.HookTimeout = time.Duration(secs) * time.Second

	for _, h := range []struct {
		key string
		dst *[]string
	}{
		{"PRE_EXTRACT_SETUP", &c.PreSetup},
		{"PRE_EXTRACT_HOST", &c.PreHost},
		{"POST_EXTRACT_HOST", &c.PostHost},
		{"POST_EXTRACT_TEARDOWN", &c.PostTeardown},
	} {
		if *h.dst, err = c.resolveHooks(h.key, env[h.key]); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *Config) loadSource(env map[string]string) error {
	type src struct {
		key  string
		mode SourceMode
	}
	var set []src
	for _, s := range []src{
		{"EXTRACT_TREE", ModeTree},
		{"EXTRACT_FILES_REMOTE", ModeFilesRemote},
		{"EXTRACT_FILES_LOCAL", ModeFilesLocal},
	} {
		if env[s.key] != "" {
			set = append(set, s)
		}
	}
	switch len(set) {
	case 0:
		return fmt.Errorf("one of EXTRACT_TREE, EXTRACT_FILES_REMOTE, EXTRACT_FILES_LOCAL must be set")
	case 1:
	default:
		return fmt.Errorf("only one of EXTRACT_TREE, EXTRACT_FILES_REMOTE, EXTRACT_FILES_LOCAL may be set")
	}
	c.Mode = set[0].mode
	c.Source = env[set[0].key]
	if c.Mode == ModeFilesLocal {
		// rsync runs in Root and resolves a relative --files-from there, as
		// the Perl version did, so check that path. Source itself is passed
		// to rsync and the hooks unchanged.
		p := c.Source
		if !filepath.IsAbs(p) {
			p = filepath.Join(c.Root, p)
		}
		fi, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("EXTRACT_FILES_LOCAL %s: %w", p, err)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("EXTRACT_FILES_LOCAL %s: not a regular file", p)
		}
		if err := unix.Access(p, unix.R_OK); err != nil {
			return fmt.Errorf("EXTRACT_FILES_LOCAL %s: not readable: %w", p, err)
		}
	}
	return nil
}

func (c *Config) resolveHooks(key, val string) ([]string, error) {
	var out []string
	for _, name := range strings.Split(val, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		p := name
		if !filepath.IsAbs(p) {
			p = filepath.Join(c.ScriptsDir, p)
		}
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("%s hook %s: %w", key, p, err)
		}
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%s hook %s: not a regular file", key, p)
		}
		if err := unix.Access(p, unix.X_OK); err != nil {
			return nil, fmt.Errorf("%s hook %s: not executable: %w", key, p, err)
		}
		out = append(out, p)
	}
	return out, nil
}
