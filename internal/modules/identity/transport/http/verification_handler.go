package http

import (
	"net/http"

	"github.com/t4l3x/go-webapp-template/internal/api/openapi"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/request"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
)

type VerificationHandler struct {
	verify    *application.VerifyEmailService
	resend    *application.ResendEmailVerificationService
	responder *response.Responder
}

func NewVerificationHandler(
	verify *application.VerifyEmailService,
	resend *application.ResendEmailVerificationService,
	responder *response.Responder,
) *VerificationHandler {
	return &VerificationHandler{verify: verify, resend: resend, responder: responder}
}
func (h *VerificationHandler) Verify(w http.ResponseWriter, r *http.Request) {
	req, err := request.DecodeJSON[openapi.VerifyEmailRequest](w, r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.verify.Verify(r.Context(), req.Token); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *VerificationHandler) Resend(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.responder.Error(w, r, errUnauthorized)
		return
	}
	if err := h.resend.Resend(r.Context(), principal.UserID); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
