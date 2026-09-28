//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/postgres"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

// mustCreateUser inserts a user row directly via SQL rather than
// through a repository — UserRepository is read-only (user creation
// lives behind application.RegistrationStore, which also atomically
// writes an email-verification credential and outbox event these
// session tests have no need for).
func mustCreateUser(t *testing.T, pool *pgxpool.Pool) *domain.User {
	t.Helper()

	hash := "hashed-password"
	user := domain.NewUser("user@example.com", nil, &hash)

	const query = `
		INSERT INTO users (
			id, email, phone, password_hash, status,
			email_verified_at, phone_verified_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := pool.Exec(context.Background(), query,
		user.ID, user.Email, user.Phone, user.PasswordHash, string(user.Status),
		user.EmailVerifiedAt, user.PhoneVerifiedAt, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}

	return user
}

func TestSessionRepository_CreateAndFindByRefreshTokenHash(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	sessions := postgres.NewSessionRepository(pool)
	ctx := context.Background()

	user := mustCreateUser(t, pool)

	userAgent := "test-agent"
	ip := "203.0.113.10"
	session := domain.NewSession(user.ID, "refresh-hash", time.Now().UTC().Add(time.Hour), &userAgent, &ip)

	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	found, err := sessions.FindByRefreshTokenHash(ctx, "refresh-hash")
	if err != nil {
		t.Fatalf("FindByRefreshTokenHash() error = %v", err)
	}
	if found.ID != session.ID {
		t.Fatalf("ID = %v, want %v", found.ID, session.ID)
	}
	if found.IPAddress == nil {
		t.Fatalf("IPAddress = nil, want %q", ip)
	}
	if *found.IPAddress != ip {
		t.Fatalf("IPAddress = %q, want %q", *found.IPAddress, ip)
	}
	if found.UserAgent == nil || *found.UserAgent != userAgent {
		t.Fatalf("UserAgent = %v, want %q", found.UserAgent, userAgent)
	}
}

func TestSessionRepository_FindByRefreshTokenHash_NotFound(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	sessions := postgres.NewSessionRepository(pool)

	_, err := sessions.FindByRefreshTokenHash(context.Background(), "missing-hash")
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("FindByRefreshTokenHash() error = %v, want %v", err, domain.ErrSessionNotFound)
	}
}

func TestSessionRepository_Revoke(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	sessions := postgres.NewSessionRepository(pool)
	ctx := context.Background()

	user := mustCreateUser(t, pool)
	session := domain.NewSession(user.ID, "refresh-hash", time.Now().UTC().Add(time.Hour), nil, nil)

	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := sessions.Revoke(ctx, session.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	found, err := sessions.FindByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if !found.IsRevoked() {
		t.Fatalf("expected session to be revoked")
	}
}

func TestSessionRepository_RotateRefreshToken_Success(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	sessions := postgres.NewSessionRepository(pool)
	ctx := context.Background()

	user := mustCreateUser(t, pool)
	session := domain.NewSession(user.ID, "old-hash", time.Now().UTC().Add(time.Hour), nil, nil)

	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	newExpiresAt := time.Now().UTC().Add(2 * time.Hour)

	rotated, err := sessions.RotateRefreshToken(ctx, session.ID, "old-hash", "new-hash", newExpiresAt)
	if err != nil {
		t.Fatalf("RotateRefreshToken() error = %v", err)
	}
	if rotated.RefreshTokenHash != "new-hash" {
		t.Fatalf("RefreshTokenHash = %q, want %q", rotated.RefreshTokenHash, "new-hash")
	}
	if rotated.LastSeenAt == nil {
		t.Fatalf("expected LastSeenAt to be set")
	}

	if _, err := sessions.FindByRefreshTokenHash(ctx, "old-hash"); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("old hash should no longer resolve, error = %v", err)
	}
}

func TestSessionRepository_RotateRefreshToken_StaleHashRejected(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	sessions := postgres.NewSessionRepository(pool)
	ctx := context.Background()

	user := mustCreateUser(t, pool)
	session := domain.NewSession(user.ID, "old-hash", time.Now().UTC().Add(time.Hour), nil, nil)

	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := sessions.RotateRefreshToken(ctx, session.ID, "wrong-current-hash", "new-hash", time.Now().UTC().Add(time.Hour))
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("RotateRefreshToken() with a stale hash error = %v, want %v", err, domain.ErrSessionNotFound)
	}

	// The row must be entirely untouched by the rejected attempt.
	unchanged, err := sessions.FindByRefreshTokenHash(ctx, "old-hash")
	if err != nil {
		t.Fatalf("FindByRefreshTokenHash() error = %v, want the original hash to still resolve", err)
	}
	if unchanged.ID != session.ID {
		t.Fatalf("ID = %v, want %v", unchanged.ID, session.ID)
	}
}

func TestSessionRepository_RotateRefreshToken_RevokedSession(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	sessions := postgres.NewSessionRepository(pool)
	ctx := context.Background()

	user := mustCreateUser(t, pool)
	session := domain.NewSession(user.ID, "old-hash", time.Now().UTC().Add(time.Hour), nil, nil)

	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := sessions.Revoke(ctx, session.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	_, err := sessions.RotateRefreshToken(ctx, session.ID, "old-hash", "new-hash", time.Now().UTC().Add(time.Hour))
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("RotateRefreshToken() on a revoked session error = %v, want %v", err, domain.ErrSessionNotFound)
	}
}

func TestSessionRepository_RotateRefreshToken_ExpiredSession(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	sessions := postgres.NewSessionRepository(pool)
	ctx := context.Background()

	user := mustCreateUser(t, pool)
	session := domain.NewSession(user.ID, "old-hash", time.Now().UTC().Add(-time.Hour), nil, nil)

	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := sessions.RotateRefreshToken(ctx, session.ID, "old-hash", "new-hash", time.Now().UTC().Add(time.Hour))
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("RotateRefreshToken() on an expired session error = %v, want %v", err, domain.ErrSessionNotFound)
	}
}

// TestSessionRepository_RotateRefreshToken_ConcurrentSameTokenOnlyOneWins
// is the real proof that rotation is race-safe against PostgreSQL: two
// goroutines call RotateRefreshToken concurrently with the same current
// hash, released together via a barrier channel (never a sleep).
// Exactly one must win, and the row must end up holding exactly that
// winner's new hash.
func TestSessionRepository_RotateRefreshToken_ConcurrentSameTokenOnlyOneWins(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	sessions := postgres.NewSessionRepository(pool)
	ctx := context.Background()

	user := mustCreateUser(t, pool)
	session := domain.NewSession(user.ID, "shared-old-hash", time.Now().UTC().Add(time.Hour), nil, nil)

	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	const attempts = 2

	newHashes := make([]string, attempts)
	for i := range newHashes {
		newHashes[i] = fmt.Sprintf("new-hash-%d", i)
	}

	start := make(chan struct{})
	results := make(chan error, attempts)

	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := sessions.RotateRefreshToken(ctx, session.ID, "shared-old-hash", newHashes[i], time.Now().UTC().Add(2*time.Hour))
			results <- err
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	successCount := 0
	for err := range results {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, domain.ErrSessionNotFound):
			// expected for the loser
		default:
			t.Fatalf("unexpected error = %v, want nil or %v", err, domain.ErrSessionNotFound)
		}
	}

	if successCount != 1 {
		t.Fatalf("successCount = %d, want exactly 1", successCount)
	}

	final, err := sessions.FindByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}

	if final.RefreshTokenHash != newHashes[0] && final.RefreshTokenHash != newHashes[1] {
		t.Fatalf("final RefreshTokenHash = %q, want one of %v", final.RefreshTokenHash, newHashes)
	}
}
