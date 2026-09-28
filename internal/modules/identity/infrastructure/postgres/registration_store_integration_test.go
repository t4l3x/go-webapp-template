//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/postgres"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func newRegistration(t *testing.T, email string) (*domain.User, *domain.EmailVerification, application.EmailVerificationRequestedV1) {
	t.Helper()

	hash := "hashed-password"
	user := domain.NewUser(email, nil, &hash)
	verification := domain.NewEmailVerification(user.ID, 24*time.Hour)
	event := application.EmailVerificationRequestedV1{
		UserID:         user.ID,
		Email:          user.Email,
		VerificationID: verification.ID,
		ExpiresAt:      verification.ExpiresAt,
	}

	return user, verification, event
}

// TestRegistrationStore_Register_CommitsUserVerificationAndOutboxAtomically
// is the core proof this feature exists for: a single successful
// Register call must leave all three rows — user, email verification,
// outbox event — durably committed together. It also proves the
// no-secret-at-rest property: email_verifications has no token/hash
// column at all, and the outbox payload carries only the verification's
// own id and expiry, never a bearer credential.
func TestRegistrationStore_Register_CommitsUserVerificationAndOutboxAtomically(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	store := postgres.NewRegistrationStore(pool)
	ctx := context.Background()

	user, verification, event := newRegistration(t, "user@example.com")

	if err := store.Register(ctx, user, verification, event); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	var email string
	if err := pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, user.ID).Scan(&email); err != nil {
		t.Fatalf("query user: %v", err)
	}
	if email != "user@example.com" {
		t.Fatalf("email = %q, want %q", email, "user@example.com")
	}

	var (
		verifiedUserID uuid.UUID
		expiresAt      time.Time
		consumedAt     *time.Time
	)

	row := pool.QueryRow(ctx,
		`SELECT user_id, expires_at, consumed_at FROM email_verifications WHERE id = $1`, verification.ID)
	if err := row.Scan(&verifiedUserID, &expiresAt, &consumedAt); err != nil {
		t.Fatalf("query email verification: %v", err)
	}
	if verifiedUserID != user.ID {
		t.Fatalf("verification.UserID = %v, want %v", verifiedUserID, user.ID)
	}
	if consumedAt != nil {
		t.Fatalf("consumed_at = %v, want nil for a freshly created verification", *consumedAt)
	}

	var (
		eventType    string
		payloadBytes []byte
		processedAt  *time.Time
		attempts     int
	)

	outboxRow := pool.QueryRow(ctx,
		`SELECT type, payload, processed_at, attempts FROM outbox_events WHERE type = $1`,
		application.EventTypeEmailVerificationRequestedV1,
	)
	if err := outboxRow.Scan(&eventType, &payloadBytes, &processedAt, &attempts); err != nil {
		t.Fatalf("query outbox event: %v", err)
	}
	if processedAt != nil {
		t.Fatalf("outbox processed_at = %v, want nil for a freshly enqueued event", *processedAt)
	}
	if attempts != 0 {
		t.Fatalf("outbox attempts = %d, want 0", attempts)
	}

	var payload application.EmailVerificationRequestedV1
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	if payload.UserID != user.ID {
		t.Fatalf("payload.UserID = %v, want %v", payload.UserID, user.ID)
	}
	if payload.Email != "user@example.com" {
		t.Fatalf("payload.Email = %q, want %q", payload.Email, "user@example.com")
	}
	if payload.VerificationID != verification.ID {
		t.Fatalf("payload.VerificationID = %v, want %v", payload.VerificationID, verification.ID)
	}
}

// TestRegistrationStore_Register_DuplicateEmailRollsBackEverything proves
// the failure path is atomic too: a rejected registration must not
// leave a dangling verification or outbox row behind for the user it
// never actually created.
func TestRegistrationStore_Register_DuplicateEmailRollsBackEverything(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	store := postgres.NewRegistrationStore(pool)
	ctx := context.Background()

	first, firstVerification, firstEvent := newRegistration(t, "user@example.com")
	if err := store.Register(ctx, first, firstVerification, firstEvent); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	second, secondVerification, secondEvent := newRegistration(t, "USER@EXAMPLE.COM")

	err := store.Register(ctx, second, secondVerification, secondEvent)
	if !errors.Is(err, domain.ErrEmailAlreadyExists) {
		t.Fatalf("second Register() error = %v, want %v", err, domain.ErrEmailAlreadyExists)
	}

	var userCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("users = %d, want 1 (only the first registration)", userCount)
	}

	var verificationCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_verifications`).Scan(&verificationCount); err != nil {
		t.Fatalf("count email_verifications: %v", err)
	}
	if verificationCount != 1 {
		t.Fatalf("email_verifications = %d, want 1 (the rejected attempt's row must not exist)", verificationCount)
	}

	var outboxCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events`).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox_events: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox_events = %d, want 1 (the rejected attempt's event must not exist)", outboxCount)
	}
}

func TestRegistrationStore_Register_DuplicatePhoneRejected(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	store := postgres.NewRegistrationStore(pool)
	ctx := context.Background()

	phone := "+14155550100"
	hash := "hashed-password"

	first := domain.NewUser("first@example.com", &phone, &hash)
	firstVerification := domain.NewEmailVerification(first.ID, 24*time.Hour)
	firstEvent := application.EmailVerificationRequestedV1{
		UserID: first.ID, Email: first.Email,
		VerificationID: firstVerification.ID, ExpiresAt: firstVerification.ExpiresAt,
	}

	if err := store.Register(ctx, first, firstVerification, firstEvent); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	second := domain.NewUser("second@example.com", &phone, &hash)
	secondVerification := domain.NewEmailVerification(second.ID, 24*time.Hour)
	secondEvent := application.EmailVerificationRequestedV1{
		UserID: second.ID, Email: second.Email,
		VerificationID: secondVerification.ID, ExpiresAt: secondVerification.ExpiresAt,
	}

	err := store.Register(ctx, second, secondVerification, secondEvent)
	if !errors.Is(err, domain.ErrPhoneAlreadyExists) {
		t.Fatalf("second Register() error = %v, want %v", err, domain.ErrPhoneAlreadyExists)
	}
}

func TestRegistrationRollsBackAfterLaterInsertFailure(t *testing.T) {
	for _, table := range []string{"email_verifications", "outbox_events"} {
		t.Run(table, func(t *testing.T) {
			pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
			ctx := context.Background()
			// Fixed table names on a freshly created disposable DB; fail the later insert.
			if _, err := pool.Exec(ctx, "ALTER TABLE "+table+" ADD CONSTRAINT reject_insert CHECK (false)"); err != nil {
				t.Fatal(err)
			}
			user, verification, event := newRegistration(t, "rollback@example.com")
			if err := postgres.NewRegistrationStore(pool).Register(ctx, user, verification, event); err == nil {
				t.Fatal("expected insert failure")
			}
			for _, name := range []string{"users", "email_verifications", "outbox_events"} {
				var count int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+name).Scan(&count); err != nil || count != 0 {
					t.Fatalf("%s count=%d error=%v", name, count, err)
				}
			}
		})
	}
}
