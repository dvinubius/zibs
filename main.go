package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const shutdownTimeout = 5 * time.Second

const expiryCleanupInterval = 24 * time.Hour

const adminTokenEnvironmentVariable = "ADMIN_TOKEN"

const metricsAddressEnvironmentVariable = "METRICS_ADDRESS"

const defaultMetricsAddress = "127.0.0.1:9091"

type serverSpec struct {
	name     string
	server   *http.Server
	listener net.Listener
}

func serve(ctx context.Context, server *http.Server, listener net.Listener, logger *slog.Logger) error {
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(listener)
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("shutdown signal received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}

		err := <-serverErr
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP after shutdown: %w", err)
		}

		logger.Info("server stopped")
		return nil
	}
}

func serveAll(ctx context.Context, servers []serverSpec, logger *slog.Logger) error {
	serverCtx, cancelServers := context.WithCancel(ctx)
	defer cancelServers()

	serverErrs := make(chan error, len(servers))
	for _, spec := range servers {
		spec := spec
		go func() {
			err := serve(serverCtx, spec.server, spec.listener, logger.With("server", spec.name))
			if err != nil {
				serverErrs <- fmt.Errorf("%s server: %w", spec.name, err)
				return
			}
			serverErrs <- nil
		}()
	}

	firstErr := <-serverErrs
	cancelServers()
	secondErr := <-serverErrs
	if firstErr != nil {
		return firstErr
	}
	return secondErr
}

func run(ctx context.Context, address, metricsAddress, databasePath, adminToken string, logger *slog.Logger) error {
	if adminToken == "" {
		return fmt.Errorf("%s is required", adminTokenEnvironmentVariable)
	}

	db, err := openDB(databasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := migrate(db); err != nil {
		return err
	}

	metrics, registry, err := newMetrics()
	if err != nil {
		return fmt.Errorf("create metrics: %w", err)
	}
	if err := registerDBStatsMetrics(registry, db); err != nil {
		return fmt.Errorf("register database metrics: %w", err)
	}
	store := newLinkStore(db, metrics, logger)
	if err := registerActiveLinksMetric(registry, store); err != nil {
		return fmt.Errorf("register active links metric: %w", err)
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	metricsListener, err := net.Listen("tcp", metricsAddress)
	if err != nil {
		listener.Close()
		return fmt.Errorf("listen for metrics on %s: %w", metricsAddress, err)
	}

	server := &http.Server{
		Handler:           logRequests(logger, metrics, newHandler(store, adminToken)),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	metricsServer := &http.Server{
		Handler:           newMetricsHandler(registry),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("server started", "address", listener.Addr().String(), "database", databasePath)
	logger.Info("metrics server started", "address", metricsListener.Addr().String())

	cleanupCtx, cancelCleanup := context.WithCancel(ctx)
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		runExpiryCleanup(cleanupCtx, store, expiryCleanupInterval, logger, metrics)
	}()

	err = serveAll(ctx, []serverSpec{
		{name: "application", server: server, listener: listener},
		{name: "metrics", server: metricsServer, listener: metricsListener},
	}, logger)
	cancelCleanup()
	<-cleanupDone
	return err
}

func metricsAddressFromEnvironment() string {
	if address := os.Getenv(metricsAddressEnvironmentVariable); address != "" {
		return address
	}
	return defaultMetricsAddress
}

func runBackup(arguments []string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	databasePath := flags.String("database", "zibs.db", "SQLite database to back up")
	destinationPath := flags.String("output", "", "new backup file path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("backup does not accept positional arguments")
	}
	if *destinationPath == "" {
		return fmt.Errorf("backup requires --output")
	}
	if err := backupDatabase(*databasePath, *destinationPath); err != nil {
		return err
	}

	fmt.Printf("SQLite backup written to %s\n", *destinationPath)
	return nil
}

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] != "backup" {
			fmt.Fprintf(os.Stderr, "unknown command %q (expected backup)\n", os.Args[1])
			os.Exit(2)
		}
		if err := runBackup(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, ":8080", metricsAddressFromEnvironment(), "zibs.db", os.Getenv(adminTokenEnvironmentVariable), logger); err != nil {
		logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}
