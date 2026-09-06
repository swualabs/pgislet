package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestTimeoutAndGateway(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{StatementTimeout: 100 * time.Millisecond})
	h, b := islet(t, m), islet(t, m)

	if _, err := m.Execute(ctx, h, `SELECT pg_sleep(2)`); !errors.Is(err, pgislet.ErrTimeout) {
		t.Fatal(err)
	}

	canceled, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()

	if _, err := m.Execute(canceled, h, `SELECT pg_sleep(2)`); !errors.Is(err, pgislet.ErrTimeout) {
		t.Fatal(err)
	}

	md, _ := m.lookup(ctx, h.ID)
	conn, err := m.connectRuntime(ctx, md)
	if err != nil {
		t.Fatal(err)
	}

	defer closeRuntime(conn)

	for _, args := range [][]any{{b.ID, b.Generation, nil}, {h.ID, h.Generation + 1, nil}, {h.ID, h.Generation, "wrong"}} {
		if _, err = conn.Exec(ctx, `SELECT pgislet_api.enter($1,$2,$3)`, args...); err == nil {
			t.Fatal("gateway accepted invalid identity/generation/token")
		}
	}

	if _, err = conn.Exec(ctx, `SELECT pgislet_api.finish($1,$2,NULL)`, h.ID, h.Generation); err == nil {
		t.Fatal("finish accepted NULL token")
	}

	run(t, m, h, `CREATE FUNCTION current_user_fake() RETURNS text LANGUAGE sql AS $$ SELECT 'postgres'::text $$`)
	run(t, m, h, `SELECT 1`)
}

func TestPolicyFunctionOverloads(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	h := islet(t, m)

	for _, sql := range []string{
		`CREATE FUNCTION pg_notify() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$`,
		`CREATE FUNCTION query_to_xml() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$`,
		`CREATE FUNCTION wrapper() RETURNS text LANGUAGE sql AS $$ SELECT pg_catalog.set_config('statement_timeout','0',false) $$`,
		`CREATE FUNCTION nested() RETURNS int LANGUAGE sql AS $$ CREATE FUNCTION pg_notify() RETURNS int LANGUAGE sql AS 'SELECT 1'; SELECT 1 $$`,
		`CREATE FUNCTION sneaky(n text DEFAULT pg_read_file('/etc/passwd')) RETURNS text LANGUAGE sql AS $$ SELECT n $$`,
		`CREATE DOMAIN sneaky AS text CHECK (pg_notify('a','b') IS NULL)`,
		`CREATE TABLE sneaky(n text DEFAULT query_to_xml('SELECT 1',false,false,''))`,
		`CALL pgislet_api.enter('x',1)`,
	} {
		if _, err := m.Execute(ctx, h, sql); !errors.Is(err, pgislet.ErrPolicy) {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	run(t, m, h, `CREATE FUNCTION increment(n int) RETURNS int LANGUAGE sql RETURN n+1`)

	if value := run(t, m, h, `SELECT increment(3)`).Rows[0][0]; value != "4" {
		t.Fatal(value)
	}
}

func TestCommitFailurePreservesPostgresError(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	h := islet(t, m)
	run(t, m, h, `CREATE TABLE deferred(n int UNIQUE DEFERRABLE INITIALLY DEFERRED)`)
	_, err := m.Execute(ctx, h, `INSERT INTO deferred VALUES(1),(1)`)

	var pe *pgconn.PgError

	if !errors.Is(err, pgislet.ErrQuery) || !errors.As(err, &pe) || pe.Code != "23505" {
		t.Fatal(err)
	}

	if got := run(t, m, h, `SELECT count(*) FROM deferred`).Rows[0][0]; got != "0" {
		t.Fatal(got)
	}
}
