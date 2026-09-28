package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/platform/outbox"
)

type VerificationStore struct{ pool *pgxpool.Pool }

func NewVerificationStore(pool *pgxpool.Pool) *VerificationStore {
	return &VerificationStore{pool: pool}
}

func (s *VerificationStore) CanDeliver(ctx context.Context, event application.EmailVerificationRequestedV1) (bool, error) {
	var eligible bool
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM email_verifications v
			JOIN users u ON u.id = v.user_id
			WHERE v.id = $1
				AND v.user_id = $2
				AND u.email = $3
				AND date_trunc('second', v.expires_at) = $4
				AND date_trunc('second', v.expires_at) > clock_timestamp()
				AND v.consumed_at IS NULL
				AND u.email_verified_at IS NULL
				AND u.status = 'active'
		)
	`
	err := s.pool.QueryRow(
		ctx,
		query,
		event.VerificationID,
		event.UserID,
		event.Email,
		event.ExpiresAt.Truncate(time.Second),
	).Scan(&eligible)
	if err != nil {
		return false, fmt.Errorf("query verification eligibility: %w", err)
	}
	return eligible, nil
}

// Consume and Replace both lock the user first, so a concurrent resend cannot
// leave a fresh credential behind after verification has completed.
func (s *VerificationStore) Consume(ctx context.Context, id uuid.UUID, expiresAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin verification: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var userID uuid.UUID
	var status string
	const lockUser = `
		SELECT u.id, u.status
		FROM users u
		JOIN email_verifications v ON v.user_id = u.id
		WHERE v.id = $1
		FOR UPDATE OF u
	`
	err = tx.QueryRow(ctx, lockUser, id).Scan(&userID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrInvalidEmailVerification
	}
	if err != nil {
		return fmt.Errorf("lock verification user: %w", err)
	}
	if status != string(domain.UserStatusActive) {
		return domain.ErrInvalidEmailVerification
	}
	const consumeVerification = `
		UPDATE email_verifications
		SET consumed_at = clock_timestamp()
		WHERE id = $1
			AND consumed_at IS NULL
			AND expires_at > clock_timestamp()
			AND date_trunc('second', expires_at) = $2
			AND $2 > clock_timestamp()
	`
	tag, err := tx.Exec(ctx, consumeVerification, id, expiresAt.Truncate(time.Second))
	if err != nil {
		return fmt.Errorf("consume verification: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrInvalidEmailVerification
	}
	const verifyUserEmail = `
		UPDATE users
		SET email_verified_at = clock_timestamp(),
			updated_at = clock_timestamp()
		WHERE id = $1
	`
	if _, err := tx.Exec(ctx, verifyUserEmail, userID); err != nil {
		return fmt.Errorf("verify user email: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit verification: %w", err)
	}
	return nil
}

// Replace snapshots the recipient under the same lock as credential replacement.
// A verified account needs no new delivery; every other successful write includes
// its outbox event in this transaction.
func (s *VerificationStore) Replace(ctx context.Context, verification *domain.EmailVerification) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin verification replacement: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var email, status string
	var verifiedAt *time.Time
	const lockRecipient = `
		SELECT email, status, email_verified_at
		FROM users
		WHERE id = $1
		FOR UPDATE
	`
	err = tx.QueryRow(ctx, lockRecipient, verification.UserID).Scan(&email, &status, &verifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("lock verification recipient: %w", err)
	}
	if status != string(domain.UserStatusActive) {
		return domain.ErrAccountDisabled
	}
	if verifiedAt != nil {
		return nil
	}
	const invalidatePrevious = `
		UPDATE email_verifications
		SET consumed_at = clock_timestamp()
		WHERE user_id = $1
			AND consumed_at IS NULL
	`
	if _, err := tx.Exec(ctx, invalidatePrevious, verification.UserID); err != nil {
		return fmt.Errorf("invalidate previous verification: %w", err)
	}
	event := application.EmailVerificationRequestedV1{
		UserID:         verification.UserID,
		Email:          email,
		VerificationID: verification.ID,
		ExpiresAt:      verification.ExpiresAt,
	}
	if err := insertVerificationDelivery(ctx, tx, verification, event); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit verification replacement: %w", err)
	}
	return nil
}

// insertVerificationDelivery is shared by registration and resend. The caller
// owns the transaction that makes the credential and delivery request inseparable.
func insertVerificationDelivery(ctx context.Context, tx pgx.Tx, verification *domain.EmailVerification, event application.EmailVerificationRequestedV1) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal verification delivery: %w", err)
	}
	const insertVerification = `
		INSERT INTO email_verifications (
			id,
			user_id,
			expires_at,
			consumed_at,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err = tx.Exec(
		ctx,
		insertVerification,
		verification.ID,
		verification.UserID,
		verification.ExpiresAt,
		verification.ConsumedAt,
		verification.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert email verification: %w", err)
	}
	if err := outbox.Insert(ctx, tx, application.EventTypeEmailVerificationRequestedV1, payload); err != nil {
		return fmt.Errorf("insert verification delivery: %w", err)
	}
	return nil
}
