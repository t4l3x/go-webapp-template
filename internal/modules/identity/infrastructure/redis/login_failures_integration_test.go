//go:build integration

package redis_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric/noop"

	identityredis "github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/redis"
	"github.com/t4l3x/go-webapp-template/internal/platform/observability"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

const keyPrefix = "abuse:identity.login.account_ip:"

// newCounter connects to the integration suite's dedicated Redis and
// deletes this adapter's keys before and after the test. Only keys under
// its own prefix are touched — never FLUSHDB.
func newCounter(t *testing.T) (*identityredis.LoginFailureCounter, *goredis.Client) {
	t.Helper()

	url := os.Getenv("REDIS_URL_TEST")
	if url == "" {
		t.Skip("REDIS_URL_TEST is not set; run via `make test-integration`")
	}
	opts, err := goredis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	client := goredis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}

	deleteKeys(t, client)
	t.Cleanup(func() {
		deleteKeys(t, client)
		if err := client.Close(); err != nil && !errors.Is(err, goredis.ErrClosed) {
			t.Errorf("close redis: %v", err)
		}
	})

	counter := identityredis.NewLoginFailureCounter(client, "test-abuse-key-secret-that-is-32-bytes-plus", time.Second,
		newSignal(t, slog.New(slog.DiscardHandler)))
	return counter, client
}

func newSignal(t *testing.T, logger *slog.Logger) *observability.ProtectionSignal {
	t.Helper()
	signal, err := observability.NewProtectionSignal(noop.NewMeterProvider(), logger, identityredis.ProtectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	return signal
}

func keys(t *testing.T, client *goredis.Client) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	iter := client.Scan(ctx, 0, keyPrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		out = append(out, iter.Val())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func deleteKeys(t *testing.T, client *goredis.Client) {
	t.Helper()
	if k := keys(t, client); len(k) > 0 {
		if err := client.Del(context.Background(), k...).Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func ptr(s string) *string { return &s }

func TestLoginFailureCounter_KeysNeverContainTheEmail(t *testing.T) {
	counter, client := newCounter(t)
	ctx := context.Background()

	counter.Reserve(ctx, "victim.person@example.com", ptr("198.51.100.7"), time.Minute)

	got := keys(t, client)
	if len(got) != 1 {
		t.Fatalf("keys = %v, want exactly one", got)
	}
	for _, fragment := range []string{"victim", "person", "example.com", "@"} {
		if strings.Contains(got[0], fragment) {
			t.Fatalf("key %q contains %q from the raw email", got[0], fragment)
		}
	}
	if !strings.HasSuffix(got[0], ":ip:198.51.100.7") {
		t.Fatalf("key %q, want it keyed by the client IP", got[0])
	}
}

func TestLoginFailureCounter_IsolatedByIPAndCountsInWindow(t *testing.T) {
	counter, client := newCounter(t)
	ctx := context.Background()

	for want := 1; want <= 3; want++ {
		if n, remaining := counter.Reserve(ctx, "user@example.com", ptr("198.51.100.7"), time.Minute); n != want || remaining <= 0 || remaining > time.Minute {
			t.Fatalf("Reserve #%d = %d, %s; want %d within a 1m window", want, n, remaining, want)
		}
	}
	if n, _ := counter.Reserve(ctx, "user@example.com", ptr("203.0.113.9"), time.Minute); n != 1 {
		t.Fatalf("another IP started at %d, want 1 (separate bucket)", n)
	}

	if k := keys(t, client); len(k) != 2 {
		t.Fatalf("keys = %v, want 2", k)
	}

	// The window is fixed at the first attempt: a later attempt (even one
	// asking for a longer window) does not extend it.
	if _, remaining := counter.Reserve(ctx, "user@example.com", ptr("198.51.100.7"), time.Hour); remaining > time.Minute {
		t.Fatalf("remaining = %s after a later attempt, want the original <= 1m window", remaining)
	}
}

func TestLoginFailureCounter_ResetClearsOnlyThatBucket(t *testing.T) {
	counter, _ := newCounter(t)
	ctx := context.Background()

	counter.Reserve(ctx, "user@example.com", ptr("198.51.100.7"), time.Minute)
	counter.Reserve(ctx, "user@example.com", ptr("198.51.100.7"), time.Minute)
	counter.Reserve(ctx, "user@example.com", ptr("203.0.113.9"), time.Minute)

	counter.Reset(ctx, "user@example.com", ptr("198.51.100.7"))

	if n, _ := counter.Reserve(ctx, "user@example.com", ptr("198.51.100.7"), time.Minute); n != 1 {
		t.Fatalf("after reset count = %d, want 1", n)
	}
	if n, _ := counter.Reserve(ctx, "user@example.com", ptr("203.0.113.9"), time.Minute); n != 2 {
		t.Fatalf("other IP's count = %d, want 2 (untouched by the reset)", n)
	}
}

// Concurrent reserves each get a distinct count: the increment is atomic
// in Redis, so no two attempts can both see "below the threshold".
func TestLoginFailureCounter_ConcurrentReservesGetDistinctCounts(t *testing.T) {
	counter, _ := newCounter(t)

	const attempts = 30
	start := make(chan struct{})
	counts := make(chan int, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Go(func() {
			<-start
			n, _ := counter.Reserve(context.Background(), "user@example.com", ptr("198.51.100.7"), time.Minute)
			counts <- n
		})
	}
	close(start)
	wg.Wait()
	close(counts)

	seen := map[int]bool{}
	for n := range counts {
		if n < 1 || n > attempts || seen[n] {
			t.Fatalf("count %d duplicated or out of range — reserves were not atomic", n)
		}
		seen[n] = true
	}
}

func TestLoginFailureCounter_FailsOpenWhenRedisIsDown(t *testing.T) {
	_, client := newCounter(t) // skips without REDIS_URL_TEST; its client stays open for cleanup

	// A separate client, closed on purpose, stands in for an unreachable Redis.
	dead := goredis.NewClient(client.Options())
	if err := dead.Close(); err != nil {
		t.Fatal(err)
	}
	logger, logs := testkit.NewLogger()
	counter := identityredis.NewLoginFailureCounter(dead, "test-abuse-key-secret-that-is-32-bytes-plus", time.Second, newSignal(t, logger))

	for range 100 {
		if n, _ := counter.Reserve(context.Background(), "user@example.com", ptr("198.51.100.7"), time.Minute); n != 0 {
			t.Fatalf("Reserve with Redis down = %d, want 0 (fail open)", n)
		}
	}

	// Fail-open is only acceptable if it is loud and alertable — but one
	// transition line, not one per attempt — and it must not leak the
	// subject.
	out := logs.String()
	if !strings.Contains(out, `"level":"ERROR"`) || !strings.Contains(out, `"event":"auth_abuse_protection_unavailable"`) {
		t.Fatalf("expected an ERROR log with event=auth_abuse_protection_unavailable, got: %s", out)
	}
	if n := strings.Count(out, "\n"); n != 1 {
		t.Fatalf("100 failed attempts produced %d log lines, want 1", n)
	}
	if strings.Contains(out, "user@example.com") || strings.Contains(out, "198.51.100.7") {
		t.Fatalf("log leaked the account or IP: %s", out)
	}
}
