//go:build integration

package ratelimit_test

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

// These run against the exact Redis image docker-compose.test.yml
// ships. That is the point of having them: redis_rate's GCRA logic is a
// Lua script that calls redis.replicate_commands() and TIME, and its
// behavior is a property of the Redis server, not of any Go code that
// unit tests could cover.

func testScope(t *testing.T) string {
	t.Helper()

	// Unique per test so parallel or repeated runs never share a bucket,
	// and so cleanup can delete exactly this test's keys.
	return fmt.Sprintf("test.%s.%d", t.Name(), time.Now().UnixNano())
}

func newIntegrationLimiter(t *testing.T) *ratelimit.RedisLimiter {
	t.Helper()

	return ratelimit.NewRedisLimiter(newTestRedis(t), ratelimit.Config{Timeout: 5 * time.Second})
}

// TestRedisLimiter_LuaScriptRunsOnShippedRedis is the compatibility
// check. redis_rate is lightly maintained and its script has historically
// depended on server-version details, so a straight "does it execute and
// return a sane result" test is worth more here than any amount of
// mocking.
func TestRedisLimiter_LuaScriptRunsOnShippedRedis(t *testing.T) {
	client := newTestRedis(t)
	scope := testScope(t)
	t.Cleanup(func() { deleteKeys(t, client, scope) })

	limiter := ratelimit.NewRedisLimiter(client, ratelimit.Config{Timeout: 5 * time.Second})
	key := ratelimit.NewIPKey(scope, netip.MustParseAddr("198.51.100.7"))

	result, err := limiter.Allow(context.Background(), key, ratelimit.PerMinute(10))
	if err != nil {
		t.Fatalf("Allow() error = %v — the GCRA script did not run on this Redis", err)
	}
	if !result.Allowed {
		t.Fatalf("Allowed = false on the first request of an empty bucket")
	}
	if result.Remaining != 9 {
		t.Fatalf("Remaining = %d, want 9 after one of ten", result.Remaining)
	}
	if result.RetryAfter != 0 {
		t.Fatalf("RetryAfter = %s, want 0 while allowed", result.RetryAfter)
	}

	version, err := client.Info(context.Background(), "server").Result()
	if err != nil {
		t.Fatalf("read server info: %v", err)
	}

	t.Logf("GCRA script verified against: %s", firstLineContaining(version, "redis_version"))
}

// TestRedisLimiter_BurstThenDeny checks the shape of the allowance: a
// full burst may arrive at once, and the next request is refused.
func TestRedisLimiter_BurstThenDeny(t *testing.T) {
	client := newTestRedis(t)
	scope := testScope(t)
	t.Cleanup(func() { deleteKeys(t, client, scope) })

	limiter := ratelimit.NewRedisLimiter(client, ratelimit.Config{Timeout: 5 * time.Second})
	key := ratelimit.NewIPKey(scope, netip.MustParseAddr("198.51.100.7"))
	policy := ratelimit.Policy{Rate: 5, Burst: 5, Period: time.Minute}

	for i := range 5 {
		result, err := limiter.Allow(context.Background(), key, policy)
		if err != nil {
			t.Fatalf("Allow() #%d error = %v", i+1, err)
		}
		if !result.Allowed {
			t.Fatalf("request %d of a burst of 5 was denied", i+1)
		}
	}

	result, err := limiter.Allow(context.Background(), key, policy)
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if result.Allowed {
		t.Fatalf("the 6th request against a burst of 5 was allowed")
	}
	if result.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %s, want a positive wait when denied", result.RetryAfter)
	}
	// One slot of a 5-per-minute allowance refills in ~12s; anything
	// near a full period would mean the result is not GCRA-shaped.
	if result.RetryAfter > 15*time.Second {
		t.Fatalf("RetryAfter = %s, want roughly one refill interval", result.RetryAfter)
	}
}

// TestRedisLimiter_SeparateLimiterInstancesShareState is what makes this
// distributed rather than per-process: two limiter objects, as two API
// instances would have, must draw down one allowance.
func TestRedisLimiter_SeparateLimiterInstancesShareState(t *testing.T) {
	client := newTestRedis(t)
	scope := testScope(t)
	t.Cleanup(func() { deleteKeys(t, client, scope) })

	instanceA := ratelimit.NewRedisLimiter(client, ratelimit.Config{Timeout: 5 * time.Second})
	instanceB := ratelimit.NewRedisLimiter(newTestRedis(t), ratelimit.Config{Timeout: 5 * time.Second})

	key := ratelimit.NewIPKey(scope, netip.MustParseAddr("198.51.100.7"))
	policy := ratelimit.Policy{Rate: 4, Burst: 4, Period: time.Minute}

	// Two each, alternating, exhausts the shared allowance of four.
	for i := range 2 {
		for _, limiter := range []*ratelimit.RedisLimiter{instanceA, instanceB} {
			result, err := limiter.Allow(context.Background(), key, policy)
			if err != nil {
				t.Fatalf("Allow() round %d error = %v", i, err)
			}
			if !result.Allowed {
				t.Fatalf("round %d was denied before the shared allowance ran out", i)
			}
		}
	}

	result, err := instanceA.Allow(context.Background(), key, policy)
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if result.Allowed {
		t.Fatalf("the two instances did not share one allowance — each kept its own count")
	}
}

// TestRedisLimiter_ConcurrentRequestsShareOneAllowance is the race the
// Lua script exists to prevent: read-modify-write in Go would let
// several callers each observe a free slot and all take it.
func TestRedisLimiter_ConcurrentRequestsShareOneAllowance(t *testing.T) {
	client := newTestRedis(t)
	scope := testScope(t)
	t.Cleanup(func() { deleteKeys(t, client, scope) })

	limiter := ratelimit.NewRedisLimiter(client, ratelimit.Config{Timeout: 5 * time.Second})
	key := ratelimit.NewIPKey(scope, netip.MustParseAddr("198.51.100.7"))

	const (
		allowance = 10
		attempts  = 50
	)

	policy := ratelimit.Policy{Rate: allowance, Burst: allowance, Period: time.Minute}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed int
	)

	// A start barrier rather than a sleep: every goroutine is released
	// at once, so the requests genuinely overlap.
	start := make(chan struct{})

	for range attempts {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-start

			result, err := limiter.Allow(context.Background(), key, policy)
			if err != nil {
				return
			}

			if result.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}

	close(start)
	wg.Wait()

	if allowed != allowance {
		t.Fatalf("allowed = %d of %d concurrent attempts, want exactly %d — the decision is not atomic",
			allowed, attempts, allowance)
	}
}

// TestRedisLimiter_KeysExpire proves the limiter leaves no permanent
// state behind. redis_rate sets the TTL inside its script, so this also
// confirms this package is right not to manage expiry itself.
func TestRedisLimiter_KeysExpire(t *testing.T) {
	client := newTestRedis(t)
	scope := testScope(t)
	t.Cleanup(func() { deleteKeys(t, client, scope) })

	limiter := ratelimit.NewRedisLimiter(client, ratelimit.Config{Timeout: 5 * time.Second})
	key := ratelimit.NewIPKey(scope, netip.MustParseAddr("198.51.100.7"))

	if _, err := limiter.Allow(context.Background(), key, ratelimit.PerMinute(10)); err != nil {
		t.Fatalf("Allow() error = %v", err)
	}

	ttl, err := client.TTL(context.Background(), "rate:"+key.String()).Result()
	if err != nil {
		t.Fatalf("read TTL: %v", err)
	}

	// -1 means the key exists with no expiry, which would accumulate
	// forever; -2 means it is already gone.
	if ttl <= 0 {
		t.Fatalf("TTL = %s, want a positive expiry so limiter state is ephemeral", ttl)
	}
	if ttl > 2*time.Minute {
		t.Fatalf("TTL = %s, want it bounded by roughly the policy period", ttl)
	}
}

// TestRedisLimiter_InvalidPolicyIsRejectedBeforeRedis makes sure a
// malformed policy never reaches the script, where Rate == 0 would be a
// division by zero inside Redis rather than a clear Go error.
func TestRedisLimiter_InvalidPolicyIsRejectedBeforeRedis(t *testing.T) {
	limiter := newIntegrationLimiter(t)
	key := ratelimit.NewIPKey(testScope(t), netip.MustParseAddr("198.51.100.7"))

	if _, err := limiter.Allow(context.Background(), key, ratelimit.Policy{}); err == nil {
		t.Fatalf("Allow() error = nil, want the zero policy rejected before Redis")
	}
}

// TestRedisLimiter_BackendErrorPropagates confirms the adapter reports
// failures rather than inventing a decision — the fail-open choice is
// made one layer up, deliberately.
func TestRedisLimiter_BackendErrorPropagates(t *testing.T) {
	client := newTestRedis(t)
	limiter := ratelimit.NewRedisLimiter(client, ratelimit.Config{Timeout: 5 * time.Second})

	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}

	key := ratelimit.NewIPKey(testScope(t), netip.MustParseAddr("198.51.100.7"))

	_, err := limiter.Allow(context.Background(), key, ratelimit.PerMinute(10))
	if err == nil {
		t.Fatalf("Allow() error = nil, want a backend failure reported")
	}
	got := err.Error()
	if !strings.Contains(got, "ratelimit") || !strings.Contains(got, "redis") {
		t.Fatalf("error = %q, want it to identify the limiter and backend", got)
	}
}

func firstLineContaining(s, needle string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.Contains(line, needle) {
			return strings.TrimSpace(line)
		}
	}

	return ""
}
