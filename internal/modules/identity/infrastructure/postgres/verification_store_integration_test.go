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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/postgres"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

// noResendLimit lets tests that exercise replacement itself resend
// immediately after registering; the resend policy has its own tests.
var noResendLimit = domain.ResendPolicy{Cooldown: time.Nanosecond, MaxPerWindow: 1000}

var resendPolicy = domain.ResendPolicy{Cooldown: time.Minute, MaxPerWindow: 3}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestResendCooldownCountsRegistrationAndWritesNothingWhenRefused: the
// registration email starts the cooldown, and a refused resend leaves
// no credential and no queued mail behind.
func TestResendCooldownCountsRegistrationAndWritesNothingWhenRefused(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	ctx := context.Background()
	user, v, event := newRegistration(t, "cooldown@example.com")
	if err := postgres.NewRegistrationStore(pool).Register(ctx, user, v, event); err != nil {
		t.Fatal(err)
	}

	err := postgres.NewVerificationStore(pool).Replace(ctx, domain.NewEmailVerification(user.ID, time.Hour), resendPolicy)

	var limited *domain.ResendLimitError
	if !errors.As(err, &limited) || limited.RetryAfter() <= 0 || limited.RetryAfter() > time.Minute {
		t.Fatalf("Replace() = %v, want ResendLimitError with 0 < RetryAfter <= 1m", err)
	}
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM email_verifications`); n != 1 {
		t.Fatalf("email_verifications = %d, want 1 (refusal must not write)", n)
	}
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM outbox_events`); n != 1 {
		t.Fatalf("outbox_events = %d, want 1 (refusal must not queue mail)", n)
	}
	if ok, err := postgres.NewVerificationStore(pool).CanDeliver(ctx, event); err != nil || !ok {
		t.Fatalf("refused resend invalidated the registration link: %v %v", ok, err)
	}
}

// TestResendDailyCapCountsReplacedCredentials: replaced (consumed) rows
// still count toward the rolling cap.
func TestResendDailyCapCountsReplacedCredentials(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	ctx := context.Background()
	user, v, event := newRegistration(t, "cap@example.com")
	if err := postgres.NewRegistrationStore(pool).Register(ctx, user, v, event); err != nil {
		t.Fatal(err)
	}
	store := postgres.NewVerificationStore(pool)
	capOnly := domain.ResendPolicy{Cooldown: time.Nanosecond, MaxPerWindow: 3}

	for i := range 2 { // registration + 2 resends = 3 = the cap
		if err := store.Replace(ctx, domain.NewEmailVerification(user.ID, time.Hour), capOnly); err != nil {
			t.Fatalf("resend %d: %v", i+1, err)
		}
	}

	err := store.Replace(ctx, domain.NewEmailVerification(user.ID, time.Hour), capOnly)
	var limited *domain.ResendLimitError
	if !errors.As(err, &limited) || limited.RetryAfter() < domain.ResendWindow-time.Minute || limited.RetryAfter() > domain.ResendWindow {
		t.Fatalf("Replace() = %v, want ResendLimitError with RetryAfter ~ %s", err, domain.ResendWindow)
	}
}

// TestConcurrentResendsCannotBypassCooldown: the policy is checked under
// the user row lock, so of many simultaneous resends exactly one wins.
func TestConcurrentResendsCannotBypassCooldown(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, identityTestDBPrefix)
	ctx := context.Background()
	user, v, event := newRegistration(t, "concurrent@example.com")
	v.CreatedAt = v.CreatedAt.Add(-2 * time.Hour) // registration mail is outside the cooldown
	if err := postgres.NewRegistrationStore(pool).Register(ctx, user, v, event); err != nil {
		t.Fatal(err)
	}
	store := postgres.NewVerificationStore(pool)

	const attempts = 8
	start := make(chan struct{})
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Go(func() {
			<-start
			results <- store.Replace(ctx, domain.NewEmailVerification(user.ID, time.Hour), resendPolicy)
		})
	}
	close(start)
	wg.Wait()
	close(results)

	accepted := 0
	for err := range results {
		var limited *domain.ResendLimitError
		switch {
		case err == nil:
			accepted++
		case errors.As(err, &limited):
		default:
			t.Fatal(err)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d concurrent resends, want exactly 1", accepted)
	}
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM outbox_events`); n != 2 {
		t.Fatalf("outbox_events = %d, want 2 (registration + one resend)", n)
	}
}

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
	if err := store.Replace(ctx, second, noResendLimit); err != nil {
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
	if err := store.Replace(ctx, domain.NewEmailVerification(user.ID, time.Hour), noResendLimit); err != nil {
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
	go func() {
		<-start
		resendResult <- store.Replace(ctx, domain.NewEmailVerification(user.ID, time.Hour), noResendLimit)
	}()
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
	if err := store.Replace(ctx, domain.NewEmailVerification(user.ID, time.Hour), noResendLimit); err == nil {
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
