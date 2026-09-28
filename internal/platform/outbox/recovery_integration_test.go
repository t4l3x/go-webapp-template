//go:build integration

package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestClaimRecoversInterruptedFinalAttempt(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()
	if err := Insert(ctx, pool, "test.crash", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	events, err := claim(ctx, pool, 1, 1, time.Minute)
	if err != nil || len(events) != 1 {
		t.Fatalf("claim=%v, %v", events, err)
	}
	// Recovery must not touch an outstanding lease, even at the ceiling.
	if _, err := claim(ctx, pool, 1, 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	var failed *time.Time
	if err := pool.QueryRow(ctx, `SELECT failed_at FROM outbox_events`).Scan(&failed); err != nil || failed != nil {
		t.Fatalf("active lease failed: %v %v", failed, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET available_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, err := claim(ctx, pool, 1, 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	var message string
	if err := pool.QueryRow(ctx, `SELECT failed_at,last_error FROM outbox_events`).Scan(&failed, &message); err != nil || failed == nil || message == "" {
		t.Fatalf("missing recovery outcome: %v %q %v", failed, message, err)
	}
	if err := markProcessed(ctx, pool, events[0]); !errors.Is(err, errClaimLost) {
		t.Fatalf("late success overwrote recovery: %v", err)
	}
}

func TestOutcomeCannotOverwriteNewerClaimOrTerminalState(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()
	if err := Insert(ctx, pool, "test.stale", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	old, err := claim(ctx, pool, 1, 3, time.Minute)
	if err != nil || len(old) != 1 {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET available_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	current, err := claim(ctx, pool, 1, 3, time.Minute)
	if err != nil || len(current) != 1 {
		t.Fatal(err)
	}
	cause := errors.New("late failure")
	for _, err := range []error{
		markProcessed(ctx, pool, old[0]),
		markFailed(ctx, pool, old[0], cause, time.Now()),
		markTerminallyFailed(ctx, pool, old[0], cause),
	} {
		if !errors.Is(err, errClaimLost) {
			t.Fatalf("stale outcome accepted: %v", err)
		}
	}
	if err := markProcessed(ctx, pool, current[0]); err != nil {
		t.Fatal(err)
	}
	if err := markTerminallyFailed(ctx, pool, current[0], cause); !errors.Is(err, errClaimLost) {
		t.Fatalf("terminal success overwritten: %v", err)
	}
}

func TestRunnerRecoversFailureToRecordFinalOutcome(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()
	if err := Insert(ctx, pool, "test.mark", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	// The constraint rejects both possible final writes but allows claiming.
	if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT reject_outcomes CHECK (processed_at IS NULL AND failed_at IS NULL)`); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.MaxAttempts = 1
	runner := newTestRunner(t, RunnerParams{Handlers: []HandlerRegistration{{Type: "test.mark", Handler: func(context.Context, ClaimedEvent) error { return errors.New("delivery failed") }}}}, pool, cfg, testLogger())
	runner.pollOnce()
	if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events DROP CONSTRAINT reject_outcomes; UPDATE outbox_events SET available_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	runner.pollOnce()
	var failed *time.Time
	if err := pool.QueryRow(ctx, `SELECT failed_at FROM outbox_events`).Scan(&failed); err != nil || failed == nil {
		t.Fatalf("final mark not recovered: %v %v", failed, err)
	}
}

func TestRunnerPermanentFailureDoesNotRetry(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()
	if err := Insert(ctx, pool, "test.invalid", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	calls := 0
	runner := newTestRunner(t, RunnerParams{Handlers: []HandlerRegistration{{Type: "test.invalid", Handler: func(context.Context, ClaimedEvent) error { calls++; return Permanent(errors.New("invalid payload")) }}}}, pool, testConfig(), testLogger())
	runner.pollOnce()
	runner.pollOnce()
	var failed *time.Time
	if err := pool.QueryRow(ctx, `SELECT failed_at FROM outbox_events`).Scan(&failed); err != nil || failed == nil || calls != 1 {
		t.Fatalf("permanent failure: calls=%d failed=%v err=%v", calls, failed, err)
	}
}

func TestRunnerBoundsHandlerAndSkipsExpiredLease(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()
	if err := Insert(ctx, pool, "test.deadline", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	events, err := claim(ctx, pool, 1, 10, time.Minute)
	if err != nil || len(events) != 1 {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.BatchSize = 10
	cfg.ClaimLease = 80 * time.Second
	calls := 0
	runner := newTestRunner(t, RunnerParams{Handlers: []HandlerRegistration{{Type: "test.deadline", Handler: func(ctx context.Context, _ ClaimedEvent) error {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second || time.Until(deadline) <= 0 {
			t.Errorf("handler deadline not bounded to batch slot: %v", deadline)
		}
		return nil
	}}}}, pool, cfg, testLogger())
	// This event has spent its handler budget waiting in the batch.
	expired := events[0]
	expired.LeaseUntil = time.Now().Add(markTimeout - time.Second)
	runner.process(expired)
	if calls != 0 {
		t.Fatal("expired lease invoked handler")
	}
	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET available_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	current, err := claim(ctx, pool, 1, 10, time.Minute)
	if err != nil || len(current) != 1 {
		t.Fatal(err)
	}
	runner.process(current[0])
	if calls != 1 {
		t.Fatal("valid lease did not invoke handler")
	}
}
