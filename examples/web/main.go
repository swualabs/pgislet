package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/swualabs/pgislet"
	"github.com/swualabs/pgislet/examples/web/app"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	disposable := flag.Bool("container", false, "start two disposable PostgreSQL 18 containers")
	migrateOnly := flag.Bool("migrate", false, "apply application database migrations and exit")
	healthcheck := flag.Bool("healthcheck", false, "check the local HTTP readiness endpoint")
	flag.Parse()

	if *healthcheck {
		client := &http.Client{Timeout: 4 * time.Second}
		response, err := client.Get("http://127.0.0.1:8080/readyz")
		if err != nil {
			return fmt.Errorf("readiness check failed")
		}

		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("server is not ready")
		}

		return nil
	}
	gin.SetMode(gin.ReleaseMode)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *disposable {
		for _, item := range []struct{ env, database string }{{"APP_DATABASE_URL", "playground_app"}, {"PGISLET_DATABASE_URL", "playground_islets"}} {
			setup, cancel := context.WithTimeout(ctx, 3*time.Minute)
			db, err := postgres.Run(setup, "postgres:18-alpine", postgres.WithDatabase(item.database), postgres.WithUsername("postgres"), postgres.WithPassword("disposable-example-secret"), postgres.BasicWaitStrategies())
			cancel()
			if err != nil {
				return err
			}

			defer func() {
				cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
				defer done()

				if err := db.Terminate(cleanup); err != nil {
					slog.Error("container cleanup failed", "error", err)
				}
			}()

			dsn, err := db.ConnectionString(ctx, "sslmode=disable")
			if err != nil {
				return err
			}

			if err := os.Setenv(item.env, dsn); err != nil {
				return err
			}
		}
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		return err
	}

	setup, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	store, err := app.OpenStore(setup, cfg.AppDSN)
	if err != nil {
		return fmt.Errorf("application database: %w", err)
	}

	defer store.DB.Close()

	if err := store.CheckIsolation(setup, cfg.IsletDSN); err != nil {
		return err
	}

	if err := store.Migrate(setup); err != nil {
		return fmt.Errorf("application migrations: %w", err)
	}

	if *migrateOnly {
		return nil
	}

	manager, err := pgislet.New(setup, pgislet.Config{DSN: cfg.IsletDSN, MaxSQLBytes: 64 << 10})
	if err != nil {
		return fmt.Errorf("workspace database: %w", err)
	}

	defer manager.Close()

	if _, err := os.Stat(cfg.Assets + "/index.html"); err != nil {
		return err
	}

	application, err := app.New(cfg, store, manager, os.DirFS(cfg.Assets))
	if err != nil {
		return err
	}

	server := &http.Server{Addr: cfg.Address, Handler: application, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
	stopped := make(chan error, 1)
	go func() {
		stopped <- server.ListenAndServe()
	}()
	slog.Info("playground listening", "origin", cfg.Origin, "address", cfg.Address)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	var serveErr error
	running := true

	for running {
		select {
		case <-ctx.Done():
			running = false
		case serveErr = <-stopped:
			running = false
		case <-ticker.C:
			cleanup, done := context.WithTimeout(ctx, 5*time.Second)

			if err := store.Cleanup(cleanup); err != nil {
				slog.Error("expired session cleanup failed")
			}

			done()
		}
	}

	shutdown, done := context.WithTimeout(context.Background(), 45*time.Second)
	defer done()

	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		return err
	}

	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}

	return serveErr
}
