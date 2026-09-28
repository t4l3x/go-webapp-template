// Package worker adapts durable outbox messages to identity use cases.
package worker

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/platform/outbox"
)

type EmailVerificationHandler struct {
	delivery *application.DeliverEmailVerificationService
}

func NewEmailVerificationHandler(delivery *application.DeliverEmailVerificationService) *EmailVerificationHandler {
	return &EmailVerificationHandler{delivery: delivery}
}

func (h *EmailVerificationHandler) Handle(ctx context.Context, event outbox.ClaimedEvent) error {
	var payload application.EmailVerificationRequestedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		// Do not include payload contents in persisted errors or logs.
		return outbox.Permanent(application.ErrInvalidVerificationDelivery)
	}
	err := h.delivery.Deliver(ctx, payload)
	if errors.Is(err, application.ErrInvalidVerificationDelivery) {
		return outbox.Permanent(err)
	}
	return err
}

func (h *EmailVerificationHandler) Registration() outbox.HandlerRegistration {
	return outbox.HandlerRegistration{Type: application.EventTypeEmailVerificationRequestedV1, Handler: h.Handle}
}
