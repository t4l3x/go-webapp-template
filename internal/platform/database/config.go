package database

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	DSN string `env:"DB_DSN,required,notEmpty"`

	MaxConns     int32 `env:"DB_MAX_CONNS" envDefault:"10"`
	MinIdleConns int32 `env:"DB_MIN_IDLE_CONNS" envDefault:"2"`

	MaxConnLifetime       time.Duration `env:"DB_MAX_CONN_LIFETIME" envDefault:"1h"`
	MaxConnLifetimeJitter time.Duration `env:"DB_MAX_CONN_LIFETIME_JITTER" envDefault:"5m"`
	MaxConnIdleTime       time.Duration `env:"DB_MAX_CONN_IDLE_TIME" envDefault:"15m"`
	HealthCheckPeriod     time.Duration `env:"DB_HEALTH_CHECK_PERIOD" envDefault:"1m"`

	ConnectTimeout time.Duration `env:"DB_CONNECT_TIMEOUT" envDefault:"5s"`
	PingTimeout    time.Duration `env:"DB_PING_TIMEOUT" envDefault:"5s"`
}

func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("load database config: %w", err)
	}

	if cfg.MaxConns <= 0 {
		return Config{}, fmt.Errorf("DB_MAX_CONNS must be greater than zero")
	}

	if cfg.MinIdleConns < 0 || cfg.MinIdleConns > cfg.MaxConns {
		return Config{}, fmt.Errorf(
			"DB_MIN_IDLE_CONNS must be between 0 and DB_MAX_CONNS",
		)
	}

	if cfg.MaxConnLifetime <= 0 ||
		cfg.MaxConnLifetimeJitter <= 0 ||
		cfg.MaxConnIdleTime <= 0 ||
		cfg.HealthCheckPeriod <= 0 ||
		cfg.ConnectTimeout <= 0 ||
		cfg.PingTimeout <= 0 {
		return Config{}, fmt.Errorf("database durations must be greater than zero")
	}

	return cfg, nil
}
