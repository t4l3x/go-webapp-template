package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2Params are the Argon2id parameters used to hash passwords.
// Chosen per the RFC 9106 recommendation for environments without
// dedicated hardware: 64 MiB memory, 3 iterations, 4 parallel lanes.
type argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultArgon2Params = argon2Params{
	memory:      64 * 1024,
	iterations:  3,
	parallelism: 4,
	saltLength:  16,
	keyLength:   32,
}

// Bounds enforced when decoding a stored hash, so that a corrupted or
// tampered-with encoded value can never make Verify perform an
// absurdly expensive (or trivially bypassable) computation:
//   - minSaltBytes/minKeyBytes reject a hash with a missing or
//     truncated salt/key field. Without this, an encoded hash with an
//     empty key field decodes to a 0-length key, and a freshly
//     computed 0-length candidate key would then compare as "equal"
//     to it for any password.
//   - the memory/iterations/parallelism ceilings reject parameters far
//     beyond anything this package would ever generate itself,
//     preventing a corrupted hash from triggering an unbounded
//     memory allocation or CPU spend inside argon2.IDKey.
const (
	minSaltBytes = 8
	minKeyBytes  = 16

	maxArgon2Memory      = 1 << 20 // 1 GiB, in KiB as argon2 expects
	maxArgon2Iterations  = 10
	maxArgon2Parallelism = 64
	maxArgon2KeyBytes    = 128
)

// PasswordHasher hashes and verifies passwords using Argon2id. It
// implements application.PasswordHasher.
type PasswordHasher struct {
	params argon2Params
}

func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{params: defaultArgon2Params}
}

// Hash returns a self-describing encoded hash containing the algorithm
// parameters, salt, and derived key, in PHC-like string format:
// $argon2id$v=19$m=<memory>,t=<iterations>,p=<parallelism>$<salt>$<hash>
func (h *PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.saltLength)

	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey(
		[]byte(password),
		salt,
		h.params.iterations,
		h.params.memory,
		h.params.parallelism,
		h.params.keyLength,
	)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.params.memory,
		h.params.iterations,
		h.params.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)

	return encoded, nil
}

// Verify reports whether password matches the given encoded hash, using
// a constant-time comparison of the derived keys.
func (h *PasswordHasher) Verify(password string, encodedHash string) (bool, error) {
	params, salt, key, err := decodeArgon2Hash(encodedHash)
	if err != nil {
		return false, err
	}

	if len(key) > maxArgon2KeyBytes {
		return false, fmt.Errorf("key too long")
	}

	candidate := argon2.IDKey(
		[]byte(password),
		salt,
		params.iterations,
		params.memory,
		params.parallelism,
		uint32(len(key)), //nolint:gosec // len(key) is bounded by maxArgon2KeyBytes immediately above.
	)

	return subtle.ConstantTimeCompare(candidate, key) == 1, nil
}

func decodeArgon2Hash(encoded string) (argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")

	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argon2Params{}, nil, nil, fmt.Errorf("invalid encoded hash format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("parse hash version: %w", err)
	}
	if version != argon2.Version {
		return argon2Params{}, nil, nil, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var params argon2Params
	if _, err := fmt.Sscanf(
		parts[3],
		"m=%d,t=%d,p=%d",
		&params.memory,
		&params.iterations,
		&params.parallelism,
	); err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("parse hash params: %w", err)
	}

	if params.memory == 0 || params.memory > maxArgon2Memory ||
		params.iterations == 0 || params.iterations > maxArgon2Iterations ||
		params.parallelism == 0 || params.parallelism > maxArgon2Parallelism {
		return argon2Params{}, nil, nil, fmt.Errorf("hash params outside of allowed bounds")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("decode salt: %w", err)
	}
	if len(salt) < minSaltBytes {
		return argon2Params{}, nil, nil, fmt.Errorf("salt too short")
	}

	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("decode key: %w", err)
	}
	if len(key) < minKeyBytes {
		return argon2Params{}, nil, nil, fmt.Errorf("key too short")
	}

	return params, salt, key, nil
}
