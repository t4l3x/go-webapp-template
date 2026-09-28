package response

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
)

type Responder struct {
	logger *slog.Logger
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewResponder(logger *slog.Logger) *Responder {
	return &Responder{
		logger: logger.With(
			"component", "http_response",
		),
	}
}

func (r *Responder) Error(
	w http.ResponseWriter,
	req *http.Request,
	err error,
) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "Internal server error"

	var appErr *apperror.Error

	if errors.As(err, &appErr) {
		status = statusFromKind(appErr.Kind)

		if appErr.Kind != apperror.KindInternal {
			code = appErr.Code
			message = appErr.Message
		}
	}

	r.logError(req, err, appErr)

	r.JSON(
		w,
		req,
		status,
		ErrorResponse{
			Error: ErrorBody{
				Code:    code,
				Message: message,
			},
		},
	)
}

func (r *Responder) logError(
	req *http.Request,
	err error,
	appErr *apperror.Error,
) {
	if appErr == nil {
		r.logger.Error(
			"unhandled request error",
			"request_id", requestctx.RequestID(req.Context()),
			"method", req.Method,
			"path", req.URL.Path,
			"error", err,
		)

		return
	}

	switch appErr.Kind {
	case apperror.KindInternal:
		r.logger.Error(
			"internal request error",
			"request_id", requestctx.RequestID(req.Context()),
			"method", req.Method,
			"path", req.URL.Path,
			"code", appErr.Code,
			"error", errorCause(appErr),
		)

	case apperror.KindUnavailable:
		r.logger.Warn(
			"service unavailable",
			"request_id", requestctx.RequestID(req.Context()),
			"method", req.Method,
			"path", req.URL.Path,
			"code", appErr.Code,
			"error", errorCause(appErr),
		)
	}
}

func errorCause(err *apperror.Error) error {
	if err.Cause != nil {
		return err.Cause
	}

	return err
}

func statusFromKind(kind apperror.Kind) int {
	switch kind {
	case apperror.KindValidation:
		return http.StatusBadRequest

	case apperror.KindUnauthorized:
		return http.StatusUnauthorized

	case apperror.KindForbidden:
		return http.StatusForbidden

	case apperror.KindNotFound:
		return http.StatusNotFound

	case apperror.KindConflict:
		return http.StatusConflict

	case apperror.KindUnavailable:
		return http.StatusServiceUnavailable

	case apperror.KindPayloadTooLarge:
		return http.StatusRequestEntityTooLarge

	case apperror.KindUnsupportedMediaType:
		return http.StatusUnsupportedMediaType

	case apperror.KindTooManyRequests:
		return http.StatusTooManyRequests

	default:
		return http.StatusInternalServerError
	}
}
