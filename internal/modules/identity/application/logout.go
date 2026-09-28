package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
)

type LogoutService struct {
	sessions SessionRepository
}

func NewLogoutService(sessions SessionRepository) *LogoutService {
	return &LogoutService{sessions: sessions}
}

func (s *LogoutService) Logout(ctx context.Context, sessionID uuid.UUID) error {
	if err := s.sessions.Revoke(ctx, sessionID); err != nil {
		return apperror.Wrap(
			apperror.KindInternal,
			"logout_failed",
			"Failed to logout",
			err,
		)
	}

	return nil
}
