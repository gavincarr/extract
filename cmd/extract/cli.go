package main

import (
	"io"

	"github.com/alecthomas/kong"
	helpcolours "github.com/gavincarr/kong-help-colours"
)

// CLI is the extract command-line grammar. Global flags apply to every
// subcommand and may appear before or after it.
type CLI struct {
	Config  string `name:"config" short:"c" default:"/etc/extract/extract.conf" help:"Config file path."`
	Verbose int    `name:"verbose" short:"v" type:"counter" help:"Increase verbosity (repeatable)."`
	Version bool   `name:"version" help:"Print version and exit."`

	// Sync is the default: plain "extract [flags] [host ...]" runs it, so
	// pre-subcommand invocations (cron jobs, scripts) keep working.
	Sync SyncCmd `cmd:"" name:"sync" default:"withargs" help:"Copy files from the remote hosts (the default command)."`
}

// SyncCmd holds the flags and arguments of the sync subcommand.
type SyncCmd struct {
	Timeout int      `name:"timeout" short:"t" default:"60" help:"Per-host timeout in seconds (0 for none)."`
	Noop    bool     `name:"noop" short:"n" help:"Dry run: show what would be done without changing anything."`
	Hosts   []string `arg:"" optional:"" name:"host" help:"Hosts to extract (default: all in config)."`
}

// exitCode is panicked by the kong exit hook so run can recover it
// instead of terminating the process.
type exitCode int

func newParser(cli *CLI, stdout, stderr io.Writer) (*kong.Kong, error) {
	return kong.New(cli,
		kong.Name("extract"),
		kong.Description("Extract configuration and data from remote hosts via rsync. With no command, extract runs sync."),
		kong.Writers(stdout, stderr),
		kong.UsageOnError(),
		kong.Help(helpcolours.Help),
		kong.Exit(func(code int) { panic(exitCode(code)) }),
	)
}
