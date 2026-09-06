package engine

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/jackc/pgx/v5"
)

func opaque() (string, error) {
	var b [24]byte

	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(b[:]), nil
}

func ident(s string) string {
	return pgx.Identifier{s}.Sanitize()
}

func literal(s string) string {
	return "'" + replaceQuotes(s) + "'"
}

func replaceQuotes(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
