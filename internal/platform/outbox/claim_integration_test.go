//go:build integration

package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

// TestClaim_ConcurrentWorkersClaimDisjointSets is the real proof that
// claiming is race-safe: multiple goroutines (standing in for multiple
// cmd/worker processes sharing this table) claim concurrently,
// released together via a barrier channel (never a sleep). Every event
// must end up claimed by exactly one goroutine exactly once.
func TestClaim_ConcurrentWorkersClaimDisjointSets(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	const totalEvents = 20

	for i := 0; i < totalEvents; i++ {
		if err := Insert(ctx, pool, "test.concurrent", []byte(`{}`)); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
	}

	const workers = 4

	start := make(chan struct{})
	errs := make(chan error, workers)

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = make(map[uuid.UUID]int)
	)

	for w := 0; w < workers; w++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			<-start

			for {
				claimed, err := claim(ctx, pool, 3, 10, time.Minute)
				if err != nil {
					errs <- err

					return
				}
				if len(claimed) == 0 {
					return
				}

				for _, event := range claimed {
					// Mark processed immediately so a claimed row can
					// never be claimed again by another goroutine —
					// this is what makes "claimed exactly once" a
					// meaningful property to check below, rather than
					// every worker just re-claiming the same rows
					// forever.
					if err := markProcessed(ctx, pool, event); err != nil {
						errs <- err

						return
					}

					mu.Lock()
					seen[event.ID]++
					mu.Unlock()
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("worker error: %v", err)
	}

	if len(seen) != totalEvents {
		t.Fatalf("distinct claimed events = %d, want %d", len(seen), totalEvents)
	}

	for id, count := range seen {
		if count != 1 {
			t.Fatalf("event %s claimed %d times, want exactly 1", id, count)
		}
	}
}

// TestClaim_LeaseHidesEventUntilItExpires directly proves the claim
// lease itself: a claimed-but-not-yet-marked event must not be
// reclaimable while its lease is still active (this is what prevents
// two workers from processing the same event at the same time), and
// must become reclaimable again once the lease has elapsed (this is
// what makes a crashed worker's claim self-healing rather than a
// permanently stuck event).
func TestClaim_LeaseHidesEventUntilItExpires(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	if err := Insert(ctx, pool, "test.lease", []byte(`{}`)); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	const lease = 50 * time.Millisecond

	first, err := claim(ctx, pool, 10, 10, lease)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("claimed = %d, want 1", len(first))
	}

	// The lease is still active: a second claimer (e.g. another worker
	// process) must not see this event as due yet, even though it was
	// never marked processed or failed.
	stillLeased, err := claim(ctx, pool, 10, 10, lease)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if len(stillLeased) != 0 {
		t.Fatalf("claimed = %d while the lease is still active, want 0", len(stillLeased))
	}

	time.Sleep(lease + 50*time.Millisecond)

	afterExpiry, err := claim(ctx, pool, 10, 10, lease)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if len(afterExpiry) != 1 {
		t.Fatalf("claimed = %d after the lease expired, want 1", len(afterExpiry))
	}
	if afterExpiry[0].Attempts != 2 {
		t.Fatalf("Attempts = %d, want 2 (claimed twice: once, then again after lease expiry)", afterExpiry[0].Attempts)
	}
}

func TestClaim_ProcessedEventNotReclaimed(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	if err := Insert(ctx, pool, "test.once", []byte(`{}`)); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	first, err := claim(ctx, pool, 10, 10, time.Minute)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("claimed = %d, want 1", len(first))
	}

	if err := markProcessed(ctx, pool, first[0]); err != nil {
		t.Fatalf("markProcessed() error = %v", err)
	}

	second, err := claim(ctx, pool, 10, 10, time.Minute)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("claimed = %d after processing, want 0", len(second))
	}
}

func TestClaim_FailedEventRemainsRetryableAfterBackoffElapses(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	if err := Insert(ctx, pool, "test.retry", []byte(`{}`)); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	first, err := claim(ctx, pool, 10, 10, time.Minute)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("claimed = %d, want 1", len(first))
	}
	if first[0].Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", first[0].Attempts)
	}

	// Not yet due: a future available_at must not be reclaimed
	// immediately.
	if err := markFailed(ctx, pool, first[0], errors.New("boom"), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("markFailed() error = %v", err)
	}

	notYetDue, err := claim(ctx, pool, 10, 10, time.Minute)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if len(notYetDue) != 0 {
		t.Fatalf("claimed = %d before backoff elapsed, want 0", len(notYetDue))
	}

	// Once the backoff has elapsed, the event becomes claimable again,
	// with attempts incremented further — a real retry, not a fresh
	// event.
	if err := markFailed(ctx, pool, first[0], errors.New("boom"), time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("markFailed() error = %v", err)
	}

	due, err := claim(ctx, pool, 10, 10, time.Minute)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("claimed = %d after backoff elapsed, want 1", len(due))
	}
	if due[0].Attempts != 2 {
		t.Fatalf("Attempts = %d, want 2", due[0].Attempts)
	}
}

// TestClaim_RespectsMaxAttempts proves poison-event containment: once
// an event's attempts reaches maxAttempts, claim stops returning it —
// it stays in the table, visible for operational inspection, instead
// of being retried forever.
func TestClaim_RespectsMaxAttempts(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	if err := Insert(ctx, pool, "test.poison", []byte(`{}`)); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	const maxAttempts = 3

	for i := 0; i < maxAttempts; i++ {
		claimed, err := claim(ctx, pool, 10, maxAttempts, time.Minute)
		if err != nil {
			t.Fatalf("claim() #%d error = %v", i, err)
		}
		if len(claimed) != 1 {
			t.Fatalf("claim() #%d claimed = %d, want 1", i, len(claimed))
		}

		if err := markFailed(ctx, pool, claimed[0], errors.New("boom"), time.Now().Add(-time.Second)); err != nil {
			t.Fatalf("markFailed() #%d error = %v", i, err)
		}
	}

	final, err := claim(ctx, pool, 10, maxAttempts, time.Minute)
	if err != nil {
		t.Fatalf("final claim() error = %v", err)
	}
	if len(final) != 0 {
		t.Fatalf("claimed = %d once attempts reached maxAttempts, want 0", len(final))
	}
}

// TestMarkTerminallyFailed_EventNeverReclaimed proves the explicit
// terminal state: once an event is marked terminally failed, it is
// excluded from claim regardless of maxAttempts (a much larger ceiling
// than its actual attempts is passed here specifically to prove
// exclusion comes from failed_at, not from attempts coincidentally
// reaching the ceiling), and last_error/failed_at remain queryable for
// operational inspection.
func TestMarkTerminallyFailed_EventNeverReclaimed(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	if err := Insert(ctx, pool, "test.terminal", []byte(`{}`)); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	claimed, err := claim(ctx, pool, 10, 10, time.Minute)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}

	cause := errors.New("permanent: no such recipient")
	if err := markTerminallyFailed(ctx, pool, claimed[0], cause); err != nil {
		t.Fatalf("markTerminallyFailed() error = %v", err)
	}

	// A generous maxAttempts (far above the single attempt this event
	// actually has) proves failed_at, not attempts, is what excludes it.
	reclaimed, err := claim(ctx, pool, 10, 1000, time.Minute)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if len(reclaimed) != 0 {
		t.Fatalf("claimed = %d for a terminally failed event, want 0", len(reclaimed))
	}

	var (
		failedAt  *time.Time
		lastError *string
	)

	row := pool.QueryRow(ctx, `SELECT failed_at, last_error FROM outbox_events WHERE type = 'test.terminal'`)
	if err := row.Scan(&failedAt, &lastError); err != nil {
		t.Fatalf("query event state: %v", err)
	}
	if failedAt == nil {
		t.Fatalf("failed_at = nil, want set")
	}
	if lastError == nil || *lastError != cause.Error() {
		t.Fatalf("last_error = %v, want %q", lastError, cause.Error())
	}
}
