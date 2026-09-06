package engine

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRuntimeIdentityConfiguration(t *testing.T) {
	source, err := pgx.ParseConfig("postgres://management:management-secret@localhost/practice?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}

	source.RuntimeParams["search_path"] = "pg_catalog"
	cfg, err := (Config{}).normalized()
	if err != nil {
		t.Fatal(err)
	}

	manager := &Manager{runtime: source, config: cfg}
	runtime := manager.runtimeConfig(metadata{schema: "workspace", role: "runtime", password: "runtime-secret"})

	if runtime.User != "runtime" || runtime.Password != "runtime-secret" || runtime.Database != "practice" {
		t.Fatal("runtime connection did not retain its own authentication identity")
	}

	if runtime.RuntimeParams["search_path"] != `"workspace",pg_catalog` || runtime.RuntimeParams["statement_timeout"] != "10000" || runtime.RuntimeParams["lock_timeout"] != "1000" || runtime.RuntimeParams["idle_in_transaction_session_timeout"] != "15000" {
		t.Fatalf("incorrect runtime settings: %+v", runtime.RuntimeParams)
	}

	runtime.RuntimeParams["search_path"] = "changed"

	if source.User != "management" || source.Password != "management-secret" || source.RuntimeParams["search_path"] != "pg_catalog" {
		t.Fatal("runtime configuration mutated management credentials or session settings")
	}
}

type commitTransaction struct {
	pgx.Tx
	err   error
	calls int
}

func (tx *commitTransaction) Commit(context.Context) error {
	tx.calls++
	return tx.err
}

func TestCommitOutcomes(t *testing.T) {
	cases := []struct {
		name  string
		cause error
		kind  error
	}{
		{name: "success"},
		{name: "server rejection", cause: &pgconn.PgError{Code: "23505"}, kind: ErrQuery},
		{name: "missing acknowledgement", cause: io.EOF, kind: ErrOutcomeUnknown},
		{name: "deadline during commit", cause: context.DeadlineExceeded, kind: ErrOutcomeUnknown},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tx := &commitTransaction{err: test.cause}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			err := commit(ctx, tx)
			if !errors.Is(err, test.kind) || !errors.Is(err, test.cause) || tx.calls != 1 {
				t.Fatalf("commit outcome: %v, attempts: %d", err, tx.calls)
			}
		})
	}
}
