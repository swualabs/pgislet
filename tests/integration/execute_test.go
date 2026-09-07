package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/swualabs/pgislet"
)

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

	for _, args := range [][]any{{b.ID, md.Generation, nil}, {h.ID, md.Generation + 1, nil}, {h.ID, md.Generation, "wrong"}} {
		if _, err = conn.Exec(ctx, `SELECT pgislet_api.enter($1,$2,$3)`, args...); err == nil {
			t.Fatal("gateway accepted invalid identity/generation/token")
		}
	}

	if _, err = conn.Exec(ctx, `SELECT pgislet_api.finish($1,$2,NULL)`, h.ID, md.Generation); err == nil {
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
