package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/swualabs/pgislet"
)

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
