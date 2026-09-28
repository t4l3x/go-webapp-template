package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestResponder_JSON(t *testing.T) {
	logger, _ := testkit.NewLogger()
	rec := httptest.NewRecorder()

	response.NewResponder(logger).JSON(
		rec,
		httptest.NewRequest(http.MethodGet, "/", nil),
		http.StatusCreated,
		map[string]string{"id": "123"},
	)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	want := "application/json; charset=utf-8"
	if got := rec.Header().Get("Content-Type"); got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["id"] != "123" {
		t.Fatalf("body[id] = %q, want %q", body["id"], "123")
	}
}

// TestResponder_JSON_EncodingFailure pins that an unencodable value
// becomes a clean 500 with the standard error envelope — not the
// intended status with a truncated body — and is logged.
func TestResponder_JSON_EncodingFailure(t *testing.T) {
	logger, logs := testkit.NewLogger()
	rec := httptest.NewRecorder()

	response.NewResponder(logger).JSON(
		rec,
		httptest.NewRequest(http.MethodGet, "/", nil),
		http.StatusOK,
		map[string]any{"bad": make(chan int)},
	)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body response.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "internal_error" {
		t.Fatalf("error.code = %q, want %q", body.Error.Code, "internal_error")
	}

	if !strings.Contains(logs.String(), `"level":"ERROR"`) || !strings.Contains(logs.String(), "encode response body") {
		t.Fatalf("expected an error log for the encoding failure, got: %s", logs.String())
	}
}

func TestResponder_NoContent(t *testing.T) {
	logger, _ := testkit.NewLogger()
	rec := httptest.NewRecorder()

	response.NewResponder(logger).NoContent(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "" {
		t.Fatalf("Content-Type = %q, want none on a bodiless response", got)
	}
}

func TestResponder_Status(t *testing.T) {
	logger, _ := testkit.NewLogger()
	rec := httptest.NewRecorder()

	response.NewResponder(logger).Status(rec, httptest.NewRequest(http.MethodPost, "/", nil), http.StatusAccepted)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}
