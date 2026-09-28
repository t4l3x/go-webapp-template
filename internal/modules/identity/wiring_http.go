package identity

import (
	"fmt"

	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
	identityhttp "github.com/t4l3x/go-webapp-template/internal/modules/identity/transport/http"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

// HTTPModule composes session and verification endpoints. Verification consumes
// the dedicated email key; delivery adapters remain worker-only.
var HTTPModule = fx.Module(
	"identity-http",
	verificationTokenProviders,
	fx.Invoke(validateIdentityKeys),

	fx.Provide(
		LoadConfig,

		// Use cases and infrastructure depend only on the narrow config
		// shape they need, not on the module's root Config, so that
		// application/infrastructure packages never import this
		// package (which would create an import cycle with the wiring
		// below).
		func(cfg Config) application.RegisterConfig {
			return application.RegisterConfig{
				PasswordMinLength:    cfg.PasswordMinLength,
				EmailVerificationTTL: cfg.EmailVerificationTTL,
			}
		},
		func(cfg Config) application.SessionConfig {
			return application.SessionConfig{
				RefreshTokenTTL: cfg.RefreshTokenTTL,
			}
		},
		func(cfg Config) security.AuthTokenConfig {
			return security.AuthTokenConfig{
				Secret:         cfg.JWTSecret,
				Issuer:         cfg.JWTIssuer,
				AccessTokenTTL: cfg.AccessTokenTTL,
			}
		},

		fx.Annotate(security.NewPasswordHasher, fx.As(new(application.PasswordHasher))),

		fx.Annotate(security.NewTokenManager, fx.As(new(application.TokenManager))),

		application.NewRegisterService,
		application.NewLoginService,
		application.NewRefreshService,
		application.NewLogoutService,
		application.NewGetMeService,
		func(signer *security.VerificationSigner) application.VerificationTokenVerifier { return signer },
		func(cfg Config) application.ResendVerificationConfig {
			return application.ResendVerificationConfig{TTL: cfg.EmailVerificationTTL}
		},
		application.NewVerifyEmailService,
		application.NewResendEmailVerificationService,

		// Identity's endpoint limits, adapted from the module's root
		// Config into the narrow shape its transport needs — the same
		// pattern as the application/security config shapes above.
		func(cfg Config) identityhttp.RateLimitPolicies {
			return identityhttp.RateLimitPolicies{
				Register: ratelimit.PerMinute(cfg.RateLimitRegisterPerMinute),
				LoginIP:  ratelimit.PerMinute(cfg.RateLimitLoginIPPerMinute),
				Refresh:  ratelimit.PerMinute(cfg.RateLimitRefreshPerMinute),
			}
		},

		identityhttp.NewHandler,
		identityhttp.NewAuthMiddleware,
		identityhttp.NewRateLimiter,

		fx.Annotate(
			identityhttp.NewRoutes,
			fx.ResultTags(`group:"http_routes,flatten"`),
		),
	),
)

func validateIdentityKeys(cfg Config, verification VerificationTokenConfig) error {
	if cfg.JWTSecret == verification.Secret {
		return fmt.Errorf("AUTH_JWT_SECRET and AUTH_EMAIL_VERIFICATION_SECRET must differ")
	}
	return nil
}
