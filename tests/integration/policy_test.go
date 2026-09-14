package integration

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/swualabs/pgislet"
)

func TestPolicyRejectionAtomicity(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	other := islet(t, m)
	run(t, m, other, `CREATE TABLE guard(value text)`)
	run(t, m, other, `INSERT INTO guard VALUES('other')`)

	var version int

	if err := m.pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&version); err != nil {
		t.Fatal(err)
	}

	type policyCase struct {
		name string
		sql  string
	}

	cases := []policyCase{
		{"notify", `SELECT pg_notify('policy_probe','message')`},
		{"file access", `SELECT pg_read_file('policy_probe_missing_file')`},
		{"binary file access", `SELECT pg_read_binary_file('policy_probe_missing_file')`},
		{"dynamic query", `SELECT query_to_xml('SELECT 1',false,false,'')`},
		{"external builtin name", `SELECT public.lower('SAFE')`},
		{"gateway enter", `SELECT pgislet_api.enter('probe',1,NULL)`},
		{"gateway finish", `SELECT pgislet_api.finish('probe',1,NULL)`},
		{"subquery", `SELECT (SELECT pg_catalog.set_config('application_name','policy-probe',true))`},
		{"cte", `WITH payload AS (SELECT pg_catalog.set_config('application_name','policy-probe',true)) SELECT * FROM payload`},
		{"unicode name", `SELECT pg_catalog.U&"set_\0063onfig"('application_name','policy-probe',true)`},
		{"returning", `UPDATE guard SET value='changed' RETURNING pg_catalog.set_config('application_name','policy-probe',true)`},
		{"function text", `CREATE FUNCTION payload() RETURNS text LANGUAGE sql AS $$ SELECT pg_catalog.set_config('application_name','policy-probe',true) $$`},
		{"function atomic", `CREATE FUNCTION payload() RETURNS text LANGUAGE sql BEGIN ATOMIC SELECT pg_catalog.set_config('application_name','policy-probe',true); END`},
		{"function default", `CREATE FUNCTION payload(n text DEFAULT pg_catalog.set_config('application_name','policy-probe',true)) RETURNS text LANGUAGE sql RETURN n`},
		{"generated stored", `CREATE TABLE payload(n int, value text GENERATED ALWAYS AS (pg_catalog.set_config('application_name','policy-probe',true)) STORED)`},
		{"constraint", `ALTER TABLE guard ADD CONSTRAINT payload CHECK (pg_catalog.set_config('application_name','policy-probe',true) IS NOT NULL)`},
		{"row policy", `CREATE POLICY payload ON guard USING (pg_catalog.set_config('application_name','policy-probe',true) IS NOT NULL)`},
		{"multiple statements", `SELECT 1; UPDATE guard SET value='changed'`},
		{"nested transaction", `CREATE FUNCTION payload() RETURNS int LANGUAGE sql AS $$ COMMIT; SELECT 1 $$`},
		{"unlogged table", `ALTER TABLE guard SET UNLOGGED`},
		{"unlogged sequence", `ALTER SEQUENCE guard_seq SET UNLOGGED`},
	}

	if version >= 180000 {
		cases = append(cases, policyCase{"generated virtual", `CREATE TABLE payload(n int, value text GENERATED ALWAYS AS (pg_catalog.set_config('application_name','policy-probe',true)) VIRTUAL`})
		cases = append(cases, policyCase{"returning aliases", `UPDATE guard SET value='changed' RETURNING WITH (OLD AS o, NEW AS n) pg_catalog.set_config('application_name','policy-probe',true)`})
	}

	for _, test := range cases {
		for _, position := range []int{0, 2, 4} {
			t.Run(fmt.Sprintf("%s/position_%d", test.name, position), func(t *testing.T) {
				h := islet(t, m)
				run(t, m, h, `CREATE TABLE guard(id int PRIMARY KEY, value text)`)
				run(t, m, h, `INSERT INTO guard VALUES(1,'original')`)
				run(t, m, h, `CREATE SEQUENCE guard_seq`)
				before, err := m.lookup(ctx, h.ID)
				if err != nil {
					t.Fatal(err)
				}

				statements := []string{
					`INSERT INTO guard VALUES(2,'pending')`,
					`CREATE TABLE staged(n int)`,
					`CREATE FUNCTION staged_fn() RETURNS int LANGUAGE sql RETURN 1`,
					`UPDATE guard SET value='tail'`,
				}
				batch := append([]string{}, statements[:position]...)
				batch = append(batch, test.sql)
				batch = append(batch, statements[position:]...)

				if position < len(statements) {
					batch = append(batch, `SELECT nextval('guard_seq')`)
				}

				results, err := m.Batch(ctx, h, batch)
				var pe *pgislet.Error

				if !errors.Is(err, pgislet.ErrPolicy) || !errors.As(err, &pe) || pe.Statement != position || len(results) != position {
					t.Fatalf("expected policy rejection at statement %d with %d results, got %d: %v", position, position, len(results), err)
				}

				rows := run(t, m, h, `SELECT id,value FROM guard`).Rows

				if len(rows) != 1 || rows[0][0] != "1" || rows[0][1] != "original" {
					t.Fatalf("batch changed existing data: %v", rows)
				}

				row := run(t, m, h, `SELECT to_regclass('staged') IS NULL, to_regprocedure('staged_fn()') IS NULL, to_regclass('payload') IS NULL, to_regprocedure('payload()') IS NULL`).Rows[0]

				if !reflect.DeepEqual(row, []any{"t", "t", "t", "t"}) {
					t.Fatalf("batch left objects behind: %v", row)
				}

				if got := run(t, m, h, `SELECT is_called FROM guard_seq`).Rows[0][0]; got != "f" {
					t.Fatal("statement after policy rejection was executed")
				}

				var leaked int

				if err := m.pool.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relnamespace=$1::regnamespace AND relpersistence<>'p'`, before.schema).Scan(&leaked); err != nil {
					t.Fatal(err)
				}

				if leaked != 0 {
					t.Fatal("batch changed relation persistence")
				}

				if err := m.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_constraint WHERE connamespace=$1::regnamespace AND conname='payload') + (SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid WHERE c.relnamespace=$1::regnamespace AND p.polname='payload')`, before.schema).Scan(&leaked); err != nil {
					t.Fatal(err)
				}

				if leaked != 0 {
					t.Fatal("batch left constraints or policies behind")
				}

				after, err := m.lookup(ctx, h.ID)
				if err != nil {
					t.Fatal(err)
				}

				if !reflect.DeepEqual(before, after) {
					t.Fatal("policy rejection changed registry metadata")
				}

				if got := run(t, m, other, `SELECT value FROM guard`).Rows[0][0]; got != "other" {
					t.Fatal("policy rejection affected another Islet")
				}
			})
		}
	}
}

func TestPolicyInitializationRollback(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	h, err := m.CreateWithInitialization(ctx, []string{
		`CREATE TABLE staged(n int)`,
		`INSERT INTO staged VALUES(1)`,
		`SELECT pg_catalog.set_config('application_name','policy-probe',true)`,
	})

	if !errors.Is(err, pgislet.ErrPolicy) || !errors.Is(err, pgislet.ErrInitialization) || h.State != "failed" {
		t.Fatalf("expected failed initialization with policy error: state=%s: %v", h.State, err)
	}

	t.Cleanup(func() {
		if err := m.Delete(ctx, h); err != nil {
			t.Error(err)
		}
	})

	md, err := m.lookup(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}

	var objects int

	if err := m.pool.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relnamespace=$1::regnamespace`, md.schema).Scan(&objects); err != nil {
		t.Fatal(err)
	}

	if objects != 0 || md.token != "" {
		t.Fatal("failed initialization retained objects or an initialization token")
	}

	h, err = m.Reinitialize(ctx, h, []string{`CREATE TABLE restored(n int)`, `INSERT INTO restored VALUES(7)`})
	if err != nil {
		t.Fatal(err)
	}

	if got := run(t, m, h, `SELECT n FROM restored`).Rows[0][0]; got != "7" {
		t.Fatal(got)
	}
}

func TestPolicyLocalFunctionBatchLifecycle(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	h := islet(t, m)
	results, err := m.Batch(ctx, h, []string{
		`CREATE FUNCTION calculate(n int) RETURNS int LANGUAGE sql RETURN n+1`,
		`SELECT calculate(3)`,
		`CREATE OR REPLACE FUNCTION calculate(n int) RETURNS int LANGUAGE sql AS $$ SELECT n+2 $$`,
		`SELECT calculate(3)`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 4 || results[1].Rows[0][0] != "4" || results[3].Rows[0][0] != "5" {
		t.Fatalf("function definitions were not visible within the batch: %v", results)
	}

	_, err = m.Batch(ctx, h, []string{
		`DROP FUNCTION calculate(int)`,
		`SELECT calculate(3)`,
	})
	var pe *pgislet.Error

	if !errors.Is(err, pgislet.ErrPolicy) || !errors.As(err, &pe) || pe.Statement != 1 {
		t.Fatalf("dropped function remained allowed: %v", err)
	}

	if got := run(t, m, h, `SELECT calculate(3)`).Rows[0][0]; got != "5" {
		t.Fatal("failed batch did not restore the function")
	}

	_, err = m.Batch(ctx, h, []string{
		`CREATE OR REPLACE FUNCTION calculate(n int) RETURNS int LANGUAGE sql RETURN n+10`,
		`SELECT set_config('application_name','probe',true)`,
	})

	if !errors.Is(err, pgislet.ErrPolicy) {
		t.Fatal(err)
	}

	if got := run(t, m, h, `SELECT calculate(3)`).Rows[0][0]; got != "5" {
		t.Fatal("failed batch retained a replacement function body")
	}

	results, err = m.Batch(ctx, h, []string{
		`DROP FUNCTION calculate(int)`,
		`CREATE FUNCTION calculate(n int) RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT n+3; END`,
		`SELECT calculate(3)`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if results[2].Rows[0][0] != "6" {
		t.Fatal("recreated function did not use its new body")
	}

	other := islet(t, m)

	if _, err := m.Execute(ctx, other, `SELECT calculate(3)`); !errors.Is(err, pgislet.ErrPolicy) {
		t.Fatalf("local function permissions leaked between Islets: %v", err)
	}
}
