package httpserver

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/config"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

// operationalPaths are this router's own unversioned endpoints. They
// are exempt from rate limiting: they exist for orchestrators and
// monitoring, which poll them from a small number of addresses at a
// rate that has nothing to do with user traffic.
var operationalPaths = []string{"/health"}

var allowedRouteMethods = map[string]struct{}{
	http.MethodGet:    {},
	http.MethodPost:   {},
	http.MethodPut:    {},
	http.MethodPatch:  {},
	http.MethodDelete: {},
}

type RouterParams struct {
	fx.In

	Routes []Route `group:"http_routes"`
}

func NewRouter(
	params RouterParams,
	cfg config.HTTP,
	logger *slog.Logger,
	responder *response.Responder,
	limiter ratelimit.Limiter,
	resolver *clientip.Resolver,
	rateLimitCfg ratelimit.Config,
) (http.Handler, error) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", health(responder))

	if err := registerRoutes(mux, params.Routes); err != nil {
		return nil, err
	}

	httpLogger := logger.With(
		"component", "http",
	)

	// Order matters, outermost first:
	//
	//   RequestID  every request gets an id, including rejected ones
	//   ClientIP   resolves the caller once (trusted-proxy policy); the
	//              access log, limiters and handlers all read that value
	//   AccessLog  a 429 is still logged, with that id and address
	//   Recovery   catches panics from everything inside, limiter included
	//   CORS       429s carry CORS headers so a browser can read them;
	//              preflight OPTIONS is answered here and never spends
	//              a caller's allowance
	//   RateLimit  innermost, so the router never runs for a denied request
	return middleware.Chain(
		mux,

		middleware.RequestID,
		middleware.ClientIP(resolver),
		middleware.AccessLog(httpLogger),
		middleware.Recovery(responder),
		middleware.CORS(cfg.CORSAllowedOrigins),
		middleware.RateLimit(
			limiter,
			responder,
			rateLimitCfg.GlobalPolicy(),
			operationalPaths,
		),
	), nil
}

func registerRoutes(mux *http.ServeMux, routes []Route) error {
	seen := make(map[string]struct{}, len(routes))

	for _, route := range routes {
		if _, ok := allowedRouteMethods[route.Method]; !ok {
			return fmt.Errorf("httpserver: invalid route method %q for path %q", route.Method, route.Path)
		}

		if !strings.HasPrefix(route.Path, "/") {
			return fmt.Errorf("httpserver: route path %q must start with %q", route.Path, "/")
		}

		if route.Handler == nil {
			return fmt.Errorf("httpserver: route %s %s has a nil handler", route.Method, route.Path)
		}

		key := route.pattern()

		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("httpserver: duplicate route %s %s", route.Method, route.Path)
		}
		seen[key] = struct{}{}

		mux.Handle(key, route.Handler)
	}

	return nil
}

func health(responder *response.Responder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		responder.JSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	}
}
