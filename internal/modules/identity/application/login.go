package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/validation"
)

var (
	errInvalidCredentials = apperror.New(apperror.KindUnauthorized, "invalid_credentials", "Invalid email or password")
	errAccountDisabled    = apperror.New(apperror.KindForbidden, "account_disabled", "Account is disabled")
)

// errLoginThrottled deliberately uses the HTTP limiter's code and
// message (rate_limit_exceeded): a caller must not be able to tell the
// account+IP rule from the per-IP limit, or the difference would leak
// whether the account exists.
func errLoginThrottled(retryAfter time.Duration) error {
	return apperror.Wrap(
		apperror.KindTooManyRequests,
		"rate_limit_exceeded",
		"Too many requests",
		&ThrottledError{Wait: retryAfter},
	)
}

// ThrottledError carries how long a throttled caller should wait. The
// HTTP responder reads RetryAfter to set the Retry-After header.
type ThrottledError struct {
	Wait time.Duration
}

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("throttled; retry after %s", e.Wait)
}

func (e *ThrottledError) RetryAfter() time.Duration {
	return e.Wait
}

// SessionConfig holds the session-lifetime settings shared by the
// use cases that create or rotate authentication sessions.
type SessionConfig struct {
	RefreshTokenTTL time.Duration
}

type LoginService struct {
	users    UserRepository
	sessions SessionRepository
	hasher   PasswordHasher
	tokens   TokenManager
	risk     LoginRiskEvaluator
	events   SecurityEvents
	cfg      SessionConfig

	// dummyHash is verified against when there is no real hash to check
	// (unknown account, password-less account), so those attempts cost
	// the same as a wrong password. Made once at construction by the
	// configured hasher, so it always uses the current Argon2 parameters
	// — changing them updates it on the next start, with nothing to forget.
	dummyHash string
}

func NewLoginService(
	users UserRepository,
	sessions SessionRepository,
	hasher PasswordHasher,
	tokens TokenManager,
	risk LoginRiskEvaluator,
	events SecurityEvents,
	cfg SessionConfig,
) (*LoginService, error) {
	dummyHash, err := hasher.Hash("login-timing-equalizer")
	if err != nil {
		return nil, fmt.Errorf("precompute login timing hash: %w", err)
	}

	return &LoginService{
		users:     users,
		sessions:  sessions,
		hasher:    hasher,
		tokens:    tokens,
		risk:      risk,
		events:    events,
		cfg:       cfg,
		dummyHash: dummyHash,
	}, nil
}

type LoginInput struct {
	Email     string
	Password  string
	UserAgent *string
	IPAddress *string
}

type LoginOutput struct {
	User                 UserView
	AccessToken          string
	AccessTokenExpiresAt time.Time
	RefreshToken         string
}

func (s *LoginService) Login(ctx context.Context, in LoginInput) (LoginOutput, error) {
	// A malformed email is reported as invalid credentials, not as a
	// validation error: login must not reveal anything about which part
	// of the input was wrong. It names no account, so it is not counted
	// per account (the per-IP limit covers it) and skips the password
	// work — which reveals only what the caller already knows: the
	// input was not an address.
	email, err := validation.NormalizeEmail(in.Email)
	if err != nil {
		return LoginOutput{}, errInvalidCredentials
	}

	attempt := LoginAttempt{Email: email, IPAddress: in.IPAddress}

	decision, err := s.risk.Evaluate(ctx, attempt)
	if err != nil {
		return LoginOutput{}, apperror.Wrap(apperror.KindInternal, "login_risk_evaluation_failed", "Failed to login", err)
	}
	if decision.Action != RiskAllow {
		s.events.Publish(ctx, SecurityEvent{
			Type:       SecurityEventLoginThrottled,
			IPAddress:  in.IPAddress,
			Reason:     loginThrottledRiskDenied,
			RetryAfter: decision.RetryAfter,
		})

		return LoginOutput{}, errLoginThrottled(decision.RetryAfter)
	}

	// Every attempt that names an account — existing or not, with or
	// without a password — costs exactly one password verification and
	// ends in the same invalid_credentials. That equalizes the dominant
	// (Argon2) cost, so neither the response nor its timing readily tells
	// a caller whether the address is registered, and guessing unknown
	// addresses is no cheaper than guessing real ones. It is timing
	// equalization, not constant time: lookup paths can differ slightly.
	user, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, domain.ErrUserNotFound) {
		s.equalizePasswordWork(in.Password)
		return LoginOutput{}, s.loginFailed(ctx, in, nil, loginFailedUnknownAccount, errInvalidCredentials)
	}
	if err != nil {
		return LoginOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"login_lookup_failed",
			"Failed to login",
			err,
		)
	}

	if user.PasswordHash == nil {
		s.equalizePasswordWork(in.Password)
		return LoginOutput{}, s.loginFailed(ctx, in, &user.ID, loginFailedNoPassword, errInvalidCredentials)
	}

	ok, err := s.hasher.Verify(in.Password, *user.PasswordHash)
	if err != nil {
		return LoginOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"password_verify_failed",
			"Failed to login",
			err,
		)
	}
	if !ok {
		return LoginOutput{}, s.loginFailed(ctx, in, &user.ID, loginFailedWrongPassword, errInvalidCredentials)
	}

	// Only after the correct password: telling a caller who doesn't know
	// the password that an account is disabled would confirm it exists.
	if !user.IsActive() {
		return LoginOutput{}, s.loginFailed(ctx, in, &user.ID, loginFailedAccountDisabled, errAccountDisabled)
	}

	// The credentials were right: clear failure state now, even if
	// issuing the session below fails for an internal reason.
	s.risk.Succeeded(ctx, attempt)

	rawRefreshToken, err := s.tokens.GenerateRefreshToken()
	if err != nil {
		return LoginOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"refresh_token_generate_failed",
			"Failed to login",
			err,
		)
	}

	now := time.Now().UTC()

	// session.ID is generated locally (no DB round trip), so the access
	// token can be generated before anything is persisted. That way, if
	// token generation fails, nothing has been written to the database
	// and no orphaned session is left behind.
	session := domain.NewSession(
		user.ID,
		s.tokens.HashRefreshToken(rawRefreshToken),
		now.Add(s.cfg.RefreshTokenTTL),
		in.UserAgent,
		in.IPAddress,
	)

	accessToken, accessTokenExpiresAt, err := s.tokens.GenerateAccessToken(user.ID, session.ID)
	if err != nil {
		return LoginOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"access_token_generate_failed",
			"Failed to login",
			err,
		)
	}

	if err := s.sessions.Create(ctx, session); err != nil {
		return LoginOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"session_create_failed",
			"Failed to login",
			err,
		)
	}

	return LoginOutput{
		User:                 newUserView(*user),
		AccessToken:          accessToken,
		AccessTokenExpiresAt: accessTokenExpiresAt,
		RefreshToken:         rawRefreshToken,
	}, nil
}

// equalizePasswordWork verifies against the precomputed dummy hash, so an
// attempt with no real hash to check costs one verification — never a
// hash generation, which would be more expensive and just as telling.
func (s *LoginService) equalizePasswordWork(password string) {
	_, _ = s.hasher.Verify(password, s.dummyHash)
}

// loginFailed publishes the failure and returns the caller-facing error.
// The attempt already counts toward the risk rules (Evaluate reserved
// it), so nothing else is recorded here.
func (s *LoginService) loginFailed(ctx context.Context, in LoginInput, userID *uuid.UUID, reason string, err error) error {
	s.events.Publish(ctx, SecurityEvent{
		Type:      SecurityEventLoginFailed,
		UserID:    userID,
		IPAddress: in.IPAddress,
		Reason:    reason,
	})

	return err
}
