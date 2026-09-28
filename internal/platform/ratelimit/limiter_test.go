package ratelimit_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

var errBackend = errors.New("redis: connection refused")

// TestFailOpenLimiter_BackendFailureAllowsRequest pins the project's
// failure policy: a rate limiter is protective, not an authorization
// boundary, so an unreachable Redis must not become a way to reject all
// traffic.
func TestFailOpenLimiter_BackendFailureAllowsRequest(t *testing.T) {
	logger, _ := testkit.NewLogger()
	limiter := ratelimit.NewFailOpenLimiter(&stubLimiter{err: errBackend}, logger)

	result, err := limiter.Allow(context.Background(), testKey(), ratelimit.PerMinute(10))
	if err != nil {
		t.Fatalf("Allow() error = %v, want the failure absorbed", err)
	}
	if !result.Allowed {
		t.Fatalf("Allowed = false, want the request allowed when the backend is down")
	}
}

// TestFailOpenLimiter_BackendFailureIsLogged is the other half of the
// policy: failing open must never be silent, or a dead limiter looks
// exactly like a working one.
func TestFailOpenLimiter_BackendFailureIsLogged(t *testing.T) {
	logger, logs := testkit.NewLogger()
	limiter := ratelimit.NewFailOpenLimiter(&stubLimiter{err: errBackend}, logger)

	if _, err := limiter.Allow(context.Background(), testKey(), ratelimit.PerMinute(10)); err != nil {
		t.Fatalf("Allow() error = %v", err)
	}

	logged := logs.String()

	if !strings.Contains(logged, "rate limiter backend unavailable") {
		t.Fatalf("logs do not record the limiter failure: %s", logged)
	}
	if !strings.Contains(logged, "identity.login") {
		t.Fatalf("logs do not record the scope: %s", logged)
	}
}

// TestFailOpenLimiter_DoesNotLogSubject guards against putting a
// per-client identifier into log aggregation on every request. The
// scope is low-cardinality and safe; the subject is not.
func TestFailOpenLimiter_DoesNotLogSubject(t *testing.T) {
	logger, logs := testkit.NewLogger()
	limiter := ratelimit.NewFailOpenLimiter(&stubLimiter{err: errBackend}, logger)

	if _, err := limiter.Allow(context.Background(), testKey(), ratelimit.PerMinute(10)); err != nil {
		t.Fatalf("Allow() error = %v", err)
	}

	logged := logs.String()

	if strings.Contains(logged, "198.51.100.7") {
		t.Fatalf("logs contain the key's subject: %s", logged)
	}
}

func TestFailOpenLimiter_PassesThroughDecisions(t *testing.T) {
	logger, logs := testkit.NewLogger()

	denied := ratelimit.Result{Allowed: false, Remaining: 0}
	limiter := ratelimit.NewFailOpenLimiter(&stubLimiter{result: denied}, logger)

	result, err := limiter.Allow(context.Background(), testKey(), ratelimit.PerMinute(10))
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if result.Allowed {
		t.Fatalf("Allowed = true, want a healthy backend's denial respected")
	}
	if logs.Len() != 0 {
		t.Fatalf("a normal denial was logged as a backend failure: %s", logs.String())
	}
}

func testKey() ratelimit.Key {
	return ratelimit.NewIPKey("identity.login.ip", netip.MustParseAddr("198.51.100.7"))
}

type stubLimiter struct {
	result ratelimit.Result
	err    error
}

func (l *stubLimiter) Allow(context.Context, ratelimit.Key, ratelimit.Policy) (ratelimit.Result, error) {
	return l.result, l.err
}
