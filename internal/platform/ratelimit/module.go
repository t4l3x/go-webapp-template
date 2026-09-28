package ratelimit

import (
	"log/slog"

	"go.uber.org/fx"
)

// Module wires the rate limiter. Include it in a process that serves
// traffic it needs to protect — currently only cmd/api. It requires
// platform/redis for the client NewRedisLimiter takes.
//
// The injected Limiter is the fail-open one: every consumer gets the
// project's failure policy by construction rather than by remembering
// to apply it. The bare *RedisLimiter stays constructible for tests
// that need to observe backend errors directly.
var Module = fx.Module(
	"ratelimit",

	fx.Provide(
		LoadConfig,

		NewRedisLimiter,

		func(limiter *RedisLimiter, logger *slog.Logger) Limiter {
			return NewFailOpenLimiter(limiter, logger)
		},
	),
)

// assert the adapter satisfies the port it is bound to above.
var _ Limiter = (*RedisLimiter)(nil)
