//go:build integration

package ratelimit_test

import (
	"context"
	"errors"
	"os"
	"testing"

	goredis "github.com/redis/go-redis/v9"

	"github.com/t4l3x/go-webapp-template/internal/platform/redis"
)

// newTestRedis connects to the integration suite's dedicated Redis (see
// docker-compose.test.yml) and returns a client plus a unique key
// prefix for the calling test.
//
// Keys are namespaced per test and deleted on cleanup rather than
// flushed: FLUSHALL/FLUSHDB against a Redis someone might have pointed
// at a development instance would destroy unrelated state, and the
// point of a limiter test is never worth that risk.
func newTestRedis(t *testing.T) *goredis.Client {
	t.Helper()

	url := os.Getenv("REDIS_URL_TEST")
	if url == "" {
		t.Skip("REDIS_URL_TEST is not set; run via `make test-integration`")
	}

	t.Setenv("REDIS_URL", url)

	cfg, err := redis.LoadConfig()
	if err != nil {
		t.Fatalf("load redis config: %v", err)
	}

	client, err := redis.NewClient(cfg)
	if err != nil {
		t.Fatalf("build redis client: %v", err)
	}

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}

	t.Cleanup(func() {
		// A test that deliberately closes the client to provoke a
		// backend failure has already done this; that is not a cleanup
		// error.
		if err := client.Close(); err != nil && !errors.Is(err, goredis.ErrClosed) {
			t.Errorf("close redis: %v", err)
		}
	})

	return client
}

// deleteKeys removes only what a test created. redis_rate prefixes
// every key it writes with "rate:", and each test scopes its keys with
// a unique name, so this scan is narrow by construction.
func deleteKeys(t *testing.T, client *goredis.Client, scope string) {
	t.Helper()

	ctx := context.Background()
	pattern := "rate:" + scope + ":*"

	iter := client.Scan(ctx, 0, pattern, 100).Iterator()

	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if err := iter.Err(); err != nil {
		t.Fatalf("scan %q: %v", pattern, err)
	}

	if len(keys) == 0 {
		return
	}

	if err := client.Del(ctx, keys...).Err(); err != nil {
		t.Fatalf("delete %v: %v", keys, err)
	}
}
