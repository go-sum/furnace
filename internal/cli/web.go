package cli

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/internal/app"
)

func newWebCmd() *cobra.Command {
	var listenAddr string

	cmd := &cobra.Command{
		Use:    "web",
		Short:  "Run the furnace-web HTTP server",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
			return app.Run(ctx, DBPath, listenAddr, logger)
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen", ":8080", "HTTP listen address")
	return cmd
}
