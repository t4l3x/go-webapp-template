package http

import (
	"time"

	"github.com/t4l3x/go-webapp-template/internal/api/openapi"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
)

// The wire request/response shapes are the generated OpenAPI types
// (docs/api/openapi.yaml is the source of truth for them — see
// internal/api/openapi). Only the mapping to/from application
// inputs/outputs lives here, so generated types never leak past this
// transport package.
//
// The spec deliberately avoids `format: email`: oapi-codegen maps that
// to a type whose own JSON unmarshaling runs mail.ParseAddress and
// silently keeps just the extracted mailbox address, discarding a
// "Display Name <addr>" wrapper before application-layer validation
// ever sees the raw input. Email fields here are plain strings so the
// application layer's own validation (validation.NormalizeEmail) is the
// sole authority on what counts as a valid address.

func newUserResponse(user application.UserView) openapi.User {
	return openapi.User{
		Id:            user.ID,
		Email:         user.Email,
		Phone:         user.Phone,
		Status:        openapi.UserStatus(user.Status),
		EmailVerified: user.EmailVerifiedAt != nil,
		PhoneVerified: user.PhoneVerifiedAt != nil,
		CreatedAt:     user.CreatedAt,
	}
}

func newRegisterResponse(out application.RegisterOutput) openapi.RegisterResponse {
	return openapi.RegisterResponse{
		User: newUserResponse(out.User),
	}
}

func newTokenResponse(out application.LoginOutput) openapi.TokenResponse {
	return openapi.TokenResponse{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn(out.AccessTokenExpiresAt),
		User:         newUserResponse(out.User),
	}
}

func newRefreshResponse(out application.RefreshOutput) openapi.RefreshResponse {
	return openapi.RefreshResponse{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn(out.AccessTokenExpiresAt),
	}
}

// expiresIn renders an absolute expiry time as the seconds-from-now
// value the OpenAPI contract's expires_in field expects. This is an
// HTTP/API representation concern, not an application/domain one —
// application code deals in absolute times, not response encodings.
func expiresIn(expiresAt time.Time) int64 {
	seconds := int64(time.Until(expiresAt).Seconds())
	if seconds < 0 {
		return 0
	}

	return seconds
}
