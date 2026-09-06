package engine

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type errorRow struct{ err error }

func (r errorRow) Scan(...any) error {
	return r.err
}

func TestMetadataReadErrors(t *testing.T) {
	_, err := readMetadata(errorRow{err: pgx.ErrNoRows})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing registry entry: %v", err)
	}

	original := &pgconn.PgError{Code: "55P03", Message: "registry row locked"}
	_, err = readMetadata(errorRow{err: original})

	if !errors.Is(err, ErrBusy) || !errors.Is(err, original) {
		t.Fatalf("registry lock error not preserved: %v", err)
	}
}
