package security_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
)

// The auth TokenManager must keep satisfying the application port on its
// own, now that it no longer doubles as the verification signer.
var _ application.TokenManager = (*security.TokenManager)(nil)

func TestTokenManager_GenerateAndParseAccessToken(t *testing.T) {
	manager := security.NewTokenManager(security.AuthTokenConfig{
		Secret:         "test-secret",
		Issuer:         "go-webapp-template",
		AccessTokenTTL: 15 * time.Minute,
	})

	userID := uuid.New()
	sessionID := uuid.New()

	token, expiresAt, err := manager.GenerateAccessToken(userID, sessionID)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	if token == "" {
		t.Fatalf("GenerateAccessToken() returned empty token")
	}
	if !expiresAt.After(time.Now().UTC()) {
		t.Fatalf("expiresAt = %v, want a time in the future", expiresAt)
	}

	claims, err := manager.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("ParseAccessToken() error = %v", err)
	}
	if claims.UserID != userID {
		t.Fatalf("UserID = %v, want %v", claims.UserID, userID)
	}
	if claims.SessionID != sessionID {
		t.Fatalf("SessionID = %v, want %v", claims.SessionID, sessionID)
	}
}

func TestTokenManager_ParseAccessToken_Expired(t *testing.T) {
	manager := security.NewTokenManager(security.AuthTokenConfig{
		Secret:         "test-secret",
		Issuer:         "go-webapp-template",
		AccessTokenTTL: -time.Minute,
	})

	token, _, err := manager.GenerateAccessToken(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	if _, err := manager.ParseAccessToken(token); err == nil {
		t.Fatalf("ParseAccessToken() error = nil, want error for an expired token")
	}
}

func TestTokenManager_ParseAccessToken_WrongIssuer(t *testing.T) {
	issuedBy := security.NewTokenManager(security.AuthTokenConfig{
		Secret:         "test-secret",
		Issuer:         "issuer-a",
		AccessTokenTTL: 15 * time.Minute,
	})
	verifiedBy := security.NewTokenManager(security.AuthTokenConfig{
		Secret:         "test-secret",
		Issuer:         "issuer-b",
		AccessTokenTTL: 15 * time.Minute,
	})

	token, _, err := issuedBy.GenerateAccessToken(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	if _, err := verifiedBy.ParseAccessToken(token); err == nil {
		t.Fatalf("ParseAccessToken() error = nil, want error for a mismatched issuer")
	}
}

func TestTokenManager_ParseAccessToken_WrongSigningMethod(t *testing.T) {
	manager := security.NewTokenManager(security.AuthTokenConfig{
		Secret:         "test-secret",
		Issuer:         "go-webapp-template",
		AccessTokenTTL: 15 * time.Minute,
	})

	now := time.Now().UTC()

	claims := jwt.MapClaims{
		"sub": uuid.New().String(),
		"sid": uuid.New().String(),
		"iss": "go-webapp-template",
		"iat": jwt.NewNumericDate(now),
		"exp": jwt.NewNumericDate(now.Add(15 * time.Minute)),
	}

	// HS512 is a valid HMAC signature that could be produced with the
	// same secret, but the manager must only accept HS256.
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)

	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	if _, err := manager.ParseAccessToken(signed); err == nil {
		t.Fatalf("ParseAccessToken() error = nil, want error for an unexpected signing method")
	}
}

func TestTokenManager_ParseAccessToken_MalformedSubjectUUID(t *testing.T) {
	manager := security.NewTokenManager(security.AuthTokenConfig{
		Secret:         "test-secret",
		Issuer:         "go-webapp-template",
		AccessTokenTTL: 15 * time.Minute,
	})

	signed := signRawClaims(t, "test-secret", jwt.MapClaims{
		"sub": "not-a-uuid",
		"sid": uuid.New().String(),
		"iss": "go-webapp-template",
		"iat": jwt.NewNumericDate(time.Now().UTC()),
		"exp": jwt.NewNumericDate(time.Now().UTC().Add(15 * time.Minute)),
	})

	if _, err := manager.ParseAccessToken(signed); err == nil {
		t.Fatalf("ParseAccessToken() error = nil, want error for a malformed subject UUID")
	}
}

func TestTokenManager_ParseAccessToken_MalformedSessionUUID(t *testing.T) {
	manager := security.NewTokenManager(security.AuthTokenConfig{
		Secret:         "test-secret",
		Issuer:         "go-webapp-template",
		AccessTokenTTL: 15 * time.Minute,
	})

	signed := signRawClaims(t, "test-secret", jwt.MapClaims{
		"sub": uuid.New().String(),
		"sid": "not-a-uuid",
		"iss": "go-webapp-template",
		"iat": jwt.NewNumericDate(time.Now().UTC()),
		"exp": jwt.NewNumericDate(time.Now().UTC().Add(15 * time.Minute)),
	})

	if _, err := manager.ParseAccessToken(signed); err == nil {
		t.Fatalf("ParseAccessToken() error = nil, want error for a malformed session UUID")
	}
}

func TestTokenManager_ParseAccessToken_MissingSessionClaim(t *testing.T) {
	manager := security.NewTokenManager(security.AuthTokenConfig{
		Secret:         "test-secret",
		Issuer:         "go-webapp-template",
		AccessTokenTTL: 15 * time.Minute,
	})

	signed := signRawClaims(t, "test-secret", jwt.MapClaims{
		"sub": uuid.New().String(),
		"iss": "go-webapp-template",
		"iat": jwt.NewNumericDate(time.Now().UTC()),
		"exp": jwt.NewNumericDate(time.Now().UTC().Add(15 * time.Minute)),
	})

	if _, err := manager.ParseAccessToken(signed); err == nil {
		t.Fatalf("ParseAccessToken() error = nil, want error for a token missing the session claim")
	}
}

func signRawClaims(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	return signed
}

func TestTokenManager_ParseAccessToken_MalformedToken(t *testing.T) {
	manager := security.NewTokenManager(security.AuthTokenConfig{
		Secret:         "test-secret",
		Issuer:         "go-webapp-template",
		AccessTokenTTL: 15 * time.Minute,
	})

	if _, err := manager.ParseAccessToken("not-a-jwt"); err == nil {
		t.Fatalf("ParseAccessToken() error = nil, want error for a malformed token")
	}
}

func TestTokenManager_GenerateRefreshToken_IsRandomAndHashDeterministic(t *testing.T) {
	manager := security.NewTokenManager(security.AuthTokenConfig{
		Secret:         "test-secret",
		Issuer:         "go-webapp-template",
		AccessTokenTTL: 15 * time.Minute,
	})

	first, err := manager.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	second, err := manager.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	if first == second {
		t.Fatalf("GenerateRefreshToken() produced identical tokens, want distinct values")
	}
	if len(first) < 32 {
		t.Fatalf("GenerateRefreshToken() token length = %d, want at least 32 characters of encoded randomness", len(first))
	}

	hash1 := manager.HashRefreshToken(first)
	hash2 := manager.HashRefreshToken(first)

	if hash1 != hash2 {
		t.Fatalf("HashRefreshToken() is not deterministic: %q != %q", hash1, hash2)
	}
	if hash1 == first {
		t.Fatalf("HashRefreshToken() returned the raw token unmodified")
	}
}
