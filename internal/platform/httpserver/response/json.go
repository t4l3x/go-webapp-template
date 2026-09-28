package response

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
)

const contentTypeJSON = "application/json; charset=utf-8"

// internalErrorBody is what a client receives when a response value
// cannot be encoded. It is pre-rendered so that reporting an encoding
// failure can never itself fail to encode.
var internalErrorBody = []byte(`{"error":{"code":"internal_error","message":"Internal server error"}}` + "\n")

// JSON writes value as a JSON response with the given status.
//
// The value is encoded before anything is written, so an encoding
// failure is still reported as a clean 500 with the standard error
// envelope instead of a truncated body under the intended status.
func (r *Responder) JSON(
	w http.ResponseWriter,
	req *http.Request,
	status int,
	value any,
) {
	body, err := json.Marshal(value)
	if err != nil {
		r.logger.Error(
			"encode response body",
			"request_id", requestctx.RequestID(req.Context()),
			"method", req.Method,
			"path", req.URL.Path,
			"status", status,
			"error", fmt.Errorf("encode %T: %w", value, err),
		)

		status = http.StatusInternalServerError
		body = internalErrorBody
	} else {
		body = append(body, '\n')
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)

	// A write error means the client is gone; the status is already
	// sent and nothing can be done for this request. Debug, because a
	// disconnecting client is routine rather than a server fault.
	if _, err := w.Write(body); err != nil {
		r.logger.Debug(
			"write response body",
			"request_id", requestctx.RequestID(req.Context()),
			"method", req.Method,
			"path", req.URL.Path,
			"error", err,
		)
	}
}

// NoContent writes a bodiless 204 No Content.
func (r *Responder) NoContent(w http.ResponseWriter, req *http.Request) {
	r.Status(w, req, http.StatusNoContent)
}

// Status writes a bodiless response with the given status, for
// successful outcomes that carry no payload (e.g. 202 Accepted).
func (r *Responder) Status(w http.ResponseWriter, _ *http.Request, status int) {
	w.WriteHeader(status)
}
