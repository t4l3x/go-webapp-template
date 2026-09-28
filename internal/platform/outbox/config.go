package outbox

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	PollInterval time.Duration `env:"OUTBOX_POLL_INTERVAL" envDefault:"2s"`
	BatchSize    int           `env:"OUTBOX_BATCH_SIZE" envDefault:"10"`
	MaxAttempts  int           `env:"OUTBOX_MAX_ATTEMPTS" envDefault:"10"`

	// ClaimLease covers the entire sequential batch, including outcome writes.
	ClaimLease time.Duration `env:"OUTBOX_CLAIM_LEASE" envDefault:"3m"`

	BaseBackoff time.Duration `env:"OUTBOX_BASE_BACKOFF" envDefault:"5s"`
	MaxBackoff  time.Duration `env:"OUTBOX_MAX_BACKOFF" envDefault:"5m"`

	ShutdownTimeout time.Duration `env:"OUTBOX_SHUTDOWN_TIMEOUT" envDefault:"10s"`
}

func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("load outbox config: %w", err)
	}

	if cfg.PollInterval <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_POLL_INTERVAL must be greater than zero")
	}
	if cfg.BatchSize <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_BATCH_SIZE must be greater than zero")
	}
	if cfg.MaxAttempts <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_MAX_ATTEMPTS must be greater than zero")
	}
	if cfg.ClaimLease <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_CLAIM_LEASE must be greater than zero")
	}
	if cfg.BaseBackoff <= 0 || cfg.MaxBackoff <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_BASE_BACKOFF and OUTBOX_MAX_BACKOFF must be greater than zero")
	}
	if cfg.BaseBackoff > cfg.MaxBackoff {
		return Config{}, fmt.Errorf("OUTBOX_BASE_BACKOFF must not exceed OUTBOX_MAX_BACKOFF")
	}
	if cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_SHUTDOWN_TIMEOUT must be greater than zero")
	}

	return cfg, nil
}

// ValidateHandlerTimeout checks the composed worker's actual I/O budget without
// importing a feature or mail adapter into the outbox. Division avoids overflow.
func (c Config) ValidateHandlerTimeout(timeout time.Duration) error {
	if c.BatchSize <= 0 || timeout <= 0 || c.ClaimLease <= 0 ||
		timeout >= c.ClaimLease/time.Duration(c.BatchSize)-markTimeout {
		return fmt.Errorf("OUTBOX_CLAIM_LEASE must exceed OUTBOX_BATCH_SIZE * (handler timeout + %s outcome budget)", markTimeout)
	}
	return nil
}
