package middleware

import (
	"net/http"
	"strings"
)

func CORS(allowedOrigins []string) Middleware {
	allowed := make(map[string]struct{}, len(allowedOrigins))

	allowAll := false

	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)

		if origin == "" {
			continue
		}

		if origin == "*" {
			allowAll = true
			continue
		}

		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			origin := r.Header.Get("Origin")

			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			_, originAllowed := allowed[origin]

			if !allowAll && !originAllowed {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Add("Vary", "Origin")

			if allowAll {
				w.Header().Set(
					"Access-Control-Allow-Origin",
					"*",
				)
			} else {
				w.Header().Set(
					"Access-Control-Allow-Origin",
					origin,
				)
			}

			w.Header().Set(
				"Access-Control-Allow-Methods",
				"GET, POST, PUT, PATCH, DELETE, OPTIONS",
			)

			w.Header().Set(
				"Access-Control-Allow-Headers",
				"Authorization, Content-Type, X-Request-ID",
			)

			w.Header().Set(
				"Access-Control-Expose-Headers",
				RequestIDHeader,
			)

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
