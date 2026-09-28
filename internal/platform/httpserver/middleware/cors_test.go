package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
)

func TestCORS_NoOriginHeader(t *testing.T) {
	handler := middleware.CORS([]string{"https://example.com"})(okHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	requireHeader(t, rec, "Access-Control-Allow-Origin", "")
}

func TestCORS_AllowsConfiguredOrigin(t *testing.T) {
	handler := middleware.CORS([]string{"https://example.com"})(okHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	requireHeader(t, rec, "Access-Control-Allow-Origin", "https://example.com")
	requireHeader(t, rec, "Vary", "Origin")
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	handler := middleware.CORS([]string{"https://example.com"})(okHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (request still reaches handler)", rec.Code, http.StatusOK)
	}
	requireHeader(t, rec, "Access-Control-Allow-Origin", "")
}

func TestCORS_PreflightAllowedOrigin(t *testing.T) {
	handler := middleware.CORS([]string{"https://example.com"})(okHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatalf("expected Access-Control-Allow-Methods to be set")
	}
}

func TestCORS_PreflightDisallowedOrigin(t *testing.T) {
	handler := middleware.CORS([]string{"https://example.com"})(okHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://evil.example")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (falls through to next handler, not short-circuited)", rec.Code, http.StatusOK)
	}
}

func TestCORS_MultipleConfiguredOrigins(t *testing.T) {
	handler := middleware.CORS([]string{"https://a.example", "https://b.example"})(okHandler())

	for _, origin := range []string{"https://a.example", "https://b.example"} {
		t.Run(origin, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Origin", origin)

			handler.ServeHTTP(rec, req)

			requireHeader(t, rec, "Access-Control-Allow-Origin", origin)
		})
	}

	t.Run("unconfigured origin", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "https://c.example")

		handler.ServeHTTP(rec, req)

		requireHeader(t, rec, "Access-Control-Allow-Origin", "")
	})
}

func TestCORS_Wildcard(t *testing.T) {
	handler := middleware.CORS([]string{"*"})(okHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anything.example")

	handler.ServeHTTP(rec, req)

	requireHeader(t, rec, "Access-Control-Allow-Origin", "*")
}

// okHandler returns a handler that always responds 200 OK, used as the
// terminal handler when only CORS's own behavior is under test.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// requireHeader fails the test unless the recorded response header exactly
// matches want (pass "" to assert the header is absent/empty).
func requireHeader(t *testing.T, rec *httptest.ResponseRecorder, name, want string) {
	t.Helper()

	if got := rec.Header().Get(name); got != want {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}
