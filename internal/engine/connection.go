package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = tx.Rollback(ctx)
}

func closeRuntime(conn *pgx.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = conn.Close(ctx)
}

func commit(ctx context.Context, tx pgx.Tx) error {
	err := tx.Commit(ctx)
	if err == nil {
		return nil
	}

	if pgconnError(err) {
		return classify(err)
	}

	return wrap(ErrOutcomeUnknown, err)
}

func (m *Manager) connectRuntime(ctx context.Context, md metadata) (*pgx.Conn, error) {
	cfg := m.runtimeConfig(md)
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, wrap(ErrRuntimeConnection, classify(err))
	}

	return conn, nil
}

func (m *Manager) runtimeConfig(md metadata) *pgx.ConnConfig {
	cfg := m.runtime.Copy()
	cfg.User = md.role
	cfg.Password = md.password
	cfg.RuntimeParams = map[string]string{
		"search_path":                         ident(md.schema) + ",pg_catalog",
		"standard_conforming_strings":         "on",
		"statement_timeout":                   fmt.Sprint(m.config.StatementTimeout.Milliseconds()),
		"lock_timeout":                        fmt.Sprint(m.config.LockTimeout.Milliseconds()),
		"idle_in_transaction_session_timeout": fmt.Sprint(m.config.IdleTransactionTimeout.Milliseconds()),
		"application_name":                    "pgislet-runtime",
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeExec

	return cfg
}
