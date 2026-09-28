//go:build integration

package outbox

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestRunner_ProcessesEventAndMarksProcessed(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	if err := Insert(ctx, pool, "test.success", []byte(`{"n":1}`)); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	called := make(chan ClaimedEvent, 1)

	runner := newTestRunner(t,
		RunnerParams{Handlers: []HandlerRegistration{
			{Type: "test.success", Handler: func(_ context.Context, event ClaimedEvent) error {
				called <- event

				return nil
			}},
		}},
		pool, testConfig(), testLogger(),
	)

	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatalf("handler was not called within timeout")
	}

	// Stop only returns once run()'s current iteration — which
	// includes the markProcessed call for whatever it just handled —
	// has fully finished, so no extra wait is needed here.
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	var processedAt *time.Time

	row := pool.QueryRow(ctx, `SELECT processed_at FROM outbox_events WHERE type = 'test.success'`)
	if err := row.Scan(&processedAt); err != nil {
		t.Fatalf("query processed_at: %v", err)
	}
	if processedAt == nil {
		t.Fatalf("processed_at = nil, want set")
	}
}

func TestRunner_FailedHandlerLeavesEventRetryable(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	if err := Insert(ctx, pool, "test.failure", []byte(`{}`)); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	handlerErr := errors.New("mail provider unavailable")
	called := make(chan struct{}, 1)

	runner := newTestRunner(t,
		RunnerParams{Handlers: []HandlerRegistration{
			{Type: "test.failure", Handler: func(context.Context, ClaimedEvent) error {
				called <- struct{}{}

				return handlerErr
			}},
		}},
		pool, testConfig(), testLogger(),
	)

	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatalf("handler was not called within timeout")
	}

	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	var (
		processedAt *time.Time
		attempts    int
		lastError   *string
	)

	row := pool.QueryRow(ctx, `SELECT processed_at, attempts, last_error FROM outbox_events WHERE type = 'test.failure'`)
	if err := row.Scan(&processedAt, &attempts, &lastError); err != nil {
		t.Fatalf("query event state: %v", err)
	}

	if processedAt != nil {
		t.Fatalf("processed_at = %v, want nil for a failed delivery", *processedAt)
	}
	if attempts < 1 {
		t.Fatalf("attempts = %d, want at least 1", attempts)
	}
	if lastError == nil || *lastError != handlerErr.Error() {
		t.Fatalf("last_error = %v, want %q", lastError, handlerErr.Error())
	}
}

// TestRunner_MaxAttemptsReachedMarksTerminallyFailed proves the
// dead-letter-visibility behavior end to end: once a failing handler's
// attempt reaches the configured ceiling, the Runner marks the event
// terminally failed (failed_at set) rather than scheduling another
// retry, and last_error is preserved.
func TestRunner_MaxAttemptsReachedMarksTerminallyFailed(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	if err := Insert(ctx, pool, "test.poison", []byte(`{}`)); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	handlerErr := errors.New("permanently broken")
	called := make(chan struct{}, 1)

	cfg := testConfig()
	cfg.MaxAttempts = 1

	runner := newTestRunner(t,
		RunnerParams{Handlers: []HandlerRegistration{
			{Type: "test.poison", Handler: func(context.Context, ClaimedEvent) error {
				called <- struct{}{}

				return handlerErr
			}},
		}},
		pool, cfg, testLogger(),
	)

	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatalf("handler was not called within timeout")
	}

	// It must never be claimed again — wait through several more poll
	// ticks, with the runner still actively polling, to make sure it
	// doesn't call the handler a second time.
	select {
	case <-called:
		t.Fatalf("handler was called again after the event was marked terminally failed")
	case <-time.After(5 * cfg.PollInterval):
	}

	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	var (
		processedAt *time.Time
		failedAt    *time.Time
		lastError   *string
	)

	row := pool.QueryRow(ctx, `SELECT processed_at, failed_at, last_error FROM outbox_events WHERE type = 'test.poison'`)
	if err := row.Scan(&processedAt, &failedAt, &lastError); err != nil {
		t.Fatalf("query event state: %v", err)
	}

	if processedAt != nil {
		t.Fatalf("processed_at = %v, want nil", *processedAt)
	}
	if failedAt == nil {
		t.Fatalf("failed_at = nil, want set once attempts reached the ceiling")
	}
	if lastError == nil || *lastError != handlerErr.Error() {
		t.Fatalf("last_error = %v, want %q", lastError, handlerErr.Error())
	}
}

// TestRunner_UnregisteredEventTypeIsRetriedNotDropped covers what
// happens to an event whose type nothing handles — the shape a
// superseded or not-yet-deployed event version takes. The type used
// here is deliberately the pre-versioning name of a real event
// ("identity.email_verification_requested", now
// "...requested.v1"): types are matched exactly, so an unrecognized
// one must be recorded as failed and left claimable, never silently
// dropped and never dispatched to some other type's handler.
func TestRunner_UnregisteredEventTypeIsRetriedNotDropped(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	const unregisteredType = "identity.email_verification_requested"

	if err := Insert(ctx, pool, unregisteredType, []byte(`{}`)); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	called := make(chan struct{}, 1)

	// A handler for a *different* type is registered, so this also
	// proves dispatch doesn't fall back to "some handler" when the
	// exact type is missing.
	runner := newTestRunner(t,
		RunnerParams{Handlers: []HandlerRegistration{
			{Type: "identity.email_verification_requested.v1", Handler: func(context.Context, ClaimedEvent) error {
				called <- struct{}{}

				return nil
			}},
		}},
		pool, testConfig(), testLogger(),
	)

	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Give the poll loop several ticks to claim and dispatch the event.
	deadline := time.After(5 * time.Second)

	for {
		var attempts int

		row := pool.QueryRow(ctx, `SELECT attempts FROM outbox_events WHERE type = $1`, unregisteredType)
		if err := row.Scan(&attempts); err != nil {
			t.Fatalf("query attempts: %v", err)
		}

		if attempts > 0 {
			break
		}

		select {
		case <-deadline:
			t.Fatalf("event was never claimed within timeout")
		case <-time.After(testConfig().PollInterval):
		}
	}

	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	select {
	case <-called:
		t.Fatalf("the v1 handler was invoked for an unregistered event type")
	default:
	}

	var (
		processedAt *time.Time
		failedAt    *time.Time
		lastError   *string
	)

	row := pool.QueryRow(ctx,
		`SELECT processed_at, failed_at, last_error FROM outbox_events WHERE type = $1`, unregisteredType)
	if err := row.Scan(&processedAt, &failedAt, &lastError); err != nil {
		t.Fatalf("query event state: %v", err)
	}

	if processedAt != nil {
		t.Fatalf("processed_at = %v, want nil — an unhandled event must never be marked processed", *processedAt)
	}
	if lastError == nil || !strings.Contains(*lastError, "no handler registered") {
		t.Fatalf("last_error = %v, want it to record that no handler was registered", lastError)
	}
}

func TestRunner_StopReturnsPromptlyWhenIdle(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)

	runner := newTestRunner(t, RunnerParams{}, pool, testConfig(), testLogger())

	if err := runner.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v, want graceful shutdown with no pending work", err)
	}
}

// TestRunner_Stop_TimesOutIfHandlerHangs proves Stop enforces its own
// shutdown budget rather than blocking forever on a handler that
// doesn't respect context cancellation.
func TestRunner_Stop_TimesOutIfHandlerHangs(t *testing.T) {
	pool := testkit.NewPostgresTestDB(t, outboxTestDBPrefix)
	ctx := context.Background()

	if err := Insert(ctx, pool, "test.hang", []byte(`{}`)); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})

	cfg := testConfig()
	cfg.ShutdownTimeout = 50 * time.Millisecond

	runner := newTestRunner(t,
		RunnerParams{Handlers: []HandlerRegistration{
			{Type: "test.hang", Handler: func(context.Context, ClaimedEvent) error {
				close(entered)
				<-release // deliberately ignores context cancellation

				return nil
			}},
		}},
		pool, cfg, testLogger(),
	)

	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatalf("handler was not entered within timeout")
	}

	if err := runner.Stop(context.Background()); err == nil {
		t.Fatalf("Stop() error = nil, want a timeout error for a hanging handler")
	}

	close(release)

	select {
	case <-runner.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("runner did not finish after the handler was released")
	}
}

func newTestRunner(t *testing.T, params RunnerParams, pool *pgxpool.Pool, cfg Config, logger *slog.Logger) *Runner {
	t.Helper()
	runner, err := NewRunner(params, pool, cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}
