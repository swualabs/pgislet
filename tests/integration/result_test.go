package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/swualabs/pgislet"
)

func TestBatchResultsAndLimits(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{MaxRows: 3, MaxResultBytes: 1024})
	h := islet(t, m)
	results, err := m.Batch(ctx, h, []string{`CREATE TABLE x(n int UNIQUE)`, `INSERT INTO x VALUES(1),(2)`, `SELECT n,NULL::text AS empty FROM x ORDER BY n`})
	if err != nil {
		t.Fatal(err)
	}

	if results[1].RowsAffected != 2 || results[2].Columns[0].DataTypeOID != 23 || results[2].Rows[0][1] != nil || results[0].CommandTag != "CREATE TABLE" {
		t.Fatal(results)
	}

	_, err = m.Batch(ctx, h, []string{`INSERT INTO x VALUES(3)`, `INSERT INTO x VALUES(1)`})

	var pe *pgconn.PgError

	if !errors.As(err, &pe) || pe.Code != "23505" || pe.ConstraintName == "" {
		t.Fatal(err)
	}

	if got := run(t, m, h, `SELECT count(*) FROM x`).Rows[0][0]; got != "2" {
		t.Fatal(got)
	}

	r, err := m.Execute(ctx, h, `INSERT INTO x SELECT generate_series(3,10) RETURNING n`)
	if !errors.Is(err, pgislet.ErrResultLimit) || !r.Truncated {
		t.Fatalf("%+v %v", r, err)
	}

	if got := run(t, m, h, `SELECT count(*) FROM x`).Rows[0][0]; got != "2" {
		t.Fatal(got)
	}

	_, err = m.Execute(ctx, h, `SELECT repeat('x',2000)`)
	if !errors.Is(err, pgislet.ErrResultLimit) {
		t.Fatal(err)
	}

	_, err = m.Batch(ctx, h, []string{`SELECT generate_series(1,2)`, `SELECT generate_series(1,2)`})
	if !errors.Is(err, pgislet.ErrResultLimit) {
		t.Fatal(err)
	}

	_, err = m.Execute(ctx, h, strings.Repeat("x", (1<<20)+1))
	if !errors.Is(err, pgislet.ErrSQLTooLarge) {
		t.Fatal(err)
	}

	_, err = m.Batch(ctx, h, make([]string, 101))
	if !errors.Is(err, pgislet.ErrSQLTooLarge) {
		t.Fatal(err)
	}

	_, err = m.Execute(ctx, h, `COMMIT`)
	if !errors.Is(err, pgislet.ErrPolicy) {
		t.Fatal(err)
	}
}
