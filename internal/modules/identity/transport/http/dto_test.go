package http

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

func TestExpiresIn_PositiveDuration(t *testing.T) {
	got := expiresIn(time.Now().Add(15 * time.Minute))

	// Allow a small margin for the time elapsed during the test itself.
	if got <= 14*60 || got > 15*60 {
		t.Fatalf("expiresIn() = %d, want a value close to %d seconds", got, 15*60)
	}
}

func TestExpiresIn_PastTimeReturnsZero(t *testing.T) {
	got := expiresIn(time.Now().Add(-time.Hour))

	if got != 0 {
		t.Fatalf("expiresIn() = %d, want 0 for a time already in the past", got)
	}
}

func TestNewUserResponse_MapsFields(t *testing.T) {
	userID := uuid.New()
	phone := "+15551234567"
	emailVerifiedAt := time.Now().Add(-time.Hour)
	createdAt := time.Now().Add(-24 * time.Hour)

	user := application.UserView{
		ID:              userID,
		Email:           "user@example.com",
		Phone:           &phone,
		Status:          domain.UserStatusDisabled,
		EmailVerifiedAt: &emailVerifiedAt,
		PhoneVerifiedAt: nil,
		CreatedAt:       createdAt,
	}

	got := newUserResponse(user)

	if got.Id != userID {
		t.Fatalf("Id = %v, want %v", got.Id, userID)
	}
	if got.Email != "user@example.com" {
		t.Fatalf("Email = %q, want %q", got.Email, "user@example.com")
	}
	if got.Phone == nil || *got.Phone != phone {
		t.Fatalf("Phone = %v, want %q", got.Phone, phone)
	}
	if string(got.Status) != string(domain.UserStatusDisabled) {
		t.Fatalf("Status = %q, want %q", got.Status, domain.UserStatusDisabled)
	}
	if !got.EmailVerified {
		t.Fatalf("EmailVerified = false, want true")
	}
	if got.PhoneVerified {
		t.Fatalf("PhoneVerified = true, want false")
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, createdAt)
	}
}

func TestNewRegisterResponse_WrapsUser(t *testing.T) {
	user := application.UserView{ID: uuid.New(), Email: "user@example.com"}

	got := newRegisterResponse(application.RegisterOutput{User: user})

	if got.User.Email != "user@example.com" {
		t.Fatalf("User.Email = %q, want %q", got.User.Email, "user@example.com")
	}
}

func TestNewTokenResponse_MapsFields(t *testing.T) {
	user := application.UserView{ID: uuid.New(), Email: "user@example.com"}

	out := application.LoginOutput{
		User:                 user,
		AccessToken:          "access-token",
		AccessTokenExpiresAt: time.Now().Add(15 * time.Minute),
		RefreshToken:         "refresh-token",
	}

	got := newTokenResponse(out)

	if got.AccessToken != "access-token" {
		t.Fatalf("AccessToken = %q, want %q", got.AccessToken, "access-token")
	}
	if got.RefreshToken != "refresh-token" {
		t.Fatalf("RefreshToken = %q, want %q", got.RefreshToken, "refresh-token")
	}
	if got.TokenType != "Bearer" {
		t.Fatalf("TokenType = %q, want %q", got.TokenType, "Bearer")
	}
	if got.ExpiresIn <= 0 {
		t.Fatalf("ExpiresIn = %d, want > 0", got.ExpiresIn)
	}
	if got.User.Email != "user@example.com" {
		t.Fatalf("User.Email = %q, want %q", got.User.Email, "user@example.com")
	}
}

func TestNewRefreshResponse_MapsFields(t *testing.T) {
	out := application.RefreshOutput{
		AccessToken:          "access-token",
		AccessTokenExpiresAt: time.Now().Add(15 * time.Minute),
		RefreshToken:         "refresh-token",
	}

	got := newRefreshResponse(out)

	if got.AccessToken != "access-token" {
		t.Fatalf("AccessToken = %q, want %q", got.AccessToken, "access-token")
	}
	if got.RefreshToken != "refresh-token" {
		t.Fatalf("RefreshToken = %q, want %q", got.RefreshToken, "refresh-token")
	}
	if got.TokenType != "Bearer" {
		t.Fatalf("TokenType = %q, want %q", got.TokenType, "Bearer")
	}
	if got.ExpiresIn <= 0 {
		t.Fatalf("ExpiresIn = %d, want > 0", got.ExpiresIn)
	}
}
