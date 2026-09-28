package config

import (
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
)

type Log struct {
	Level string `env:"LOG_LEVEL" envDefault:"info"`
}

func LoadLog() (Log, error) {
	cfg, err := env.ParseAs[Log]()
	if err != nil {
		return Log{}, fmt.Errorf("load log config: %w", err)
	}

	cfg.Level = strings.ToLower(cfg.Level)

	switch cfg.Level {
	case "debug", "info", "warn", "error":
	default:
		return Log{}, fmt.Errorf(
			"unsupported LOG_LEVEL %q",
			cfg.Level,
		)
	}

	return cfg, nil
}
