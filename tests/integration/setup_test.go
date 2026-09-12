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

	major := os.Getenv("PGISLET_TEST_POSTGRES_MAJOR")

	if major == "" {
		major = "18"
	}

	if major != "17" && major != "18" {
		fmt.Fprintln(os.Stderr, "PGISLET_TEST_POSTGRES_MAJOR must be 17 or 18")
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "Integration PostgreSQL major:", major)
	container, err := postgres.Run(ctx, "postgres:"+major+"-alpine", postgres.WithDatabase("pgislet"), postgres.WithUsername("postgres"), postgres.WithPassword("integration-secret"), postgres.BasicWaitStrategies())
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
