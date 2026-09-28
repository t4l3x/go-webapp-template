package httpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/config"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestNewRouter_Health(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	handler, err := httpserver.NewRouter(
		httpserver.RouterParams{},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body[status] = %q, want %q", body["status"], "ok")
	}

	if got := rec.Header().Get(middleware.RequestIDHeader); got == "" {
		t.Fatalf("expected request id header to be set by the middleware chain")
	}
}

func TestNewRouter_MountsRegisteredRoutes(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	custom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	handler, err := httpserver.NewRouter(
		httpserver.RouterParams{
			Routes: []httpserver.Route{
				httpserver.GET("/example", custom),
			},
		},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/example", nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}

// TestNewRouter_MountsPrefixedRoutesAtFinalPath checks the end-to-end
// result of route prefixing: a route declared relative to a mount point
// is reachable at the joined URL, and only there.
func TestNewRouter_MountsPrefixedRoutesAtFinalPath(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	custom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	routes, err := httpserver.Prefix("/api/v1", httpserver.GET("/example", custom))
	if err != nil {
		t.Fatalf("Prefix() error = %v", err)
	}

	handler, err := httpserver.NewRouter(
		httpserver.RouterParams{Routes: routes},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/example", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("/api/v1/example: status = %d, want %d", rec.Code, http.StatusTeapot)
	}

	unprefixed := httptest.NewRecorder()
	handler.ServeHTTP(unprefixed, httptest.NewRequest(http.MethodGet, "/example", nil))

	if unprefixed.Code != http.StatusNotFound {
		t.Fatalf("/example: status = %d, want %d — the unprefixed path must not be served",
			unprefixed.Code, http.StatusNotFound)
	}
}

// TestNewRouter_HealthIsNotVersioned pins the operational-endpoint half
// of the versioning policy: /health is registered by the router itself,
// never through a route group, so it exists only at its unversioned
// path and does not move when the API's major version does.
func TestNewRouter_HealthIsNotVersioned(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	handler, err := httpserver.NewRouter(
		httpserver.RouterParams{},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("/api/v1/health: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestNewRouter_DuplicateRouteRejectedAfterPrefixing covers the case
// prefixing newly makes possible: two groups whose paths differ only by
// mount point are fine, but two routes that collide once joined must
// still be caught.
func TestNewRouter_DuplicateRouteRejectedAfterPrefixing(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	custom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	first, err := httpserver.Prefix("/api/v1", httpserver.GET("/example", custom))
	if err != nil {
		t.Fatalf("Prefix() error = %v", err)
	}

	second, err := httpserver.Prefix("/api/v1", httpserver.GET("/example", custom))
	if err != nil {
		t.Fatalf("Prefix() error = %v", err)
	}

	_, err = httpserver.NewRouter(
		httpserver.RouterParams{Routes: append(first, second...)},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err == nil {
		t.Fatalf("NewRouter() error = nil, want error for routes colliding after prefixing")
	}
}

// TestNewRouter_SamePathUnderDifferentPrefixesAllowed is the converse:
// the same relative path mounted under two different major versions is
// exactly what a future v2 looks like, and must register cleanly.
func TestNewRouter_SamePathUnderDifferentPrefixesAllowed(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	custom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	v1, err := httpserver.Prefix("/api/v1", httpserver.GET("/example", custom))
	if err != nil {
		t.Fatalf("Prefix() error = %v", err)
	}

	v2, err := httpserver.Prefix("/api/v2", httpserver.GET("/example", custom))
	if err != nil {
		t.Fatalf("Prefix() error = %v", err)
	}

	handler, err := httpserver.NewRouter(
		httpserver.RouterParams{Routes: append(v1, v2...)},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	for _, path := range []string{"/api/v1/example", "/api/v2/example"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("path %s: status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

func TestNewRouter_DuplicateRouteRejected(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	custom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	_, err := httpserver.NewRouter(
		httpserver.RouterParams{
			Routes: []httpserver.Route{
				httpserver.GET("/example", custom),
				httpserver.GET("/example", custom),
			},
		},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err == nil {
		t.Fatalf("NewRouter() error = nil, want error for duplicate route")
	}
}

func TestNewRouter_InvalidMethodRejected(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	custom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	_, err := httpserver.NewRouter(
		httpserver.RouterParams{
			Routes: []httpserver.Route{
				{Method: "TRACE", Path: "/example", Handler: custom},
			},
		},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err == nil {
		t.Fatalf("NewRouter() error = nil, want error for unsupported method")
	}
}

func TestNewRouter_InvalidPathRejected(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	custom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	_, err := httpserver.NewRouter(
		httpserver.RouterParams{
			Routes: []httpserver.Route{
				httpserver.GET("example", custom),
			},
		},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err == nil {
		t.Fatalf("NewRouter() error = nil, want error for path missing leading slash")
	}
}

func TestNewRouter_NilHandlerRejected(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	_, err := httpserver.NewRouter(
		httpserver.RouterParams{
			Routes: []httpserver.Route{
				{Method: http.MethodGet, Path: "/example", Handler: nil},
			},
		},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err == nil {
		t.Fatalf("NewRouter() error = nil, want error for nil handler")
	}
}

func TestNewRouter_SameMethodDifferentPathsAllowed(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	custom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler, err := httpserver.NewRouter(
		httpserver.RouterParams{
			Routes: []httpserver.Route{
				httpserver.GET("/example/one", custom),
				httpserver.GET("/example/two", custom),
			},
		},
		config.HTTP{Port: 8080},
		logger,
		responder,
		allowAllLimiter{},
		clientip.NewResolver(nil),
		testRateLimitConfig(),
	)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	for _, path := range []string{"/example/one", "/example/two"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("path %s: status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

// testRateLimitConfig gives router tests a limit far above anything
// they generate, so the generic limiter never interferes with what they
// are actually asserting. The limiter middleware has its own tests.
func testRateLimitConfig() ratelimit.Config {
	return ratelimit.Config{GlobalPerMinute: 10000, GlobalBurst: 10000, Timeout: time.Second}
}

// allowAllLimiter is a ratelimit.Limiter that permits everything.
type allowAllLimiter struct{}

func (allowAllLimiter) Allow(context.Context, ratelimit.Key, ratelimit.Policy) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: true, Remaining: 1}, nil
}
