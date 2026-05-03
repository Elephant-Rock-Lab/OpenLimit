package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/logging"
	"openlimit/internal/migrate"
	"openlimit/internal/server"
	"openlimit/internal/store"
	"openlimit/pkg/version"
)

func main() {
	// Check for --migrate-only flag
	for _, arg := range os.Args[1:] {
		if arg == "--migrate-only" {
			os.Exit(runMigrateOnly())
		}
	}

	cfg, err := config.Load("configs/gateway.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.Logging)
	slog.SetDefault(logger)

	var db *sql.DB
	if cfg.Database.URL != "" {
		var dbErr error
		db, dbErr = store.Open(cfg.Database)
		if dbErr != nil {
			logger.Error("failed to connect to database", "error", dbErr)
			os.Exit(1)
		}
		defer db.Close()

		if err := migrate.Run(db); err != nil {
			logger.Error("failed to run migrations", "error", err)
			os.Exit(1)
		}
		logger.Info("database migrations applied")
	} else if cfg.Auth.Enabled {
		logger.Error("auth is enabled but database.url is not configured")
		os.Exit(1)
	}

	runtime := server.NewRuntime(cfg, logger, db)
	srv := runtime.Server

	// Start config hot-reload watcher
	configWatcher := config.NewWatcher("configs/gateway.yaml", cfg, func(old, newCfg config.Config) {
		logger.Info("config hot-reloaded",
			"old_level", old.Logging.Level,
			"new_level", newCfg.Logging.Level,
			"providers", len(newCfg.Providers),
			"models", len(newCfg.Models),
		)
	}, logger)
	configWatcher.StartBackground()

	go func() {
		logger.Info("starting OpenLimit gateway",
			"version", version.Version,
			"addr", cfg.Server.Address(),
		)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down gateway")
	runtime.Tracker.MarkShuttingDown()

	// Stop config watcher
	configWatcher.Close()

	// Shut down A2A handler (drains workers, cancels in-flight tasks)
	if runtime.A2AHandler != nil {
		runtime.A2AHandler.Shutdown()
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout(cfg))
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	runtime.Tracker.Wait()
	logger.Info("gateway stopped")
}

func shutdownTimeout(cfg config.Config) time.Duration {
	if cfg.Server.ShutdownTimeout > 0 {
		return time.Duration(cfg.Server.ShutdownTimeout) * time.Millisecond
	}
	return 10 * time.Second
}

// runMigrateOnly loads config, connects to the database, runs migrations, and exits.
// Returns 0 on success, 1 on failure.
func runMigrateOnly() int {
	cfg, err := config.Load("configs/gateway.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		return 1
	}

	if cfg.Database.URL == "" {
		fmt.Fprintln(os.Stderr, "database.url is required for migrations")
		return 1
	}

	db, err := store.Open(cfg.Database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to database: %v\n", err)
		return 1
	}
	defer db.Close()

	if err := migrate.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "failed to run migrations: %v\n", err)
		return 1
	}

	fmt.Println("migrations applied successfully")
	return 0
}
