package validation_test

import (
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/validation"
)

func TestNormalizeE164Phone_Canonicalizes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"already_canonical", "+14155550100", "+14155550100"},
		{"spaces_and_hyphens", "+1 415-555-0100", "+14155550100"},
		{"parentheses", "+1 (415) 555-0100", "+14155550100"},
		{"surrounding_whitespace", "  +14155550100  ", "+14155550100"},
		{"shortest_valid", "+12", "+12"},
		{"longest_valid", "+123456789012345", "+123456789012345"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validation.NormalizeE164Phone(tc.raw)
			if err != nil {
				t.Fatalf("NormalizeE164Phone(%q) error = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeE164Phone(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeE164Phone_RejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"not_a_number", "not-a-phone"},
		{"missing_plus", "14155550100"},
		{"leading_zero_country_code", "+04155550100"},
		{"too_short", "+1"},
		{"too_long", "+1234567890123456"},
		{"letters_mixed_in", "+1415555010a"},
		{"unstripped_separator", "+1.415.555.0100"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validation.NormalizeE164Phone(tc.raw)
			if err == nil {
				t.Fatalf("NormalizeE164Phone(%q) = %q, want an error", tc.raw, got)
			}
		})
	}
}
