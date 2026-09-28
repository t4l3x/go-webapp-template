package testkit

import (
	"bytes"
	"log/slog"
)

// NewLogger returns a *slog.Logger that writes JSON log lines to the
// returned buffer, so tests can assert on log output.
func NewLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}

	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	return logger, buf
}
