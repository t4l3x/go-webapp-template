package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

const (
	usersEmailUniqueConstraint = "users_email_lower_key"
	usersPhoneUniqueConstraint = "users_phone_key"
)

// RegistrationStore commits the account, credential, and delivery request together.
type RegistrationStore struct {
	pool *pgxpool.Pool
}

func NewRegistrationStore(pool *pgxpool.Pool) *RegistrationStore {
	return &RegistrationStore{pool: pool}
}

func (s *RegistrationStore) Register(
	ctx context.Context,
	user *domain.User,
	verification *domain.EmailVerification,
	event application.EmailVerificationRequestedV1,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin registration transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once Commit has succeeded

	const insertUser = `
		INSERT INTO users (
			id, email, phone, password_hash, status,
			email_verified_at, phone_verified_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = tx.Exec(ctx, insertUser,
		user.ID, user.Email, user.Phone, user.PasswordHash, string(user.Status),
		user.EmailVerifiedAt, user.PhoneVerifiedAt, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case usersEmailUniqueConstraint:
				return domain.ErrEmailAlreadyExists
			case usersPhoneUniqueConstraint:
				return domain.ErrPhoneAlreadyExists
			}
		}

		return fmt.Errorf("insert user: %w", err)
	}

	if err := insertVerificationDelivery(ctx, tx, verification, event); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit registration transaction: %w", err)
	}

	return nil
}
