package application

import (
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

// UserView is the subset of a user's data that use cases may hand to
// transport (and, from there, to a client). It deliberately excludes
// PasswordHash and any other internal-only field, so a future
// transport change cannot accidentally serialize a credential by
// reusing domain.User directly.
type UserView struct {
	ID              uuid.UUID
	Email           string
	Phone           *string
	Status          domain.UserStatus
	EmailVerifiedAt *time.Time
	PhoneVerifiedAt *time.Time
	CreatedAt       time.Time
}

func newUserView(user domain.User) UserView {
	return UserView{
		ID:              user.ID,
		Email:           user.Email,
		Phone:           user.Phone,
		Status:          user.Status,
		EmailVerifiedAt: user.EmailVerifiedAt,
		PhoneVerifiedAt: user.PhoneVerifiedAt,
		CreatedAt:       user.CreatedAt,
	}
}
