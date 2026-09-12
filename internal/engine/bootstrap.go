package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (m *Manager) bootstrap(ctx context.Context) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return wrap(ErrUnsupported, err)
	}

	defer rollback(tx)

	var version int

	var database string

	if err = tx.QueryRow(ctx, `SELECT current_setting('server_version_num')::int,current_database()`).Scan(&version, &database); err != nil {
		return wrap(ErrUnsupported, err)
	}

	if version < 170000 || version >= 190000 {
		return wrap(ErrUnsupported, fmt.Errorf("PostgreSQL 17 or 18 required, got %d", version))
	}

	if _, err = tx.Exec(ctx, `SELECT pg_catalog.pg_advisory_xact_lock(719326448107)`); err != nil {
		return err
	}

	if _, err = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS pgislet_internal; CREATE SCHEMA IF NOT EXISTS pgislet_api`); err != nil {
		return wrap(ErrUnsupported, err)
	}

	var owned bool

	if err = tx.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(nspowner=current_user::regrole) FROM pg_catalog.pg_namespace WHERE nspname IN ('pgislet_internal','pgislet_api')`).Scan(&owned); err != nil {
		return err
	}

	if !owned {
		return wrap(ErrUnsupported, fmt.Errorf("internal schemas must belong to management identity"))
	}

	if _, err = tx.Exec(ctx, `REVOKE ALL ON SCHEMA pgislet_internal,pgislet_api FROM PUBLIC;
 CREATE TABLE IF NOT EXISTS pgislet_internal.version (singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), version integer NOT NULL);
 INSERT INTO pgislet_internal.version VALUES(true,1) ON CONFLICT DO NOTHING`); err != nil {
		return err
	}

	if err = tx.QueryRow(ctx, `SELECT version FROM pgislet_internal.version`).Scan(&version); err != nil {
		return err
	}

	if version != 1 {
		return wrap(ErrUnsupported, fmt.Errorf("unsupported internal schema version %d", version))
	}

	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS pgislet_internal.islets (
 id text PRIMARY KEY, schema_name text UNIQUE NOT NULL, role_name text UNIQUE NOT NULL,
 password text NOT NULL, state text NOT NULL CHECK(state IN ('active','initializing','failed','deleting')),
 generation bigint NOT NULL CHECK(generation>0), init_token text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp());
 REVOKE ALL ON ALL TABLES IN SCHEMA pgislet_internal FROM PUBLIC;
 REVOKE ALL ON SCHEMA public FROM PUBLIC;`); err != nil {
		return err
	}

	if _, err = tx.Exec(ctx, `REVOKE CREATE,TEMPORARY ON DATABASE `+ident(database)+` FROM PUBLIC`); err != nil {
		return err
	}

	var unsafe string
	err = tx.QueryRow(ctx, `SELECT nspname FROM pg_catalog.pg_namespace n CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(n.nspacl,pg_catalog.acldefault('n',n.nspowner))) a WHERE left(nspname,3)<>'pg_' AND nspname NOT IN ('information_schema','public') AND a.grantee=0 AND a.privilege_type IN ('USAGE','CREATE') LIMIT 1`).Scan(&unsafe)
	if err == nil {
		return wrap(ErrUnsupported, fmt.Errorf("schema %q grants PUBLIC access", unsafe))
	}

	if err != pgx.ErrNoRows {
		return err
	}

	if _, err = tx.Exec(ctx, gatewaySQL+finishSQL); err != nil {
		return err
	}

	rows, err := tx.Query(ctx, `SELECT DISTINCT proname FROM pg_catalog.pg_proc WHERE pronamespace='pg_catalog'::regnamespace`)
	if err != nil {
		return err
	}

	m.reserved = map[string]bool{}

	for rows.Next() {
		var name string

		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}

		m.reserved[name] = false
	}

	rows.Close()

	if err = rows.Err(); err != nil {
		return err
	}

	return commit(ctx, tx)
}

const gatewaySQL = `CREATE OR REPLACE FUNCTION pgislet_api.enter(p_id text,p_generation bigint,p_token text DEFAULT NULL)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pgislet_internal AS $gateway$
DECLARE r pgislet_internal.islets%ROWTYPE;
BEGIN
 SELECT * INTO r FROM pgislet_internal.islets WHERE id=p_id;
 IF NOT FOUND OR session_user<>r.role_name THEN RAISE EXCEPTION 'islet not found' USING ERRCODE='PI001'; END IF;
 SELECT * INTO r FROM pgislet_internal.islets WHERE id=p_id FOR UPDATE NOWAIT;
 IF NOT FOUND OR session_user<>r.role_name THEN RAISE EXCEPTION 'islet not found' USING ERRCODE='PI001'; END IF;
 IF r.generation<>p_generation THEN RAISE EXCEPTION 'stale generation' USING ERRCODE='PI002'; END IF;
 IF r.state='failed' THEN RAISE EXCEPTION 'islet failed' USING ERRCODE='PI004'; END IF;
 IF ((r.state='active' AND p_token IS NULL) OR (r.state='initializing' AND p_token IS NOT NULL AND r.init_token=p_token)) IS NOT TRUE THEN
 RAISE EXCEPTION 'islet unavailable' USING ERRCODE='PI003'; END IF;
END $gateway$;
REVOKE ALL ON FUNCTION pgislet_api.enter(text,bigint,text) FROM PUBLIC;`

const finishSQL = `CREATE OR REPLACE FUNCTION pgislet_api.finish(p_id text,p_generation bigint,p_token text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pgislet_internal AS $gateway$
BEGIN
 IF p_token IS NULL THEN RAISE EXCEPTION 'initialization token required' USING ERRCODE='PI003'; END IF;
 PERFORM pgislet_api.enter(p_id,p_generation,p_token);
 UPDATE pgislet_internal.islets SET state='active',init_token=NULL,updated_at=clock_timestamp() WHERE id=p_id;
END $gateway$;
REVOKE ALL ON FUNCTION pgislet_api.finish(text,bigint,text) FROM PUBLIC;`
