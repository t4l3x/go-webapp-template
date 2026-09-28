package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
)

func TestGetMeService_GetMe_Success(t *testing.T) {
	users := newFakeUserRepository()
	getMe := application.NewGetMeService(users)

	user := mustRegisterUser(t, users, "user@example.com", "password")

	got, err := getMe.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetMe() error = %v", err)
	}

	if got.Email != "user@example.com" {
		t.Fatalf("Email = %q, want %q", got.Email, "user@example.com")
	}
}

func TestGetMeService_GetMe_UnknownUser(t *testing.T) {
	users := newFakeUserRepository()
	getMe := application.NewGetMeService(users)

	_, err := getMe.GetMe(context.Background(), uuid.New())

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindUnauthorized {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindUnauthorized)
	}
	if appErr.Code != "unauthorized" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "unauthorized")
	}
}
