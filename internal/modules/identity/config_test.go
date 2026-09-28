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
}

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
