package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

// ip_address is stored as inet; host() returns it as plain text with
// no netmask suffix, so it scans cleanly into a plain *string without
// depending on pgx's inet type mapping.
const selectSessionColumns = `
	id, user_id, refresh_token_hash, expires_at, revoked_at,
	last_seen_at, user_agent, host(ip_address), created_at, updated_at
`

// SessionRepository implements application.SessionRepository against
// PostgreSQL using pgx. pgx types never leave this package.
type SessionRepository struct {
	pool *pgxpool.Pool
}

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

func (r *SessionRepository) Create(ctx context.Context, session *domain.Session) error {
	const query = `
		INSERT INTO auth_sessions (
			id, user_id, refresh_token_hash, expires_at, revoked_at,
			last_seen_at, user_agent, ip_address, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::inet, $9, $10)
	`

	_, err := r.pool.Exec(
		ctx,
		query,
		session.ID,
		session.UserID,
		session.RefreshTokenHash,
		session.ExpiresAt,
		session.RevokedAt,
		session.LastSeenAt,
		session.UserAgent,
		session.IPAddress,
		session.CreatedAt,
		session.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}

	return nil
}

func (r *SessionRepository) FindByRefreshTokenHash(ctx context.Context, hash string) (*domain.Session, error) {
	query := fmt.Sprintf(`SELECT %s FROM auth_sessions WHERE refresh_token_hash = $1`, selectSessionColumns)

	return scanSession(r.pool.QueryRow(ctx, query, hash))
}

// FindByID is not part of application.SessionRepository — no use case
// needs to look up a session by ID alone. It is kept here only because
// integration tests (which use this concrete type directly, not the
// interface) need a way to inspect session state after a mutation.
func (r *SessionRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Session, error) {
	query := fmt.Sprintf(`SELECT %s FROM auth_sessions WHERE id = $1`, selectSessionColumns)

	return scanSession(r.pool.QueryRow(ctx, query, id))
}

func (r *SessionRepository) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	const query = `
		UPDATE auth_sessions
		SET revoked_at = $2, updated_at = $2
		WHERE id = $1 AND revoked_at IS NULL
	`

	if _, err := r.pool.Exec(ctx, query, sessionID, time.Now().UTC()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	return nil
}

// RotateRefreshToken performs the rotation as a single conditional
// UPDATE: the WHERE clause requires the row to still carry
// currentRefreshTokenHash and to be neither revoked nor expired. This
// is an atomic compare-and-swap — PostgreSQL serializes any concurrent
// UPDATE against the same row, so if two callers race with the same
// current hash, only the first to commit can match the WHERE clause;
// the second necessarily re-evaluates against the already-rotated row
// and affects zero rows. No explicit transaction or row lock is
// needed because a single statement is inherently atomic.
func (r *SessionRepository) RotateRefreshToken(
	ctx context.Context,
	sessionID uuid.UUID,
	currentRefreshTokenHash string,
	newRefreshTokenHash string,
	newExpiresAt time.Time,
) (*domain.Session, error) {
	now := time.Now().UTC()

	query := fmt.Sprintf(`
		UPDATE auth_sessions
		SET refresh_token_hash = $1, expires_at = $2, last_seen_at = $3, updated_at = $3
		WHERE id = $4
			AND refresh_token_hash = $5
			AND revoked_at IS NULL
			AND expires_at > $3
		RETURNING %s
	`, selectSessionColumns)

	return scanSession(r.pool.QueryRow(
		ctx, query,
		newRefreshTokenHash, newExpiresAt, now, sessionID, currentRefreshTokenHash,
	))
}

func scanSession(row rowScanner) (*domain.Session, error) {
	var session domain.Session

	err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.RefreshTokenHash,
		&session.ExpiresAt,
		&session.RevokedAt,
		&session.LastSeenAt,
		&session.UserAgent,
		&session.IPAddress,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}

	return &session, nil
}
