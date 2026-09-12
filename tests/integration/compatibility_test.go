package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/swualabs/pgislet"
)

func TestSharedSQLCompatibility(t *testing.T) {
	m := manager(t, pgislet.Config{})
	h := islet(t, m)
	run(t, m, h, `CREATE TABLE items(id int PRIMARY KEY, n int, doubled int GENERATED ALWAYS AS (n*2) STORED)`)
	run(t, m, h, `INSERT INTO items(id,n) VALUES(1,4)`)
	run(t, m, h, `MERGE INTO items AS target USING (VALUES(1,5),(2,6)) AS source(id,n) ON target.id=source.id WHEN MATCHED THEN UPDATE SET n=source.n WHEN NOT MATCHED THEN INSERT(id,n) VALUES(source.id,source.n)`)
	rows := run(t, m, h, `SELECT id,doubled FROM items ORDER BY id`).Rows

	if len(rows) != 2 || rows[0][1] != "10" || rows[1][1] != "12" {
		t.Fatal(rows)
	}

	row := run(t, m, h, `SELECT gen_random_uuid() IS NOT NULL, jsonb_path_query_first('{"a":[1,2]}'::jsonb,'$.a[1]')`).Rows[0]

	if row[0] != "t" || row[1] != "2" {
		t.Fatal(row)
	}

	_, err := m.Execute(context.Background(), h, `INSERT INTO items(id,n) VALUES(1,9)`)
	var pe *pgconn.PgError

	if !errors.Is(err, pgislet.ErrQuery) || !errors.As(err, &pe) || pe.Code != "23505" {
		t.Fatalf("expected duplicate key violation, got %v", err)
	}

	if got := run(t, m, h, `SELECT doubled FROM items WHERE id=1`).Rows[0][0]; got != "10" {
		t.Fatal(got)
	}
}
