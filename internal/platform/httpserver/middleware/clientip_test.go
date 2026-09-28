package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/middleware"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
)

func serveClientIP(t *testing.T, resolver *clientip.Resolver, req *http.Request) netip.Addr {
	t.Helper()

	var got netip.Addr
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = requestctx.ClientIP(r.Context())
	})

	middleware.ClientIP(resolver)(next).ServeHTTP(httptest.NewRecorder(), req)

	return got
}

func TestClientIP_StoresResolvedPeer(t *testing.T) {
	req := newRequest("198.51.100.7:1234", "/")
	req.Header.Set("X-Forwarded-For", "203.0.113.1") // untrusted peer: ignored

	got := serveClientIP(t, clientip.NewResolver(nil), req)

	if want := netip.MustParseAddr("198.51.100.7"); got != want {
		t.Fatalf("ClientIP = %v, want %v", got, want)
	}
}

func TestClientIP_StoresForwardedAddressFromTrustedProxy(t *testing.T) {
	resolver := clientip.NewResolver([]netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")})

	req := newRequest("198.51.100.7:1234", "/")
	req.Header.Set("X-Forwarded-For", "203.0.113.1")

	got := serveClientIP(t, resolver, req)

	if want := netip.MustParseAddr("203.0.113.1"); got != want {
		t.Fatalf("ClientIP = %v, want %v", got, want)
	}
}

func TestClientIP_UnresolvableIsZero(t *testing.T) {
	got := serveClientIP(t, clientip.NewResolver(nil), newRequest("not-an-address", "/"))

	if got.IsValid() {
		t.Fatalf("ClientIP = %v, want the zero address", got)
	}
}

func TestClientIP_AbsentWithoutMiddleware(t *testing.T) {
	if got := requestctx.ClientIP(httptest.NewRequest(http.MethodGet, "/", nil).Context()); got.IsValid() {
		t.Fatalf("ClientIP = %v, want the zero address when the middleware did not run", got)
	}
}
