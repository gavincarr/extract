package main

import (
	"context"
	"io"
	"log/slog"

	"github.com/gavincarr/extract/internal/config"
)

// app is the state every subcommand shares: the parsed global flags, the
// logger, the stdout writer and the context cancelled by interrupt signals.
type app struct {
	ctx    context.Context
	cli    *CLI
	log    *slog.Logger
	stdout io.Writer
}

// fail logs a usage or configuration error and returns exit code 2.
func (a *app) fail(err error) int {
	a.log.Error("extract: " + err.Error())
	return 2
}

// loadConfig loads the config named by --config. On error it has already
// been logged, and the returned code is the exit code to use.
func (a *app) loadConfig() (*config.Config, int) {
	cfg, err := config.Load(a.cli.Config)
	if err != nil {
		return nil, a.fail(err)
	}
	return cfg, 0
}
