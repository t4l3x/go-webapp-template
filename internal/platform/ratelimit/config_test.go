package ratelimit_test

import (
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func unsetRateLimitEnv(t *testing.T) {
	t.Helper()

	testkit.UnsetEnv(t,
		"RATELIMIT_GLOBAL_PER_MINUTE",
		"RATELIMIT_GLOBAL_BURST",
		"RATELIMIT_TIMEOUT",
	)
}

func TestLoadConfig_Defaults(t *testing.T) {
	unsetRateLimitEnv(t)

	cfg, err := ratelimit.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.GlobalPerMinute != 300 {
		t.Fatalf("GlobalPerMinute = %d, want 300", cfg.GlobalPerMinute)
	}
	if cfg.Timeout != 200*time.Millisecond {
		t.Fatalf("Timeout = %s, want 200ms", cfg.Timeout)
	}
}

// TestLoadConfig_ZeroBurstMeansFullAllowance covers the documented
// meaning of the default: an idle IP may spend a whole period's worth
// at once, which suits a UI that fires several requests on page load.
func TestLoadConfig_ZeroBurstMeansFullAllowance(t *testing.T) {
	unsetRateLimitEnv(t)
	t.Setenv("RATELIMIT_GLOBAL_PER_MINUTE", "120")
	t.Setenv("RATELIMIT_GLOBAL_BURST", "0")

	cfg, err := ratelimit.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.GlobalBurst != 120 {
		t.Fatalf("GlobalBurst = %d, want it defaulted to the rate (120)", cfg.GlobalBurst)
	}

	policy := cfg.GlobalPolicy()
	if policy.Rate != 120 || policy.Burst != 120 || policy.Period != time.Minute {
		t.Fatalf("GlobalPolicy() = %+v, want 120/120 per minute", policy)
	}
}

func TestLoadConfig_ExplicitBurstIsPreserved(t *testing.T) {
	unsetRateLimitEnv(t)
	t.Setenv("RATELIMIT_GLOBAL_PER_MINUTE", "120")
	t.Setenv("RATELIMIT_GLOBAL_BURST", "10")

	cfg, err := ratelimit.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.GlobalBurst != 10 {
		t.Fatalf("GlobalBurst = %d, want 10", cfg.GlobalBurst)
	}
}

// TestLoadConfig_RejectsUnusableValues checks that a misconfigured
// limit fails startup rather than surfacing on the first request that
// happens to hit it.
func TestLoadConfig_RejectsUnusableValues(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		wants string
	}{
		{"zero_rate", map[string]string{"RATELIMIT_GLOBAL_PER_MINUTE": "0"}, "RATELIMIT_GLOBAL_PER_MINUTE"},
		{"negative_rate", map[string]string{"RATELIMIT_GLOBAL_PER_MINUTE": "-5"}, "RATELIMIT_GLOBAL_PER_MINUTE"},
		{"negative_burst", map[string]string{"RATELIMIT_GLOBAL_BURST": "-1"}, "RATELIMIT_GLOBAL_BURST"},
		{"zero_timeout", map[string]string{"RATELIMIT_TIMEOUT": "0s"}, "RATELIMIT_TIMEOUT"},
		{"negative_timeout", map[string]string{"RATELIMIT_TIMEOUT": "-1s"}, "RATELIMIT_TIMEOUT"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			unsetRateLimitEnv(t)

			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			_, err := ratelimit.LoadConfig()
			if err == nil {
				t.Fatalf("LoadConfig() error = nil, want a startup failure")
			}
		})
	}
}
