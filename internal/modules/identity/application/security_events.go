package application

import (
	"time"

	"github.com/google/uuid"
)

// SecurityEventType names a security-relevant outcome. These are
// in-process signals for alerting, audit and metrics — not outbox
// events: nothing persists them, so they are not versioned. More types
// (login.challenge_required, login.risk_high, mfa.failed) arrive with
// the features that produce them.
type SecurityEventType string

const (
	SecurityEventLoginFailed    SecurityEventType = "identity.login.failed"
	SecurityEventLoginThrottled SecurityEventType = "identity.login.throttled"
)

// Reasons are internal, never shown to the caller: the HTTP response
// stays the generic invalid_credentials / rate_limit_exceeded.
const (
	loginFailedUnknownAccount  = "unknown_account"
	loginFailedNoPassword      = "no_password"
	loginFailedWrongPassword   = "wrong_password"
	loginFailedAccountDisabled = "account_disabled"
	loginThrottledRiskDenied   = "risk_denied"
)

// SecurityEvent carries no email address or password: UserID when an
// account matched, and the client IP (already present in access logs).
type SecurityEvent struct {
	Type       SecurityEventType
	UserID     *uuid.UUID
	IPAddress  *string
	Reason     string
	RetryAfter time.Duration
}
