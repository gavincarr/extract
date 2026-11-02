// Package report renders a run Result as plain, timestamp-free log lines.
package report

import (
	"context"
	"io"
	"log/slog"
)

// LevelTrace is the level used for -vvv step detail.
const LevelTrace = slog.LevelDebug - 4

type handler struct {
	stdout, stderr io.Writer
	level          slog.Level
}

// NewLogger returns a logger whose level follows the verbosity: Warn at 0,
// Info at 1, Debug at 2 and LevelTrace at 3 or more. Each record is written
// as its message and a newline; Warn and above go to stderr, the rest to
// stdout.
func NewLogger(stdout, stderr io.Writer, verbosity int) *slog.Logger {
	lvl := slog.LevelWarn
	switch {
	case verbosity >= 3:
		lvl = LevelTrace
	case verbosity == 2:
		lvl = slog.LevelDebug
	case verbosity == 1:
		lvl = slog.LevelInfo
	}
	return slog.New(&handler{stdout: stdout, stderr: stderr, level: lvl})
}

func (h *handler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	w := h.stdout
	if r.Level >= slog.LevelWarn {
		w = h.stderr
	}
	_, err := io.WriteString(w, r.Message+"\n")
	return err
}

func (h *handler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *handler) WithGroup(string) slog.Handler      { return h }
