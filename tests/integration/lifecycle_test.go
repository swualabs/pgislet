package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/swualabs/pgislet"
)

func TestLifecycle(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	h := islet(t, m)
	run(t, m, h, `CREATE TABLE seed(n int)`)
	run(t, m, h, `CREATE TABLE custom(n int)`)
	run(t, m, h, `DROP TABLE seed`)
	old := h

	var err error
	h, err = m.Reset(ctx, h)
	if err != nil {
		t.Fatal(err)
	}

	if h.Generation != old.Generation+1 {
		t.Fatal(h)
	}

	if got := run(t, m, h, `SELECT count(*) FROM pg_tables WHERE schemaname=current_schema()`).Rows[0][0]; got != "0" {
		t.Fatal(got)
	}

	if _, err = m.Execute(ctx, old, `SELECT 1`); !errors.Is(err, pgislet.ErrStaleGeneration) {
		t.Fatal(err)
	}

	h, err = m.Reinitialize(ctx, h, []string{`CREATE TABLE seed(n int)`, `INSERT INTO seed VALUES(7)`})
	if err != nil {
		t.Fatal(err)
	}

	if got := run(t, m, h, `SELECT n FROM seed`).Rows[0][0]; got != "7" {
		t.Fatal(got)
	}

	h, err = m.Reinitialize(ctx, h, []string{`CREATE TABLE partial(n int)`, `SELECT missing FROM partial`})
	if !errors.Is(err, pgislet.ErrInitialization) || h.State != "failed" {
		t.Fatalf("%+v %v", h, err)
	}

	if _, err = m.Execute(ctx, h, `SELECT 1`); !errors.Is(err, pgislet.ErrFailed) {
		t.Fatal(err)
	}

	h, err = m.Reinitialize(ctx, h, []string{`CREATE TABLE recovered(n int)`})
	if err != nil {
		t.Fatal(err)
	}

	run(t, m, h, `DROP TABLE recovered`)

	if err = m.Delete(ctx, h); err != nil {
		t.Fatal(err)
	}

	if err = m.Delete(ctx, h); err != nil {
		t.Fatal(err)
	}

	if _, err = m.Open(ctx, h.ID); !errors.Is(err, pgislet.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestInitializationRecovery(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	h, err := m.CreateWithInitialization(ctx, []string{`CREATE TABLE seed(n int)`})
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		latest, e := m.Open(ctx, h.ID)

		if e == nil {
			e = m.Delete(ctx, latest)
		}

		if e != nil {
			t.Error(e)
		}
	}()

	run(t, m, h, `ALTER TABLE seed ADD COLUMN name text`)
	run(t, m, h, `TRUNCATE seed`)
	run(t, m, h, `DROP TABLE seed`)
	pending, err := m.stageInitialization(ctx, h)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = m.Execute(ctx, pending, `SELECT 1`); !errors.Is(err, pgislet.ErrUnavailable) {
		t.Fatal(err)
	}

	if _, err = m.Reset(ctx, pending); !errors.Is(err, pgislet.ErrUnavailable) {
		t.Fatal(err)
	}

	if err = m.Delete(ctx, pending); !errors.Is(err, pgislet.ErrUnavailable) {
		t.Fatal(err)
	}

	oldMetadata, err := m.lookup(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}

	recovered, err := m.Recover(ctx, pending)
	if err != nil {
		t.Fatal(err)
	}

	if recovered.Generation <= pending.Generation || recovered.State != "active" {
		t.Fatal(recovered)
	}

	if err = m.enter(ctx, oldMetadata, oldMetadata.token); !hasCode(err, "PI002") {
		t.Fatal(err)
	}

	if _, err = m.Reset(ctx, pending); !errors.Is(err, pgislet.ErrStaleGeneration) {
		t.Fatal(err)
	}

	if err = m.Delete(ctx, pending); !errors.Is(err, pgislet.ErrStaleGeneration) {
		t.Fatal(err)
	}

	if _, err = m.Batch(ctx, pgislet.Islet{ID: "missing", Generation: 1}, nil); !errors.Is(err, pgislet.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestDeleteExternalOwnership(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	h := islet(t, m)
	md, _ := m.lookup(ctx, h.ID)
	run(t, m, h, `CREATE TABLE local_data(n int)`)

	if _, err := m.pool.Exec(ctx, `CREATE SCHEMA unexpected_owner AUTHORIZATION `+ident(md.role)); err != nil {
		t.Fatal(err)
	}

	defer func() {
		_, err := m.pool.Exec(ctx, `DROP SCHEMA unexpected_owner`)
		if err != nil {
			t.Error(err)
		}
	}()

	if err := m.Delete(ctx, h); !errors.Is(err, pgislet.ErrLifecycle) {
		t.Fatal(err)
	}

	current, err := m.Open(ctx, h.ID)
	if err != nil || current.State != "failed" {
		t.Fatalf("%+v %v", current, err)
	}

	var preserved bool
	err = m.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL AND EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='unexpected_owner')`, md.schema+".local_data").Scan(&preserved)
	if err != nil || !preserved {
		t.Fatalf("cleanup crossed boundary: %v", err)
	}
}

func TestExternalDependencies(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	h := islet(t, m)
	md, _ := m.lookup(ctx, h.ID)
	run(t, m, h, `CREATE TABLE x(n int)`)
	_, err := m.pool.Exec(ctx, `CREATE SCHEMA outside; CREATE VIEW outside.v AS SELECT * FROM `+ident(md.schema)+`.x`)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_, err := m.pool.Exec(ctx, `DROP SCHEMA outside CASCADE`)
		if err != nil {
			t.Error(err)
		}
	}()

	if _, err = m.Execute(ctx, h, `DROP TABLE x CASCADE`); !errors.Is(err, pgislet.ErrExternalDependency) {
		t.Fatal(err)
	}

	if _, err = m.Reset(ctx, h); !errors.Is(err, pgislet.ErrExternalDependency) {
		t.Fatal(err)
	}

	var exists bool

	if err = m.pool.QueryRow(ctx, `SELECT to_regclass('outside.v') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Fatalf("external view lost %v", err)
	}

	if err = m.Delete(ctx, h); !errors.Is(err, pgislet.ErrExternalDependency) {
		t.Fatal(err)
	}
}
