package domain

import "errors"

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailAlreadyExists = errors.New("email already exists")
	ErrPhoneAlreadyExists = errors.New("phone already exists")

	// ErrSessionNotFound also covers a session that exists but is
	// revoked, expired, or no longer holds the expected refresh token
	// hash: RotateRefreshToken's compare-and-swap folds all of those
	// into a single "no matching row" outcome, since distinguishing
	// them at that point would only be meaningful in a race window.
	ErrSessionNotFound          = errors.New("session not found")
	ErrInvalidEmailVerification = errors.New("invalid email verification")
	ErrAccountDisabled          = errors.New("account disabled")
)
