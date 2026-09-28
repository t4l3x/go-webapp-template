package response

import (
	"math"
	"net/http"
	"strconv"
	"time"
)

// SetRetryAfter sets the Retry-After header for a 429, in the integer
// seconds RFC 9110 requires. Call it before the response is written.
//
// Rounded up, always. Rounding 1.2s down to 1 would invite the client
// back before its allowance exists, so a well-behaved client retrying
// exactly when told would be rejected again — and a client that trusts
// the header would loop. A sub-second wait becomes 1 rather than 0 for
// the same reason: 0 means "retry immediately", which is never true of
// a request that was just refused.
func SetRetryAfter(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int64(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}

	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
}
