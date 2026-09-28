package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

func newRegisterService() (*application.RegisterService, *fakeRegistrationStore) {
	store := newFakeRegistrationStore()
	svc := application.NewRegisterService(store, fakeHasher{}, application.RegisterConfig{
		PasswordMinLength:    12,
		EmailVerificationTTL: 24 * time.Hour,
	})

	return svc, store
}

func TestRegisterService_Register_Success(t *testing.T) {
	svc, store := newRegisterService()

	out, err := svc.Register(context.Background(), application.RegisterInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if out.User.Email != "user@example.com" {
		t.Fatalf("User.Email = %q, want %q", out.User.Email, "user@example.com")
	}
	if out.User.Status != domain.UserStatusActive {
		t.Fatalf("User.Status = %q, want %q", out.User.Status, domain.UserStatusActive)
	}

	// application.UserView has no PasswordHash field at all — this is a
	// compile-time guarantee, not something to assert at runtime.

	persisted, ok := store.findUserByEmail("user@example.com")
	if !ok {
		t.Fatalf("expected user to be persisted")
	}
	if persisted.PasswordHash == nil || *persisted.PasswordHash == "supersecretpassword" {
		t.Fatalf("persisted PasswordHash was not hashed: %v", persisted.PasswordHash)
	}
}

// TestRegisterService_Register_PersistsEmailVerificationAndEvent also
// documents a security property: neither the verification row nor the
// outbox event carries any token/secret material — only the
// verification's own id and its expiry. The worker derives the actual
// bearer token later, deterministically, from those two non-secret
// fields (see application.VerificationSigner) — Register itself never touches a
// token at all.
func TestRegisterService_Register_PersistsEmailVerificationAndEvent(t *testing.T) {
	svc, store := newRegisterService()

	out, err := svc.Register(context.Background(), application.RegisterInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if len(store.verifications) != 1 {
		t.Fatalf("verifications persisted = %d, want 1", len(store.verifications))
	}

	var verification *domain.EmailVerification
	for _, v := range store.verifications {
		verification = v
	}

	if verification.UserID != out.User.ID {
		t.Fatalf("verification.UserID = %v, want %v", verification.UserID, out.User.ID)
	}
	if verification.ConsumedAt != nil {
		t.Fatalf("a freshly registered verification must not already be consumed")
	}
	if !verification.ExpiresAt.After(time.Now()) {
		t.Fatalf("verification.ExpiresAt = %v, want a future time", verification.ExpiresAt)
	}

	if len(store.events) != 1 {
		t.Fatalf("events recorded = %d, want 1", len(store.events))
	}

	event := store.events[0]
	if event.UserID != out.User.ID {
		t.Fatalf("event.UserID = %v, want %v", event.UserID, out.User.ID)
	}
	if event.Email != "user@example.com" {
		t.Fatalf("event.Email = %q, want %q", event.Email, "user@example.com")
	}
	if event.VerificationID != verification.ID {
		t.Fatalf("event.VerificationID = %v, want %v (the persisted verification's own id)", event.VerificationID, verification.ID)
	}
	if !event.ExpiresAt.Equal(verification.ExpiresAt) {
		t.Fatalf("event.ExpiresAt = %v, want %v", event.ExpiresAt, verification.ExpiresAt)
	}
}

func TestRegisterService_Register_NormalizesEmail(t *testing.T) {
	svc, store := newRegisterService()

	out, err := svc.Register(context.Background(), application.RegisterInput{
		Email:    "  User@Example.COM  ",
		Password: "supersecretpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if out.User.Email != "user@example.com" {
		t.Fatalf("User.Email = %q, want normalized %q", out.User.Email, "user@example.com")
	}

	if _, ok := store.findUserByEmail("user@example.com"); !ok {
		t.Fatalf("expected normalized email to be persisted")
	}
}

func TestRegisterService_Register_DuplicateEmail(t *testing.T) {
	svc, _ := newRegisterService()
	ctx := context.Background()

	if _, err := svc.Register(ctx, application.RegisterInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
	}); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	_, err := svc.Register(ctx, application.RegisterInput{
		Email:    "USER@EXAMPLE.COM",
		Password: "anothersecretpassword",
	})

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindConflict {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindConflict)
	}
	if appErr.Code != "email_already_exists" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "email_already_exists")
	}
}

func TestRegisterService_Register_DuplicateEmail_LeavesNoOrphanedVerificationOrEvent(t *testing.T) {
	svc, store := newRegisterService()
	ctx := context.Background()

	if _, err := svc.Register(ctx, application.RegisterInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
	}); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	if _, err := svc.Register(ctx, application.RegisterInput{
		Email:    "USER@EXAMPLE.COM",
		Password: "anothersecretpassword",
	}); err == nil {
		t.Fatalf("second Register() error = nil, want a conflict")
	}

	// Exactly the first registration's user/verification/event must
	// exist — the rejected duplicate must not have left a trace of any
	// of the three (proving the atomic-write contract holds on the
	// failure path too, not just the success path).
	if store.count() != 1 {
		t.Fatalf("users persisted = %d, want 1", store.count())
	}
	if len(store.verifications) != 1 {
		t.Fatalf("verifications persisted = %d, want 1", len(store.verifications))
	}
	if len(store.events) != 1 {
		t.Fatalf("events recorded = %d, want 1", len(store.events))
	}
}

func TestRegisterService_Register_InvalidEmail(t *testing.T) {
	svc, _ := newRegisterService()

	_, err := svc.Register(context.Background(), application.RegisterInput{
		Email:    "not-an-email",
		Password: "supersecretpassword",
	})

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindValidation {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindValidation)
	}
	if appErr.Code != "invalid_email" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "invalid_email")
	}
}

func TestRegisterService_Register_InvalidPassword(t *testing.T) {
	svc, _ := newRegisterService()

	_, err := svc.Register(context.Background(), application.RegisterInput{
		Email:    "user@example.com",
		Password: "short",
	})

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindValidation {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindValidation)
	}
	if appErr.Code != "invalid_password" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "invalid_password")
	}
}

func TestRegisterService_Register_InvalidPhone(t *testing.T) {
	svc, _ := newRegisterService()

	invalidPhone := "not-a-phone"

	_, err := svc.Register(context.Background(), application.RegisterInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
		Phone:    &invalidPhone,
	})

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Code != "invalid_phone" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "invalid_phone")
	}
}

func TestRegisterService_Register_ValidPhone(t *testing.T) {
	svc, _ := newRegisterService()

	phone := "+1 415-555-0100"

	out, err := svc.Register(context.Background(), application.RegisterInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
		Phone:    &phone,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if out.User.Phone == nil || *out.User.Phone != "+14155550100" {
		t.Fatalf("User.Phone = %v, want normalized %q", out.User.Phone, "+14155550100")
	}
}

func TestRegisterService_Register_UnexpectedStoreErrorIsInternal(t *testing.T) {
	store := newFakeRegistrationStore()
	store.registerErr = errBoom

	svc := application.NewRegisterService(store, fakeHasher{}, application.RegisterConfig{
		PasswordMinLength:    12,
		EmailVerificationTTL: 24 * time.Hour,
	})

	_, err := svc.Register(context.Background(), application.RegisterInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
	})

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindInternal {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindInternal)
	}
	if appErr.Code != "registration_failed" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "registration_failed")
	}
}
