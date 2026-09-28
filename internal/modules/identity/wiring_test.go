package identity_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/config"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/platform/localization"
	"github.com/t4l3x/go-webapp-template/internal/platform/mail"
	"github.com/t4l3x/go-webapp-template/internal/platform/outbox"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

// These tests exist because splitting identity's wiring into CoreModule
// + HTTPModule + WorkerModule made one thing checkable that Fx laziness
// previously left implicit: which adapters a process actually runs.
// They assert both halves of that — each composition contributes its
// own adapters, and contributes none of the other's. A misplaced
// provider (an HTTP route declared in CoreModule, say, or a worker
// handler that quietly stops being registered) type-checks fine and
// would otherwise only show up in a running binary.
//
// Each composition is built with only its own process's secrets, so a
// provider that pulls in the other process's secret fails here too.
//
// They stay wiring-only: no lifecycle is started, so the nil pool
// below is never dialed, and the use cases themselves are covered by
// ordinary direct-construction tests in application/.

type routesIn struct {
	fx.In

	Routes []httpserver.Route `group:"http_routes"`
}

type outboxHandlersIn struct {
	fx.In

	Handlers []outbox.HandlerRegistration `group:"outbox_handlers"`
}

func TestHTTPModule_ProvidesAuthRoutesAndNoOutboxHandlers(t *testing.T) {
	apiIdentityEnv(t)

	var (
		routes   []httpserver.Route
		handlers []outbox.HandlerRegistration
	)

	app := fx.New(
		identity.CoreModule,
		identity.HTTPModule,
		stubDependencies(),
		fx.Invoke(func(in routesIn, handlersIn outboxHandlersIn) {
			routes = in.Routes
			handlers = handlersIn.Handlers
		}),
		fx.NopLogger,
	)
	if err := app.Err(); err != nil {
		t.Fatalf("identity.CoreModule + HTTPModule failed to build with API secrets: %v", err)
	}

	// The full, externally visible paths — prefix included. Identity
	// declares these relative to /api/v1, so asserting the joined result
	// is what actually pins the public surface.
	want := []httpserver.Route{
		{Method: "POST", Path: "/api/v1/auth/register"},
		{Method: "POST", Path: "/api/v1/auth/login"},
		{Method: "POST", Path: "/api/v1/auth/refresh"},
		{Method: "POST", Path: "/api/v1/auth/logout"},
		{Method: "GET", Path: "/api/v1/auth/me"},
		{Method: "POST", Path: "/api/v1/auth/verify-email"},
		{Method: "POST", Path: "/api/v1/auth/resend-verification"},
	}

	if len(routes) != len(want) {
		t.Fatalf("routes = %d, want %d", len(routes), len(want))
	}

	for _, wantRoute := range want {
		if !hasRoute(routes, wantRoute.Method, wantRoute.Path) {
			t.Fatalf("route %s %s was not contributed by HTTPModule", wantRoute.Method, wantRoute.Path)
		}
	}

	if len(handlers) != 0 {
		t.Fatalf("outbox handlers = %d, want 0 — the API composition must not register worker handlers", len(handlers))
	}
}

// TestWorkerModule_RegistersVersionedEmailVerificationHandler pins the
// event type the worker composition actually binds. The type is a
// persisted contract, so this guards the wiring end of it: a rename
// here would leave already-enqueued rows with no registered handler.
func TestWorkerModule_RegistersVersionedEmailVerificationHandler(t *testing.T) {
	workerIdentityEnv(t)

	var (
		routes   []httpserver.Route
		handlers []outbox.HandlerRegistration
	)

	// localization.Module is composed here, as the worker process does,
	// rather than stubbed: that makes this also a check that identity's
	// embedded catalog registers into the localization group and passes
	// the engine's startup validation.
	app := fx.New(
		identity.CoreModule,
		identity.WorkerModule,
		localization.Module,
		stubDependencies(),
		fx.Invoke(func(in routesIn, handlersIn outboxHandlersIn) {
			routes = in.Routes
			handlers = handlersIn.Handlers
		}),
		fx.NopLogger,
	)
	if err := app.Err(); err != nil {
		t.Fatalf("identity.CoreModule + WorkerModule failed to build without AUTH_JWT_*: %v", err)
	}

	if len(handlers) != 1 {
		t.Fatalf("outbox handlers = %d, want 1", len(handlers))
	}
	if handlers[0].Type != "identity.email_verification_requested.v1" {
		t.Fatalf("handler type = %q, want %q",
			handlers[0].Type, "identity.email_verification_requested.v1")
	}
	if handlers[0].Handler == nil {
		t.Fatalf("registered handler is nil")
	}

	if len(routes) != 0 {
		t.Fatalf("http routes = %d, want 0 — the worker composition must not register HTTP routes", len(routes))
	}
}

// TestWorkerModule_SignsWithDedicatedVerificationSigner pins what the
// worker's VerificationSigner binding resolves to: the verification-only
// signer, keyed by AUTH_EMAIL_VERIFICATION_SECRET — not the auth token
// manager that used to double as it. It runs without AUTH_JWT_*.
func TestWorkerModule_SignsWithDedicatedVerificationSigner(t *testing.T) {
	workerIdentityEnv(t)

	var signer application.VerificationSigner

	app := fx.New(
		identity.CoreModule,
		identity.WorkerModule,
		stubDependencies(),
		fx.Populate(&signer),
		fx.NopLogger,
	)
	if err := app.Err(); err != nil {
		t.Fatalf("identity.CoreModule + WorkerModule failed to build: %v", err)
	}

	if _, ok := signer.(*security.VerificationSigner); !ok {
		t.Fatalf("VerificationSigner is %T, want *security.VerificationSigner", signer)
	}

	// A token the wired signer produces must verify under the configured
	// secret, proving the env value reached it.
	verificationID := uuid.New()
	expiresAt := time.Now().Add(time.Hour)
	token := signer.SignVerificationToken(verificationID, expiresAt)

	verifier := security.NewVerificationSigner(security.VerificationTokenConfig{Secret: testEmailVerificationSecret})

	gotID, _, err := verifier.VerifyVerificationToken(token)
	if err != nil {
		t.Fatalf("token from the wired signer does not verify under AUTH_EMAIL_VERIFICATION_SECRET: %v", err)
	}
	if gotID != verificationID {
		t.Fatalf("verified id = %v, want %v", gotID, verificationID)
	}
}

// TestHTTPModule_RoutesMatchOpenAPIContract closes the gap that route
// prefixing opens: the /api/v1 prefix now lives in two places — the
// route group here and the path keys in docs/api/openapi.yaml — and
// nothing else compares them. `make openapi-validate` only checks the
// document is well-formed; `make openapi-check` only checks the
// generated types aren't stale. A spec path left unprefixed, or an
// endpoint documented but never routed (or the reverse), would pass
// both.
//
// Scope is identity's own surface: every /api/v1/... operation in the
// contract must be a route this module registers, and vice versa.
// Operational paths (/health) belong to the router, not to a module,
// and are covered by httpserver's own tests.
func TestHTTPModule_RoutesMatchOpenAPIContract(t *testing.T) {
	apiIdentityEnv(t)

	var routes []httpserver.Route

	app := fx.New(
		identity.CoreModule,
		identity.HTTPModule,
		stubDependencies(),
		fx.Invoke(func(in routesIn) { routes = in.Routes }),
		fx.NopLogger,
	)
	if err := app.Err(); err != nil {
		t.Fatalf("identity.CoreModule + HTTPModule failed to build: %v", err)
	}

	loader := openapi3.NewLoader()

	doc, err := loader.LoadFromFile(filepath.Join("..", "..", "..", "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("load OpenAPI contract: %v", err)
	}

	documented := make(map[string]struct{})

	for path, item := range doc.Paths.Map() {
		if !strings.HasPrefix(path, "/api/") {
			continue
		}

		for method := range item.Operations() {
			documented[method+" "+path] = struct{}{}
		}
	}

	registered := make(map[string]struct{}, len(routes))

	for _, route := range routes {
		registered[route.Method+" "+route.Path] = struct{}{}
	}

	for operation := range documented {
		if _, ok := registered[operation]; !ok {
			t.Errorf("%s is documented in openapi.yaml but not registered as a route", operation)
		}
	}

	for operation := range registered {
		if _, ok := documented[operation]; !ok {
			t.Errorf("%s is registered as a route but not documented in openapi.yaml", operation)
		}
	}
}

// TestOpenAPIContract_LeavesOperationalPathsUnversioned pins the other
// half of the policy in the contract itself: /health is documented at
// its bare path, and no operational endpoint hides under /api/v1.
func TestOpenAPIContract_LeavesOperationalPathsUnversioned(t *testing.T) {
	loader := openapi3.NewLoader()

	doc, err := loader.LoadFromFile(filepath.Join("..", "..", "..", "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("load OpenAPI contract: %v", err)
	}

	if doc.Paths.Find("/health") == nil {
		t.Fatalf("/health is not documented at its unversioned path")
	}

	for path := range doc.Paths.Map() {
		if strings.HasPrefix(path, "/api/") && strings.Contains(path, "health") {
			t.Fatalf("%q puts an operational endpoint behind an API major version", path)
		}
	}
}

// apiIdentityEnv grants only the API's identity secrets: the JWT
// settings and the email-verification key needed to consume links.
func apiIdentityEnv(t *testing.T) {
	t.Helper()

	t.Setenv("AUTH_EMAIL_VERIFICATION_SECRET", testEmailVerificationSecret)
	t.Setenv("AUTH_JWT_SECRET", "test-jwt-secret-that-is-at-least-32-bytes-long")
	t.Setenv("AUTH_JWT_ISSUER", "go-webapp-template")
}

// workerIdentityEnv grants only the worker's identity secret: the
// email-verification secret, and deliberately not AUTH_JWT_*.
func workerIdentityEnv(t *testing.T) {
	t.Helper()

	testkit.UnsetEnv(t, "AUTH_JWT_SECRET", "AUTH_JWT_ISSUER")
	t.Setenv("AUTH_EMAIL_VERIFICATION_SECRET", testEmailVerificationSecret)
}

const testEmailVerificationSecret = "test-email-verification-secret-32-bytes-plus"

// stubDependencies supplies what identity's compositions expect from
// the platform modules a real process would include, without building
// any of them. The pool is nil on purpose: repository constructors only
// store it, and nothing here starts a lifecycle that would dial it.
func stubDependencies() fx.Option {
	return fx.Provide(
		func() *pgxpool.Pool { return nil },
		func() *slog.Logger { return slog.New(slog.DiscardHandler) },
		func() config.App { return config.App{PublicURL: "https://app.example.com"} },
		func() *clientip.Resolver { return clientip.NewResolver(nil) },
		func() mail.Sender { return stubSender{} },
		func() ratelimit.Limiter { return stubLimiter{} },
		response.NewResponder,
	)
}

func hasRoute(routes []httpserver.Route, method, path string) bool {
	for _, route := range routes {
		if route.Method == method && route.Path == path {
			return true
		}
	}

	return false
}

type stubSender struct{}

func (stubSender) Send(context.Context, mail.Message) error { return nil }

// stubLimiter stands in for the Redis-backed limiter: these tests check
// wiring, not throttling behavior.
type stubLimiter struct{}

func (stubLimiter) Allow(context.Context, ratelimit.Key, ratelimit.Policy) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: true}, nil
}
