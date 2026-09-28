// Package securitylog is the first consumer of identity's security
// events: it writes them as structured logs for alerting and audit.
// Metrics or a SIEM forwarder would be further application.SecurityEvents
// implementations, fanned out in wiring — never calls added to use cases.
package securitylog

import (
	"context"
	"log/slog"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
)

type Publisher struct {
	logger *slog.Logger
}

var _ application.SecurityEvents = (*Publisher)(nil)

func NewPublisher(logger *slog.Logger) *Publisher {
	return &Publisher{logger: logger.With("component", "identity_security")}
}

// Publish logs at Warn: each event is an authentication failure or a
// throttle, the signal alerting keys on. Never the email or password.
func (p *Publisher) Publish(ctx context.Context, event application.SecurityEvent) {
	attrs := []any{
		"event", string(event.Type),
		"reason", event.Reason,
		"request_id", requestctx.RequestID(ctx),
	}
	if event.UserID != nil {
		attrs = append(attrs, "user_id", event.UserID.String())
	}
	if event.IPAddress != nil {
		attrs = append(attrs, "client_ip", *event.IPAddress)
	}
	if event.RetryAfter > 0 {
		attrs = append(attrs, "retry_after", event.RetryAfter)
	}

	p.logger.WarnContext(ctx, "security event", attrs...)
}
