package domain

import (
	"fmt"
	"time"
)

// ResendWindow is the rolling window ResendPolicy.MaxPerWindow counts in.
const ResendWindow = 24 * time.Hour

// ResendPolicy bounds how often one account is sent a verification
// email. It is keyed by account, not by caller, so a new IP address
// buys nothing — the IP limit on the endpoint is a separate, coarser
// layer. It throttles (the caller is told when to retry); it never locks
// the account.
type ResendPolicy struct {
	// Cooldown is the minimum time since the most recently issued
	// credential, including the one issued at registration.
	Cooldown time.Duration

	// MaxPerWindow caps credentials issued in any rolling ResendWindow,
	// again including the registration one.
	MaxPerWindow int
}

// ResendLimitError reports that a new credential may not be issued yet.
// Wait is exact: retrying after it is guaranteed not to hit this same
// limit again (barring new sends in the meantime).
type ResendLimitError struct {
	Wait time.Duration
}

func (e *ResendLimitError) Error() string {
	return fmt.Sprintf("verification resend limited; retry after %s", e.Wait)
}

// RetryAfter lets transports set a Retry-After header without knowing
// this type.
func (e *ResendLimitError) RetryAfter() time.Duration {
	return e.Wait
}

// Check decides whether a credential may be issued at now, given the
// creation times of credentials already issued within ResendWindow
// before now, newest first. The caller must hold the per-account lock
// while reading issued and acting on the result, or concurrent requests
// could each see the same history and all pass.
func (p ResendPolicy) Check(issued []time.Time, now time.Time) error {
	var retryAfter time.Duration

	if len(issued) > 0 {
		if wait := issued[0].Add(p.Cooldown).Sub(now); wait > retryAfter {
			retryAfter = wait
		}
	}

	// The window frees a slot when the MaxPerWindow-th newest credential
	// ages out; only then does the count drop below the cap.
	if len(issued) >= p.MaxPerWindow {
		if wait := issued[p.MaxPerWindow-1].Add(ResendWindow).Sub(now); wait > retryAfter {
			retryAfter = wait
		}
	}

	if retryAfter > 0 {
		return &ResendLimitError{Wait: retryAfter}
	}

	return nil
}
