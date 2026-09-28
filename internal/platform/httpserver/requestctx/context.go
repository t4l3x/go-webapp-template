package requestctx

import (
	"context"
	"net/netip"
)

type requestIDKey struct{}

type clientIPKey struct{}

func WithRequestID(
	ctx context.Context,
	requestID string,
) context.Context {
	return context.WithValue(
		ctx,
		requestIDKey{},
		requestID,
	)
}

func RequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey{}).(string)

	return requestID
}

// WithClientIP stores the request's resolved client address. Only
// middleware.ClientIP should call it, so every reader sees the one
// value resolved through the trusted-proxy policy.
func WithClientIP(ctx context.Context, addr netip.Addr) context.Context {
	return context.WithValue(ctx, clientIPKey{}, addr)
}

// ClientIP returns the address middleware.ClientIP resolved for this
// request, or the zero netip.Addr (check with IsValid) when it could
// not be determined or the middleware did not run.
func ClientIP(ctx context.Context) netip.Addr {
	addr, _ := ctx.Value(clientIPKey{}).(netip.Addr)

	return addr
}
