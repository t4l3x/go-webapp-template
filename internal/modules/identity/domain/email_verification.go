package domain

import (
	"time"

	"github.com/google/uuid"
)

// EmailVerification is server-side expiry and one-time-use state. ConsumedAt
// also marks credentials invalidated by resend. No bearer token is persisted.
type EmailVerification struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

// NewEmailVerification creates a fresh, unconsumed verification
// credential for userID, expiring after ttl.
func NewEmailVerification(userID uuid.UUID, ttl time.Duration) *EmailVerification {
	now := time.Now().UTC()

	return &EmailVerification{
		ID:        uuid.New(),
		UserID:    userID,
		ExpiresAt: now.Add(ttl).Truncate(time.Microsecond),
		CreatedAt: now,
	}
}
