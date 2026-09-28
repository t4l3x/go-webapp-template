package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestRateLimit_AllowedRequestReachesHandler(t *testing.T) {
	limiter := &recordingLimiter{result: ratelimit.Result{Allowed: true, Remaining: 5}}

	rec, reached := serveRateLimited(t, limiter, nil, newRequest("198.51.100.7:1234", "/api/v1/resource"))

	if !reached {
		t.Fatalf("handler was not reached for an allowed request")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRateLimit_DeniedRequestReturns429(t *testing.T) {
	limiter := &recordingLimiter{
		result: ratelimit.Result{Allowed: false, RetryAfter: 30 * time.Second},
	}

	rec, reached := serveRateLimited(t, limiter, nil, newRequest("198.51.100.7:1234", "/api/v1/resource"))

	if reached {
		t.Fatalf("handler ran for a denied request — the router must not execute denied traffic")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimit_DeniedRequestUsesStableErrorEnvelope(t *testing.T) {
	limiter := &recordingLimiter{result: ratelimit.Result{Allowed: false, RetryAfter: time.Second}}

	rec, _ := serveRateLimited(t, limiter, nil, newRequest("198.51.100.7:1234", "/api/v1/resource"))

	var body response.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body.Error.Code != "rate_limit_exceeded" {
		t.Fatalf("code = %q, want %q", body.Error.Code, "rate_limit_exceeded")
	}
	if body.Error.Message == "" {
		t.Fatalf("message is empty")
	}
}

// TestRateLimit_RetryAfterRoundsUp guards against telling a client to
// come back sooner than its allowance exists. A client that trusts the
// header and retries exactly on time would otherwise be rejected again,
// and a client that loops on it would hammer the endpoint.
func TestRateLimit_RetryAfterRoundsUp(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter time.Duration
		want       string
	}{
		{"whole_second", 30 * time.Second, "30"},
		{"fraction_rounds_up", 1200 * time.Millisecond, "2"},
		{"sub_second_becomes_one", 200 * time.Millisecond, "1"},
		{"zero_becomes_one", 0, "1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			limiter := &recordingLimiter{
				result: ratelimit.Result{Allowed: false, RetryAfter: tc.retryAfter},
			}

			rec, _ := serveRateLimited(t, limiter, nil, newRequest("198.51.100.7:1234", "/api/v1/resource"))

			if got := rec.Header().Get("Retry-After"); got != tc.want {
				t.Fatalf("Retry-After = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRateLimit_BackendFailureFailsOpen documents the chosen policy: an
// unreachable limiter must not become a way to reject all traffic.
func TestRateLimit_BackendFailureFailsOpen(t *testing.T) {
	limiter := &recordingLimiter{err: errors.New("redis: connection refused")}

	rec, reached := serveRateLimited(t, limiter, nil, newRequest("198.51.100.7:1234", "/api/v1/resource"))

	if !reached {
		t.Fatalf("handler was not reached — the limiter must fail open")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestRateLimit_ExemptPathsSkipTheLimiter covers the operational
// endpoints. Rate-limiting a liveness probe is an outage mode: the
// probe source would start receiving 429s, which reads as an unhealthy
// process and gets a healthy one restarted.
func TestRateLimit_ExemptPathsSkipTheLimiter(t *testing.T) {
	limiter := &recordingLimiter{result: ratelimit.Result{Allowed: false, RetryAfter: time.Minute}}

	rec, reached := serveRateLimited(t, limiter, []string{"/health"}, newRequest("198.51.100.7:1234", "/health"))

	if !reached {
		t.Fatalf("an exempt path was rate limited")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(limiter.keys) != 0 {
		t.Fatalf("the limiter was consulted for an exempt path: %v", limiter.keys)
	}
}

// TestRateLimit_UsesResolvedClientIP is the security property that makes
// IP limiting worth anything: the key must come from the trusted-proxy
// resolver, never from a header the caller controls. Here the peer is
// untrusted, so its X-Forwarded-For must be ignored and both requests
// must land in the same bucket.
func TestRateLimit_UsesResolvedClientIP(t *testing.T) {
	limiter := &recordingLimiter{result: ratelimit.Result{Allowed: true}}

	first := newRequest("198.51.100.7:1234", "/api/v1/resource")
	first.Header.Set("X-Forwarded-For", "203.0.113.1")

	second := newRequest("198.51.100.7:1234", "/api/v1/resource")
	second.Header.Set("X-Forwarded-For", "203.0.113.2")

	serveRateLimited(t, limiter, nil, first)
	serveRateLimited(t, limiter, nil, second)

	if len(limiter.keys) != 2 {
		t.Fatalf("limiter consulted %d times, want 2", len(limiter.keys))
	}
	if limiter.keys[0] != limiter.keys[1] {
		t.Fatalf("a spoofed X-Forwarded-For changed the bucket: %q vs %q", limiter.keys[0], limiter.keys[1])
	}
	if !strings.Contains(limiter.keys[0], "198.51.100.7") {
		t.Fatalf("key = %q, want it keyed on the verified peer address", limiter.keys[0])
	}
	if strings.Contains(limiter.keys[0], "203.0.113") {
		t.Fatalf("key = %q, want the forwarded header ignored for an untrusted peer", limiter.keys[0])
	}
}

// TestRateLimit_TrustedProxyForwardedIPIsHonored is the converse: behind
// a configured proxy the real client must get its own bucket, or every
// request through the proxy would share one.
func TestRateLimit_TrustedProxyForwardedIPIsHonored(t *testing.T) {
	limiter := &recordingLimiter{result: ratelimit.Result{Allowed: true}}
	resolver := clientip.NewResolver([]netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")})

	first := newRequest("198.51.100.7:1234", "/api/v1/resource")
	first.Header.Set("X-Forwarded-For", "203.0.113.1")

	second := newRequest("198.51.100.7:1234", "/api/v1/resource")
	second.Header.Set("X-Forwarded-For", "203.0.113.2")

	serveWithResolver(t, limiter, resolver, nil, first)
	serveWithResolver(t, limiter, resolver, nil, second)

	if limiter.keys[0] == limiter.keys[1] {
		t.Fatalf("distinct clients behind a trusted proxy share a bucket: %q", limiter.keys[0])
	}
	if !strings.Contains(limiter.keys[0], "203.0.113.1") {
		t.Fatalf("key = %q, want the forwarded client address", limiter.keys[0])
	}
}

func newRequest(remoteAddr, path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr

	return req
}

func serveRateLimited(
	t *testing.T,
	limiter ratelimit.Limiter,
	exempt []string,
	req *http.Request,
) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	return serveWithResolver(t, limiter, clientip.NewResolver(nil), exempt, req)
}

func serveWithResolver(
	t *testing.T,
	limiter ratelimit.Limiter,
	resolver *clientip.Resolver,
	exempt []string,
	req *http.Request,
) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	logger, _ := testkit.NewLogger()

	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.RateLimit(
		limiter,
		resolver,
		response.NewResponder(logger),
		ratelimit.PerMinute(10),
		exempt,
	)(next)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec, reached
}

// recordingLimiter returns a fixed decision and records the keys it was
// asked about, so tests can assert which bucket a request landed in.
type recordingLimiter struct {
	result ratelimit.Result
	err    error
	keys   []string
}

func (l *recordingLimiter) Allow(_ context.Context, key ratelimit.Key, _ ratelimit.Policy) (ratelimit.Result, error) {
	l.keys = append(l.keys, key.String())

	return l.result, l.err
}
