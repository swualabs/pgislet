package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/swualabs/pgislet"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	_, source, _, _ := runtime.Caller(0)
	flags := flag.NewFlagSet("pgislet-web", flag.ContinueOnError)
	disposable := flags.Bool("container", false, "use a disposable PostgreSQL 18 Docker container")
	dsn := flags.String("dsn", os.Getenv("PGISLET_DSN"), "management DSN for a dedicated PostgreSQL 17 or 18 database")
	addr := flags.String("addr", "127.0.0.1:8080", "HTTP listen address (loopback only)")
	assets := flags.String("assets", filepath.Join(filepath.Dir(source), "static"), "frontend asset directory")

	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}

	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return fmt.Errorf("listen address: %w", err)
	}

	ip := net.ParseIP(host)

	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("the example listen address must be localhost or a loopback IP")
	}

	if _, err := os.Stat(filepath.Join(*assets, "index.html")); err != nil {
		return fmt.Errorf("frontend assets: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *disposable {
		setup, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()

		db, err := postgres.Run(setup, "postgres:18-alpine", postgres.WithDatabase("pgislet_web"), postgres.WithUsername("postgres"), postgres.WithPassword("disposable-web-secret"), postgres.BasicWaitStrategies())
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

		*dsn, err = db.ConnectionString(setup, "sslmode=disable")
		if err != nil {
			return err
		}
	}

	if *dsn == "" {
		return errors.New("provide -container, -dsn, or PGISLET_DSN")
	}

	manager, err := pgislet.New(ctx, pgislet.Config{DSN: *dsn, MaxSQLBytes: 64 << 10})
	if err != nil {
		return err
	}

	defer manager.Close()

	app := New(manager, os.DirFS(*assets))
	server := &http.Server{Addr: *addr, Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
	stopped := make(chan error, 1)
	go func() {
		stopped <- server.ListenAndServe()
	}()

	fmt.Printf("pgislet web playground: http://%s\nPress Ctrl+C to stop and remove example workspaces.\n", *addr)
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
			if err := app.Cleanup(ctx, false); err != nil {
				fmt.Fprintln(os.Stderr, "expired workspace cleanup:", err)
			}
		}
	}

	shutdown, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		serveErr = errors.Join(serveErr, err)
	}

	cleanupErr := app.Cleanup(shutdown, true)

	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}

	return errors.Join(serveErr, cleanupErr)
}
