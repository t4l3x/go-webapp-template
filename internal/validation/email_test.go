package validation_test

import (
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/validation"
)

func TestNormalizeEmail_Canonicalizes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"already_canonical", "user@example.com", "user@example.com"},
		{"surrounding_whitespace", "  user@example.com  ", "user@example.com"},
		{"uppercase", "USER@EXAMPLE.COM", "user@example.com"},
		{"mixed_case_and_whitespace", "  User@Example.COM  ", "user@example.com"},
		{"plus_addressing_preserved", "user+tag@example.com", "user+tag@example.com"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validation.NormalizeEmail(tc.raw)
			if err != nil {
				t.Fatalf("NormalizeEmail(%q) error = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeEmail(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestNormalizeEmail_RejectsDisplayNameForm guards a security-relevant
// property, not just a syntax preference: net/mail.ParseAddress happily
// extracts a mailbox out of a "Display Name <addr>" string, so without
// this check a caller could submit a wrapper whose visible text differs
// from the address actually stored.
func TestNormalizeEmail_RejectsDisplayNameForm(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"quoted_display_name", `"Real Name" <user@example.com>`},
		{"bare_display_name", "Real Name <user@example.com>"},
		{"angle_brackets_only", "<user@example.com>"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validation.NormalizeEmail(tc.raw)
			if err == nil {
				t.Fatalf("NormalizeEmail(%q) = %q, want an error", tc.raw, got)
			}
		})
	}
}

func TestNormalizeEmail_RejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"whitespace_only", "   "},
		{"no_at_sign", "not-an-email"},
		{"missing_local_part", "@example.com"},
		{"missing_domain", "user@"},
		{"two_addresses", "first@example.com, second@example.com"},
		{"trailing_garbage", "user@example.com garbage"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validation.NormalizeEmail(tc.raw)
			if err == nil {
				t.Fatalf("NormalizeEmail(%q) = %q, want an error", tc.raw, got)
			}
		})
	}
}
