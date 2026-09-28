package http

import (
	"net/http"
	"strings"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
)

// clientIP is the caller's address as resolved once by the platform
// ClientIP middleware, or nil if it could not be determined. The
// application input keeps *string rather than netip.Addr.
func clientIP(r *http.Request) *string {
	addr := requestctx.ClientIP(r.Context())
	if !addr.IsValid() {
		return nil
	}

	ip := addr.String()

	return &ip
}

func userAgent(r *http.Request) *string {
	agent := strings.TrimSpace(r.UserAgent())
	if agent == "" {
		return nil
	}

	return &agent
}
