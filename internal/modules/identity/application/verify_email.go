package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

var errInvalidVerification = apperror.New(apperror.KindValidation, "invalid_email_verification", "Email verification link is invalid or expired")

type VerifyEmailService struct {
	store    VerificationStore
	verifier VerificationTokenVerifier
}

func NewVerifyEmailService(store VerificationStore, verifier VerificationTokenVerifier) *VerifyEmailService {
	return &VerifyEmailService{store: store, verifier: verifier}
}

func (s *VerifyEmailService) Verify(ctx context.Context, token string) error {
	if len(token) == 0 || len(token) > 512 {
		return errInvalidVerification
	}
	id, expiresAt, err := s.verifier.VerifyVerificationToken(token)
	if err != nil || id == uuid.Nil || !expiresAt.After(time.Now()) {
		return errInvalidVerification
	}
	err = s.store.Consume(ctx, id, expiresAt)
	if errors.Is(err, domain.ErrInvalidEmailVerification) {
		return errInvalidVerification
	}
	if err != nil {
		return apperror.Wrap(apperror.KindInternal, "email_verification_failed", "Failed to verify email", err)
	}
	return nil
}
