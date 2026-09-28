package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

// UserRepository is the read persistence contract required by the
// identity use cases. It is implemented by infrastructure/postgres and
// consumed here as an interface so application code stays independent
// of pgx. It has no write methods: creating a user is not a standalone
// operation in this module (see RegistrationStore).
type UserRepository interface {
	FindByEmail(ctx context.Context, email string) (*domain.User, error)
	FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
}

// RegistrationStore atomically saves the account, credential, and delivery
// request. The application owns this invariant; infrastructure owns the SQL.
type RegistrationStore interface {
	Register(
		ctx context.Context,
		user *domain.User,
		verification *domain.EmailVerification,
		event EmailVerificationRequestedV1,
	) error
}

// SessionRepository is the persistence contract for authentication
// sessions backed by rotating refresh tokens.
type SessionRepository interface {
	Create(ctx context.Context, session *domain.Session) error
	FindByRefreshTokenHash(ctx context.Context, hash string) (*domain.Session, error)

	RotateRefreshToken(
		ctx context.Context,
		sessionID uuid.UUID,
		currentRefreshTokenHash string,
		newRefreshTokenHash string,
		newExpiresAt time.Time,
	) (*domain.Session, error)

	Revoke(ctx context.Context, sessionID uuid.UUID) error
}

// PasswordHasher hashes and verifies passwords. Implementations must
// never return the plaintext password or log it.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password string, encodedHash string) (bool, error)
}

// AccessTokenClaims is the minimal set of claims the application layer
// needs from a parsed access token.
type AccessTokenClaims struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

// TokenManager issues and validates access tokens, and manages the
// opaque/hashed representation of refresh tokens. It hides the
// concrete token technology (JWT, crypto/rand) from application code.
type TokenManager interface {
	GenerateAccessToken(userID, sessionID uuid.UUID) (token string, expiresAt time.Time, err error)
	ParseAccessToken(token string) (AccessTokenClaims, error)

	GenerateRefreshToken() (raw string, err error)
	HashRefreshToken(raw string) string
}

// VerificationDeliveryStore checks the credential and recipient against current state.
type VerificationDeliveryStore interface {
	CanDeliver(context.Context, EmailVerificationRequestedV1) (bool, error)
}

// VerificationSigner holds only the email verification key, never the JWT key.
type VerificationSigner interface {
	SignVerificationToken(uuid.UUID, time.Time) string
}

type VerificationTokenVerifier interface {
	VerifyVerificationToken(string) (uuid.UUID, time.Time, error)
}

// LoginFailureCounter keeps short-lived account+IP login attempt counts
// for AccountIPFailureRule. Implementations must not store the raw email
// (key it by a keyed hash), and fail open: when the backend is
// unavailable Reserve reports a zero count and the failure is logged,
// the same policy as the HTTP rate limiters.
type LoginFailureCounter interface {
	// Reserve atomically counts one attempt and returns the count in the
	// current fixed window (including this one) and the time left in it.
	// The window starts at the first attempt and is not extended by later
	// ones.
	Reserve(ctx context.Context, email string, ip *string, window time.Duration) (count int, remaining time.Duration)

	// Reset clears the count after a successful login.
	Reset(ctx context.Context, email string, ip *string)
}

// SecurityEvents receives security-relevant outcomes so alerting, audit
// logging and metrics subscribe here instead of living in use cases.
// Publish must not block the request meaningfully and cannot fail it.
type SecurityEvents interface {
	Publish(ctx context.Context, event SecurityEvent)
}

// VerificationStore serializes consumption and replacement by locking the user.
//
// Replace applies policy under that same lock, against the account's
// already-issued credentials, and returns *domain.ResendLimitError when
// it refuses — so concurrent resends for one account cannot all pass.
type VerificationStore interface {
	Consume(context.Context, uuid.UUID, time.Time) error
	Replace(context.Context, *domain.EmailVerification, domain.ResendPolicy) error
}
