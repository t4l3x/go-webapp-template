package security_test

import (
	"encoding/base64"
	"fmt"
	"testing"

	"golang.org/x/crypto/argon2"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/infrastructure/security"
)

func TestPasswordHasher_HashAndVerify_RoundTrip(t *testing.T) {
	hasher := security.NewPasswordHasher()

	encoded, err := hasher.Hash("supersecretpassword")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	if encoded == "supersecretpassword" {
		t.Fatalf("Hash() returned the plaintext password unmodified")
	}

	ok, err := hasher.Verify("supersecretpassword", encoded)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !ok {
		t.Fatalf("Verify() = false, want true for the correct password")
	}
}

func TestPasswordHasher_Verify_WrongPassword(t *testing.T) {
	hasher := security.NewPasswordHasher()

	encoded, err := hasher.Hash("supersecretpassword")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	ok, err := hasher.Verify("wrongpassword", encoded)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if ok {
		t.Fatalf("Verify() = true, want false for an incorrect password")
	}
}

func TestPasswordHasher_Hash_UsesRandomSalt(t *testing.T) {
	hasher := security.NewPasswordHasher()

	first, err := hasher.Hash("supersecretpassword")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	second, err := hasher.Hash("supersecretpassword")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	if first == second {
		t.Fatalf("Hash() produced identical output for two calls, want distinct salts")
	}
}

func TestPasswordHasher_Verify_MalformedHash(t *testing.T) {
	hasher := security.NewPasswordHasher()

	if _, err := hasher.Verify("supersecretpassword", "not-a-valid-hash"); err == nil {
		t.Fatalf("Verify() error = nil, want error for malformed encoded hash")
	}
}

// TestPasswordHasher_Verify_EmptyKeyFieldRejected guards against a
// bypass: if Verify trusted the stored hash's (possibly empty) key
// length to size the freshly computed candidate, a hash with a missing
// key field would decode to a 0-length key, a 0-length candidate would
// also be 0 bytes, and subtle.ConstantTimeCompare treats two 0-length
// slices as equal — meaning any password would "verify" successfully.
func TestPasswordHasher_Verify_EmptyKeyFieldRejected(t *testing.T) {
	hasher := security.NewPasswordHasher()

	salt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	malformed := fmt.Sprintf("$argon2id$v=%d$m=65536,t=3,p=4$%s$", argon2.Version, salt)

	if ok, err := hasher.Verify("any-password-at-all", malformed); err == nil {
		t.Fatalf("Verify() = (%v, nil), want an error for an encoded hash with an empty key field", ok)
	}
}

func TestPasswordHasher_Verify_ExcessiveMemoryParamRejected(t *testing.T) {
	hasher := security.NewPasswordHasher()

	salt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	key := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	malformed := fmt.Sprintf("$argon2id$v=%d$m=4000000000,t=3,p=4$%s$%s", argon2.Version, salt, key)

	if _, err := hasher.Verify("password", malformed); err == nil {
		t.Fatalf("Verify() error = nil, want error for a memory parameter far beyond what this package generates")
	}
}

func TestPasswordHasher_Verify_MalformedInputsNeverPanic(t *testing.T) {
	hasher := security.NewPasswordHasher()

	inputs := []string{
		"",
		"$",
		"$argon2id$",
		"$argon2id$v=19$m=65536,t=3,p=4$$",
		"$argon2id$v=19$m=0,t=0,p=0$c29tZXNhbHQ$c29tZWhhc2g",
		"$argon2id$v=19$m=abc,t=3,p=4$c29tZXNhbHQ$c29tZWhhc2g",
		"$bcrypt$v=19$m=65536,t=3,p=4$c29tZXNhbHQ$c29tZWhhc2g",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			// The only contract under test is "never panics"; any
			// returned error is acceptable for garbage input.
			_, _ = hasher.Verify("password", input)
		})
	}
}
