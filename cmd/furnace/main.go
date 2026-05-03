package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-sum/foundry/pkg/web/serve"

	"github.com/go-sum/furnace/internal/app"
)

func main() {
	configPath := flag.String("config", "/etc/furnace/furnace.yaml", "path to configuration file")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := run(*configPath, logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	cfg, err := app.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	application, err := app.New(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("create app: %w", err)
	}

	logger.Info("starting furnace", "addr", cfg.Listen)

	if err := serve.ListenAndServe(ctx, application.Handler, serve.ServerConfig{
		Addr:              cfg.Listen,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   30 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := application.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown app: %w", err)
	}

	logger.Info("stopped")
	return nil
}
