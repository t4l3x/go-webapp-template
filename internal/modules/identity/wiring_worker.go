package identity

import (
	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/config"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/translations"
	identityworker "github.com/t4l3x/go-webapp-template/internal/modules/identity/transport/worker"
	"github.com/t4l3x/go-webapp-template/internal/platform/localization"
	"github.com/t4l3x/go-webapp-template/internal/platform/outbox"
)

// WorkerModule composes delivery only. It does not load identity's JWT config.
var WorkerModule = fx.Module(
	"identity-worker",

	verificationTokenProviders,

	fx.Provide(
		func(appCfg config.App) application.DeliveryConfig {
			return application.DeliveryConfig{
				AppPublicURL: appCfg.PublicURL,
			}
		},

		func(signer *security.VerificationSigner) application.VerificationSigner { return signer },

		// Identity contributes its own wording to the localization
		// engine. The dependency points this way only: identity knows
		// platform/localization exists, platform/localization knows
		// nothing about identity. It is registered here rather than in
		// CoreModule because the worker is the only process that renders
		// human-facing text today.
		fx.Annotate(
			func() localization.Catalog {
				return localization.Catalog{
					Owner: "identity",
					Files: translations.Files,
				}
			},
			fx.ResultTags(`group:"localization_catalogs"`),
		),

		application.NewDeliverEmailVerificationService,
		fx.Annotate(
			func(delivery *application.DeliverEmailVerificationService) outbox.HandlerRegistration {
				return identityworker.NewEmailVerificationHandler(delivery).Registration()
			},
			fx.ResultTags(`group:"outbox_handlers"`),
		),
	),
)
