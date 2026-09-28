package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
)

type verificationStore struct {
	err         error
	consumed    uuid.UUID
	replacement *domain.EmailVerification
	policy      domain.ResendPolicy
}

func (s *verificationStore) Consume(_ context.Context, id uuid.UUID, _ time.Time) error {
	s.consumed = id
	return s.err
}
func (s *verificationStore) Replace(_ context.Context, v *domain.EmailVerification, policy domain.ResendPolicy) error {
	s.replacement = v
	s.policy = policy
	return s.err
}

func TestVerifyEmailRejectsInvalidTokensBeforePersistence(t *testing.T) {
	signer := security.NewVerificationSigner(security.VerificationTokenConfig{Secret: strings.Repeat("s", 32)})
	valid := signer.SignVerificationToken(uuid.New(), time.Now().Add(time.Hour))
	for _, token := range []string{"", "bad", valid + "tampered", strings.Repeat("x", 513), signer.SignVerificationToken(uuid.New(), time.Now().Add(-time.Hour))} {
		store := &verificationStore{}
		err := application.NewVerifyEmailService(store, signer).Verify(context.Background(), token)
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.Code != "invalid_email_verification" || store.consumed != uuid.Nil {
			t.Fatalf("invalid token reached persistence: %v", err)
		}
	}
}

func TestVerifyEmailMapsPersistenceOutcome(t *testing.T) {
	signer := security.NewVerificationSigner(security.VerificationTokenConfig{Secret: strings.Repeat("s", 32)})
	id := uuid.New()
	token := signer.SignVerificationToken(id, time.Now().Add(time.Hour))
	for _, tc := range []struct {
		cause error
		code  string
	}{{nil, ""}, {domain.ErrInvalidEmailVerification, "invalid_email_verification"}, {errBoom, "email_verification_failed"}} {
		store := &verificationStore{err: tc.cause}
		err := application.NewVerifyEmailService(store, signer).Verify(context.Background(), token)
		if store.consumed != id {
			t.Fatal("wrong credential consumed")
		}
		if tc.code == "" {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.Code != tc.code {
			t.Fatalf("error=%v want=%s", err, tc.code)
		}
	}
}

func TestResendVerificationPassesPolicyAndMapsLimit(t *testing.T) {
	policy := domain.ResendPolicy{Cooldown: time.Minute, MaxPerWindow: 5}
	store := &verificationStore{err: &domain.ResendLimitError{Wait: 42 * time.Second}}

	err := application.NewResendEmailVerificationService(store, application.ResendVerificationConfig{TTL: time.Hour, Policy: policy}).
		Resend(context.Background(), uuid.New())

	if store.policy != policy {
		t.Fatalf("store got policy %+v, want %+v", store.policy, policy)
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperror.KindTooManyRequests || appErr.Code != "email_verification_resend_limited" {
		t.Fatalf("error = %v, want too_many_requests/email_verification_resend_limited", err)
	}
	var limited *domain.ResendLimitError
	if !errors.As(err, &limited) || limited.RetryAfter() != 42*time.Second {
		t.Fatalf("retry delay not preserved through the application error: %v", err)
	}
}

func TestResendVerificationCreatesCredentialAndMapsFailure(t *testing.T) {
	for _, tc := range []struct {
		cause error
		code  string
	}{{nil, ""}, {domain.ErrUserNotFound, "unauthorized"}, {domain.ErrAccountDisabled, "account_disabled"}, {errBoom, "email_verification_resend_failed"}} {
		store := &verificationStore{err: tc.cause}
		id := uuid.New()
		err := application.NewResendEmailVerificationService(store, application.ResendVerificationConfig{TTL: time.Hour}).Resend(context.Background(), id)
		if store.replacement == nil || store.replacement.UserID != id || store.replacement.ID == uuid.Nil || !store.replacement.ExpiresAt.After(time.Now()) {
			t.Fatal("invalid replacement")
		}
		if tc.code == "" {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.Code != tc.code {
			t.Fatalf("error=%v want=%s", err, tc.code)
		}
	}
}
