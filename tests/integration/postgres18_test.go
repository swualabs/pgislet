package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/swualabs/pgislet"
)

func TestPostgres18Features(t *testing.T) {
	m := manager(t, pgislet.Config{})
	var version int

	if err := m.pool.QueryRow(context.Background(), `SELECT current_setting('server_version_num')::int`).Scan(&version); err != nil {
		t.Fatal(err)
	}

	if version < 180000 {
		t.Skip("PostgreSQL 18 syntax requires PostgreSQL 18")
	}

	h := islet(t, m)
	run(t, m, h, `CREATE TABLE generated(n int, doubled int GENERATED ALWAYS AS (n*2) VIRTUAL)`)

	if got := run(t, m, h, `INSERT INTO generated(n) VALUES(4) RETURNING doubled`).Rows[0][0]; got != "8" {
		t.Fatal(got)
	}

	row := run(t, m, h, `UPDATE generated SET n=5 RETURNING WITH (OLD AS o, NEW AS n) o.doubled,n.doubled`).Rows[0]

	if row[0] != "8" || row[1] != "10" {
		t.Fatal(row)
	}

	row = run(t, m, h, `SELECT uuid_extract_version(uuidv4()), uuid_extract_version(uuidv7()), uuid_extract_timestamp(uuidv7()) IS NOT NULL`).Rows[0]

	if row[0] != "4" || row[1] != "7" || row[2] != "t" {
		t.Fatal(row)
	}

	run(t, m, h, `CREATE TABLE periods(resource daterange, valid_at daterange, PRIMARY KEY(resource, valid_at WITHOUT OVERLAPS))`)
	run(t, m, h, `CREATE TABLE bookings(resource daterange, valid_at daterange, FOREIGN KEY(resource, PERIOD valid_at) REFERENCES periods(resource, PERIOD valid_at))`)
	run(t, m, h, `INSERT INTO periods VALUES ('[2000-01-01,2000-01-02)', '[2026-01-01,2026-02-01)')`)
	run(t, m, h, `INSERT INTO bookings VALUES ('[2000-01-01,2000-01-02)', '[2026-01-10,2026-01-20)')`)

	for sql, code := range map[string]string{
		`INSERT INTO periods VALUES ('[2000-01-01,2000-01-02)', '[2026-01-15,2026-02-15)')`:  "23P01",
		`INSERT INTO bookings VALUES ('[2000-01-01,2000-01-02)', '[2026-03-01,2026-04-01)')`: "23503",
	} {
		_, err := m.Execute(context.Background(), h, sql)
		var pe *pgconn.PgError

		if !errors.Is(err, pgislet.ErrQuery) || !errors.As(err, &pe) || pe.Code != code {
			t.Fatalf("constraint accepted invalid range: %s: %v", sql, err)
		}
	}

	for _, sql := range []string{
		`CREATE TABLE unsafe(n text GENERATED ALWAYS AS (pg_read_file('/etc/passwd')) VIRTUAL)`,
		`UPDATE generated SET n=6 RETURNING WITH (OLD AS o, NEW AS n) pg_catalog.set_config('statement_timeout','0',false)`,
	} {
		if _, err := m.Execute(context.Background(), h, sql); !errors.Is(err, pgislet.ErrPolicy) {
			t.Fatalf("policy accepted unsafe expression: %s: %v", sql, err)
		}
	}

	if got := run(t, m, h, `SELECT n FROM generated`).Rows[0][0]; got != "5" {
		t.Fatal(got)
	}
}
