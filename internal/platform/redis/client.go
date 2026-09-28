// Package redis is the platform's Redis client capability: connection
// configuration, lifecycle, and nothing else.
//
// Redis here is ephemeral enforcement and coordination state — rate
// limiter counters today — never durable business data. Anything that
// must survive a Redis restart belongs in PostgreSQL.
//
// The client is available to platform and infrastructure adapters that
// genuinely need it (see platform/ratelimit). Application and domain
// code must never import this package or go-redis: a use case depends
// on a narrow port, not on a Redis connection.
package redis

import (
	"context"
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/fx"
)

func NewClient(cfg Config) (*goredis.Client, error) {
	opts, err := cfg.Options()
	if err != nil {
		return nil, err
	}

	return goredis.NewClient(opts), nil
}

// RegisterLifecycle checks connectivity on start and closes the pool on
// shutdown.
//
// A failed Ping logs a warning but does not fail startup. Redis here
// backs only the rate limiter, which already fails open when a call
// fails (see ratelimit.NewFailOpenLimiter) — refusing to boot the whole
// API because that one optional dependency is briefly unreachable would
// turn it into a hard requirement, which is a worse outcome than the
// throttling gap that opens while it stays down. go-redis's own pool
// reconnects on its own once Redis becomes reachable: ordinary calls
// just start succeeding again, with nothing here to retry or restart.
//
// This is a connectivity check, not a configuration check: a malformed
// REDIS_URL still fails the process, in LoadConfig/Options at config
// load time, before this ever runs.
func RegisterLifecycle(lifecycle fx.Lifecycle, client *goredis.Client, logger *slog.Logger) {
	redisLogger := logger.With("component", "redis")

	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := client.Ping(ctx).Err(); err != nil {
				redisLogger.Warn("redis unreachable at startup, continuing without it", "error", err)

				return nil
			}

			redisLogger.Info("redis connected")

			return nil
		},
		OnStop: func(context.Context) error {
			if err := client.Close(); err != nil {
				return fmt.Errorf("close redis: %w", err)
			}

			redisLogger.Info("redis closed")

			return nil
		},
	})
}
