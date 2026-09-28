package ratelimit_test

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/ratelimit"
)

func TestNewIPKey_IsDeterministicAndNamespaced(t *testing.T) {
	ip := netip.MustParseAddr("198.51.100.7")

	first := ratelimit.NewIPKey("identity.login.ip", ip)
	second := ratelimit.NewIPKey("identity.login.ip", ip)

	if first.String() != second.String() {
		t.Fatalf("same scope and IP produced different keys: %q vs %q", first.String(), second.String())
	}
	if first.String() != "identity.login.ip:ip:198.51.100.7" {
		t.Fatalf("key = %q, want %q", first.String(), "identity.login.ip:ip:198.51.100.7")
	}
	if first.Scope() != "identity.login.ip" {
		t.Fatalf("Scope() = %q, want %q", first.Scope(), "identity.login.ip")
	}
}

func TestNewIPKey_DifferentSubjectsGetDifferentBuckets(t *testing.T) {
	one := ratelimit.NewIPKey("http.global", netip.MustParseAddr("198.51.100.7"))
	two := ratelimit.NewIPKey("http.global", netip.MustParseAddr("198.51.100.8"))

	if one.String() == two.String() {
		t.Fatalf("different IPs share a bucket: %q", one.String())
	}
}

func TestNewIPKey_ScopesAreIsolated(t *testing.T) {
	ip := netip.MustParseAddr("198.51.100.7")

	login := ratelimit.NewIPKey("identity.login.ip", ip)
	register := ratelimit.NewIPKey("identity.register", ip)

	if login.String() == register.String() {
		t.Fatalf("different scopes share a bucket: %q", login.String())
	}
}

// TestNewIPKey_UnknownAddress covers the case where the resolver could
// not determine an address at all. Those requests must still share a
// bucket rather than each getting a fresh one, or an unresolvable peer
// would be unlimited.
func TestNewIPKey_UnknownAddress(t *testing.T) {
	first := ratelimit.NewIPKey("http.global", netip.Addr{})
	second := ratelimit.NewIPKey("http.global", netip.Addr{})

	if first.String() != second.String() {
		t.Fatalf("unresolved addresses produced different keys: %q vs %q", first.String(), second.String())
	}
	if !strings.HasSuffix(first.String(), ":unknown") {
		t.Fatalf("key = %q, want it to end in a shared placeholder subject", first.String())
	}
}
