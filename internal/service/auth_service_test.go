package service

import (
	"strings"
	"testing"
)

func TestHashPasswordUsesArgon2id(t *testing.T) {
	hash, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("hash format = %q, want argon2id PHC format", hash)
	}
	if !verifyPassword("correct horse battery staple", hash) {
		t.Fatal("expected password verification to succeed")
	}
	if verifyPassword("wrong password", hash) {
		t.Fatal("expected wrong password verification to fail")
	}
}

func TestHashPasswordUsesUniqueSalts(t *testing.T) {
	first, err := hashPassword("same password")
	if err != nil {
		t.Fatalf("hash first password: %v", err)
	}
	second, err := hashPassword("same password")
	if err != nil {
		t.Fatalf("hash second password: %v", err)
	}
	if first == second {
		t.Fatal("expected separate password hashes to use different salts")
	}
}

func TestVerifyPasswordRejectsMalformedOrLegacyHash(t *testing.T) {
	for _, hash := range []string{"", "not-a-password-hash", "$2a$10$legacy-bcrypt-hash"} {
		if verifyPassword("password", hash) {
			t.Fatalf("expected hash %q to be rejected", hash)
		}
	}
}
