package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identityhttp "github.com/t4l3x/go-webapp-template/internal/modules/identity/transport/http"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

func testPolicies() identityhttp.RateLimitPolicies {
	return identityhttp.RateLimitPolicies{
		Register: ratelimit.PerMinute(5),
		LoginIP:  ratelimit.PerMinute(15),
		Refresh:  ratelimit.PerMinute(30),
	}
}

func testRateLimiter(limiter ratelimit.Limiter, responder *response.Responder) *identityhttp.RateLimiter {
	return identityhttp.NewRateLimiter(limiter, responder, testPolicies())
}

// TestRoutes_SensitiveEndpointsAreRateLimited walks the real route
// table, so an endpoint added later without a limit shows up here
// rather than in production.
func TestRoutes_SensitiveEndpointsAreRateLimited(t *testing.T) {
	handler, _, _ := newTestHandler(t)
	responder := mustResponder(t)

	limiter := &countingLimiter{result: ratelimit.Result{Allowed: false, RetryAfter: 20 * time.Second}}

	routes, err := identityhttp.NewRoutes(
		handler,
		identityhttp.NewAuthMiddleware(newFakeTokenManager(), responder),
		testRateLimiter(limiter, responder),
		testPolicies(),
	)
	if err != nil {
		t.Fatalf("NewRoutes() error = %v", err)
	}

	tests := []struct {
		path      string
		wantScope string
	}{
		{"/api/v1/auth/register", "identity.register"},
		{"/api/v1/auth/login", "identity.login.ip"},
		{"/api/v1/auth/refresh", "identity.refresh"},
		{"/api/v1/auth/verify-email", "identity.verify_email"},
		{"/api/v1/auth/resend-verification", "identity.resend_verification"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			limiter.keys = nil

			route := findRoute(t, routes, http.MethodPost, tc.path)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{}`))
			req.RemoteAddr = "198.51.100.7:1234"
			req.Header.Set("X-Forwarded-For", "192.0.2.1") // untrusted peer: must be ignored
			req.Header.Set("Authorization", "Bearer valid-access-token")

			// The router resolves the client IP once, before any route
			// middleware; reproduce that here.
			middleware.ClientIP(clientip.NewResolver(nil))(route.Handler).ServeHTTP(rec, req)

			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
			}
			if want := tc.wantScope + ":ip:198.51.100.7"; len(limiter.keys) == 0 || limiter.keys[0] != want {
				t.Fatalf("keys = %v, want the first to be %q", limiter.keys, want)
			}
			if rec.Header().Get("Retry-After") != "20" {
				t.Fatalf("Retry-After = %q, want %q", rec.Header().Get("Retry-After"), "20")
			}
		})
	}
}

func findRoute(t *testing.T, routes []httpserver.Route, method, path string) httpserver.Route {
	t.Helper()

	for _, route := range routes {
		if route.Method == method && route.Path == path {
			return route
		}
	}

	t.Fatalf("route %s %s not registered", method, path)

	return httpserver.Route{}
}

// countingLimiter returns a fixed decision and records the keys it saw.
type countingLimiter struct {
	result ratelimit.Result
	err    error
	keys   []string
}

func (l *countingLimiter) Allow(_ context.Context, key ratelimit.Key, _ ratelimit.Policy) (ratelimit.Result, error) {
	l.keys = append(l.keys, key.String())

	return l.result, l.err
}
