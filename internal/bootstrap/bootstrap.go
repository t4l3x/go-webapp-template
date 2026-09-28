package bootstrap

import (
	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/config"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity"
	"github.com/t4l3x/go-webapp-template/internal/platform/database"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver"
	"github.com/t4l3x/go-webapp-template/internal/platform/localization"
	"github.com/t4l3x/go-webapp-template/internal/platform/mail"
	"github.com/t4l3x/go-webapp-template/internal/platform/observability"
	"github.com/t4l3x/go-webapp-template/internal/platform/outbox"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
	"github.com/t4l3x/go-webapp-template/internal/platform/redis"
)

// Constructors here are named for the process they build, one per
// binary under cmd/ — NewAPI().Run() and NewWorker().Run(). Each lists
// both the platform modules it needs and, for every business module,
// exactly which of that module's adapters it runs: a module's shared
// providers plus its HTTP or worker composition (see
// identity.CoreModule/HTTPModule/WorkerModule). Which adapters a process
// builds — and therefore which secrets it loads — is stated here, not
// inferred from which providers happened to find a consumer.

// NewAPI builds the HTTP API process (cmd/api).
//
// It is the only process composing redis.Module and ratelimit.Module:
// rate limiting protects inbound traffic, and the worker has none. The
// worker does not get a Redis connection merely because the API needs
// one.
//
// It loads the JWT key for sessions and the email key for token verification.
func NewAPI() *fx.App {
	return fx.New(
		fx.RecoverFromPanics(),

		config.Module,
		observability.Module,
		database.Module,
		redis.Module,
		ratelimit.Module,
		httpserver.Module,

		identity.CoreModule,
		identity.HTTPModule,

		fx.WithLogger(observability.NewFxLogger),
	)
}

// NewWorker builds the background worker process (cmd/worker): it
// polls the PostgreSQL outbox and dispatches claimed events to
// module-owned handlers (see internal/platform/outbox,
// internal/platform/mail). It runs no HTTP server, and deliberately
// includes no module's HTTP composition.
//
// It is the only process that composes localization.Module: the worker
// generates human-facing text (email), while the API returns stable
// machine-readable error codes a client localizes itself.
//
// Of identity's secrets it loads only AUTH_EMAIL_VERIFICATION_SECRET:
// it signs verification links but never issues or checks sessions.
func NewWorker() *fx.App {
	return fx.New(
		fx.RecoverFromPanics(),

		config.Module,
		observability.Module,
		database.Module,
		mail.Module,
		localization.Module,
		outbox.Module,

		identity.CoreModule,
		identity.WorkerModule,

		fx.Invoke(validateWorkerDeliveryBudget),

		fx.WithLogger(observability.NewFxLogger),
	)
}

func validateWorkerDeliveryBudget(outboxCfg outbox.Config, mailCfg mail.Config) error {
	return outboxCfg.ValidateHandlerTimeout(mailCfg.Timeout)
}
