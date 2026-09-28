package identity

import (
	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/postgres"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
)

// CoreModule declares shared persistence once. Process modules choose use cases.
var CoreModule = fx.Module(
	"identity-core",

	fx.Provide(
		fx.Annotate(postgres.NewVerificationStore, fx.As(new(application.VerificationStore)), fx.As(new(application.VerificationDeliveryStore))),

		fx.Annotate(postgres.NewUserRepository, fx.As(new(application.UserRepository))),
		fx.Annotate(postgres.NewRegistrationStore, fx.As(new(application.RegistrationStore))),
		fx.Annotate(postgres.NewSessionRepository, fx.As(new(application.SessionRepository))),
	),
)

// Both delivery and verification need this key; neither depends on JWT signing.
var verificationTokenProviders = fx.Provide(
	LoadVerificationTokenConfig,
	func(cfg VerificationTokenConfig) security.VerificationTokenConfig {
		return security.VerificationTokenConfig{Secret: cfg.Secret}
	},
	security.NewVerificationSigner,
)
