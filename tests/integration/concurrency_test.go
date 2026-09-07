package integration

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/swualabs/pgislet"
)

func waitForQuery(t *testing.T, m *fixture, h pgislet.Islet) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		var running bool
		err := m.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=(SELECT role_name FROM pgislet_internal.islets WHERE id=$1) AND state='active' AND query LIKE '%pg_sleep%')`, h.ID).Scan(&running)
		if err != nil {
			t.Fatal(err)
		}

		if running {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("runtime query did not start")
}

func TestDistributedConcurrency(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	other := manager(t, pgislet.Config{})
	h, b := islet(t, m), islet(t, m)

	for _, op := range []string{"execute", "reset", "delete", "batch", "reinitialize"} {
		t.Run(op, func(t *testing.T) {
			active, cancel := context.WithCancel(ctx)
			defer cancel()

			done := make(chan error, 1)
			go func() {
				_, err := m.Execute(active, h, `SELECT pg_sleep(3)`)
				done <- err
			}()
			waitForQuery(t, m, h)

			var err error

			switch op {
			case "execute":
				_, err = other.Execute(ctx, h, `SELECT 1`)
			case "reset":
				_, err = other.Reset(ctx, h)
			case "delete":
				err = other.Delete(ctx, h)
			case "batch":
				_, err = other.Batch(ctx, h, []string{`SELECT 1`})
			case "reinitialize":
				_, err = other.Reinitialize(ctx, h, nil)
			}

			if !errors.Is(err, pgislet.ErrBusy) {
				t.Fatal(err)
			}

			run(t, other, b, `SELECT 1`)
			cancel()
			<-done
			run(t, other, h, `SELECT 1`)
		})
	}

	old, err := m.lookup(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	h, err = other.Reset(ctx, h)
	if err != nil {
		t.Fatal(err)
	}

	md, err := m.lookup(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}

	md.Generation = old.Generation

	if err = m.enter(ctx, md, nil); !hasCode(err, "PI002") {
		t.Fatal(err)
	}
}

func TestReinitializeFencing(t *testing.T) {
	ctx := context.Background()
	m := manager(t, pgislet.Config{})
	other := manager(t, pgislet.Config{})
	h := islet(t, m)
	done := make(chan error, 1)
	go func() {
		_, err := m.Reinitialize(ctx, h, []string{`CREATE TABLE seed(n int)`, `SELECT pg_sleep(1)`, `INSERT INTO seed VALUES(1)`})
		done <- err
	}()
	waitForQuery(t, m, h)
	current, err := other.Open(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}

	if current.State != "initializing" {
		t.Fatal(current)
	}

	if _, err = other.Execute(ctx, current, `SELECT 1`); !errors.Is(err, pgislet.ErrBusy) && !errors.Is(err, pgislet.ErrUnavailable) {
		t.Fatal(err)
	}

	if err = <-done; err != nil {
		t.Fatal(err)
	}

	current, err = other.Open(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}

	run(t, other, current, `SELECT * FROM seed`)
}

func TestProcessCrash(t *testing.T) {
	if os.Getenv("PGISLET_CRASH_HELPER") == "1" {
		node, err := pgislet.New(context.Background(), pgislet.Config{DSN: os.Getenv("PGISLET_HELPER_DSN"), StatementTimeout: 2 * time.Second})
		if err != nil {
			t.Fatal(err)
		}

		h, err := node.Open(context.Background(), os.Getenv("PGISLET_HELPER_ID"))
		if err != nil {
			t.Fatal(err)
		}

		_, _ = node.Execute(context.Background(), h, `SELECT pg_sleep(30)`)
		return
	}

	m := manager(t, pgislet.Config{})
	h := islet(t, m)
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessCrash$")
	cmd.Env = append(os.Environ(), "PGISLET_CRASH_HELPER=1", "PGISLET_HELPER_DSN="+testDSN, "PGISLET_HELPER_ID="+h.ID)

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	waitForQuery(t, m, h)

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		_, err := m.Execute(context.Background(), h, `SELECT 1`)
		if err == nil {
			return
		}

		if !errors.Is(err, pgislet.ErrBusy) {
			t.Fatal(err)
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("crash did not release islet lock")
}
