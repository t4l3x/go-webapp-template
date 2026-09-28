package application

import (
	"context"
	"time"
)

// LoginAttempt is what a login risk rule sees about one attempt, before
// any credential is checked. Email is already normalized. It carries no
// password: no rule needs one, and none should be able to log one.
type LoginAttempt struct {
	Email     string
	IPAddress *string
}

// RiskAction is what a LoginRiskEvaluator tells LoginService to do.
//
// Only the actions a rule currently produces exist. Delay, Challenge
// (CAPTCHA) and RequireMFA are added together with the first rule and
// provider that produce them, not ahead of it.
type RiskAction int

const (
	RiskAllow RiskAction = iota

	// RiskDeny refuses the attempt before credentials are checked, for
	// RetryAfter. It throttles; it is never an account lockout.
	RiskDeny
)

type RiskDecision struct {
	Action     RiskAction
	RetryAfter time.Duration
}

// LoginRiskEvaluator is the Strategy seam for login abuse rules: account+IP
// failure throttling today; trusted-device recognition, account-wide
// progressive throttling and external risk signals later. More than one
// rule becomes a small composite that runs them in order and returns the
// strictest decision — composed in wiring, not inside LoginService.
//
// Evaluate runs before the password hash is verified, so a refused
// attempt costs no Argon2 work.
type LoginRiskEvaluator interface {
	Evaluate(ctx context.Context, attempt LoginAttempt) (RiskDecision, error)

	// Succeeded reports that the attempt authenticated, so a rule can
	// clear the state it keeps. Best effort: implementations handle and
	// log their own failures; it never fails a login that succeeded.
	Succeeded(ctx context.Context, attempt LoginAttempt)
}

// LoginFailureConfig is the account+IP failure policy.
type LoginFailureConfig struct {
	MaxFailures int
	Window      time.Duration
}

// AccountIPFailureRule throttles repeated failed logins for one account
// from one client IP: after MaxFailures in a fixed Window, further
// attempts from that IP for that account are refused until the window
// ends. Other IPs — including the real user's — are unaffected, which is
// why this can never lock a victim out.
//
// Every attempt reserves a slot atomically before the password is
// checked, so concurrent guesses cannot all pass a check-then-verify
// race; a successful login deletes the counter, so only failures
// accumulate. An attempt that fails for an internal reason (database
// down) keeps its slot — rare, and it errs toward caution.
type AccountIPFailureRule struct {
	counter LoginFailureCounter
	cfg     LoginFailureConfig
}

func NewAccountIPFailureRule(counter LoginFailureCounter, cfg LoginFailureConfig) *AccountIPFailureRule {
	return &AccountIPFailureRule{counter: counter, cfg: cfg}
}

func (r *AccountIPFailureRule) Evaluate(ctx context.Context, attempt LoginAttempt) (RiskDecision, error) {
	count, remaining := r.counter.Reserve(ctx, attempt.Email, attempt.IPAddress, r.cfg.Window)
	if count > r.cfg.MaxFailures {
		return RiskDecision{Action: RiskDeny, RetryAfter: remaining}, nil
	}

	return RiskDecision{Action: RiskAllow}, nil
}

func (r *AccountIPFailureRule) Succeeded(ctx context.Context, attempt LoginAttempt) {
	r.counter.Reset(ctx, attempt.Email, attempt.IPAddress)
}
