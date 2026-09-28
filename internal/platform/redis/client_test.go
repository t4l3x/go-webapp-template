package redis_test

import (
	"context"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/platform/redis"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

// fakeLifecycle captures the hooks RegisterLifecycle appends, so a test
// can drive OnStart/OnStop directly without building a whole *fx.App.
type fakeLifecycle struct {
	hooks []fx.Hook
}

func (l *fakeLifecycle) Append(h fx.Hook) {
	l.hooks = append(l.hooks, h)
}

func (l *fakeLifecycle) start(ctx context.Context) error {
	for _, h := range l.hooks {
		if h.OnStart == nil {
			continue
		}
		if err := h.OnStart(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (l *fakeLifecycle) stop(ctx context.Context) error {
	for _, h := range l.hooks {
		if h.OnStop == nil {
			continue
		}
		if err := h.OnStop(ctx); err != nil {
			return err
		}
	}

	return nil
}

// unreachableClient points at an address nothing listens on, with a
// short dial timeout so the test fails fast rather than waiting on the
// OS's own connect timeout.
func unreachableClient() *goredis.Client {
	return goredis.NewClient(&goredis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 200 * time.Millisecond,
	})
}

// TestRegisterLifecycle_StartupConnectivityFailureDoesNotFailStartup is
// the fail-open change: Redis backs only the rate limiter, which already
// tolerates a down backend at request time, so a briefly unreachable
// Redis must not stop the whole API from booting.
func TestRegisterLifecycle_StartupConnectivityFailureDoesNotFailStartup(t *testing.T) {
	logger, logs := testkit.NewLogger()
	client := unreachableClient()
	t.Cleanup(func() { _ = client.Close() })

	lifecycle := &fakeLifecycle{}
	redis.RegisterLifecycle(lifecycle, client, logger)

	if err := lifecycle.start(context.Background()); err != nil {
		t.Fatalf("OnStart() error = %v, want startup to succeed despite Redis being unreachable", err)
	}

	if !strings.Contains(logs.String(), "redis unreachable at startup") {
		t.Fatalf("logs = %q, want a warning that Redis was unreachable", logs.String())
	}
}

// TestRegisterLifecycle_ClosesClientOnStop pins the one thing that must
// still happen unconditionally on shutdown, fail-open startup or not.
func TestRegisterLifecycle_ClosesClientOnStop(t *testing.T) {
	logger, _ := testkit.NewLogger()
	client := unreachableClient()

	lifecycle := &fakeLifecycle{}
	redis.RegisterLifecycle(lifecycle, client, logger)

	if err := lifecycle.stop(context.Background()); err != nil {
		t.Fatalf("OnStop() error = %v", err)
	}

	// A client used after Close reports it plainly, which is the
	// simplest observable proof the pool was actually closed.
	if err := client.Ping(context.Background()).Err(); err == nil {
		t.Fatalf("Ping() succeeded after Close(), want the client to be shut down")
	}
}
