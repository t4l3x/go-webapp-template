package http_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
	identityhttp "github.com/t4l3x/go-webapp-template/internal/modules/identity/transport/http"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
)

type httpVerificationStore struct {
	calls  int
	err    error
	userID uuid.UUID
}

func (s *httpVerificationStore) Consume(context.Context, uuid.UUID, time.Time) error {
	s.calls++
	return s.err
}
func (s *httpVerificationStore) Replace(_ context.Context, v *domain.EmailVerification, _ domain.ResendPolicy) error {
	s.calls++
	s.userID = v.UserID
	return s.err
}

// policyVerificationStore applies the real domain.ResendPolicy to an
// in-memory per-user history, with a mutex standing in for the row lock
// the Postgres store takes (whose concurrency is covered by the
// integration suite).
type policyVerificationStore struct {
	mu     sync.Mutex
	issued map[uuid.UUID][]time.Time // newest first
}

func (s *policyVerificationStore) Consume(context.Context, uuid.UUID, time.Time) error { return nil }

func (s *policyVerificationStore) Replace(_ context.Context, v *domain.EmailVerification, policy domain.ResendPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := policy.Check(s.issued[v.UserID], v.CreatedAt); err != nil {
		return err
	}
	s.issued[v.UserID] = append([]time.Time{v.CreatedAt}, s.issued[v.UserID]...)
	return nil
}

// newResendEndpoint is the resend handler behind real AuthMiddleware,
// authenticating every request as userID.
func newResendEndpoint(t *testing.T, userID uuid.UUID) http.Handler {
	t.Helper()

	store := &policyVerificationStore{issued: map[uuid.UUID][]time.Time{}}
	resend := application.NewResendEmailVerificationService(store, application.ResendVerificationConfig{
		TTL:    time.Hour,
		Policy: domain.ResendPolicy{Cooldown: time.Minute, MaxPerWindow: 5},
	})
	responder := mustResponder(t)
	h := newVerificationHandler(t, nil, resend, responder)

	tokens := newFakeTokenManager()
	tokens.claims = application.AccessTokenClaims{UserID: userID, SessionID: uuid.New()}

	return identityhttp.NewAuthMiddleware(tokens, responder).Authenticate(http.HandlerFunc(h.ResendVerification))
}

func resendFrom(ip string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/resend-verification", nil)
	req.Header.Set("Authorization", "Bearer anything")
	return req.WithContext(requestctx.WithClientIP(req.Context(), netip.MustParseAddr(ip)))
}

// TestHandler_ResendVerification_CooldownSurvivesIPChange: the cooldown is
// keyed by account, so switching address right after a send is refused.
func TestHandler_ResendVerification_CooldownSurvivesIPChange(t *testing.T) {
	endpoint := newResendEndpoint(t, uuid.New())

	first := httptest.NewRecorder()
	endpoint.ServeHTTP(first, resendFrom("198.51.100.7"))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first resend: status = %d, want %d, body = %s", first.Code, http.StatusAccepted, first.Body)
	}

	second := httptest.NewRecorder()
	endpoint.ServeHTTP(second, resendFrom("203.0.113.9"))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("resend from a new IP inside the cooldown: status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if code := decodeErrorCode(t, second.Body.Bytes()); code != "email_verification_resend_limited" {
		t.Fatalf("error.code = %q, want %q", code, "email_verification_resend_limited")
	}
	if got := second.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want %q", got, "60")
	}
}

// TestHandler_ResendVerification_ConcurrentAttemptsCannotBypassCooldown:
// of many simultaneous resends for one account, exactly one is accepted.
func TestHandler_ResendVerification_ConcurrentAttemptsCannotBypassCooldown(t *testing.T) {
	endpoint := newResendEndpoint(t, uuid.New())

	const attempts = 20
	start := make(chan struct{})
	codes := make(chan int, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Go(func() {
			<-start
			rec := httptest.NewRecorder()
			endpoint.ServeHTTP(rec, resendFrom(fmt.Sprintf("198.51.100.%d", i+1)))
			codes <- rec.Code
		})
	}
	close(start)
	wg.Wait()
	close(codes)

	accepted := 0
	for code := range codes {
		switch code {
		case http.StatusAccepted:
			accepted++
		case http.StatusTooManyRequests:
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted = %d concurrent resends, want exactly 1", accepted)
	}
}

// newVerificationHandler builds a Handler wired only with the
// verification use cases; the session use cases are unused here.
func newVerificationHandler(
	t *testing.T,
	verify *application.VerifyEmailService,
	resend *application.ResendEmailVerificationService,
	responder *response.Responder,
) *identityhttp.Handler {
	t.Helper()

	return identityhttp.NewHandler(nil, nil, nil, nil, nil, verify, resend, responder)
}

func TestHandler_VerifyEmail_DecodesAndMapsErrors(t *testing.T) {
	signer := security.NewVerificationSigner(security.VerificationTokenConfig{Secret: strings.Repeat("s", 32)})
	token := signer.SignVerificationToken(uuid.New(), time.Now().Add(time.Hour))
	for _, tc := range []struct {
		name, body, contentType string
		storeErr                error
		wantStatus, wantCalls   int
		code                    string
	}{
		{"valid", `{"token":"` + token + `"}`, "application/json", nil, 204, 1, ""},
		{"invalid", `{"token":"invalid"}`, "application/json", nil, 400, 0, "invalid_email_verification"},
		{"missing", `{}`, "application/json", nil, 400, 0, "invalid_email_verification"},
		{"unknown field", `{"token":"` + token + `","extra":true}`, "application/json", nil, 400, 0, ""},
		{"wrong content type", `{}`, "text/plain", nil, 415, 0, "unsupported_content_type"},
		{"expired row", `{"token":"` + token + `"}`, "application/json", domain.ErrInvalidEmailVerification, 400, 1, "invalid_email_verification"},
		{"DB failure", `{"token":"` + token + `"}`, "application/json", errors.New("private database detail"), 500, 1, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &httpVerificationStore{err: tc.storeErr}
			h := newVerificationHandler(t, application.NewVerifyEmailService(store, signer), nil, mustResponder(t))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-email", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			h.VerifyEmail(rec, req)
			if rec.Code != tc.wantStatus || store.calls != tc.wantCalls {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, store.calls, rec.Body)
			}
			if tc.code != "" && decodeErrorCode(t, rec.Body.Bytes()) != tc.code {
				t.Fatalf("wrong code: %s", rec.Body)
			}
			if strings.Contains(rec.Body.String(), "private database detail") || strings.Contains(rec.Body.String(), token) {
				t.Fatal("response leaked private data")
			}
		})
	}
}

func TestHandler_ResendVerification_RequiresAuthenticationAndUsesPrincipal(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		store := &httpVerificationStore{}
		responder := mustResponder(t)
		h := newVerificationHandler(t, nil, application.NewResendEmailVerificationService(store, application.ResendVerificationConfig{TTL: time.Hour}), responder)
		tokens := newFakeTokenManager()
		id := uuid.New()
		tokens.claims = application.AccessTokenClaims{UserID: id, SessionID: uuid.New()}
		access, _, err := tokens.GenerateAccessToken(id, uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		handler := identityhttp.NewAuthMiddleware(tokens, responder).Authenticate(http.HandlerFunc(h.ResendVerification))
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/resend-verification", nil)
		if authenticated {
			req.Header.Set("Authorization", "Bearer "+access)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if !authenticated {
			if rec.Code != 401 || store.calls != 0 {
				t.Fatal("unauthenticated resend accepted")
			}
		} else if rec.Code != 202 || store.calls != 1 || store.userID != id {
			t.Fatalf("wrong resend identity: status=%d user=%v want=%v", rec.Code, store.userID, id)
		}
	}
}
