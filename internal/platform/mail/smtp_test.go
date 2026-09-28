package mail_test

import (
	"context"
	"errors"
	"io"
	"mime/quotedprintable"
	"strings"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/platform/mail"
)

// decodeQuotedPrintableBody decodes the body portion (after the first
// blank line) of a raw RFC 5322 message that used
// Content-Transfer-Encoding: quoted-printable.
func decodeQuotedPrintableBody(t *testing.T, rawMessage string) string {
	t.Helper()

	_, body, found := strings.Cut(rawMessage, "\r\n\r\n")
	if !found {
		t.Fatalf("message has no header/body separator: %q", rawMessage)
	}

	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("decode quoted-printable body: %v", err)
	}

	return string(decoded)
}

func testConfig(t *testing.T, server *fakeSMTPServer) mail.Config {
	t.Helper()

	host, port := server.addr()

	return mail.Config{
		Host:      host,
		Port:      port,
		From:      "no-reply@go-webapp-template.local",
		TLSPolicy: mail.TLSPolicyNone,
		Timeout:   5 * time.Second,
	}
}

// TestSMTPSender_Send_ZeroValueTLSPolicyDefaultsToNone guards against a
// real bug this test caught during development: go-mail's own default
// (when no TLS policy option is passed at all) is TLSMandatory — the
// opposite of this package's documented "none" default. A Config
// constructed directly (bypassing LoadConfig, which would otherwise
// reject an empty/unrecognized TLSPolicy) must still behave as
// documented rather than silently inheriting the library's stricter
// default.
func TestSMTPSender_Send_ZeroValueTLSPolicyDefaultsToNone(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := testConfig(t, server)
	cfg.TLSPolicy = "" // zero value, as if never set

	sender, err := mail.NewSMTPSender(cfg)
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	if err := sender.Send(context.Background(), mail.Message{To: "user@example.com", Subject: "Hi"}); err != nil {
		t.Fatalf("Send() error = %v, want the zero-value TLSPolicy to behave like %q against a non-TLS server", err, mail.TLSPolicyNone)
	}
}

func TestSMTPSender_Send_Success(t *testing.T) {
	server := newFakeSMTPServer(t)
	sender, err := mail.NewSMTPSender(testConfig(t, server))
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	err = sender.Send(context.Background(), mail.Message{
		To:      "user@example.com",
		Subject: "Verify your email address",
		Body:    "click here: https://example.com/verify?token=abc123",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	messages := server.messages()
	if len(messages) != 1 {
		t.Fatalf("server received %d messages, want 1", len(messages))
	}

	msg := messages[0]
	if !strings.Contains(msg.from, "no-reply@go-webapp-template.local") {
		t.Fatalf("MAIL FROM = %q, want it to contain the configured From address", msg.from)
	}
	if !strings.Contains(msg.to, "user@example.com") {
		t.Fatalf("RCPT TO = %q, want it to contain the recipient", msg.to)
	}
	if !strings.Contains(msg.data, "Subject: Verify your email address") {
		t.Fatalf("message data missing Subject header: %q", msg.data)
	}
	// go-mail quoted-printable-encodes text/plain bodies by default (a
	// normal, RFC 2045-compliant choice any real mail client decodes
	// transparently), so "=" in the URL becomes "=3D" on the wire —
	// decode before asserting on the body content.
	if !strings.Contains(decodeQuotedPrintableBody(t, msg.data), "https://example.com/verify?token=abc123") {
		t.Fatalf("decoded message body missing the verification link: %q", msg.data)
	}
}

func TestSMTPSender_Send_AuthenticatesWithConfiguredCredentials(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := testConfig(t, server)
	cfg.Username = "smtp-user"
	cfg.Password = "smtp-pass"

	sender, err := mail.NewSMTPSender(cfg)
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	if err := sender.Send(context.Background(), mail.Message{To: "user@example.com", Subject: "Hi"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	attempts := server.authAttempts
	if len(attempts) != 1 {
		t.Fatalf("auth attempts = %d, want 1", len(attempts))
	}
	if !strings.Contains(attempts[0], "smtp-user") || !strings.Contains(attempts[0], "smtp-pass") {
		t.Fatalf("auth payload = %q, want it to contain the configured credentials", attempts[0])
	}
}

func TestSMTPSender_Send_RejectedRecipientReturnsError(t *testing.T) {
	server := newFakeSMTPServer(t)
	server.rejectRcpt = true

	sender, err := mail.NewSMTPSender(testConfig(t, server))
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	err = sender.Send(context.Background(), mail.Message{To: "user@example.com", Subject: "Hi"})
	if err == nil {
		t.Fatalf("Send() error = nil, want error for a rejected recipient")
	}
	if !strings.Contains(err.Error(), "permanent") {
		t.Fatalf("Send() error = %v, want it classified as permanent (550 is a 5xx code)", err)
	}
}

func TestSMTPSender_Send_RejectedDataReturnsError(t *testing.T) {
	server := newFakeSMTPServer(t)
	server.rejectData = true

	sender, err := mail.NewSMTPSender(testConfig(t, server))
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	err = sender.Send(context.Background(), mail.Message{To: "user@example.com", Subject: "Hi"})
	if err == nil {
		t.Fatalf("Send() error = nil, want error for a rejected DATA transaction")
	}
	if !strings.Contains(err.Error(), "permanent") {
		t.Fatalf("Send() error = %v, want it classified as permanent (554 is a 5xx code)", err)
	}
}

func TestSMTPSender_Send_InvalidRecipientRejectedBeforeDialing(t *testing.T) {
	server := newFakeSMTPServer(t)
	sender, err := mail.NewSMTPSender(testConfig(t, server))
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	err = sender.Send(context.Background(), mail.Message{To: "not-an-email", Subject: "Hi"})
	if err == nil {
		t.Fatalf("Send() error = nil, want error for an invalid recipient address")
	}

	if len(server.messages()) != 0 {
		t.Fatalf("server received a message despite an invalid recipient")
	}
}

func TestSMTPSender_Send_RejectsHeaderInjectionAttempt(t *testing.T) {
	server := newFakeSMTPServer(t)
	sender, err := mail.NewSMTPSender(testConfig(t, server))
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	err = sender.Send(context.Background(), mail.Message{
		To:      "user@example.com",
		Subject: "Hi\r\nBcc: attacker@evil.example",
	})
	if err == nil {
		t.Fatalf("Send() error = nil, want error for a subject containing CRLF")
	}
	if len(server.messages()) != 0 {
		t.Fatalf("server received a message despite a header-injection attempt")
	}
}

func TestSMTPSender_Send_NewSMTPSenderRejectsInvalidFrom(t *testing.T) {
	if _, err := mail.NewSMTPSender(mail.Config{Host: "localhost", Port: 1025, From: "not-an-email"}); err == nil {
		t.Fatalf("NewSMTPSender() error = nil, want error for an invalid From address")
	}
}

func TestSMTPSender_Send_TimesOutOnSlowServer(t *testing.T) {
	server := newFakeSMTPServer(t)
	server.delayMail = 200 * time.Millisecond

	cfg := testConfig(t, server)
	cfg.Timeout = 20 * time.Millisecond

	sender, err := mail.NewSMTPSender(cfg)
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	start := time.Now()

	err = sender.Send(context.Background(), mail.Message{To: "user@example.com", Subject: "Hi"})
	if err == nil {
		t.Fatalf("Send() error = nil, want a timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Send() took %s, want it to respect the configured timeout rather than hang", elapsed)
	}
}

func TestSMTPSender_Send_RespectsContextCancellation(t *testing.T) {
	server := newFakeSMTPServer(t)
	sender, err := mail.NewSMTPSender(testConfig(t, server))
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = sender.Send(ctx, mail.Message{To: "user@example.com", Subject: "Hi"})
	if err == nil {
		t.Fatalf("Send() error = nil, want error for an already-cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want it to wrap context.Canceled", err)
	}
}

func TestSMTPSender_Send_ErrorNeverContainsBodyOrCredentials(t *testing.T) {
	server := newFakeSMTPServer(t)
	server.rejectData = true

	cfg := testConfig(t, server)
	cfg.Username = "smtp-user"
	cfg.Password = "super-secret-password"

	sender, err := mail.NewSMTPSender(cfg)
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	const secretBody = "verification token: extremely-secret-token-value"

	err = sender.Send(context.Background(), mail.Message{
		To:      "user@example.com",
		Subject: "Verify your email address",
		Body:    secretBody,
	})
	if err == nil {
		t.Fatalf("Send() error = nil, want error (server rejects DATA)")
	}

	if strings.Contains(err.Error(), secretBody) {
		t.Fatalf("error leaked the message body: %v", err)
	}
	if strings.Contains(err.Error(), cfg.Password) {
		t.Fatalf("error leaked the configured password: %v", err)
	}
}
