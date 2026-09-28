package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	identityhttp "github.com/t4l3x/go-webapp-template/internal/modules/identity/transport/http"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

// testPolicies gives every endpoint a distinct rate, so a route wired to
// the wrong policy is caught rather than hidden by equal values.
func testPolicies() identityhttp.RateLimitPolicies {
	return identityhttp.RateLimitPolicies{
		Register:           ratelimit.PerMinute(5),
		LoginIP:            ratelimit.PerMinute(15),
		Refresh:            ratelimit.PerMinute(30),
		VerifyEmail:        ratelimit.PerMinute(10),
		ResendVerification: ratelimit.PerMinute(4),
	}
}

// TestRoutes_SensitiveEndpointsAreRateLimited walks the real route
// table, so an endpoint added later without a limit shows up here
// rather than in production.
func TestRoutes_SensitiveEndpointsAreRateLimited(t *testing.T) {
	handler, _, _ := newTestHandler(t)
	responder := mustResponder(t)
	policies := testPolicies()

	limiter := &countingLimiter{result: ratelimit.Result{Allowed: false, RetryAfter: 20 * time.Second}}

	routes, err := identityhttp.NewRoutes(
		handler,
		identityhttp.NewAuthMiddleware(newFakeTokenManager(), responder),
		identityhttp.NewRateLimiter(limiter, responder, policies),
		policies,
	)
	if err != nil {
		t.Fatalf("NewRoutes() error = %v", err)
	}

	tests := []struct {
		path       string
		wantScope  string
		wantPolicy ratelimit.Policy
	}{
		{"/api/v1/auth/register", "identity.register", policies.Register},
		{"/api/v1/auth/login", "identity.login.ip", policies.LoginIP},
		{"/api/v1/auth/refresh", "identity.refresh", policies.Refresh},
		{"/api/v1/auth/verify-email", "identity.verify_email", policies.VerifyEmail},
		{"/api/v1/auth/resend-verification", "identity.resend_verification", policies.ResendVerification},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			limiter.keys, limiter.policies = nil, nil

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
			if len(limiter.policies) == 0 || limiter.policies[0] != tc.wantPolicy {
				t.Fatalf("policy = %+v, want %+v", limiter.policies, tc.wantPolicy)
			}
			if rec.Header().Get("Retry-After") != "20" {
				t.Fatalf("Retry-After = %q, want %q", rec.Header().Get("Retry-After"), "20")
			}
		})
	}
}

// TestRoutes_LoginIPLimitRejectsBeforePasswordHashing pins the ordering
// that makes the login limit worth having: a denied request never
// reaches the use case, so it costs no Argon2 work.
func TestRoutes_LoginIPLimitRejectsBeforePasswordHashing(t *testing.T) {
	responder := mustResponder(t)
	users := newFakeUserRepository()
	mustRegisterUser(t, users, "user@example.com", "supersecretpassword")
	hasher := &countingHasher{}

	login := mustLogin(application.NewLoginService(users, newFakeSessionRepository(), hasher, newFakeTokenManager(),
		allowAllRisk{}, discardEvents{}, application.SessionConfig{RefreshTokenTTL: time.Hour}))
	handler := identityhttp.NewHandler(nil, login, nil, nil, nil, nil, nil, responder)

	limiter := &countingLimiter{result: ratelimit.Result{Allowed: false, RetryAfter: time.Second}}
	routes, err := identityhttp.NewRoutes(handler, identityhttp.NewAuthMiddleware(newFakeTokenManager(), responder),
		identityhttp.NewRateLimiter(limiter, responder, testPolicies()), testPolicies())
	if err != nil {
		t.Fatalf("NewRoutes() error = %v", err)
	}

	rec := httptest.NewRecorder()
	req := newJSONRequest(http.MethodPost, "/api/v1/auth/login", `{"email":"user@example.com","password":"supersecretpassword"}`)
	findRoute(t, routes, http.MethodPost, "/api/v1/auth/login").Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if n := hasher.verifies.Load(); n != 0 {
		t.Fatalf("password verified %d times for a rate-limited request, want 0", n)
	}
}

func TestRateLimiter_PerIP(t *testing.T) {
	tests := []struct {
		name        string
		limiter     *countingLimiter
		wantReached bool
		wantStatus  int
		wantRetry   string
	}{
		{"allowed", &countingLimiter{result: ratelimit.Result{Allowed: true}}, true, http.StatusOK, ""},
		{"denied never reaches the handler", &countingLimiter{result: ratelimit.Result{RetryAfter: 1500 * time.Millisecond}}, false, http.StatusTooManyRequests, "2"},
		{"limiter failure fails open", &countingLimiter{err: errors.New("redis down")}, true, http.StatusOK, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			})

			rl := identityhttp.NewRateLimiter(tc.limiter, mustResponder(t), testPolicies())
			rec := httptest.NewRecorder()
			rl.PerIP("identity.test", ratelimit.PerMinute(1))(next).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

			if reached != tc.wantReached {
				t.Fatalf("handler reached = %v, want %v", reached, tc.wantReached)
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("Retry-After"); got != tc.wantRetry {
				t.Fatalf("Retry-After = %q, want %q", got, tc.wantRetry)
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

// countingLimiter returns a fixed decision and records the keys and
// policies it was asked about.
type countingLimiter struct {
	result   ratelimit.Result
	err      error
	keys     []string
	policies []ratelimit.Policy
}

func (l *countingLimiter) Allow(_ context.Context, key ratelimit.Key, policy ratelimit.Policy) (ratelimit.Result, error) {
	l.keys = append(l.keys, key.String())
	l.policies = append(l.policies, policy)

	return l.result, l.err
}
