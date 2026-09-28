package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
)

const RequestIDHeader = "X-Request-ID"

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		requestID := strings.TrimSpace(
			r.Header.Get(RequestIDHeader),
		)

		if requestID == "" || len(requestID) > 128 {
			requestID = newRequestID()
		}

		ctx := requestctx.WithRequestID(
			r.Context(),
			requestID,
		)

		w.Header().Set(RequestIDHeader, requestID)

		next.ServeHTTP(
			w,
			r.WithContext(ctx),
		)
	})
}

func newRequestID() string {
	var value [16]byte

	if _, err := rand.Read(value[:]); err != nil {
		return "unknown"
	}

	return hex.EncodeToString(value[:])
}
