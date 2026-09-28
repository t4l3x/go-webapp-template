package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
)

// opaqueTokenBytes is the amount of randomness backing an opaque
// refresh token, chosen to exceed the required 32-byte minimum.
const opaqueTokenBytes = 32

// AuthTokenConfig holds only what the TokenManager needs to sign and
// validate access tokens. It carries no email-verification secret —
// that belongs to VerificationTokenConfig — so a process that never
// issues sessions never has to hold the JWT key, and vice versa. It
// mirrors fields from identity.Config rather than importing it, which
// would create an import cycle with the module's wiring.
type AuthTokenConfig struct {
	Secret         string
	Issuer         string
	AccessTokenTTL time.Duration
}

type accessTokenClaims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

// TokenManager issues and validates JWT access tokens and manages
// opaque refresh tokens. It implements application.TokenManager, and
// nothing else: email-verification tokens are VerificationSigner's.
type TokenManager struct {
	cfg AuthTokenConfig
}

func NewTokenManager(cfg AuthTokenConfig) *TokenManager {
	return &TokenManager{cfg: cfg}
}

func (m *TokenManager) GenerateAccessToken(userID, sessionID uuid.UUID) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(m.cfg.AccessTokenTTL)

	claims := accessTokenClaims{
		SessionID: sessionID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    m.cfg.Issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString([]byte(m.cfg.Secret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}

	return signed, expiresAt, nil
}

func (m *TokenManager) ParseAccessToken(tokenString string) (application.AccessTokenClaims, error) {
	var claims accessTokenClaims

	_, err := jwt.ParseWithClaims(
		tokenString,
		&claims,
		func(*jwt.Token) (interface{}, error) {
			return []byte(m.cfg.Secret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.cfg.Issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return application.AccessTokenClaims{}, fmt.Errorf("parse access token: %w", err)
	}

	if claims.Subject == "" || claims.SessionID == "" {
		return application.AccessTokenClaims{}, errors.New("access token missing required claims")
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return application.AccessTokenClaims{}, fmt.Errorf("parse subject claim: %w", err)
	}

	sessionID, err := uuid.Parse(claims.SessionID)
	if err != nil {
		return application.AccessTokenClaims{}, fmt.Errorf("parse session claim: %w", err)
	}

	return application.AccessTokenClaims{
		UserID:    userID,
		SessionID: sessionID,
	}, nil
}

func (m *TokenManager) GenerateRefreshToken() (string, error) {
	return generateOpaqueToken()
}

func (m *TokenManager) HashRefreshToken(raw string) string {
	return hashOpaqueToken(raw)
}

func generateOpaqueToken() (string, error) {
	raw := make([]byte, opaqueTokenBytes)

	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate opaque token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashOpaqueToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))

	return hex.EncodeToString(sum[:])
}
