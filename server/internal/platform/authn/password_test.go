package authn

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestArgon2idHasherRoundTripUsesFrozenParameters(t *testing.T) {
	t.Parallel()
	hasher := &Argon2idHasher{random: bytes.NewReader(bytes.Repeat([]byte{7}, argon2SaltBytes))}
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	wantPrefix := "$argon2id$v=19$m=19456,t=2,p=1$" +
		base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{7}, argon2SaltBytes)) + "$"
	if !strings.HasPrefix(encoded, wantPrefix) {
		t.Fatalf("Hash() = %q", encoded)
	}
	matches, err := hasher.Verify(encoded, "correct horse battery staple")
	if err != nil || !matches {
		t.Fatalf("Verify() valid = %v, %v", matches, err)
	}
	matches, err = hasher.Verify(encoded, "incorrect horse battery staple")
	if err != nil || matches {
		t.Fatalf("Verify() invalid = %v, %v", matches, err)
	}
}

func TestArgon2idHasherUsesIndependentSalt(t *testing.T) {
	t.Parallel()
	hasher := NewArgon2idHasher()
	first, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() first error = %v", err)
	}
	second, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() second error = %v", err)
	}
	if first == second {
		t.Fatal("Hash() reused a salt")
	}
}

func TestArgon2idHasherRejectsMalformedOrUnboundedHashes(t *testing.T) {
	t.Parallel()
	hasher := NewArgon2idHasher()
	tests := []string{
		"",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=16$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=019456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=1048576,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA$extra",
		"$argon2id$v=19$m=19456,t=2,p=1$not+strict/$not+strict/",
	}
	for _, encoded := range tests {
		if _, err := hasher.Verify(encoded, "password"); err == nil {
			t.Fatalf("Verify(%q) error = nil", encoded)
		}
	}
}

func TestArgon2idHasherDummyVerificationUsesSameWorkFactor(t *testing.T) {
	t.Parallel()
	if err := NewArgon2idHasher().VerifyDummy("unknown account password"); err != nil {
		t.Fatalf("VerifyDummy() error = %v", err)
	}
}
