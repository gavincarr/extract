// Command extract pulls configuration and data from remote hosts.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/gavincarr/extract/internal/config"
	"github.com/gavincarr/extract/internal/extract"
	"github.com/gavincarr/extract/internal/report"
	"github.com/gavincarr/extract/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// interruptSignals lists the signals that should cancel a run, leaving out
// any that were already ignored when extract started (SIGHUP under nohup,
// SIGINT for a background job in a script): signal.Notify would otherwise
// un-ignore them.
func interruptSignals(ignored func(os.Signal) bool) []os.Signal {
	var sigs []os.Signal
	for _, s := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} {
		if !ignored(s) {
			sigs = append(sigs, s)
		}
	}
	return sigs
}

// signalContext returns a context cancelled by the first interrupt signal
// that wasn't ignored at startup. With every signal ignored it returns a
// context that is never cancelled, because signal.NotifyContext with no
// signals would relay all of them.
func signalContext(ignored func(os.Signal) bool) (context.Context, context.CancelFunc) {
	sigs := interruptSignals(ignored)
	if len(sigs) == 0 {
		return context.Background(), func() {}
	}
	return signal.NotifyContext(context.Background(), sigs...)
}

// run parses args and returns the process exit code. Kong exits are
// trapped: code 0 (e.g. --help) stays 0, anything else becomes 2.
func run(args []string, stdout, stderr io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			c, ok := r.(exitCode)
			if !ok {
				panic(r)
			}
			if c == 0 {
				code = 0
			} else {
				code = 2
			}
		}
	}()

	var cli CLI
	parser, err := newParser(&cli, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "extract:", err)
		return 2
	}
	if _, err := parser.Parse(args); err != nil {
		parser.Stdout = stderr // usage goes to stderr on errors
		parser.FatalIfErrorf(err)
		return 2
	}
	if cli.Version {
		fmt.Fprintf(stdout, "extract %s\n", version.Version)
		return 0
	}

	// Children run in their own process groups, so a terminal Ctrl-C or a
	// kill of extract would not reach them; cancel instead, which kills the
	// running child's group (EXTRACT_HOST_CMD, a hook or rsync) and starts
	// nothing further.
	ctx, stop := signalContext(signal.Ignored)
	defer stop()

	log := report.NewLogger(stdout, stderr, cli.Verbose)
	fail := func(err error) int {
		log.Error("extract: " + err.Error())
		return 2
	}

	if cli.Timeout < 0 {
		return fail(fmt.Errorf("invalid timeout %d: must be >= 0", cli.Timeout))
	}
	if _, err := exec.LookPath("rsync"); err != nil {
		return fail(fmt.Errorf("rsync not found on PATH"))
	}
	cfg, err := config.Load(cli.Config)
	if err != nil {
		return fail(err)
	}
	hosts, err := extract.ResolveHosts(ctx, cli.Hosts, cfg)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Interrupted, not misconfigured: exit 1 like any interrupted run.
			log.Error("extract: " + err.Error())
			return 1
		}
		return fail(err)
	}

	r := extract.Run(cfg, hosts, extract.Options{
		Ctx:       ctx,
		Noop:      cli.Noop,
		Verbosity: cli.Verbose,
		Timeout:   time.Duration(cli.Timeout) * time.Second,
	})
	if cli.Noop {
		report.PrintPlan(stdout, r)
	}
	report.Report(log, r)
	return report.ExitCode(r)
}
