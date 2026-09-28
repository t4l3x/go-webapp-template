// Package request holds production-safe HTTP request-parsing helpers
// shared across feature modules' transport layers. It stays narrowly
// scoped to request decoding — it is not a general utility package.
package request

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
)

// MaxJSONBodyBytes bounds ordinary JSON request bodies. It is not meant
// to accommodate file/media uploads — those get their own dedicated
// upload/object-storage flow with its own, much larger, limit.
const MaxJSONBodyBytes = 1 << 20 // 1 MiB

var (
	errMissingContentType     = apperror.New(apperror.KindUnsupportedMediaType, "missing_content_type", "Content-Type header is required")
	errUnsupportedContentType = apperror.New(apperror.KindUnsupportedMediaType, "unsupported_content_type", "Content-Type must be application/json")
	errInvalidBody            = apperror.New(apperror.KindValidation, "invalid_request_body", "Invalid request body")
	errBodyTooLarge           = apperror.New(apperror.KindPayloadTooLarge, "request_body_too_large", "Request body is too large")
)

// DecodeJSON decodes r's body as JSON into a value of type T.
//
// It requires a JSON Content-Type, caps the body size, rejects unknown
// fields, and rejects a body that contains anything beyond exactly one
// JSON value (an empty body, a second JSON object, or trailing garbage
// all fail). Parser/decoder errors are never returned to the caller
// directly — only stable apperror values are.
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var value T

	if err := requireJSONContentType(r); err != nil {
		return value, err
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&value); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return value, errBodyTooLarge
		}

		return value, errInvalidBody
	}

	// A second Decode call must hit exactly EOF. Anything else means
	// the body held more than one JSON value (a second object) or
	// trailing non-whitespace data after the first one.
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return value, errInvalidBody
	}

	return value, nil
}

func requireJSONContentType(r *http.Request) error {
	header := r.Header.Get("Content-Type")
	if header == "" {
		return errMissingContentType
	}

	mediaType, params, err := mime.ParseMediaType(header)
	if err != nil || mediaType != "application/json" {
		return errUnsupportedContentType
	}

	if charset, ok := params["charset"]; ok && !strings.EqualFold(charset, "utf-8") {
		return errUnsupportedContentType
	}

	return nil
}
