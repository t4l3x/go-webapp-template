package outbox

import "go.uber.org/fx"

var Module = fx.Module(
	"outbox",

	fx.Provide(
		LoadConfig,
		NewRunner,
	),

	fx.Invoke(
		RegisterRunnerLifecycle,
	),
)
