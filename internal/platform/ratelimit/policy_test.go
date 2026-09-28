package ratelimit_test

import (
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

func TestPolicy_Validate_AcceptsUsablePolicies(t *testing.T) {
	tests := []struct {
		name   string
		policy ratelimit.Policy
	}{
		{"burst_equals_rate", ratelimit.Policy{Rate: 10, Burst: 10, Period: time.Minute}},
		{"smoothed", ratelimit.Policy{Rate: 100, Burst: 5, Period: time.Minute}},
		{"minimum", ratelimit.Policy{Rate: 1, Burst: 1, Period: time.Second}},
		{"per_minute_helper", ratelimit.PerMinute(30)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.policy.Validate(); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

// TestPolicy_Validate_RejectsUnusablePolicies matters because the
// backend is a Lua script, not a validating API: a zero rate there is a
// division by zero inside Redis, and a negative one silently misbehaves.
func TestPolicy_Validate_RejectsUnusablePolicies(t *testing.T) {
	tests := []struct {
		name   string
		policy ratelimit.Policy
	}{
		{"zero_rate", ratelimit.Policy{Rate: 0, Burst: 10, Period: time.Minute}},
		{"negative_rate", ratelimit.Policy{Rate: -1, Burst: 10, Period: time.Minute}},
		{"zero_burst", ratelimit.Policy{Rate: 10, Burst: 0, Period: time.Minute}},
		{"negative_burst", ratelimit.Policy{Rate: 10, Burst: -5, Period: time.Minute}},
		{"zero_period", ratelimit.Policy{Rate: 10, Burst: 10, Period: 0}},
		{"negative_period", ratelimit.Policy{Rate: 10, Burst: 10, Period: -time.Second}},
		{"zero_value", ratelimit.Policy{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.policy.Validate(); err == nil {
				t.Fatalf("Validate() error = nil, want an error for %+v", tc.policy)
			}
		})
	}
}

func TestPerMinute(t *testing.T) {
	policy := ratelimit.PerMinute(20)

	if policy.Rate != 20 {
		t.Fatalf("Rate = %d, want 20", policy.Rate)
	}
	if policy.Burst != 20 {
		t.Fatalf("Burst = %d, want 20 (a full period's allowance)", policy.Burst)
	}
	if policy.Period != time.Minute {
		t.Fatalf("Period = %s, want %s", policy.Period, time.Minute)
	}
}
