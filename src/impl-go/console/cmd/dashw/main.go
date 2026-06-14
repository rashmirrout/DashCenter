// Command dashw is the DashCenter Web Console — a single binary that
// serves the SPA, proxies dashd APIs, bridges gRPC streams to
// WebSockets, and aggregates multi-endpoint data into view models.
//
// Usage:
//
//	dashw [flags]
//	dashw --listen :3000 --dashd-rest http://dashd-1:8443 --dashd-admin http://dashd-1:7443
//
// Flags are documented in internal/config. Environment variables
// (DASHW_LISTEN, DASHD_REST_ADDR, etc.) serve as fallbacks.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
	"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/server"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	cfg := config.Parse(os.Args[1:])
	cfg.Version = version

	// ── Structured JSON logger ──────────────────────────────────
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	// ── Graceful shutdown on SIGINT / SIGTERM ────────────────────
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// ── Build and run the server ────────────────────────────────
	srv, err := server.New(cfg, logger)
	if err != nil {
		slog.Error("server init failed", "error", err)
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}

	slog.Info("dashw shutdown complete")
}