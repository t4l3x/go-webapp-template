package ratelimit

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds the platform-level rate limiting settings: the backend
// call budget, and the generic per-IP limit that covers the whole API.
//
// Endpoint-specific limits are deliberately absent. A module owns the
// policy for the endpoints it exposes (see identity's Config), so this
// package does not grow an environment variable every time a feature
// wants a limit — which is how a handful of knobs turns into hundreds.
type Config struct {
	// GlobalPerMinute is the generic per-IP request allowance across
	// the whole API. It is a blunt backstop against a single source
	// saturating the service, not a security control — sensitive
	// endpoints carry their own far stricter limits on top.
	//
	// The default is conservative and chosen without traffic data:
	// tune it from real percentiles per environment before it is
	// load-bearing. Set it high enough that a legitimate client using
	// the app normally never approaches it.
	GlobalPerMinute int `env:"RATELIMIT_GLOBAL_PER_MINUTE" envDefault:"300"`

	// GlobalBurst is how many requests a previously idle IP may make at
	// once. Zero means "a full minute's allowance", which suits a UI
	// that fires several requests when a page opens.
	GlobalBurst int `env:"RATELIMIT_GLOBAL_BURST" envDefault:"0"`

	// Timeout bounds a single limiter check. It is deliberately short:
	// this runs on every request, so a slow backend must degrade to
	// fail-open quickly rather than adding its latency to every
	// response.
	Timeout time.Duration `env:"RATELIMIT_TIMEOUT" envDefault:"200ms"`
}

func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("load ratelimit config: %w", err)
	}

	if cfg.GlobalBurst == 0 {
		cfg.GlobalBurst = cfg.GlobalPerMinute
	}

	if cfg.Timeout <= 0 {
		return Config{}, fmt.Errorf("RATELIMIT_TIMEOUT must be greater than zero")
	}

	// Validated here rather than on first use, so a misconfigured limit
	// fails startup with a message naming the variable.
	if err := cfg.GlobalPolicy().Validate(); err != nil {
		return Config{}, fmt.Errorf(
			"RATELIMIT_GLOBAL_PER_MINUTE/RATELIMIT_GLOBAL_BURST are not a usable policy: %w", err)
	}

	return cfg, nil
}

// GlobalPolicy is the generic per-IP limit applied to the whole API.
func (c Config) GlobalPolicy() Policy {
	return Policy{
		Rate:   c.GlobalPerMinute,
		Burst:  c.GlobalBurst,
		Period: time.Minute,
	}
}
