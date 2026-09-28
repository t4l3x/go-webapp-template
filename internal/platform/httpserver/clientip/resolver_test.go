package clientip_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
)

func TestResolver_ClientIP_DirectClient(t *testing.T) {
	resolver := clientip.NewResolver(nil)

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "203.0.113.10:54321"

	got := resolver.ClientIP(req)

	requireAddr(t, got, "203.0.113.10")
}

func TestResolver_ClientIP_SpoofedHeaderFromUntrustedPeerIgnored(t *testing.T) {
	resolver := clientip.NewResolver(nil)

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := resolver.ClientIP(req)

	requireAddr(t, got, "203.0.113.10")
}

func TestResolver_ClientIP_TrustedSingleProxy(t *testing.T) {
	resolver := clientip.NewResolver(prefixes(t, "203.0.113.0/24"))

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	got := resolver.ClientIP(req)

	requireAddr(t, got, "198.51.100.7")
}

func TestResolver_ClientIP_TrustedProxyChain(t *testing.T) {
	resolver := clientip.NewResolver(prefixes(t, "203.0.113.0/24", "10.0.0.0/8"))

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	// Real client, then two trusted internal hops, closest hop last.
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.2, 10.0.0.1")

	got := resolver.ClientIP(req)

	requireAddr(t, got, "198.51.100.7")
}

func TestResolver_ClientIP_UntrustedHopInChainWins(t *testing.T) {
	resolver := clientip.NewResolver(prefixes(t, "203.0.113.0/24", "10.0.0.0/8"))

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	// 198.51.100.99 is not trusted, so it (not the leftmost entry) is
	// the real client — everything to its left is unverifiable.
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.99, 10.0.0.1")

	got := resolver.ClientIP(req)

	requireAddr(t, got, "198.51.100.99")
}

func TestResolver_ClientIP_MalformedForwardedForFallsBackToRemoteAddr(t *testing.T) {
	resolver := clientip.NewResolver(prefixes(t, "203.0.113.0/24"))

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	req.Header.Set("X-Forwarded-For", "not-an-ip, 10.0.0.1")

	got := resolver.ClientIP(req)

	requireAddr(t, got, "203.0.113.10")
}

func TestResolver_ClientIP_MalformedRemoteAddrIsInvalid(t *testing.T) {
	resolver := clientip.NewResolver(nil)

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "not-an-address"

	got := resolver.ClientIP(req)

	if got.IsValid() {
		t.Fatalf("ClientIP() = %v, want invalid for a malformed RemoteAddr", got)
	}
}

func TestResolver_ClientIP_IPv4(t *testing.T) {
	resolver := clientip.NewResolver(nil)

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "192.0.2.1:8080"

	got := resolver.ClientIP(req)

	requireAddr(t, got, "192.0.2.1")
}

func TestResolver_ClientIP_IPv6(t *testing.T) {
	resolver := clientip.NewResolver(nil)

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "[2001:db8::1]:8080"

	got := resolver.ClientIP(req)

	requireAddr(t, got, "2001:db8::1")
}

func TestResolver_ClientIP_TrustedProxyIPv6ForwardedFor(t *testing.T) {
	resolver := clientip.NewResolver(prefixes(t, "2001:db8::/32"))

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "[2001:db8::1]:8080"
	req.Header.Set("X-Forwarded-For", "2001:db8:1::1")

	got := resolver.ClientIP(req)

	requireAddr(t, got, "2001:db8:1::1")
}

func TestResolver_ClientIP_TrustedProxyXRealIP(t *testing.T) {
	resolver := clientip.NewResolver(prefixes(t, "203.0.113.0/24"))

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	req.Header.Set("X-Real-IP", "198.51.100.7")

	got := resolver.ClientIP(req)

	requireAddr(t, got, "198.51.100.7")
}

func TestResolver_ClientIP_MalformedXRealIPFallsBack(t *testing.T) {
	resolver := clientip.NewResolver(prefixes(t, "203.0.113.0/24"))

	req := httptest.NewRequest(http.MethodGet, "/example", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	req.Header.Set("X-Real-IP", "not-an-ip")

	got := resolver.ClientIP(req)

	requireAddr(t, got, "203.0.113.10")
}

func requireAddr(t *testing.T, got netip.Addr, want string) {
	t.Helper()

	if !got.IsValid() {
		t.Fatalf("ClientIP() is invalid, want %q", want)
	}
	if got.String() != want {
		t.Fatalf("ClientIP() = %q, want %q", got.String(), want)
	}
}

func prefixes(t *testing.T, cidrs ...string) []netip.Prefix {
	t.Helper()

	out := make([]netip.Prefix, 0, len(cidrs))
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			t.Fatalf("ParsePrefix(%q) error = %v", cidr, err)
		}
		out = append(out, prefix)
	}

	return out
}
