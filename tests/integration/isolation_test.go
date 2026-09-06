package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/swualabs/pgislet"
)

func TestIsolationAndOwnership(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	a, b := islet(t, m), islet(t, m)

	if _, err := m.Batch(ctx, a, []string{`CREATE TABLE users(id int PRIMARY KEY,name text)`, `INSERT INTO users VALUES (1,'Alice')`}); err != nil {
		t.Fatal(err)
	}

	run(t, m, b, `CREATE TABLE users(id int)`)
	run(t, m, b, `CREATE VIEW v AS SELECT * FROM users`)
	md, err := m.lookup(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}

	r := run(t, m, a, `SELECT current_user=session_user,tableowner=current_user FROM pg_catalog.pg_tables WHERE schemaname=current_schema() AND tablename='users'`)

	if fmt.Sprint(r.Rows) != "[[t t]]" {
		t.Fatal(r.Rows)
	}

	var schemaOwner string

	if err = m.pool.QueryRow(ctx, `SELECT nspowner::regrole::text FROM pg_namespace WHERE nspname=$1`, md.schema).Scan(&schemaOwner); err != nil {
		t.Fatal(err)
	}

	if schemaOwner == md.role {
		t.Fatal("runtime owns schema")
	}

	for _, sql := range []string{`SELECT * FROM users`, `UPDATE users SET name='Bob'`, `DELETE FROM users WHERE id=2`, `ALTER TABLE users ADD COLUMN age int`, `CREATE INDEX ix ON users(age)`, `DROP INDEX ix`, `CREATE VIEW v AS SELECT * FROM users`, `DROP VIEW v`, `TRUNCATE users`, `DROP TABLE users`} {
		run(t, m, a, sql)
	}

	other, _ := m.lookup(ctx, b.ID)

	for _, sql := range []string{`SELECT * FROM %s.users`, `INSERT INTO %s.users VALUES(1)`, `UPDATE %s.users SET id=2`, `DELETE FROM %s.users`, `CREATE TABLE %s.bad(id int)`, `ALTER TABLE %s.users ADD COLUMN bad int`, `DROP TABLE %s.users`, `DROP VIEW %s.v`} {
		_, err := m.Execute(ctx, a, fmt.Sprintf(sql, ident(other.schema)))

		var pe *pgconn.PgError

		if !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatalf("expected ACL denial for %s: %v", sql, err)
		}
	}

	conn, err := m.connectRuntime(ctx, md)
	if err != nil {
		t.Fatal(err)
	}

	defer closeRuntime(conn)

	for _, sql := range []string{`SELECT * FROM ` + ident(other.schema) + `.users`, `SELECT * FROM pgislet_internal.islets`, `CREATE TABLE pgislet_internal.bad(n int)`, `CREATE TABLE pgislet_api.bad(n int)`, `CREATE TABLE public.bad(n int)`, `CREATE ROLE bad`, `CREATE DATABASE bad`, `CREATE SCHEMA bad`, `DROP SCHEMA ` + ident(md.schema) + ` CASCADE`, `CREATE EXTENSION hstore`, `ALTER SYSTEM SET work_mem='1GB'`, `SET ROLE postgres`, `SET SESSION AUTHORIZATION postgres`} {
		if _, err = conn.Exec(ctx, sql); err == nil {
			t.Fatalf("ACL accepted %s", sql)
		}
	}

	var safe bool
	err = m.pool.QueryRow(ctx, `SELECT NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND NOT rolbypassrls AND NOT rolinherit AND rolcanlogin AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid) FROM pg_roles r WHERE rolname=$1`, md.role).Scan(&safe)
	if err != nil || !safe {
		t.Fatalf("role privileges: %v %v", safe, err)
	}

	if _, err = conn.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}

	var user string

	if err = conn.QueryRow(ctx, `SELECT current_user`).Scan(&user); err != nil || user != md.role {
		t.Fatalf("identity escaped: %s %v", user, err)
	}
}

func TestLocalObjects(t *testing.T) {
	m := manager(t, pgislet.Config{})
	h := islet(t, m)

	for _, sql := range []string{
		`CREATE TYPE mood AS ENUM ('happy','sad')`, `ALTER TYPE mood ADD VALUE 'neutral'`,
		`CREATE DOMAIN positive AS int CHECK(VALUE>0)`, `CREATE TYPE pair AS (a int,b text)`,
		`CREATE SEQUENCE seq`, `ALTER SEQUENCE seq RESTART WITH 5`, `SELECT nextval('seq')`,
		`CREATE TABLE x(n positive, m mood)`, `INSERT INTO x VALUES(1,'happy')`,
		`CREATE MATERIALIZED VIEW mv AS SELECT * FROM x`, `REFRESH MATERIALIZED VIEW mv`,
		`CREATE FUNCTION inc(n int) RETURNS int LANGUAGE sql AS $$ SELECT n+1 $$`,
		`SELECT inc(1)`, `CREATE PROCEDURE add_row() LANGUAGE sql AS $$ INSERT INTO x VALUES(2,'sad') $$`,
		`CALL add_row()`, `CREATE POLICY p ON x USING(n>0)`, `ALTER TABLE x ENABLE ROW LEVEL SECURITY`,
		`COMMENT ON TABLE x IS 'learning'`, `EXPLAIN SELECT * FROM x`,
		`ALTER TABLE x RENAME COLUMN n TO number`, `ALTER TABLE x ALTER COLUMN number DROP NOT NULL`,
		`DROP POLICY p ON x`, `DROP PROCEDURE add_row()`, `DROP FUNCTION inc(int)`, `DROP MATERIALIZED VIEW mv`,
		`DROP TABLE x`, `DROP DOMAIN positive`, `DROP TYPE mood`, `DROP TYPE pair`, `DROP SEQUENCE seq`,
	} {
		run(t, m, h, sql)
	}
}
