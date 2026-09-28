//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/postgres"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestUserRepository_FindByEmail(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	repo := postgres.NewUserRepository(pool)

	user := mustCreateUser(t, pool)

	found, err := repo.FindByEmail(context.Background(), "USER@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("FindByEmail() error = %v", err)
	}
	if found.ID != user.ID {
		t.Fatalf("ID = %v, want %v", found.ID, user.ID)
	}
	if found.Email != "user@example.com" {
		t.Fatalf("Email = %q, want %q", found.Email, "user@example.com")
	}
}

func TestUserRepository_FindByEmail_NotFound(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	repo := postgres.NewUserRepository(pool)

	_, err := repo.FindByEmail(context.Background(), "missing@example.com")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("FindByEmail() error = %v, want %v", err, domain.ErrUserNotFound)
	}
}

func TestUserRepository_FindByID(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	repo := postgres.NewUserRepository(pool)

	user := mustCreateUser(t, pool)

	found, err := repo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.Email != user.Email {
		t.Fatalf("Email = %q, want %q", found.Email, user.Email)
	}
}

func TestUserRepository_FindByID_NotFound(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	repo := postgres.NewUserRepository(pool)

	_, err := repo.FindByID(context.Background(), domain.NewUser("unused@example.com", nil, nil).ID)
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("FindByID() error = %v, want %v", err, domain.ErrUserNotFound)
	}
}
