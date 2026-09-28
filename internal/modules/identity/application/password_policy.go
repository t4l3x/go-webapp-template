package application

import (
	"unicode/utf8"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
)

// passwordMaxLength bounds password length to keep Argon2's hashing
// cost predictable; it is not a configurable value since it exists
// purely to reject pathological input, not to express a policy.
const passwordMaxLength = 256

var errInvalidPassword = apperror.New(
	apperror.KindValidation,
	"invalid_password",
	"Password does not meet length requirements",
)

// validatePasswordLength enforces identity's password-length policy:
// an operator-tunable minimum (floored in identity.Config) and the
// fixed maximum above. This is module policy, deliberately kept out of
// internal/validation — that package holds only syntactic,
// business-neutral rules, and how long a password must be is a
// decision this module owns.
func validatePasswordLength(password string, minLength int) error {
	length := utf8.RuneCountInString(password)

	if length < minLength || length > passwordMaxLength {
		return errInvalidPassword
	}

	return nil
}
