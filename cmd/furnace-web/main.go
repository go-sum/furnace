package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-sum/furnace/internal/app"
)

// Version is set at build time via -ldflags "-X github.com/go-sum/furnace/cmd/furnace-web.Version=vX.Y.Z".
var Version = "dev"

func main() {
	var configPath string
	var listenAddr string

	flag.StringVar(&configPath, "config", "/etc/furnace/furnace.yaml", "path to configuration file")
	flag.StringVar(&listenAddr, "listen", ":8080", "HTTP listen address")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("furnace-web starting", "version", Version)
	if err := app.Run(ctx, configPath, listenAddr, logger); err != nil {
		logger.Error("furnace-web exited", "error", err)
		os.Exit(1)
	}
}
