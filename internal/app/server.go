package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/go-sum/foundry/pkg/web/serve"

	"github.com/go-sum/furnace/internal/storage"
)

// Run loads configPath, constructs the furnace-web app, and serves it until ctx
// is cancelled.
func Run(ctx context.Context, configPath, listenAddr string, logger *slog.Logger) error {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := storage.OpenDB(filepath.Join(cfg.DataDir, "furnace.db"), true, logger)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	a, err := New(cfg, db, logger)
	if err != nil {
		return fmt.Errorf("create app: %w", err)
	}

	serverCfg := serve.InitialServerConfig()
	serverCfg.Addr = listenAddr

	logger.Info("furnace-web starting", "addr", listenAddr)
	if err := serve.ListenAndServe(ctx, a.Handler, serverCfg); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
