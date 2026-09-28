package application

import (
	"time"

	"github.com/google/uuid"
)

// EventTypeEmailVerificationRequestedV1 This persisted type is frozen. Breaking payload changes require a new version.
const EventTypeEmailVerificationRequestedV1 = "identity.email_verification_requested.v1"

// EmailVerificationRequestedV1 carries non-secret delivery data. The worker
// derives the token; HTTP inputs and domain entities are not serialized here.
type EmailVerificationRequestedV1 struct {
	UserID         uuid.UUID `json:"user_id"`
	Email          string    `json:"email"`
	VerificationID uuid.UUID `json:"verification_id"`
	ExpiresAt      time.Time `json:"expires_at"`
}
