package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
)

var errUnauthorized = apperror.New(apperror.KindUnauthorized, "unauthorized", "Unauthorized")

type principalContextKey struct{}

// Principal is the minimal authenticated identity extracted from a
// valid access token and stored in the request context.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)

	return principal, ok
}

// AuthMiddleware authenticates requests using the identity module's
// access tokens. It is identity-specific and intentionally kept out of
// the generic platform middleware.
type AuthMiddleware struct {
	tokens    application.TokenManager
	responder *response.Responder
}

func NewAuthMiddleware(tokens application.TokenManager, responder *response.Responder) *AuthMiddleware {
	return &AuthMiddleware{
		tokens:    tokens,
		responder: responder,
	}
}

func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			m.responder.Error(w, r, errUnauthorized)
			return
		}

		claims, err := m.tokens.ParseAccessToken(token)
		if err != nil {
			m.responder.Error(w, r, errUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), principalContextKey{}, Principal{
			UserID:    claims.UserID,
			SessionID: claims.SessionID,
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "

	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}

	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", false
	}

	return token, true
}

func userAgent(r *http.Request) *string {
	agent := strings.TrimSpace(r.UserAgent())
	if agent == "" {
		return nil
	}

	return &agent
}
