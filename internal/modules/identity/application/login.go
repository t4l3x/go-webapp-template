package application

import (
	"context"
	"errors"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/validation"
)

var (
	errInvalidCredentials = apperror.New(apperror.KindUnauthorized, "invalid_credentials", "Invalid email or password")
	errAccountDisabled    = apperror.New(apperror.KindForbidden, "account_disabled", "Account is disabled")
)

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
	cfg      SessionConfig
}

func NewLoginService(
	users UserRepository,
	sessions SessionRepository,
	hasher PasswordHasher,
	tokens TokenManager,
	cfg SessionConfig,
) *LoginService {
	return &LoginService{
		users:    users,
		sessions: sessions,
		hasher:   hasher,
		tokens:   tokens,
		cfg:      cfg,
	}
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
	// of the input was wrong.
	email, err := validation.NormalizeEmail(in.Email)
	if err != nil {
		return LoginOutput{}, errInvalidCredentials
	}

	user, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, domain.ErrUserNotFound) {
		return LoginOutput{}, errInvalidCredentials
	}
	if err != nil {
		return LoginOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"login_lookup_failed",
			"Failed to login",
			err,
		)
	}

	if !user.IsActive() {
		return LoginOutput{}, errAccountDisabled
	}

	if user.PasswordHash == nil {
		return LoginOutput{}, errInvalidCredentials
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
		return LoginOutput{}, errInvalidCredentials
	}

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
