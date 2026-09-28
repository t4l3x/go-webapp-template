package apperror_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
)

func TestNew(t *testing.T) {
	err := apperror.New(
		apperror.KindNotFound,
		"resource_not_found",
		"Resource not found",
	)

	if err.Kind != apperror.KindNotFound {
		t.Fatalf("Kind = %v, want %v", err.Kind, apperror.KindNotFound)
	}
	if err.Code != "resource_not_found" {
		t.Fatalf("Code = %q, want %q", err.Code, "resource_not_found")
	}
	if err.Message != "Resource not found" {
		t.Fatalf("Message = %q, want %q", err.Message, "Resource not found")
	}
	if err.Cause != nil {
		t.Fatalf("Cause = %v, want nil", err.Cause)
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("connection refused")

	err := apperror.Wrap(
		apperror.KindInternal,
		"resource_lookup_failed",
		"Failed to load resource",
		cause,
	)

	if err.Kind != apperror.KindInternal {
		t.Fatalf("Kind = %v, want %v", err.Kind, apperror.KindInternal)
	}
	if err.Code != "resource_lookup_failed" {
		t.Fatalf("Code = %q, want %q", err.Code, "resource_lookup_failed")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(err, cause) = false, want true")
	}
}

func TestError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *apperror.Error
		want string
	}{
		{
			name: "without cause",
			err: apperror.New(
				apperror.KindValidation,
				"field_required",
				"Name is required",
			),
			want: "Name is required",
		},
		{
			name: "with cause",
			err: apperror.Wrap(
				apperror.KindInternal,
				"resource_lookup_failed",
				"Failed to load resource",
				errors.New("boom"),
			),
			want: "Failed to load resource: boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := errors.New("boom")
		err := apperror.Wrap(
			apperror.KindInternal,
			"resource_lookup_failed",
			"Failed to load resource",
			cause,
		)

		if got := errors.Unwrap(err); !errors.Is(got, cause) {
			t.Fatalf("Unwrap() = %v, want %v", got, cause)
		}
	})

	t.Run("without cause", func(t *testing.T) {
		err := apperror.New(
			apperror.KindValidation,
			"field_required",
			"Name is required",
		)

		if got := errors.Unwrap(err); got != nil {
			t.Fatalf("Unwrap() = %v, want nil", got)
		}
	})
}

func TestError_As(t *testing.T) {
	original := apperror.New(
		apperror.KindConflict,
		"resource_conflict",
		"Resource is in a conflicting state",
	)

	wrapped := fmt.Errorf("resolve action: %w", original)

	var appErr *apperror.Error
	if !errors.As(wrapped, &appErr) {
		t.Fatalf("errors.As() = false, want true")
	}
	if appErr.Code != "resource_conflict" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "resource_conflict")
	}
	if appErr.Kind != apperror.KindConflict {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindConflict)
	}
}
