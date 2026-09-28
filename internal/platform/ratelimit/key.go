package ratelimit

import "net/netip"

// dimensionIP names the IP dimension: an IP is an infrastructure
// concept, not a domain one, so it belongs here.
const dimensionIP = "ip"

// Key identifies one rate-limited bucket. Its fields are unexported on
// purpose: callers build keys through the constructors below, so this
// package stays the only place that decides how a bucket is named. A
// handler cannot accidentally put a raw email address into Redis by
// assembling a key string itself.
type Key struct {
	scope     string
	dimension string
	subject   string
}

// NewIPKey limits a scope per client IP.
//
// The address must come from platform/httpserver/clientip, which
// applies the trusted-proxy policy. An IP taken straight from
// X-Forwarded-For would let any caller pick its own bucket and bypass
// the limit entirely.
//
// IPs are stored in the clear: they are not credentials, they are
// already in access logs, and keeping them readable makes an abusive
// source directly greppable during an incident. Hashing them would buy
// nothing and cost that.
func NewIPKey(scope string, ip netip.Addr) Key {
	subject := "unknown"
	if ip.IsValid() {
		subject = ip.Unmap().String()
	}

	return Key{scope: scope, dimension: dimensionIP, subject: subject}
}

// Scope reports the operation this key limits ("identity.login.ip"). It
// is safe to log and low-cardinality, unlike the subject — which has
// no accessor, because nothing outside this package should be putting
// a per-client identifier into a log line or a metric label.
func (k Key) Scope() string {
	return k.scope
}

// String renders the storage key. The backend adds its own namespace
// prefix on top, so the full Redis key is "rate:<scope>:<dimension>:<subject>",
// e.g. "rate:identity.login.ip:ip:198.51.100.7". Exactly how that string
// is namespaced is an implementation detail of this package and its
// adapter; nothing outside depends on it.
func (k Key) String() string {
	return k.scope + ":" + k.dimension + ":" + k.subject
}
