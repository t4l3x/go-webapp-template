//go:build integration

package outbox

import (
	"log/slog"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

// outboxTestDBPrefix is the required prefix for any disposable database
// this package's integration tests create via testkit.NewPostgresTestDB.
const outboxTestDBPrefix = "app_test_outbox_"

// testConfig returns a Config tuned for fast, deterministic tests: a
// short poll interval so Runner tests don't need to wait long, and a
// short base backoff.
func testConfig() Config {
	return Config{
		PollInterval:    20 * time.Millisecond,
		BatchSize:       10,
		MaxAttempts:     10,
		ClaimLease:      time.Minute,
		BaseBackoff:     50 * time.Millisecond,
		MaxBackoff:      time.Second,
		ShutdownTimeout: 5 * time.Second,
	}
}

func testLogger() *slog.Logger {
	logger, _ := testkit.NewLogger()

	return logger
}
