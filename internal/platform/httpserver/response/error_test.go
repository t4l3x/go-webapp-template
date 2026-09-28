package response_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestResponder_Error_StatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		kind       apperror.Kind
		wantStatus int
	}{
		{"validation", apperror.KindValidation, http.StatusBadRequest},
		{"unauthorized", apperror.KindUnauthorized, http.StatusUnauthorized},
		{"forbidden", apperror.KindForbidden, http.StatusForbidden},
		{"not_found", apperror.KindNotFound, http.StatusNotFound},
		{"conflict", apperror.KindConflict, http.StatusConflict},
		{"unavailable", apperror.KindUnavailable, http.StatusServiceUnavailable},
		{"payload_too_large", apperror.KindPayloadTooLarge, http.StatusRequestEntityTooLarge},
		{"unsupported_media_type", apperror.KindUnsupportedMediaType, http.StatusUnsupportedMediaType},
		{"internal", apperror.KindInternal, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responder, _ := testResponder(t)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/resource/1", nil)

			responder.Error(
				rec,
				req,
				apperror.New(tt.kind, "some_code", "Some message"),
			)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestResponder_Error_PublicBody(t *testing.T) {
	responder, _ := testResponder(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource/1", nil)

	responder.Error(
		rec,
		req,
		apperror.New(apperror.KindNotFound, "resource_not_found", "Resource not found"),
	)

	body := decodeErrorResponse(t, rec)

	if body.Error.Code != "resource_not_found" {
		t.Fatalf("code = %q, want %q", body.Error.Code, "resource_not_found")
	}
	if body.Error.Message != "Resource not found" {
		t.Fatalf("message = %q, want %q", body.Error.Message, "Resource not found")
	}
}

func TestResponder_Error_InternalErrorHidesCause(t *testing.T) {
	responder, _ := testResponder(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource/1", nil)

	cause := errors.New("connection refused")
	responder.Error(
		rec,
		req,
		apperror.Wrap(
			apperror.KindInternal,
			"resource_lookup_failed",
			"Failed to load resource",
			cause,
		),
	)

	body := decodeErrorResponse(t, rec)

	if body.Error.Code != "internal_error" {
		t.Fatalf("code = %q, want %q", body.Error.Code, "internal_error")
	}
	if body.Error.Message != "Internal server error" {
		t.Fatalf("message = %q, want %q", body.Error.Message, "Internal server error")
	}
	if strings.Contains(rec.Body.String(), "connection refused") {
		t.Fatalf("response body leaked internal cause: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "resource_lookup_failed") {
		t.Fatalf("response body leaked internal code: %s", rec.Body.String())
	}
}

func TestResponder_Error_InternalErrorIsLogged(t *testing.T) {
	responder, logs := testResponder(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource/1", nil)

	cause := errors.New("connection refused")
	responder.Error(
		rec,
		req,
		apperror.Wrap(
			apperror.KindInternal,
			"resource_lookup_failed",
			"Failed to load resource",
			cause,
		),
	)

	if !strings.Contains(logs.String(), "connection refused") {
		t.Fatalf("expected internal cause to be logged, got: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "resource_lookup_failed") {
		t.Fatalf("expected internal code to be logged, got: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("expected internal error to log at ERROR level, got: %s", logs.String())
	}
}

func TestResponder_Error_UnavailableLogsAtWarn(t *testing.T) {
	responder, logs := testResponder(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/resource", nil)

	cause := errors.New("dependency not seeded")
	responder.Error(
		rec,
		req,
		apperror.Wrap(
			apperror.KindUnavailable,
			"dependency_unavailable",
			"Required dependency is not seeded",
			cause,
		),
	)

	if !strings.Contains(logs.String(), "dependency not seeded") {
		t.Fatalf("expected unavailable cause to be logged, got: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("expected unavailable error to log at WARN level, got: %s", logs.String())
	}
}

func TestResponder_Error_ClientErrorsAreNotLogged(t *testing.T) {
	responder, logs := testResponder(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource/1", nil)

	responder.Error(
		rec,
		req,
		apperror.New(apperror.KindNotFound, "resource_not_found", "Resource not found"),
	)

	if logs.Len() != 0 {
		t.Fatalf("expected no log output for a client error, got: %s", logs.String())
	}
}

func TestResponder_Error_NonAppError(t *testing.T) {
	responder, logs := testResponder(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource/1", nil)

	responder.Error(rec, req, errors.New("unexpected"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	body := decodeErrorResponse(t, rec)
	if body.Error.Code != "internal_error" {
		t.Fatalf("code = %q, want %q", body.Error.Code, "internal_error")
	}

	if !strings.Contains(logs.String(), "unhandled request error") {
		t.Fatalf("expected an unhandled request error log, got: %s", logs.String())
	}
}

// testResponder builds a Responder backed by a buffered logger, so tests
// can assert on both the HTTP output and what got logged.
func testResponder(t *testing.T) (*response.Responder, *bytes.Buffer) {
	t.Helper()

	logger, logs := testkit.NewLogger()

	return response.NewResponder(logger), logs
}

// decodeErrorResponse decodes a recorded response body as the shared
// error envelope, failing the test if it isn't valid JSON.
func decodeErrorResponse(t *testing.T, rec *httptest.ResponseRecorder) response.ErrorResponse {
	t.Helper()

	var body response.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	return body
}
