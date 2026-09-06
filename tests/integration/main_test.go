package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var testDSN string

func TestMain(m *testing.M) {
	if os.Getenv("PGISLET_CRASH_HELPER") == "1" {
		os.Exit(m.Run())
	}

	if os.Getenv("PGISLET_UNIT_ONLY") == "1" {
		os.Exit(m.Run())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:17-alpine", postgres.WithDatabase("pgislet"), postgres.WithUsername("postgres"), postgres.WithPassword("integration-secret"), postgres.BasicWaitStrategies())
	if err != nil {
		fmt.Fprintln(os.Stderr, "PostgreSQL testcontainer:", err)
		os.Exit(1)
	}

	testDSN, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(context.Background())
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()

	if err = container.Terminate(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}

	os.Exit(code)
}
