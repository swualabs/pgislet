package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (m *Manager) query(ctx context.Context, tx pgx.Tx, conn *pgx.Conn, sql string, rowsLeft, bytesLeft *int) (result Result, err error) {
	start := time.Now()
	defer func() {
		result.Duration = time.Since(start)
	}()

	rows, err := tx.Query(ctx, sql, pgx.QueryExecModeExec, pgx.QueryResultFormats{pgx.TextFormatCode})
	if err != nil {
		return result, err
	}

	defer rows.Close()

	for _, field := range rows.FieldDescriptions() {
		*bytesLeft -= len(field.Name) + 4
		result.Columns = append(result.Columns, Column{Name: field.Name, DataTypeOID: field.DataTypeOID})
	}

	for rows.Next() {
		raw := rows.RawValues()
		size := 0

		for _, value := range raw {
			size += len(value) + 1
		}

		if *rowsLeft <= 0 || size > *bytesLeft {
			result.Truncated = true
			closeRuntime(conn)
			return result, wrap(ErrResultLimit, fmt.Errorf("batch row or byte budget exceeded; transaction rolled back"))
		}

		values := make([]any, len(raw))

		for i, value := range raw {
			if value != nil {
				values[i] = string(value)
			}
		}

		result.Rows = append(result.Rows, values)
		*rowsLeft--
		*bytesLeft -= size
	}

	if err = rows.Err(); err != nil {
		return result, err
	}

	if *bytesLeft < 0 {
		result.Truncated = true
		return result, ErrResultLimit
	}

	result.CommandTag = rows.CommandTag().String()
	result.RowsAffected = rows.CommandTag().RowsAffected()
	return result, nil
}
