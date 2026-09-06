package engine

import (
	"encoding/hex"
	"testing"
)

func TestOpaqueIdentifiers(t *testing.T) {
	seen := map[string]bool{}

	for range 100 {
		id, err := opaque()
		if err != nil {
			t.Fatal(err)
		}

		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != 24 || seen[id] {
			t.Fatalf("invalid or duplicate identifier: %q", id)
		}

		seen[id] = true
	}
}

func TestSQLQuoting(t *testing.T) {
	if got := ident(`islet"; DROP SCHEMA public; --`); got != `"islet""; DROP SCHEMA public; --"` {
		t.Fatal(got)
	}

	if got := literal("user's data"); got != "'user''s data'" {
		t.Fatal(got)
	}
}
