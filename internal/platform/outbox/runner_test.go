package outbox

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func TestNewRunnerRejectsInvalidRegistrations(t *testing.T) {
	handler := func(context.Context, ClaimedEvent) error { return nil }
	for _, registrations := range [][]HandlerRegistration{
		{{Type: "event", Handler: handler}, {Type: "event", Handler: handler}},
		{{Type: "", Handler: handler}}, {{Type: "event"}},
	} {
		if _, err := NewRunner(RunnerParams{Handlers: registrations}, nil, Config{}, slog.Default()); err == nil {
			t.Fatal("expected invalid registration error")
		}
	}
}

func TestDeliveryBudgetIncludesOutcomeWrites(t *testing.T) {
	for _, tc := range []struct {
		name           string
		batch          int
		lease, timeout time.Duration
		valid          bool
	}{
		{"defaults", 10, 3 * time.Minute, 10 * time.Second, true},
		{"no outcome room", 10, 100 * time.Second, 10 * time.Second, false},
		{"boundary", 10, 150 * time.Second, 10 * time.Second, false},
		{"large batch", 20, 3 * time.Minute, 10 * time.Second, false},
		{"long SMTP timeout", 10, 3 * time.Minute, 20 * time.Second, false},
		{"zero batch", 0, time.Minute, time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (Config{BatchSize: tc.batch, ClaimLease: tc.lease}).ValidateHandlerTimeout(tc.timeout)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}

func TestPermanentPreservesCause(t *testing.T) {
	cause := errors.New("bad payload")
	if !isPermanent(Permanent(cause)) || !errors.Is(Permanent(cause), cause) || isPermanent(cause) {
		t.Fatal("permanent error classification failed")
	}
}
