package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/platform/localization"
	"github.com/t4l3x/go-webapp-template/internal/platform/mail"
	"github.com/t4l3x/go-webapp-template/internal/validation"
)

var ErrInvalidVerificationDelivery = errors.New("invalid email verification delivery")

type DeliveryConfig struct{ AppPublicURL string }

const (
	verifyEmailPath              = "/verify-email"
	verificationSubjectMessageID = "identity.email_verification.subject"
	verificationBodyMessageID    = "identity.email_verification.body"
	expiresAtLayout              = "2006-01-02 15:04 UTC"
)

type DeliverEmailVerificationService struct {
	store     VerificationDeliveryStore
	sender    mail.Sender
	signer    VerificationSigner
	localizer localization.Localizer
	cfg       DeliveryConfig
}

func NewDeliverEmailVerificationService(
	store VerificationDeliveryStore,
	sender mail.Sender,
	signer VerificationSigner,
	localizer localization.Localizer,
	cfg DeliveryConfig,
) *DeliverEmailVerificationService {
	return &DeliverEmailVerificationService{
		store: store, sender: sender, signer: signer, localizer: localizer, cfg: cfg,
	}
}

// Deliver skips obsolete requests and renders only a current verification.
// Retries preserve the credential but may send duplicate emails. A concurrent
// resend/verification can invalidate a link after the eligibility check;
// validity is enforced again when the link is consumed, not by holding a DB
// transaction open during SMTP.
func (s *DeliverEmailVerificationService) Deliver(ctx context.Context, payload EmailVerificationRequestedV1) error {
	email, err := validation.NormalizeEmail(payload.Email)
	if err != nil || email != payload.Email || payload.UserID == uuid.Nil ||
		payload.VerificationID == uuid.Nil || payload.ExpiresAt.IsZero() {
		return ErrInvalidVerificationDelivery
	}
	if !payload.ExpiresAt.Truncate(time.Second).After(time.Now()) {
		return nil
	}
	eligible, err := s.store.CanDeliver(ctx, payload)
	if err != nil {
		return fmt.Errorf("check verification delivery: %w", err)
	}
	if !eligible {
		return nil
	}
	token := s.signer.SignVerificationToken(payload.VerificationID, payload.ExpiresAt)
	link := fmt.Sprintf("%s%s?token=%s", s.cfg.AppPublicURL, verifyEmailPath, url.QueryEscape(token))

	data := map[string]any{
		"VerificationURL": link,
		"ExpiresAt":       payload.ExpiresAt.UTC().Format(expiresAtLayout),
	}

	// No preferred locale is stored; use the configured default.
	var locale string

	subject, err := s.localizer.Translate(locale, localization.Message{
		ID:   verificationSubjectMessageID,
		Data: data,
	})
	if err != nil {
		return fmt.Errorf("localize verification email subject: %w", err)
	}

	body, err := s.localizer.Translate(locale, localization.Message{
		ID:   verificationBodyMessageID,
		Data: data,
	})
	if err != nil {
		return fmt.Errorf("localize verification email body: %w", err)
	}

	return s.sender.Send(ctx, mail.Message{
		To:      payload.Email,
		Subject: subject,
		Body:    body,
	})
}
