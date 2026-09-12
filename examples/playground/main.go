package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	disposable := flag.Bool("container", false, "start and remove a disposable PostgreSQL 18 Docker container")
	dsn := flag.String("dsn", os.Getenv("PGISLET_DSN"), "management DSN for a dedicated PostgreSQL 17 or 18 database")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if *disposable {
		db, err := postgres.Run(ctx, "postgres:18-alpine", postgres.WithDatabase("pgislet_example"), postgres.WithUsername("postgres"), postgres.WithPassword("disposable-example-secret"), postgres.BasicWaitStrategies())
		if err != nil {
			return err
		}

		defer func() {
			cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
			defer done()

			if err := db.Terminate(cleanup); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}()

		*dsn, err = db.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			return err
		}
	}

	if *dsn == "" {
		return errors.New("provide -container, -dsn, or PGISLET_DSN")
	}

	return Run(ctx, *dsn, os.Stdout)
}
