package request_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/request"
)

type payload struct {
	Email string `json:"email"`
}

func TestDecodeJSON_ValidBody(t *testing.T) {
	got, err := decode(t, `{"email":"a@b.com"}`, "application/json")
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
	if got.Email != "a@b.com" {
		t.Fatalf("Email = %q, want %q", got.Email, "a@b.com")
	}
}

func TestDecodeJSON_ApplicationJSONWithUTF8Charset(t *testing.T) {
	_, err := decode(t, `{"email":"a@b.com"}`, "application/json; charset=utf-8")
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
}

func TestDecodeJSON_EmptyBody(t *testing.T) {
	_, err := decode(t, ``, "application/json")
	requireCode(t, err, "invalid_request_body")
}

func TestDecodeJSON_MalformedBody(t *testing.T) {
	_, err := decode(t, `{not json`, "application/json")
	requireCode(t, err, "invalid_request_body")
}

func TestDecodeJSON_UnknownFieldsRejected(t *testing.T) {
	_, err := decode(t, `{"email":"a@b.com","admin":true}`, "application/json")
	requireCode(t, err, "invalid_request_body")
}

func TestDecodeJSON_SecondJSONObjectRejected(t *testing.T) {
	_, err := decode(t, `{"email":"a@b.com"}{"email":"c@d.com"}`, "application/json")
	requireCode(t, err, "invalid_request_body")
}

func TestDecodeJSON_TrailingGarbageRejected(t *testing.T) {
	_, err := decode(t, `{"email":"a@b.com"} garbage`, "application/json")
	requireCode(t, err, "invalid_request_body")
}

func TestDecodeJSON_TrailingWhitespaceAllowed(t *testing.T) {
	_, err := decode(t, "{\"email\":\"a@b.com\"}\n", "application/json")
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v, want nil for trailing whitespace", err)
	}
}

func TestDecodeJSON_MissingContentTypeRejected(t *testing.T) {
	_, err := decode(t, `{"email":"a@b.com"}`, "")
	requireCode(t, err, "missing_content_type")
}

func TestDecodeJSON_WrongContentTypeRejected(t *testing.T) {
	_, err := decode(t, `{"email":"a@b.com"}`, "text/plain")
	requireCode(t, err, "unsupported_content_type")
}

func TestDecodeJSON_UnsupportedCharsetRejected(t *testing.T) {
	_, err := decode(t, `{"email":"a@b.com"}`, "application/json; charset=iso-8859-1")
	requireCode(t, err, "unsupported_content_type")
}

func TestDecodeJSON_OversizedBodyRejected(t *testing.T) {
	huge := `{"email":"` + strings.Repeat("a", request.MaxJSONBodyBytes) + `"}`

	req := httptest.NewRequest(http.MethodPost, "/example", strings.NewReader(huge))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	_, err := request.DecodeJSON[payload](rec, req)

	requireCode(t, err, "request_body_too_large")

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindPayloadTooLarge {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindPayloadTooLarge)
	}
}

func decode(t *testing.T, body string, contentType string) (payload, error) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/example", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()

	return request.DecodeJSON[payload](rec, req)
}

func requireCode(t *testing.T, err error, wantCode string) {
	t.Helper()

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T (%v)", err, err)
	}
	if appErr.Code != wantCode {
		t.Fatalf("Code = %q, want %q", appErr.Code, wantCode)
	}
}
