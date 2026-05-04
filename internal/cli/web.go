package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-sum/foundry/pkg/web/serve"
	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/internal/app"
)

func newWebCmd(configPath *string) *cobra.Command {
	var listenAddr string

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Run the furnace-web HTTP server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

			cfg, err := app.LoadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			a, err := app.New(ctx, cfg, logger)
			if err != nil {
				return fmt.Errorf("create app: %w", err)
			}

			serverCfg := serve.InitialServerConfig()
			serverCfg.Addr = listenAddr

			logger.Info("furnace-web starting", "addr", listenAddr)
			if err := serve.ListenAndServe(ctx, a.Handler, serverCfg); err != nil {
				return fmt.Errorf("server: %w", err)
			}

			shutdownCtx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return a.Shutdown(shutdownCtx)
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen", ":8080", "HTTP listen address")
	return cmd
}
