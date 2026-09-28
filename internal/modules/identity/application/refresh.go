package application

import (
	"context"
	"errors"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

var (
	errInvalidRefreshToken = apperror.New(apperror.KindUnauthorized, "invalid_refresh_token", "Invalid refresh token")
	errSessionExpired      = apperror.New(apperror.KindUnauthorized, "session_expired", "Session has expired")
)

type RefreshService struct {
	sessions SessionRepository
	users    UserRepository
	tokens   TokenManager
	cfg      SessionConfig
}

func NewRefreshService(
	sessions SessionRepository,
	users UserRepository,
	tokens TokenManager,
	cfg SessionConfig,
) *RefreshService {
	return &RefreshService{
		sessions: sessions,
		users:    users,
		tokens:   tokens,
		cfg:      cfg,
	}
}

type RefreshOutput struct {
	AccessToken          string
	AccessTokenExpiresAt time.Time
	RefreshToken         string
}

// Refresh validates the presented refresh token, then rotates it.
//
// Ordering matters here for two reasons:
//   - All fallible, non-DB operations (generating the new refresh
//     token and the new access token) happen before the database is
//     ever mutated, so a failure never leaves the old refresh token
//     invalidated without the caller receiving a replacement.
//   - The rotation itself is a compare-and-swap keyed on the hash we
//     just read: RotateRefreshToken only succeeds if that exact hash
//     is still current, is not revoked, and is not expired. This is
//     what prevents two concurrent requests presenting the same
//     refresh token from both succeeding.
func (s *RefreshService) Refresh(ctx context.Context, rawRefreshToken string) (RefreshOutput, error) {
	currentHash := s.tokens.HashRefreshToken(rawRefreshToken)

	session, err := s.sessions.FindByRefreshTokenHash(ctx, currentHash)
	if errors.Is(err, domain.ErrSessionNotFound) {
		return RefreshOutput{}, errInvalidRefreshToken
	}
	if err != nil {
		return RefreshOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"refresh_lookup_failed",
			"Failed to refresh session",
			err,
		)
	}

	now := time.Now().UTC()

	// These checks are for a fast, precise error in the common
	// (non-racing) case only. The authoritative check is the
	// conditional UPDATE in RotateRefreshToken below, which
	// re-validates all of this atomically at the moment of the write.
	if session.IsRevoked() {
		return RefreshOutput{}, errInvalidRefreshToken
	}
	if session.IsExpired(now) {
		return RefreshOutput{}, errSessionExpired
	}

	user, err := s.users.FindByID(ctx, session.UserID)
	if errors.Is(err, domain.ErrUserNotFound) {
		return RefreshOutput{}, errInvalidRefreshToken
	}
	if err != nil {
		return RefreshOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"refresh_user_lookup_failed",
			"Failed to refresh session",
			err,
		)
	}
	if !user.IsActive() {
		return RefreshOutput{}, errAccountDisabled
	}

	newRawRefreshToken, err := s.tokens.GenerateRefreshToken()
	if err != nil {
		return RefreshOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"refresh_token_generate_failed",
			"Failed to refresh session",
			err,
		)
	}

	// The session ID never changes across rotation, so the access
	// token can be generated now, before the database is touched.
	accessToken, accessTokenExpiresAt, err := s.tokens.GenerateAccessToken(session.UserID, session.ID)
	if err != nil {
		return RefreshOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"access_token_generate_failed",
			"Failed to refresh session",
			err,
		)
	}

	newHash := s.tokens.HashRefreshToken(newRawRefreshToken)
	newExpiresAt := now.Add(s.cfg.RefreshTokenTTL)

	if _, err := s.sessions.RotateRefreshToken(ctx, session.ID, currentHash, newHash, newExpiresAt); err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			return RefreshOutput{}, errInvalidRefreshToken
		}

		return RefreshOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"refresh_rotate_failed",
			"Failed to refresh session",
			err,
		)
	}

	return RefreshOutput{
		AccessToken:          accessToken,
		AccessTokenExpiresAt: accessTokenExpiresAt,
		RefreshToken:         newRawRefreshToken,
	}, nil
}
