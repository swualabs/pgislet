package pgislet_test

import (
	"context"
	"errors"
	"testing"

	"github.com/swualabs/pgislet"
)

func TestPublicErrorContract(t *testing.T) {
	cause := context.Canceled
	err := &pgislet.Error{Kind: pgislet.ErrInitialization, Cause: cause, Statement: 2}

	if !errors.Is(err, pgislet.ErrInitialization) || !errors.Is(err, cause) {
		t.Fatal("public error contract lost classification or cause")
	}
}

func TestPublicConstructorValidation(t *testing.T) {
	manager, err := pgislet.New(context.Background(), pgislet.Config{MaxRows: -1})
	if err == nil {
		manager.Close()
		t.Fatal("public constructor accepted invalid configuration")
	}
}
