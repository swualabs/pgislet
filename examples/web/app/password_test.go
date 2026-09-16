package app

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswords(t *testing.T) {
	for _, password := range []string{"short", strings.Repeat("a", 73), "invalid\xffpassword"} {
		if _, err := hashPassword(password); err == nil {
			t.Fatal("accepted invalid password")
		}
	}

	hash, err := hashPassword("a long unique password")
	if err != nil {
		t.Fatal(err)
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("a long unique password")) != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte("incorrect password")) == nil {
		t.Fatal("incorrect password comparison")
	}

	first, second := randomToken(), randomToken()

	if len(first) != 64 || first == second || tokenHash(first) == first || tokenHash(first) != tokenHash(first) {
		t.Fatal("invalid session token generation")
	}
}

func TestEmailNormalization(t *testing.T) {
	email, err := normalizeEmail("  USER@Example.com ")
	if err != nil || email != "user@example.com" {
		t.Fatalf("%s: %v", email, err)
	}

	for _, value := range []string{"not-email", "Person <a@example.com>", "a@b", strings.Repeat("x", 255) + "@example.com"} {
		if _, err := normalizeEmail(value); err == nil {
			t.Fatal("accepted invalid email")
		}
	}
}
