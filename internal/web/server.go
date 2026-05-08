package web

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/go-sum/foundry/pkg/web/serve"

	"github.com/go-sum/furnace/internal/storage"
)

// Run opens dbPath, constructs the furnace-web app, and serves until ctx is cancelled.
func Run(ctx context.Context, dbPath, listenAddr string, logger *slog.Logger) error {
	db, err := storage.OpenDB(dbPath, true, logger)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	a, err := New(ctx, db, filepath.Dir(dbPath), logger)
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
