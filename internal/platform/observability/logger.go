package observability

import (
	"fmt"
	"log/slog"
	"os"

	"go.uber.org/fx/fxevent"

	"github.com/t4l3x/go-webapp-template/internal/config"
)

func NewLogger(
	appCfg config.App,
	logCfg config.Log,
) (*slog.Logger, error) {
	level, err := parseLogLevel(logCfg.Level)
	if err != nil {
		return nil, err
	}

	logger := slog.New(
		slog.NewJSONHandler(
			os.Stdout,
			&slog.HandlerOptions{
				Level: level,
			},
		),
	).With(
		"service", appCfg.Service,
		"version", appCfg.Version,
		"environment", appCfg.Environment,
	)

	return logger, nil
}

func SetDefaultLogger(logger *slog.Logger) {
	slog.SetDefault(logger)
}

func NewFxLogger(logger *slog.Logger) fxevent.Logger {
	fxLogger := &fxevent.SlogLogger{
		Logger: logger.With("component", "fx"),
	}

	fxLogger.UseLogLevel(slog.LevelDebug)
	fxLogger.UseErrorLevel(slog.LevelError)

	return fxLogger
}

func parseLogLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported LOG_LEVEL %q", value)
	}
}
