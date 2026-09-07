package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/swualabs/pgislet"
)

func Run(ctx context.Context, dsn string, output io.Writer) error {
	manager, err := pgislet.New(ctx, pgislet.Config{DSN: dsn})
	if err != nil {
		return err
	}

	defer manager.Close()

	node, err := pgislet.New(ctx, pgislet.Config{DSN: dsn})
	if err != nil {
		return err
	}

	defer node.Close()

	seed := []string{`CREATE TABLE learners(id int PRIMARY KEY,name text)`, `INSERT INTO learners VALUES(1,'Alice'),(2,'Bob')`}
	a, err := manager.CreateWithInitialization(ctx, seed)
	if err != nil {
		return err
	}

	defer cleanup(manager, a.ID, output)

	b, err := manager.Create(ctx)
	if err != nil {
		return err
	}

	defer cleanup(manager, b.ID, output)

	result, err := node.Execute(ctx, a, `SELECT name FROM learners ORDER BY id`)
	if err != nil {
		return err
	}

	fmt.Fprintf(output, "Seed queried from another application node: %v\n", result.Rows)

	if _, err = manager.Execute(ctx, a, `DROP TABLE learners`); err != nil {
		return err
	}

	fmt.Fprintln(output, "The runtime role dropped its seed table.")
	schema, err := manager.Execute(ctx, b, `SELECT current_schema()`)
	if err != nil {
		return err
	}

	if _, err = manager.Execute(ctx, b, `CREATE TABLE private_data(n int)`); err != nil {
		return err
	}

	_, err = manager.Execute(ctx, a, fmt.Sprintf(`SELECT * FROM %s.private_data`, schema.Rows[0][0]))
	if !errors.Is(err, pgislet.ErrQuery) {
		return fmt.Errorf("expected PostgreSQL isolation error, got %v", err)
	}

	fmt.Fprintln(output, "Cross-islet access was denied by PostgreSQL.")
	old := a
	a, err = manager.Reset(ctx, a)
	if err != nil {
		return err
	}

	_, err = node.Execute(ctx, old, `SELECT 1`)
	if !errors.Is(err, pgislet.ErrStaleGeneration) {
		return fmt.Errorf("expected stale generation, got %v", err)
	}

	fmt.Fprintln(output, "Reset left an empty schema and fenced the old handle.")
	a, err = manager.Reinitialize(ctx, a, seed)
	if err != nil {
		return err
	}

	result, err = node.Execute(ctx, a, `SELECT count(*) FROM learners`)
	if err != nil {
		return err
	}

	if result.Rows[0][0] != "2" {
		return fmt.Errorf("unexpected restored rows: %v", result.Rows)
	}

	fmt.Fprintf(output, "Caller-provided initialization restored %v learners.\n", result.Rows[0][0])

	if err = manager.Delete(ctx, a); err != nil {
		return err
	}

	if err = manager.Delete(ctx, b); err != nil {
		return err
	}

	fmt.Fprintln(output, "Both islets deleted successfully.")
	return nil
}

func cleanup(manager *pgislet.Manager, id string, output io.Writer) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := manager.Open(ctx, id)
	if errors.Is(err, pgislet.ErrNotFound) {
		return
	}

	if err == nil && h.State == "initializing" {
		h, err = manager.Recover(ctx, h)
	}

	if err == nil {
		err = manager.Delete(ctx, h)
	}

	if err != nil {
		fmt.Fprintln(output, "cleanup:", err)
	}
}
