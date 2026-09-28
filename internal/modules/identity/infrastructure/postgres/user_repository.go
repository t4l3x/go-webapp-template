package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

// UserRepository implements application.UserRepository against
// PostgreSQL using pgx. pgx types never leave this package.
//
// It is read-only: user creation is not a standalone operation in this
// module — it must commit atomically with an initial email-verification
// credential and an outbox event (see application.RegistrationStore /
// RegistrationStore in registration_store.go), so it has no place on a
// plain per-entity repository.
type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	const query = `
		SELECT id, email, phone, password_hash, status,
			email_verified_at, phone_verified_at, created_at, updated_at
		FROM users
		WHERE lower(email) = lower($1)
	`

	return scanUser(r.pool.QueryRow(ctx, query, email))
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	const query = `
		SELECT id, email, phone, password_hash, status,
			email_verified_at, phone_verified_at, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	return scanUser(r.pool.QueryRow(ctx, query, id))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*domain.User, error) {
	var (
		user   domain.User
		status string
	)

	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.Phone,
		&user.PasswordHash,
		&status,
		&user.EmailVerifiedAt,
		&user.PhoneVerifiedAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}

	user.Status = domain.UserStatus(status)

	return &user, nil
}
