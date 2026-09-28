package middleware_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestRecovery_PassesThroughNormally(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	handler := middleware.Recovery(responder)(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}

func TestRecovery_PanicReturnsInternalError(t *testing.T) {
	logger, logs := testkit.NewLogger()
	responder := response.NewResponder(logger)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something exploded: secret-token-abc")
	})

	handler := middleware.Recovery(responder)(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body response.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "internal_error" {
		t.Fatalf("code = %q, want %q", body.Error.Code, "internal_error")
	}
	if body.Error.Message != "Internal server error" {
		t.Fatalf("message = %q, want %q", body.Error.Message, "Internal server error")
	}
	if strings.Contains(rec.Body.String(), "secret-token-abc") {
		t.Fatalf("response body leaked panic value: %s", rec.Body.String())
	}

	if !strings.Contains(logs.String(), "secret-token-abc") {
		t.Fatalf("expected panic value to be logged, got: %s", logs.String())
	}
}

func TestRecovery_ErrAbortHandlerIsRepanicked(t *testing.T) {
	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	})

	handler := middleware.Recovery(responder)(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	defer func() {
		recovered := recover()
		err, ok := recovered.(error)
		if !ok || !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("recovered = %v, want %v", recovered, http.ErrAbortHandler)
		}
	}()

	handler.ServeHTTP(rec, req)

	t.Fatalf("expected http.ErrAbortHandler to propagate, but ServeHTTP returned normally")
}
