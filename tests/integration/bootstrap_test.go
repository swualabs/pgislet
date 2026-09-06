package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/swualabs/pgislet"
)

func TestConcurrentBootstrap(t *testing.T) {
	admin := manager(t, pgislet.Config{})
	ctx := context.Background()

	if _, err := admin.pool.Exec(ctx, `CREATE DATABASE bootstrap_concurrency`); err != nil {
		t.Fatal(err)
	}

	defer func() {
		_, err := admin.pool.Exec(ctx, `DROP DATABASE bootstrap_concurrency`)
		if err != nil {
			t.Error(err)
		}
	}()

	cfg, err := pgx.ParseConfig(testDSN)
	if err != nil {
		t.Fatal(err)
	}

	dsn := fmt.Sprintf("host=%s port=%d user=postgres password=integration-secret dbname=bootstrap_concurrency sslmode=disable", cfg.Host, cfg.Port)

	var wg sync.WaitGroup
	errs := make(chan error, 4)

	for range 4 {
		wg.Go(func() {
			node, err := pgislet.New(ctx, pgislet.Config{DSN: dsn})
			if err == nil {
				node.Close()
			}

			errs <- err
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestBootstrapValidation(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})

	if _, err := m.pool.Exec(ctx, `UPDATE pgislet_internal.version SET version=99`); err != nil {
		t.Fatal(err)
	}

	node, err := pgislet.New(ctx, pgislet.Config{DSN: testDSN})

	if node != nil {
		node.Close()
	}

	if _, e := m.pool.Exec(ctx, `UPDATE pgislet_internal.version SET version=1`); e != nil {
		t.Fatal(e)
	}

	if !errors.Is(err, pgislet.ErrUnsupported) {
		t.Fatal(err)
	}

	if _, err = m.pool.Exec(ctx, `CREATE SCHEMA pgtenant; GRANT USAGE ON SCHEMA pgtenant TO PUBLIC`); err != nil {
		t.Fatal(err)
	}

	node, err = pgislet.New(ctx, pgislet.Config{DSN: testDSN})

	if node != nil {
		node.Close()
	}

	if _, e := m.pool.Exec(ctx, `DROP SCHEMA pgtenant`); e != nil {
		t.Fatal(e)
	}

	if !errors.Is(err, pgislet.ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestNonSuperuserManagement(t *testing.T) {
	ctx := context.Background()
	admin := manager(t, pgislet.Config{})

	if _, err := admin.pool.Exec(ctx, `CREATE ROLE limited_manager LOGIN CREATEROLE PASSWORD 'limited-secret'`); err != nil {
		t.Fatal(err)
	}

	defer func() {
		_, err := admin.pool.Exec(ctx, `DROP ROLE limited_manager`)
		if err != nil {
			t.Error(err)
		}
	}()

	if _, err := admin.pool.Exec(ctx, `CREATE DATABASE limited_db OWNER limited_manager`); err != nil {
		t.Fatal(err)
	}

	defer func() {
		_, err := admin.pool.Exec(ctx, `DROP DATABASE limited_db`)
		if err != nil {
			t.Error(err)
		}
	}()

	cfg, err := pgx.ParseConfig(testDSN)
	if err != nil {
		t.Fatal(err)
	}

	dsn := fmt.Sprintf("host=%s port=%d user=limited_manager password=limited-secret dbname=limited_db sslmode=disable", cfg.Host, cfg.Port)
	node, err := pgislet.New(ctx, pgislet.Config{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}

	defer node.Close()

	h, err := node.CreateWithInitialization(ctx, []string{`CREATE TABLE x(n int)`})
	if err != nil {
		t.Fatal(err)
	}

	run(t, node, h, `INSERT INTO x VALUES(1)`)
	h, err = node.Reset(ctx, h)
	if err != nil {
		t.Fatal(err)
	}

	if err = node.Delete(ctx, h); err != nil {
		t.Fatal(err)
	}
}
