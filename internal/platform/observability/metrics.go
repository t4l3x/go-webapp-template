package observability

import (
	"context"
	"fmt"

	"github.com/caarlos0/env/v11"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/config"
)

// MetricsConfig switches OpenTelemetry metrics export on. Where they go
// is not configured here: the OTLP exporter reads the standard
// OTEL_EXPORTER_OTLP_* variables itself (endpoint, headers, TLS), so
// deployments configure it the way every OTel SDK is configured.
type MetricsConfig struct {
	Enabled bool `env:"OTEL_METRICS_ENABLED" envDefault:"false"`
}

func LoadMetricsConfig() (MetricsConfig, error) {
	cfg, err := env.ParseAs[MetricsConfig]()
	if err != nil {
		return MetricsConfig{}, fmt.Errorf("load metrics config: %w", err)
	}

	return cfg, nil
}

// meterScope names the instrumentation scope of this project's own
// instruments.
const meterScope = "github.com/t4l3x/go-webapp-template"

// NewMeterProvider returns an OTLP/HTTP-exporting provider when metrics
// are enabled, otherwise a no-op one — so instruments are always safe to
// create and record, and nothing is exported (or dialed) locally unless
// asked for. The provider is flushed and shut down with the process.
func NewMeterProvider(lc fx.Lifecycle, cfg MetricsConfig, app config.App) (metric.MeterProvider, error) {
	if !cfg.Enabled {
		return noop.NewMeterProvider(), nil
	}

	exporter, err := otlpmetrichttp.New(context.Background())
	if err != nil {
		return nil, fmt.Errorf("create OTLP metric exporter: %w", err)
	}

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", app.Service),
		attribute.String("service.version", app.Version),
		attribute.String("deployment.environment.name", app.Environment),
	))
	if err != nil {
		return nil, fmt.Errorf("build metrics resource: %w", err)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
		sdkmetric.WithResource(res),
	)

	lc.Append(fx.Hook{OnStop: provider.Shutdown})

	return provider, nil
}
