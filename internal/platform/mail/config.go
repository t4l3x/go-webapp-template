package mail

import (
	"fmt"
	"net/mail"
	"time"

	"github.com/caarlos0/env/v11"
)

// TLSPolicy is this platform's own, provider-neutral description of
// how a connection should be secured — deliberately not the SMTP
// library's own TLSPolicy type, so a future library swap can't leak
// into Config's shape.
type TLSPolicy string

const (
	// TLSPolicyNone sends in the clear. Matches Mailpit, which does not
	// speak TLS at all.
	TLSPolicyNone TLSPolicy = "none"
	// TLSPolicySTARTTLS upgrades the connection via STARTTLS after
	// connecting in the clear (the common case on port 587), and fails
	// the send rather than silently falling back to plaintext if the
	// server doesn't support it.
	TLSPolicySTARTTLS TLSPolicy = "starttls"
	// TLSPolicyImplicit connects already wrapped in TLS from the first
	// byte (the common case on port 465) — no STARTTLS negotiation.
	TLSPolicyImplicit TLSPolicy = "implicit"
)

type Config struct {
	Host string `env:"MAIL_HOST" envDefault:"localhost"`
	Port int    `env:"MAIL_PORT" envDefault:"1025"`

	// Username/Password authenticate to the SMTP server via PLAIN auth.
	// Both empty (the default, matching Mailpit, which accepts
	// unauthenticated connections) means no AUTH is attempted.
	Username string `env:"MAIL_USERNAME"`
	Password string `env:"MAIL_PASSWORD"`

	// From is used both as the envelope sender and the message's From
	// header. Validated as a real address so a typo fails fast at
	// startup rather than as a per-send SMTP protocol error.
	From string `env:"MAIL_FROM" envDefault:"no-reply@go-webapp-template.local"`

	// TLSPolicy defaults to "none" for local development against
	// Mailpit, which does not speak TLS. A real provider typically
	// requires "starttls" (port 587) or "implicit" (port 465).
	TLSPolicy TLSPolicy `env:"MAIL_TLS_POLICY" envDefault:"none"`

	// Timeout bounds one whole Send call (connect through QUIT). It
	// must stay comfortably below platform/outbox's OUTBOX_CLAIM_LEASE
	// — see that package's Config for the exact invariant — since this
	// handler's send time is the dominant cost the lease has to cover.
	// SMTPSender never retries internally; a slow/unreachable server
	// just means Send returns an error once Timeout elapses, and
	// outbox's own retry policy decides what happens next.
	Timeout time.Duration `env:"MAIL_TIMEOUT" envDefault:"10s"`
}

func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("load mail config: %w", err)
	}

	if cfg.Host == "" {
		return Config{}, fmt.Errorf("MAIL_HOST must not be empty")
	}

	if cfg.Port < 1 || cfg.Port > 65535 {
		return Config{}, fmt.Errorf("MAIL_PORT must be between 1 and 65535")
	}

	if (cfg.Username == "") != (cfg.Password == "") {
		return Config{}, fmt.Errorf("MAIL_USERNAME and MAIL_PASSWORD must both be set or both be empty")
	}

	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return Config{}, fmt.Errorf("MAIL_FROM must be a valid email address: %w", err)
	}

	switch cfg.TLSPolicy {
	case TLSPolicyNone, TLSPolicySTARTTLS, TLSPolicyImplicit:
	default:
		return Config{}, fmt.Errorf(
			"MAIL_TLS_POLICY must be one of %q, %q, %q, got %q",
			TLSPolicyNone, TLSPolicySTARTTLS, TLSPolicyImplicit, cfg.TLSPolicy,
		)
	}

	if cfg.Timeout <= 0 {
		return Config{}, fmt.Errorf("MAIL_TIMEOUT must be greater than zero")
	}

	return cfg, nil
}
