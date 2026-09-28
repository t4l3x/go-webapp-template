package identity

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

// minSecretBytes is the minimum length required for any HMAC secret
// this module signs with (AUTH_JWT_SECRET, AUTH_EMAIL_VERIFICATION_SECRET).
// 32 bytes (256 bits) matches the key size HMAC-SHA256/HS256 are
// designed around; shorter secrets are within reach of brute-force/
// dictionary attacks.
const minSecretBytes = 32

// minPasswordMinLength is a hard floor under which AUTH_PASSWORD_MIN_LENGTH
// may never be configured. The default (12) may be raised by an
// operator, but this floor cannot be lowered via configuration.
const minPasswordMinLength = 8

// Config contains API identity policy and session settings. The worker does not
// load it; its verification key is parsed separately below.
type Config struct {
	PasswordMinLength int `env:"AUTH_PASSWORD_MIN_LENGTH" envDefault:"12"`

	JWTSecret string `env:"AUTH_JWT_SECRET,required,notEmpty"`
	JWTIssuer string `env:"AUTH_JWT_ISSUER,required,notEmpty"`

	AccessTokenTTL  time.Duration `env:"AUTH_ACCESS_TOKEN_TTL" envDefault:"15m"`
	RefreshTokenTTL time.Duration `env:"AUTH_REFRESH_TOKEN_TTL" envDefault:"720h"`

	EmailVerificationTTL time.Duration `env:"AUTH_EMAIL_VERIFICATION_TTL" envDefault:"24h"`

	// Rate limits for identity's own endpoints, in requests per minute.
	// They live with this module because the platform owns rate-limiting
	// mechanics while a module owns what its endpoints are worth.
	//
	// The defaults are deliberately strict — these endpoints exist to be
	// used a handful of times per session, not continuously — and are
	// starting points, not settled values: tune them per environment
	// from real traffic before treating them as load-bearing. Only
	// endpoints worth abusing get their own knob; the rest are covered
	// by the generic per-IP limit.
	//
	// Registration creates a user and sends mail, so it is the
	// strictest.
	RateLimitRegisterPerMinute int `env:"AUTH_RATE_LIMIT_REGISTER_PER_MINUTE" envDefault:"5"`

	// Login allows for a few honest retries and a password manager
	// filling the form again, without leaving room for guessing.
	//
	// This is deliberately the only login limit: an account-keyed one,
	// consulted before credentials are checked, would let an attacker who
	// merely knows a victim's address throttle that victim's own login
	// attempts. Account-aware abuse protection can be designed again,
	// but not as a hard block on an unauthenticated request.
	RateLimitLoginIPPerMinute int `env:"AUTH_RATE_LIMIT_LOGIN_IP_PER_MINUTE" envDefault:"15"`

	// Refresh is called by legitimate clients on a schedule (roughly
	// once per access-token lifetime), so its limit is looser.
	RateLimitRefreshPerMinute int `env:"AUTH_RATE_LIMIT_REFRESH_PER_MINUTE" envDefault:"30"`
}

func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("load identity config: %w", err)
	}

	if cfg.PasswordMinLength < minPasswordMinLength {
		return Config{}, fmt.Errorf("AUTH_PASSWORD_MIN_LENGTH must be at least %d", minPasswordMinLength)
	}

	if len(cfg.JWTSecret) < minSecretBytes {
		return Config{}, fmt.Errorf("AUTH_JWT_SECRET must be at least %d bytes", minSecretBytes)
	}

	if cfg.AccessTokenTTL <= 0 {
		return Config{}, fmt.Errorf("AUTH_ACCESS_TOKEN_TTL must be greater than zero")
	}

	if cfg.RefreshTokenTTL <= 0 {
		return Config{}, fmt.Errorf("AUTH_REFRESH_TOKEN_TTL must be greater than zero")
	}

	if cfg.AccessTokenTTL >= cfg.RefreshTokenTTL {
		return Config{}, fmt.Errorf("AUTH_ACCESS_TOKEN_TTL must be less than AUTH_REFRESH_TOKEN_TTL")
	}

	if cfg.EmailVerificationTTL <= 0 {
		return Config{}, fmt.Errorf("AUTH_EMAIL_VERIFICATION_TTL must be greater than zero")
	}

	// Validated at startup rather than on the first request that hits a
	// limit, so a bad value fails the process with the variable named.
	for variable, perMinute := range map[string]int{
		"AUTH_RATE_LIMIT_REGISTER_PER_MINUTE": cfg.RateLimitRegisterPerMinute,
		"AUTH_RATE_LIMIT_LOGIN_IP_PER_MINUTE": cfg.RateLimitLoginIPPerMinute,
		"AUTH_RATE_LIMIT_REFRESH_PER_MINUTE":  cfg.RateLimitRefreshPerMinute,
	} {
		if err := ratelimit.PerMinute(perMinute).Validate(); err != nil {
			return Config{}, fmt.Errorf("%s is not a usable rate limit: %w", variable, err)
		}
	}

	return cfg, nil
}

// VerificationTokenConfig is shared by API verification and worker delivery.
// HTTP composition also validates that this key differs from the JWT key.
type VerificationTokenConfig struct {
	Secret string `env:"AUTH_EMAIL_VERIFICATION_SECRET,required,notEmpty"`
}

func LoadVerificationTokenConfig() (VerificationTokenConfig, error) {
	cfg, err := env.ParseAs[VerificationTokenConfig]()
	if err != nil {
		return VerificationTokenConfig{}, fmt.Errorf("load identity verification token config: %w", err)
	}

	if len(cfg.Secret) < minSecretBytes {
		return VerificationTokenConfig{}, fmt.Errorf("AUTH_EMAIL_VERIFICATION_SECRET must be at least %d bytes", minSecretBytes)
	}

	return cfg, nil
}
