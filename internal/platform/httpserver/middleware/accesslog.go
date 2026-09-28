package middleware

import (
	"log/slog"
	"net/http"

	"github.com/felixge/httpsnoop"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
)

func AccessLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			metrics := httpsnoop.CaptureMetrics(
				next,
				w,
				r,
			)

			logger.Info(
				"http request",
				"request_id", requestctx.RequestID(r.Context()),
				"client_ip", clientIPAttr(r),
				"method", r.Method,
				"path", r.URL.Path,
				"status", metrics.Code,
				"bytes", metrics.Written,
				"duration", metrics.Duration,
			)
		})
	}
}

// clientIPAttr renders the address ClientIP resolved, or "" when it is
// unknown, rather than netip.Addr's "invalid IP".
func clientIPAttr(r *http.Request) string {
	addr := requestctx.ClientIP(r.Context())
	if !addr.IsValid() {
		return ""
	}

	return addr.String()
}
