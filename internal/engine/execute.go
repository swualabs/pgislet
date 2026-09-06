package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/swualabs/pgislet/internal/policy"
)

func (m *Manager) Execute(ctx context.Context, h Islet, sql string) (Result, error) {
	results, err := m.Batch(ctx, h, []string{sql})

	if len(results) > 0 {
		return results[0], err
	}

	return Result{}, err
}

func (m *Manager) Batch(ctx context.Context, h Islet, statements []string) ([]Result, error) {
	ctx, cancel := context.WithTimeout(ctx, m.config.OperationTimeout)
	defer cancel()

	if err := m.checkSize(statements); err != nil {
		return nil, err
	}

	md, err := m.lookup(ctx, h.ID)
	if err != nil {
		return nil, err
	}

	if md.Generation != h.Generation {
		return nil, ErrStaleGeneration
	}

	return m.execute(ctx, md, statements, false)
}

func (m *Manager) checkSize(statements []string) error {
	if len(statements) > m.config.MaxBatchStatements {
		return wrap(ErrSQLTooLarge, fmt.Errorf("batch statement limit is %d", m.config.MaxBatchStatements))
	}

	size := 0

	for _, sql := range statements {
		if len(sql) > m.config.MaxSQLBytes-size {
			return ErrSQLTooLarge
		}

		size += len(sql)
	}

	return nil
}

func (m *Manager) execute(ctx context.Context, md metadata, statements []string, initializing bool) ([]Result, error) {
	ctx, cancel := context.WithTimeout(ctx, m.config.OperationTimeout)
	defer cancel()

	if err := m.checkSize(statements); err != nil {
		return nil, err
	}

	conn, err := m.connectRuntime(ctx, md)
	if err != nil {
		return nil, err
	}

	defer closeRuntime(conn)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, classify(err)
	}

	defer rollback(tx)

	var token any

	if initializing {
		token = md.token
	}

	if _, err = tx.Exec(ctx, `SELECT pgislet_api.enter($1,$2,$3)`, md.ID, md.Generation, token); err != nil {
		return nil, classify(err)
	}

	if err = checkDependencies(ctx, m.pool, md.schema); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(statements))
	rowsLeft, bytesLeft := m.config.MaxRows, m.config.MaxResultBytes

	for i, sql := range statements {
		local, err := m.localFunctions(ctx, tx, md.schema)
		if err != nil {
			return results, classify(err)
		}

		if err = policy.Check(sql, md.schema, local); err != nil {
			return results, &Error{Kind: ErrPolicy, Cause: err, Statement: i}
		}

		result, err := m.query(ctx, tx, conn, sql, &rowsLeft, &bytesLeft)
		results = append(results, result)

		if err != nil {
			mapped := classify(err)

			if e, ok := mapped.(*Error); ok {
				e.Statement = i
			}

			return results, mapped
		}
	}

	if initializing {
		if _, err = tx.Exec(ctx, `SELECT pgislet_api.finish($1,$2,$3)`, md.ID, md.Generation, md.token); err != nil {
			return results, classify(err)
		}
	}

	return results, commit(ctx, tx)
}

func (m *Manager) localFunctions(ctx context.Context, tx pgx.Tx, schema string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT proname FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=$1`, schema)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	names := map[string]bool{}

	for name := range m.reserved {
		names[name] = false
	}

	for rows.Next() {
		var name string

		if err = rows.Scan(&name); err != nil {
			return nil, err
		}

		if _, reserved := m.reserved[name]; !reserved {
			names[name] = true
		}
	}

	return names, rows.Err()
}
