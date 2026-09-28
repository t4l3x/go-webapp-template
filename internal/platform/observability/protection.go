package observability

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// protectionSummaryInterval bounds how often an ongoing outage is logged.
const protectionSummaryInterval = time.Minute

// ProtectionSignal reports a protective control (a rate limiter, an
// abuse counter) failing open because its backend is down — loudly, but
// without a log storm: during an attack a dead Redis would otherwise
// produce one identical line per request.
//
//   - every failure increments the OTel counter named after the signal
//     (exported to Prometheus as <name>_total) — alert on its rate;
//   - one Error log when protection becomes unavailable;
//   - while it stays unavailable, at most one Error summary per minute,
//     with the number of failures since the last one;
//   - one Info log when it recovers, with the outage's duration and total.
//
// Logs carry the stable event name and a low-cardinality scope, never a
// subject (IP, account hash).
type ProtectionSignal struct {
	event   string
	counter metric.Int64Counter
	logger  *slog.Logger
	now     func() time.Time

	degraded atomic.Bool // fast path: Succeeded is on every healthy call

	mu             sync.Mutex
	since, lastLog time.Time
	total, pending int64
}

// NewProtectionSignal creates the signal and its counter. event is both
// the counter name and the log "event" attribute, e.g.
// "rate_limit_protection_unavailable".
func NewProtectionSignal(provider metric.MeterProvider, logger *slog.Logger, event string) (*ProtectionSignal, error) {
	counter, err := provider.Meter(meterScope).Int64Counter(
		event,
		metric.WithDescription("Checks allowed through because a protective control's backend was unavailable (fail-open)."),
		metric.WithUnit("{check}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s counter: %w", event, err)
	}

	return &ProtectionSignal{event: event, counter: counter, logger: logger, now: time.Now}, nil
}

// Failed records one check that failed open.
func (s *ProtectionSignal) Failed(ctx context.Context, scope string, err error) {
	s.counter.Add(ctx, 1, metric.WithAttributes(attribute.String("scope", scope)))

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.total++

	if !s.degraded.Load() {
		s.degraded.Store(true)
		s.since, s.lastLog, s.pending = now, now, 0
		s.logger.Error("protection unavailable, failing open",
			"event", s.event, "state", "unavailable", "scope", scope, "error", err)

		return
	}

	s.pending++
	if now.Sub(s.lastLog) < protectionSummaryInterval {
		return
	}

	s.logger.Error("protection still unavailable, failing open",
		"event", s.event, "state", "unavailable",
		"failures_since_last_log", s.pending,
		"unavailable_for", now.Sub(s.since).Round(time.Second),
		"error", err)
	s.lastLog, s.pending = now, 0
}

// Succeeded records a check that reached its backend. It costs one atomic
// load while healthy and logs recovery once after an outage.
func (s *ProtectionSignal) Succeeded() {
	if !s.degraded.Load() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.degraded.Load() {
		return
	}

	s.degraded.Store(false)
	s.logger.Info("protection recovered",
		"event", s.event, "state", "recovered",
		"unavailable_for", s.now().Sub(s.since).Round(time.Second),
		"failures", s.total)
	s.total = 0
}
