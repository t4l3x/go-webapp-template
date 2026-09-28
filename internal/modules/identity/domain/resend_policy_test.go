package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

func TestResendPolicy_Check(t *testing.T) {
	policy := domain.ResendPolicy{Cooldown: time.Minute, MaxPerWindow: 3}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	tests := []struct {
		name      string
		issued    []time.Time // newest first
		wantRetry time.Duration
	}{
		{"nothing issued", nil, 0},
		{"cooldown elapsed", []time.Time{ago(time.Minute)}, 0},
		{"inside cooldown", []time.Time{ago(20 * time.Second)}, 40 * time.Second},
		{"under cap", []time.Time{ago(time.Hour), ago(2 * time.Hour)}, 0},
		{"at cap: wait for oldest counted to age out", []time.Time{ago(time.Hour), ago(2 * time.Hour), ago(20 * time.Hour)}, 4 * time.Hour},
		// A lowered cap: the 3rd newest, not the oldest, is the one that must age out.
		{"over cap", []time.Time{ago(time.Hour), ago(2 * time.Hour), ago(3 * time.Hour), ago(23 * time.Hour)}, 21 * time.Hour},
		{"cooldown and cap: longer wins", []time.Time{ago(10 * time.Second), ago(2 * time.Hour), ago(23*time.Hour + 59*time.Minute + 30*time.Second)}, 50 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := policy.Check(tc.issued, now)

			if tc.wantRetry == 0 {
				if err != nil {
					t.Fatalf("Check() = %v, want nil", err)
				}
				return
			}

			var limited *domain.ResendLimitError
			if !errors.As(err, &limited) {
				t.Fatalf("Check() = %v, want *ResendLimitError", err)
			}
			if limited.RetryAfter() != tc.wantRetry {
				t.Fatalf("RetryAfter = %s, want %s", limited.RetryAfter(), tc.wantRetry)
			}
		})
	}
}
