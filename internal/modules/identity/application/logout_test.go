package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

func TestLogoutService_Logout_RevokesSession(t *testing.T) {
	sessions := newFakeSessionRepository()
	logout := application.NewLogoutService(sessions)

	session := domain.NewSession(uuid.New(), "hash", time.Now().UTC().Add(time.Hour), nil, nil)
	if err := sessions.Create(context.Background(), session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := logout.Logout(context.Background(), session.ID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	stored, err := sessions.FindByID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if !stored.IsRevoked() {
		t.Fatalf("expected session to be revoked")
	}
}

func TestLogoutService_Logout_UnknownSessionIsIdempotent(t *testing.T) {
	sessions := newFakeSessionRepository()
	logout := application.NewLogoutService(sessions)

	if err := logout.Logout(context.Background(), uuid.New()); err != nil {
		t.Fatalf("Logout() error = %v, want no error for unknown session", err)
	}
}
