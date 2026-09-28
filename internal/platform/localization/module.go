package localization

import "go.uber.org/fx"

// Module wires the localization engine. Include it in a process that
// generates human-facing text — currently only cmd/worker, which sends
// email; the API returns machine-readable error codes and needs none of
// this.
//
// Modules contribute their own wording by providing a Catalog into the
// "localization_catalogs" group:
//
//	fx.Annotate(
//	    func() localization.Catalog { ... },
//	    fx.ResultTags(`group:"localization_catalogs"`),
//	)
//
// The group is the whole registration mechanism — the same pattern as
// outbox_handlers and http_routes. It keeps the dependency pointing one
// way: modules know about localization, localization knows about no
// module.
var Module = fx.Module(
	"localization",

	fx.Provide(
		LoadConfig,

		NewEngine,
		func(engine *Engine) Localizer { return engine },
	),
)
