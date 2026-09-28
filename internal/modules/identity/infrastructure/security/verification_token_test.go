package security_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
)

const testVerificationSecret = "test-email-verification-secret"

func newVerificationSigner(secret string) *security.VerificationSigner {
	return security.NewVerificationSigner(security.VerificationTokenConfig{Secret: secret})
}

// TestVerificationSigner_SignVerificationToken_MatchesFormat pins the
// exact token for fixed inputs, computed independently of the signer.
// Tokens are mailed and clicked days later, so the format is a contract
// with links already sitting in inboxes: any change to the purpose
// string, the canonical HMAC input, or the encoding must fail here
// rather than silently invalidate them.
func TestVerificationSigner_SignVerificationToken_MatchesFormat(t *testing.T) {
	verificationID := uuid.MustParse("0b6f2c1e-8a4d-4b7e-9c3a-5d2e1f0a9b8c")
	expiresAt := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)

	mac := hmac.New(sha256.New, []byte(testVerificationSecret))
	mac.Write([]byte("email-verification:v1|0b6f2c1e-8a4d-4b7e-9c3a-5d2e1f0a9b8c|1789387200"))
	want := "0b6f2c1e-8a4d-4b7e-9c3a-5d2e1f0a9b8c.1789387200." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	got := newVerificationSigner(testVerificationSecret).SignVerificationToken(verificationID, expiresAt)

	if got != want {
		t.Fatalf("SignVerificationToken() = %q, want %q", got, want)
	}
}

func TestVerificationSigner_SignVerificationToken_DeterministicForSameInputs(t *testing.T) {
	signer := newVerificationSigner(testVerificationSecret)

	verificationID := uuid.New()
	expiresAt := time.Now().Add(24 * time.Hour)

	first := signer.SignVerificationToken(verificationID, expiresAt)
	second := signer.SignVerificationToken(verificationID, expiresAt)

	if first != second {
		t.Fatalf("SignVerificationToken() is not deterministic: %q != %q — a retried delivery must link to the same credential", first, second)
	}
}

func TestVerificationSigner_SignVerificationToken_DifferentInputsProduceDifferentTokens(t *testing.T) {
	signer := newVerificationSigner(testVerificationSecret)

	expiresAt := time.Now().Add(24 * time.Hour)

	tokenA := signer.SignVerificationToken(uuid.New(), expiresAt)
	tokenB := signer.SignVerificationToken(uuid.New(), expiresAt)

	if tokenA == tokenB {
		t.Fatalf("SignVerificationToken() produced identical tokens for different verification ids")
	}
}

func TestVerificationSigner_VerifyVerificationToken_RoundTrip(t *testing.T) {
	signer := newVerificationSigner(testVerificationSecret)

	verificationID := uuid.New()
	expiresAt := time.Now().Add(24 * time.Hour)

	token := signer.SignVerificationToken(verificationID, expiresAt)

	gotID, gotExpiresAt, err := signer.VerifyVerificationToken(token)
	if err != nil {
		t.Fatalf("VerifyVerificationToken() error = %v", err)
	}
	if gotID != verificationID {
		t.Fatalf("VerifyVerificationToken() id = %v, want %v", gotID, verificationID)
	}

	// The token encodes expiresAt as whole seconds, so compare at that
	// resolution rather than requiring exact sub-second equality.
	if gotExpiresAt.Unix() != expiresAt.Unix() {
		t.Fatalf("VerifyVerificationToken() expiresAt = %v, want %v", gotExpiresAt, expiresAt)
	}
}

func TestVerificationSigner_VerifyVerificationToken_RejectsTamperedSignature(t *testing.T) {
	signer := newVerificationSigner(testVerificationSecret)

	token := signer.SignVerificationToken(uuid.New(), time.Now().Add(24*time.Hour))

	// Flip the signature segment's first character rather than its
	// last: base64 encoding a 32-byte (SHA-256) digest leaves a couple
	// of padding bits in the final character that the decoder ignores,
	// so some alternate last characters decode to the exact same
	// bytes — flipping the last character is not a reliable tamper.
	// The first character of a base64 quantum carries a full 6 bits,
	// so changing it always changes the decoded bytes.
	sigStart := strings.LastIndex(token, ".") + 1
	replacement := byte('x')
	if token[sigStart] == replacement {
		replacement = 'y'
	}
	tampered := token[:sigStart] + string(replacement) + token[sigStart+1:]

	if tampered == token {
		t.Fatalf("test setup did not actually alter the token")
	}

	if _, _, err := signer.VerifyVerificationToken(tampered); err == nil {
		t.Fatalf("VerifyVerificationToken() error = nil, want error for a tampered signature")
	}
}

func TestVerificationSigner_VerifyVerificationToken_RejectsForgedIDWithoutSecret(t *testing.T) {
	signer := newVerificationSigner(testVerificationSecret)

	legitimate := signer.SignVerificationToken(uuid.New(), time.Now().Add(24*time.Hour))
	parts := strings.SplitN(legitimate, ".", 3)

	// An attacker who somehow learns a legitimate id/expiry pair (e.g.
	// from a full email_verifications table dump, which stores neither
	// a token nor a hash of one) still cannot forge a token for a
	// *different* id without the HMAC secret: reusing the genuine
	// signature with a substituted id must fail verification.
	forged := uuid.New().String() + "." + parts[1] + "." + parts[2]

	if _, _, err := signer.VerifyVerificationToken(forged); err == nil {
		t.Fatalf("VerifyVerificationToken() error = nil, want error for a forged verification id")
	}
}

func TestVerificationSigner_VerifyVerificationToken_RejectsWrongSecret(t *testing.T) {
	signedBy := newVerificationSigner("secret-a")
	verifiedBy := newVerificationSigner("secret-b")

	token := signedBy.SignVerificationToken(uuid.New(), time.Now().Add(24*time.Hour))

	if _, _, err := verifiedBy.VerifyVerificationToken(token); err == nil {
		t.Fatalf("VerifyVerificationToken() error = nil, want error for a mismatched secret")
	}
}

func TestVerificationSigner_VerifyVerificationToken_RejectsMalformedToken(t *testing.T) {
	signer := newVerificationSigner(testVerificationSecret)

	tests := []string{
		"",
		"not-a-token",
		"only.two-parts",
		uuid.New().String() + ".not-a-unix-timestamp." + "sig",
	}

	for _, tc := range tests {
		if _, _, err := signer.VerifyVerificationToken(tc); err == nil {
			t.Fatalf("VerifyVerificationToken(%q) error = nil, want error", tc)
		}
	}
}
