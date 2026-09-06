package engine

import (
	"context"
	"testing"
)

func TestMalformedDSN(t *testing.T) {
	manager, err := New(context.Background(), Config{DSN: "postgres://%"})
	if err == nil {
		manager.Close()
		t.Fatal("malformed DSN accepted")
	}
}

func TestNewRejectsInvalidLimits(t *testing.T) {
	manager, err := New(context.Background(), Config{MaxRows: -1})
	if err == nil {
		manager.Close()
		t.Fatal("invalid limits accepted")
	}
}
