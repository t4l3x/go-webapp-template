// Package ratelimit is the platform's distributed rate-limiting
// capability. It owns the mechanics — key naming, policy validation,
// the backend, the failure strategy — while each business module owns
// the policy for the endpoints it exposes.
//
// The backend is Redis, so a limit is shared across every API process
// rather than being per-instance. The counter state is ephemeral
// enforcement state with backend-managed expiry: it is never written to
// PostgreSQL, and losing it entirely means limits reset, not that
// business data is lost.
//
// Nothing outside this package sees the backend library's types. That
// is the point of the Policy/Result/Key shapes here — replacing the
// implementation should not reach a single business module.
//
// This is rate limiting, not account lockout. Limits throttle: a
// blocked caller is delayed and recovers continuously. There is
// deliberately no persistent "this account is locked for an hour"
// state, because an attacker who can trigger it can deny service to
// any user they can name.
package ratelimit

import (
	"context"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/platform/observability"
)

// Result is the outcome of one limiter check.
type Result struct {
	// Allowed reports whether the request may proceed.
	Allowed bool

	// Remaining is how many further requests this key could make
	// immediately.
	Remaining int

	// RetryAfter is how long until the request would be permitted.
	// Zero when Allowed.
	RetryAfter time.Duration

	// ResetAfter is how long until the key returns to its full
	// allowance.
	ResetAfter time.Duration
}

// Limiter decides whether one more request against a key is permitted.
//
// The Limiter injected by this package's Module never returns an error
// for a backend failure — it fails open (see NewFailOpenLimiter). The
// error return remains because an implementation may reject a caller
// mistake, such as an invalid policy, which is a bug rather than an
// outage.
type Limiter interface {
	Allow(ctx context.Context, key Key, policy Policy) (Result, error)
}

// failOpenLimiter allows requests through when the backend fails.
//
// This is the whole of the project's failure policy, in one place
// rather than repeated at each call site — so it is one decision that
// can be reviewed and changed, not a pattern that might be applied
// inconsistently.
//
// Fail open, because a rate limiter is a protective measure, not an
// authorization boundary: making Redis able to reject all traffic would
// turn an optional dependency into a global single point of failure,
// which is a worse and much more likely outcome than the abuse window
// that opens while Redis is down. Anything that must hold even with
// Redis unavailable belongs in an authorization check, not here.
//
// It is not silent: every failure increments the
// rate_limit_protection_unavailable counter, and the outage is logged on
// transition, as a periodic summary, and on recovery (see
// observability.ProtectionSignal) — never once per request, which during
// an attack would be a log storm. Logs carry the scope, never the subject.
type failOpenLimiter struct {
	inner  Limiter
	signal *observability.ProtectionSignal
}

// ProtectionEvent names the counter and log event for limiter outages.
const ProtectionEvent = "rate_limit_protection_unavailable"

// NewFailOpenLimiter wraps a limiter so backend failures allow the
// request instead of rejecting it, reported through signal.
func NewFailOpenLimiter(inner Limiter, signal *observability.ProtectionSignal) Limiter {
	return &failOpenLimiter{inner: inner, signal: signal}
}

func (l *failOpenLimiter) Allow(ctx context.Context, key Key, policy Policy) (Result, error) {
	result, err := l.inner.Allow(ctx, key, policy)
	if err != nil {
		l.signal.Failed(ctx, key.Scope(), err)

		return Result{Allowed: true}, nil
	}

	l.signal.Succeeded()

	return result, nil
}
