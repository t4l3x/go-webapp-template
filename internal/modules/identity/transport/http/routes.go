package http

import (
	"net/http"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

// Public paths change major version only for breaking API changes.
const apiV1Prefix = "/api/v1"

func NewRoutes(
	handler *Handler,
	verification *VerificationHandler,
	authMiddleware *AuthMiddleware,
	rateLimiter *RateLimiter,
	policies RateLimitPolicies,
) ([]httpserver.Route, error) {
	authenticated := func(h http.HandlerFunc) http.Handler {
		return middleware.Chain(h, authMiddleware.Authenticate)
	}

	// Verification reuses the credential-check allowance; resend reuses the
	// stricter registration allowance. Each has its own per-IP bucket.
	rateLimited := func(scope string, policy ratelimit.Policy, h http.HandlerFunc) http.Handler {
		return middleware.Chain(h, rateLimiter.PerIP(scope, policy))
	}

	return httpserver.Prefix(
		apiV1Prefix,

		httpserver.POST("/auth/register", rateLimited(scopeRegister, policies.Register, handler.Register)),
		httpserver.POST("/auth/login", rateLimited(scopeLoginIP, policies.LoginIP, handler.Login)),
		httpserver.POST("/auth/refresh", rateLimited(scopeRefresh, policies.Refresh, handler.Refresh)),
		httpserver.POST("/auth/logout", authenticated(handler.Logout)),
		httpserver.GET("/auth/me", authenticated(handler.GetMe)),
		httpserver.POST("/auth/verify-email", rateLimited(scopeVerifyEmail, policies.LoginIP, verification.Verify)),
		httpserver.POST("/auth/resend-verification", middleware.Chain(
			rateLimited(scopeResendVerification, policies.Register, verification.Resend), authMiddleware.Authenticate),
		),
	)
}
