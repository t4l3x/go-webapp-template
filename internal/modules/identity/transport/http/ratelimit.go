package http

import (
	"net/http"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
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
type RateLimitPolicies struct {
	Register ratelimit.Policy
	LoginIP  ratelimit.Policy
	Refresh  ratelimit.Policy
}

// RateLimiter applies identity's policies to identity's endpoints.
//
// The generic per-IP limit in platform/httpserver still applies
// underneath; these are the much stricter allowances the sensitive
// endpoints get on top.
type RateLimiter struct {
	limiter   ratelimit.Limiter
	clientIP  *clientip.Resolver
	responder *response.Responder
	policies  RateLimitPolicies
}

func NewRateLimiter(
	limiter ratelimit.Limiter,
	clientIP *clientip.Resolver,
	responder *response.Responder,
	policies RateLimitPolicies,
) *RateLimiter {
	return &RateLimiter{
		limiter:   limiter,
		clientIP:  clientIP,
		responder: responder,
		policies:  policies,
	}
}

// PerIP limits a scope by resolved client address.
//
// Applied as route middleware, so a rejected request never reaches the
// handler — and on login, never reaches password verification. That
// ordering is the point: Argon2 verification is deliberately expensive,
// which makes an unthrottled login endpoint a way to spend the
// server's CPU rather than the caller's.
func (l *RateLimiter) PerIP(scope string, policy ratelimit.Policy) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := ratelimit.NewIPKey(scope, l.clientIP.ClientIP(r))

			result, err := l.limiter.Allow(r.Context(), key, policy)
			if err != nil || result.Allowed {
				next.ServeHTTP(w, r)

				return
			}

			middleware.WriteRateLimited(w, r, l.responder, result.RetryAfter)
		})
	}
}
