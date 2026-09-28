//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/postgres"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestVerificationLifecycle(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	ctx := context.Background()
	user, first, event := newRegistration(t, "verify@example.com")
	if err := postgres.NewRegistrationStore(pool).Register(ctx, user, first, event); err != nil {
		t.Fatal(err)
	}
	store := postgres.NewVerificationStore(pool)
	if ok, err := store.CanDeliver(ctx, event); err != nil || !ok {
		t.Fatalf("initial delivery=%v %v", ok, err)
	}
	second := domain.NewEmailVerification(user.ID, time.Hour)
	if err := store.Replace(ctx, second); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.CanDeliver(ctx, event); err != nil || ok {
		t.Fatalf("old delivery=%v %v", ok, err)
	}
	if err := store.Consume(ctx, first.ID, first.ExpiresAt); !errors.Is(err, domain.ErrInvalidEmailVerification) {
		t.Fatalf("replaced credential accepted: %v", err)
	}
	next := application.EmailVerificationRequestedV1{UserID: user.ID, Email: user.Email, VerificationID: second.ID, ExpiresAt: second.ExpiresAt}
	if ok, err := store.CanDeliver(ctx, next); err != nil || !ok {
		t.Fatalf("replacement delivery=%v %v", ok, err)
	}
	signer := security.NewVerificationSigner(security.VerificationTokenConfig{Secret: strings.Repeat("s", 32)})
	token := signer.SignVerificationToken(second.ID, second.ExpiresAt)
	if err := application.NewVerifyEmailService(store, signer).Verify(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(ctx, second.ID, second.ExpiresAt); !errors.Is(err, domain.ErrInvalidEmailVerification) {
		t.Fatalf("replay accepted: %v", err)
	}
	if ok, err := store.CanDeliver(ctx, next); err != nil || ok {
		t.Fatalf("consumed delivery=%v %v", ok, err)
	}
	if err := store.Replace(ctx, domain.NewEmailVerification(user.ID, time.Hour)); err != nil {
		t.Fatal(err)
	}
	var verified *time.Time
	var count int
	if err := pool.QueryRow(ctx, `SELECT email_verified_at FROM users WHERE id=$1`, user.ID).Scan(&verified); err != nil || verified == nil {
		t.Fatalf("not verified: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("verified resend queued extra mail: %d %v", count, err)
	}
}

func TestVerificationRejectsExpiredDisabledAndMismatchedCredentials(t *testing.T) {
	for _, scenario := range []string{"expired", "disabled", "wrong expiry", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
			ctx := context.Background()
			user, v, event := newRegistration(t, "verify@example.com")
			if scenario == "expired" {
				v.ExpiresAt = time.Now().Add(-time.Hour)
				event.ExpiresAt = v.ExpiresAt
			}
			if scenario == "disabled" {
				user.Status = domain.UserStatusDisabled
			}
			if err := postgres.NewRegistrationStore(pool).Register(ctx, user, v, event); err != nil {
				t.Fatal(err)
			}
			id, expires := v.ID, v.ExpiresAt
			if scenario == "wrong expiry" {
				expires = expires.Add(time.Hour)
				event.ExpiresAt = expires
			}
			if scenario == "unknown" {
				id = uuid.New()
				event.VerificationID = id
			}
			store := postgres.NewVerificationStore(pool)
			if err := store.Consume(ctx, id, expires); !errors.Is(err, domain.ErrInvalidEmailVerification) {
				t.Fatalf("invalid credential accepted: %v", err)
			}
			if ok, err := store.CanDeliver(ctx, event); err != nil || ok {
				t.Fatalf("invalid credential deliverable: %v %v", ok, err)
			}
		})
	}
}

func TestConcurrentVerificationConsumesOnce(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	ctx := context.Background()
	user, v, event := newRegistration(t, "race@example.com")
	if err := postgres.NewRegistrationStore(pool).Register(ctx, user, v, event); err != nil {
		t.Fatal(err)
	}
	store := postgres.NewVerificationStore(pool)
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { <-start; results <- store.Consume(ctx, v.ID, v.ExpiresAt) })
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, domain.ErrInvalidEmailVerification) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("consumed %d times", successes)
	}
}

func TestConcurrentVerifyAndResendLeaveConsistentState(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	ctx := context.Background()
	user, v, event := newRegistration(t, "race@example.com")
	if err := postgres.NewRegistrationStore(pool).Register(ctx, user, v, event); err != nil {
		t.Fatal(err)
	}
	store := postgres.NewVerificationStore(pool)
	start := make(chan struct{})
	verifyResult := make(chan error, 1)
	resendResult := make(chan error, 1)
	go func() { <-start; verifyResult <- store.Consume(ctx, v.ID, v.ExpiresAt) }()
	go func() { <-start; resendResult <- store.Replace(ctx, domain.NewEmailVerification(user.ID, time.Hour)) }()
	close(start)
	verifyErr, resendErr := <-verifyResult, <-resendResult
	if resendErr != nil {
		t.Fatal(resendErr)
	}
	if verifyErr != nil && !errors.Is(verifyErr, domain.ErrInvalidEmailVerification) {
		t.Fatal(verifyErr)
	}
	var verified bool
	var active int
	if err := pool.QueryRow(ctx, `SELECT email_verified_at IS NOT NULL FROM users WHERE id=$1`, user.ID).Scan(&verified); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_verifications WHERE consumed_at IS NULL`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if verifyErr == nil {
		if !verified || active != 0 {
			t.Fatal("verification left active credential")
		}
	} else if verified || active != 1 {
		t.Fatal("resend left inconsistent state")
	}
}

func TestResendRollbackPreservesPreviousCredential(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	ctx := context.Background()
	user, v, event := newRegistration(t, "rollback@example.com")
	if err := postgres.NewRegistrationStore(pool).Register(ctx, user, v, event); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT reject_new_delivery CHECK(false) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	store := postgres.NewVerificationStore(pool)
	if err := store.Replace(ctx, domain.NewEmailVerification(user.ID, time.Hour)); err == nil {
		t.Fatal("expected delivery insert failure")
	}
	if ok, err := store.CanDeliver(ctx, event); err != nil || !ok {
		t.Fatalf("old credential lost: %v %v", ok, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_verifications`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("replacement leaked: %d %v", count, err)
	}
}

func TestVerificationRollbackDoesNotConsumeLink(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	ctx := context.Background()
	user, v, event := newRegistration(t, "rollback@example.com")
	if err := postgres.NewRegistrationStore(pool).Register(ctx, user, v, event); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE users ADD CONSTRAINT reject_verified CHECK(email_verified_at IS NULL)`); err != nil {
		t.Fatal(err)
	}
	store := postgres.NewVerificationStore(pool)
	if err := store.Consume(ctx, v.ID, v.ExpiresAt); err == nil {
		t.Fatal("expected user update failure")
	}
	if ok, err := store.CanDeliver(ctx, event); err != nil || !ok {
		t.Fatalf("failed verification consumed link: %v %v", ok, err)
	}
}
