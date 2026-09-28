package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
)

// Outcome writes survive graceful shutdown but remain bounded. A lost final
// outcome is recovered when its lease expires, with delivery marked unknown.
const markTimeout = 5 * time.Second

type RunnerParams struct {
	fx.In

	Handlers []HandlerRegistration `group:"outbox_handlers"`
}

// Runner polls outbox_events and dispatches claimed events to the
// Handler registered for their type. Running more than one Runner —
// e.g. multiple cmd/worker processes — against the same database is
// safe and is the intended way to scale processing (see claim's
// FOR UPDATE SKIP LOCKED), rather than fanning out goroutines within
// one process: this Runner processes its own claimed batch
// sequentially.
type Runner struct {
	pool     *pgxpool.Pool
	handlers map[string]Handler
	cfg      Config
	logger   *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func NewRunner(params RunnerParams, pool *pgxpool.Pool, cfg Config, logger *slog.Logger) (*Runner, error) {
	handlers := make(map[string]Handler, len(params.Handlers))
	for _, reg := range params.Handlers {
		if reg.Type == "" || reg.Handler == nil {
			return nil, fmt.Errorf("invalid outbox handler registration")
		}
		if _, exists := handlers[reg.Type]; exists {
			return nil, fmt.Errorf("duplicate outbox handler for %q", reg.Type)
		}
		handlers[reg.Type] = reg.Handler
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Runner{
		pool:     pool,
		handlers: handlers,
		cfg:      cfg,
		logger:   logger.With("component", "outbox"),
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}, nil
}

// RegisterRunnerLifecycle starts polling on application start and
// stops it gracefully on shutdown.
func RegisterRunnerLifecycle(lifecycle fx.Lifecycle, runner *Runner) {
	lifecycle.Append(fx.Hook{
		OnStart: runner.Start,
		OnStop:  runner.Stop,
	})
}

func (r *Runner) Start(context.Context) error {
	r.logger.Info("outbox runner started",
		"poll_interval", r.cfg.PollInterval, "batch_size", r.cfg.BatchSize)

	go r.run()

	return nil
}

// Stop cancels work and waits within both the caller's and configured budgets.
func (r *Runner) Stop(ctx context.Context) error {
	r.cancel()

	shutdownCtx, cancel := context.WithTimeout(ctx, r.cfg.ShutdownTimeout)
	defer cancel()

	select {
	case <-r.done:
		r.logger.Info("outbox runner stopped")

		return nil
	case <-shutdownCtx.Done():
		return fmt.Errorf("outbox runner: did not stop within %s: %w", r.cfg.ShutdownTimeout, shutdownCtx.Err())
	}
}

func (r *Runner) run() {
	defer close(r.done)

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.pollOnce()
		}
	}
}

func (r *Runner) pollOnce() {
	claimed, err := claim(r.ctx, r.pool, r.cfg.BatchSize, r.cfg.MaxAttempts, r.cfg.ClaimLease)
	if err != nil {
		if r.ctx.Err() != nil {
			return
		}

		r.logger.Error("claim outbox events failed", "error", err)

		return
	}

	for _, event := range claimed {
		if r.ctx.Err() != nil {
			return
		}

		r.process(event)
	}
}

func (r *Runner) process(event ClaimedEvent) {
	handler, ok := r.handlers[event.Type]
	if !ok {
		r.fail(event, fmt.Errorf("no handler registered for event type %q", event.Type))

		return
	}

	// Reserve time to record the outcome before this lease can be reclaimed.
	deadline := time.Now().Add(r.cfg.ClaimLease/time.Duration(r.cfg.BatchSize) - markTimeout)
	if leaseDeadline := event.LeaseUntil.Add(-markTimeout); leaseDeadline.Before(deadline) {
		deadline = leaseDeadline
	}
	ctx, cancel := context.WithDeadline(r.ctx, deadline)
	defer cancel()
	err := ctx.Err()
	if err == nil {
		err = handler(ctx, event)
	}
	if err != nil {
		r.fail(event, err)

		return
	}

	markCtx, cancel := context.WithTimeout(context.Background(), markTimeout)
	defer cancel()

	if err := markProcessed(markCtx, r.pool, event); err != nil {
		r.logger.Error("mark outbox event processed failed", "id", event.ID, "error", err)
	}
}

// Invalid payloads and exhausted attempts fail terminally. Other failures retry
// with backoff. Only diagnostics, never event payloads, are logged or persisted.
func (r *Runner) fail(event ClaimedEvent, cause error) {
	markCtx, cancel := context.WithTimeout(context.Background(), markTimeout)
	defer cancel()

	if event.Attempts >= r.cfg.MaxAttempts || isPermanent(cause) {
		r.logger.Error("outbox event permanently failed",
			"type", event.Type, "id", event.ID, "attempts", event.Attempts, "error", cause)

		if err := markTerminallyFailed(markCtx, r.pool, event, cause); err != nil {
			r.logger.Error("mark outbox event terminally failed failed", "id", event.ID, "error", err)
		}

		return
	}

	r.logger.Error("outbox event handler failed, will retry",
		"type", event.Type, "id", event.ID, "attempts", event.Attempts, "error", cause)

	availableAt := time.Now().UTC().Add(backoff(event.Attempts, r.cfg.BaseBackoff, r.cfg.MaxBackoff))

	if err := markFailed(markCtx, r.pool, event, cause, availableAt); err != nil {
		r.logger.Error("mark outbox event failed failed", "id", event.ID, "error", err)
	}
}

// backoff returns an exponential delay based on attempts, capped at
// maxDelay, so a persistently failing event backs off instead of being
// retried as fast as the poll loop can claim it.
func backoff(attempts int, base, maxDelay time.Duration) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 30 { // guard against overflow from the shift below
		attempts = 30
	}

	d := base << (attempts - 1)
	if d <= 0 || d > maxDelay {
		return maxDelay
	}

	return d
}
