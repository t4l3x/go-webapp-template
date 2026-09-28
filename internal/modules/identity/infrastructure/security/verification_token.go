package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// VerificationTokenConfig carries the dedicated email key, separate from JWTs.
type VerificationTokenConfig struct {
	Secret string
}

// Include purpose and format version in the HMAC to separate token domains.
const verificationTokenPurpose = "email-verification:v1" //nolint:gosec // HMAC purpose label, not a secret

// VerificationSigner signs and checks email tokens; use cases enforce expiry
// and one-time consumption against persisted state.
type VerificationSigner struct {
	secret []byte
}

func NewVerificationSigner(cfg VerificationTokenConfig) *VerificationSigner {
	return &VerificationSigner{secret: []byte(cfg.Secret)}
}

// SignVerificationToken is deterministic for a fixed key, ID, and expiry.
// Retried mail uses the same credential but may still be delivered twice.
// Rotating the key invalidates outstanding links; no historical keys are kept.
func (s *VerificationSigner) SignVerificationToken(verificationID uuid.UUID, expiresAt time.Time) string {
	id := verificationID.String()
	exp := strconv.FormatInt(expiresAt.UTC().Unix(), 10)

	signature := s.signature(id, exp)

	return id + "." + exp + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// VerifyVerificationToken checks a token produced by
// SignVerificationToken, returning the verification ID and expiry it
// commits to if — and only if — the signature is valid for the
// configured secret and purpose/version. It does not check expiresAt
// against the current time or consult the database; a verify use case
// is responsible for both of those (this function only proves the
// token wasn't forged and hasn't been tampered with).
func (s *VerificationSigner) VerifyVerificationToken(token string) (verificationID uuid.UUID, expiresAt time.Time, err error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return uuid.Nil, time.Time{}, errors.New("malformed verification token")
	}

	id, expUnix, signature := parts[0], parts[1], parts[2]

	want := s.signature(id, expUnix)

	got, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(want, got) {
		return uuid.Nil, time.Time{}, errors.New("invalid verification token signature")
	}

	verificationID, err = uuid.Parse(id)
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("parse verification id: %w", err)
	}

	seconds, err := strconv.ParseInt(expUnix, 10, 64)
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("parse verification expiry: %w", err)
	}

	return verificationID, time.Unix(seconds, 0).UTC(), nil
}

// signature computes the canonical HMAC over (purpose, id, exp). Each
// field is written as its own Write call separated by '|': the purpose
// is a fixed constant containing no '|', id is a canonical UUID string
// (hyphens and hex digits only), and exp is decimal digits only — none
// of the three can contain '|', so no two distinct (purpose, id, exp)
// triples can ever canonicalize to the same byte sequence (unlike naive
// concatenation, e.g. "ab"+"c" vs "a"+"bc").
func (s *VerificationSigner) signature(id, exp string) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(verificationTokenPurpose))
	mac.Write([]byte{'|'})
	mac.Write([]byte(id))
	mac.Write([]byte{'|'})
	mac.Write([]byte(exp))

	return mac.Sum(nil)
}
