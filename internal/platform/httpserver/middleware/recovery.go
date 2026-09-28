package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
)

func Recovery(responder *response.Responder) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}

				cause := fmt.Errorf(
					"panic: %v\n%s",
					recovered,
					debug.Stack(),
				)

				responder.Error(
					w,
					r,
					apperror.Wrap(
						apperror.KindInternal,
						"panic_recovered",
						"Internal server error",
						cause,
					),
				)
			}()

			next.ServeHTTP(w, r)
		})
	}
}
