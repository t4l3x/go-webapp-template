package apperror

import "fmt"

type Kind string

const (
	KindValidation           Kind = "validation"
	KindUnauthorized         Kind = "unauthorized"
	KindForbidden            Kind = "forbidden"
	KindNotFound             Kind = "not_found"
	KindConflict             Kind = "conflict"
	KindUnavailable          Kind = "unavailable"
	KindPayloadTooLarge      Kind = "payload_too_large"
	KindUnsupportedMediaType Kind = "unsupported_media_type"

	// KindTooManyRequests means the caller exceeded an allowance and
	// should retry later. It belongs in this transport-independent set
	// rather than only in the HTTP layer because "you are going too
	// fast, try again" is a real application-level outcome with an
	// equivalent everywhere (HTTP 429, gRPC ResourceExhausted) — not an
	// HTTP status in search of a Kind.
	KindTooManyRequests Kind = "too_many_requests"

	KindInternal Kind = "internal"
)

type Error struct {
	Kind    Kind
	Code    string
	Message string
	Cause   error
}

func New(
	kind Kind,
	code string,
	message string,
) *Error {
	return &Error{
		Kind:    kind,
		Code:    code,
		Message: message,
	}
}

func Wrap(
	kind Kind,
	code string,
	message string,
	cause error,
) *Error {
	return &Error{
		Kind:    kind,
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}

	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}
