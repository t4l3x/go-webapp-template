package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

type ResendVerificationConfig struct{ TTL time.Duration }

type ResendEmailVerificationService struct {
	store VerificationStore
	cfg   ResendVerificationConfig
}

func NewResendEmailVerificationService(store VerificationStore, cfg ResendVerificationConfig) *ResendEmailVerificationService {
	return &ResendEmailVerificationService{store: store, cfg: cfg}
}

// Resend is available only to the authenticated account owner. Verified accounts
// are a successful no-op. Replacements invalidate older links atomically.
func (s *ResendEmailVerificationService) Resend(ctx context.Context, userID uuid.UUID) error {
	err := s.store.Replace(ctx, domain.NewEmailVerification(userID, s.cfg.TTL))
	if errors.Is(err, domain.ErrUserNotFound) {
		return errUnauthorized
	}
	if errors.Is(err, domain.ErrAccountDisabled) {
		return errAccountDisabled
	}
	if err != nil {
		return apperror.Wrap(apperror.KindInternal, "email_verification_resend_failed", "Failed to request verification email", err)
	}
	return nil
}
