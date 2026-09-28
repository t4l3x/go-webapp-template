package http

import (
	"net/http"
	"strings"

	"github.com/t4l3x/go-webapp-template/internal/api/openapi"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/request"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
)

type Handler struct {
	register           *application.RegisterService
	login              *application.LoginService
	refresh            *application.RefreshService
	logout             *application.LogoutService
	getMe              *application.GetMeService
	verifyEmail        *application.VerifyEmailService
	resendVerification *application.ResendEmailVerificationService
	responder          *response.Responder
}

func NewHandler(
	register *application.RegisterService,
	login *application.LoginService,
	refresh *application.RefreshService,
	logout *application.LogoutService,
	getMe *application.GetMeService,
	verifyEmail *application.VerifyEmailService,
	resendVerification *application.ResendEmailVerificationService,
	responder *response.Responder,
) *Handler {
	return &Handler{
		register:           register,
		login:              login,
		refresh:            refresh,
		logout:             logout,
		getMe:              getMe,
		verifyEmail:        verifyEmail,
		resendVerification: resendVerification,
		responder:          responder,
	}
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	req, err := request.DecodeJSON[openapi.RegisterRequest](w, r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	out, err := h.register.Register(r.Context(), application.RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		Phone:    req.Phone,
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	h.responder.JSON(w, r, http.StatusCreated, newRegisterResponse(out))
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	req, err := request.DecodeJSON[openapi.LoginRequest](w, r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	out, err := h.login.Login(r.Context(), application.LoginInput{
		Email:     req.Email,
		Password:  req.Password,
		UserAgent: userAgent(r),
		IPAddress: clientIP(r),
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	h.responder.JSON(w, r, http.StatusOK, newTokenResponse(out))
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	req, err := request.DecodeJSON[openapi.RefreshRequest](w, r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	out, err := h.refresh.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	h.responder.JSON(w, r, http.StatusOK, newRefreshResponse(out))
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.responder.Error(w, r, errUnauthorized)
		return
	}

	if err := h.logout.Logout(r.Context(), principal.SessionID); err != nil {
		h.responder.Error(w, r, err)
		return
	}

	h.responder.NoContent(w, r)
}

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.responder.Error(w, r, errUnauthorized)
		return
	}

	user, err := h.getMe.GetMe(r.Context(), principal.UserID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	h.responder.JSON(w, r, http.StatusOK, newUserResponse(user))
}

func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	req, err := request.DecodeJSON[openapi.VerifyEmailRequest](w, r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	if err := h.verifyEmail.Verify(r.Context(), req.Token); err != nil {
		h.responder.Error(w, r, err)
		return
	}

	h.responder.NoContent(w, r)
}

func (h *Handler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.responder.Error(w, r, errUnauthorized)
		return
	}

	if err := h.resendVerification.Resend(r.Context(), principal.UserID); err != nil {
		h.responder.Error(w, r, err)
		return
	}

	h.responder.Status(w, r, http.StatusAccepted)
}

// clientIP is the caller's address as resolved once by the platform
// ClientIP middleware, or nil if it could not be determined. The
// application input keeps *string rather than netip.Addr.
func clientIP(r *http.Request) *string {
	addr := requestctx.ClientIP(r.Context())
	if !addr.IsValid() {
		return nil
	}

	ip := addr.String()

	return &ip
}

func userAgent(r *http.Request) *string {
	agent := strings.TrimSpace(r.UserAgent())
	if agent == "" {
		return nil
	}

	return &agent
}
