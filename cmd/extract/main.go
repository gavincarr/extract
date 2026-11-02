// Command extract pulls configuration and data from remote hosts.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/gavincarr/extract/internal/config"
	"github.com/gavincarr/extract/internal/extract"
	"github.com/gavincarr/extract/internal/report"
	"github.com/gavincarr/extract/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
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
	hosts, err := extract.ResolveHosts(cli.Hosts, cfg)
	if err != nil {
		return fail(err)
	}

	r := extract.Run(cfg, hosts, extract.Options{
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
