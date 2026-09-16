package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || len(value) > 254 || !strings.Contains(value, ".") {
		return "", fmt.Errorf("enter a valid email address")
	}

	return value, nil
}

func hashPassword(password string) (string, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 72 {
		return "", fmt.Errorf("use at least 12 characters and at most 72 bytes for your password")
	}

	value, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(value), err
}

func randomToken() string {
	token := make([]byte, 32)
	_, _ = rand.Read(token)
	return hex.EncodeToString(token)
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
