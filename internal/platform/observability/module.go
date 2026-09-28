package observability

import (
	"go.uber.org/fx"
)

var Module = fx.Module(
	"observability",

	fx.Provide(
		NewLogger,
		LoadMetricsConfig,
		NewMeterProvider,
	),
	fx.Invoke(SetDefaultLogger),
)
