// Package redis holds identity's Redis-backed adapters: short-lived abuse
// counters, never business data.
package redis

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/platform/observability"
)

// loginFailureKeyPrefix namespaces the counters; the full key is
// "<prefix><hex HMAC of email>:ip:<ip>". The account part is a keyed
// hash, so a Redis dump reveals no address, and it is stable per account
// so a future account-wide rule can use the same subject.
const loginFailureKeyPrefix = "abuse:identity.login.account_ip:"

// reserveScript counts one attempt and starts the fixed window on the
// first. One script so the increment and the expiry are atomic: a crash
// between them would otherwise leave a counter that never expires.
var reserveScript = goredis.NewScript(`
local count = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then
	redis.call('PEXPIRE', KEYS[1], ARGV[1])
	ttl = tonumber(ARGV[1])
end
return {count, ttl}
`)

// ProtectionEvent names the counter and log event for this adapter's
// outages. While it fires, login brute-force protection is off (and,
// since it shares Redis, most likely HTTP rate limiting too); only
// edge/WAF protection remains. Alert on it.
const ProtectionEvent = "auth_abuse_protection_unavailable"

const scope = "identity.login.account_ip"

// LoginFailureCounter implements application.LoginFailureCounter.
//
// It fails open, like the platform rate limiters: an unavailable Redis
// must not block logins. That trade is only acceptable if it is loud, so
// every failure goes through an observability.ProtectionSignal (metric
// per failure; logs on transition, periodic summary and recovery — no
// log storm), never exposing the key (it identifies an account + IP).
type LoginFailureCounter struct {
	client  *goredis.Client
	secret  []byte
	timeout time.Duration
	signal  *observability.ProtectionSignal
}

var _ application.LoginFailureCounter = (*LoginFailureCounter)(nil)

// NewLoginFailureCounter takes the AUTH_ABUSE_KEY_SECRET used to hash
// account identifiers, the time budget for one Redis call on the login
// path (RATELIMIT_TIMEOUT), and the signal that reports outages.
func NewLoginFailureCounter(client *goredis.Client, secret string, timeout time.Duration, signal *observability.ProtectionSignal) *LoginFailureCounter {
	return &LoginFailureCounter{
		client:  client,
		secret:  []byte(secret),
		timeout: timeout,
		signal:  signal,
	}
}

func (c *LoginFailureCounter) Reserve(ctx context.Context, email string, ip *string, window time.Duration) (int, time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	values, err := reserveScript.Run(ctx, c.client, []string{c.key(email, ip)}, window.Milliseconds()).Int64Slice()
	if err == nil && len(values) != 2 {
		err = fmt.Errorf("unexpected reserve reply of %d values", len(values))
	}
	if err != nil {
		c.signal.Failed(ctx, scope, err)

		return 0, 0
	}

	c.signal.Succeeded()

	return int(values[0]), time.Duration(values[1]) * time.Millisecond
}

func (c *LoginFailureCounter) Reset(ctx context.Context, email string, ip *string) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if err := c.client.Del(ctx, c.key(email, ip)).Err(); err != nil {
		c.signal.Failed(ctx, scope, err)

		return
	}

	c.signal.Succeeded()
}

// key never contains the email: only HMAC-SHA256(secret, email). The IP
// stays readable, as in every other limiter key.
func (c *LoginFailureCounter) key(email string, ip *string) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(email))

	return loginFailureKeyPrefix + hex.EncodeToString(mac.Sum(nil)) + ":ip:" + ipSubject(ip)
}

func ipSubject(ip *string) string {
	if ip == nil {
		return "unknown"
	}

	addr, err := netip.ParseAddr(*ip)
	if err != nil {
		return "unknown"
	}

	return addr.Unmap().String()
}
