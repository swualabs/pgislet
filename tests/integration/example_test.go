package integration

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/swualabs/pgislet"
	"github.com/swualabs/pgislet/internal/playground"
)

func TestPlayground(t *testing.T) {
	_ = manager(t, pgislet.Config{})

	var output bytes.Buffer

	if err := playground.Run(context.Background(), testDSN, &output); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"Seed queried from another application node: [[Alice] [Bob]]",
		"The runtime role dropped its seed table.",
		"Cross-islet access was denied by PostgreSQL.",
		"Reset left an empty schema and fenced the old handle.",
		"Caller-provided initialization restored 2 learners.",
		"Both islets deleted successfully.",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q in example output: %s", want, output.String())
		}
	}
}
