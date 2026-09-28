package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
	identityhttp "github.com/t4l3x/go-webapp-template/internal/modules/identity/transport/http"
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
func (s *httpVerificationStore) Replace(_ context.Context, v *domain.EmailVerification) error {
	s.calls++
	s.userID = v.UserID
	return s.err
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
