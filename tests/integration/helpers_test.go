package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/swualabs/pgislet"
)

func islet(t *testing.T, m *fixture) pgislet.Islet {
	t.Helper()
	h, err := m.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		h, err := m.Open(context.Background(), h.ID)
		if errors.Is(err, pgislet.ErrNotFound) {
			return
		}

		if err != nil {
			t.Error(err)
			return
		}

		if h.State == "initializing" {
			h, err = m.Recover(context.Background(), h)
			if err != nil {
				t.Error(err)
				return
			}
		}

		if err = m.Delete(context.Background(), h); err != nil {
			t.Error(err)
		}
	})
	return h
}

type sqlExecutor interface {
	Execute(context.Context, pgislet.Islet, string) (pgislet.Result, error)
}

func run(t *testing.T, m sqlExecutor, h pgislet.Islet, sql string) pgislet.Result {
	t.Helper()
	r, err := m.Execute(context.Background(), h, sql)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}

	return r
}

type fixture struct {
	*pgislet.Manager
	pool *pgxpool.Pool
}

type metadata struct {
	pgislet.Islet
	Generation int64
	schema     string
	role       string
	password   string
	token      string
}

func manager(t *testing.T, cfg pgislet.Config) *fixture {
	t.Helper()

	if testDSN == "" {
		t.Skip("integration disabled by PGISLET_UNIT_ONLY")
	}

	cfg.DSN = testDSN
	node, err := pgislet.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(node.Close)
	pool, err := pgxpool.New(context.Background(), testDSN)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)
	return &fixture{Manager: node, pool: pool}
}

func (f *fixture) lookup(ctx context.Context, id string) (metadata, error) {
	var md metadata

	err := f.pool.QueryRow(ctx, `SELECT id,generation,state,schema_name,role_name,password,COALESCE(init_token,'') FROM pgislet_internal.islets WHERE id=$1`, id).Scan(&md.ID, &md.Generation, &md.State, &md.schema, &md.role, &md.password, &md.token)
	return md, err
}

func (f *fixture) connectRuntime(ctx context.Context, md metadata) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(testDSN)
	if err != nil {
		return nil, err
	}

	cfg.User = md.role
	cfg.Password = md.password
	cfg.RuntimeParams = map[string]string{"search_path": ident(md.schema) + ",pg_catalog"}
	return pgx.ConnectConfig(ctx, cfg)
}

func (f *fixture) enter(ctx context.Context, md metadata, token any) error {
	conn, err := f.connectRuntime(ctx, md)
	if err != nil {
		return err
	}

	defer closeRuntime(conn)

	_, err = conn.Exec(ctx, `SELECT pgislet_api.enter($1,$2,$3)`, md.ID, md.Generation, token)
	return err
}

func (f *fixture) stageInitialization(ctx context.Context, h pgislet.Islet) (pgislet.Islet, error) {
	_, err := f.pool.Exec(ctx, `UPDATE pgislet_internal.islets SET state='initializing',generation=generation+1,init_token='abandoned-initialization-fixture' WHERE id=$1`, h.ID)
	if err != nil {
		return h, err
	}

	return f.Open(ctx, h.ID)
}

func hasCode(err error, code string) bool {
	var pe *pgconn.PgError

	return errors.As(err, &pe) && pe.Code == code
}

func ident(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

func closeRuntime(conn *pgx.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = conn.Close(ctx)
}
