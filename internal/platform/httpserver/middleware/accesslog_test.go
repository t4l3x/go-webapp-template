package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestAccessLog_RecordsRequestFields(t *testing.T) {
	logger, logs := testkit.NewLogger()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	handler := middleware.Chain(
		next,
		middleware.RequestID,
		middleware.ClientIP(clientip.NewResolver(nil)),
		middleware.AccessLog(logger),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/resource?token=secret", nil)
	req.RemoteAddr = "198.51.100.7:1234"

	handler.ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode log line: %v (raw: %s)", err, logs.String())
	}

	if entry["method"] != http.MethodPost {
		t.Fatalf("method = %v, want %v", entry["method"], http.MethodPost)
	}
	if entry["path"] != "/resource" {
		t.Fatalf("path = %v, want %v", entry["path"], "/resource")
	}
	if status, ok := entry["status"].(float64); !ok || status != http.StatusCreated {
		t.Fatalf("status = %v, want %v", entry["status"], http.StatusCreated)
	}
	if bytesWritten, ok := entry["bytes"].(float64); !ok || bytesWritten <= 0 {
		t.Fatalf("bytes = %v, want > 0", entry["bytes"])
	}
	if _, ok := entry["duration"]; !ok {
		t.Fatalf("expected a duration field to be present")
	}
	if id, ok := entry["request_id"].(string); !ok || id == "" {
		t.Fatalf("expected a non-empty request_id field, got %v", entry["request_id"])
	}
	if entry["client_ip"] != "198.51.100.7" {
		t.Fatalf("client_ip = %v, want %q (the address ClientIP resolved)", entry["client_ip"], "198.51.100.7")
	}
}

func TestAccessLog_DoesNotLogQueryString(t *testing.T) {
	logger, logs := testkit.NewLogger()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.AccessLog(logger)(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource?token=super-secret", nil)

	handler.ServeHTTP(rec, req)

	if strings.Contains(logs.String(), "super-secret") {
		t.Fatalf("expected query string not to be logged, got: %s", logs.String())
	}
}
