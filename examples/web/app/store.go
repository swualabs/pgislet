package app

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

type Store struct {
	DB *bun.DB
}

func OpenStore(ctx context.Context, dsn string) (*Store, error) {
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	raw.SetMaxOpenConns(16)
	raw.SetMaxIdleConns(4)
	raw.SetConnMaxLifetime(30 * time.Minute)
	store := &Store{DB: bun.NewDB(raw, pgdialect.New())}

	if err := store.DB.PingContext(ctx); err != nil {
		_ = store.DB.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(722031994)`); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS web_schema_versions(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return err
		}

		var version int

		if err := tx.NewRaw(`SELECT COALESCE(max(version),0) FROM web_schema_versions`).Scan(ctx, &version); err != nil {
			return err
		}

		if version > 1 {
			return fmt.Errorf("unsupported web database schema version: %d", version)
		}

		if version == 1 {
			return nil
		}

		for _, statement := range []string{
			`CREATE TABLE accounts(id text PRIMARY KEY, email text NOT NULL UNIQUE, name text NOT NULL, password_hash text NOT NULL, workspace_id text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now())`,
			`CREATE UNIQUE INDEX accounts_workspace ON accounts(workspace_id) WHERE workspace_id<>''`,
			`CREATE TABLE sessions(token_hash text PRIMARY KEY, account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now())`,
			`CREATE INDEX sessions_account ON sessions(account_id)`,
			`CREATE INDEX sessions_expiry ON sessions(expires_at)`,
			`CREATE TABLE auth_limits(key text PRIMARY KEY, requests integer NOT NULL, window_start timestamptz NOT NULL)`,
			`INSERT INTO web_schema_versions(version) VALUES(1)`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}

		return nil
	})
}

func (s *Store) Cleanup(ctx context.Context) error {
	if _, err := s.DB.NewDelete().Model((*Session)(nil)).Where("expires_at <= now()").Exec(ctx); err != nil {
		return err
	}

	_, err := s.DB.ExecContext(ctx, `DELETE FROM auth_limits WHERE window_start < now()-interval '1 hour'`)
	return err
}

func (s *Store) CheckIsolation(ctx context.Context, isletDSN string) error {
	conn, err := pgx.Connect(ctx, isletDSN)
	if err != nil {
		return fmt.Errorf("workspace database connection failed")
	}

	defer conn.Close(ctx)

	var appName, isletName string
	var appStarted, isletStarted time.Time
	query := `SELECT current_database(), pg_postmaster_start_time()`

	if err := s.DB.NewRaw(query).Scan(ctx, &appName, &appStarted); err != nil {
		return err
	}

	if err := conn.QueryRow(ctx, query).Scan(&isletName, &isletStarted); err != nil {
		return err
	}

	if appName == isletName && appStarted.Equal(isletStarted) {
		return fmt.Errorf("application and workspace connections resolve to the same database")
	}

	return nil
}
