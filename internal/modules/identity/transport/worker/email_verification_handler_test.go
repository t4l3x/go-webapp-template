package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/translations"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/transport/worker"
	"github.com/t4l3x/go-webapp-template/internal/platform/localization"
	"github.com/t4l3x/go-webapp-template/internal/platform/mail"
	"github.com/t4l3x/go-webapp-template/internal/platform/outbox"
)

// These tests run against identity's real embedded catalog rather than
// a stub localizer wherever the wording matters, so they also prove the
// shipped en.toml parses, defines the message IDs this handler asks
// for, and declares exactly the template variables the handler passes.

func TestEmailVerificationHandler_Handle_SendsLocalizedLinkWithSignedToken(t *testing.T) {
	sender := &fakeSender{}
	signer := &fakeSigner{}
	handler := newHandler(
		sender, signer, identityLocalizer(t), application.DeliveryConfig{AppPublicURL: "https://app.example.com"})

	verificationID := uuid.New()
	expiresAt := time.Now().Add(24 * time.Hour)

	event := mustClaimedEvent(t, application.EmailVerificationRequestedV1{
		UserID:         uuid.New(),
		Email:          "user@example.com",
		VerificationID: verificationID,
		ExpiresAt:      expiresAt,
	})

	if err := handler.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if len(sender.sent) != 1 {
		t.Fatalf("sent = %d messages, want 1", len(sender.sent))
	}

	msg := sender.sent[0]
	if msg.To != "user@example.com" {
		t.Fatalf("To = %q, want %q", msg.To, "user@example.com")
	}

	// The subject is the catalog's, not a string literal in Go code.
	if msg.Subject != "Verify your email address" {
		t.Fatalf("Subject = %q, want it rendered from the catalog", msg.Subject)
	}

	// Compute the expected token independently of the fake (rather than
	// calling signer.SignVerificationToken again here), so the call
	// count assertion below reflects only the handler's own call.
	//
	// The link opens the user-facing app's route, not the API endpoint:
	// no /api/v1 prefix belongs in an emailed URL.
	wantToken := "signed-" + verificationID.String() + "-" + strconv.FormatInt(expiresAt.Unix(), 10)
	wantLink := "https://app.example.com/verify-email?token=" + wantToken

	if !strings.Contains(msg.Body, wantLink) {
		t.Fatalf("Body does not contain the expected verification link: got %q, want substring %q", msg.Body, wantLink)
	}
	if !strings.Contains(msg.Body, expiresAt.UTC().Format("2006-01-02 15:04 UTC")) {
		t.Fatalf("Body does not interpolate the expiry: got %q", msg.Body)
	}
	if strings.Contains(msg.Body, "{{") {
		t.Fatalf("Body contains an unrendered template placeholder: %q", msg.Body)
	}
	if len(signer.calls) != 1 {
		t.Fatalf("signer called %d times, want 1", len(signer.calls))
	}
}

// TestEmailVerificationHandler_Handle_RetryPreservesVerificationLink proves the property
// this design exists for: the payload carries no token, only
// (verification_id, expires_at) — signing is deterministic, so a
// retried delivery (e.g. crash after send, before processed_at) always
// links to the exact same credential, never a freshly generated one.
func TestEmailVerificationHandler_Handle_RetryPreservesVerificationLink(t *testing.T) {
	sender := &fakeSender{}
	signer := &fakeSigner{}
	handler := newHandler(
		sender, signer, identityLocalizer(t), application.DeliveryConfig{AppPublicURL: "https://app.example.com"})

	event := mustClaimedEvent(t, application.EmailVerificationRequestedV1{
		UserID:         uuid.New(),
		Email:          "user@example.com",
		VerificationID: uuid.New(),
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	})

	if err := handler.Handle(context.Background(), event); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if err := handler.Handle(context.Background(), event); err != nil {
		t.Fatalf("second Handle() error = %v", err)
	}

	if len(sender.sent) != 2 {
		t.Fatalf("sent = %d messages, want 2", len(sender.sent))
	}
	if sender.sent[0].Body != sender.sent[1].Body {
		t.Fatalf("retried delivery produced a different message body:\nfirst:  %q\nsecond: %q",
			sender.sent[0].Body, sender.sent[1].Body)
	}
}

// TestEmailVerificationHandler_Handle_UnknownLocaleStillSends covers the
// current locale reality: nothing persists a recipient's language, so
// the handler asks for an unspecified locale and must still produce a
// complete email via the default-locale fallback.
func TestEmailVerificationHandler_Handle_UnknownLocaleStillSends(t *testing.T) {
	sender := &fakeSender{}
	handler := newHandler(
		sender, &fakeSigner{}, identityLocalizer(t), application.DeliveryConfig{AppPublicURL: "https://app.example.com"})

	event := mustClaimedEvent(t, application.EmailVerificationRequestedV1{
		UserID:         uuid.New(),
		Email:          "user@example.com",
		VerificationID: uuid.New(),
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	})

	if err := handler.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if sender.sent[0].Subject == "" {
		t.Fatalf("Subject is empty — an unknown locale must fall back, not blank out the email")
	}
	if sender.sent[0].Body == "" {
		t.Fatalf("Body is empty — an unknown locale must fall back, not blank out the email")
	}
}

func TestEmailVerificationHandler_Handle_MalformedPayloadReturnsError(t *testing.T) {
	sender := &fakeSender{}
	handler := newHandler(
		sender, &fakeSigner{}, identityLocalizer(t), application.DeliveryConfig{AppPublicURL: "https://app.example.com"})

	event := outbox.ClaimedEvent{
		ID:      uuid.New(),
		Type:    application.EventTypeEmailVerificationRequestedV1,
		Payload: []byte("not json"),
	}

	if err := handler.Handle(context.Background(), event); err == nil {
		t.Fatalf("Handle() error = nil, want an error for a malformed payload")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent = %d messages, want 0 for a malformed payload", len(sender.sent))
	}
}

// TestEmailVerificationHandler_Handle_MissingTranslationFailsInsteadOfSending
// is the reason Translate returns an error rather than "": a catalog
// missing this message must abort the delivery so outbox retry and
// alerting can see it, not mail a blank body nobody can un-send.
func TestEmailVerificationHandler_Handle_MissingTranslationFailsInsteadOfSending(t *testing.T) {
	sender := &fakeSender{}

	// A catalog that defines the subject but not the body.
	localizer := newLocalizer(t, fstest.MapFS{
		"en.toml": &fstest.MapFile{Data: []byte("[identity.email_verification]\nsubject = \"Verify your email address\"\n")},
	})

	handler := newHandler(
		sender, &fakeSigner{}, localizer, application.DeliveryConfig{AppPublicURL: "https://app.example.com"})

	event := mustClaimedEvent(t, application.EmailVerificationRequestedV1{
		UserID:         uuid.New(),
		Email:          "user@example.com",
		VerificationID: uuid.New(),
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	})

	err := handler.Handle(context.Background(), event)
	if err == nil {
		t.Fatalf("Handle() error = nil, want a failure when a message is missing")
	}
	if !errors.Is(err, localization.ErrMessageNotFound) {
		t.Fatalf("Handle() error = %v, want it to wrap %v", err, localization.ErrMessageNotFound)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent = %d messages, want 0 — a partially rendered email must never go out", len(sender.sent))
	}
}

// TestEmailVerificationHandler_Handle_ErrorDoesNotLeakToken guards a
// security property of the failure path, not the happy path: this
// error travels to outbox.Runner, which logs it at Error level. The
// template data it was rendering contains the verification URL, so an
// error that quoted its inputs would write a live bearer credential
// into the logs.
func TestEmailVerificationHandler_Handle_ErrorDoesNotLeakToken(t *testing.T) {
	localizer := newLocalizer(t, fstest.MapFS{
		"en.toml": &fstest.MapFile{Data: []byte("[identity.email_verification]\nsubject = \"Verify your email address\"\n")},
	})

	verificationID := uuid.New()
	expiresAt := time.Now().Add(24 * time.Hour)

	handler := newHandler(
		&fakeSender{}, &fakeSigner{}, localizer, application.DeliveryConfig{AppPublicURL: "https://app.example.com"})

	err := handler.Handle(context.Background(), mustClaimedEvent(t, application.EmailVerificationRequestedV1{
		UserID:         uuid.New(),
		Email:          "user@example.com",
		VerificationID: verificationID,
		ExpiresAt:      expiresAt,
	}))
	if err == nil {
		t.Fatalf("Handle() error = nil, want a failure")
	}

	token := "signed-" + verificationID.String() + "-" + strconv.FormatInt(expiresAt.Unix(), 10)

	if strings.Contains(err.Error(), token) {
		t.Fatalf("error text contains the verification token: %v", err)
	}
	if strings.Contains(err.Error(), "verify-email") {
		t.Fatalf("error text contains the verification URL: %v", err)
	}
}

func TestEmailVerificationHandler_Handle_MailSendFailurePropagates(t *testing.T) {
	sendErr := errors.New("smtp unavailable")
	sender := &fakeSender{err: sendErr}
	handler := newHandler(
		sender, &fakeSigner{}, identityLocalizer(t), application.DeliveryConfig{AppPublicURL: "https://app.example.com"})

	event := mustClaimedEvent(t, application.EmailVerificationRequestedV1{
		UserID:         uuid.New(),
		Email:          "user@example.com",
		VerificationID: uuid.New(),
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	})

	err := handler.Handle(context.Background(), event)
	if !errors.Is(err, sendErr) {
		t.Fatalf("Handle() error = %v, want it to wrap %v", err, sendErr)
	}
}

// TestEmailVerificationHandler_Registration_BindsVersionedEventType
// pins the exact string this handler claims from the outbox. It is a
// persisted contract, not an internal name: rows written by an earlier
// deploy carry this literal, so changing it would strand them with no
// registered handler. A v2 payload gets its own type and its own
// registration next to this one.
func TestEmailVerificationHandler_Registration_BindsVersionedEventType(t *testing.T) {
	handler := newHandler(
		&fakeSender{}, &fakeSigner{}, identityLocalizer(t), application.DeliveryConfig{})

	registration := handler.Registration()

	if registration.Type != "identity.email_verification_requested.v1" {
		t.Fatalf("Registration().Type = %q, want %q",
			registration.Type, "identity.email_verification_requested.v1")
	}
	if registration.Handler == nil {
		t.Fatalf("Registration().Handler = nil, want the handler's Handle method")
	}
}

// identityLocalizer builds an engine over identity's real embedded
// catalog, so these tests fail if the shipped wording stops providing
// what this handler asks for.
func identityLocalizer(t *testing.T) localization.Localizer {
	t.Helper()

	return newLocalizer(t, translations.Files)
}

func newLocalizer(t *testing.T, files fs.FS) localization.Localizer {
	t.Helper()

	engine, err := localization.NewEngine(
		localization.EngineParams{
			Catalogs: []localization.Catalog{{Owner: "identity", Files: files}},
		},
		localization.Config{DefaultLocale: "en"},
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("build localization engine: %v", err)
	}

	return engine
}

func mustClaimedEvent(t *testing.T, payload application.EmailVerificationRequestedV1) outbox.ClaimedEvent {
	t.Helper()

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	return outbox.ClaimedEvent{
		ID:      uuid.New(),
		Type:    application.EventTypeEmailVerificationRequestedV1,
		Payload: data,
	}
}

type fakeSender struct {
	sent []mail.Message
	err  error
}

func (s *fakeSender) Send(_ context.Context, msg mail.Message) error {
	if s.err != nil {
		return s.err
	}

	s.sent = append(s.sent, msg)

	return nil
}

// fakeSigner is a deterministic application.VerificationSigner stand-in —
// no real HMAC needed to test the handler's own logic (payload
// decoding, link construction, idempotent re-derivation).
type fakeSigner struct {
	calls []uuid.UUID
}

func (s *fakeSigner) SignVerificationToken(verificationID uuid.UUID, expiresAt time.Time) string {
	s.calls = append(s.calls, verificationID)

	return "signed-" + verificationID.String() + "-" + strconv.FormatInt(expiresAt.Unix(), 10)
}

func newHandler(sender mail.Sender, signer application.VerificationSigner, localizer localization.Localizer, cfg application.DeliveryConfig) *worker.EmailVerificationHandler {
	return worker.NewEmailVerificationHandler(application.NewDeliverEmailVerificationService(eligibleStore{}, sender, signer, localizer, cfg))
}

type eligibleStore struct{}

func (eligibleStore) CanDeliver(context.Context, application.EmailVerificationRequestedV1) (bool, error) {
	return true, nil
}

func TestEmailVerificationHandlerInvalidPayloadNeverSignsOrSends(t *testing.T) {
	for _, payload := range []string{`null`, `{}`, `{"email":"user@example.com"}`, `{"user_id":"not-a-uuid"}`, `[]`} {
		t.Run(payload, func(t *testing.T) {
			sender, signer := &fakeSender{}, &fakeSigner{}
			h := newHandler(sender, signer, identityLocalizer(t), application.DeliveryConfig{})
			err := h.Handle(context.Background(), outbox.ClaimedEvent{Payload: []byte(payload)})
			if !errors.Is(err, application.ErrInvalidVerificationDelivery) || len(sender.sent) != 0 || len(signer.calls) != 0 {
				t.Fatalf("invalid payload reached delivery: %v", err)
			}
		})
	}
}

type deliveryState struct {
	eligible bool
	err      error
	calls    int
}

func (s *deliveryState) CanDeliver(context.Context, application.EmailVerificationRequestedV1) (bool, error) {
	s.calls++
	return s.eligible, s.err
}

func TestEmailVerificationHandlerSkipsObsoleteAndRetriesLookupFailure(t *testing.T) {
	for _, tc := range []struct {
		name       string
		expiry     time.Time
		eligible   bool
		lookupErr  error
		wantErr    bool
		wantLookup int
	}{
		{"expired", time.Now().Add(-time.Hour), true, nil, false, 0},
		{"signed expiry elapsed", time.Now().Truncate(time.Second).Add(time.Nanosecond), true, nil, false, 0},
		{"consumed or superseded", time.Now().Add(time.Hour), false, nil, false, 1},
		{"database unavailable", time.Now().Add(time.Hour), false, errors.New("database unavailable"), true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender, signer := &fakeSender{}, &fakeSigner{}
			store := &deliveryState{eligible: tc.eligible, err: tc.lookupErr}
			h := worker.NewEmailVerificationHandler(application.NewDeliverEmailVerificationService(store, sender, signer, identityLocalizer(t), application.DeliveryConfig{}))
			err := h.Handle(context.Background(), mustClaimedEvent(t, application.EmailVerificationRequestedV1{UserID: uuid.New(), VerificationID: uuid.New(), Email: "user@example.com", ExpiresAt: tc.expiry}))
			if (err != nil) != tc.wantErr || store.calls != tc.wantLookup || len(sender.sent) != 0 || len(signer.calls) != 0 {
				t.Fatalf("err=%v lookup=%d sent=%d signed=%d", err, store.calls, len(sender.sent), len(signer.calls))
			}
		})
	}
}
