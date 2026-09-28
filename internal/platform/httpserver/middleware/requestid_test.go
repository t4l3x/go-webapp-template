package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
)

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	var gotFromContext string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFromContext = requestctx.RequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.RequestID(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(rec, req)

	headerID := rec.Header().Get(middleware.RequestIDHeader)

	if headerID == "" {
		t.Fatalf("expected a generated request id, got empty header")
	}
	if gotFromContext != headerID {
		t.Fatalf("context id = %q, want %q (header value)", gotFromContext, headerID)
	}
}

func TestRequestID_PropagatesIncomingID(t *testing.T) {
	const incoming = "client-supplied-id-123"

	var gotFromContext string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFromContext = requestctx.RequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.RequestID(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(middleware.RequestIDHeader, incoming)

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(middleware.RequestIDHeader); got != incoming {
		t.Fatalf("response header id = %q, want %q", got, incoming)
	}
	if gotFromContext != incoming {
		t.Fatalf("context id = %q, want %q", gotFromContext, incoming)
	}
}

func TestRequestID_ReplacesInvalidIncoming(t *testing.T) {
	tests := []struct {
		name     string
		incoming string
	}{
		{"oversized", strings.Repeat("a", 129)},
		{"blank", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			handler := middleware.RequestID(next)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(middleware.RequestIDHeader, tt.incoming)

			handler.ServeHTTP(rec, req)

			got := rec.Header().Get(middleware.RequestIDHeader)
			if got == tt.incoming {
				t.Fatalf("expected invalid incoming id to be replaced, got it echoed back")
			}
			if got == "" {
				t.Fatalf("expected a replacement id, got empty")
			}
		})
	}
}
