package webtest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var appDSN, isletDSN string

func TestMain(m *testing.M) {
	if os.Getenv("PGISLET_UNIT_ONLY") == "1" {
		os.Exit(m.Run())
	}

	code := runTests(m)
	os.Exit(code)
}

func runTests(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	major := os.Getenv("PGISLET_TEST_POSTGRES_MAJOR")

	if major == "" {
		major = "18"
	}

	if major != "17" && major != "18" {
		fmt.Fprintln(os.Stderr, "PGISLET_TEST_POSTGRES_MAJOR must be 17 or 18")
		return 1
	}

	for _, target := range []struct {
		name string
		dsn  *string
	}{{"web_app", &appDSN}, {"web_islets", &isletDSN}} {
		db, err := postgres.Run(ctx, "postgres:"+major+"-alpine", postgres.WithDatabase(target.name), postgres.WithUsername("postgres"), postgres.WithPassword("web-integration-secret"), postgres.BasicWaitStrategies())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		defer func() {
			cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
			defer done()
			_ = db.Terminate(cleanup)
		}()

		*target.dsn, err = db.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	return m.Run()
}
