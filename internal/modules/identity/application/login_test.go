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

type loginFixture struct {
	login    *application.LoginService
	users    *fakeUserRepository
	sessions *fakeSessionRepository
}

func newLoginFixture() loginFixture {
	users := newFakeUserRepository()
	sessions := newFakeSessionRepository()

	login := application.NewLoginService(
		users,
		sessions,
		fakeHasher{},
		newFakeTokenManager(15*time.Minute),
		application.SessionConfig{RefreshTokenTTL: 720 * time.Hour},
	)

	return loginFixture{login: login, users: users, sessions: sessions}
}

func mustRegisterUser(t *testing.T, users *fakeUserRepository, email, password string) *domain.User {
	t.Helper()

	hash, err := fakeHasher{}.Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	user := domain.NewUser(email, nil, &hash)

	if err := users.Create(context.Background(), user); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	return user
}

func TestLoginService_Login_Success(t *testing.T) {
	fx := newLoginFixture()
	mustRegisterUser(t, fx.users, "user@example.com", "supersecretpassword")

	out, err := fx.login.Login(context.Background(), application.LoginInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if out.AccessToken == "" {
		t.Fatalf("AccessToken is empty")
	}
	if out.RefreshToken == "" {
		t.Fatalf("RefreshToken is empty")
	}

	if _, err := fx.sessions.FindByRefreshTokenHash(
		context.Background(),
		"hash:"+out.RefreshToken,
	); err != nil {
		t.Fatalf("expected session to be persisted, FindByRefreshTokenHash() error = %v", err)
	}
}

func TestLoginService_Login_UnknownEmail(t *testing.T) {
	fx := newLoginFixture()

	_, err := fx.login.Login(context.Background(), application.LoginInput{
		Email:    "missing@example.com",
		Password: "supersecretpassword",
	})

	assertInvalidCredentials(t, err)
}

func TestLoginService_Login_WrongPassword(t *testing.T) {
	fx := newLoginFixture()
	mustRegisterUser(t, fx.users, "user@example.com", "supersecretpassword")

	_, err := fx.login.Login(context.Background(), application.LoginInput{
		Email:    "user@example.com",
		Password: "wrongpassword",
	})

	assertInvalidCredentials(t, err)
}

func TestLoginService_Login_UnknownEmailAndWrongPasswordProduceSameError(t *testing.T) {
	fx := newLoginFixture()
	mustRegisterUser(t, fx.users, "user@example.com", "supersecretpassword")

	_, unknownEmailErr := fx.login.Login(context.Background(), application.LoginInput{
		Email:    "missing@example.com",
		Password: "supersecretpassword",
	})

	_, wrongPasswordErr := fx.login.Login(context.Background(), application.LoginInput{
		Email:    "user@example.com",
		Password: "wrongpassword",
	})

	var unknownAppErr, wrongAppErr *apperror.Error
	if !errors.As(unknownEmailErr, &unknownAppErr) || !errors.As(wrongPasswordErr, &wrongAppErr) {
		t.Fatalf("expected both errors to be *apperror.Error")
	}

	if unknownAppErr.Code != wrongAppErr.Code {
		t.Fatalf("codes differ: %q vs %q, want identical", unknownAppErr.Code, wrongAppErr.Code)
	}
	if unknownAppErr.Message != wrongAppErr.Message {
		t.Fatalf("messages differ: %q vs %q, want identical", unknownAppErr.Message, wrongAppErr.Message)
	}
}

func TestLoginService_Login_DisabledAccount(t *testing.T) {
	fx := newLoginFixture()
	user := mustRegisterUser(t, fx.users, "user@example.com", "supersecretpassword")

	fx.users.mu.Lock()
	fx.users.users[user.ID].Status = domain.UserStatusDisabled
	fx.users.mu.Unlock()

	_, err := fx.login.Login(context.Background(), application.LoginInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
	})

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindForbidden {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindForbidden)
	}
	if appErr.Code != "account_disabled" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "account_disabled")
	}
}

func TestLoginService_Login_NilPasswordHashUser(t *testing.T) {
	fx := newLoginFixture()

	// Simulates a user provisioned via an external identity provider
	// (e.g. Google/Apple) who has never set a password.
	user := domain.NewUser("oauth@example.com", nil, nil)
	if err := fx.users.Create(context.Background(), user); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := fx.login.Login(context.Background(), application.LoginInput{
		Email:    "oauth@example.com",
		Password: "whatever-they-typed",
	})

	assertInvalidCredentials(t, err)
}

func TestLoginService_Login_AccessTokenFailureLeavesNoOrphanedSession(t *testing.T) {
	users := newFakeUserRepository()
	sessions := newFakeSessionRepository()
	tokens := newFakeTokenManager(15 * time.Minute)
	tokens.generateAccessTokenErr = errBoom

	login := application.NewLoginService(users, sessions, fakeHasher{}, tokens, application.SessionConfig{RefreshTokenTTL: time.Hour})
	mustRegisterUser(t, users, "user@example.com", "supersecretpassword")

	if _, err := login.Login(context.Background(), application.LoginInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
	}); err == nil {
		t.Fatalf("Login() error = nil, want error")
	}

	if got := sessions.count(); got != 0 {
		t.Fatalf("sessions created = %d, want 0 after access token generation failed", got)
	}
}

func TestLoginService_Login_RefreshTokenFailureLeavesNoOrphanedSession(t *testing.T) {
	users := newFakeUserRepository()
	sessions := newFakeSessionRepository()
	tokens := newFakeTokenManager(15 * time.Minute)
	tokens.generateRefreshTokenErr = errBoom

	login := application.NewLoginService(users, sessions, fakeHasher{}, tokens, application.SessionConfig{RefreshTokenTTL: time.Hour})
	mustRegisterUser(t, users, "user@example.com", "supersecretpassword")

	if _, err := login.Login(context.Background(), application.LoginInput{
		Email:    "user@example.com",
		Password: "supersecretpassword",
	}); err == nil {
		t.Fatalf("Login() error = nil, want error")
	}

	if got := sessions.count(); got != 0 {
		t.Fatalf("sessions created = %d, want 0 after refresh token generation failed", got)
	}
}

func assertInvalidCredentials(t *testing.T, err error) {
	t.Helper()

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindUnauthorized {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindUnauthorized)
	}
	if appErr.Code != "invalid_credentials" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "invalid_credentials")
	}
}
