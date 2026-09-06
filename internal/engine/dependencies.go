package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func checkDependencies(ctx context.Context, q queryer, schema string) error {
	var object string
	err := q.QueryRow(ctx, dependencySQL, schema).Scan(&object)
	if err == pgx.ErrNoRows {
		return nil
	}

	if err != nil {
		return err
	}

	return wrap(ErrExternalDependency, fmt.Errorf("cleanup would affect %s", object))
}

const dependencySQL = `WITH RECURSIVE edges AS (
 SELECT refclassid AS source_class,refobjid AS source_id,classid AS target_class,objid AS target_id FROM pg_catalog.pg_depend
 UNION
 SELECT classid,objid,refclassid,refobjid FROM pg_catalog.pg_depend WHERE deptype='i'
), affected(classid,objid) AS (
 SELECT 'pg_catalog.pg_namespace'::regclass::oid,oid FROM pg_catalog.pg_namespace WHERE nspname=$1
 UNION
 SELECT e.target_class,e.target_id FROM affected a JOIN edges e ON e.source_class=a.classid AND e.source_id=a.objid
)
SELECT i.identity FROM affected a CROSS JOIN LATERAL pg_catalog.pg_identify_object(a.classid,a.objid,0) i
WHERE (i.schema IS NOT NULL AND i.schema<>$1 AND i.schema NOT LIKE 'pg_toast%')
OR (i.schema IS NULL AND a.classid NOT IN ('pg_namespace'::regclass,'pg_attrdef'::regclass,'pg_rewrite'::regclass,'pg_trigger'::regclass,'pg_policy'::regclass,'pg_constraint'::regclass))
LIMIT 1`
