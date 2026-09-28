package identity

import (
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	identityredis "github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/redis"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/securitylog"
	identityhttp "github.com/t4l3x/go-webapp-template/internal/modules/identity/transport/http"
	"github.com/t4l3x/go-webapp-template/internal/platform/observability"
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

		// Login abuse protection. The failure counter lives in Redis (the
		// API already composes platform/redis for rate limiting) and gets
		// the same request-path time budget as the rate limiters.
		func(client *goredis.Client, cfg Config, rl ratelimit.Config, provider metric.MeterProvider, logger *slog.Logger) (application.LoginFailureCounter, error) {
			signal, err := observability.NewProtectionSignal(provider, logger.With("component", "identity_login_failures"), identityredis.ProtectionEvent)
			if err != nil {
				return nil, err
			}
			return identityredis.NewLoginFailureCounter(client, cfg.AbuseKeySecret, rl.Timeout, signal), nil
		},
		func(cfg Config) application.LoginFailureConfig {
			return application.LoginFailureConfig{MaxFailures: cfg.LoginFailureMax, Window: cfg.LoginFailureWindow}
		},
		// The one risk rule today. A second one (trusted devices,
		// account-wide throttling) becomes a composite evaluator here.
		fx.Annotate(application.NewAccountIPFailureRule, fx.As(new(application.LoginRiskEvaluator))),
		fx.Annotate(securitylog.NewPublisher, fx.As(new(application.SecurityEvents))),

		application.NewRegisterService,
		application.NewLoginService,
		application.NewRefreshService,
		application.NewLogoutService,
		application.NewGetMeService,
		func(signer *security.VerificationSigner) application.VerificationTokenVerifier { return signer },
		func(cfg Config) application.ResendVerificationConfig {
			return application.ResendVerificationConfig{
				TTL: cfg.EmailVerificationTTL,
				Policy: domain.ResendPolicy{
					Cooldown:     cfg.EmailVerificationResendCooldown,
					MaxPerWindow: cfg.EmailVerificationResendMaxPerWindow,
				},
			}
		},
		application.NewVerifyEmailService,
		application.NewResendEmailVerificationService,

		// Identity's endpoint limits, adapted from the module's root
		// Config into the narrow shape its transport needs — the same
		// pattern as the application/security config shapes above.
		func(cfg Config) identityhttp.RateLimitPolicies {
			return identityhttp.RateLimitPolicies{
				Register:           ratelimit.PerMinute(cfg.RateLimitRegisterPerMinute),
				LoginIP:            ratelimit.PerMinute(cfg.RateLimitLoginIPPerMinute),
				Refresh:            ratelimit.PerMinute(cfg.RateLimitRefreshPerMinute),
				VerifyEmail:        ratelimit.PerMinute(cfg.RateLimitVerifyEmailPerMinute),
				ResendVerification: ratelimit.PerMinute(cfg.RateLimitResendVerificationPerMinute),
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
	if cfg.AbuseKeySecret == verification.Secret {
		return fmt.Errorf("AUTH_ABUSE_KEY_SECRET and AUTH_EMAIL_VERIFICATION_SECRET must differ")
	}
	return nil
}
