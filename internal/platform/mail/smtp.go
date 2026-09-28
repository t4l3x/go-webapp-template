package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	gomail "github.com/wneessen/go-mail"
)

// SMTPSender sends mail over SMTP, using github.com/wneessen/go-mail
// for the protocol/MIME implementation. This file is the only place
// that library's types appear — nothing above platform/mail
// (identity, application, domain) imports it or sees it through
// Sender/Message. Swapping to a different SMTP library, or to a
// provider-native adapter (SES, Postmark), means writing a new type
// in this package that also implements Sender; nothing outside this
// package changes.
//
// It works identically against Mailpit locally and a real provider in
// production — only Config changes between them; nothing here is
// Mailpit-specific.
//
// It never retries internally: a failed Send returns an error and
// stops. Retrying a durable delivery is platform/outbox's job, not
// this transport's — retrying twice in two different places would
// make the effective backoff policy impossible to reason about.
type SMTPSender struct {
	cfg      Config
	fromAddr string
}

// NewSMTPSender validates cfg.From once at construction (LoadConfig
// already validates it too; re-checking here means SMTPSender is safe
// to construct directly, e.g. in a test, without going through
// LoadConfig) and stores the bare address the envelope/From header
// needs.
func NewSMTPSender(cfg Config) (*SMTPSender, error) {
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("invalid MAIL_FROM address: %w", err)
	}

	return &SMTPSender{cfg: cfg, fromAddr: from.Address}, nil
}

// Send delivers msg over SMTP. The whole operation — dial, optional
// STARTTLS/implicit TLS, optional AUTH, message transfer — is bounded
// by min(ctx's deadline, now+Config.Timeout): go-mail's
// DialAndSendWithContext takes a context directly and returns once
// that context is done, so no manual conn-deadline plumbing is needed
// here the way a raw net/smtp implementation would require.
//
// Send never logs anything — msg.Body may carry a verification
// secret, and Config.Username/Password must never appear in a log
// line. Errors returned here wrap only SMTP protocol responses and our
// own diagnostic text, never message content or credentials.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}

	// Belt-and-suspenders ahead of go-mail's own RFC 5322 address
	// validation: guarantee no header-injection vector reaches message
	// construction at all, regardless of how the library encodes
	// headers internally.
	if strings.ContainsAny(msg.Subject, "\r\n") || strings.ContainsAny(msg.To, "\r\n") {
		return errors.New("message subject/recipient must not contain control characters")
	}

	m := gomail.NewMsg()

	if err := m.From(s.fromAddr); err != nil {
		return fmt.Errorf("set from address: %w", err)
	}
	if err := m.To(to.Address); err != nil {
		return fmt.Errorf("set recipient address: %w", err)
	}

	m.Subject(msg.Subject)
	m.SetBodyString(gomail.TypeTextPlain, msg.Body)

	client, err := s.newClient()
	if err != nil {
		return fmt.Errorf("create smtp client: %w", err)
	}

	sendCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	if err := client.DialAndSendWithContext(sendCtx, m); err != nil {
		return fmt.Errorf("send mail: %w", classifySendError(err))
	}

	return nil
}

func (s *SMTPSender) newClient() (*gomail.Client, error) {
	opts := []gomail.Option{
		gomail.WithPort(s.cfg.Port),
		gomail.WithTimeout(s.cfg.Timeout),
	}

	// go-mail's own default (no policy option given at all) is
	// TLSMandatory — the opposite of ours. Every branch here must be
	// explicit so a zero-value/unrecognized Config.TLSPolicy (which
	// LoadConfig would normally reject, but a directly-constructed
	// Config — e.g. in a test — might not) safely falls through to our
	// documented default of "none", never to go-mail's stricter one.
	switch s.cfg.TLSPolicy {
	case TLSPolicySTARTTLS:
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	case TLSPolicyImplicit:
		opts = append(opts, gomail.WithSSL())
	case TLSPolicyNone:
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	default:
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	}

	// go-mail's own docs are explicit that servers which don't
	// support/require auth (Mailpit included) should never be passed
	// WithSMTPAuth at all — so this only activates when credentials are
	// actually configured, matching LoadConfig's both-or-neither rule.
	if s.cfg.Username != "" {
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(s.cfg.Username),
			gomail.WithPassword(s.cfg.Password),
		)
	}

	return gomail.NewClient(s.cfg.Host, opts...)
}

// classifySendError wraps a *gomail.SendError with an explicit
// temporary/permanent label (per the library's own IsTemp, which
// reflects the underlying SMTP reply code), without discarding the
// original error — errors.As still finds the *gomail.SendError
// underneath. This is purely for observability (log messages, and any
// future caller that wants to branch on it); platform/outbox's retry
// policy is unchanged either way, and SMTPSender still never retries
// anything itself.
func classifySendError(err error) error {
	var sendErr *gomail.SendError

	if !errors.As(err, &sendErr) {
		return err
	}

	if sendErr.IsTemp() {
		return fmt.Errorf("temporary: %w", err)
	}

	return fmt.Errorf("permanent: %w", err)
}
