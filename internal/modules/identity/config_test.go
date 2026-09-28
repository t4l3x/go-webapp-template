package identity_test

import (
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

var identityEnvKeys = []string{
	"AUTH_PASSWORD_MIN_LENGTH",
	"AUTH_JWT_SECRET",
	"AUTH_JWT_ISSUER",
	"AUTH_ACCESS_TOKEN_TTL",
	"AUTH_REFRESH_TOKEN_TTL",
	"AUTH_EMAIL_VERIFICATION_TTL",
	"AUTH_EMAIL_VERIFICATION_SECRET",
	"AUTH_RATE_LIMIT_REGISTER_PER_MINUTE",
	"AUTH_RATE_LIMIT_LOGIN_IP_PER_MINUTE",
	"AUTH_RATE_LIMIT_REFRESH_PER_MINUTE",
	"AUTH_RATE_LIMIT_VERIFY_EMAIL_PER_MINUTE",
	"AUTH_RATE_LIMIT_RESEND_VERIFICATION_PER_MINUTE",
	"AUTH_EMAIL_VERIFICATION_RESEND_COOLDOWN",
	"AUTH_EMAIL_VERIFICATION_RESEND_MAX_PER_DAY",
	"AUTH_ABUSE_KEY_SECRET",
	"AUTH_LOGIN_FAILURE_MAX",
	"AUTH_LOGIN_FAILURE_WINDOW",
}

const validAbuseKeySecret = "test-abuse-key-secret-that-is-32-bytes-plus"

// validJWTSecret/validEmailVerificationSecret satisfy the minimum-length
// requirement so tests that aren't specifically about secret strength
// don't trip over them.
const (
	validJWTSecret               = "test-jwt-secret-that-is-at-least-32-bytes-long"
	validEmailVerificationSecret = "test-email-verification-secret-32-bytes-plus"
)

// setValidSecrets sets every env var LoadConfig requires to a value it
// accepts, so a test can override just the one value it cares about.
// It deliberately leaves AUTH_EMAIL_VERIFICATION_SECRET unset: the core
// config must not need it.
func setValidSecrets(t *testing.T) {
	t.Helper()

	t.Setenv("AUTH_JWT_SECRET", validJWTSecret)
	t.Setenv("AUTH_JWT_ISSUER", "go-webapp-template")
	t.Setenv("AUTH_ABUSE_KEY_SECRET", validAbuseKeySecret)
}

func TestLoadConfig_LoginAbuseSettings(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)

	cfg, err := identity.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.LoginFailureMax != 5 || cfg.LoginFailureWindow != 15*time.Minute {
		t.Fatalf("login failure policy = %d / %s, want 5 / 15m", cfg.LoginFailureMax, cfg.LoginFailureWindow)
	}

	for name, env := range map[string]map[string]string{
		"missing abuse secret":      {"AUTH_ABUSE_KEY_SECRET": ""},
		"weak abuse secret":         {"AUTH_ABUSE_KEY_SECRET": "too-short"},
		"abuse secret equal to JWT": {"AUTH_ABUSE_KEY_SECRET": validJWTSecret},
		"zero max failures":         {"AUTH_LOGIN_FAILURE_MAX": "0"},
		"zero window":               {"AUTH_LOGIN_FAILURE_WINDOW": "0s"},
	} {
		t.Run(name, func(t *testing.T) {
			testkit.UnsetEnv(t, identityEnvKeys...)
			setValidSecrets(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if _, err := identity.LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() error = nil, want error for %v", env)
			}
		})
	}
}

func TestLoadConfig_RequiresJWTSecret(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)
	t.Setenv("AUTH_JWT_SECRET", "")

	if _, err := identity.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for missing AUTH_JWT_SECRET")
	}
}

func TestLoadConfig_RequiresJWTIssuer(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)
	t.Setenv("AUTH_JWT_ISSUER", "")

	if _, err := identity.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for missing AUTH_JWT_ISSUER")
	}
}

func TestLoadConfig_WeakJWTSecretRejected(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)
	t.Setenv("AUTH_JWT_SECRET", "too-short")

	if _, err := identity.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for a JWT secret shorter than 32 bytes")
	}
}

// TestLoadConfig_DoesNotRequireEmailVerificationSecret pins the split:
// the core config, which the API loads, must not need the
// email-verification secret.
func TestLoadConfig_DoesNotRequireEmailVerificationSecret(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)

	if _, err := identity.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig() error = %v, want success without AUTH_EMAIL_VERIFICATION_SECRET", err)
	}
}

func TestLoadVerificationTokenConfig_LoadsSecretWithoutJWTSettings(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	t.Setenv("AUTH_EMAIL_VERIFICATION_SECRET", validEmailVerificationSecret)

	cfg, err := identity.LoadVerificationTokenConfig()
	if err != nil {
		t.Fatalf("LoadVerificationTokenConfig() error = %v, want success without AUTH_JWT_*", err)
	}
	if cfg.Secret != validEmailVerificationSecret {
		t.Fatalf("Secret = %q, want %q", cfg.Secret, validEmailVerificationSecret)
	}
}

func TestLoadVerificationTokenConfig_RequiresSecret(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)

	if _, err := identity.LoadVerificationTokenConfig(); err == nil {
		t.Fatalf("LoadVerificationTokenConfig() error = nil, want error for missing AUTH_EMAIL_VERIFICATION_SECRET")
	}
}

func TestLoadVerificationTokenConfig_WeakSecretRejected(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	t.Setenv("AUTH_EMAIL_VERIFICATION_SECRET", "too-short")

	if _, err := identity.LoadVerificationTokenConfig(); err == nil {
		t.Fatalf("LoadVerificationTokenConfig() error = nil, want error for a secret shorter than 32 bytes")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)

	cfg, err := identity.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.PasswordMinLength != 12 {
		t.Fatalf("PasswordMinLength = %d, want %d", cfg.PasswordMinLength, 12)
	}
	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Fatalf("AccessTokenTTL = %v, want %v", cfg.AccessTokenTTL, 15*time.Minute)
	}
	if cfg.RefreshTokenTTL != 720*time.Hour {
		t.Fatalf("RefreshTokenTTL = %v, want %v", cfg.RefreshTokenTTL, 720*time.Hour)
	}
	if cfg.EmailVerificationTTL != 24*time.Hour {
		t.Fatalf("EmailVerificationTTL = %v, want %v", cfg.EmailVerificationTTL, 24*time.Hour)
	}

	for name, got := range map[string][2]int{
		"RateLimitRegisterPerMinute":           {cfg.RateLimitRegisterPerMinute, 5},
		"RateLimitLoginIPPerMinute":            {cfg.RateLimitLoginIPPerMinute, 15},
		"RateLimitRefreshPerMinute":            {cfg.RateLimitRefreshPerMinute, 30},
		"RateLimitVerifyEmailPerMinute":        {cfg.RateLimitVerifyEmailPerMinute, 10},
		"RateLimitResendVerificationPerMinute": {cfg.RateLimitResendVerificationPerMinute, 5},
		"EmailVerificationResendMaxPerWindow":  {cfg.EmailVerificationResendMaxPerWindow, 5},
	} {
		if got[0] != got[1] {
			t.Fatalf("%s = %d, want %d", name, got[0], got[1])
		}
	}
	if cfg.EmailVerificationResendCooldown != time.Minute {
		t.Fatalf("EmailVerificationResendCooldown = %v, want %v", cfg.EmailVerificationResendCooldown, time.Minute)
	}
}

func TestLoadConfig_InvalidRateLimitsAndResendPolicyRejected(t *testing.T) {
	for variable, value := range map[string]string{
		"AUTH_RATE_LIMIT_VERIFY_EMAIL_PER_MINUTE":        "0",
		"AUTH_RATE_LIMIT_RESEND_VERIFICATION_PER_MINUTE": "0",
		"AUTH_EMAIL_VERIFICATION_RESEND_COOLDOWN":        "0s",
		"AUTH_EMAIL_VERIFICATION_RESEND_MAX_PER_DAY":     "1", // includes registration: 1 means no resend ever
	} {
		t.Run(variable, func(t *testing.T) {
			testkit.UnsetEnv(t, identityEnvKeys...)
			setValidSecrets(t)
			t.Setenv(variable, value)

			if _, err := identity.LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() error = nil, want error for %s=%s", variable, value)
			}
		})
	}

	t.Run("cooldown not shorter than the cap window", func(t *testing.T) {
		testkit.UnsetEnv(t, identityEnvKeys...)
		setValidSecrets(t)
		t.Setenv("AUTH_EMAIL_VERIFICATION_RESEND_COOLDOWN", "24h")

		if _, err := identity.LoadConfig(); err == nil {
			t.Fatalf("LoadConfig() error = nil, want error for a cooldown of 24h")
		}
	})
}

func TestLoadConfig_PasswordMinLengthBelowFloorRejected(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)
	t.Setenv("AUTH_PASSWORD_MIN_LENGTH", "1")

	if _, err := identity.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for AUTH_PASSWORD_MIN_LENGTH=1 (below the safety floor)")
	}
}

func TestLoadConfig_InvalidPasswordMinLength(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)
	t.Setenv("AUTH_PASSWORD_MIN_LENGTH", "0")

	if _, err := identity.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for AUTH_PASSWORD_MIN_LENGTH=0")
	}
}

func TestLoadConfig_InvalidAccessTokenTTL(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)
	t.Setenv("AUTH_ACCESS_TOKEN_TTL", "0s")

	if _, err := identity.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for AUTH_ACCESS_TOKEN_TTL=0s")
	}
}

func TestLoadConfig_InvalidRefreshTokenTTL(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)
	t.Setenv("AUTH_REFRESH_TOKEN_TTL", "0s")

	if _, err := identity.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for AUTH_REFRESH_TOKEN_TTL=0s")
	}
}

func TestLoadConfig_AccessTokenTTLMustBeLessThanRefreshTokenTTL(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)
	t.Setenv("AUTH_ACCESS_TOKEN_TTL", "24h")
	t.Setenv("AUTH_REFRESH_TOKEN_TTL", "1h")

	if _, err := identity.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error when AUTH_ACCESS_TOKEN_TTL >= AUTH_REFRESH_TOKEN_TTL")
	}
}

func TestLoadConfig_InvalidEmailVerificationTTL(t *testing.T) {
	testkit.UnsetEnv(t, identityEnvKeys...)
	setValidSecrets(t)
	t.Setenv("AUTH_EMAIL_VERIFICATION_TTL", "0s")

	if _, err := identity.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for AUTH_EMAIL_VERIFICATION_TTL=0s")
	}
}
