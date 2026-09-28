package observability

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestProtectionSignal_TransitionSummaryRecoveryAndCounter(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	logger, logs := testkit.NewLogger()

	signal, err := NewProtectionSignal(provider, logger, "test_protection_unavailable")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	signal.now = func() time.Time { return clock }

	ctx := context.Background()
	errDown := errors.New("redis down")

	signal.Succeeded() // healthy: no log
	for range 500 {
		signal.Failed(ctx, "scope.a", errDown)
	}
	clock = clock.Add(59 * time.Second)
	signal.Failed(ctx, "scope.a", errDown) // still inside the interval: no log
	clock = clock.Add(2 * time.Second)
	signal.Failed(ctx, "scope.a", errDown) // interval passed: one summary
	signal.Succeeded()                     // recovery log
	signal.Succeeded()                     // already healthy: no log

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d log lines, want 3 (unavailable, summary, recovered):\n%s", len(lines), logs.String())
	}
	for i, want := range []string{`"state":"unavailable"`, `"failures_since_last_log":501`, `"state":"recovered"`} {
		if !strings.Contains(lines[i], want) || !strings.Contains(lines[i], `"event":"test_protection_unavailable"`) {
			t.Fatalf("line %d = %s, want it to contain %s and the event name", i, lines[i], want)
		}
	}
	if !strings.Contains(lines[2], `"failures":502`) {
		t.Fatalf("recovery line = %s, want the outage's total of 502 failures", lines[2])
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	if got := counterTotal(rm, "test_protection_unavailable"); got != 502 {
		t.Fatalf("counter = %d, want 502 (every failure, logged or not)", got)
	}
}

func counterTotal(rm metricdata.ResourceMetrics, name string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == name {
				for _, dp := range sum.DataPoints {
					total += dp.Value
				}
			}
		}
	}
	return total
}
