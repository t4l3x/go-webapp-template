package httpserver

import (
	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/config"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
)

var Module = fx.Module(
	"httpserver",

	fx.Provide(
		response.NewResponder,
		NewRouter,
		NewHTTPServer,

		func(cfg config.HTTP) *clientip.Resolver {
			return clientip.NewResolver(cfg.TrustedProxies)
		},
	),

	fx.Invoke(
		RegisterHTTPServerLifecycle,
	),
)
