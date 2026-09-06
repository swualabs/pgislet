package policy

import "testing"

func TestPolicy(t *testing.T) {
	allowed := []string{
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
