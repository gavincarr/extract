package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/gavincarr/extract/internal/extract"
	"github.com/gavincarr/extract/internal/report"
)

// sync runs the sync subcommand: copy files from each host with rsync,
// running the configured hooks, and report the result.
func (a *app) sync(c *SyncCmd) int {
	if c.Timeout < 0 {
		return a.fail(fmt.Errorf("invalid timeout %d: must be >= 0", c.Timeout))
	}
	if _, err := exec.LookPath("rsync"); err != nil {
		return a.fail(fmt.Errorf("rsync not found on PATH"))
	}
	cfg, code := a.loadConfig()
	if cfg == nil {
		return code
	}
	a.warnRootPerms(cfg.Root)
	hosts, err := extract.ResolveHosts(a.ctx, c.Hosts, cfg)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Interrupted, not misconfigured: exit 1 like any interrupted run.
			a.log.Error("extract: " + err.Error())
			return 1
		}
		return a.fail(err)
	}

	r := extract.Run(cfg, hosts, extract.Options{
		Ctx:       a.ctx,
		Noop:      c.Noop,
		Verbosity: a.cli.Verbose,
		Timeout:   time.Duration(c.Timeout) * time.Second,
	})
	if c.Noop {
		report.PrintPlan(a.stdout, r)
	}
	report.Report(a.log, r)
	return report.ExitCode(r)
}

// warnRootPerms warns, without failing, when root grants any group or other
// permission. rsync keeps each file's mode in the tree, but git's object
// store uses the umask, so root's own mode is what keeps local users out.
func (a *app) warnRootPerms(root string) {
	fi, err := os.Stat(root)
	if err != nil {
		return // config.Load has just checked it; rsync will report anything new
	}
	if mode := fi.Mode().Perm(); mode&0o077 != 0 {
		a.log.Warn(fmt.Sprintf("warning: EXTRACT_ROOT %s is accessible by group or others (mode %04o); it holds copies of remote secrets, so chmod 700 it (see README \"Sensitive data\")", root, mode))
	}
}
