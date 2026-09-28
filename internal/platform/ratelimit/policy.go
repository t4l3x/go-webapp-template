package ratelimit

import (
	"fmt"
	"time"
)

// Policy is how much traffic one key may generate. It is this
// project's own shape, deliberately not the backend library's: modules
// express limits in these terms and never import the limiter
// implementation.
type Policy struct {
	// Rate is how many requests are permitted per Period once the
	// bucket is in its steady state.
	Rate int

	// Burst is how many requests may arrive at once from an idle key.
	// Equal to Rate means "a full period's worth may arrive instantly";
	// lower means the allowance is smoothed out.
	Burst int

	// Period is the window Rate refers to.
	Period time.Duration
}

// PerMinute is the common case: a whole minute's allowance may arrive
// at once, then refills continuously. Continuous refill is what makes
// these limits throttling rather than lockout — a throttled caller is
// delayed, never shut out for a fixed penalty window.
func PerMinute(rate int) Policy {
	return Policy{Rate: rate, Burst: rate, Period: time.Minute}
}

// Validate rejects a policy that cannot mean anything useful.
//
// Called at startup for statically configured policies, so a bad limit
// fails the process rather than surfacing as a strange result on the
// first request that happens to hit it. The backend's Lua script is
// not a validation layer: a zero rate there means a division by zero
// inside Redis, not a clear error.
func (p Policy) Validate() error {
	if p.Rate <= 0 {
		return fmt.Errorf("ratelimit: Rate must be greater than zero, got %d", p.Rate)
	}

	if p.Burst < 1 {
		return fmt.Errorf("ratelimit: Burst must be at least 1, got %d", p.Burst)
	}

	if p.Period <= 0 {
		return fmt.Errorf("ratelimit: Period must be greater than zero, got %s", p.Period)
	}

	return nil
}
