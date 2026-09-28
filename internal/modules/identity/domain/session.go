package domain

import (
	"time"

	"github.com/google/uuid"
)

// Session is an authentication session backed by a rotating opaque
// refresh token. Only the refresh token's hash is ever stored.
type Session struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	RefreshTokenHash string
	ExpiresAt        time.Time
	RevokedAt        *time.Time
	LastSeenAt       *time.Time
	UserAgent        *string
	IPAddress        *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func NewSession(
	userID uuid.UUID,
	refreshTokenHash string,
	expiresAt time.Time,
	userAgent *string,
	ipAddress *string,
) *Session {
	now := time.Now().UTC()

	return &Session{
		ID:               uuid.New(),
		UserID:           userID,
		RefreshTokenHash: refreshTokenHash,
		ExpiresAt:        expiresAt,
		UserAgent:        userAgent,
		IPAddress:        ipAddress,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func (s *Session) IsRevoked() bool {
	return s.RevokedAt != nil
}

func (s *Session) IsExpired(now time.Time) bool {
	return now.After(s.ExpiresAt)
}
