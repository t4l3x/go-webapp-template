package middleware

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

// globalScope names the generic per-IP limit in Redis keys and logs.
const globalScope = "http.global"

// ErrRateLimited is the response every rate-limited path returns. The
// code is stable and says nothing about which limit was hit or how much
// allowance remains: a caller that could tell "IP limit" from "account
// limit" could use the difference to probe for account existence.
var ErrRateLimited = apperror.New(
	apperror.KindTooManyRequests,
	"rate_limit_exceeded",
	"Too many requests",
)

// RateLimit applies a generic per-IP allowance to every request it
// wraps.
//
// Its place in the chain is deliberate (see httpserver.NewRouter):
// inside ClientIP, which resolves the address it keys on; inside
// RequestID and AccessLog, so a rejected request is still
// logged and correlatable; inside Recovery, so a panic in here is
// caught like any other; inside CORS, so a 429 carries the CORS
// headers a browser needs to read it, and so preflight OPTIONS — which
// CORS answers itself — never spends a caller's allowance. It is the
// innermost middleware, so the router never runs for a denied request.
//
// exempt paths are checked before the limiter. Operational endpoints
// belong there: an orchestrator probing liveness from one address would
// otherwise exhaust that address's allowance and start receiving 429s,
// which reads as an unhealthy process and gets a healthy one restarted.
func RateLimit(
	limiter ratelimit.Limiter,
	responder *response.Responder,
	policy ratelimit.Policy,
	exempt []string,
) Middleware {
	exemptPaths := make(map[string]struct{}, len(exempt))
	for _, path := range exempt {
		exemptPaths[path] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, skip := exemptPaths[r.URL.Path]; skip {
				next.ServeHTTP(w, r)

				return
			}

			// Resolved once by the ClientIP middleware through the
			// trusted-proxy policy, never read from a forwarded header
			// here: a caller that could choose its own IP could choose an
			// unused bucket per request.
			key := ratelimit.NewIPKey(globalScope, requestctx.ClientIP(r.Context()))

			result, err := limiter.Allow(r.Context(), key, policy)
			if err != nil || result.Allowed {
				next.ServeHTTP(w, r)

				return
			}

			WriteRateLimited(w, r, responder, result.RetryAfter)
		})
	}
}

// WriteRateLimited sends the shared 429 response, including a
// Retry-After header derived from the limiter.
//
// It is exported because module-specific limits (identity's login and
// registration policies) must produce a response indistinguishable from
// this one — a different body or a missing header would tell a caller
// which limit it hit.
func WriteRateLimited(
	w http.ResponseWriter,
	r *http.Request,
	responder *response.Responder,
	retryAfter time.Duration,
) {
	w.Header().Set("Retry-After", retryAfterSeconds(retryAfter))

	responder.Error(w, r, ErrRateLimited)
}

// retryAfterSeconds renders a duration as the integer seconds RFC 9110
// requires.
//
// Rounded up, always. Rounding 1.2s down to 1 would invite the client
// back before its allowance exists, so a well-behaved client retrying
// exactly when told would be rejected again — and a client that trusts
// the header would loop. A sub-second wait becomes 1 rather than 0 for
// the same reason: 0 means "retry immediately", which is never true of
// a request that was just refused.
func retryAfterSeconds(retryAfter time.Duration) string {
	seconds := int64(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}

	return strconv.FormatInt(seconds, 10)
}
