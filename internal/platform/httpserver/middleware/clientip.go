package middleware

import (
	"net/http"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
)

// ClientIP resolves the caller's address once, through the trusted-proxy
// policy, and stores it in the request context. Everything downstream —
// access logging, rate limiting, handlers — reads requestctx.ClientIP
// rather than resolving again, so no two consumers can disagree about
// who the client is, and none of them reads a forwarded header itself.
func ClientIP(resolver *clientip.Resolver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := requestctx.WithClientIP(r.Context(), resolver.ClientIP(r))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
