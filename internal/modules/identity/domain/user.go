package domain

import (
	"time"

	"github.com/google/uuid"
)

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

// User is an identity. Email and phone are expected to already be
// normalized (trimmed, lowercased for email, E.164 for phone) by the
// application layer before a User is constructed.
type User struct {
	ID           uuid.UUID
	Email        string
	Phone        *string
	PasswordHash *string
	Status       UserStatus

	EmailVerifiedAt *time.Time
	PhoneVerifiedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewUser creates a new active user with a freshly generated identity.
func NewUser(email string, phone *string, passwordHash *string) *User {
	now := time.Now().UTC()

	return &User{
		ID:           uuid.New(),
		Email:        email,
		Phone:        phone,
		PasswordHash: passwordHash,
		Status:       UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func (u *User) IsActive() bool {
	return u.Status == UserStatusActive
}
