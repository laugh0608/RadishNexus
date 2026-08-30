package authn

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argon2Memory      = 19 * 1024
	argon2Iterations  = 2
	argon2Parallelism = 1
	argon2SaltBytes   = 16
	argon2KeyBytes    = 32
	dummySalt         = "radishnexus-dmmy"
)

type Argon2idHasher struct {
	random   io.Reader
	dummyKey []byte
}

func NewArgon2idHasher() *Argon2idHasher {
	return &Argon2idHasher{
		random: rand.Reader,
		dummyKey: argon2.IDKey(
			[]byte("radishnexus invalid credential"),
			[]byte(dummySalt),
			argon2Iterations,
			argon2Memory,
			argon2Parallelism,
			argon2KeyBytes,
		),
	}
}

func (hasher *Argon2idHasher) Hash(password string) (string, error) {
	salt := make([]byte, argon2SaltBytes)
	if _, err := io.ReadFull(hasher.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey(
		[]byte(password),
		salt,
		argon2Iterations,
		argon2Memory,
		argon2Parallelism,
		argon2KeyBytes,
	)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argon2Memory,
		argon2Iterations,
		argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func (*Argon2idHasher) Verify(encoded string, password string) (bool, error) {
	salt, expected, err := parseArgon2idHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey(
		[]byte(password),
		salt,
		argon2Iterations,
		argon2Memory,
		argon2Parallelism,
		argon2KeyBytes,
	)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func (hasher *Argon2idHasher) VerifyDummy(password string) error {
	if len(hasher.dummyKey) != argon2KeyBytes {
		return fmt.Errorf("Argon2id dummy verifier is not initialized")
	}
	actual := argon2.IDKey(
		[]byte(password),
		[]byte(dummySalt),
		argon2Iterations,
		argon2Memory,
		argon2Parallelism,
		argon2KeyBytes,
	)
	_ = subtle.ConstantTimeCompare(actual, hasher.dummyKey)
	return nil
}

func parseArgon2idHash(encoded string) ([]byte, []byte, error) {
	if len(encoded) > 256 {
		return nil, nil, fmt.Errorf("invalid Argon2id password hash length")
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return nil, nil, fmt.Errorf("invalid Argon2id password hash format")
	}
	version, err := parseExactPositiveInteger(parts[2], "v=")
	if err != nil || version != argon2.Version {
		return nil, nil, fmt.Errorf("unsupported Argon2id password hash version")
	}
	parameters := strings.Split(parts[3], ",")
	if len(parameters) != 3 {
		return nil, nil, fmt.Errorf("invalid Argon2id password hash parameters")
	}
	memory, memoryErr := parseExactPositiveInteger(parameters[0], "m=")
	iterations, iterationErr := parseExactPositiveInteger(parameters[1], "t=")
	parallelism, parallelismErr := parseExactPositiveInteger(parameters[2], "p=")
	if memoryErr != nil || iterationErr != nil || parallelismErr != nil ||
		memory != argon2Memory || iterations != argon2Iterations || parallelism != argon2Parallelism {
		return nil, nil, fmt.Errorf("unsupported Argon2id password hash parameters")
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) != argon2SaltBytes {
		return nil, nil, fmt.Errorf("invalid Argon2id password hash salt")
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) != argon2KeyBytes {
		return nil, nil, fmt.Errorf("invalid Argon2id password hash output")
	}
	return salt, key, nil
}

func parseExactPositiveInteger(value string, prefix string) (int, error) {
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return 0, fmt.Errorf("missing integer")
	}
	number := value[len(prefix):]
	if len(number) > 1 && number[0] == '0' {
		return 0, fmt.Errorf("non-canonical integer")
	}
	parsed, err := strconv.Atoi(number)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid integer")
	}
	return parsed, nil
}
