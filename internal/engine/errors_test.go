package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestErrorChains(t *testing.T) {
	err := wrap(ErrRuntimeConnection, classify(context.DeadlineExceeded))

	for _, target := range []error{ErrRuntimeConnection, ErrTimeout, context.DeadlineExceeded} {
		if !errors.Is(err, target) {
			t.Fatalf("lost %v", target)
		}
	}

	if classify(nil) != nil {
		t.Fatal("nil error changed")
	}

	if !errors.Is(classify(context.Canceled), context.Canceled) {
		t.Fatal("cancellation lost")
	}
}

func TestPostgreSQLErrorMapping(t *testing.T) {
	cases := map[string]error{
		"55P03": ErrBusy,
		"57014": ErrTimeout,
		"PI001": ErrNotFound,
		"PI002": ErrStaleGeneration,
		"PI003": ErrUnavailable,
		"PI004": ErrFailed,
		"23505": ErrQuery,
	}

	for code, kind := range cases {
		t.Run(code, func(t *testing.T) {
			original := &pgconn.PgError{Code: code, Message: "original message", Detail: "detail", Hint: "hint", Position: 7, ConstraintName: "unique_key"}
			err := classify(original)

			var preserved *pgconn.PgError

			if !errors.Is(err, kind) || !errors.As(err, &preserved) || preserved != original {
				t.Fatalf("error information lost: %v", err)
			}

			var mapped *Error

			if !errors.As(err, &mapped) || mapped.Statement != -1 {
				t.Fatalf("invalid statement index: %v", err)
			}

			if mapped.Error() == "" {
				t.Fatal("missing error text")
			}
		})
	}
}
