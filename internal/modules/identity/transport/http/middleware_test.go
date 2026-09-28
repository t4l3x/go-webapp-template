package http_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	identityhttp "github.com/t4l3x/go-webapp-template/internal/modules/identity/transport/http"
)

var errRejected = errors.New("token rejected")

func TestAuthMiddleware_MissingToken(t *testing.T) {
	mw := identityhttp.NewAuthMiddleware(newFakeTokenManager(), mustResponder(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rec := httptest.NewRecorder()

	mw.Authenticate(unreachableHandler(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_MalformedAuthorizationHeader(t *testing.T) {
	mw := identityhttp.NewAuthMiddleware(newFakeTokenManager(), mustResponder(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "not-a-bearer-token")
	rec := httptest.NewRecorder()

	mw.Authenticate(unreachableHandler(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	tokens := &rejectingTokenManager{}
	mw := identityhttp.NewAuthMiddleware(tokens, mustResponder(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	mw.Authenticate(unreachableHandler(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	tokens := newFakeTokenManager()
	wantPrincipal := identityhttp.Principal{UserID: uuid.New(), SessionID: uuid.New()}
	tokens.claims = application.AccessTokenClaims(wantPrincipal)

	mw := identityhttp.NewAuthMiddleware(tokens, mustResponder(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	var gotPrincipal identityhttp.Principal
	var ok bool

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrincipal, ok = identityhttp.PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw.Authenticate(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !ok {
		t.Fatalf("expected a principal to be present in the request context")
	}
	if gotPrincipal.UserID != wantPrincipal.UserID {
		t.Fatalf("UserID = %v, want %v", gotPrincipal.UserID, wantPrincipal.UserID)
	}
	if gotPrincipal.SessionID != wantPrincipal.SessionID {
		t.Fatalf("SessionID = %v, want %v", gotPrincipal.SessionID, wantPrincipal.SessionID)
	}
}

func unreachableHandler(t *testing.T) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatalf("next handler must not be called when authentication fails")
	})
}

type rejectingTokenManager struct{}

func (rejectingTokenManager) GenerateAccessToken(uuid.UUID, uuid.UUID) (string, time.Time, error) {
	panic("not used in this test")
}

func (rejectingTokenManager) ParseAccessToken(string) (application.AccessTokenClaims, error) {
	return application.AccessTokenClaims{}, errRejected
}

func (rejectingTokenManager) GenerateRefreshToken() (string, error) {
	panic("not used in this test")
}

func (rejectingTokenManager) HashRefreshToken(string) string {
	panic("not used in this test")
}
