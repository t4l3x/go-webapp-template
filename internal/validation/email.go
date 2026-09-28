// Package validation holds syntactic, business-neutral normalization
// rules shared across modules. A rule belongs here only if it is
// genuinely project-wide and free of policy: canonicalizing an email
// address qualifies, "an email must be verified before login" does
// not — that stays in the module that owns the policy.
//
// It depends on the standard library only, and never on apperror,
// HTTP, a database, or any module. Errors returned here are ordinary
// errors describing what was wrong syntactically; translating one into
// a stable application error code (invalid_email, invalid_phone) is
// the calling application layer's job, not this package's.
package validation

import (
	"errors"
	"net/mail"
	"strings"
)

// NormalizeEmail trims surrounding whitespace, lowercases, and checks
// the result is a bare mailbox address, returning the canonical form
// callers should store and compare against.
func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))

	if email == "" {
		return "", errors.New("email is empty")
	}

	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", err
	}

	// mail.ParseAddress accepts "Display Name <addr>" forms and would
	// happily parse one out of a string containing more than a bare
	// mailbox. Reject anything where the parsed address isn't an exact,
	// name-free match for what was supplied, so a display-name payload
	// is never stored as if it were an email address.
	if addr.Name != "" || addr.Address != email {
		return "", errors.New("email must be a bare mailbox address")
	}

	return email, nil
}
