package http

import (
	"net/http"

	"github.com/t4l3x/go-webapp-template/internal/api/openapi"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/clientip"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/request"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
)

type Handler struct {
	register  *application.RegisterService
	login     *application.LoginService
	refresh   *application.RefreshService
	logout    *application.LogoutService
	getMe     *application.GetMeService
	clientIP  *clientip.Resolver
	responder *response.Responder
}

func NewHandler(
	register *application.RegisterService,
	login *application.LoginService,
	refresh *application.RefreshService,
	logout *application.LogoutService,
	getMe *application.GetMeService,
	clientIP *clientip.Resolver,
	responder *response.Responder,
) *Handler {
	return &Handler{
		register:  register,
		login:     login,
		refresh:   refresh,
		logout:    logout,
		getMe:     getMe,
		clientIP:  clientIP,
		responder: responder,
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

	response.JSON(w, http.StatusCreated, newRegisterResponse(out))
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
		IPAddress: h.remoteIP(r),
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, newTokenResponse(out))
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

	response.JSON(w, http.StatusOK, newRefreshResponse(out))
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

	w.WriteHeader(http.StatusNoContent)
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

	response.JSON(w, http.StatusOK, newUserResponse(user))
}

// remoteIP resolves the caller's IP address as a string, or nil if it
// can't be determined. It is a thin adapter over clientip.Resolver so
// application inputs keep using *string rather than netip.Addr.
func (h *Handler) remoteIP(r *http.Request) *string {
	addr := h.clientIP.ClientIP(r)
	if !addr.IsValid() {
		return nil
	}

	ip := addr.String()

	return &ip
}
