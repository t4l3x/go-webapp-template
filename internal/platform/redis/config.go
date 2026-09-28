package redis

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
	goredis "github.com/redis/go-redis/v9"
)

// Timeout defaults. go-redis leaves some of these effectively unbounded
// or generous by default; every one is set explicitly here because a
// Redis call that can block indefinitely turns a cache/limiter into a
// way to hang request handling. Whatever Redis is used for, waiting
// forever is never the right answer.
const (
	defaultDialTimeout  = 3 * time.Second
	defaultReadTimeout  = 1 * time.Second
	defaultWriteTimeout = 1 * time.Second
	defaultPoolTimeout  = 2 * time.Second
)

type Config struct {
	// URL is the single canonical connection setting, matching this
	// project's one-variable-per-dependency rule (DB_DSN, REDIS_URL) —
	// no separate host/port/db/password variables to keep in sync.
	//
	// go-redis's own URL parser also accepts tuning as query
	// parameters, so an operator can override any of the timeouts below
	// without this package growing an environment variable per knob:
	//
	//	redis://localhost:6379/0?read_timeout=2s&pool_size=20
	URL string `env:"REDIS_URL" envDefault:"redis://localhost:6379/0"`
}

// LoadConfig parses REDIS_URL and applies timeout defaults for anything
// the URL did not set explicitly.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("load redis config: %w", err)
	}

	// Parsed at startup so a malformed URL fails the process rather
	// than the first request that needs Redis. This also covers an
	// empty value, which ParseURL rejects for having no scheme.
	if _, err := cfg.Options(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Options turns the configured URL into go-redis client options, with
// this project's timeout defaults filled in where the URL was silent.
func (c Config) Options() (*goredis.Options, error) {
	opts, err := goredis.ParseURL(c.URL)
	if err != nil {
		// Deliberately does not include the URL: it may carry a
		// password. The parser's own message describes the problem
		// without needing the value echoed back.
		return nil, fmt.Errorf("REDIS_URL is not a valid Redis URL: %w", err)
	}

	if opts.DialTimeout == 0 {
		opts.DialTimeout = defaultDialTimeout
	}

	if opts.ReadTimeout == 0 {
		opts.ReadTimeout = defaultReadTimeout
	}

	if opts.WriteTimeout == 0 {
		opts.WriteTimeout = defaultWriteTimeout
	}

	if opts.PoolTimeout == 0 {
		opts.PoolTimeout = defaultPoolTimeout
	}

	return opts, nil
}
