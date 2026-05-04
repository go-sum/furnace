package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/internal/app"
)

// Version is set at build time via -ldflags "-X github.com/go-sum/furnace/internal/cli.Version=vX.Y.Z".
var Version = "dev"

func NewRootCommand() *cobra.Command {
	var configPath string

	root := &cobra.Command{
		Use:          "furnace",
		Short:        "Secure VPS deployment agent",
		Long:         fmt.Sprintf("Secure VPS deployment agent %s", Version),
		Version:      Version,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&configPath, "config", "/etc/furnace/furnace.yaml",
		"path to configuration file")

	root.AddCommand(
		newInitCmd(),
		newStartCmd(&configPath),
		newWebCmd(&configPath),
		newWorkerCmd(&configPath),
		newProxyCmd(&configPath),
		newValidateCmd(&configPath),
	)

	return root
}

func newValidateCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := app.LoadConfig(*configPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config ok: %d app(s)\n", len(cfg.Apps))
			return nil
		},
	}
}
