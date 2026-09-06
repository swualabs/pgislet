package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Manager struct {
	pool     *pgxpool.Pool
	runtime  *pgx.ConnConfig
	config   Config
	reserved map[string]bool
}

func New(ctx context.Context, cfg Config) (*Manager, error) {
	cfg, err := cfg.normalized()
	if err != nil {
		return nil, err
	}

	pc, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, err
	}

	pc.ConnConfig.RuntimeParams["search_path"] = "pg_catalog"
	pc.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprint(cfg.OperationTimeout.Milliseconds())
	pc.ConnConfig.RuntimeParams["lock_timeout"] = fmt.Sprint(cfg.LockTimeout.Milliseconds())
	pc.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = fmt.Sprint(cfg.IdleTransactionTimeout.Milliseconds())
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, err
	}

	m := &Manager{pool: pool, runtime: pc.ConnConfig.Copy(), config: cfg}
	opctx, cancel := context.WithTimeout(ctx, cfg.OperationTimeout)
	defer cancel()

	if err = m.bootstrap(opctx); err != nil {
		pool.Close()
		return nil, err
	}

	return m, nil
}

func (m *Manager) Close() {
	m.pool.Close()
}

func (m *Manager) Open(ctx context.Context, id string) (Islet, error) {
	md, err := m.lookup(ctx, id)
	return md.Islet, err
}
