package playground

import (
	"bytes"
	"context"
	"testing"
)

func TestInvalidConnection(t *testing.T) {
	var output bytes.Buffer

	if err := Run(context.Background(), "postgres://%", &output); err == nil {
		t.Fatal("invalid connection accepted")
	}

	if output.Len() != 0 {
		t.Fatal("example reported success before connecting")
	}
}
