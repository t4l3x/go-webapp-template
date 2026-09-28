package ratelimit

import (
	"log/slog"

	"go.opentelemetry.io/otel/metric"
	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/platform/observability"
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

		func(limiter *RedisLimiter, provider metric.MeterProvider, logger *slog.Logger) (Limiter, error) {
			signal, err := observability.NewProtectionSignal(provider, logger.With("component", "ratelimit"), ProtectionEvent)
			if err != nil {
				return nil, err
			}

			return NewFailOpenLimiter(limiter, signal), nil
		},
	),
)

// assert the adapter satisfies the port it is bound to above.
var _ Limiter = (*RedisLimiter)(nil)
