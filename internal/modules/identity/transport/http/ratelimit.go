package http

import (
	"net/http"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

// Scopes name identity's limits. They are the stable part of a Redis
// key and the only limiter identifier that ever reaches a log line, so
// they are low-cardinality and carry no subject.
const (
	scopeVerifyEmail        = "identity.verify_email"
	scopeResendVerification = "identity.resend_verification"
	scopeRegister           = "identity.register"
	scopeLoginIP            = "identity.login.ip"
	scopeRefresh            = "identity.refresh"
)

// RateLimitPolicies are identity's own limits for its own endpoints.
//
// They live here rather than in platform config because the platform
// owns rate-limiting mechanics while a module owns what its endpoints
// are worth: nobody outside identity can say what a reasonable
// registration rate is, and putting every feature's limits in one
// global config is how that config grows without bound.
//
// Every limit here is per-IP. An account-aware dimension (throttling a
// known identifier regardless of source) is deliberately not part of
// this shape: a blocking limiter keyed on the account and consulted
// before credentials are checked lets an attacker who merely knows a
// victim's address throttle that victim's own login attempts. If that
// protection is designed again, it needs to not be a hard block on an
// unauthenticated request.
//
// These are the per-IP layer only. Rules keyed by account (the resend
// cooldown and daily cap) live in the application, under the account's
// row lock, where a changed IP or a burst of concurrent requests can't
// get around them.
type RateLimitPolicies struct {
	Register           ratelimit.Policy
	LoginIP            ratelimit.Policy
	Refresh            ratelimit.Policy
	VerifyEmail        ratelimit.Policy
	ResendVerification ratelimit.Policy
}

// RateLimiter applies identity's policies to identity's endpoints.
//
// The generic per-IP limit in platform/httpserver still applies
// underneath; these are the much stricter allowances the sensitive
// endpoints get on top.
type RateLimiter struct {
	limiter   ratelimit.Limiter
	responder *response.Responder
	policies  RateLimitPolicies
}

func NewRateLimiter(
	limiter ratelimit.Limiter,
	responder *response.Responder,
	policies RateLimitPolicies,
) *RateLimiter {
	return &RateLimiter{
		limiter:   limiter,
		responder: responder,
		policies:  policies,
	}
}

// PerIP limits a scope by the client address the platform ClientIP
// middleware resolved for this request.
//
// Applied as route middleware, so a rejected request never reaches the
// handler — and on login, never reaches password verification. That
// ordering is the point: Argon2 verification is deliberately expensive,
// which makes an unthrottled login endpoint a way to spend the
// server's CPU rather than the caller's.
func (l *RateLimiter) PerIP(scope string, policy ratelimit.Policy) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := ratelimit.NewIPKey(scope, requestctx.ClientIP(r.Context()))

			result, err := l.limiter.Allow(r.Context(), key, policy)
			if err != nil || result.Allowed {
				next.ServeHTTP(w, r)

				return
			}

			middleware.WriteRateLimited(w, r, l.responder, result.RetryAfter)
		})
	}
}
