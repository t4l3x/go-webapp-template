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
	errInvalidEmail       = apperror.New(apperror.KindValidation, "invalid_email", "Invalid email address")
	errInvalidPhone       = apperror.New(apperror.KindValidation, "invalid_phone", "Invalid phone number")
	errEmailAlreadyExists = apperror.New(apperror.KindConflict, "email_already_exists", "Email already exists")
	errPhoneAlreadyExists = apperror.New(apperror.KindConflict, "phone_already_exists", "Phone already exists")
)

type RegisterConfig struct {
	PasswordMinLength    int
	EmailVerificationTTL time.Duration
}

type RegisterService struct {
	store  RegistrationStore
	hasher PasswordHasher
	cfg    RegisterConfig
}

func NewRegisterService(
	store RegistrationStore,
	hasher PasswordHasher,
	cfg RegisterConfig,
) *RegisterService {
	return &RegisterService{
		store:  store,
		hasher: hasher,
		cfg:    cfg,
	}
}

type RegisterInput struct {
	Email    string
	Password string
	Phone    *string
}

type RegisterOutput struct {
	User UserView
}

// Register validates and hashes before atomically saving the user, verification,
// and delivery request. Success means delivery is queued, not that mail arrived.
func (s *RegisterService) Register(ctx context.Context, in RegisterInput) (RegisterOutput, error) {
	email, err := validation.NormalizeEmail(in.Email)
	if err != nil {
		return RegisterOutput{}, errInvalidEmail
	}

	var phone *string

	if in.Phone != nil {
		normalized, err := validation.NormalizeE164Phone(*in.Phone)
		if err != nil {
			return RegisterOutput{}, errInvalidPhone
		}

		phone = &normalized
	}

	if err := validatePasswordLength(in.Password, s.cfg.PasswordMinLength); err != nil {
		return RegisterOutput{}, err
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return RegisterOutput{}, apperror.Wrap(
			apperror.KindInternal,
			"password_hash_failed",
			"Failed to register user",
			err,
		)
	}

	user := domain.NewUser(email, phone, &hash)

	verification := domain.NewEmailVerification(user.ID, s.cfg.EmailVerificationTTL)

	event := EmailVerificationRequestedV1{
		UserID:         user.ID,
		Email:          user.Email,
		VerificationID: verification.ID,
		ExpiresAt:      verification.ExpiresAt,
	}

	if err := s.store.Register(ctx, user, verification, event); err != nil {
		switch {
		case errors.Is(err, domain.ErrEmailAlreadyExists):
			return RegisterOutput{}, errEmailAlreadyExists
		case errors.Is(err, domain.ErrPhoneAlreadyExists):
			return RegisterOutput{}, errPhoneAlreadyExists
		default:
			return RegisterOutput{}, apperror.Wrap(
				apperror.KindInternal,
				"registration_failed",
				"Failed to register user",
				err,
			)
		}
	}

	return RegisterOutput{User: newUserView(*user)}, nil
}
