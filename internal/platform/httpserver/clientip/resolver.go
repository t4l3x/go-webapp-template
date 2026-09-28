// Package clientip resolves the real client IP address of an HTTP
// request, honoring forwarded-address headers only when the immediate
// network peer is an explicitly configured trusted proxy. This matters
// beyond any single feature: the resolved address is a candidate input
// for rate limiting, authentication auditing, and abuse detection, so
// it must never be spoofable by an untrusted client.
package clientip

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Resolver derives a request's client IP address, trusting forwarded
// headers only from configured trusted proxies.
type Resolver struct {
	trustedProxies []netip.Prefix
}

// NewResolver builds a Resolver. An empty trustedProxies means no peer
// is ever trusted, so forwarded headers are always ignored and the
// direct TCP peer address (RemoteAddr) is used — the safe default.
func NewResolver(trustedProxies []netip.Prefix) *Resolver {
	return &Resolver{trustedProxies: trustedProxies}
}

// ClientIP returns the resolved client address, or the zero netip.Addr
// (check with !addr.IsValid()) if it cannot be determined at all (e.g.
// RemoteAddr itself is malformed, which practically never happens for
// a real net/http connection).
func (res *Resolver) ClientIP(r *http.Request) netip.Addr {
	remote := parseHostAddr(r.RemoteAddr)
	if !remote.IsValid() {
		return netip.Addr{}
	}

	if !res.isTrusted(remote) {
		return remote
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, ok := res.resolveForwardedFor(xff); ok {
			return ip
		}
		// Malformed forwarded-for content from a trusted proxy is
		// still not trustworthy input — fall back to the verified
		// direct peer rather than guessing.
		return remote
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if ip, err := netip.ParseAddr(xri); err == nil {
			return ip.Unmap()
		}
		return remote
	}

	return remote
}

// resolveForwardedFor walks a validated X-Forwarded-For chain from the
// right (the hop closest to us, which we've already confirmed is
// trusted) toward the left, treating each further hop as trusted only
// while it is itself in the trusted proxy set. The first untrusted (or
// absent) hop encountered is the real client. Any unparseable entry
// invalidates the whole header — never let malformed input resolve to
// a "trusted" client IP.
func (res *Resolver) resolveForwardedFor(header string) (netip.Addr, bool) {
	parts := strings.Split(header, ",")
	ips := make([]netip.Addr, 0, len(parts))

	for _, part := range parts {
		addr, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			return netip.Addr{}, false
		}
		ips = append(ips, addr.Unmap())
	}

	client := ips[0]

	for i := len(ips) - 1; i >= 0; i-- {
		if !res.isTrusted(ips[i]) {
			return ips[i], true
		}
		client = ips[i]
	}

	// The entire chain was made of trusted proxies; best effort is the
	// leftmost (oldest) entry.
	return client, true
}

func (res *Resolver) isTrusted(addr netip.Addr) bool {
	for _, prefix := range res.trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}

func parseHostAddr(remoteAddr string) netip.Addr {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}
	}

	return addr.Unmap()
}
