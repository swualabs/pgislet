package policy

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	pgquery "github.com/pganalyze/pg_query_go/v6"
)

func TestPolicy(t *testing.T) {
	allowed := []string{
		`SELECT uuidv4(), uuidv7(), uuid_extract_version(uuidv7()), uuid_extract_timestamp(uuidv7())`,
		`CREATE TABLE generated(n int, doubled int GENERATED ALWAYS AS (n*2) VIRTUAL)`,
		`UPDATE users SET age=age+1 RETURNING WITH (OLD AS o, NEW AS n) o.age,n.age`,
		`CREATE TABLE periods(resource daterange, valid_at daterange, PRIMARY KEY(resource, valid_at WITHOUT OVERLAPS))`,
		`CREATE TABLE bookings(resource daterange, valid_at daterange, FOREIGN KEY(resource, PERIOD valid_at) REFERENCES periods(resource, PERIOD valid_at))`,
		`ALTER TABLE users SET LOGGED`, `ALTER SEQUENCE seq SET LOGGED`,
		`SELECT 1`, `WITH x AS (SELECT 1 AS n) SELECT sum(n) FROM x`,
		`CREATE TABLE users(id int PRIMARY KEY, name text)`, `ALTER TABLE users ADD COLUMN age int`,
		`DROP TABLE users CASCADE`, `CREATE INDEX x ON users(id)`, `DROP INDEX x`,
		`CREATE VIEW x AS SELECT * FROM users`, `CREATE MATERIALIZED VIEW x AS SELECT 1`,
		`CREATE SEQUENCE s`, `ALTER SEQUENCE s RESTART`, `CREATE TYPE mood AS ENUM ('happy')`,
		`CREATE TYPE pair AS (a int,b text)`, `CREATE DOMAIN positive AS int CHECK(VALUE>0)`,
		`CREATE FUNCTION inc(x int) RETURNS int LANGUAGE sql AS $$ SELECT x+1 $$`,
		`CREATE PROCEDURE insert_user() LANGUAGE sql AS $$ INSERT INTO users VALUES(1) $$`,
		`SELECT mine()`, `SELECT "sum"(n) FROM users`, `COMMENT ON TABLE users IS 'hello'`,
		`EXPLAIN (ANALYZE true) SELECT 1`, `ALTER TABLE users RENAME COLUMN name TO title`,
		`CREATE POLICY p ON users USING (id>0)`, `TRUNCATE users`,
	}

	for _, sql := range allowed {
		t.Run(sql, func(t *testing.T) {
			if err := Check(sql, "islet", map[string]bool{"mine": true}); err != nil {
				t.Fatal(err)
			}
		})
	}

	denied := []string{
		`ALTER TABLE users SET UNLOGGED`,
		`ALTER TABLE ONLY users SET UNLOGGED`,
		`ALTER TABLE IF EXISTS users SET UNLOGGED`,
		`ALTER SEQUENCE seq SET UNLOGGED`,
		`CREATE TABLE generated(n text GENERATED ALWAYS AS (pg_read_file('/etc/passwd')) VIRTUAL)`,
		`UPDATE users SET age=1 RETURNING WITH (OLD AS o, NEW AS n) pg_read_file('/etc/passwd')`,
		`SELECT uuidv7(public.sneaky())`,
		``, `SELECT 1; SELECT 2`, `CoMmIt`, `/* a */ SET /*b*/ ROLE postgres`,
		`CREATE ROLE x`, `CREATE DATABASE x`, `CREATE SCHEMA x`, `DROP SCHEMA islet CASCADE`,
		`ALTER SCHEMA islet RENAME TO x`, `GRANT SELECT ON users TO PUBLIC`,
		`ALTER DEFAULT PRIVILEGES GRANT ALL ON TABLES TO PUBLIC`, `ALTER SYSTEM SET work_mem='1GB'`,
		`COPY users TO PROGRAM 'id'`, `CREATE EXTENSION hstore`, `CREATE PUBLICATION x FOR ALL TABLES`,
		`SELECT pg_catalog.pg_advisory_lock(1)`, `WITH a AS (SELECT pg_notify('x','y')) SELECT * FROM a`,
		`SELECT (SELECT "pg_catalog"."set_config"('statement_timeout','0',false))`,
		`SELECT lo_create(0)`, `SELECT query_to_xml('COMMIT',false,false,'')`,
		`CREATE FUNCTION evil() RETURNS text LANGUAGE sql AS $$ SELECT pg_read_file('/etc/passwd') $$`,
		`CREATE FUNCTION evil() RETURNS void LANGUAGE plpgsql AS $$ BEGIN EXECUTE 'COMMIT'; END $$`,
		`CREATE FUNCTION evil() RETURNS int LANGUAGE sql SET statement_timeout='0' AS $$ SELECT 1 $$`,
		`CREATE FUNCTION evil() RETURNS void LANGUAGE sql AS $$ GRANT ALL ON users TO PUBLIC $$`,
		`SELECT pgislet_api.enter('x',1)`, `CREATE TEMP TABLE x(n int)`,
		`ALTER TABLE users OWNER TO postgres`, `ALTER TABLE users SET SCHEMA public`,
		`CREATE INDEX CONCURRENTLY x ON users(id)`, `SELECT public.dblink('x','y')`,
		`CREATE FUNCTION public.x() RETURNS int LANGUAGE sql AS $$SELECT 1$$`,
		`CREATE CAST (int AS text) WITH INOUT`, `DO $$BEGIN END$$`,
	}

	for _, sql := range denied {
		t.Run(sql, func(t *testing.T) {
			if err := Check(sql, "islet", nil); err == nil {
				t.Fatal("accepted prohibited SQL")
			}
		})
	}
}

var expressionContexts = []struct {
	name string
	sql  string
}{
	{"select", "SELECT %s"},
	{"subquery", "SELECT (SELECT %s)"},
	{"cte", "WITH payload AS (SELECT %s AS value) SELECT * FROM payload"},
	{"case", "SELECT CASE WHEN false THEN %s ELSE 'safe' END"},
	{"insert", "INSERT INTO guard(value) VALUES (%s)"},
	{"update", "UPDATE guard SET value=%s"},
	{"delete returning", "DELETE FROM guard RETURNING %s"},
	{"default", "CREATE TABLE payload(value text DEFAULT %s)"},
	{"generated stored", "CREATE TABLE payload(n int, value text GENERATED ALWAYS AS (%s) STORED)"},
	{"generated virtual", "CREATE TABLE payload(n int, value text GENERATED ALWAYS AS (%s) VIRTUAL)"},
	{"check", "ALTER TABLE guard ADD CONSTRAINT payload CHECK (%s IS NOT NULL)"},
	{"domain", "CREATE DOMAIN payload AS text CHECK (%s IS NOT NULL)"},
	{"view", "CREATE VIEW payload AS SELECT %s AS value"},
	{"materialized view", "CREATE MATERIALIZED VIEW payload AS SELECT %s AS value"},
	{"index", "CREATE INDEX payload ON guard ((%s))"},
	{"policy", "CREATE POLICY payload ON guard USING (%s IS NOT NULL)"},
	{"function text", "CREATE FUNCTION payload() RETURNS text LANGUAGE sql AS $body$ SELECT %s $body$"},
	{"function atomic", "CREATE FUNCTION payload() RETURNS text LANGUAGE sql BEGIN ATOMIC SELECT %s; END"},
	{"function return", "CREATE FUNCTION payload() RETURNS text LANGUAGE sql RETURN %s"},
	{"function default", "CREATE FUNCTION payload(n text DEFAULT %s) RETURNS text LANGUAGE sql RETURN n"},
	{"explain", "EXPLAIN (ANALYZE true) SELECT %s"},
	{"returning aliases", "UPDATE guard SET value='changed' RETURNING WITH (OLD AS o, NEW AS n) %s"},
	{"merge", "MERGE INTO guard g USING (VALUES(1)) s(id) ON g.id=s.id WHEN MATCHED THEN UPDATE SET value=%s"},
}

var prohibitedCalls = []struct {
	namespace string
	name      string
	args      string
}{
	{"pg_catalog", "set_config", "('application_name','policy-probe',true)"},
	{"pg_catalog", "pg_notify", "('policy_probe','message')"},
	{"pg_catalog", "pg_read_file", "('policy_probe_missing_file')"},
	{"pg_catalog", "pg_read_binary_file", "('policy_probe_missing_file')"},
	{"pg_catalog", "query_to_xml", "('SELECT 1',false,false,'')"},
	{"external", "mine", "()"},
	{"external", "lower", "('SAFE')"},
	{"pgislet_api", "enter", "('probe',1,NULL)"},
	{"pgislet_api", "finish", "('probe',1,NULL)"},
}

func policyExpression(allowed bool, spelling uint8) string {
	call := prohibitedCalls[int(spelling/4)%len(prohibitedCalls)]
	namespace, name, args := call.namespace, call.name, call.args

	if allowed {
		namespace, name = "pg_catalog", "lower"
		args = "('SAFE')"
	}

	switch spelling % 4 {
	case 0:
		if namespace == "pg_catalog" {
			return name + args
		}

		return namespace + "." + name + args
	case 1:
		return strings.ToUpper(namespace) + "." + strings.ToUpper(name) + args
	case 2:
		return `"` + namespace + `"."` + name + `"` + args
	default:
		return namespace + ` /* outer /* nested */ comment */ . U&"` + fmt.Sprintf(`\%04x`, name[0]) + name[1:] + `"` + args
	}
}

func checkPolicyExpression(t *testing.T, context int, spelling uint8, depth uint8) {
	t.Helper()

	for _, allowed := range []bool{true, false} {
		expression := policyExpression(allowed, spelling)

		for range int(depth % 8) {
			expression = "coalesce(" + expression + ", 'safe')"
		}

		sql := fmt.Sprintf(expressionContexts[context].sql, expression)

		if _, err := pgquery.ParseToJSON(sql); err != nil {
			t.Fatalf("invalid test SQL: %s: %v", sql, err)
		}

		err := Check(sql, "islet", map[string]bool{"lower": false, "set_config": false, "mine": true})

		if (err == nil) != allowed {
			t.Fatalf("allowed=%t: %s: %v", allowed, sql, err)
		}
	}
}

func TestPolicyExpressionContexts(t *testing.T) {
	for i, context := range expressionContexts {
		t.Run(context.name, func(t *testing.T) {
			for spelling := range uint8(4 * len(prohibitedCalls)) {
				checkPolicyExpression(t, i, spelling, 2)
			}
		})
	}
}

func FuzzPolicyExpressions(f *testing.F) {
	for i := range expressionContexts {
		for spelling := range uint8(4 * len(prohibitedCalls)) {
			f.Add(uint8(i), spelling, uint8(0))
		}
	}

	f.Fuzz(func(t *testing.T, context, spelling, depth uint8) {
		checkPolicyExpression(t, int(context)%len(expressionContexts), spelling, depth)
	})
}

func FuzzPolicySQL(f *testing.F) {
	for _, sql := range []string{
		"ALTER TABLE guard SET UNLOGGED", "ALTER SEQUENCE guard_seq SET UNLOGGED",
		"SELECT 1", "", "SELECT '", "SELECT 1; COMMIT", "SELECT 1\x00; COMMIT",
		"SELECT \xff", "/* nested /* comment */ */ SELECT 1", `SELECT "MiXeD"()`,
		`CREATE FUNCTION mine(n int) RETURNS int LANGUAGE sql RETURN n+1`,
		`CREATE FUNCTION mine() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$`,
		`CREATE FUNCTION mine() RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT 1; END`,
		`SELECT U&"pg_\006eotify"('a','b')`,
		`SELECT '; SET ROLE postgres'`,
	} {
		f.Add(sql)
	}

	f.Fuzz(func(t *testing.T, sql string) {
		if len(sql) > 32<<10 {
			return
		}

		local := map[string]bool{"mine": true, "set_config": false, "pg_notify": false}
		before := maps.Clone(local)
		first := Check(sql, "islet", local)
		second := Check(sql, "islet", local)

		if (first == nil) != (second == nil) {
			t.Fatalf("nondeterministic policy decision: %q: %v / %v", sql, first, second)
		}

		if !maps.Equal(local, before) {
			t.Fatalf("policy mutated caller function permissions: %q", sql)
		}
	})
}

func TestPolicyNestedFunctionBodies(t *testing.T) {
	for _, test := range []struct {
		name    string
		body    string
		depth   int
		allowed bool
	}{
		{"depth limit", "SELECT 1", 16, true},
		{"excessive depth", "SELECT 1", 17, false},
		{"nested forbidden call", "SELECT pg_catalog.set_config('application_name','probe',true)", 8, false},
		{"nested transaction", "COMMIT", 8, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sql := test.body

			for i := range test.depth {
				sql = fmt.Sprintf("CREATE FUNCTION nested_%d() RETURNS int LANGUAGE sql AS $fn%d$ %s; SELECT 1 $fn%d$", i, i, sql, i)
			}

			err := Check(sql, "islet", map[string]bool{"set_config": false})

			if (err == nil) != test.allowed {
				t.Fatalf("allowed=%t: %v", test.allowed, err)
			}
		})
	}
}
