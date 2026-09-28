package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis_rate/v10"
	goredis "github.com/redis/go-redis/v9"
)

// This file is the only place in the codebase that imports
// redis_rate. Everything else speaks Policy/Key/Result, so swapping
// the implementation is a change here and nowhere else.

// RedisLimiter enforces limits in Redis using redis_rate's GCRA
// implementation.
//
// The decision runs as a Lua script inside Redis, which is what makes
// it correct across API instances: read-modify-write in Go would let
// two processes both observe "one request left" and both allow it. The
// script is also where key expiry is set, so this package never
// manages TTLs itself and Redis does not accumulate permanent state
// for keys that stopped being used.
type RedisLimiter struct {
	limiter *redis_rate.Limiter
	timeout time.Duration
}

func NewRedisLimiter(client *goredis.Client, cfg Config) *RedisLimiter {
	return &RedisLimiter{
		limiter: redis_rate.NewLimiter(client),
		timeout: cfg.Timeout,
	}
}

// Allow implements Limiter.
func (l *RedisLimiter) Allow(ctx context.Context, key Key, policy Policy) (Result, error) {
	// Validated per call as well as at startup: a policy computed at
	// request time (from a future per-tenant setting, say) would
	// otherwise reach the Lua script, where Rate == 0 is a division by
	// zero inside Redis rather than a clear Go error.
	if err := policy.Validate(); err != nil {
		return Result{}, err
	}

	// A limiter must never be the reason a request hangs. The client's
	// own read timeout is a backstop, but this bounds the whole call —
	// including time spent waiting for a pooled connection — at a value
	// chosen for "a check on the request path", not for general Redis
	// use.
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	result, err := l.limiter.Allow(ctx, key.String(), redis_rate.Limit{
		Rate:   policy.Rate,
		Burst:  policy.Burst,
		Period: policy.Period,
	})
	if err != nil {
		// Names the scope but never the key: the subject is an IP or an
		// account hash, and this error is logged on the request path.
		return Result{}, fmt.Errorf("ratelimit: redis allow for scope %q: %w", key.Scope(), err)
	}

	return fromRedisRate(result), nil
}

// fromRedisRate translates the library's result into ours.
func fromRedisRate(result *redis_rate.Result) Result {
	// The library reports Allowed as a count of permitted events, and
	// uses RetryAfter == -1 as its "not limited" sentinel rather than
	// zero. Both are normalized here so no caller has to know either
	// convention — a -1 duration leaking into a Retry-After header
	// would be a malformed response.
	retryAfter := result.RetryAfter
	if retryAfter < 0 {
		retryAfter = 0
	}

	resetAfter := result.ResetAfter
	if resetAfter < 0 {
		resetAfter = 0
	}

	return Result{
		Allowed:    result.Allowed > 0,
		Remaining:  result.Remaining,
		RetryAfter: retryAfter,
		ResetAfter: resetAfter,
	}
}
