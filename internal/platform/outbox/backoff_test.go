package outbox

import (
	"testing"
	"time"
)

func TestBackoff_GrowsExponentiallyWithAttempts(t *testing.T) {
	base := time.Second
	maxDelay := time.Hour

	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{attempts: 1, want: 1 * time.Second},
		{attempts: 2, want: 2 * time.Second},
		{attempts: 3, want: 4 * time.Second},
		{attempts: 4, want: 8 * time.Second},
	}

	for _, tc := range tests {
		if got := backoff(tc.attempts, base, maxDelay); got != tc.want {
			t.Fatalf("backoff(%d, %s, %s) = %s, want %s", tc.attempts, base, maxDelay, got, tc.want)
		}
	}
}

func TestBackoff_CapsAtMax(t *testing.T) {
	got := backoff(20, time.Second, time.Minute)
	if got != time.Minute {
		t.Fatalf("backoff() = %s, want capped at %s", got, time.Minute)
	}
}

func TestBackoff_NeverOverflowsForLargeAttempts(t *testing.T) {
	got := backoff(1000, time.Second, time.Hour)
	if got != time.Hour {
		t.Fatalf("backoff() = %s, want capped at %s for a very large attempts count", got, time.Hour)
	}
}

func TestBackoff_TreatsZeroOrNegativeAttemptsAsOne(t *testing.T) {
	base := time.Second

	got0 := backoff(0, base, time.Hour)
	gotNeg := backoff(-5, base, time.Hour)

	if got0 != base {
		t.Fatalf("backoff(0, ...) = %s, want %s", got0, base)
	}
	if gotNeg != base {
		t.Fatalf("backoff(-5, ...) = %s, want %s", gotNeg, base)
	}
}
