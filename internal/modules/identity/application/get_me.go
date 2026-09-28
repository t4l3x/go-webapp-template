package application

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

var errUnauthorized = apperror.New(apperror.KindUnauthorized, "unauthorized", "Unauthorized")

type GetMeService struct {
	users UserRepository
}

func NewGetMeService(users UserRepository) *GetMeService {
	return &GetMeService{users: users}
}

func (s *GetMeService) GetMe(ctx context.Context, userID uuid.UUID) (UserView, error) {
	user, err := s.users.FindByID(ctx, userID)
	if errors.Is(err, domain.ErrUserNotFound) {
		return UserView{}, errUnauthorized
	}
	if err != nil {
		return UserView{}, apperror.Wrap(
			apperror.KindInternal,
			"get_me_failed",
			"Failed to load user",
			err,
		)
	}

	return newUserView(*user), nil
}
